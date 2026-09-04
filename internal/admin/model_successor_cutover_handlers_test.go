package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

func TestModelSuccessorFingerprintPreservesPublishedV1WireAndSeparatesStagingV2(t *testing.T) {
	request := modelSuccessorCutoverRequest{
		ExpectedReleaseVersion:   2,
		StableRequestModel:       "glm-5.2",
		PredecessorPublicModelID: 11, PredecessorModelName: "glm-5.2",
		ExpectedPredecessorLifecycle: "ga", ExpectedPredecessorApprovedCatalog: true,
		SuccessorPublicModelID: 12, SuccessorModelName: "glm-5.2-20260902",
		ExpectedSuccessorLifecycle: "staging", ExpectedSuccessorApprovedCatalog: false,
		ExpectedModelSets: []runtimechange.ModelSuccessorExpectedModelSet{},
		Reason:            "first successor",
	}
	published, err := modelSuccessorCutoverMutation("rcr_glm52_successor", request)
	require.NoError(t, err)
	publishedWire, err := json.Marshal(published)
	require.NoError(t, err)
	require.NotContains(t, string(publishedWire), "predecessor_authority_mode")
	require.NotContains(t, string(publishedWire), "expected_primary_alias_id")
	require.NotContains(t, string(publishedWire), "expected_predecessor_slot_version")
	require.NotContains(t, string(publishedWire), "expected_predecessor_release_ref")
	publishedFingerprint, err := modelSuccessorCutoverFingerprint(published, "hash-secret")
	require.NoError(t, err)

	request.ExpectedPredecessorLifecycle = "staging"
	request.ExpectedPredecessorApprovedCatalog = false
	staging, err := stagingModelSuccessorCutoverMutation("rcr_glm52_successor", request)
	require.NoError(t, err)
	stagingWire, err := json.Marshal(staging)
	require.NoError(t, err)
	require.NotContains(t, string(stagingWire), "predecessor_authority_mode")
	stagingFingerprint, err := modelSuccessorCutoverFingerprint(staging, "hash-secret")
	require.NoError(t, err)
	require.NotEqual(t, publishedFingerprint, stagingFingerprint)
}

func TestRepeatedSuccessorFingerprintPinsEveryBeforeStateField(t *testing.T) {
	request := modelSuccessorCutoverRequest{
		PredecessorAuthorityMode: runtimechange.ModelSuccessorPredecessorPrimaryAlias,
		ExpectedPrimaryAliasID:   7, ExpectedPredecessorSlotVersion: 2, ExpectedPredecessorReleaseRef: "rcr_previous",
		ExpectedReleaseVersion: 2, StableRequestModel: "glm-5.2",
		PredecessorPublicModelID: 11, PredecessorModelName: "glm-5.2-old",
		ExpectedPredecessorLifecycle: "ga", ExpectedPredecessorApprovedCatalog: true,
		SuccessorPublicModelID: 12, SuccessorModelName: "glm-5.2-next",
		ExpectedSuccessorLifecycle: "staging", ExpectedModelSets: []runtimechange.ModelSuccessorExpectedModelSet{},
	}
	mutation, err := modelSuccessorCutoverMutation("rcr_next", request)
	require.NoError(t, err)
	want, err := modelSuccessorCutoverFingerprint(mutation, "test-only-secret")
	require.NoError(t, err)
	for _, mutate := range []func(*runtimechange.ModelSuccessorCutoverMutation){
		func(m *runtimechange.ModelSuccessorCutoverMutation) { m.ExpectedPrimaryAliasID++ },
		func(m *runtimechange.ModelSuccessorCutoverMutation) { m.ExpectedPredecessorSlotVersion++ },
		func(m *runtimechange.ModelSuccessorCutoverMutation) { m.ExpectedPredecessorReleaseRef = "rcr_another" },
	} {
		changed := mutation
		mutate(&changed)
		got, err := modelSuccessorCutoverFingerprint(changed, "test-only-secret")
		require.NoError(t, err)
		require.NotEqual(t, want, got)
	}
	_, err = stagingModelSuccessorCutoverMutation("rcr_next", request)
	require.Error(t, err)
}

