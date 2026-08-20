//go:build integration

package customercontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnsureBaselineAdoptsSoleRootAndPreservesExistingStateIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID, orgAdminID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (
			org_id,username,password_hash,organization_role,role,status,auth_provider)
		VALUES ($1,'single-customer-admin','fixture','platform_admin','platform_admin','active','local')
		RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (
			org_id,username,password_hash,organization_role,role,status,auth_provider)
		VALUES ($1,'single-customer-org-admin','fixture','org_admin','org_admin','active','local')
		RETURNING id`, orgID).Scan(&orgAdminID))
	var channelID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status)
		VALUES ('legacy-active-model','kimi','https://provider.invalid','legacy-kimi-model','staged')
		RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO runtime_change_releases (
		  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
		  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
		  reason,created_by,validated_by,activated_by,validated_at,activated_at)
		VALUES (
		  'rcr_legacy_active_model','legacy-kimi-model','unirouter.runtime-change-candidate.v1',
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		  jsonb_build_object(
		    'contract_version','unirouter.runtime-change-candidate.v1',
		    'model_name','legacy-kimi-model',
		    'overrides',jsonb_build_array(jsonb_build_object(
		      'channel_id',$2::bigint,'expected_routing_epoch',1,'admission','allow'))),
		  '[]','{}','unchanged','active',3,
		  'legacy stable route',$1,$1,$1,NOW(),NOW())
		RETURNING id`, userID, channelID).Scan(&releaseID))
	_, err := pool.Exec(ctx, `
		INSERT INTO runtime_change_slots (
		  model_name,stable_release_id,canary_release_id,canary_basis_points,slot_version)
		VALUES ('legacy-kimi-model',$1,NULL,0,1)`, releaseID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO commercial_license_entitlements (
		singleton,license_id,deployment_id,key_id,expires_at,features,active_projects
	) VALUES (TRUE,'baseline-license','baseline-deployment','baseline-license-key',
		NOW()+INTERVAL '1 year','[]'::jsonb,1)`)
	require.NoError(t, err)

	type baselineResult struct {
		baseline Baseline
		err      error
	}
	results := make(chan baselineResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			baseline, err := EnsureBaseline(ctx, pool)
			results <- baselineResult{baseline: baseline, err: err}
		}()
	}
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	baseline := first.baseline
	require.Equal(t, baseline.ProjectID, second.baseline.ProjectID)
	require.Equal(t, StateReady, baseline.Customer.State)
	require.Equal(t, orgID, baseline.Customer.OrganizationID)
	require.Positive(t, baseline.ProjectID)

	var walletCount, platformMembershipCount, orgAdminMembershipCount, activeProjectCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE org_id=$1`, orgID).Scan(&walletCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM project_members
		WHERE project_id=$1 AND user_id=$2 AND role='project_admin'`, baseline.ProjectID, userID).Scan(&platformMembershipCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM project_members
		WHERE project_id=$1 AND user_id=$2 AND role='project_admin'`, baseline.ProjectID, orgAdminID).Scan(&orgAdminMembershipCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects WHERE status='active'`).Scan(&activeProjectCount))
	require.Equal(t, 1, walletCount)
	require.Zero(t, platformMembershipCount)
	require.Equal(t, 1, orgAdminMembershipCount)
	require.Equal(t, 1, activeProjectCount, "concurrent baselines must consume exactly one active-project License seat")
	var modelLifecycle, providerFamily string
	var approvedCatalog bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT lifecycle,provider_family,approved_catalog
		  FROM model_registry
		 WHERE org_id=$1 AND model_name='legacy-kimi-model'`, orgID).Scan(
		&modelLifecycle, &providerFamily, &approvedCatalog,
	))
	require.Equal(t, "ga", modelLifecycle)
	require.Equal(t, "kimi", providerFamily)
	require.True(t, approvedCatalog)

	_, err = pool.Exec(ctx, `UPDATE wallets SET balance_micros=123456 WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, baseline.ProjectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE model_registry
		   SET lifecycle='retired',approved_catalog=FALSE
		 WHERE org_id=$1 AND model_name='legacy-kimi-model'`, orgID)
	require.NoError(t, err)
	replayed, err := EnsureBaseline(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, baseline.ProjectID, replayed.ProjectID)

	var balance int64
	var projectStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balance))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM projects WHERE id=$1`, baseline.ProjectID).Scan(&projectStatus))
	require.EqualValues(t, 123456, balance)
	require.Equal(t, "disabled", projectStatus)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT lifecycle,approved_catalog
		  FROM model_registry
		 WHERE org_id=$1 AND model_name='legacy-kimi-model'`, orgID).Scan(&modelLifecycle, &approvedCatalog))
	require.Equal(t, "retired", modelLifecycle)
	require.False(t, approvedCatalog)
}
