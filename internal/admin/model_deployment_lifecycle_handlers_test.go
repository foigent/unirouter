package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

type deploymentLifecycleApplicationStub struct {
	state            runtimechange.DeploymentLifecycleState
	options          runtimechange.DeploymentReleaseAuthoringOptions
	inspectErr       error
	listErr          error
	authorReceipt    runtimechange.DeploymentReleaseAuthoringReceipt
	authorErr        error
	receipt          runtimechange.DeploymentLifecycleReceipt
	successorReceipt runtimechange.ModelSuccessorCutoverReceipt
	applyErr         error
	successorErr     error
	inspectCalls     int
	applyCalls       int
	inspectRef       string
	inspectOrgID     int64
	command          runtimechange.DeploymentLifecycleCommand
	successorCommand runtimechange.ModelSuccessorCutoverCommand
	authorCommand    runtimechange.DeploymentReleaseAuthoringCommand
	listCalls        int
	authorCalls      int
}

func (stub *deploymentLifecycleApplicationStub) ApplyModelSuccessorCutover(
	_ context.Context,
	command runtimechange.ModelSuccessorCutoverCommand,
) (runtimechange.ModelSuccessorCutoverReceipt, error) {
	stub.successorCommand = command
	receipt := stub.successorReceipt
	if receipt.Lifecycle.ServerOperationID == "" {
		receipt.Lifecycle.ServerOperationID = command.ServerOperationID
	}
	return receipt, stub.successorErr
}

func (stub *deploymentLifecycleApplicationStub) InspectDeploymentLifecycle(
	_ context.Context,
	releaseRef string,
	orgID int64,
) (runtimechange.DeploymentLifecycleState, error) {
	stub.inspectCalls++
	stub.inspectRef, stub.inspectOrgID = releaseRef, orgID
	return stub.state, stub.inspectErr
}

func (stub *deploymentLifecycleApplicationStub) ListDeploymentReleaseAuthoringOptions(
	_ context.Context,
	_ int64,
) (runtimechange.DeploymentReleaseAuthoringOptions, error) {
	stub.listCalls++
	return stub.options, stub.listErr
}

func (stub *deploymentLifecycleApplicationStub) AuthorSealedDeploymentRelease(
	_ context.Context,
	command runtimechange.DeploymentReleaseAuthoringCommand,
) (runtimechange.DeploymentReleaseAuthoringReceipt, error) {
	stub.authorCalls++
	stub.authorCommand = command
	receipt := stub.authorReceipt
	if receipt.ServerOperationID == "" {
		receipt.ServerOperationID = command.ServerOperationID
	}
	return receipt, stub.authorErr
}

func (stub *deploymentLifecycleApplicationStub) ApplyDeploymentLifecycle(
	_ context.Context,
	command runtimechange.DeploymentLifecycleCommand,
) (runtimechange.DeploymentLifecycleReceipt, error) {
	stub.applyCalls++
	stub.command = command
	receipt := stub.receipt
	if receipt.ServerOperationID == "" {
		receipt.ServerOperationID = command.ServerOperationID
	}
	return receipt, stub.applyErr
}

