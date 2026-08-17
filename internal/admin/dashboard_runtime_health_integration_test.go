//go:build integration

package admin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDashboardSeparatesRuntimeAvailabilityFromWindowHealthIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, userID, channelID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('dashboard-runtime-health','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'dashboard-runtime-owner','test-hash','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status,circuit_state)
		VALUES ('Kimi','kimi','https://provider.invalid','kimi-k3,deepseek-v4-flash','staged','healthy') RETURNING id`,
	).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO runtime_change_releases (
		  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
		  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
		  reason,created_by,validated_by,activated_by,validated_at,activated_at)
		VALUES (
		  'rcr_dashboard_runtime_health','kimi-k3','unirouter.runtime-change-candidate.v1',
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		  jsonb_build_object(
		    'contract_version','unirouter.runtime-change-candidate.v1',
		    'model_name','kimi-k3',
		    'overrides',jsonb_build_array(jsonb_build_object(
		      'channel_id',$2::bigint,'expected_routing_epoch',1,'admission','allow'))),
		  '[]','{}','unchanged','active',3,
		  'dashboard runtime availability fixture',$1,$1,$1,NOW(),NOW())
		RETURNING id`, userID, channelID).Scan(&releaseID))
	_, err := pool.Exec(ctx, `
		INSERT INTO runtime_change_slots (model_name,stable_release_id,canary_release_id,canary_basis_points,slot_version)
		VALUES ('kimi-k3',$1,NULL,0,1)`, releaseID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO call_logs (request_id,org_id,model,requested_model,provider,channel,status_code,latency_ms,created_at)
		VALUES
		 ('dashboard-kimi-legacy',$1,'Kimi K3','Kimi K3','kimi','Kimi',503,45,NOW()),
		 ('dashboard-kimi-canonical',$1,'kimi-k3','kimi-k3','kimi','Kimi',200,10624,NOW())`, orgID)
	require.NoError(t, err)

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)
	h := NewHandler(pool, "dashboard-test", []byte("0123456789abcdef01234567"), nil)
	h.SetRoutingController(router)
	h.SetRuntimeChangeManager(manager)
	models, err := h.dashboardModelDistribution(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "kimi-k3", models[0]["model"])
	require.Equal(t, 2, models[0]["requests"])
	require.Equal(t, 1, models[0]["success_count"])
	require.Equal(t, 1, models[0]["fail_count"])
	require.InDelta(t, 0.5, models[0]["success_rate"], 0.0001)
	require.InDelta(t, 10095.05, models[0]["p95_latency_ms"], 0.01)

	channels, err := h.dashboardChannelHealth(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, true, channels[0]["runtime_published"])
	require.Equal(t, true, channels[0]["enabled"])
	require.Equal(t, false, channels[0]["credential_ready"])
	require.Equal(t, false, channels[0]["available"])
	require.Equal(t, "no_credential", channels[0]["status"])

	_, err = pool.Exec(ctx, `
		INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
		VALUES ($1,'encrypted-dashboard-key','deepseek-v4-flash','active')`, channelID)
	require.NoError(t, err)
	router.Invalidate()
	channels, err = h.dashboardChannelHealth(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, false, channels[0]["credential_ready"], "a key for another configured model must not make the published model look ready")
	require.Equal(t, false, channels[0]["available"])
	require.Equal(t, "no_credential", channels[0]["status"])

	_, err = pool.Exec(ctx, `UPDATE channel_keys SET allowed_models='kimi-k3',state_epoch=state_epoch+1 WHERE channel_id=$1`, channelID)
	require.NoError(t, err)
	router.Invalidate()
	channels, err = h.dashboardChannelHealth(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, true, channels[0]["credential_ready"])
	require.Equal(t, true, channels[0]["available"])
	require.Equal(t, "warning", channels[0]["status"])

	_, err = pool.Exec(ctx, `UPDATE channels SET routing_epoch=routing_epoch+1 WHERE id=$1`, channelID)
	require.NoError(t, err)
	router.Invalidate()
	channels, err = h.dashboardChannelHealth(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, false, channels[0]["runtime_published"])
	require.Equal(t, false, channels[0]["available"])
	require.Equal(t, "stale_release", channels[0]["status"])
}
