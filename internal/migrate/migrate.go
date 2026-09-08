package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// migrationAdvisoryLockID serializes schema discovery and application across
// every UniRouter replica sharing a database. It is session-scoped so the lock
// covers the complete migration list, including the gaps between individual
// transactions.
const migrationAdvisoryLockID int64 = 86420517831702491

// CapabilityContractPublicationMarker identifies declaration-owned migrations
// that ordinary startup and migrate-up paths must never apply implicitly.
const CapabilityContractPublicationMarker = "-- unirouter:capability-contract-publication-v1"
const CompatibleExpandMarker = "-- unirouter:explicit-compatible-expand-v1"

// CompatibleExpandHooks belongs to the same schema migration owner/lock.
// Before must prove the exact serving fleet is already running the bridge.
// After installs the verified initial leases in the same transaction as DDL.
// Installation into an isolated empty test DB supplies its own explicit proof.
type CompatibleExpandHooks struct {
	Version        string
	ExpectedSHA256 string
	Before         func(context.Context, *pgxpool.Conn) error
	After          func(context.Context, pgx.Tx) error
}

// PublicationHooks bind one exact declaration-owned migration to its validated
// environment Plan. All callbacks run while the same physical connection owns
// the session migration lock.
type PublicationHooks struct {
	Version              string
	ExpectedUpSHA256     map[string]string
	ExpectedVerifySHA256 string
	Before               func(context.Context, *pgxpool.Conn) error
	ApplyProjection      func(context.Context, pgx.Tx) error
	VerifyInTx           func(context.Context, pgx.Tx) error
	VerifyPostCommit     func(context.Context, *pgxpool.Conn) error
}

// PublicationDownHooks bind a guarded recovery to the original exact Plan.
// Down is never an ordinary startup migration path.
type PublicationDownHooks struct {
	Version            string
	ExpectedUpSHA256   map[string]string
	ExpectedDownSHA256 string
	Before             func(context.Context, *pgxpool.Conn) error
	RevertProjection   func(context.Context, pgx.Tx) error
	VerifyInTx         func(context.Context, pgx.Tx) error
	VerifyPostCommit   func(context.Context, *pgxpool.Conn) error
}

type runOptions struct {
	allowedMigrations  []string
	ceiling            string
	requireExactPrefix bool
	publication        *PublicationHooks
	runtimeBridge      bool
	scopeSet           bool
	expand             *CompatibleExpandHooks
}

// Run applies all .up.sql migration files from dir that haven't been applied yet.
// It creates a schema_migrations table to track applied versions.
func Run(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	return run(ctx, pool, dir, runOptions{})
}

// RunRuntime preserves pending explicit expansions. The caller must then
// verify the actual complete ledger against the finite runtime bridge.
func RunRuntime(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	return run(ctx, pool, dir, runOptions{runtimeBridge: true})
}

func RunCompatibleExpand(ctx context.Context, pool *pgxpool.Pool, dir string, hooks CompatibleExpandHooks) error {
	if hooks.Before == nil || hooks.After == nil || len(hooks.ExpectedSHA256) != 71 || !strings.HasPrefix(hooks.ExpectedSHA256, "sha256:") {
		return fmt.Errorf("compatible expand requires exact bytes and a fleet preflight")
	}
	if hooks.Version == "" {
		hooks.Version = schemaauthority.CapabilityExpandHead
	}
	if hooks.Version != schemaauthority.CapabilityExpandHead && hooks.Version != schemaauthority.StreamCounterExpandHead {
		return fmt.Errorf("unknown exact expand target")
	}
	return run(ctx, pool, dir, runOptions{expand: &hooks, ceiling: hooks.Version, requireExactPrefix: true})
}