func TestCanonicalDeploymentLifecycleHandlersUseSoleApplicationExecutorForEveryAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action runtimechange.DeploymentLifecycleAction
		body   string
		serve  func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{
			name: "activate initial", action: runtimechange.DeploymentLifecycleActivateInitial,
			body:  `{"expected_release_version":2}`,
			serve: (*Handler).ActivateInitialCanonicalDeploymentLifecycle,
		},
		{
			name: "start canary", action: runtimechange.DeploymentLifecycleStartCanary,
			body:  `{"expected_release_version":2,"expected_slot_version":4,"canary_basis_points":500}`,
			serve: (*Handler).StartCanonicalDeploymentCanary,
		},
		{
			name: "promote", action: runtimechange.DeploymentLifecyclePromote,
			body:  `{"expected_release_version":3,"expected_slot_version":5}`,
			serve: (*Handler).PromoteCanonicalDeploymentCanary,
		},
		{
			name: "cancel canary", action: runtimechange.DeploymentLifecycleCancelCanary,
			body:  `{"expected_release_version":3,"expected_slot_version":5,"reason":" safety contraction "}`,
			serve: (*Handler).CancelCanonicalDeploymentCanary,
		},
		{
			name: "rollback stable", action: runtimechange.DeploymentLifecycleRollbackStable,
			body:  `{"expected_release_version":4,"expected_slot_version":6}`,
			serve: (*Handler).RollbackCanonicalDeploymentStable,
		},
		{
			name: "cutover legacy", action: runtimechange.DeploymentLifecycleCutoverLegacy,
			body:  `{"expected_release_version":2,"expected_slot_version":7,"reason":"verified parity cutover"}`,
			serve: (*Handler).CutoverLegacyCanonicalDeploymentLifecycle,
		},
		{
			name: "rollback legacy", action: runtimechange.DeploymentLifecycleRollbackLegacy,
			body:  `{"expected_release_version":3,"expected_slot_version":8,"reason":"safety rollback"}`,
			serve: (*Handler).RollbackLegacyCanonicalDeploymentLifecycle,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &deploymentLifecycleApplicationStub{receipt: runtimechange.DeploymentLifecycleReceipt{
				ContractVersion: runtimechange.DeploymentLifecycleReceiptContractVersion,
				ReceiptID:       91, Action: test.action, TargetReleaseRef: "rcr_http-one",
				OriginalHTTPStatus: http.StatusOK,
			}}
			handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
			handler.deploymentLifecycleManager = stub
			request := canonicalLifecycleHandlerRequest(t, http.MethodPost,
				"/api/v1/model-deployment-releases/rcr_http-one", test.body, true)
			request.SetPathValue("releaseRef", "RCR_HTTP-ONE")
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret("hash-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				test.serve(handler, w, r)
			})).ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, 1, stub.applyCalls)
			require.Equal(t, test.action, stub.command.Mutation.Action)
			require.Equal(t, "rcr_http-one", stub.command.Mutation.ReleaseRef)
			require.Equal(t, int64(17), stub.command.CustomerOrgID)
			require.Equal(t, int64(23), stub.command.ActorUserID)
			require.Equal(t, int64(17), stub.command.ActorOrgID)
			require.Len(t, stub.command.IdempotencyKeyHash, 64)
			require.True(t, strings.HasPrefix(stub.command.RequestFingerprint, "hmac-sha256:"))
			require.Equal(t, response.Header().Get(middleware.OperationIDHeader), stub.command.ServerOperationID)
			require.Equal(t, stub.command.ServerOperationID, stub.command.Audit.RequestID)
			require.Equal(t, "admin_console", stub.command.Audit.Source)
			require.Equal(t, "unit-client-request", stub.command.Audit.ClientRequestID)
		})
	}
}

func TestCanonicalDeploymentLifecycleHandlerReturnsFrozenReplayStatusAndHeaders(t *testing.T) {
	t.Parallel()

	stub := &deploymentLifecycleApplicationStub{receipt: runtimechange.DeploymentLifecycleReceipt{
		ContractVersion: runtimechange.DeploymentLifecycleReceiptContractVersion,
		ReceiptID:       77, Action: runtimechange.DeploymentLifecyclePromote,
		TargetReleaseRef: "rcr_replay-one", ServerOperationID: "op_original-frozen",
		OriginalHTTPStatus: http.StatusOK, IdempotencyReplay: true,
	}}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(t, http.MethodPost,
		"/api/v1/model-deployment-releases/rcr_replay-one/promote",
		`{"expected_release_version":3,"expected_slot_version":5}`, true)
	request.SetPathValue("releaseRef", "rcr_replay-one")
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret("hash-secret")(
		http.HandlerFunc(handler.PromoteCanonicalDeploymentCanary),
	).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "true", response.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, "op_original-frozen", response.Header().Get(middleware.OriginalOperationHeader))
	require.NotEqual(t, response.Header().Get(middleware.OperationIDHeader), response.Header().Get(middleware.OriginalOperationHeader))
	var receipt runtimechange.DeploymentLifecycleReceipt
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
	require.True(t, receipt.IdempotencyReplay)
	require.Equal(t, int64(77), receipt.ReceiptID)
}

