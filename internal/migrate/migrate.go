package migrate

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationAdvisoryLockID serializes schema discovery and application across
// every UniRouter replica sharing a database. It is session-scoped so the lock
// covers the complete migration list, including the gaps between individual
// transactions.
const migrationAdvisoryLockID int64 = 86420517831702491

// Run applies all .up.sql migration files from dir that haven't been applied yet.
// It creates a schema_migrations table to track applied versions.
func Run(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	lockAttempted := false
	lockAcquired := false
	defer func() {
		if !lockAttempted {
			conn.Release()
			return
		}

		if lockAcquired {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var unlocked bool
			unlockErr := conn.QueryRow(unlockCtx,
				`SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockID,
			).Scan(&unlocked)
			cancel()
			if unlockErr == nil && unlocked {
				conn.Release()
				return
			}
			log.Printf("[migrate] release advisory lock: unlocked=%t err=%v; closing connection", unlocked, unlockErr)
		} else {
			// When lock acquisition returns an error, the server-side outcome can
			// be unknown (for example after a network failure). Never put that
			// physical session back into the pool where a lock could be retained.
			log.Printf("[migrate] advisory lock acquisition did not complete; closing connection")
		}

		// A session-scoped lock must never return to the pool if unlock cannot be
		// confirmed. Hijack and close that physical connection; PostgreSQL then
		// releases every session lock even when the caller's context was canceled.
		rawConn := conn.Hijack()
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rawConn.Close(closeCtx); err != nil {
			log.Printf("[migrate] close connection after advisory unlock failure: %v", err)
		}
	}()

	lockAttempted = true
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	lockAcquired = true

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
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read applied migrations: %w", err)
	}
	rows.Close()

	// Read .up.sql files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// Apply pending migrations in order
	for _, fname := range files {
		version := strings.TrimSuffix(fname, ".up.sql")
		if applied[version] {
			log.Printf("[migrate] %s already applied, skipping", version)
			continue
		}

		path := filepath.Join(dir, fname)
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
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

		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", fname, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", fname, err)
		}

		log.Printf("[migrate] %s applied in %s", fname, time.Since(start).Round(time.Millisecond))
	}

	return nil
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