// RunThrough is an explicit install/restore primitive. It applies exactly the
// artifact prefix through ceiling and refuses to cross a declaration-owned
// publication. Ordinary backend startup must use Run, not this function.
func RunThrough(ctx context.Context, pool *pgxpool.Pool, dir, ceiling string) error {
	entries, err := migrationFiles(dir)
	if err != nil {
		return err
	}
	found := false
	for _, file := range entries {
		version := strings.TrimSuffix(file, ".up.sql")
		if version == ceiling {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("migration ceiling %q is not an exact artifact migration", ceiling)
	}
	return run(ctx, pool, dir, runOptions{ceiling: ceiling, requireExactPrefix: true})
}

// RunPublication applies the exact pending prefix authorized by one validated
// publication Plan and stops at hooks.Version. It never applies later pending
// migrations from the artifact.
func RunPublication(ctx context.Context, pool *pgxpool.Pool, dir string, allowedMigrations []string, hooks PublicationHooks) error {
	if hooks.Version == "" || len(allowedMigrations) == 0 || allowedMigrations[len(allowedMigrations)-1] != hooks.Version {
		return fmt.Errorf("publication migration and exact allowed prefix are required")
	}
	if len(hooks.ExpectedUpSHA256) == 0 || strings.TrimSpace(hooks.ExpectedVerifySHA256) == "" {
		return fmt.Errorf("publication requires exact migration and verifier digests")
	}
	if hooks.Before == nil || hooks.ApplyProjection == nil || hooks.VerifyInTx == nil || hooks.VerifyPostCommit == nil {
		return fmt.Errorf("publication requires complete Plan validation, projection, and verification hooks")
	}
	return run(ctx, pool, dir, runOptions{
		allowedMigrations: append([]string(nil), allowedMigrations...),
		publication:       &hooks,
	})
}

// WithSessionLock executes one read-only or recovery operation on the same
// physical connection while the existing migration-owner lock is held.
func WithSessionLock(ctx context.Context, pool *pgxpool.Pool, fn func(*pgxpool.Conn) error) error {
	conn, release, err := acquireSessionLock(ctx, pool)
	if err != nil {
		return err
	}
	defer release()
	return fn(conn)
}

// RunPublicationDown executes one generated guarded down migration plus the
// exact Plan-owned projection rollback in one transaction.
func RunPublicationDown(ctx context.Context, pool *pgxpool.Pool, dir string, hooks PublicationDownHooks) error {
	if strings.TrimSpace(hooks.Version) == "" {
		return fmt.Errorf("publication down requires an exact version")
	}
	if len(hooks.ExpectedUpSHA256) == 0 || strings.TrimSpace(hooks.ExpectedDownSHA256) == "" {
		return fmt.Errorf("publication down requires exact migration digests")
	}
	if hooks.Before == nil || hooks.RevertProjection == nil || hooks.VerifyInTx == nil || hooks.VerifyPostCommit == nil {
		return fmt.Errorf("publication down requires complete Plan validation, projection, and verification hooks")
	}
	return WithSessionLock(ctx, pool, func(conn *pgxpool.Conn) error {
		files, err := migrationFiles(dir)
		if err != nil {
			return err
		}
		if err := verifyExpectedUpMigrationDigests(dir, files, hooks.ExpectedUpSHA256); err != nil {
			return err
		}
		if err := hooks.Before(ctx, conn); err != nil {
			return fmt.Errorf("validate publication recovery context: %w", err)
		}
		path := filepath.Join(dir, hooks.Version+".down.sql")
		wire, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if !strings.Contains(string(wire), CapabilityContractPublicationMarker) {
			return fmt.Errorf("publication down %s lacks declaration marker", hooks.Version)
		}
		if fileDigest(wire) != hooks.ExpectedDownSHA256 {
			return fmt.Errorf("publication down %s does not match the approved artifact bytes", hooks.Version)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin guarded publication down: %w", err)
		}
		if err := hooks.RevertProjection(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("revert exact publication projection: %w", err)
		}
		if _, err := tx.Exec(ctx, migrationTransactionBody(string(wire))); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute %s: %w", path, err)
		}
		if err := hooks.VerifyInTx(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("verify guarded publication down: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit guarded publication down: %w", err)
		}
		if err := hooks.VerifyPostCommit(ctx, conn); err != nil {
			return fmt.Errorf("verify committed publication down: %w", err)
		}
		return nil
	})
}

