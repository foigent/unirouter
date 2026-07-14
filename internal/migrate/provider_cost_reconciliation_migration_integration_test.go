//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
)

func TestP6F40CProviderCostReconciliationMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000032_prd6_delivery_receipt.up.sql")
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000030_prd6_provider_cost_reconciliation.down.sql"))
	require.NoError(t, err)
	usageAuthorityDownSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000031_prd6_usage_authority.down.sql"))
	require.NoError(t, err)
	deliveryReceiptDownSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000032_prd6_delivery_receipt.down.sql"))
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var tables, indexes, ledger int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='provider_cost_reconciliations'`).Scan(&tables))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_indexes WHERE schemaname='public' AND indexname IN ('idx_provider_cost_reconciliations_one_request','idx_provider_cost_reconciliations_org_created')`).Scan(&indexes))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000030_prd6_provider_cost_reconciliation'`).Scan(&ledger))
		require.Equal(t, 1, tables)
		require.Equal(t, 2, indexes)
		require.Equal(t, 1, ledger)
	}
	assertUp()
	_, err = pool.Exec(ctx, string(deliveryReceiptDownSQL))
	require.NoError(t, err, "latest successor delivery-receipt schema must be removed first")
	_, err = pool.Exec(ctx, string(usageAuthorityDownSQL))
	require.NoError(t, err, "successor usage-authority schema must be removed first")
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()

	_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency (request_id,org_id,frozen_amount,reserved_balance_amount,status) VALUES ('op_p6f40c_down',9901,1,1,'settlement_pending')`)
	require.Error(t, err, "pending reservation without a financial marker must remain rejected")
	_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency (request_id,org_id,frozen_amount,reserved_balance_amount,status,provider_cost_pending_at,settlement_pending_reason) VALUES ('op_p6f40c_down',9901,1,1,'settlement_pending',NOW(),'ambiguous_attempt')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts (org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty) VALUES (9901,'op_p6f40c_down','att_p6f40c_down',1,2,3,1,1,'/v1/chat/completions','test','test','outcome_unknown','unknown')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO provider_cost_reconciliations (org_id,request_id,attempt_id,api_key_id,action,phase,actor_user_id,reason,evidence_ref) VALUES (9901,'op_p6f40c_down','att_p6f40c_down',1,'refund','requested',7,'verified','case://down')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F40C down refused")
	assertUp()
}
