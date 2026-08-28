package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestRegisterModelDeploymentRoutesEnforcesRoleAndGatewayLicenseBoundaries(t *testing.T) {
	const secret = "canonical-model-deployment-route-secret"
	database := activeAdminPrincipalDB()
	handler := admin.NewHandler(database, secret, []byte("0123456789abcdef01234567"), nil)
	validLicense := canonicalDeploymentGatewayLicenseRuntime(t)

	organizationRoute := func(next http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(database, secret)(middleware.RequireOrganizationRoles(roles...)(next))
	}
	readRoute := func(next http.HandlerFunc) http.Handler {
		return organizationRoute(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer)
	}
	licensedWriteRoute := func(runtime *licensing.Runtime) modelDeploymentRouteGuard {
		return func(next http.HandlerFunc) http.Handler {
			gated := runtime.Gate(licensing.FeatureGateway)(next)
			return organizationRoute(http.HandlerFunc(gated.ServeHTTP), auth.RolePlatformAdmin, auth.RoleOrgAdmin)
		}
	}
	licensedApprovalRoute := func(runtime *licensing.Runtime) modelDeploymentRouteGuard {
		return func(next http.HandlerFunc) http.Handler {
			gated := runtime.Gate(licensing.FeatureGateway)(next)
			return organizationRoute(http.HandlerFunc(gated.ServeHTTP), auth.RolePlatformAdmin)
		}
	}
	safetyRoute := func(next http.HandlerFunc) http.Handler {
		return organizationRoute(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	}

	serve := func(mux *http.ServeMux, method, target, body, role string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+activeAdminToken(t, 7, 11, role, secret))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}

	validMux := http.NewServeMux()
	registerModelDeploymentRoutes(validMux, handler, modelDeploymentRouteGuards{
		read: readRoute, write: licensedWriteRoute(validLicense),
		approve: licensedApprovalRoute(validLicense), safety: safetyRoute,
	})
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer} {
		response := serve(validMux, http.MethodGet, "/api/v1/model-deployments", "", role)
		require.NotEqual(t, http.StatusForbidden, response.Code, "read role=%s body=%s", role, response.Body.String())
	}
	for _, role := range []string{auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer} {
		require.Equal(t, http.StatusForbidden, serve(validMux, http.MethodGet, "/api/v1/model-deployments", "", role).Code, "read role=%s", role)
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin} {
		response := serve(validMux, http.MethodPost, "/api/v1/model-deployments", "not-json", role)
		require.Equal(t, http.StatusBadRequest, response.Code, "licensed write role=%s body=%s", role, response.Body.String())
		require.Contains(t, response.Body.String(), "canonical_application_idempotency_required", "role=%s", role)
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin} {
		response := serve(
			validMux, http.MethodPost,
			"/api/v1/model-deployments/1/operations/openai.chat_completions.v1/probes",
			"not-json", role,
		)
		require.Equal(t, http.StatusBadRequest, response.Code, "licensed probe role=%s body=%s", role, response.Body.String())
		require.Contains(t, response.Body.String(), "canonical_operation_probe_invalid", "role=%s", role)
	}
	viewerWrite := serve(validMux, http.MethodPost, "/api/v1/model-deployments", "not-json", auth.RoleOrgViewer)
	require.Equal(t, http.StatusForbidden, viewerWrite.Code)
	require.NotContains(t, viewerWrite.Body.String(), "license_error", "RBAC must reject before the License gate")
	viewerProbe := serve(
		validMux, http.MethodPost,
		"/api/v1/model-deployments/1/operations/openai.chat_completions.v1/probes",
		"not-json", auth.RoleOrgViewer,
	)
	require.Equal(t, http.StatusForbidden, viewerProbe.Code)
	require.NotContains(t, viewerProbe.Body.String(), "license_error", "probe RBAC must reject before the License gate")
	approvalPath := "/api/v1/model-deployments/1/operations/openai.responses.v1/production-approvals"
	platformApproval := serve(validMux, http.MethodPost, approvalPath, "not-json", auth.RolePlatformAdmin)
	require.Equal(t, http.StatusBadRequest, platformApproval.Code, platformApproval.Body.String())
	require.Contains(t, platformApproval.Body.String(), "model_operation_production_approval_invalid")
	orgApproval := serve(validMux, http.MethodPost, approvalPath, "not-json", auth.RoleOrgAdmin)
	require.Equal(t, http.StatusForbidden, orgApproval.Code)
	require.NotContains(t, orgApproval.Body.String(), "license_error", "approval RBAC must reject before the License gate")
	capabilityBase := "/api/v1/model-deployments/1/operations/openai.chat_completions.v1/capabilities/streaming"
	capabilityRead := serve(validMux, http.MethodGet, capabilityBase, "", auth.RoleOrgViewer)
	require.NotEqual(t, http.StatusNotFound, capabilityRead.Code, capabilityRead.Body.String())
	capabilityProbe := serve(validMux, http.MethodPost, capabilityBase+"/probes", "not-json", auth.RoleOrgAdmin)
	require.Equal(t, http.StatusBadRequest, capabilityProbe.Code, capabilityProbe.Body.String())
	require.Contains(t, capabilityProbe.Body.String(), "model_capability_qualification_invalid")
	capabilityApproval := serve(validMux, http.MethodPost, capabilityBase+"/routing-approvals", "not-json", auth.RolePlatformAdmin)
	require.Equal(t, http.StatusBadRequest, capabilityApproval.Code, capabilityApproval.Body.String())
	require.Contains(t, capabilityApproval.Body.String(), "model_capability_qualification_invalid")
	require.Equal(t, http.StatusForbidden,
		serve(validMux, http.MethodPost, capabilityBase+"/routing-approvals", "not-json", auth.RoleOrgAdmin).Code)

	missingLicenseMux := http.NewServeMux()
	registerModelDeploymentRoutes(missingLicenseMux, handler, modelDeploymentRouteGuards{
		read: readRoute, write: licensedWriteRoute(licensing.NewRuntime(nil)),
		approve: licensedApprovalRoute(licensing.NewRuntime(nil)), safety: safetyRoute,
	})
	readWithoutLicense := serve(missingLicenseMux, http.MethodGet, "/api/v1/model-deployments", "", auth.RoleOrgViewer)
	require.NotEqual(t, http.StatusForbidden, readWithoutLicense.Code, readWithoutLicense.Body.String())
	writeWithoutLicense := serve(missingLicenseMux, http.MethodPost, "/api/v1/model-deployments", "not-json", auth.RoleOrgAdmin)
	require.Equal(t, http.StatusForbidden, writeWithoutLicense.Code)
	require.Contains(t, writeWithoutLicense.Body.String(), "license_error.missing")
	probeWithoutLicense := serve(
		missingLicenseMux, http.MethodPost,
		"/api/v1/model-deployments/1/operations/openai.chat_completions.v1/probes",
		"not-json", auth.RoleOrgAdmin,
	)
	require.Equal(t, http.StatusForbidden, probeWithoutLicense.Code)
	require.Contains(t, probeWithoutLicense.Body.String(), "license_error.missing")
	approvalWithoutLicense := serve(missingLicenseMux, http.MethodPost, approvalPath, "not-json", auth.RolePlatformAdmin)
	require.Equal(t, http.StatusForbidden, approvalWithoutLicense.Code)
	require.Contains(t, approvalWithoutLicense.Body.String(), "license_error.missing")
	capabilityReadWithoutLicense := serve(missingLicenseMux, http.MethodGet, capabilityBase, "", auth.RoleOrgViewer)
	require.NotEqual(t, http.StatusForbidden, capabilityReadWithoutLicense.Code, capabilityReadWithoutLicense.Body.String())
	capabilityProbeWithoutLicense := serve(missingLicenseMux, http.MethodPost, capabilityBase+"/probes", "not-json", auth.RoleOrgAdmin)
	require.Equal(t, http.StatusForbidden, capabilityProbeWithoutLicense.Code)
	require.Contains(t, capabilityProbeWithoutLicense.Body.String(), "license_error.missing")
	capabilityApprovalWithoutLicense := serve(missingLicenseMux, http.MethodPost, capabilityBase+"/routing-approvals", "not-json", auth.RolePlatformAdmin)
	require.Equal(t, http.StatusForbidden, capabilityApprovalWithoutLicense.Code)
	require.Contains(t, capabilityApprovalWithoutLicense.Body.String(), "license_error.missing")
	for _, safetyRequest := range []struct {
		path string
		body string
	}{
		{path: "/api/v1/provider-connections/1/pause", body: `{"expected_config_revision":1}`},
		{path: "/api/v1/provider-credential-bindings/1/revoke", body: `{"expected_state_revision":1}`},
	} {
		response := serve(missingLicenseMux, http.MethodPost, safetyRequest.path, safetyRequest.body, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), "model_route_safety_unavailable")
		require.NotContains(t, response.Body.String(), "license_error", "safe contraction must remain available without a License")
		viewerResponse := serve(missingLicenseMux, http.MethodPost, safetyRequest.path, safetyRequest.body, auth.RoleOrgViewer)
		require.Equal(t, http.StatusForbidden, viewerResponse.Code)
		require.NotContains(t, viewerResponse.Body.String(), "model_route_safety_unavailable", "RBAC must reject before the handler")
	}

	detail := serve(
		validMux, http.MethodGet,
		"/api/v1/model-deployments/1?operation=openai.chat_completions.v1", "", auth.RoleOrgViewer,
	)
	require.NotEqual(t, http.StatusNotFound, detail.Code, "detail route must be registered")

	lifecycleRead := serve(
		missingLicenseMux, http.MethodGet,
		"/api/v1/model-deployment-releases/rcr_route-one", "", auth.RoleOrgViewer,
	)
	require.Equal(t, http.StatusServiceUnavailable, lifecycleRead.Code, lifecycleRead.Body.String())
	require.Contains(t, lifecycleRead.Body.String(), "canonical_deployment_lifecycle_unavailable")
	require.NotContains(t, lifecycleRead.Body.String(), "license_error", "lifecycle truth read must not require a License")
	collectionRead := serve(
		missingLicenseMux, http.MethodGet,
		"/api/v1/model-deployment-releases", "", auth.RoleOrgViewer,
	)
	require.Equal(t, http.StatusServiceUnavailable, collectionRead.Code, collectionRead.Body.String())
	require.Contains(t, collectionRead.Body.String(), "canonical_deployment_release_authoring_unavailable")
	require.NotContains(t, collectionRead.Body.String(), "license_error", "release collection truth read must not require a License")

	for _, path := range []string{
		"/api/v1/model-deployment-releases",
		"/api/v1/model-deployment-releases/rcr_route-one/activate-initial",
		"/api/v1/model-deployment-releases/rcr_route-one/start-canary",
		"/api/v1/model-deployment-releases/rcr_route-one/promote",
		"/api/v1/model-deployment-releases/rcr_route-one/rollback-stable",
		"/api/v1/model-deployment-releases/rcr_route-one/cutover-legacy",
	} {
		licensed := serve(validMux, http.MethodPost, path, `{}`, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusBadRequest, licensed.Code, "path=%s body=%s", path, licensed.Body.String())
		require.Contains(t, licensed.Body.String(), "idempotency_required")
		unlicensed := serve(missingLicenseMux, http.MethodPost, path, `{}`, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusForbidden, unlicensed.Code, "path=%s body=%s", path, unlicensed.Body.String())
		require.Contains(t, unlicensed.Body.String(), "license_error.missing")
		viewer := serve(missingLicenseMux, http.MethodPost, path, `{}`, auth.RoleOrgViewer)
		require.Equal(t, http.StatusForbidden, viewer.Code, "path=%s body=%s", path, viewer.Body.String())
		require.NotContains(t, viewer.Body.String(), "license_error", "RBAC must reject before lifecycle License admission")
	}

	cancelPath := "/api/v1/model-deployment-releases/rcr_route-one/cancel-canary"
	unlicensedCancel := serve(missingLicenseMux, http.MethodPost, cancelPath, `{}`, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusBadRequest, unlicensedCancel.Code, unlicensedCancel.Body.String())
	require.Contains(t, unlicensedCancel.Body.String(), "canonical_deployment_lifecycle_idempotency_required")
	require.NotContains(t, unlicensedCancel.Body.String(), "license_error", "safe contraction must remain available without a License")
	viewerCancel := serve(missingLicenseMux, http.MethodPost, cancelPath, `{}`, auth.RoleOrgViewer)
	require.Equal(t, http.StatusForbidden, viewerCancel.Code, viewerCancel.Body.String())
	require.NotContains(t, viewerCancel.Body.String(), "canonical_deployment_lifecycle_idempotency_required", "RBAC must reject before the lifecycle handler")
	legacyRollbackPath := "/api/v1/model-deployment-releases/rcr_route-one/rollback-legacy"
	unlicensedLegacyRollback := serve(missingLicenseMux, http.MethodPost, legacyRollbackPath, `{}`, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusBadRequest, unlicensedLegacyRollback.Code, unlicensedLegacyRollback.Body.String())
	require.Contains(t, unlicensedLegacyRollback.Body.String(), "canonical_deployment_lifecycle_idempotency_required")
	require.NotContains(t, unlicensedLegacyRollback.Body.String(), "license_error", "legacy rollback must remain available without a License")
	viewerLegacyRollback := serve(missingLicenseMux, http.MethodPost, legacyRollbackPath, `{}`, auth.RoleOrgViewer)
	require.Equal(t, http.StatusForbidden, viewerLegacyRollback.Code, viewerLegacyRollback.Body.String())
	require.NotContains(t, viewerLegacyRollback.Body.String(), "canonical_deployment_lifecycle_idempotency_required", "RBAC must reject before legacy rollback handler")

	unauthenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/model-deployment-releases/rcr_route-one/promote", strings.NewReader(`{}`))
	missingLicenseMux.ServeHTTP(unauthenticated, request)
	require.Equal(t, http.StatusUnauthorized, unauthenticated.Code, unauthenticated.Body.String())
	require.NotContains(t, unauthenticated.Body.String(), "license_error", "authentication must reject before License admission")
}

