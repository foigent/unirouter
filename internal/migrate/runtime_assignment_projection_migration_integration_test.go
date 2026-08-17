//go:build integration

package migrate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRuntimeAssignmentProjectionMigrationIsCanonicalAndReversible(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID, channelID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('assignment-projection','active','customer') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
		VALUES ($1,'assignment-projection-owner','test-hash','org_admin','active','org_admin') RETURNING id`, orgID,
	).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
		VALUES ('assignment-projection-channel','custom','https://provider.invalid','public-model','staged',7)
		RETURNING id`,
	).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO runtime_change_releases (
		  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
		  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
		  reason,created_by,validated_by,activated_by,validated_at,activated_at)
		VALUES (
		  'rcr_assignment_projection','public-model','unirouter.runtime-change-candidate.v1',
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		  jsonb_build_object(
		    'contract_version','unirouter.runtime-change-candidate.v1',
		    'model_name','public-model',
		    'overrides',jsonb_build_array(jsonb_build_object(
		      'channel_id',$2::bigint,'expected_routing_epoch',7,'admission','allow'))),
		  '[]','{}','unchanged','active',5,
		  'assignment projection fixture',$1,$1,$1,NOW(),NOW())
		RETURNING id`, userID, channelID,
	).Scan(&releaseID))
	_, err := pool.Exec(ctx, `
		INSERT INTO runtime_change_slots (model_name,stable_release_id,canary_basis_points,slot_version)
		VALUES ('public-model',$1,0,9)`, releaseID)
	require.NoError(t, err)

	var model, kind, ref, stage, admission string
	var projectedChannel, expectedEpoch, releaseVersion, slotVersion int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT model_name,assignment_kind,release_ref,release_stage,admission,
		       channel_id,expected_routing_epoch,release_version,slot_version
		  FROM runtime_channel_assignments_v1
		 WHERE model_name='public-model'`,
	).Scan(&model, &kind, &ref, &stage, &admission, &projectedChannel, &expectedEpoch, &releaseVersion, &slotVersion))
	require.Equal(t, "public-model", model)
	require.Equal(t, "stable", kind)
	require.Equal(t, "rcr_assignment_projection", ref)
	require.Equal(t, "active", stage)
	require.Equal(t, "allow", admission)
	require.Equal(t, channelID, projectedChannel)
	require.Equal(t, int64(7), expectedEpoch)
	require.Equal(t, int64(5), releaseVersion)
	require.Equal(t, int64(9), slotVersion)

	execMigrationDown(t, ctx, pool, "000067_call_log_route_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000066_runtime_assignment_projection.down.sql")
	var viewExists, ledgerExists bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT to_regclass('public.runtime_channel_assignments_v1') IS NOT NULL,
		       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000066_runtime_assignment_projection')`,
	).Scan(&viewExists, &ledgerExists))
	require.False(t, viewExists)
	require.False(t, ledgerExists)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT to_regclass('public.runtime_channel_assignments_v1') IS NOT NULL,
		       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000066_runtime_assignment_projection')`,
	).Scan(&viewExists, &ledgerExists))
	require.True(t, viewExists)
	require.True(t, ledgerExists)
}
