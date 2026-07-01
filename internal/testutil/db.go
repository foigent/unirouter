package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDB creates a connection to the real PostgreSQL for testing.
// Uses DB_DSN from environment or falls back to default.
func TestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://uniroute:uniroute@localhost:5432/uniroute?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// TestDBTx starts a transaction for test isolation.
// The transaction is automatically rolled back on test cleanup.
func TestDBTx(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
	})

	return tx
}

// SeedTestData inserts minimal test data needed for tests.
// Returns orgID and wallet info.
func SeedTestData(t *testing.T, pool *pgxpool.Pool) (orgID int64) {
	t.Helper()

	ctx := context.Background()

	// Ensure at least one org exists
	err := pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) 
		 VALUES ('test-org', 'active', 'standard') 
		 ON CONFLICT DO NOTHING 
		 RETURNING id`,
	).Scan(&orgID)
	if err != nil {
		// Org might already exist, try to get it
		err = pool.QueryRow(ctx, `SELECT id FROM orgs ORDER BY id LIMIT 1`).Scan(&orgID)
		if err != nil {
			t.Fatalf("ensure test org: %v", err)
		}
	}

	// Ensure wallet exists for this org
	_, err = pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		 VALUES ($1, 100000, 0, 0, 0)
		 ON CONFLICT (org_id) DO NOTHING`,
		orgID)
	if err != nil {
		t.Fatalf("ensure test wallet: %v", err)
	}

	return orgID
}

// CleanupTestData removes test data created during tests.
// Call this at the end of tests that don't use transaction rollback.
func CleanupTestData(t *testing.T, pool *pgxpool.Pool, orgID int64) {
	t.Helper()

	ctx := context.Background()
	// Delete in reverse dependency order
	pool.Exec(ctx, `DELETE FROM freeze_idempotency WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM transactions WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM call_logs WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM audit_logs WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM grants WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM wallets WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM api_keys WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM project_members WHERE project_id IN (SELECT id FROM projects WHERE org_id = $1)`, orgID)
	pool.Exec(ctx, `DELETE FROM users WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM projects WHERE org_id = $1`, orgID)
	pool.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, orgID)
}
