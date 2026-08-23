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

func TestObservationProviderCostReconciliationMigrationIntegration(t *testing.T) {
	t.Run("safe down and roll forward", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		down, err := os.ReadFile(filepath.Join(
			testutil.MigrationsDir(t), "000080_observation_provider_cost_reconciliation.down.sql",
		))
		require.NoError(t, err)
		execMigrationDown(t, ctx, pool, "000081_versioned_model_execution_profiles.down.sql")
		_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000084_future_contract')`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(down))
		require.ErrorContains(t, err, "later migrations must be reversed first")
		var currentLedger, futureLedger int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT
			  (SELECT COUNT(*) FROM schema_migrations
			    WHERE version='000080_observation_provider_cost_reconciliation'),
			  (SELECT COUNT(*) FROM schema_migrations
			    WHERE version='000084_future_contract')`,
		).Scan(&currentLedger, &futureLedger))
		require.Equal(t, 1, currentLedger)
		require.Equal(t, 1, futureLedger)
		_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000084_future_contract'`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(down))
		require.NoError(t, err)

		var columnPresent, triggerPresent, ledgerPresent bool
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT
			  EXISTS(SELECT 1 FROM information_schema.columns
			          WHERE table_schema='public' AND table_name='provider_cost_reconciliations'
			            AND column_name='terminal_observation_receipt_id'),
			  EXISTS(SELECT 1 FROM pg_trigger
			          WHERE tgname='provider_cost_reconciliation_completion_guard'),
			  EXISTS(SELECT 1 FROM schema_migrations
			          WHERE version='000080_observation_provider_cost_reconciliation')`,
		).Scan(&columnPresent, &triggerPresent, &ledgerPresent))
		require.False(t, columnPresent)
		require.False(t, triggerPresent)
		require.False(t, ledgerPresent)

		require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT
			  EXISTS(SELECT 1 FROM information_schema.columns
			          WHERE table_schema='public' AND table_name='provider_cost_reconciliations'
			            AND column_name='terminal_observation_receipt_id'),
			  EXISTS(SELECT 1 FROM pg_trigger
			          WHERE tgname='provider_cost_reconciliation_completion_guard'),
			  EXISTS(SELECT 1 FROM schema_migrations
			          WHERE version='000080_observation_provider_cost_reconciliation')`,
		).Scan(&columnPresent, &triggerPresent, &ledgerPresent))
		require.True(t, columnPresent)
		require.True(t, triggerPresent)
		require.True(t, ledgerPresent)
	})

	t.Run("completion rejects a receipt that drifts from frozen usage authority", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO freeze_idempotency (
			  request_id,org_id,status,pricing_version,billing_mode,
			  provider_cost_pending_at,settlement_pending_reason,
			  calculated_cost_micros,usage_authority,usage_purpose,usage_finality,
			  usage_contract_version,cache_usage_status)
			VALUES (
			  'op_m80_drift',980081,'settlement_pending','m80-price','cost_observation',
			  NOW(),'provider_response_unusable',69,'provider_reported','provider_evidence','final',
			  'unirouter.usage.v1','not_reported')`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_provider_attempts (
			  org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
			  route_sequence,route_total,endpoint,provider,channel_name,state,
			  outcome_certainty,calculated_cost_micros)
			VALUES (
			  980081,'op_m80_drift','att_m80_drift',77,88,99,
			  1,1,'/v1/chat/completions','test','test','output_withheld','known_usage',69)`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO provider_cost_reconciliations (
			  org_id,request_id,attempt_id,api_key_id,action,phase,actor_user_id,reason,evidence_ref)
			VALUES (
			  980081,'op_m80_drift','att_m80_drift',77,'refund','requested',7,
			  'reviewed','case://m80-drift')`)
		require.NoError(t, err)
		var receiptID int64
		require.NoError(t, tx.QueryRow(ctx, `
			INSERT INTO billing_observation_receipts (
			  org_id,request_id,terminal_status,calculated_cost_micros,
			  charged_cost_micros,pricing_version,usage_authority,usage_purpose,
			  usage_finality,usage_contract_version)
			VALUES (
			  980081,'op_m80_drift','cancelled',70,0,'m80-price',
			  'provider_reported','provider_evidence','final','unirouter.usage.v1')
			RETURNING id`,
		).Scan(&receiptID))
		_, err = tx.Exec(ctx, `
			UPDATE freeze_idempotency
			   SET status='cancelled',terminal_observation_receipt_id=$1,
			       terminal_at=NOW(),provider_cost_pending_at=NULL,settlement_pending_reason=''
			 WHERE org_id=980081 AND request_id='op_m80_drift'`, receiptID)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			UPDATE billing_provider_attempts
			   SET state='reconciled',financial_resolved_at=NOW()
			 WHERE org_id=980081 AND request_id='op_m80_drift'
			   AND attempt_id='att_m80_drift'`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO provider_cost_reconciliations (
			  org_id,request_id,attempt_id,api_key_id,action,phase,actor_user_id,
			  reason,evidence_ref,terminal_observation_receipt_id)
			VALUES (
			  980081,'op_m80_drift','att_m80_drift',77,'refund','completed',7,
			  'reviewed','case://m80-drift',$1)`, receiptID)
		require.NoError(t, err)
		err = tx.Commit(ctx)
		require.ErrorContains(t, err, "not an exact zero-charge cancellation")

		var durableRows int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT
			  (SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=980081)
			  + (SELECT COUNT(*) FROM billing_observation_receipts WHERE org_id=980081)
			  + (SELECT COUNT(*) FROM provider_cost_reconciliations WHERE org_id=980081)`,
		).Scan(&durableRows))
		require.Zero(t, durableRows)
	})

	t.Run("durable observation evidence refuses down atomically", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO freeze_idempotency (
			  request_id,org_id,status,pricing_version,billing_mode,
			  provider_cost_pending_at,settlement_pending_reason,
			  calculated_cost_micros,usage_authority,usage_purpose,usage_finality,
			  usage_contract_version,cache_usage_status)
			VALUES (
			  'op_m80_observation',980080,'settlement_pending','m80-price','cost_observation',
			  NOW(),'provider_response_unusable',69,'provider_reported','provider_evidence','final',
			  'unirouter.usage.v1','not_reported')`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_provider_attempts (
			  org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
			  route_sequence,route_total,endpoint,provider,channel_name,state,
			  outcome_certainty,calculated_cost_micros)
			VALUES (
			  980080,'op_m80_observation','att_m80_observation',77,88,99,
			  1,1,'/v1/chat/completions','test','test','output_withheld','known_usage',69)`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO provider_cost_reconciliations (
			  org_id,request_id,attempt_id,api_key_id,action,phase,actor_user_id,reason,evidence_ref)
			VALUES (
			  980080,'op_m80_observation','att_m80_observation',77,'refund','requested',7,
			  'reviewed','case://m80')`)
		require.NoError(t, err)
		var receiptID int64
		require.NoError(t, tx.QueryRow(ctx, `
			INSERT INTO billing_observation_receipts (
			  org_id,request_id,terminal_status,calculated_cost_micros,
			  charged_cost_micros,pricing_version,usage_authority,usage_purpose,
			  usage_finality,usage_contract_version)
			VALUES (
			  980080,'op_m80_observation','cancelled',69,0,'m80-price',
			  'provider_reported','provider_evidence','final','unirouter.usage.v1')
			RETURNING id`,
		).Scan(&receiptID))
		_, err = tx.Exec(ctx, `
			UPDATE freeze_idempotency
			   SET status='cancelled',terminal_observation_receipt_id=$1,
			       terminal_at=NOW(),provider_cost_pending_at=NULL,settlement_pending_reason=''
			 WHERE org_id=980080 AND request_id='op_m80_observation'`, receiptID)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			UPDATE billing_provider_attempts
			   SET state='reconciled',financial_resolved_at=NOW()
			 WHERE org_id=980080 AND request_id='op_m80_observation'
			   AND attempt_id='att_m80_observation'`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO provider_cost_reconciliations (
			  org_id,request_id,attempt_id,api_key_id,action,phase,actor_user_id,
			  reason,evidence_ref,terminal_observation_receipt_id)
			VALUES (
			  980080,'op_m80_observation','att_m80_observation',77,'refund','completed',7,
			  'reviewed','case://m80',$1)`, receiptID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))

		down, err := os.ReadFile(filepath.Join(
			testutil.MigrationsDir(t), "000080_observation_provider_cost_reconciliation.down.sql",
		))
		require.NoError(t, err)
		execMigrationDown(t, ctx, pool, "000081_versioned_model_execution_profiles.down.sql")
		_, err = pool.Exec(ctx, string(down))
		require.ErrorContains(t, err, "durable observation reconciliation evidence exists")

		var ledger, receiptLink, reconciliationRows int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT
			  (SELECT COUNT(*) FROM schema_migrations
			    WHERE version='000080_observation_provider_cost_reconciliation'),
			  (SELECT COUNT(*) FROM freeze_idempotency
			    WHERE org_id=980080 AND request_id='op_m80_observation'
			      AND terminal_observation_receipt_id=$1),
			  (SELECT COUNT(*) FROM provider_cost_reconciliations
			    WHERE org_id=980080 AND request_id='op_m80_observation')`, receiptID,
		).Scan(&ledger, &receiptLink, &reconciliationRows))
		require.Equal(t, 1, ledger)
		require.Equal(t, 1, receiptLink)
		require.Equal(t, 2, reconciliationRows)
	})
}