func run(ctx context.Context, pool *pgxpool.Pool, dir string, options runOptions) error {
	conn, release, err := acquireSessionLock(ctx, pool)
	if err != nil {
		return err
	}
	defer release()

	// Ensure tracking table exists
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(64) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Collect applied versions
	applied := make(map[string]bool)
	var appliedVersions []string
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scan version: %w", err)
		}
		applied[v] = true
		appliedVersions = append(appliedVersions, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read applied migrations: %w", err)
	}
	rows.Close()

	// Read the exact artifact migration inventory.
	files, err := migrationFiles(dir)
	if err != nil {
		return err
	}
	artifactVersions := make([]string, 0, len(files))
	for _, file := range files {
		artifactVersions = append(artifactVersions, strings.TrimSuffix(file, ".up.sql"))
	}
	if options.requireExactPrefix {
		if err := validateExactLedgerPrefix(appliedVersions, artifactVersions, options.ceiling); err != nil {
			return err
		}
	}

	pending := make([]string, 0)
	publicationFiles := make(map[string]bool)
	expandFiles := make(map[string]bool)
	for _, fname := range files {
		version := strings.TrimSuffix(fname, ".up.sql")
		if applied[version] {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Join(dir, fname), err)
		}
		pending = append(pending, version)
		publicationFiles[version] = strings.Contains(string(body), CapabilityContractPublicationMarker)
		expandFiles[version] = strings.Contains(string(body), CompatibleExpandMarker)
	}
	if options.runtimeBridge {
		options.scopeSet = true
		for _, version := range pending {
			if expandFiles[version] {
				if version != schemaauthority.CapabilityExpandHead && version != schemaauthority.StreamCounterExpandHead {
					return fmt.Errorf("unknown runtime expand boundary %s", version)
				}
				break
			}
			options.allowedMigrations = append(options.allowedMigrations, version)
		}
	}
	if options.ceiling != "" {
		options.allowedMigrations = options.allowedMigrations[:0]
		for _, version := range pending {
			if version > options.ceiling {
				break
			}
			options.allowedMigrations = append(options.allowedMigrations, version)
		}
		if !applied[options.ceiling] && (len(options.allowedMigrations) == 0 || options.allowedMigrations[len(options.allowedMigrations)-1] != options.ceiling) {
			return fmt.Errorf("migration ceiling %q is not reachable from current ledger", options.ceiling)
		}
		if applied[options.ceiling] && len(options.allowedMigrations) == 0 {
			return nil
		}
	}
	allowed, err := validateRunScope(pending, publicationFiles, options)
	if err != nil {
		return err
	}
	for _, version := range allowed {
		if expandFiles[version] && (options.expand == nil || version != options.expand.Version) {
			return fmt.Errorf("pending compatible expansion %s requires explicit fleet preflight", version)
		}
	}
	if options.expand != nil {
		if len(allowed) != 1 || allowed[0] != options.expand.Version || !expandFiles[allowed[0]] {
			return fmt.Errorf("compatible expand requires the exact pending named transition")
		}
		wire, err := os.ReadFile(filepath.Join(dir, allowed[0]+".up.sql"))
		if err != nil {
			return err
		}
		if fileDigest(wire) != options.expand.ExpectedSHA256 {
			return fmt.Errorf("compatible expand artifact bytes changed")
		}
		if err := options.expand.Before(ctx, conn); err != nil {
			return fmt.Errorf("compatible expand fleet preflight: %w", err)
		}
	}
	var publicationVerifyWire []byte
	if options.publication != nil {
		if err := verifyExpectedUpMigrationDigests(dir, files, options.publication.ExpectedUpSHA256); err != nil {
			return err
		}
		verifyPath := filepath.Join(dir, options.publication.Version+".verify.sql")
		publicationVerifyWire, err = os.ReadFile(verifyPath)
		if err != nil {
			return fmt.Errorf("preflight committed publication verifier %s: %w", verifyPath, err)
		}
		if !strings.Contains(string(publicationVerifyWire), CapabilityContractPublicationMarker) {
			return fmt.Errorf("publication verifier %s lacks declaration marker", verifyPath)
		}
		if fileDigest(publicationVerifyWire) != options.publication.ExpectedVerifySHA256 {
			return fmt.Errorf("publication verifier %s does not match the approved artifact bytes", verifyPath)
		}
	}
	if options.publication != nil {
		if err := options.publication.Before(ctx, conn); err != nil {
			return fmt.Errorf("revalidate publication plan before apply: %w", err)
		}
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, version := range allowed {
		allowedSet[version] = struct{}{}
	}

	// Apply pending migrations in order
	for _, fname := range files {
		version := strings.TrimSuffix(fname, ".up.sql")
		if applied[version] {
			log.Printf("[migrate] %s already applied, skipping", version)
			continue
		}
		if _, ok := allowedSet[version]; !ok {
			break
		}

		path := filepath.Join(dir, fname)
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if options.expand != nil && fileDigest(sqlBytes) != options.expand.ExpectedSHA256 {
			return fmt.Errorf("compatible expand bytes changed after preflight")
		}
		if options.publication != nil {
			if want, exists := options.publication.ExpectedUpSHA256[version]; !exists || fileDigest(sqlBytes) != want {
				return fmt.Errorf("migration artifact bytes changed after Plan: %s", fname)
			}
		}

		sqlContent := migrationTransactionBody(string(sqlBytes))
		log.Printf("[migrate] applying %s ...", fname)

		start := time.Now()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin %s: %w", fname, err)
		}
		if _, err := tx.Exec(ctx, sqlContent); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute %s: %w", fname, err)
		}
		if options.publication != nil && version == options.publication.Version {
			if err := options.publication.ApplyProjection(ctx, tx); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("apply exact publication projection %s: %w", fname, err)
			}
			if err := options.publication.VerifyInTx(ctx, tx); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("verify publication final state %s: %w", fname, err)
			}
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", fname, err)
		}
		if options.expand != nil {
			if err := options.expand.After(ctx, tx); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("initialize verified bridge fleet %s: %w", fname, err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", fname, err)
		}

		log.Printf("[migrate] %s applied in %s", fname, time.Since(start).Round(time.Millisecond))
		if options.publication != nil && version == options.publication.Version {
			verifyPath := filepath.Join(dir, version+".verify.sql")
			if _, err := conn.Exec(ctx, string(publicationVerifyWire)); err != nil {
				return fmt.Errorf("execute committed publication verifier %s: %w", verifyPath, err)
			}
			if err := options.publication.VerifyPostCommit(ctx, conn); err != nil {
				return fmt.Errorf("verify committed publication %s: %w", fname, err)
			}
		}
	}

	return nil
}