func TestOpenAPIDocumentsCanonicalModelDeploymentIdempotencyContract(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	require.NoError(t, err)
	spec := string(raw)
	for _, fragment := range []string{
		"  /api/v1/model-deployments:\n    x-unirouter-rbac:\n      get: [platform_admin, org_admin, org_viewer]\n      post: [platform_admin, org_admin]",
		"        - name: Idempotency-Key\n          in: header\n          required: true",
		"Canonical JSON object limited to 65536 bytes; duplicate members, arrays and scalar roots are rejected. The complete request body is limited to 131072 bytes.",
		"X-UniRouter-Idempotency-Replayed:",
		"X-UniRouter-Original-Request-ID:",
		"using the same key with another normalized payload returns 409",
		"canonical_application_https_origin_required",
		"Responses and Messages require an exact path-free HTTPS origin",
		"  /api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes:",
		"The server resolves only the supplied deployment ID, operation ID and credential binding ID",
		"Only a successful response whose connection, deployment, operation, profile, credential and binding-link identity remain current can advance operation evidence to live_probed.",
		"required: [probe_id, deployment_id, operation_id, credential_binding_id, success, provider_success, current, stale, operation_advanced, reason_code, provider_reason_code, status_code, latency_ms]",
		"Stable sanitized provider classification retained even when the final result is stale.",
	} {
		require.Contains(t, spec, fragment)
	}
}

