package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/money"
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
		testDatabaseUnavailable(t, "cannot connect to test DB: %v (set TEST_DATABASE_URL or create uniroute_test)", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		testDatabaseUnavailable(t, "cannot ping test DB: %v", err)
	}

	// Truncate all business tables in dependency order
	TruncateAll(t, pool)

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// SetupIsolatedTestDB creates a temporary PostgreSQL database for integration
// tests that must run migrations from a clean schema.
func SetupIsolatedTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	baseDSN := TestDB_DSN()
	adminDSN, tempDSN, dbName, err := tempDatabaseDSNs(baseDSN)
	require.NoError(t, err, "build temporary database DSN")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		testDatabaseUnavailable(t, "cannot connect to PostgreSQL admin database: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot ping PostgreSQL admin database: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+quoteIdent(dbName)); err != nil {
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot create temporary test database %s: %v", dbName, err)
	}

	pool, err := pgxpool.New(ctx, tempDSN)
	if err != nil {
		_ = dropTemporaryDatabase(ctx, adminPool, dbName)
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot connect to temporary test database %s: %v", dbName, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_ = dropTemporaryDatabase(ctx, adminPool, dbName)
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot ping temporary test database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		pool.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if err := dropTemporaryDatabase(dropCtx, adminPool, dbName); err != nil {
			t.Errorf("drop temporary test database %s: %v", dbName, err)
		}
		adminPool.Close()
	})

	return pool
}

// SetupMigratedTestDB creates a clean temporary database and applies all repo migrations.
func SetupMigratedTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, migrate.Run(ctx, pool, MigrationsDir(t)), "run migrations in isolated test database")
	TruncateAll(t, pool)
	return pool
}

// MigrationsDir returns the repository migrations directory from the testutil package location.
func MigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolve testutil path")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
}

func tempDatabaseDSNs(baseDSN string) (adminDSN, tempDSN, dbName string, err error) {
	parsed, err := url.Parse(baseDSN)
	if err != nil {
		return "", "", "", err
	}
	dbName = fmt.Sprintf("uniroute_it_%d_%d", os.Getpid(), time.Now().UnixNano())
	admin := *parsed
	admin.Path = "/postgres"
	temp := *parsed
	temp.Path = "/" + dbName
	return admin.String(), temp.String(), dbName, nil
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func dropTemporaryDatabase(ctx context.Context, adminPool *pgxpool.Pool, dbName string) error {
	// The test pool is closed before this runs, so a regular DROP both avoids
	// requiring pg_signal_backend and exposes real connection leaks instead of
	// terminating unrelated sessions from a privileged CI role.
	_, err := adminPool.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdent(dbName))
	return err
}

func testDatabaseUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	required := strings.TrimSpace(os.Getenv("REQUIRE_TEST_DATABASE"))
	if required == "1" || strings.EqualFold(required, "true") {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// TruncateAll truncates all business tables in dependency order.
// Skips schema_migrations and settings (base config).
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	// Security-control state is reset explicitly for isolated tests. Keep the
	// release_state singleton itself because runtime code relies on its presence.
	for _, statement := range []string{
		"DELETE FROM release_history",
		"DELETE FROM commercial_license_entitlements",
		"UPDATE release_state SET release_sequence = 0, version = '', commit = '', package_sha256 = NULL, trusted_key_id = '', target_schema = '', manifest = NULL, updated_at = NOW() WHERE singleton = TRUE",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Logf("release-control reset warning for %q: %v", statement, err)
		}
	}

	tables := []string{
		"guardrail_policies",
		"distributed_limit_policies",
		"provider_cost_reconciliations",
		"billing_provider_attempts",
		"internal_outbox_events",
		"usage_finalization_journal",
		"completed_execution_feed_items",
		"completed_execution_feed_states",
		"project_budget_usage",
		"runtime_terminal_hooks",
		"notification_deliveries",
		"notification_webhooks",
		"notification_events",
		"request_idempotency",
		"call_logs",
		"freeze_idempotency",
		"transactions",
		"audit_logs",
		"grants",
		"wallets",
		"api_keys",
		"project_model_sets",
		"model_set_items",
		"model_aliases",
		"model_sets",
		"model_registry",
		"project_members",
		"channel_keys",
		"org_model_prices",
		"model_prices",
		"users",
		"projects",
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

// NewTestWalletCents creates a wallet from an explicitly cents-denominated test fixture.
func NewTestWalletCents(t *testing.T, pool *pgxpool.Pool, orgID int64, balanceCents int64) {
	t.Helper()
	ctx := context.Background()
	balanceMicros, err := money.CentsToMicros(balanceCents)
	require.NoError(t, err, "convert test wallet cents")

	_, err = pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, $2, 0, 0, 0)`,
		orgID, balanceMicros,
	)
	require.NoError(t, err, "create test wallet")
}

// NewTestGrantCents creates a grant from an explicitly cents-denominated test fixture.
func NewTestGrantCents(t *testing.T, pool *pgxpool.Pool, orgID int64, amountCents int64, expiresAt *time.Time) {
	t.Helper()
	ctx := context.Background()
	amountMicros, err := money.CentsToMicros(amountCents)
	require.NoError(t, err, "convert test grant cents")

	_, err = pool.Exec(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, status, expires_at)
		 VALUES ($1, $2, $2, 'active', $3)`,
		orgID, amountMicros, expiresAt,
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
