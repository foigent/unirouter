//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelEndpointLifecycleKeepsOneStableTruthAcrossPauseAndResume(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID, channelID, credentialID, routingEpoch, credentialEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'model-endpoint-owner','fixture','platform_admin','platform_admin','active')
RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
  name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
  priority,routing_weight,degradation_threshold,status,circuit_state)
VALUES ('DeepSeek official','deepseek','https://provider.invalid','company-fast',
  '{"company-fast":"deepseek/deepseek-v4-flash"}','openai_compatible','{}',
  100,100,10,'staged','healthy')
RETURNING id,routing_epoch`).Scan(&channelID, &routingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
VALUES ($1,'encrypted-fixture','company-fast','active')
RETURNING id,state_epoch`, channelID).Scan(&credentialID, &credentialEpoch))
	_, err := pool.Exec(ctx, `
INSERT INTO channel_model_probes (
  channel_id,public_model,upstream_model,routing_epoch,credential_id,
  credential_state_epoch,success,status_code,latency_ms,tested_by)
VALUES ($1,'company-fast','deepseek/deepseek-v4-flash',$2,$3,$4,TRUE,200,18,$5)`,
		channelID, routingEpoch, credentialID, credentialEpoch, userID)
	require.NoError(t, err)

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)
	handler := NewHandler(pool, "model-endpoint-jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRoutingController(router)
	handler.SetRuntimeChangeManager(manager)

	authorized := func(method, target, body string) *http.Request {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		requestContext := context.WithValue(request.Context(), middleware.CtxUserID, userID)
		requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, auth.RolePlatformAdmin)
		customer := customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: orgID, OrganizationName: "Customer"}
		requestContext = context.WithValue(requestContext, middleware.CtxCustomerContext, customer)
		requestContext = context.WithValue(requestContext, middleware.CtxCustomerOrgID, orgID)
		return request.WithContext(requestContext)
	}

	activate := authorized(http.MethodPost, "/api/v1/model-endpoints/activate",
		fmt.Sprintf(`{"channel_id":%d,"model":"company-fast","expected_routing_epoch":%d}`, channelID, routingEpoch))
	activationResponse := httptest.NewRecorder()
	handler.ActivateModelEndpoint(activationResponse, activate)
	require.Equal(t, http.StatusOK, activationResponse.Code, activationResponse.Body.String())
	var activated runtimechange.Release
	require.NoError(t, json.Unmarshal(activationResponse.Body.Bytes(), &activated))
	require.Equal(t, "active", activated.Stage)
	require.NotEmpty(t, activated.ReleaseRef)

	assertProjection := func(expectedState string, expectedRouteAvailable bool, expectedTargetState string) modelEndpointProjection {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ListModelEndpoints(response, authorized(http.MethodGet, "/api/v1/model-endpoints", ""))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var projection modelEndpointProjection
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
		require.Len(t, projection.Models, 1)
		model := projection.Models[0]
		require.Equal(t, modelEndpointProjectionVersion, projection.ContractVersion)
		require.Equal(t, "company-fast", model.PublicModel)
		require.Equal(t, expectedState, model.State)
		require.Equal(t, expectedRouteAvailable, model.RouteAvailable)
		require.Equal(t, activated.ReleaseRef, model.StableReleaseRef)
		require.Equal(t, 1, model.ReleaseHistoryCount)
		require.Len(t, model.Targets, 1)
		require.Equal(t, expectedTargetState, model.Targets[0].State)
		return projection
	}

	assertProjection("active", true, "active")

	pause := authorized(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d", channelID),
		fmt.Sprintf(`{"status":"disabled","expected_routing_epoch":%d}`, routingEpoch))
	pause.SetPathValue("id", fmt.Sprintf("%d", channelID))
	pauseResponse := httptest.NewRecorder()
	handler.UpdateChannel(pauseResponse, pause)
	require.Equal(t, http.StatusOK, pauseResponse.Code, pauseResponse.Body.String())
	router.Invalidate()
	assertProjection("degraded", false, "paused")

	_, err = pool.Exec(ctx, `UPDATE channel_keys SET state_epoch=state_epoch+1,updated_at=NOW() WHERE id=$1`, credentialID)
	require.NoError(t, err)
	var rotatedCredentialEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT state_epoch FROM channel_keys WHERE id=$1`, credentialID).Scan(&rotatedCredentialEpoch))
	blockedResume := authorized(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d", channelID),
		fmt.Sprintf(`{"status":"staged","expected_routing_epoch":%d}`, routingEpoch))
	blockedResume.SetPathValue("id", fmt.Sprintf("%d", channelID))
	blockedResumeResponse := httptest.NewRecorder()
	handler.UpdateChannel(blockedResumeResponse, blockedResume)
	require.Equal(t, http.StatusConflict, blockedResumeResponse.Code, blockedResumeResponse.Body.String())
	require.Contains(t, blockedResumeResponse.Body.String(), `"code":"current_probe_required"`)

	_, err = pool.Exec(ctx, `
