package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

func TestCanonicalDeploymentReleaseAuthoringHandlerUsesAtomicWriterAndFrozenHTTPIdentity(t *testing.T) {
	t.Parallel()

	stub := &deploymentLifecycleApplicationStub{authorReceipt: deploymentReleaseAuthoringHTTPReceipt(false)}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(
		t, http.MethodPost, "/api/v1/model-deployment-releases",
		`{"public_model_id":7,"baseline":{"kind":"initial","expected_slot_version":0},"selections":[{"deployment_id":19,"operation_id":"openai.chat_completions.v1"}],"reason":" first candidate "}`,
		true,
	)
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret("hash-secret")(
		http.HandlerFunc(handler.AuthorCanonicalDeploymentRelease),
	).ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	require.Equal(t, "/api/v1/model-deployment-releases/rcr_authored-one", response.Header().Get("Location"))
	require.Equal(t, 1, stub.authorCalls)
	require.Equal(t, int64(7), stub.authorCommand.Mutation.PublicModelID)
	require.Equal(t, runtimechange.DeploymentReleaseBaselineInitial, stub.authorCommand.Mutation.Baseline.Kind)
	require.Equal(t, "first candidate", stub.authorCommand.Mutation.Reason)
	require.Equal(t, int64(17), stub.authorCommand.CustomerOrgID)
	require.Equal(t, int64(23), stub.authorCommand.ActorUserID)
	require.Equal(t, int64(17), stub.authorCommand.ActorOrgID)
	require.Len(t, stub.authorCommand.IdempotencyKeyHash, 64)
	require.True(t, strings.HasPrefix(stub.authorCommand.RequestFingerprint, "hmac-sha256:"))
	require.Equal(t, response.Header().Get(middleware.OperationIDHeader), stub.authorCommand.ServerOperationID)
	require.Empty(t, response.Header().Get(middleware.IdempotencyReplayHeader))
}

func TestCanonicalDeploymentReleaseAuthoringHandlerReturnsLaterReplayAs200(t *testing.T) {
	t.Parallel()

	receipt := deploymentReleaseAuthoringHTTPReceipt(true)
	receipt.ServerOperationID = "op_original-authoring"
	stub := &deploymentLifecycleApplicationStub{authorReceipt: receipt}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(
		t, http.MethodPost, "/api/v1/model-deployment-releases",
		`{"public_model_id":7,"baseline":{"kind":"initial","expected_slot_version":0},"selections":[{"deployment_id":19,"operation_id":"openai.chat_completions.v1"}],"reason":"first candidate"}`,
		true,
	)
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret("hash-secret")(
		http.HandlerFunc(handler.AuthorCanonicalDeploymentRelease),
	).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "true", response.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, receipt.ServerOperationID, response.Header().Get(middleware.OriginalOperationHeader))
	require.Equal(t, receipt.OriginalLocation, response.Header().Get("Location"))
	var decoded runtimechange.DeploymentReleaseAuthoringReceipt
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
	require.Equal(t, http.StatusCreated, decoded.OriginalHTTPStatus)
	require.True(t, decoded.IdempotencyReplay)
}

func TestCanonicalDeploymentReleaseAuthoringHTTPPreservesDataPins(t *testing.T) {
	t.Parallel()
	stub := &deploymentLifecycleApplicationStub{authorReceipt: deploymentReleaseAuthoringHTTPReceipt(false)}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	body := `{"public_model_id":7,"baseline":{"kind":"initial"},"selections":[{"deployment_id":19,"operation_id":"openai.chat_completions.v1"}],"reason":"data candidate","profile_definition_digest":"` + strings.Repeat("a", 64) + `","public_context_limit":64000,"public_output_limit":4096,"capability_approval_ids":[123,119]}`
	request := canonicalLifecycleHandlerRequest(t, http.MethodPost, "/api/v1/model-deployment-releases", body, true)
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret("hash-secret")(http.HandlerFunc(handler.AuthorCanonicalDeploymentRelease)).ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	require.Equal(t, 1, stub.authorCalls)
	require.Equal(t, strings.Repeat("a", 64), stub.authorCommand.Mutation.ProfileDefinitionDigest)
	require.Equal(t, 64000, stub.authorCommand.Mutation.PublicContextLimit)
	require.Equal(t, 4096, stub.authorCommand.Mutation.PublicOutputLimit)
	require.Equal(t, []int64{119, 123}, stub.authorCommand.Mutation.CapabilityApprovalIDs)
}