func verifyExpectedUpMigrationDigests(dir string, files []string, expected map[string]string) error {
	if len(expected) != len(files) {
		return fmt.Errorf("approved migration digest inventory does not match artifact file count")
	}
	for _, file := range files {
		version := strings.TrimSuffix(file, ".up.sql")
		want, exists := expected[version]
		if !exists {
			return fmt.Errorf("approved migration digest inventory lacks %s", version)
		}
		wire, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return fmt.Errorf("read migration artifact %s: %w", file, err)
		}
		if fileDigest(wire) != want {
			return fmt.Errorf("migration artifact bytes changed after Plan: %s", file)
		}
	}
	return nil
}

func fileDigest(wire []byte) string {
	hash := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func validateExactLedgerPrefix(actual, artifact []string, ceiling string) error {
	ceilingIndex := -1
	for index, version := range artifact {
		if version == ceiling {
			ceilingIndex = index
			break
		}
	}
	if ceilingIndex < 0 {
		return fmt.Errorf("migration ceiling %q is not an exact artifact migration", ceiling)
	}
	allowed := artifact[:ceilingIndex+1]
	if len(actual) > len(allowed) || !slicesEqual(actual, allowed[:min(len(actual), len(allowed))]) {
		return fmt.Errorf("existing migration ledger is not the exact artifact prefix through %s: actual=%v", ceiling, actual)
	}
	return nil
}

func acquireSessionLock(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockID); err != nil {
		log.Printf("[migrate] advisory lock acquisition did not complete; closing connection")
		rawConn := conn.Hijack()
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rawConn.Close(closeCtx)
		return nil, nil, fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var unlocked bool
		unlockErr := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockID).Scan(&unlocked)
		cancel()
		if unlockErr == nil && unlocked {
			conn.Release()
			return
		}
		log.Printf("[migrate] release advisory lock: unlocked=%t err=%v; closing connection", unlocked, unlockErr)
		rawConn := conn.Hijack()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := rawConn.Close(closeCtx); err != nil {
			log.Printf("[migrate] close connection after advisory unlock failure: %v", err)
		}
	}
	return conn, release, nil
}

