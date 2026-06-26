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

// Run applies all .up.sql migration files from dir that haven't been applied yet.
// It creates a schema_migrations table to track applied versions.
func Run(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	// Ensure tracking table exists
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(64) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Collect applied versions
	applied := make(map[string]bool)
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("scan version: %w", err)
		}
		applied[v] = true
	}

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

		sqlContent := string(sqlBytes)
		log.Printf("[migrate] applying %s ...", fname)

		start := time.Now()
		if _, err := pool.Exec(ctx, sqlContent); err != nil {
			return fmt.Errorf("execute %s: %w", fname, err)
		}

		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
		); err != nil {
			return fmt.Errorf("record %s: %w", fname, err)
		}

		log.Printf("[migrate] %s applied in %s", fname, time.Since(start).Round(time.Millisecond))
	}

	return nil
}
