package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestDB_DSN returns the test database DSN from environment or default.
// Uses uniroute_test database to avoid polluting development data.
func TestDB_DSN() string {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://uniroute:uniroute@localhost:5432/uniroute_test?sslmode=disable"
	}
	return dsn
}

// SetupTestDB connects to uniroute_test, truncates all business tables,
// and returns a connection pool. The pool is closed on test cleanup.
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := TestDB_DSN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("cannot connect to test DB: %v (set TEST_DATABASE_URL or create uniroute_test)", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("cannot ping test DB: %v", err)
	}

	// Truncate all business tables in dependency order
	TruncateAll(t, pool)

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// TruncateAll truncates all business tables in dependency order.
// Skips schema_migrations and settings (base config).
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	tables := []string{
		"call_logs",
		"freeze_idempotency",
		"transactions",
		"audit_logs",
		"grants",
		"wallets",
		"api_keys",
		"channel_keys",
		"org_model_prices",
		"model_prices",
		"users",
		"orgs",
	}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			t.Logf("truncate warning for %s: %v", table, err)
		}
	}
}

// NewTestOrg creates a unique test org and returns its ID.
// Uses UUID-based name to avoid conflicts between parallel tests.
// IMPORTANT: Calls TruncateAll first to clean up leftover data from previous tests.
// This prevents FK violations when the previous test's TruncateAll deletes our org.
func NewTestOrg(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()

	// Clean up leftover data from previous tests to prevent FK violations
	TruncateAll(t, pool)

	name := fmt.Sprintf("test-org-%s-%d", t.Name(), time.Now().UnixNano())
	var orgID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ($1, 'active', 'standard') RETURNING id`,
		name,
	).Scan(&orgID)
	require.NoError(t, err, "create test org")

	return orgID
}

// NewTestWallet creates a wallet for the given org with the specified balance.
func NewTestWallet(t *testing.T, pool *pgxpool.Pool, orgID int64, balance int) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		 VALUES ($1, $2, 0, 0, 0)`,
		orgID, balance,
	)
	require.NoError(t, err, "create test wallet")
}

// NewTestGrant creates a grant for the given org.
func NewTestGrant(t *testing.T, pool *pgxpool.Pool, orgID int64, amount int, expiresAt *time.Time) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO grants (org_id, amount, balance, status, expires_at)
		 VALUES ($1, $2, $2, 'active', $3)`,
		orgID, amount, expiresAt,
	)
	require.NoError(t, err, "create test grant")
}

// NewTestModelPrice creates a model price entry.
func NewTestModelPrice(t *testing.T, pool *pgxpool.Pool, model string, inputPrice, outputPrice float64) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (model_name) DO UPDATE SET input_price_cents = $2, output_price_cents = $3`,
		model, inputPrice, outputPrice,
	)
	require.NoError(t, err, "create test model price")
}
