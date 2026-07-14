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

func TestP6F18BStreamSettlementRecoveryMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeP6F18B(t, pool, migrationsDir)
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))

	assertUp := func() {
		t.Helper()
		var columns, indexes int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM information_schema.columns
			 WHERE table_schema = 'public'
			   AND table_name = 'freeze_idempotency'
			   AND column_name IN (
			       'output_started_at', 'settlement_payload_ready_at',
			       'settlement_attempts', 'settlement_claimed_until',
			       'settlement_next_attempt_at', 'settlement_last_error'
			   )`,
		).Scan(&columns))
		require.Equal(t, 6, columns)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_indexes
			 WHERE schemaname = 'public'
			   AND indexname = 'idx_freeze_idempotency_settlement_recovery'`,
		).Scan(&indexes))
		require.Equal(t, 1, indexes)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000023_prd6_stream_settlement_recovery.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var columns, ledger int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'freeze_idempotency'
		   AND column_name = 'output_started_at'`,
	).Scan(&columns))
	require.Zero(t, columns)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations
		 WHERE version = '000023_prd6_stream_settlement_recovery'`,
	).Scan(&ledger))
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (
		    request_id, org_id, frozen_amount_micros, reserved_balance_micros,
		    status, output_started_at
		 ) VALUES ('op_p6f18b_down_guard', 9001, 10000, 10000, 'settlement_pending', NOW())`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F18B down refused")
	assertUp()
}

func runMigrationsBeforeP6F18B(t *testing.T, pool *pgxpool.Pool, migrationsDir string) {
	t.Helper()
	preP6F18BDir := t.TempDir()
	entries, err := os.ReadDir(migrationsDir)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") || name >= "000023_" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(migrationsDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(preP6F18BDir, name), body, 0o600))
	}
	require.NoError(t, migrate.Run(context.Background(), pool, preP6F18BDir))
}
