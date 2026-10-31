package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

type installedReleaseState struct {
	ReleaseSequence int64
	Version         string
	Commit          string
	PackageSHA256   string
	TrustedKeyID    string
	TargetSchema    string
	UpdatedAt       time.Time
}

func runUpgradeCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute upgrade <status|verify|record-applied|maintenance-check|maintenance-retire|preflight-legacy-rollback>")
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer postgres.Close()

	switch args[0] {
	case "status":
		return printUpgradeStatus(ctx, postgres.Pool)
	case "verify":
		return runInstalledUpgradeVerify(ctx, args[1:], cfg, postgres.Pool)
	case "record-applied":
		return runUpgradeRecordApplied(ctx, args[1:], cfg, postgres.Pool)
	case "restore-compat-check", "restore-compat-apply":
		return runRestoreCompatibilityCLI(ctx, args[1:], cfg, postgres.Pool, args[0] == "restore-compat-apply")
	case "maintenance-check", "maintenance-retire":
		if len(args) != 1 {
			return fmt.Errorf("community maintenance accepts its bounded input only on stdin")
		}
		var input communityMaintenanceInput
		if err := readBoundedCLIJSON(os.Stdin, 64<<10, &input); err != nil {
			return err
		}
		result, err := runCommunityMaintenance(ctx, postgres.Pool, cfg, input, args[0] == "maintenance-retire")
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	case "preflight-legacy-rollback":
		if len(args) != 1 {
			return fmt.Errorf("usage: uniroute upgrade preflight-legacy-rollback")
		}
		return preflightLegacyPurposeRollback(ctx, postgres.Pool)
	default:
		return fmt.Errorf("unknown upgrade command %q; supported: status, verify, record-applied, maintenance-check, maintenance-retire, preflight-legacy-rollback", args[0])
	}
}

