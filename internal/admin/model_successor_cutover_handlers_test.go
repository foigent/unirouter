package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

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