// WithAuthorityLock serializes the established schema owner and capability
// materializer without merging their ledgers. The lock is acquired before a
// caller opens its transaction, so a waiting writer gets a fresh snapshot.
func WithAuthorityLock(ctx context.Context, pool *pgxpool.Pool, fn func(*pgxpool.Conn) error) error {
	conn, release, err := acquireSessionLock(ctx, pool)
	if err != nil {
		return err
	}
	defer release()
	return fn(conn)
}

func migrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func validateRunScope(pending []string, publicationFiles map[string]bool, options runOptions) ([]string, error) {
	if options.publication == nil && len(options.allowedMigrations) == 0 && !options.scopeSet {
		for _, version := range pending {
			if publicationFiles[version] {
				return nil, fmt.Errorf("pending declaration-owned publication %s requires explicit publication or install context", version)
			}
		}
		return pending, nil
	}
	allowed := options.allowedMigrations
	if len(allowed) > len(pending) || !slicesEqual(allowed, pending[:len(allowed)]) {
		return nil, fmt.Errorf("allowed migration set is not the exact pending artifact prefix: allowed=%v pending=%v", allowed, pending)
	}
	for _, version := range allowed {
		isPublication := publicationFiles[version]
		if options.publication != nil && version == options.publication.Version {
			if !isPublication {
				return nil, fmt.Errorf("authorized publication %s lacks declaration marker", version)
			}
			continue
		}
		if isPublication {
			return nil, fmt.Errorf("independent publication %s requires its own exact Plan", version)
		}
	}
	return append([]string(nil), allowed...), nil
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// migrationTransactionBody removes only the first standalone BEGIN; and the
// final standalone COMMIT; when both are present. Run owns the transaction so
// each migration and its schema_migrations ledger row commit atomically. Any
// transaction-control text inside a stored procedure remains untouched.
func migrationTransactionBody(sqlContent string) string {
	lines := strings.Split(sqlContent, "\n")
	firstStatement, lastStatement := -1, -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if firstStatement == -1 {
			firstStatement = index
		}
		lastStatement = index
	}
	if firstStatement == -1 ||
		strings.ToUpper(strings.TrimSpace(lines[firstStatement])) != "BEGIN;" ||
		strings.ToUpper(strings.TrimSpace(lines[lastStatement])) != "COMMIT;" {
		return sqlContent
	}
	kept := make([]string, 0, len(lines)-2)
	for index, line := range lines {
		if index == firstStatement || index == lastStatement {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