UPDATE channel_model_probes
   SET credential_state_epoch=$2,success=TRUE,status_code=200,latency_ms=21,tested_at=NOW()
 WHERE channel_id=$1 AND public_model='company-fast'`, channelID, rotatedCredentialEpoch)
	require.NoError(t, err)
	resume := authorized(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d", channelID),
		fmt.Sprintf(`{"status":"staged","expected_routing_epoch":%d}`, routingEpoch))
	resume.SetPathValue("id", fmt.Sprintf("%d", channelID))
	resumeResponse := httptest.NewRecorder()
	handler.UpdateChannel(resumeResponse, resume)
	require.Equal(t, http.StatusOK, resumeResponse.Code, resumeResponse.Body.String())
	router.Invalidate()
	assertProjection("active", true, "active")

	var finalEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT routing_epoch FROM channels WHERE id=$1`, channelID).Scan(&finalEpoch))
	require.Equal(t, routingEpoch, finalEpoch)

	var alternateChannelID, alternateCredentialID, alternateRoutingEpoch, alternateCredentialEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
  name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
  priority,routing_weight,degradation_threshold,status,circuit_state)
VALUES ('OpenRouter DeepSeek','openrouter','https://openrouter.invalid/v1','company-fast',
  '{"company-fast":"deepseek/deepseek-v4-flash:free"}','openai_compatible','{}',
  100,100,10,'staged','healthy')