func TestActivateModelSuccessorUsesAtomicApplicationCommand(t *testing.T) {
	stub := &deploymentLifecycleApplicationStub{successorReceipt: runtimechange.ModelSuccessorCutoverReceipt{
		ContractVersion: runtimechange.ModelSuccessorCutoverReceiptContractVersion,
		Lifecycle: runtimechange.DeploymentLifecycleReceipt{
			ContractVersion: runtimechange.ModelSuccessorLifecycleReceiptContractVersion,
			ReceiptID:       91, OriginalHTTPStatus: http.StatusOK,
		},
	}}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(t, http.MethodPost,
		"/api/v1/model-deployment-releases/rcr_glm52_successor/activate-successor", `{
  "expected_release_version":2,
  "stable_request_model":"glm-5.2",
  "predecessor_public_model_id":11,
  "predecessor_model_name":"glm-5.2",
  "expected_predecessor_lifecycle":"ga",
  "expected_predecessor_approved_catalog":true,
  "successor_public_model_id":12,
  "successor_model_name":"glm-5.2-20260902",
  "expected_successor_lifecycle":"staging",
  "expected_successor_approved_catalog":false,
  "expected_model_sets":[],
  "reason":"first successor"
}`, true)
	request.SetPathValue("releaseRef", "RCR_GLM52_SUCCESSOR")
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret("hash-secret")(http.HandlerFunc(handler.ActivateModelSuccessor)).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "rcr_glm52_successor", stub.successorCommand.Mutation.ReleaseRef)
	require.Equal(t, "glm-5.2", stub.successorCommand.Mutation.StableRequestModel)
	require.Equal(t, "glm-5.2-20260902", stub.successorCommand.Mutation.SuccessorModelName)
	require.Len(t, stub.successorCommand.IdempotencyKeyHash, 64)
	require.True(t, strings.HasPrefix(stub.successorCommand.RequestFingerprint, "hmac-sha256:"))
	require.Equal(t, int64(17), stub.successorCommand.CustomerOrgID)
	require.Equal(t, int64(23), stub.successorCommand.ActorUserID)
}

func TestActivateStagingModelSuccessorUsesIndependentContractIdentity(t *testing.T) {
	stub := &deploymentLifecycleApplicationStub{successorReceipt: runtimechange.ModelSuccessorCutoverReceipt{
		ContractVersion: runtimechange.StagingModelSuccessorCutoverReceiptContractVersion,
		Lifecycle: runtimechange.DeploymentLifecycleReceipt{
			ContractVersion: runtimechange.StagingModelSuccessorLifecycleReceiptContractVersion,
			ReceiptID:       92, OriginalHTTPStatus: http.StatusOK,
		},
	}}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(t, http.MethodPost,
		"/api/v1/model-deployment-releases/rcr_minimax_m27_successor/activate-staging-successor", `{
  "expected_release_version":2,
  "stable_request_model":"minimax-m2.7",
  "predecessor_public_model_id":963,
  "predecessor_model_name":"minimax-m2.7",
  "expected_predecessor_lifecycle":"staging",
  "expected_predecessor_approved_catalog":false,
  "successor_public_model_id":1001,
  "successor_model_name":"minimax-m2.7-20260903",
  "expected_successor_lifecycle":"staging",
  "expected_successor_approved_catalog":false,
  "expected_model_sets":[],
  "reason":"replace never-activated staging identity"
}`, true)
	request.SetPathValue("releaseRef", "RCR_MINIMAX_M27_SUCCESSOR")
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret("hash-secret")(http.HandlerFunc(handler.ActivateStagingModelSuccessor)).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, runtimechange.ModelSuccessorPredecessorNeverActivated, stub.successorCommand.Mutation.PredecessorAuthorityMode)
	require.Equal(t, "staging", stub.successorCommand.Mutation.ExpectedPredecessorLifecycle)
	require.False(t, stub.successorCommand.Mutation.ExpectedPredecessorApprovedCatalog)
	require.Equal(t, "minimax-m2.7", stub.successorCommand.Mutation.StableRequestModel)
	require.Equal(t, "minimax-m2.7-20260903", stub.successorCommand.Mutation.SuccessorModelName)
}

func TestActivateModelSuccessorRejectsUnknownAndLaterCutoverShapes(t *testing.T) {
	for _, body := range []string{
		`{"expected_release_version":2,"unknown":true}`,
		`{"expected_release_version":2,"stable_request_model":"glm-5.2","predecessor_public_model_id":11,"predecessor_model_name":"glm-old","expected_predecessor_lifecycle":"ga","expected_predecessor_approved_catalog":true,"successor_public_model_id":12,"successor_model_name":"glm-5.2-20260902","expected_successor_lifecycle":"staging","expected_successor_approved_catalog":false,"expected_model_sets":[]}`,
	} {
		stub := &deploymentLifecycleApplicationStub{}
		handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
		handler.deploymentLifecycleManager = stub
		request := canonicalLifecycleHandlerRequest(t, http.MethodPost,
			"/api/v1/model-deployment-releases/rcr_glm52_successor/activate-successor", body, true)
		request.SetPathValue("releaseRef", "rcr_glm52_successor")
		response := httptest.NewRecorder()
		middleware.OperationIdentityWithSecret("hash-secret")(http.HandlerFunc(handler.ActivateModelSuccessor)).ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "model_successor_cutover_invalid")
		require.Zero(t, stub.successorCommand.CustomerOrgID)
	}
}
