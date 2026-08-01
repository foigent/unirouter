//go:build integration

package runtimechange_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPRD8RuntimeChangeLifecycleCanaryActivationAndEmergencyDisable(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, userID, channelID, keyID, routingEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('prd8-runtime-lifecycle','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'prd8-runtime-owner','test-hash','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (
		  name,provider,base_url,models,adapter_type,adapter_config,
		  priority,routing_weight,degradation_threshold,status,circuit_state)
		VALUES ('prd8-staged-openai','openai','https://provider.invalid','gpt-prd8-runtime',
		  'openai_compatible','{}',100,100,10,'staged','healthy')
		RETURNING id,routing_epoch`).Scan(&channelID, &routingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
		VALUES ($1,'encrypted-prd8-runtime','gpt-prd8-runtime','active') RETURNING id`, channelID).Scan(&keyID))

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)

	_, err = router.RouteAllWithKey(ctx, "gpt-prd8-runtime", "before-release")
	require.ErrorContains(t, err, "no available channel")

	newRelease := func(ref string) runtimechange.Release {
		t.Helper()
		release, createErr := manager.CreateDraft(ctx, runtimechange.DraftRequest{
			ReleaseRef: ref,
			ModelName:  "gpt-prd8-runtime",
			Overrides: []routingpolicy.ChannelOverride{{
				ChannelID: channelID, ExpectedRoutingEpoch: routingEpoch,
				Admission: routingpolicy.AdmissionAllow,
			}},
			Reason: "activate an exact Provider Pack governed staged channel",
		}, userID)
		require.NoError(t, createErr)
		require.Equal(t, "draft", release.Stage)
		require.NotEmpty(t, release.CandidateHash)
		require.NotEmpty(t, release.ProviderPackPins)
		release, createErr = manager.Validate(ctx, ref, release.Version, userID)
		require.NoError(t, createErr)
		require.Equal(t, "validated", release.Stage)
		release, createErr = manager.AdvanceShadow(ctx, ref, release.Version, userID)
		require.NoError(t, createErr)
		require.Equal(t, "shadow", release.Stage)
		return release
	}

	first := newRelease("rcr_prd8_first")
	second := newRelease("rcr_prd8_second")
	first, err = manager.StartCanary(ctx, first.ReleaseRef, first.Version, 5000, userID)
	require.NoError(t, err)
	require.Equal(t, "canary", first.Stage)
	_, err = manager.StartCanary(ctx, second.ReleaseRef, second.Version, 5000, userID)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	canarySelectionKey := ""
	for index := 0; index < 100; index++ {
		selectionKey := fmt.Sprintf("prd8-canary-%d", index)
		assignment, resolveErr := manager.Resolve(ctx, "gpt-prd8-runtime", selectionKey)
		require.NoError(t, resolveErr)
		if assignment.ReleaseRef == first.ReleaseRef {
			canarySelectionKey = selectionKey
			break
		}
	}
	require.NotEmpty(t, canarySelectionKey)
	canaryRoutes, err := router.RouteAllWithKey(ctx, "gpt-prd8-runtime", canarySelectionKey)
	require.NoError(t, err)
	require.Equal(t, channelID, canaryRoutes[0].Channel.ID)
	require.Equal(t, first.ReleaseRef, canaryRoutes[0].RuntimeReleaseRef)
	require.Equal(t, "canary", canaryRoutes[0].RuntimeStage)

	first, err = manager.Rollback(ctx, first.ReleaseRef, first.Version, userID)
	require.NoError(t, err)
	require.Equal(t, "rolled_back", first.Stage)
	second, err = manager.StartCanary(ctx, second.ReleaseRef, second.Version, 5000, userID)
	require.NoError(t, err)
	second, err = manager.Promote(ctx, second.ReleaseRef, second.Version, userID)
	require.NoError(t, err)
	require.Equal(t, "active", second.Stage)

	stableRoutes, err := router.RouteAllWithKey(ctx, "gpt-prd8-runtime", "stable-request")
	require.NoError(t, err)
	require.Equal(t, channelID, stableRoutes[0].Channel.ID)
	require.Equal(t, second.ReleaseRef, stableRoutes[0].RuntimeReleaseRef)
	require.Equal(t, "active", stableRoutes[0].RuntimeStage)
	require.NotEmpty(t, stableRoutes[0].RuntimeAssignmentID)

	handler := admin.NewHandler(pool, "prd8-runtime-secret", []byte("0123456789abcdef01234567"), nil)
	authorizedRequest := func(method, target, body string) *http.Request {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		requestContext := context.WithValue(request.Context(), middleware.CtxUserID, userID)
		requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, auth.RolePlatformAdmin)
		return request.WithContext(requestContext)
	}

	update := authorizedRequest(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d", channelID),
		fmt.Sprintf(`{"name":"must-not-change","expected_routing_epoch":%d}`, routingEpoch))
	update.SetPathValue("id", fmt.Sprintf("%d", channelID))
	updateResponse := httptest.NewRecorder()
	handler.UpdateChannel(updateResponse, update)
	require.Equal(t, http.StatusConflict, updateResponse.Code)

	createKey := authorizedRequest(http.MethodPost, "/api/v1/channel-keys",
		fmt.Sprintf(`{"channel_id":%d,"key":"must-not-activate"}`, channelID))
	createKeyResponse := httptest.NewRecorder()
	handler.CreateChannelKey(createKeyResponse, createKey)
	require.Equal(t, http.StatusConflict, createKeyResponse.Code)

	deleteKey := authorizedRequest(http.MethodDelete, fmt.Sprintf("/api/v1/channel-keys/%d", keyID), "")
	deleteKey.SetPathValue("id", fmt.Sprintf("%d", keyID))
	deleteKeyResponse := httptest.NewRecorder()
	handler.DeleteChannelKey(deleteKeyResponse, deleteKey)
	require.Equal(t, http.StatusConflict, deleteKeyResponse.Code)

	emergencyDisable := authorizedRequest(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d", channelID),
		fmt.Sprintf(`{"status":"disabled","expected_routing_epoch":%d}`, routingEpoch))
	emergencyDisable.SetPathValue("id", fmt.Sprintf("%d", channelID))
	emergencyResponse := httptest.NewRecorder()
	handler.UpdateChannel(emergencyResponse, emergencyDisable)
	require.Equal(t, http.StatusOK, emergencyResponse.Code, emergencyResponse.Body.String())
	router.Invalidate()
	_, err = router.RouteAllWithKey(ctx, "gpt-prd8-runtime", "after-emergency-disable")
	require.Error(t, err)

	second, err = manager.Rollback(ctx, second.ReleaseRef, second.Version, userID)
	require.NoError(t, err, "rollback must remain available after emergency epoch drift")
	require.Equal(t, "rolled_back", second.Stage)
	slot, err := manager.Slot(ctx, "gpt-prd8-runtime")
	require.NoError(t, err)
	require.Empty(t, slot.StableReleaseRef)
	require.Empty(t, slot.CanaryReleaseRef)
	require.Equal(t, "none", slot.RuntimeMutation)

	var secondEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM runtime_change_events e
		JOIN runtime_change_releases r ON r.id=e.release_id
		WHERE r.release_ref=$1`, second.ReleaseRef).Scan(&secondEventCount))
	require.Equal(t, 6, secondEventCount)
}
