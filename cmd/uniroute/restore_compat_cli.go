package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

const restoreCompatibilitySchema = "unirouter.community-restore-compatibility.v1"

type restoreCompatibilityInput struct {
	SchemaVersion    string                    `json:"schema_version"`
	Source           communityMaintenanceInput `json:"source"`
	CloneDatabase    string                    `json:"clone_database"`
	CloneDatabaseOID int64                     `json:"clone_database_oid"`
	DumpPath         string                    `json:"dump_path"`
	DumpSHA256       string                    `json:"dump_sha256"`
}

type restoreCompatibilityResult struct {
	SchemaVersion       string                         `json:"schema_version"`
	Status              string                         `json:"status"`
	SourcePackageSHA256 string                         `json:"source_package_sha256"`
	CloneDatabase       string                         `json:"clone_database"`
	CloneDatabaseOID    int64                          `json:"clone_database_oid"`
	DumpSHA256          string                         `json:"dump_sha256"`
	CorrectionMigration string                         `json:"correction_migration"`
	CorrectionSHA256    string                         `json:"correction_sha256"`
	Original            upstreamrelease.NamespaceShape `json:"source_definition"`
	Restored            upstreamrelease.NamespaceShape `json:"restored_definition"`
}

func runRestoreCompatibilityCLI(ctx context.Context, args []string, cfg *config.Config, clone *pgxpool.Pool, apply bool) error {
	if len(args) != 0 {
		return fmt.Errorf("restore compatibility accepts one typed stdin document")
	}
	var in restoreCompatibilityInput
	if err := readBoundedCLIJSON(os.Stdin, 64<<10, &in); err != nil {
		return err
	}
	// Validate the exact source name before deriving any connection: an empty
	// database must never select libpq's implicit default.
	if err := in.Source.validate(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.DB_DSN) == "" {
		return fmt.Errorf("explicit restore clone connection required")
	}
	connection, err := pgxpool.ParseConfig(cfg.DB_DSN)
	if err != nil {
		return fmt.Errorf("restore connection configuration invalid")
	}
	if connection.ConnConfig.Database != in.CloneDatabase || connection.ConnConfig.Host == "" {
		return fmt.Errorf("restore connection must name the exact bound clone")
	}
	connection.ConnConfig.Database = in.Source.ExpectedDatabase
	source, err := pgxpool.NewWithConfig(ctx, connection)
	if err != nil {
		return fmt.Errorf("restore source connection unavailable")
	}
	defer source.Close()
	result, err := runRestoreCompatibility(ctx, source, clone, cfg, in, apply)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

// This participant belongs to the existing cold-restore owner. It can mutate
// only an empty, exact, separately created clone between pre-data and COPY.
// Source/package authority is the existing native signed maintenance check.
func runRestoreCompatibility(ctx context.Context, source, clone *pgxpool.Pool, cfg *config.Config, in restoreCompatibilityInput, apply bool) (restoreCompatibilityResult, error) {
	var out restoreCompatibilityResult
	if in.SchemaVersion != restoreCompatibilitySchema || len(in.Source.Retirements) != 0 ||
		!regexp.MustCompile(`^ur_upgrade_verify_[a-f0-9]{24}$`).MatchString(in.CloneDatabase) ||
		in.CloneDatabase == in.Source.ExpectedDatabase || in.CloneDatabaseOID <= 0 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(in.DumpSHA256) {
		return out, fmt.Errorf("exact isolated pre-data restore binding required")
	}
	if _, err := runCommunityMaintenance(ctx, source, cfg, in.Source, false); err != nil {
		return out, err
	}
	installation, err := installstate.RequireOrdinary(ctx, source)
	if err != nil {
		return out, err
	}
	if installation.Ledger[len(installation.Ledger)-1] != schemaauthority.UpstreamReleaseObservationHead {
		return out, fmt.Errorf("restore compatibility applies only to the exact M128 source")
	}
	original, err := upstreamrelease.ReadNamespace(ctx, source)
	if err != nil {
		return out, err
	}
	if !filepath.IsAbs(in.DumpPath) || filepath.Clean(in.DumpPath) != in.DumpPath || filepath.Base(in.DumpPath) != "database.dump" || filepath.Base(filepath.Dir(in.DumpPath)) != "backup" {
		return out, fmt.Errorf("exact retained cold dump path required")
	}
	info, err := os.Lstat(in.DumpPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return out, fmt.Errorf("private regular cold dump required")
	}
	file, err := os.Open(in.DumpPath)
	if err != nil {
		return out, err
	}
	defer file.Close()
	header := make([]byte, 5)
	if _, err = io.ReadFull(file, header); err != nil || string(header) != "PGDMP" {
		return out, fmt.Errorf("native custom cold dump required")
	}
	hash := sha256.New()
	_, _ = hash.Write(header)
	if _, err = io.Copy(hash, file); err != nil {
		return out, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != in.DumpSHA256 {
		return out, fmt.Errorf("cold dump identity changed")
	}
	correction, err := upstreamrelease.NamespaceCorrection(configuredMigrationsDir(cfg))
	if err != nil {
		return out, err
	}
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if apply {
		options = pgx.TxOptions{IsoLevel: pgx.Serializable}
	}
	tx, err := clone.BeginTx(ctx, options)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.Background())
	var database, system string
	var oid int64
	var owned bool
	err = tx.QueryRow(ctx, `SELECT current_database(),oid::bigint,pg_get_userbyid(datdba)=current_user,
(SELECT system_identifier::text FROM pg_control_system()) FROM pg_database WHERE datname=current_database()`).Scan(&database, &oid, &owned, &system)
	if err != nil {
		return out, err
	}
	if database != in.CloneDatabase || oid != in.CloneDatabaseOID || !owned || system != in.Source.ExpectedDatabaseSystemID {
		return out, fmt.Errorf("isolated restore database identity changed")
	}
	rows, err := tx.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		return out, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return out, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(names) == 0 || len(names) > 1024 {
		return out, fmt.Errorf("restore pre-data schema is absent or unbounded")
	}
	for _, name := range names {
		var nonempty bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+pgx.Identifier{"public", name}.Sanitize()+`)`).Scan(&nonempty); err != nil {
			return out, err
		}
		if nonempty {
			return out, fmt.Errorf("restore compatibility requires empty pre-data tables")
		}
	}
	before, err := upstreamrelease.ReadNamespace(ctx, tx)
	if err != nil {
		return out, err
	}
	// Before repair the clone must carry precisely the source definition. A
	// known canonical result is the only accepted ambiguous-commit readback.
	canonical := len(before.Config) == 1 && before.Config[0] == upstreamrelease.NamespaceConfig
	if !canonical && strings.Join(before.Config, "\x00") != strings.Join(original.Config, "\x00") {
		return out, fmt.Errorf("restore validator differs from the signed source shape")
	}
	if apply && !canonical {
		if _, err = tx.Exec(ctx, correction); err != nil {
			return out, err
		}
	}
	after, err := upstreamrelease.ReadNamespace(ctx, tx)
	if err != nil {
		return out, err
	}
	status := "eligible"
	if len(after.Config) == 1 && after.Config[0] == upstreamrelease.NamespaceConfig {
		status = "applied"
	}
	if err = tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("restore compatibility outcome unconfirmed; read back before repeating: %w", err)
	}
	digest := sha256.Sum256([]byte(correction))
	return restoreCompatibilityResult{restoreCompatibilitySchema, status, in.Source.ExpectedSourcePackageSHA256, database, oid, in.DumpSHA256, upstreamrelease.NamespaceMigration, hex.EncodeToString(digest[:]), original, after}, nil
}