func TestCanonicalDeploymentLifecycleContractParityAcrossOpenAPIAndDocs(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	require.NoError(t, err)
	spec := string(raw)
	pathSection := func(path string) string {
		t.Helper()
		start := strings.Index(spec, "  "+path+":")
		require.NotEqual(t, -1, start, "missing OpenAPI path %s", path)
		section := spec[start:]
		if next := strings.Index(section[3:], "\n  /api/v1/"); next >= 0 {
			section = section[:next+3]
		}
		return section
	}

	collectionPath := "/api/v1/model-deployment-releases"
	collectionSection := pathSection(collectionPath)
	require.Contains(t, collectionSection, "get: [platform_admin, org_admin, org_viewer]")
	require.Contains(t, collectionSection, "post: [platform_admin, org_admin]")
	require.Contains(t, collectionSection, "License-independent Repeatable Read projection")
	require.Contains(t, collectionSection, "ModelDeploymentReleaseAuthoringIdempotencyKey")
	require.Contains(t, collectionSection, `"201":`)
	require.Contains(t, collectionSection, "Location:")
	require.Contains(t, collectionSection, "Authoring ends at validated and never activates")

	readPath := collectionPath + "/{releaseRef}"
	readSection := pathSection(readPath)
	require.Contains(t, readSection, "get: [platform_admin, org_admin, org_viewer]")
	require.Contains(t, readSection, "does not require a Gateway License")
	require.Contains(t, readSection, "only cutover_legacy and rollback_legacy")
	require.Contains(t, readSection, `"401": { $ref: "#/components/responses/Error" }`)

	writes := []struct {
		path               string
		licenseDescription string
	}{
		{readPath + "/activate-initial", "Expansion requiring the Gateway License"},
		{readPath + "/start-canary", "Expansion requiring the Gateway License"},
		{readPath + "/promote", "Expansion requiring the Gateway License"},
		{readPath + "/cancel-canary", "Safety contraction available without a Gateway License"},
		{readPath + "/rollback-stable", "Expansion requiring the Gateway License"},
		{readPath + "/cutover-legacy", "Expansion requiring the Gateway License"},
		{readPath + "/rollback-legacy", "Emergency safety contraction available without a Gateway License"},
	}
	for _, write := range writes {
		section := pathSection(write.path)
		require.Contains(t, section, "post: [platform_admin, org_admin]", "path=%s", write.path)
		require.Contains(t, section, "ModelDeploymentLifecycleIdempotencyKey", "path=%s", write.path)
		require.Contains(t, section, write.licenseDescription, "path=%s", write.path)
		require.Contains(t, section, "responses:", "path=%s", write.path)
	}
	for _, fragment := range []string{
		"strict JSON object limited to 16384 bytes",
		"Durable, non-expiring lifecycle mutation identity scoped to the customer organization, actor and lifecycle action.",
		"The same key may be used independently for another lifecycle action.",
		"X-UniRouter-Idempotency-Replayed:",
		"X-UniRouter-Original-Request-ID:",
		"ModelDeploymentReleaseCollectionView:",
		"ModelDeploymentReleaseAuthoringReceipt:",
		"original_http_status: { type: integer, enum: [201] }",
		"ModelDeploymentLifecycleView:",
		"ModelDeploymentLifecycleReceipt:",
		"assigned_public_model_id:",
		"stable_parent:",
		"cutover_legacy, rollback_legacy",
		"public_execution_binding_id: { type: integer, format: int64, minimum: 1 }",
		"delivery_contract_version: { type: string, enum: [unirouter.public-nonstream-delivery.v1] }",
		"charge_contract_version: { type: string, enum: [unirouter.provider-final-token-charge.v1] }",
		"idempotency_contract_version: { type: string, enum: [unirouter.public-at-most-once.v1] }",
	} {
		require.Contains(t, spec, fragment)
	}
	lifecycleKeyStart := strings.Index(spec, "    ModelDeploymentLifecycleIdempotencyKey:")
	require.NotEqual(t, -1, lifecycleKeyStart)
	lifecycleKeySection := spec[lifecycleKeyStart:]
	if next := strings.Index(lifecycleKeySection[5:], "\n    "); next >= 0 {
		lifecycleKeySection = lifecycleKeySection[:next+5]
	}
	require.NotContains(t, lifecycleKeySection, "24-hour")

	backendDocs, err := os.ReadFile("main.go")
	require.NoError(t, err)
	frontendDocs, err := os.ReadFile("../../frontend/src/pages/ApiDocsPage.tsx")
	require.NoError(t, err)
	frontendClient, err := os.ReadFile("../../frontend/src/api/modelDeploymentLifecycle.ts")
	require.NoError(t, err)
	paths := []string{collectionPath, readPath}
	for _, write := range writes {
		paths = append(paths, write.path)
	}
	for _, path := range paths {
		require.Contains(t, string(backendDocs), path, "backend docs path=%s", path)
		require.Contains(t, string(frontendDocs), path, "frontend docs path=%s", path)
	}
	for _, actionPath := range []string{
		"activate-initial", "start-canary", "promote", "cancel-canary", "rollback-stable",
		"cutover-legacy", "rollback-legacy",
	} {
		require.Contains(t, string(frontendClient), "'"+actionPath+"'")
	}
}

