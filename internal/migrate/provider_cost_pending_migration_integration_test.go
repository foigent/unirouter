//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F40ProviderCostPendingMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)

	assertUp := func() {
		t.Helper()
		var columns, indexes, ledger int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.columns
			 WHERE table_schema = 'public'
			   AND table_name = 'freeze_idempotency'
			   AND column_name IN (
			       'provider_cost_pending_at', 'settlement_pending_reason', 'topup_amount'
			   )`,
		).Scan(&columns))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_indexes
			 WHERE schemaname = 'public'
			   AND indexname IN (
			       'idx_freeze_idempotency_settlement_recovery',
			       'idx_freeze_idempotency_provider_cost_pending'
			   )`,
		).Scan(&indexes))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM schema_migrations
			 WHERE version = '000028_prd6_provider_cost_pending'`,
		).Scan(&ledger))
		require.Equal(t, 3, columns)
		require.Equal(t, 2, indexes)
		require.Equal(t, 1, ledger)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000028_prd6_provider_cost_pending.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "an unused provider-cost schema permits controlled rollback")

	var remainingColumns, ledger int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'freeze_idempotency'
		   AND column_name IN (
		       'provider_cost_pending_at', 'settlement_pending_reason', 'topup_amount'
		   )`,
	).Scan(&remainingColumns))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations
		 WHERE version = '000028_prd6_provider_cost_pending'`,
	).Scan(&ledger))
	require.Zero(t, remainingColumns)
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (
		    request_id, org_id, frozen_amount, reserved_balance_amount,
		    status, provider_cost_pending_at, settlement_pending_reason
		 ) VALUES (
		    'op_p6f40_down_guard', 9001, 1, 1,
		    'settlement_pending', NOW(), 'insufficient_topup'
		 )`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F40 down refused")
	assertUp()
}
