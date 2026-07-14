package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F18BillingReservationMigrationUpDownAndSafeRollForward(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeP6F18(t, pool, migrationsDir)

	historicalOrgID := int64(2001)
	_, err := pool.Exec(ctx,
		`INSERT INTO orgs (id, name, status, org_type)
		 VALUES ($1, 'p6f18-historical', 'active', 'standard')`, historicalOrgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (request_id, org_id, frozen_amount, status)
		 VALUES ('op_p6f18_historical', $1, 25, 'frozen')`, historicalOrgID,
	)
	require.NoError(t, err)
	runP6F18Migration(t, pool, migrationsDir)

	var historicalCash, historicalGrant int
	var historicalVersion string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT reserved_balance_amount, reserved_granted_amount, pricing_version
		 FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = 'op_p6f18_historical'`, historicalOrgID,
	).Scan(&historicalCash, &historicalGrant, &historicalVersion))
	require.Equal(t, 25, historicalCash)
	require.Zero(t, historicalGrant)
	require.Equal(t, "legacy", historicalVersion)

	assertUp := func() {
		t.Helper()
		var columns, tables, indexes int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.columns
			 WHERE table_schema = 'public'
			   AND table_name = 'freeze_idempotency'
			   AND column_name IN (
			       'reserved_balance_amount', 'reserved_granted_amount',
			       'effective_input_price_cents', 'effective_output_price_cents',
			       'price_multiplier', 'pricing_version', 'terminal_transaction_id'
			   )`,
		).Scan(&columns))
		require.Equal(t, 7, columns)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.tables
			 WHERE table_schema = 'public'
			   AND table_name = 'billing_reservation_grant_allocations'`,
		).Scan(&tables))
		require.Equal(t, 1, tables)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_indexes
			 WHERE schemaname = 'public'
			   AND indexname = 'idx_transactions_one_billing_terminal'`,
		).Scan(&indexes))
		require.Equal(t, 1, indexes)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000022_prd6_billing_reservation_invariants.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var columns, tables, ledger int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'freeze_idempotency'
		   AND column_name IN ('reserved_balance_amount', 'pricing_version', 'terminal_transaction_id')`,
	).Scan(&columns))
	require.Zero(t, columns)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = 'public'
		   AND table_name = 'billing_reservation_grant_allocations'`,
	).Scan(&tables))
	require.Zero(t, tables)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations
		 WHERE version = '000022_prd6_billing_reservation_invariants'`,
	).Scan(&ledger))
	require.Zero(t, ledger)

	runP6F18Migration(t, pool, migrationsDir)
	assertUp()

	orgID := int64(2003)
	_, err = pool.Exec(ctx,
		`INSERT INTO orgs (id, name, status, org_type)
		 VALUES ($1, 'p6f18-down-guard', 'active', 'standard')`, orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (
			request_id, org_id, frozen_amount, reserved_balance_amount,
			status, pricing_version
		 ) VALUES ('op_p6f18_down_guard', $1, 1, 1, 'frozen', 'pv1_guard')`, orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F18A down refused")
	assertUp()
}

func TestP6F18MigrationFailsClosedOnDuplicateTerminalHistory(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeP6F18(t, pool, migrationsDir)

	orgID := int64(2002)
	_, err := pool.Exec(ctx,
		`INSERT INTO orgs (id, name, status, org_type)
		 VALUES ($1, 'p6f18-duplicate-terminal', 'active', 'standard')`, orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (request_id, org_id, frozen_amount, status)
		 VALUES ('op_p6f18_duplicate_terminal', $1, 10, 'settled')`, orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount)
		 VALUES ('op_p6f18_duplicate_terminal', $1, 'settle', 'completed', 5),
		        ('op_p6f18_duplicate_terminal', $1, 'cancel', 'completed', 5)`, orgID,
	)
	require.NoError(t, err)

	err = runP6F18MigrationError(t, pool, migrationsDir)
	require.ErrorContains(t, err, "duplicate terminal billing transactions exist")
	var ledgerCount, newColumnCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations
		 WHERE version = '000022_prd6_billing_reservation_invariants'`,
	).Scan(&ledgerCount))
	require.Zero(t, ledgerCount)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'freeze_idempotency'
		   AND column_name = 'pricing_version'`,
	).Scan(&newColumnCount))
	require.Zero(t, newColumnCount, "failed migration transaction must not leave a partial schema")
}

func runMigrationsBeforeP6F18(t *testing.T, pool *pgxpool.Pool, migrationsDir string) {
	t.Helper()
	preP6F18Dir := t.TempDir()
	entries, err := os.ReadDir(migrationsDir)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") || name >= "000022_" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(migrationsDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(preP6F18Dir, name), body, 0o600))
	}
	require.NoError(t, migrate.Run(context.Background(), pool, preP6F18Dir))
}

func runP6F18Migration(t *testing.T, pool *pgxpool.Pool, migrationsDir string) {
	t.Helper()
	require.NoError(t, runP6F18MigrationError(t, pool, migrationsDir))
}

func runP6F18MigrationError(t *testing.T, pool *pgxpool.Pool, migrationsDir string) error {
	t.Helper()
	p6F18Dir := t.TempDir()
	const migration = "000022_prd6_billing_reservation_invariants.up.sql"
	body, err := os.ReadFile(filepath.Join(migrationsDir, migration))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p6F18Dir, migration), body, 0o600))
	return migrate.Run(context.Background(), pool, p6F18Dir)
}