func TestOpenAPIDocumentsModelRouteSafetyUnavailableContract(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	require.NoError(t, err)
	spec := string(raw)
	for _, path := range []string{
		"/api/v1/provider-connections/{connectionID}/pause",
		"/api/v1/provider-credential-bindings/{bindingID}/revoke",
	} {
		start := strings.Index(spec, "  "+path+":")
		require.NotEqual(t, -1, start, "missing OpenAPI path %s", path)
		section := spec[start:]
		if next := strings.Index(section[3:], "\n  /api/v1/"); next >= 0 {
			section = section[:next+3]
		}
		require.Contains(t, section, `"503": { $ref: "#/components/responses/Error" }`, "path=%s", path)
	}
}

func canonicalDeploymentGatewayLicenseRuntime(t *testing.T) *licensing.Runtime {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	verifier := licensing.NewCommercialVerifier(
		map[string]ed25519.PublicKey{"canonical-deployment-route-key": publicKey}, nil, 30, "canonical-deployment-route",
	)
	runtime := licensing.NewRuntime(verifier)
	now := time.Now().UTC()
	document, err := licensing.SignCommercialPayload(licensing.Payload{
		LicenseID: "canonical-deployment-route-license", Customer: "Canonical Route Test",
		DeploymentID: "canonical-deployment-route", Tier: "enterprise",
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Features: []string{licensing.FeatureGateway},
	}, "canonical-deployment-route-key", privateKey)
	require.NoError(t, err)
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	status := runtime.Replace(raw)
	require.True(t, status.Valid && status.Enforceable, "test license must be enforceable: %#v", status)
	return runtime
}
