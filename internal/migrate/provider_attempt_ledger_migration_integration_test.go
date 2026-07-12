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

func TestP6F40BProviderAttemptLedgerMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)

	assertUp := func() {
		t.Helper()
		var tables, columns, indexes, ledger int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = 'billing_provider_attempts'`,
		).Scan(&tables))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.columns
			 WHERE table_schema = 'public'
			   AND table_name = 'billing_provider_attempts'
			   AND column_name IN (
			       'attempt_id', 'api_key_id', 'channel_id', 'channel_key_id',
			       'state', 'outcome_certainty', 'retry_authorized',
			       'calculated_cost', 'charged_cost', 'terminal_transaction_id'
			   )`,
		).Scan(&columns))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_indexes
			 WHERE schemaname = 'public'
			   AND indexname IN (
			       'idx_billing_provider_attempts_request',
			       'idx_billing_provider_attempts_unresolved_key',
			       'idx_billing_provider_attempts_unresolved_channel'
			   )`,
		).Scan(&indexes))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM schema_migrations
			 WHERE version = '000029_prd6_provider_attempt_ledger'`,
		).Scan(&ledger))
		require.Equal(t, 1, tables)
		require.Equal(t, 10, columns)
		require.Equal(t, 3, indexes)
		require.Equal(t, 1, ledger)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000029_prd6_provider_attempt_ledger.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "an unused provider-attempt ledger permits controlled rollback")

	var tables, ledger int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = 'billing_provider_attempts'`,
	).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations
		 WHERE version = '000029_prd6_provider_attempt_ledger'`,
	).Scan(&ledger))
	require.Zero(t, tables)
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (
		    request_id, org_id, frozen_amount, reserved_balance_amount, status
		 ) VALUES ('op_p6f40b_down_guard', 9002, 1, 1, 'frozen')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO billing_provider_attempts (
		    org_id, request_id, attempt_id, api_key_id, channel_id, channel_key_id,
		    route_sequence, route_total, endpoint, provider, channel_name
		 ) VALUES (
		    9002, 'op_p6f40b_down_guard', 'att_p6f40b_down_guard',
		    1, 2, 3, 1, 1, '/v1/chat/completions', 'test', 'test-channel'
		 )`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F40B down refused")
	assertUp()
}
