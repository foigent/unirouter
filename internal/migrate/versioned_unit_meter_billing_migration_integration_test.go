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

func TestVersionedUnitMeterBillingMigrationIntegration(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000085_model_execution_plan_freeze_fence.down.sql")
	execMigrationDown(t, ctx, pool, "000084_official_rate_source_currency_drafts.down.sql")
	var err error

	var columnCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM information_schema.columns
 WHERE table_schema='public'
   AND (
        (table_name='freeze_idempotency' AND column_name IN
          ('effective_unit_rates','reserved_meter_usage','meter_usage','non_token_cost_micros'))
        OR
        (table_name IN ('billing_observation_receipts','usage_finalization_journal','call_logs')
          AND column_name IN ('effective_unit_rates','meter_usage','non_token_cost_micros'))
   )`).Scan(&columnCount))
	require.Equal(t, 13, columnCount)

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name) VALUES ('m83-unit-meter-test') RETURNING id`).Scan(&orgID))
	_, err = pool.Exec(ctx, `
INSERT INTO enterprise_finance_rate_source_evidence (
  org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,
  payload_sha256,transform_version,currency,unit_contract,last_success_at,
  stale_after,imported_by
) VALUES (
  $1,'m83_components_source','official_import','https://provider.example/pricing',
  'revision-m83',NOW(),'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'transform.m83','CNY','versioned_rate_card_components.v1',NOW(),NOW()+INTERVAL '1 day','user:1'
)`, orgID)
	require.NoError(t, err)

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000083_versioned_unit_meter_billing.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "versioned component source evidence exists")
	_, err = pool.Exec(ctx, `DELETE FROM enterprise_finance_rate_source_evidence WHERE org_id=$1 AND evidence_ref='m83_components_source'`, orgID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
INSERT INTO freeze_idempotency (
  request_id,org_id,status,frozen_amount_micros,reserved_balance_micros,
  reserved_granted_micros,pricing_version,effective_unit_rates,reserved_meter_usage
) VALUES (
  'op_m83_meter_evidence',1,'frozen',0,0,0,'rcq3_test',
  '[{"meter":"builtin_web_search_calls","unit_quantity":1,"amount_minor":30000,"currency":"CNY","amount_scale":1000000,"component_hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]'::jsonb,
  '[{"meter":"builtin_web_search_calls","quantity":1}]'::jsonb
	)`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable meter evidence exists")

	var ledgerPresent, columnsPresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations WHERE version='000083_versioned_unit_meter_billing'),
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='freeze_idempotency'
            AND column_name='effective_unit_rates')`).Scan(&ledgerPresent, &columnsPresent))
	require.True(t, ledgerPresent)
	require.True(t, columnsPresent)

	_, err = pool.Exec(ctx, `DELETE FROM freeze_idempotency WHERE request_id='op_m83_meter_evidence' AND org_id=1`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)

	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.PreRetirementMigrationsDir(t), "000083_versioned_unit_meter_billing"))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations WHERE version='000083_versioned_unit_meter_billing'),
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='freeze_idempotency'
            AND column_name='effective_unit_rates')`).Scan(&ledgerPresent, &columnsPresent))
	require.True(t, ledgerPresent)
	require.True(t, columnsPresent)
}
