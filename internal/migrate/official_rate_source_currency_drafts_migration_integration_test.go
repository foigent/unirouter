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

func TestOfficialRateSourceCurrencyDraftMigrationGuardsUSDReviewEvidence(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000085_model_execution_plan_freeze_fence.down.sql")
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name) VALUES ('m84-usd-source-test') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO enterprise_finance_rate_source_evidence (
  org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,
  payload_sha256,transform_version,currency,unit_contract,last_success_at,
  stale_after,imported_by
) VALUES (
  $1,'m84_usd_source','official_import','https://provider.example/pricing',
  'revision-m84',NOW(),'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'transform.m84','USD','tokens',NOW(),NOW()+INTERVAL '1 day','user:1'
)`, orgID)
	require.NoError(t, err)

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000084_official_rate_source_currency_drafts.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "non-CNY source evidence exists")

	var ledgerPresent, evidencePresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations WHERE version='000084_official_rate_source_currency_drafts'),
  EXISTS(SELECT 1 FROM enterprise_finance_rate_source_evidence
          WHERE org_id=$1 AND evidence_ref='m84_usd_source')`, orgID).Scan(&ledgerPresent, &evidencePresent))
	require.True(t, ledgerPresent)
	require.True(t, evidencePresent)

	_, err = pool.Exec(ctx, `DELETE FROM enterprise_finance_rate_source_evidence WHERE org_id=$1 AND evidence_ref='m84_usd_source'`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
INSERT INTO enterprise_finance_rate_source_evidence (
  org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,
  payload_sha256,transform_version,currency,unit_contract,last_success_at,
  stale_after,imported_by
) VALUES (
  $1,'m84_usd_after_down','official_import','https://provider.example/pricing',
  'revision-m84',NOW(),'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  'transform.m84','USD','tokens',NOW(),NOW()+INTERVAL '1 day','user:1'
)`, orgID)
	require.Error(t, err)
}