func TestCanonicalDeploymentLifecycleHandlerRejectsInvalidIngressBeforeExecutor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		withKey    bool
		action     runtimechange.DeploymentLifecycleAction
		wantStatus int
		wantCode   string
	}{
		{name: "missing key", body: `{"expected_release_version":3,"expected_slot_version":5}`, action: runtimechange.DeploymentLifecyclePromote, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_idempotency_required"},
		{name: "unknown member", body: `{"expected_release_version":3,"expected_slot_version":5,"stage":"active"}`, withKey: true, action: runtimechange.DeploymentLifecyclePromote, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "duplicate member", body: `{"expected_release_version":3,"expected_slot_version":5,"expected_slot_version":6}`, withKey: true, action: runtimechange.DeploymentLifecyclePromote, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "nonzero field owned by another action", body: `{"expected_release_version":3,"expected_slot_version":5,"canary_basis_points":100}`, withKey: true, action: runtimechange.DeploymentLifecyclePromote, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "zero field owned by another action", body: `{"expected_release_version":3,"expected_slot_version":5,"canary_basis_points":0}`, withKey: true, action: runtimechange.DeploymentLifecyclePromote, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "zero slot forbidden for initial action", body: `{"expected_release_version":3,"expected_slot_version":0}`, withKey: true, action: runtimechange.DeploymentLifecycleActivateInitial, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "legacy cutover reason required", body: `{"expected_release_version":2,"expected_slot_version":5}`, withKey: true, action: runtimechange.DeploymentLifecycleCutoverLegacy, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "legacy rollback reason required", body: `{"expected_release_version":3,"expected_slot_version":6,"reason":"   "}`, withKey: true, action: runtimechange.DeploymentLifecycleRollbackLegacy, wantStatus: http.StatusBadRequest, wantCode: "canonical_deployment_lifecycle_invalid"},
		{name: "oversized", body: `{"expected_release_version":3,"expected_slot_version":5,"reason":"` + strings.Repeat("x", canonicalDeploymentLifecycleMaxRequestBytes) + `"}`, withKey: true, action: runtimechange.DeploymentLifecyclePromote, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "canonical_deployment_lifecycle_request_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &deploymentLifecycleApplicationStub{}
			handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
			handler.deploymentLifecycleManager = stub
			target := "/api/v1/model-deployment-releases/rcr_ingress-one/" +
				canonicalDeploymentLifecycleActionPath(test.action)
			request := canonicalLifecycleHandlerRequest(t, http.MethodPost,
				target, test.body, test.withKey)
			request.SetPathValue("releaseRef", "rcr_ingress-one")
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret("hash-secret")(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handler.applyCanonicalDeploymentLifecycle(w, r, test.action)
				}),
			).ServeHTTP(response, request)

			require.Equal(t, test.wantStatus, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), test.wantCode)
			require.Zero(t, stub.applyCalls)
		})
	}
}

func TestGetCanonicalDeploymentLifecycleReturnsRoleAndLicenseAwareServerActions(t *testing.T) {
	t.Parallel()

	release, slot := canonicalLifecycleCanaryFixture()
	stub := &deploymentLifecycleApplicationStub{state: runtimechange.DeploymentLifecycleState{Release: release, Slot: slot}}
	handler := NewHandlerWithAPIKeyHashSecret(nil, "jwt-secret", "hash-secret", nil, nil)
	handler.deploymentLifecycleManager = stub
	request := canonicalLifecycleHandlerRequest(t, http.MethodGet,
		"/api/v1/model-deployment-releases/rcr_canary-fixture", "", false)
	request = request.WithContext(context.WithValue(request.Context(), middleware.CtxOrganizationRole, auth.RoleOrgViewer))
	request.SetPathValue("releaseRef", "rcr_canary-fixture")
	response := httptest.NewRecorder()
	handler.GetCanonicalDeploymentLifecycle(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, stub.inspectCalls)
	require.Equal(t, int64(17), stub.inspectOrgID)
	var view canonicalDeploymentLifecycleView
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	promote := lifecycleActionDescriptor(t, view.Actions, runtimechange.DeploymentLifecyclePromote)
	require.True(t, promote.StructurallyAvailable)
	require.False(t, promote.Authorized)
	require.False(t, promote.Licensed)
	require.False(t, promote.Available)
	cancel := lifecycleActionDescriptor(t, view.Actions, runtimechange.DeploymentLifecycleCancelCanary)
	require.True(t, cancel.Licensed)
	require.False(t, cancel.Available, "viewer cannot mutate even though contraction is unlicensed")
}

func TestCanonicalDeploymentLifecycleHandlerHasNoLegacyWriterCall(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("model_deployment_lifecycle_handlers.go")
	require.NoError(t, err)
	wire := string(source)
	require.Equal(t, 1, strings.Count(wire, ".ApplyDeploymentLifecycle("))
	for _, forbidden := range []string{
		".ActivateInitialDeploymentStable(",
		".StartDeploymentCanary(",
		".PromoteDeploymentCanary(",
		".CancelDeploymentCanary(",
		".RollbackDeploymentRelease(",
	} {
		require.NotContains(t, wire, forbidden)
	}
}

func canonicalLifecycleHandlerRequest(
	t *testing.T,
	method string,
	target string,
	body string,
	withIdempotencyKey bool,
) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.ClientRequestIDHeader, "unit-client-request")
	if withIdempotencyKey {
		request.Header.Set(middleware.IdempotencyKeyHeader, "unit-lifecycle-key")
	}
	ctx := context.WithValue(request.Context(), middleware.CtxUserID, int64(23))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(17))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, auth.RoleOrgAdmin)
	return request.WithContext(ctx)
}
