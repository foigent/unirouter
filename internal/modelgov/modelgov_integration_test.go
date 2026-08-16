//go:build integration

package modelgov

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestFilterCallableModels_MatchesCheckAccessIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)

	var projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status)
		 VALUES ($1, 'Pilot Project', 'pilot', 'active')
		 RETURNING id`, orgID,
	).Scan(&projectID))

	_, err := pool.Exec(ctx,
		`INSERT INTO model_registry
		 (org_id, model_name, display_name, lifecycle, approved_catalog)
		 VALUES
		 ($1, 'pilot-direct', 'Pilot Direct', 'ga', FALSE),
		 ($1, 'pilot-alias-name-target', 'Alias Name Target', 'ga', FALSE),
		 ($1, 'pilot-target-item', 'Target Item', 'ga', FALSE),
		 ($1, 'pilot-retired', 'Retired', 'retired', TRUE),
		 ($1, 'pilot-denied', 'Denied', 'ga', TRUE)`,
		orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, status)
		 VALUES
		 ($1, 'pilot-alias-by-name', 'pilot-alias-name-target', 'active'),
		 ($1, 'pilot-alias-by-target', 'pilot-target-item', 'active')`,
		orgID,
	)
	require.NoError(t, err)

	var modelSetID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, status)
		 VALUES ($1, 'Pilot Models', 'pilot-models', 'active')
		 RETURNING id`, orgID,
	).Scan(&modelSetID))
	_, err = pool.Exec(ctx,
		`INSERT INTO model_set_items (model_set_id, item_type, item_name)
		 VALUES
		 ($1, 'model', 'pilot-direct'),
		 ($1, 'alias', 'pilot-alias-by-name'),
		 ($1, 'model', 'pilot-target-item')`,
		modelSetID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO project_model_sets (project_id, model_set_id) VALUES ($1, $2)`,
		projectID, modelSetID,
	)
	require.NoError(t, err)

	candidates := []string{
		"pilot-direct",
		"pilot-alias-by-name",
		"pilot-alias-name-target",
		"pilot-alias-by-target",
		"pilot-target-item",
		"pilot-retired",
		"pilot-denied",
		"pilot-unregistered",
	}
	for _, tc := range []struct {
		name      string
		projectID int64
	}{
		{name: "without project still excludes retired", projectID: 0},
		{name: "project model set intersection", projectID: projectID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := callableModelsByCheckAccess(t, ctx, pool, orgID, tc.projectID, candidates)
			actual, err := FilterCallableModels(ctx, pool, orgID, tc.projectID, candidates)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		})
	}
}

func TestEnsureActivatedCatalogModelRejectsEquivalentLiveIdentity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)

	_, err := pool.Exec(ctx, `
		INSERT INTO model_registry
			(org_id, model_name, display_name, lifecycle, approved_catalog)
		VALUES ($1, 'DeepSeek-V4-Flash', 'DeepSeek V4 Flash', 'ga', TRUE)`, orgID)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	err = EnsureActivatedCatalogModel(ctx, tx, orgID, "deepseek-v4-flash", "deepseek")
	require.ErrorIs(t, err, ErrModelIdentityConflict)
	require.Contains(t, err.Error(), "DeepSeek-V4-Flash")
	require.NoError(t, tx.Rollback(ctx))

	var exactCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM model_registry WHERE org_id=$1 AND model_name='deepseek-v4-flash'`, orgID,
	).Scan(&exactCount))
	require.Zero(t, exactCount)
}

func TestReconcileActivatedCatalogModelsUsesCanonicalRuntimeAssignments(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)

	var userID, channelID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
		VALUES ($1,'runtime-catalog-owner','test-hash','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
		VALUES ('runtime-catalog-provider','custom-maas','https://provider.invalid','runtime-catalog-model','staged',4)
		RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO runtime_change_releases (
		  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
		  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
		  reason,created_by,validated_by,activated_by,validated_at,activated_at)
		VALUES (
		  'rcr_runtime_catalog','runtime-catalog-model','unirouter.runtime-change-candidate.v1',
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		  jsonb_build_object(
		    'contract_version','unirouter.runtime-change-candidate.v1',
		    'model_name','runtime-catalog-model',
		    'overrides',jsonb_build_array(jsonb_build_object(
		      'channel_id',$2::bigint,'expected_routing_epoch',4,'admission','allow'))),
		  '[]','{}','unchanged','active',1,'runtime catalog fixture',$1,$1,$1,NOW(),NOW())
		RETURNING id`, userID, channelID).Scan(&releaseID))
	_, err := pool.Exec(ctx, `
		INSERT INTO runtime_change_slots (model_name,stable_release_id,slot_version)
		VALUES ('runtime-catalog-model',$1,2)`, releaseID)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, ReconcileActivatedCatalogModels(ctx, tx, orgID))
	require.NoError(t, tx.Commit(ctx))

	var providerFamily, lifecycle string
	var approved bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT provider_family,lifecycle,approved_catalog
		  FROM model_registry
		 WHERE org_id=$1 AND model_name='runtime-catalog-model'`, orgID).Scan(&providerFamily, &lifecycle, &approved))
	require.Equal(t, "custom-maas", providerFamily)
	require.Equal(t, LifecycleGA, lifecycle)
	require.True(t, approved)
}

func callableModelsByCheckAccess(t *testing.T, ctx context.Context, q db.DBQuerier, orgID, projectID int64, candidates []string) []string {
	t.Helper()
	allowed := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result, err := CheckAccess(ctx, q, orgID, projectID, candidate)
		if errors.Is(err, ErrModelRetired) || errors.Is(err, ErrModelDenied) {
			continue
		}
		require.NoError(t, err)
		if result.Allowed {
			allowed = append(allowed, candidate)
		}
	}
	return allowed
}