func preflightLegacyPurposeRollback(ctx context.Context, querier releaseStateQuerier) error {
	var activePurposeKeys int
	if err := querier.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM api_keys
		 WHERE purpose <> ''
		   AND status IN ('active', 'feed_active', 'feed_staged')`,
	).Scan(&activePurposeKeys); err != nil {
		return fmt.Errorf("read active purpose-scoped credential count: %w", err)
	}
	if activePurposeKeys > 0 {
		return fmt.Errorf(
			"legacy rollback refused: %d active purpose-scoped credential(s); stop feed consumption and disable the exact feed credential before starting a binary without purpose semantics",
			activePurposeKeys,
		)
	}
	fmt.Println("rollback_preflight=pass")
	fmt.Println("active_purpose_keys=0")
	return nil
}

func printUpgradeStatus(ctx context.Context, pool *pgxpool.Pool) error {
	state, err := readInstalledReleaseState(ctx, pool, false)
	if err != nil {
		return err
	}
	schema, err := readCurrentSchema(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("release_sequence=%d\nversion=%s\ncommit=%s\npackage_sha256=%s\ntrusted_key_id=%s\nrecorded_target_schema=%s\ncurrent_schema=%s\nupdated_at=%s\n",
		state.ReleaseSequence, state.Version, state.Commit, state.PackageSHA256, state.TrustedKeyID,
		state.TargetSchema, schema, state.UpdatedAt.UTC().Format(time.RFC3339))
	return nil
}

func runInstalledUpgradeVerify(ctx context.Context, args []string, cfg *config.Config, pool *pgxpool.Pool) error {
	fs := flag.NewFlagSet("upgrade verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	packagePath := fs.String("package", "", "signed upgrade bundle")
	atText := fs.String("at", "", "optional RFC3339 diagnostic verification time")
	maxPayloadBytes := fs.Int64("max-payload-bytes", 0, "optional payload byte limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*packagePath) == "" {
		return fmt.Errorf("--package is required")
	}
	state, err := readInstalledReleaseState(ctx, pool, false)
	if err != nil {
		return err
	}
	schema, err := readCurrentSchema(ctx, pool)
	if err != nil {
		return err
	}
	now := time.Now()
	if strings.TrimSpace(*atText) != "" {
		now, err = time.Parse(time.RFC3339, *atText)
		if err != nil {
			return fmt.Errorf("--at must be RFC3339: %w", err)
		}
	}
	verification, err := verifyInstalledPackage(*packagePath, cfg.UpgradePublicKeys, state.ReleaseSequence, schema, now, *maxPayloadBytes)
	if err != nil {
		return err
	}
	manifest := verification.Manifest
	fmt.Printf("verified=true\npackage_sha256=%s\nrelease_sequence=%d\nversion=%s\ncommit=%s\ntarget_schema=%s\npayload_bytes=%d\n",
		verification.BundleSHA256, manifest.ReleaseSequence, manifest.Version, manifest.Commit, manifest.TargetSchema, verification.PayloadBytes)
	return nil
}

func runUpgradeRecordApplied(ctx context.Context, args []string, cfg *config.Config, pool *pgxpool.Pool) error {
	fs := flag.NewFlagSet("upgrade record-applied", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	packagePath := fs.String("package", "", "signed upgrade bundle")
	recordedBy := fs.String("recorded-by", "", "operator or deployment identity")
	expectedSourceSequence := fs.Int64("expected-source-sequence", 0, "optional exact installed predecessor sequence")
	expectedSourcePackage := fs.String("expected-source-package-sha256", "", "optional exact installed predecessor package digest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	*recordedBy = strings.TrimSpace(*recordedBy)
	if strings.TrimSpace(*packagePath) == "" || *recordedBy == "" {
		return fmt.Errorf("--package and --recorded-by are required")
	}
	if len(*recordedBy) > 128 {
		return fmt.Errorf("--recorded-by must be at most 128 characters")
	}
	if *expectedSourceSequence < 0 || (*expectedSourceSequence == 0) != (*expectedSourcePackage == "") {
		return fmt.Errorf("expected source sequence and package must be supplied together for an installed predecessor")
	}
	if *expectedSourcePackage != "" {
		digest, err := hex.DecodeString(*expectedSourcePackage)
		if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != *expectedSourcePackage {
			return fmt.Errorf("expected source package must be a lowercase SHA-256 digest")
		}
	}
	if strings.TrimSpace(cfg.RuntimeVersion) == "" || strings.TrimSpace(cfg.RuntimeCommit) == "" {
		return fmt.Errorf("RUNTIME_VERSION and RUNTIME_COMMIT must identify the running release before recording it")
	}
	publicKeys, err := signing.ParsePublicKeyRing(cfg.UpgradePublicKeys, "UPGRADE_PUBLIC_KEYS")
	if err != nil {
		return err
	}
	if len(publicKeys) == 0 {
		return fmt.Errorf("UPGRADE_PUBLIC_KEYS is required")
	}
	file, err := os.Open(*packagePath)
	if err != nil {
		return fmt.Errorf("open upgrade package: %w", err)
	}
	defer file.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin release-state transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := installstate.RequireOrdinary(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('unirouter.release_state', 0))`); err != nil {
		return fmt.Errorf("lock release state: %w", err)
	}
	state, err := readInstalledReleaseState(ctx, tx, true)
	if err != nil {
		return err
	}
	if *expectedSourceSequence > 0 && (state.ReleaseSequence != *expectedSourceSequence || state.PackageSHA256 != *expectedSourcePackage) {
		return fmt.Errorf("installed release predecessor changed before target recording")
	}
	schema, err := readCurrentSchema(ctx, tx)
	if err != nil {
		return err
	}
	verification, err := upgradebundle.Verify(file, upgradebundle.VerifyOptions{
		PublicKeys:      publicKeys,
		Now:             time.Now(),
		TargetOS:        runtime.GOOS,
		TargetArch:      runtime.GOARCH,
		CurrentSequence: state.ReleaseSequence,
		CurrentSchema:   schema,
	})
	if err != nil {
		return fmt.Errorf("verify upgrade package: %w", err)
	}
	manifest := verification.Manifest
	if manifest.Version != cfg.RuntimeVersion || manifest.Commit != cfg.RuntimeCommit {
		return fmt.Errorf("running release %s/%s does not match signed package %s/%s",
			cfg.RuntimeVersion, cfg.RuntimeCommit, manifest.Version, manifest.Commit)
	}
	if schema != manifest.TargetSchema {
		return fmt.Errorf("current schema %q has not reached signed target schema %q", schema, manifest.TargetSchema)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal verified manifest: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO release_history (
			release_sequence, version, commit, package_sha256, trusted_key_id,
			target_schema, manifest, recorded_by, recorded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, NOW())`,
		manifest.ReleaseSequence, manifest.Version, manifest.Commit, verification.BundleSHA256,
		manifest.KeyID, manifest.TargetSchema, string(manifestJSON), *recordedBy,
	); err != nil {
		return fmt.Errorf("record release history: %w", err)
	}
	result, err := tx.Exec(ctx, `
		UPDATE release_state SET
			release_sequence = $1,
			version = $2,
			commit = $3,
			package_sha256 = $4,
			trusted_key_id = $5,
			target_schema = $6,
			manifest = $7::jsonb,
			updated_at = NOW()
		WHERE singleton = TRUE`,
		manifest.ReleaseSequence, manifest.Version, manifest.Commit, verification.BundleSHA256,
		manifest.KeyID, manifest.TargetSchema, string(manifestJSON),
	)
	if err != nil {
		return fmt.Errorf("advance release state: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("advance release state: expected one singleton row, updated %d", result.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit release state: %w", err)
	}
	fmt.Printf("recorded=true\nrelease_sequence=%d\nversion=%s\ncommit=%s\npackage_sha256=%s\ntarget_schema=%s\n",
		manifest.ReleaseSequence, manifest.Version, manifest.Commit, verification.BundleSHA256, manifest.TargetSchema)
	return nil
}

func verifyInstalledPackage(packagePath, encodedKeys string, currentSequence int64, currentSchema string, now time.Time, maxPayloadBytes int64) (upgradebundle.Verification, error) {
	publicKeys, err := signing.ParsePublicKeyRing(encodedKeys, "UPGRADE_PUBLIC_KEYS")
	if err != nil {
		return upgradebundle.Verification{}, err
	}
	if len(publicKeys) == 0 {
		return upgradebundle.Verification{}, fmt.Errorf("UPGRADE_PUBLIC_KEYS is required")
	}
	file, err := os.Open(packagePath)
	if err != nil {
		return upgradebundle.Verification{}, fmt.Errorf("open upgrade package: %w", err)
	}
	verification, verifyErr := upgradebundle.Verify(file, upgradebundle.VerifyOptions{
		PublicKeys:      publicKeys,
		Now:             now,
		TargetOS:        runtime.GOOS,
		TargetArch:      runtime.GOARCH,
		CurrentSequence: currentSequence,
		CurrentSchema:   currentSchema,
		MaxPayloadBytes: maxPayloadBytes,
	})
	closeErr := file.Close()
	if verifyErr != nil {
		return upgradebundle.Verification{}, fmt.Errorf("verify upgrade package: %w", verifyErr)
	}
	if closeErr != nil {
		return upgradebundle.Verification{}, fmt.Errorf("close upgrade package: %w", closeErr)
	}
	return verification, nil
}

type releaseStateQuerier interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func readInstalledReleaseState(ctx context.Context, querier releaseStateQuerier, forUpdate bool) (installedReleaseState, error) {
	query := `SELECT release_sequence, version, commit, COALESCE(package_sha256, ''), trusted_key_id, target_schema, updated_at
	          FROM release_state WHERE singleton = TRUE`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var state installedReleaseState
	if err := querier.QueryRow(ctx, query).Scan(
		&state.ReleaseSequence, &state.Version, &state.Commit, &state.PackageSHA256,
		&state.TrustedKeyID, &state.TargetSchema, &state.UpdatedAt,
	); err != nil {
		return installedReleaseState{}, fmt.Errorf("read installed release state: %w", err)
	}
	return state, nil
}

func readCurrentSchema(ctx context.Context, querier releaseStateQuerier) (string, error) {
	var schema string
	if err := querier.QueryRow(ctx, `SELECT COALESCE(MAX(version), '') FROM schema_migrations`).Scan(&schema); err != nil {
		return "", fmt.Errorf("read current schema: %w", err)
	}
	if schema == "" {
		return "", fmt.Errorf("database has no applied schema migration")
	}
	return schema, nil
}