RETURNING id,routing_epoch`).Scan(&alternateChannelID, &alternateRoutingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
VALUES ($1,'encrypted-alternate-fixture','company-fast','active')
RETURNING id,state_epoch`, alternateChannelID).Scan(&alternateCredentialID, &alternateCredentialEpoch))
	_, err = pool.Exec(ctx, `
INSERT INTO channel_model_probes (
  channel_id,public_model,upstream_model,routing_epoch,credential_id,
  credential_state_epoch,success,status_code,latency_ms,tested_by)
VALUES ($1,'company-fast','deepseek/deepseek-v4-flash:free',$2,$3,$4,TRUE,200,29,$5)`,
		alternateChannelID, alternateRoutingEpoch, alternateCredentialID, alternateCredentialEpoch, userID)
	require.NoError(t, err)
	// The production channel mutation path invalidates process-local route
	// snapshots. This fixture writes directly to PostgreSQL, so mirror that
	// contract before asking runtime change control to evaluate the new target.
	router.Invalidate()

	canaryRequest := authorized(http.MethodPost, "/api/v1/model-endpoints/canary",
		fmt.Sprintf(`{"channel_id":%d,"model":"company-fast","expected_routing_epoch":%d,"canary_basis_points":500}`,
			alternateChannelID, alternateRoutingEpoch))
	canaryResponse := httptest.NewRecorder()
	handler.StartModelEndpointCanary(canaryResponse, canaryRequest)
	require.Equal(t, http.StatusOK, canaryResponse.Code, canaryResponse.Body.String())
	var canary runtimechange.Release
	require.NoError(t, json.Unmarshal(canaryResponse.Body.Bytes(), &canary))
	require.Equal(t, "canary", canary.Stage)
	require.Len(t, canary.Candidate.Overrides, 2, "the canary must merge the stable target with the new provider target")
	seenChannels := map[int64]bool{}
	for _, override := range canary.Candidate.Overrides {
		seenChannels[override.ChannelID] = true
	}
	require.True(t, seenChannels[channelID])
	require.True(t, seenChannels[alternateChannelID])

	projectionResponse := httptest.NewRecorder()
	handler.ListModelEndpoints(projectionResponse, authorized(http.MethodGet, "/api/v1/model-endpoints", ""))
	require.Equal(t, http.StatusOK, projectionResponse.Code, projectionResponse.Body.String())
	var canaryProjection modelEndpointProjection
	require.NoError(t, json.Unmarshal(projectionResponse.Body.Bytes(), &canaryProjection))
	require.Len(t, canaryProjection.Models, 1)
	require.Equal(t, "canary", canaryProjection.Models[0].State)
	require.Equal(t, activated.ReleaseRef, canaryProjection.Models[0].StableReleaseRef)
	require.Equal(t, canary.ReleaseRef, canaryProjection.Models[0].CanaryReleaseRef)
	require.Equal(t, 2, canaryProjection.Models[0].ReleaseHistoryCount)
	require.Len(t, canaryProjection.Models[0].Targets, 2)
	targetStates := map[int64]string{}
	for _, target := range canaryProjection.Models[0].Targets {
		targetStates[target.ChannelID] = target.State
	}
	require.Equal(t, "active", targetStates[channelID])
	require.Equal(t, "canary", targetStates[alternateChannelID])

	// End the canary so the original stable release is retained, then emulate
	// the real failure mode: that endpoint is administratively disabled and its
	// model declaration changes after the release was activated. Recovery must
	// pin the old endpoint's current epoch as an explicit deny and atomically
	// replace the unavailable stable route with the already-tested alternate.
	canary, err = manager.Rollback(ctx, canary.ReleaseRef, canary.Version, userID)
	require.NoError(t, err)
	require.Equal(t, "rolled_back", canary.Stage)
	var driftedRoutingEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
