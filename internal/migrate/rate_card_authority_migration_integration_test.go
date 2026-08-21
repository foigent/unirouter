//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRateCardAuthorityMigrationAddsEvidenceContractWithoutAutoBackfill(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var tableCount, scheduleColumnCount, legacyDrafts, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name='enterprise_finance_rate_source_evidence'`).Scan(&tableCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='enterprise_finance_rate_card_versions'
		  AND column_name='daily_price_bands' AND data_type='jsonb'`).Scan(&scheduleColumnCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM enterprise_finance_rate_card_versions
		WHERE source_evidence_ref LIKE 'legacy_model_prices_%'`).Scan(&legacyDrafts))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000064_rate_card_request_pricing_authority'`).Scan(&ledger))
	require.Equal(t, 1, tableCount)
	require.Equal(t, 1, scheduleColumnCount)
	require.Zero(t, legacyDrafts, "migration must not invent maker-checker approval or activate legacy rows")
	require.Equal(t, 1, ledger)
}

func TestRateCardAuthorityMigrationRollbackRefusesDailySchedules(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('rate-schedule-rollback','active','customer') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
		INSERT INTO enterprise_finance_rate_card_versions (
		  org_id,rate_card_ref,version,provider_ref,model_ref,layer,state,effective_from,
		  daily_price_bands,created_by
	) VALUES ($1,'scheduled-rate',1,'provider-a','model-a','supplier_contracted','draft',NOW(),
	  '[{"band_ref":"whole-day","start_minute_utc":0,"end_minute_utc":1440,"components":[]}]'::jsonb,'user:1')`, orgID)
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	execMigrationDown(t, ctx, pool, "000069_model_deployment_expand.down.sql")
	execMigrationDown(t, ctx, pool, "000068_provider_reported_model_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000067_call_log_route_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000066_runtime_assignment_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000065_model_endpoint_projection.down.sql")
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000064_rate_card_request_pricing_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "daily price schedules exist")

	var columnStillPresent bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM information_schema.columns
		  WHERE table_schema='public' AND table_name='enterprise_finance_rate_card_versions'
		    AND column_name='daily_price_bands'
		)`).Scan(&columnStillPresent))
	require.True(t, columnStillPresent)
}

func TestRateCardAuthorityMigrationRollbackRefusesSourceEvidence(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('rate-source-rollback','active','customer') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
		INSERT INTO enterprise_finance_rate_source_evidence (
		  org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,payload_sha256,
		  transform_version,currency,unit_contract,last_success_at,stale_after,imported_by
		) VALUES ($1,'source-1','official_import','https://example.invalid/pricing','r1',NOW(),
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
	  'transform.v1','CNY','tokens',NOW(),NOW()+INTERVAL '1 day','user:1')`, orgID)
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	execMigrationDown(t, ctx, pool, "000069_model_deployment_expand.down.sql")
	execMigrationDown(t, ctx, pool, "000068_provider_reported_model_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000067_call_log_route_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000066_runtime_assignment_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000065_model_endpoint_projection.down.sql")
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000064_rate_card_request_pricing_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "source evidence exists")
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM enterprise_finance_rate_source_evidence WHERE org_id=$1`, orgID).Scan(&rows))
	require.Equal(t, 1, rows)
}
