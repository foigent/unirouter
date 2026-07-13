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

func TestP6F41AUsageAuthorityMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000031_prd6_usage_authority.down.sql"))
	require.NoError(t, err)
	deliveryReceiptDownSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000032_prd6_delivery_receipt.down.sql"))
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var columns, constraints, ledger int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('freeze_idempotency','call_logs','usage_finalization_journal') AND column_name IN ('usage_authority','usage_purpose','usage_finality','usage_contract_version','usage_estimate_version')`).Scan(&columns))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_constraint WHERE conname IN ('freeze_idempotency_usage_contract_check','call_logs_usage_contract_check','usage_finalization_contract_check')`).Scan(&constraints))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000031_prd6_usage_authority'`).Scan(&ledger))
		require.Equal(t, 15, columns)
		require.Equal(t, 3, constraints)
		require.Equal(t, 1, ledger)
	}
	assertUp()

	_, err = pool.Exec(ctx, `INSERT INTO call_logs (request_id,org_id,usage_source,usage_authority,usage_purpose,usage_finality,usage_contract_version) VALUES ('op_p6f41_explicit_zero',9101,'upstream','provider_reported','customer_charge','final','unirouter.usage.v1')`)
	require.NoError(t, err, "provider-reported explicit zero must be valid evidence")
	_, err = pool.Exec(ctx, `INSERT INTO call_logs (request_id,org_id,usage_source,usage_authority,usage_purpose,usage_finality,usage_contract_version) VALUES ('op_p6f41_invalid',9101,'upstream','provider_reported','customer_charge','estimated','unirouter.usage.v1')`)
	require.Error(t, err, "provider-reported usage cannot be relabelled estimated")
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F41A rollback refused")
	assertUp()

	_, err = pool.Exec(ctx, `DELETE FROM call_logs WHERE org_id=9101`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(deliveryReceiptDownSQL))
	require.NoError(t, err, "successor delivery-receipt schema must be removed first")
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
}
