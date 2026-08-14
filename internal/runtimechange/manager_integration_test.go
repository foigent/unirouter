//go:build integration

package runtimechange_test

import (
	"context"
	"encoding/json"
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

	// A disabled channel cannot receive traffic even while its historical stable
	// release remains attached, so credential recovery is safe after the
	// emergency stop and must not require deleting the release evidence first.
	replacementKey := authorizedRequest(http.MethodPost, "/api/v1/channel-keys",
		fmt.Sprintf(`{"channel_id":%d,"key":"replacement-after-disable","allowed_models":"gpt-prd8-runtime"}`, channelID))
	replacementKeyResponse := httptest.NewRecorder()
	handler.CreateChannelKey(replacementKeyResponse, replacementKey)
	require.Equal(t, http.StatusCreated, replacementKeyResponse.Code, replacementKeyResponse.Body.String())
	var replacementKeyRecord struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(replacementKeyResponse.Body.Bytes(), &replacementKeyRecord))
	require.Positive(t, replacementKeyRecord.ID)
	replacementKeyUpdate := authorizedRequest(http.MethodPut, fmt.Sprintf("/api/v1/channel-keys/%d", replacementKeyRecord.ID),
		`{"key":"rotated-after-disable","allowed_models":"gpt-prd8-runtime","status":"active"}`)
	replacementKeyUpdate.SetPathValue("id", fmt.Sprintf("%d", replacementKeyRecord.ID))
	replacementKeyUpdateResponse := httptest.NewRecorder()
	handler.UpdateChannelKey(replacementKeyUpdateResponse, replacementKeyUpdate)
	require.Equal(t, http.StatusOK, replacementKeyUpdateResponse.Code, replacementKeyUpdateResponse.Body.String())

	deleteOriginalAfterDisable := authorizedRequest(http.MethodDelete, fmt.Sprintf("/api/v1/channel-keys/%d", keyID), "")
	deleteOriginalAfterDisable.SetPathValue("id", fmt.Sprintf("%d", keyID))
	deleteOriginalAfterDisableResponse := httptest.NewRecorder()
	handler.DeleteChannelKey(deleteOriginalAfterDisableResponse, deleteOriginalAfterDisable)
	require.Equal(t, http.StatusOK, deleteOriginalAfterDisableResponse.Code, deleteOriginalAfterDisableResponse.Body.String())

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

	var disabledEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT routing_epoch FROM channels WHERE id=$1`, channelID).Scan(&disabledEpoch))
	restage := authorizedRequest(http.MethodPut, fmt.Sprintf("/api/v1/channels/%d", channelID),
		fmt.Sprintf(`{"status":"staged","expected_routing_epoch":%d}`, disabledEpoch))
	restage.SetPathValue("id", fmt.Sprintf("%d", channelID))
	restageResponse := httptest.NewRecorder()
	handler.UpdateChannel(restageResponse, restage)
	require.Equal(t, http.StatusOK, restageResponse.Code, restageResponse.Body.String())
	router.Invalidate()

	var recoveredEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT routing_epoch FROM channels WHERE id=$1`, channelID).Scan(&recoveredEpoch))
	recovered, err := manager.CreateDraft(ctx, runtimechange.DraftRequest{
		ReleaseRef: "rcr_prd8_recovered",
		ModelName:  "gpt-prd8-runtime",
		Overrides: []routingpolicy.ChannelOverride{{
			ChannelID: channelID, ExpectedRoutingEpoch: recoveredEpoch,
			Admission: routingpolicy.AdmissionAllow,
		}},
		Reason: "restore a credential-recovered emergency-disabled connection",
	}, userID)
	require.NoError(t, err)
	recovered, err = manager.Validate(ctx, recovered.ReleaseRef, recovered.Version, userID)
	require.NoError(t, err)
	recovered, err = manager.ActivateInitial(ctx, recovered.ReleaseRef, recovered.Version, userID, orgID)
	require.NoError(t, err)
	require.Equal(t, "active", recovered.Stage)
	router.Invalidate()
	recoveredRoutes, err := router.RouteAllWithKey(ctx, "gpt-prd8-runtime", "after-controlled-recovery")
	require.NoError(t, err)
	require.Equal(t, channelID, recoveredRoutes[0].Channel.ID)
	require.Equal(t, recovered.ReleaseRef, recoveredRoutes[0].RuntimeReleaseRef)

	var secondEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM runtime_change_events e
		JOIN runtime_change_releases r ON r.id=e.release_id
		WHERE r.release_ref=$1`, second.ReleaseRef).Scan(&secondEventCount))
	require.Equal(t, 6, secondEventCount)
}

func TestInitialActivationCreatesFirstStableRouteAndRejectsReplacementShortcut(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID, channelID, routingEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'initial-route-admin','fixture','platform_admin','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (
		  name,provider,base_url,models,adapter_type,adapter_config,
		  priority,routing_weight,degradation_threshold,status,circuit_state)
		VALUES ('initial-route','openai','https://provider.invalid','initial-route-model',
		  'openai_compatible','{}',100,100,10,'staged','healthy')
		RETURNING id,routing_epoch`).Scan(&channelID, &routingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
		VALUES ($1,'fixture','initial-route-model','active') RETURNING id`, channelID).Scan(new(int64)))

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)

	createValidated := func(ref string) runtimechange.Release {
		release, createErr := manager.CreateDraft(ctx, runtimechange.DraftRequest{
			ReleaseRef: ref, ModelName: "initial-route-model", Reason: "verified initial model connection",
			Overrides: []routingpolicy.ChannelOverride{{
				ChannelID: channelID, ExpectedRoutingEpoch: routingEpoch, Admission: routingpolicy.AdmissionAllow,
			}},
		}, userID)
		require.NoError(t, createErr)
		release, createErr = manager.Validate(ctx, release.ReleaseRef, release.Version, userID)
		require.NoError(t, createErr)
		return release
	}

	first := createValidated("rcr_initial_route_first")
	_, err = pool.Exec(ctx, `
		INSERT INTO model_registry (
		  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
		VALUES ($1,'initial-route-model','','','staging',FALSE)`, orgID)
	require.NoError(t, err)
	first, err = manager.ActivateInitial(ctx, first.ReleaseRef, first.Version, userID, orgID)
	require.NoError(t, err)
	require.Equal(t, "active", first.Stage)
	routes, err := router.RouteAllWithKey(ctx, "initial-route-model", "first-stable-request")
	require.NoError(t, err)
	require.Equal(t, channelID, routes[0].Channel.ID)
	require.Equal(t, first.ReleaseRef, routes[0].RuntimeReleaseRef)
	var action string
	var initialActivation bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT action, COALESCE((detail->>'initial_activation')::boolean, false)
		  FROM runtime_change_events
		 WHERE release_id=$1 AND to_stage='active'`, first.ID).Scan(&action, &initialActivation))
	require.Equal(t, "promote", action)
	require.True(t, initialActivation)
	var displayName, providerFamily, lifecycle, pricingModelName string
	var approvedCatalog bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT display_name,provider_family,lifecycle,pricing_model_name,approved_catalog
		  FROM model_registry
		 WHERE org_id=$1 AND model_name='initial-route-model'`, orgID).Scan(
		&displayName, &providerFamily, &lifecycle, &pricingModelName, &approvedCatalog,
	))
	require.Equal(t, "initial-route-model", displayName)
	require.Equal(t, "openai", providerFamily)
	require.Equal(t, "ga", lifecycle)
	require.Equal(t, "initial-route-model", pricingModelName)
	require.True(t, approvedCatalog)

	replacement := createValidated("rcr_initial_route_replacement")
	_, err = manager.ActivateInitial(ctx, replacement.ReleaseRef, replacement.Version, userID, orgID)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	first, err = manager.Rollback(ctx, first.ReleaseRef, first.Version, userID)
	require.NoError(t, err)
	require.Equal(t, "rolled_back", first.Stage)
	_, err = router.RouteAllWithKey(ctx, "initial-route-model", "after-initial-rollback")
	require.Error(t, err)
}

func TestInitialActivationRejectsRetiredCatalogModelAtomically(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID, channelID, routingEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'retired-route-admin','fixture','platform_admin','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (
		  name,provider,base_url,models,adapter_type,adapter_config,
		  priority,routing_weight,degradation_threshold,status,circuit_state)
		VALUES ('retired-initial-route','openai','https://provider.invalid','retired-initial-model',
		  'openai_compatible','{}',100,100,10,'staged','healthy')
		RETURNING id,routing_epoch`).Scan(&channelID, &routingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
		VALUES ($1,'fixture','retired-initial-model','active') RETURNING id`, channelID).Scan(new(int64)))
	_, err := pool.Exec(ctx, `
		INSERT INTO model_registry (
		  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
		VALUES ($1,'retired-initial-model','Retired model','openai','retired',FALSE)`, orgID)
	require.NoError(t, err)

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)

	release, err := manager.CreateDraft(ctx, runtimechange.DraftRequest{
		ReleaseRef: "rcr_retired_initial_route", ModelName: "retired-initial-model", Reason: "retired model must stay unavailable",
		Overrides: []routingpolicy.ChannelOverride{{
			ChannelID: channelID, ExpectedRoutingEpoch: routingEpoch, Admission: routingpolicy.AdmissionAllow,
		}},
	}, userID)
	require.NoError(t, err)
	release, err = manager.Validate(ctx, release.ReleaseRef, release.Version, userID)
	require.NoError(t, err)

	_, err = manager.ActivateInitial(ctx, release.ReleaseRef, release.Version, userID, orgID)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	stored, err := manager.Get(ctx, release.ReleaseRef)
	require.NoError(t, err)
	require.Equal(t, "validated", stored.Stage)
	slot, err := manager.Slot(ctx, "retired-initial-model")
	require.NoError(t, err)
	require.Empty(t, slot.StableReleaseRef)
	require.Equal(t, "none", slot.RuntimeMutation)

	var lifecycle string
	var approved bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT lifecycle,approved_catalog
		  FROM model_registry
		 WHERE org_id=$1 AND model_name='retired-initial-model'`, orgID).Scan(&lifecycle, &approved))
	require.Equal(t, "retired", lifecycle)
	require.False(t, approved)
}