UPDATE channels
   SET status='disabled',models='retired-old-model',routing_epoch=routing_epoch+1,updated_at=NOW()
 WHERE id=$1
 RETURNING routing_epoch`, channelID).Scan(&driftedRoutingEpoch))
	require.Greater(t, driftedRoutingEpoch, routingEpoch)
	router.Invalidate()

	_, err = router.Preview(ctx, "company-fast")
	require.ErrorIs(t, err, gateway.ErrRuntimeAssignmentStale, "the retained stable route must be unavailable before recovery")

	recoveryRequest := authorized(http.MethodPost, "/api/v1/model-endpoints/recover",
		fmt.Sprintf(`{"channel_id":%d,"model":"company-fast","expected_routing_epoch":%d}`,
			alternateChannelID, alternateRoutingEpoch))
	recoveryResponse := httptest.NewRecorder()
	handler.RecoverModelEndpoint(recoveryResponse, recoveryRequest)
	require.Equal(t, http.StatusOK, recoveryResponse.Code, recoveryResponse.Body.String())
	var recovered runtimechange.Release
	require.NoError(t, json.Unmarshal(recoveryResponse.Body.Bytes(), &recovered))
	require.Equal(t, "active", recovered.Stage)
	require.Equal(t, activated.ID, *recovered.ParentStableReleaseID)

	retainedAfterRecovery, err := manager.Get(ctx, activated.ReleaseRef)
	require.NoError(t, err)
	require.Equal(t, "rolled_back", retainedAfterRecovery.Stage)
	slot, err := manager.Slot(ctx, "company-fast")
	require.NoError(t, err)
	require.Equal(t, recovered.ReleaseRef, slot.StableReleaseRef)
	require.Empty(t, slot.CanaryReleaseRef)

	recoveryOverrides := map[int64]routingpolicy.ChannelOverride{}
	for _, override := range recovered.Candidate.Overrides {
		recoveryOverrides[override.ChannelID] = override
	}
	require.Equal(t, routingpolicy.AdmissionDeny, recoveryOverrides[channelID].Admission)
	require.Equal(t, driftedRoutingEpoch, recoveryOverrides[channelID].ExpectedRoutingEpoch)
	require.Equal(t, routingpolicy.AdmissionAllow, recoveryOverrides[alternateChannelID].Admission)
	require.Equal(t, alternateRoutingEpoch, recoveryOverrides[alternateChannelID].ExpectedRoutingEpoch)

	router.Invalidate()
	recoveredPreview, err := router.Preview(ctx, "company-fast")
	require.NoError(t, err)
	require.Len(t, recoveredPreview.Candidates, 1)
	require.Equal(t, alternateChannelID, recoveredPreview.Candidates[0].ChannelID)
}

func TestModelEndpointActivationRejectsEquivalentCatalogIdentityBeforeCreatingARelease(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID, channelID, credentialID, routingEpoch, credentialEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'identity-conflict-owner','fixture','platform_admin','platform_admin','active')
RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'DeepSeek-V4-Flash','DeepSeek V4 Flash','deepseek','ga',TRUE)`, orgID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
  name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
  priority,routing_weight,degradation_threshold,status,circuit_state)
VALUES ('DeepSeek duplicate','deepseek','https://provider.invalid','DeepSeek V4 Flash',
  '{"DeepSeek V4 Flash":"deepseek-v4-flash"}','openai_compatible','{}',
  100,100,10,'staged','healthy')
RETURNING id,routing_epoch`).Scan(&channelID, &routingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
VALUES ($1,'encrypted-fixture','DeepSeek V4 Flash','active')
RETURNING id,state_epoch`, channelID).Scan(&credentialID, &credentialEpoch))
	_, err = pool.Exec(ctx, `
INSERT INTO channel_model_probes (
  channel_id,public_model,upstream_model,routing_epoch,credential_id,
  credential_state_epoch,success,status_code,latency_ms,tested_by)
VALUES ($1,'DeepSeek V4 Flash','deepseek-v4-flash',$2,$3,$4,TRUE,200,18,$5)`,
		channelID, routingEpoch, credentialID, credentialEpoch, userID)
	require.NoError(t, err)

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	handler := NewHandler(pool, "model-endpoint-jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRoutingController(router)
	handler.SetRuntimeChangeManager(manager)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/model-endpoints/activate", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"model":"DeepSeek V4 Flash","expected_routing_epoch":%d}`, channelID, routingEpoch,
	)))
	requestContext := context.WithValue(request.Context(), middleware.CtxUserID, userID)
	requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
	requestContext = context.WithValue(requestContext, middleware.CtxUserRole, auth.RolePlatformAdmin)
	requestContext = context.WithValue(requestContext, middleware.CtxCustomerContext, customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: orgID, OrganizationName: "Customer",
	})
	requestContext = context.WithValue(requestContext, middleware.CtxCustomerOrgID, orgID)
	response := httptest.NewRecorder()
	handler.ActivateModelEndpoint(response, request.WithContext(requestContext))

	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"model_identity_conflict"`)
	require.Contains(t, response.Body.String(), `"canonical_model":"DeepSeek-V4-Flash"`)
	var releaseCount, slotCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_change_releases`).Scan(&releaseCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_change_slots`).Scan(&slotCount))
	require.Zero(t, releaseCount, "identity conflict must not leave a draft or validated orphan")
	require.Zero(t, slotCount)
}
