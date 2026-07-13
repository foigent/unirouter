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

func TestP6F41BDeliveryReceiptMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000032_prd6_delivery_receipt.down.sql"))
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var tables, columns, constraints, triggers, ledger int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='request_delivery_receipts'`).Scan(&tables))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='request_idempotency' AND column_name IN ('usage_authority','usage_purpose','usage_finality','usage_contract_version','usage_estimate_version','delivery_mode','delivery_outcome','delivered_bytes','delivered_chunks','delivery_updated_at')`).Scan(&columns))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_constraint WHERE conname IN ('request_idempotency_usage_contract_check','request_idempotency_delivery_check','request_delivery_receipts_outcome_check')`).Scan(&constraints))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.triggers WHERE event_object_schema='public' AND event_object_table='request_delivery_receipts' AND trigger_name IN ('request_delivery_receipt_transition','request_delivery_receipt_append_only')`).Scan(&triggers))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000032_prd6_delivery_receipt'`).Scan(&ledger))
		require.Equal(t, 1, tables)
		require.Equal(t, 10, columns)
		require.Equal(t, 3, constraints)
		require.Equal(t, 3, triggers, "the multi-event append-only trigger is exposed once per UPDATE/DELETE event")
		require.Equal(t, 1, ledger)
	}

	assertUp()
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "an unused delivery-receipt schema permits controlled rollback")
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()

	_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency (request_id,org_id,frozen_amount,reserved_balance_amount,status) VALUES ('op_p6f41b_migration',9201,1,1,'frozen')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO request_idempotency (org_id,api_key_id,key_hash,request_fingerprint,operation_id,expires_at) VALUES (9201,1,repeat('a',64),repeat('b',64),'op_p6f41b_migration',NOW()+INTERVAL '1 hour')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO request_delivery_receipts (org_id,request_id,sequence,mode,outcome) VALUES (9201,'op_p6f41b_migration',1,'stream','write_started')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO request_delivery_receipts (org_id,request_id,sequence,mode,outcome,delivered_bytes,delivered_chunks) VALUES (9201,'op_p6f41b_migration',3,'stream','completed',10,1)`)
	require.Error(t, err, "delivery sequence gaps must be rejected")
	_, err = pool.Exec(ctx, `INSERT INTO request_delivery_receipts (org_id,request_id,sequence,mode,outcome,delivered_bytes,delivered_chunks) VALUES (9201,'op_p6f41b_migration',2,'stream','partial',10,1)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO request_delivery_receipts (org_id,request_id,sequence,mode,outcome,delivered_bytes,delivered_chunks) VALUES (9201,'op_p6f41b_migration',3,'stream','completed',10,1)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE request_delivery_receipts SET delivered_bytes=11 WHERE org_id=9201 AND request_id='op_p6f41b_migration' AND sequence=3`)
	require.ErrorContains(t, err, "append-only")
	_, err = pool.Exec(ctx, `DELETE FROM request_delivery_receipts WHERE org_id=9201 AND request_id='op_p6f41b_migration'`)
	require.ErrorContains(t, err, "append-only")
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F41B rollback refused")
	assertUp()
}