func TestCanonicalDeploymentReleaseAuthoringDataHTTPRejectsMalformedPins(t *testing.T) {
	t.Parallel()
	valid := `{"public_model_id":7,"baseline":{"kind":"initial"},"selections":[{"deployment_id":19,"operation_id":"openai.chat_completions.v1"}],"reason":"data candidate","profile_definition_digest":"` + strings.Repeat("a", 64) + `","public_context_limit":64000,"public_output_limit":4096,"capability_approval_ids":[119]}`
	for name, body := range map[string]string{
		"unknown field":        strings.Replace(valid, `"reason":`, `"execution_snapshot_digest":"owned","reason":`, 1),
		"unknown nested field": strings.Replace(valid, `"kind":"initial"`, `"kind":"initial","candidate_hash":"owned"`, 1),
		"duplicate digest":     strings.Replace(valid, `"reason":`, `"profile_definition_digest":"`+strings.Repeat("a", 64)+`","reason":`, 1),
		"bad digest":           strings.Replace(valid, strings.Repeat("a", 64), "not-a-digest", 1),
		"invalid limit":        strings.Replace(valid, `"public_output_limit":4096`, `"public_output_limit":0`, 1),
		"duplicate approval":   strings.Replace(valid, `[119]`, `[119,119]`, 1),
		"invalid approval":     strings.Replace(valid, `[119]`, `[0]`, 1),
		"wrong approval type":  strings.Replace(valid, `[119]`, `["119"]`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			stub := &deploymentLifecycleApplicationStub{}
			handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
			handler.deploymentLifecycleManager = stub
			request := canonicalLifecycleHandlerRequest(t, http.MethodPost, "/api/v1/model-deployment-releases", body, true)
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret("hash-secret")(http.HandlerFunc(handler.AuthorCanonicalDeploymentRelease)).ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "canonical_deployment_release_authoring_invalid")
			require.Zero(t, stub.authorCalls)
		})
	}
}

func TestCanonicalDeploymentReleaseAuthoringRejectsInvalidIngressBeforeWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		withKey    bool
		wantStatus int
		wantCode   string
	}{
		{name: "missing key", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_release_authoring_idempotency_required"},
		{name: "unknown top member", body: `{"public_model_id":7,"baseline":{"kind":"initial"},"selections":[],"reason":"x","release_ref":"rcr_owned"}`, withKey: true, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_release_authoring_invalid"},
		{name: "unknown server field nested", body: `{"public_model_id":7,"baseline":{"kind":"initial","candidate_hash":"owned"},"selections":[{"deployment_id":19,"operation_id":"openai.chat_completions.v1"}],"reason":"x"}`, withKey: true, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_release_authoring_invalid"},
		{name: "duplicate member", body: `{"public_model_id":7,"public_model_id":8,"baseline":{"kind":"initial"},"selections":[],"reason":"x"}`, withKey: true, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_release_authoring_invalid"},
		{name: "oversized", body: `{"public_model_id":7,"baseline":{"kind":"initial"},"selections":[],"reason":"` + strings.Repeat("x", canonicalDeploymentLifecycleMaxRequestBytes) + `"}`, withKey: true, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "canonical_deployment_release_authoring_request_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &deploymentLifecycleApplicationStub{}
			handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
			handler.deploymentLifecycleManager = stub
			request := canonicalLifecycleHandlerRequest(
				t, http.MethodPost, "/api/v1/model-deployment-releases", test.body, test.withKey,
			)
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret("hash-secret")(
				http.HandlerFunc(handler.AuthorCanonicalDeploymentRelease),
			).ServeHTTP(response, request)
			require.Equal(t, test.wantStatus, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), test.wantCode)
			require.Zero(t, stub.authorCalls)
		})
	}
}

func TestCanonicalDeploymentReleaseCollectionReturnsServerOwnedAvailability(t *testing.T) {
	t.Parallel()

	baseline := runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}
	stub := &deploymentLifecycleApplicationStub{options: runtimechange.DeploymentReleaseAuthoringOptions{
		ContractVersion: runtimechange.DeploymentReleaseAuthoringOptionsContractVersion,
		GeneratedAt:     time.Date(2026, time.August, 20, 3, 4, 5, 0, time.UTC),
		Models: []runtimechange.DeploymentReleaseAuthoringOption{{
			PublicModel:           runtimechange.DeploymentReleasePublicModelSummary{ID: 7, ModelName: "model-one", Lifecycle: "ga"},
			StructurallyAvailable: true, ReasonCode: "ready_for_authoring", Baseline: &baseline,
			SelectionOptions: []runtimechange.DeploymentReleaseSelectionOption{{DeploymentID: 19, OperationID: "openai.chat_completions.v1"}},
		}},
	}}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(t, http.MethodGet, "/api/v1/model-deployment-releases", "", false)
	request = request.WithContext(context.WithValue(request.Context(), middleware.CtxOrganizationRole, auth.RoleOrgViewer))
	response := httptest.NewRecorder()
	handler.ListCanonicalDeploymentReleases(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, stub.listCalls)
	var view canonicalDeploymentReleaseCollectionView
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	require.Equal(t, runtimechange.DeploymentReleaseAuthoringOptionsContractVersion, view.ContractVersion)
	require.Len(t, view.Models, 1)
	require.True(t, view.Models[0].Authoring.StructurallyAvailable)
	require.False(t, view.Models[0].Authoring.Authorized)
	require.False(t, view.Models[0].Authoring.Available)
	require.Equal(t, "organization_write_role_required", view.Models[0].Authoring.ReasonCode)
	require.Equal(t, baseline, *view.Models[0].Authoring.Baseline)
}

func deploymentReleaseAuthoringHTTPReceipt(replay bool) runtimechange.DeploymentReleaseAuthoringReceipt {
	return runtimechange.DeploymentReleaseAuthoringReceipt{
		ContractVersion: runtimechange.DeploymentReleaseAuthoringReceiptContractVersion,
		ReceiptID:       78, OriginalHTTPStatus: http.StatusCreated,
		OriginalLocation:  "/api/v1/model-deployment-releases/rcr_authored-one",
		IdempotencyReplay: replay,
		Release:           runtimechange.DeploymentRelease{ReleaseRef: "rcr_authored-one"},
	}
}
