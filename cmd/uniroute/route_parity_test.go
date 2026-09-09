package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

type routeSpec struct {
	Method string
	Path   string
}

func TestAdminRoutesRegisteredForFrontendClient(t *testing.T) {
	production := productionRouteSource(t)

	routes := []string{
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/logout",
		"GET /api/v1/auth/oidc/config",
		"GET /api/v1/auth/oidc/start",
		"GET /api/v1/auth/oidc/callback",
		"GET /api/v1/me",
		"PUT /api/v1/me/password",
		"GET /api/v1/dashboard/stats",
		"GET /api/v1/channels",
		"POST /api/v1/channels",
		"PUT /api/v1/channels/{id}",
		"DELETE /api/v1/channels/{id}",
		"POST /api/v1/channels/{id}/test",
		"GET /api/v1/channels/{id}/provider-models",
		"GET /api/v1/provider-packs",
		"GET /api/v1/keys",
		"POST /api/v1/keys",
		"PUT /api/v1/keys/{id}",
		"POST /api/v1/keys/{id}/disable",
		"GET /api/v1/organizations",
		"GET /api/v1/limit-policies",
		"POST /api/v1/limit-policies",
		"PUT /api/v1/limit-policies/{id}",
		"GET /api/v1/guardrail-policies",
		"POST /api/v1/guardrail-policies",
		"PUT /api/v1/guardrail-policies/{id}",
		"GET /api/v1/projects",
		"POST /api/v1/projects",
		"GET /api/v1/projects/{id}",
		"PUT /api/v1/projects/{id}",
		"GET /api/v1/projects/{id}/model-sets",
		"PUT /api/v1/projects/{id}/model-sets",
		"GET /api/v1/projects/{id}/members",
		"PUT /api/v1/projects/{id}/members/{user_id}",
		"DELETE /api/v1/projects/{id}/members/{user_id}",
		"GET /api/v1/projects/{id}/budget",
		"PUT /api/v1/projects/{id}/budget",
		"GET /api/v1/channel-keys",
		"POST /api/v1/channel-keys",
		"PUT /api/v1/channel-keys/{id}",
		"DELETE /api/v1/channel-keys/{id}",
		"GET /api/v1/wallet",
		"POST /api/v1/wallet/topup",
		"POST /api/v1/wallet/adjust",
		"GET /api/v1/wallet/transactions",
		"GET /api/v1/logs",
		"GET /api/v1/logs/{id}",
		"GET /api/v1/logs/{id}/trace",
		"GET /api/v1/usage/analytics",
		"GET /api/v1/cost-optimization/summary",
		"GET /api/v1/cost-optimization/trend",
		"GET /api/v1/cost-optimization/breakdown",
		"GET /api/v1/cost-optimization/opportunities",
		"GET /api/v1/invocation-optimization/contract",
		"GET /api/v1/invocation-optimization/policies",
		"POST /api/v1/invocation-optimization/policies",
		"GET /api/v1/invocation-optimization/policies/{ref}",
		"GET /api/v1/invocation-optimization/policies/{ref}/readiness",
		"PUT /api/v1/invocation-optimization/policies/{ref}",
		"POST /api/v1/invocation-optimization/policies/{ref}/validate",
		"POST /api/v1/invocation-optimization/policies/{ref}/shadow",
		"POST /api/v1/invocation-optimization/policies/{ref}/canary",
		"POST /api/v1/invocation-optimization/policies/{ref}/rollback",
		"GET /api/v1/internal-status",
		"GET /api/v1/runtime/status",
		"GET /api/v1/error-docs",
		"GET /api/v1/model-prices",
		"POST /api/v1/model-prices",
		"PUT /api/v1/model-prices/{id}",
		"DELETE /api/v1/model-prices/{id}",
		"GET /api/v1/model-execution-profiles",
		"GET /api/v1/model-registry",
		"POST /api/v1/model-registry",
		"PUT /api/v1/model-registry/{id}",
		"GET /api/v1/model-deployments",
		"GET /api/v1/model-deployments/{deploymentID}",
		"POST /api/v1/model-deployments",
		"POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes",
		"GET /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}",
		"POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/probes",
		"POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/routing-approvals",
		"GET /api/v1/model-catalog",
		"GET /api/v1/model-aliases",
		"POST /api/v1/model-aliases",
		"PUT /api/v1/model-aliases/{id}",
		"GET /api/v1/model-sets",
		"POST /api/v1/model-sets",
		"GET /api/v1/model-sets/{id}/items",
		"PUT /api/v1/model-sets/{id}/items",
		"GET /api/v1/model-names",
		"GET /api/v1/org-model-prices",
		"POST /api/v1/org-model-prices",
		"PUT /api/v1/org-model-prices/{id}",
		"DELETE /api/v1/org-model-prices/{id}",
		"GET /api/v1/reconciliation",
		"GET /api/v1/settings",
		"PUT /api/v1/settings/{key}",
		"GET /api/v1/license",
		"PUT /api/v1/license",
		"GET /api/v1/users",
		"POST /api/v1/users",
		"PUT /api/v1/users/{id}",
		"DELETE /api/v1/users/{id}",
		"GET /api/v1/grants",
		"POST /api/v1/grants",
		"GET /api/v1/notification-events",
		"GET /api/v1/notification-deliveries",
		"GET /api/v1/notification-webhooks",
		"POST /api/v1/notification-webhooks",
		"PUT /api/v1/notification-webhooks/{id}",
		"POST /api/v1/notification-webhooks/{id}/verify-scope",
		"GET /api/v1/audit-logs",
		"GET /api/v1/data-exchange/items",
		"POST /api/v1/data-exchange/items",
		"PUT /api/v1/data-exchange/items/{id}",
		"GET /api/v1/data-exchange/exchanges",
		"POST /api/v1/data-exchange/exchanges",
		"GET /api/v1/data-exchange/exchanges/{id}/activity",
		"POST /api/v1/data-exchange/exchanges/{id}/deliveries",
		"POST /api/v1/data-exchange/exchanges/{id}/receipts",
		"POST /api/v1/data-exchange/exchanges/{id}/artifact-delete",
		"GET /api/v1/data-exchange/xinsight/connections/{project_id}",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/install",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/pause",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/resume",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/credentials",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/credentials/activate",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/credentials/{credential_id}/revoke",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/prepare",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/activate",
		"POST /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/cancel",
	}

	for _, route := range routes {
		if !strings.Contains(production, route) {
			t.Fatalf("route required by frontend client is not registered: %s", route)
		}
	}
}

func TestMainRegistersExtractedPricingRoutes(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		"return organizationRoleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)",
		"registerPricingRoutes(mux, adminHandler, pricingRouteGuards{read: pricingReadRoute, write: platformRoute})",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go pricing route wiring missing %q", fragment)
		}
	}
}

func TestMainInjectsProductionPlannerIntoLegacyCutoverLedger(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	const wiring = `runtimechange.NewManagerWithLegacyCutoverVerifier(
		postgres.Pool, router, effectiveConfig.RoutingPolicy.Config, executionPlanner,`
	if !strings.Contains(string(src), wiring) {
		t.Fatalf("production runtime-change manager must receive the server-owned execution planner")
	}
}

func TestProductionRoutesDoNotAuthorizeFromCompatibilityRole(t *testing.T) {
	production := productionRouteSource(t)
	for _, forbidden := range []string{
		"middleware.RequireRoles(",
		"middleware.RequireExactRoles(",
		"roleRoute(",
	} {
		if strings.Contains(production, forbidden) {
			t.Fatalf("production route wiring must not authorize from users.role compatibility shadow: %s", forbidden)
		}
	}
	for _, projectRole := range []string{"auth.RoleProjectAdmin", "auth.RoleDeveloper", "auth.RoleViewer"} {
		if strings.Contains(production, "organizationRoleRoute(h, "+projectRole) {
			t.Fatalf("project role must not be wired into an organization-role route: %s", projectRole)
		}
	}
}

func TestMainRegistersCostOptimizationWithPricingReadAuthority(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	const registration = "registerCostOptimizationRoutes(mux, adminHandler, pricingReadRoute)"
	if !strings.Contains(string(src), registration) {
		t.Fatalf("main.go cost optimization routes must use pricing-read authority: %s", registration)
	}
}

func TestMainRegistersInvocationOptimizationBehindModelGovernanceAndOrganizationAdmin(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		"registerInvocationOptimizationRoutes(",
		"licensedRoute(licensing.FeatureModelGovernance, orgAdminRoute)",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go invocation optimization authority missing %q", fragment)
		}
	}
}

func TestMainOrganizationRouteUsesScopedReadGuard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	const registration = `mux.Handle("GET /api/v1/organizations", usageRoute(adminHandler.ListOrganizations))`
	if !strings.Contains(string(src), registration) {
		t.Fatalf("main.go organization route must use the tenant-scoped read guard: %s", registration)
	}
}

func TestMainKeyMutationRoutesDeferToLiveMembershipAuthorization(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	disableRoute := `mux.Handle("POST /api/v1/keys/{id}/disable", adminRoute(adminHandler.DisableAPIKey))`
	if !strings.Contains(mainSource, disableRoute) {
		t.Fatalf("key disable route must let the handler authorize live membership: %s", disableRoute)
	}
	updateRoute := `mux.Handle("PUT /api/v1/keys/{id}", adminRoute(adminHandler.UpdateAPIKey))`
	if !strings.Contains(mainSource, updateRoute) {
		t.Fatalf("key update route must let the handler authorize live membership: %s", updateRoute)
	}

	spec, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	for _, fragment := range []string{
		"  /api/v1/keys/{id}:\n    x-unirouter-rbac:\n      get: [platform_admin, org_admin, project_admin, project_user, project_viewer]\n      put: [platform_admin, org_admin, project_admin, project_user]",
		"  /api/v1/keys/{id}/disable:\n    x-unirouter-rbac:\n      post: [platform_admin, org_admin, project_admin, project_user]",
		"users.role compatibility shadow is never an authorization source",
	} {
		if !strings.Contains(string(spec), fragment) {
			t.Fatalf("OpenAPI live-membership key boundary missing %q", fragment)
		}
	}
}

func TestModelCatalogRouteAndOpenAPIDocumentTwoAxisAuthorization(t *testing.T) {
	routes, err := os.ReadFile("model_governance_routes.go")
	if err != nil {
		t.Fatalf("read model_governance_routes.go: %v", err)
	}
	const registration = `mux.Handle("GET /api/v1/model-catalog", guards.projectRead(handler.ListApprovedModelCatalog))`
	if !strings.Contains(string(routes), registration) {
		t.Fatalf("model catalog must reach the handler's live-membership authorization: %s", registration)
	}

	spec, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	for _, fragment := range []string{
		"  /api/v1/model-catalog:\n    x-unirouter-rbac:\n      get: [platform_admin, org_admin, org_viewer, project_admin, project_user, project_viewer]",
		"billing_admin is\n      denied model-governance access",
		"organization_role=member must provide project_id",
		"users.role compatibility aliases are never an authorization source",
	} {
		if !strings.Contains(string(spec), fragment) {
			t.Fatalf("model-catalog handler/OpenAPI parity missing %q", fragment)
		}
	}
}

func TestMainDashboardRouteUsesOrganizationScopeGuard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		"dashboardRoute := func(h http.HandlerFunc) http.Handler",
		"auth.RoleOrgViewer",
		`mux.Handle("GET /api/v1/dashboard/stats", dashboardRoute(adminHandler.DashboardStats))`,
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go dashboard scope guard missing %q", fragment)
		}
	}
}

func TestMainRegistersExtractedGlobalControlRoutes(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "registerGlobalControlRoutes(mux, adminHandler") {
		t.Fatal("main.go does not call the extracted global-control route registrar")
	}
}

func TestMainRegistersExtractedModelGovernanceRoutes(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		"registerModelGovernanceRoutes(mux, adminHandler",
		"organizationRead: licensedRoute(licensing.FeatureModelGovernance",
		"organizationWrite: licensedRoute(licensing.FeatureModelGovernance, orgAdminRoute)",
		"projectRead:       licensedRoute(licensing.FeatureModelGovernance, projectReadRoute)",
		"modelSetRead: licensedRoute(licensing.FeatureModelGovernance",
		`mux.Handle("GET /api/v1/projects/{id}/model-sets", licensedRoute(licensing.FeatureModelGovernance, projectReadRoute)(adminHandler.ListProjectModelSets))`,
		`mux.Handle("PUT /api/v1/projects/{id}/model-sets", licensedRoute(licensing.FeatureModelGovernance`,
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go model-governance registrar wiring missing %q", fragment)
		}
	}
}

func TestMainRegistersCanonicalModelDeploymentRoutes(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		"registerModelDeploymentRoutes(mux, adminHandler",
		"return organizationRoleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer)",
		"write:   licensedRoute(licensing.FeatureGateway, orgAdminRoute)",
		"approve: licensedRoute(licensing.FeatureGateway, platformRoute)",
		"safety:  orgAdminRoute",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go canonical model-deployment authority missing %q", fragment)
		}
	}
	routes, err := os.ReadFile("model_deployment_routes.go")
	if err != nil {
		t.Fatalf("read model_deployment_routes.go: %v", err)
	}
	for _, registration := range []string{
		`mux.Handle("GET /api/v1/model-deployments", guards.read(handler.ListCanonicalModelApplications))`,
		`mux.Handle("GET /api/v1/model-deployments/{deploymentID}", guards.read(handler.GetCanonicalModelApplication))`,
		`mux.Handle("POST /api/v1/model-deployments", guards.write(handler.CreateCanonicalModelApplication))`,
		`mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes", guards.write(handler.ProbeCanonicalModelOperation))`,
		`mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/production-approvals", guards.approve(handler.ApproveCanonicalModelOperationProduction))`,
		`mux.Handle("GET /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}", guards.read(handler.GetCanonicalModelCapabilityQualification))`,
		`mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/probes", guards.write(handler.ProbeCanonicalModelCapability))`,
		`mux.Handle("POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/routing-approvals", guards.approve(handler.ApproveCanonicalModelCapabilityRouting))`,
		`mux.Handle("POST /api/v1/provider-connections/{connectionID}/pause", guards.safety(handler.PauseProviderConnection))`,
		`mux.Handle("POST /api/v1/provider-credential-bindings/{bindingID}/revoke", guards.safety(handler.RevokeProviderCredentialBinding))`,
	} {
		if !strings.Contains(string(routes), registration) {
			t.Fatalf("canonical model-deployment route missing %q", registration)
		}
	}
	if strings.Contains(productionRouteSource(t), "/api/v1/model-applications") {
		t.Fatal("production routes must not expose the unpublished model-applications alias")
	}
}

func TestMainRegistersCommercialFeatureGates(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		`mux.HandleFunc("GET /api/v1/auth/oidc/config", adminHandler.OIDCConfig)`,
		`mux.Handle("GET /api/v1/auth/oidc/start", licenseRuntime.Gate(licensing.FeatureOIDC)`,
		`mux.Handle("GET /api/v1/auth/oidc/callback", licenseRuntime.Gate(licensing.FeatureOIDC)`,
		"licenseRuntime.Gate(licensing.FeatureOIDC)",
		"licensedRoute(licensing.FeatureNotifications",
		`mux.Handle("GET /api/v1/internal-status", licensedRoute(licensing.FeatureNotifications, orgAdminRoute)(adminHandler.InternalStatus))`,
		"licensedRoute(licensing.FeatureDataExchange",
		"licensedRoute(licensing.FeatureGuardrails",
		"eventSvc.SetAdmission",
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go commercial feature wiring missing %q", fragment)
		}
	}
}

func TestPublicMediaRoutesOnlySubmitExecutionWork(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, registration := range []string{
		`mux.Handle("POST /v1/media/jobs", middleware.GatewayAuthAcrossBufferedBody(mediaAuth, mediaPayloadGuard)(captureExecutionContext(gatewayLicenseGate(http.HandlerFunc(mediaHandler.Submit)))))`,
		`mux.Handle("GET /v1/media/jobs/{id}", mediaAuth(gatewayLicenseGate(http.HandlerFunc(mediaHandler.Get))))`,
		`mux.Handle("POST /v1/media/jobs/{id}/cancel", mediaAuth(gatewayLicenseGate(http.HandlerFunc(mediaHandler.Cancel))))`,
		`mux.Handle("GET /v1/media/jobs/{id}/content", mediaAuth(gatewayLicenseGate(http.HandlerFunc(mediaHandler.Content))))`,
	} {
		if !strings.Contains(mainSource, registration) {
			t.Fatalf("public media route wiring missing %q", registration)
		}
	}
	for _, forbidden := range []string{
		`rateLimiter(http.HandlerFunc(mediaHandler.Submit))`,
		`rateLimiter(http.HandlerFunc(mediaHandler.Get))`,
		`rateLimiter(http.HandlerFunc(mediaHandler.Cancel))`,
		`rateLimiter(http.HandlerFunc(mediaHandler.Content))`,
	} {
		if strings.Contains(mainSource, forbidden) {
			t.Fatalf("media route must not use the legacy physical-key limiter: %s", forbidden)
		}
	}
}

func TestGatewayAuthenticationPrecedesLicenseDisclosure(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(src)
	for _, registration := range []string{
		`mux.Handle("POST /v1/chat/completions", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, chatPayloadGuard)(captureExecutionContext(gatewayLicenseGate(`,
		`mux.Handle("POST /v1/embeddings", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, embeddingsPayloadGuard)(captureExecutionContext(gatewayLicenseGate(`,
		`mux.Handle("POST /v1/responses", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, publicNonStreamPayloadGuard)(captureExecutionContext(gatewayLicenseGate(`,
		`mux.Handle("POST /v1/messages", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, publicNonStreamPayloadGuard)(captureExecutionContext(gatewayLicenseGate(`,
		`mux.Handle("GET /v1/models", gatewayAuth(gatewayLicenseGate(`,
	} {
		if !strings.Contains(mainSource, registration) {
			t.Fatalf("gateway authentication must wrap license admission: missing %q", registration)
		}
	}
	for _, forbidden := range []string{
		`gatewayLicenseGate(middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, chatPayloadGuard)`,
		`gatewayLicenseGate(middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, embeddingsPayloadGuard)`,
		`gatewayLicenseGate(middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, publicNonStreamPayloadGuard)`,
		`gatewayLicenseGate(gatewayAuth(admissionMiddleware(http.HandlerFunc(relay.HandleModels))))`,
	} {
		if strings.Contains(mainSource, forbidden) {
			t.Fatalf("gateway license admission still exposes state before authentication: %q", forbidden)
		}
	}
}

func TestGatewayRateAdmissionPrecedesIdempotencyAndBusinessHandlers(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(src)
	for _, registration := range []string{
		`sharedPolicyBilling.SetEarlyRateAdmissionEvidenceSink(relay)`,
		`middleware.StrictJSONIngress(sharedPolicyBilling.GatewayRateAdmission(idempotencyGuard(chatHandler)))`,
		`middleware.StrictJSONIngress(sharedPolicyBilling.GatewayRateAdmission(idempotencyGuard(embeddingsHandler)))`,
		`middleware.StrictJSONIngress(sharedPolicyBilling.GatewayRateAdmission(idempotencyGuard(responsesHandler)))`,
		`middleware.StrictJSONIngress(sharedPolicyBilling.GatewayRateAdmission(idempotencyGuard(messagesHandler)))`,
	} {
		if !strings.Contains(mainSource, registration) {
			t.Fatalf("early distributed rate admission wiring missing %q", registration)
		}
	}
}

func TestPublicNonStreamRoutesUseExactProductionPipeline(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		`responsesHandler := newGatewayPublicNonStreamHandler(postgres.Pool, relay, modeloperation.IDResponses, outboxSvc)`,
		`messagesHandler := newGatewayPublicNonStreamHandler(postgres.Pool, relay, modeloperation.IDMessages, outboxSvc)`,
		`publicNonStreamPayloadGuard := payloadpolicy.RequestMiddlewareWithMax(payloadBudget, 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext)`,
		`mux.Handle("POST /v1/responses", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, publicNonStreamPayloadGuard)(captureExecutionContext(gatewayLicenseGate(middleware.StrictJSONIngress(sharedPolicyBilling.GatewayRateAdmission(idempotencyGuard(responsesHandler)))))))`,
		`mux.Handle("POST /v1/messages", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, publicNonStreamPayloadGuard)(captureExecutionContext(gatewayLicenseGate(middleware.StrictJSONIngress(sharedPolicyBilling.GatewayRateAdmission(idempotencyGuard(messagesHandler)))))))`,
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("public non-stream production pipeline missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		`relay.HandleChatCompletion(w, governedRequest)`,
		`relay.HandleEmbeddings(w, governedRequest)`,
		`middleware.AdmissionFromContext(admissionCtrl)(responsesHandler)`,
		`middleware.AdmissionFromContext(admissionCtrl)(messagesHandler)`,
		`rateLimiter(responsesHandler)`,
		`rateLimiter(messagesHandler)`,
	} {
		if strings.Contains(mainSource, forbidden) {
			t.Fatalf("public non-stream route uses forbidden legacy path %q", forbidden)
		}
	}
}

func TestOpenAPISpecCoversRegisteredRoutes(t *testing.T) {
	routes := registeredMuxRoutes(t)
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	spec := string(specBytes)

	for _, route := range routes {
		if !openAPIHasOperation(spec, route) {
			t.Fatalf("OpenAPI spec missing registered route: %s %s", route.Method, route.Path)
		}
	}
}

func TestOpenAPICanonicalAdminAndRetiredMutationContractsRemainExplicit(t *testing.T) {
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	spec := string(specBytes)
	pathBlock := func(path string) string {
		t.Helper()
		pathLine := "  " + path + ":"
		start := strings.Index(spec, pathLine)
		if start < 0 {
			t.Fatalf("OpenAPI path missing: %s", path)
		}
		block := spec[start+len(pathLine):]
		if next := regexp.MustCompile(`\n  /[^:\n]*:`).FindStringIndex(block); next != nil {
			block = block[:next[0]]
		}
		return block
	}

	if got := strings.Count(spec, "*operation_legacy_model_mutation_retired"); got != 19 {
		t.Fatalf("OpenAPI retired legacy model mutation count = %d, want 19", got)
	}
	for _, fragment := range []string{
		"legacy_model_mutation_retired",
		"before License admission, request-body parsing, any business-data query, secret access",
		"contract_version: { type: string, enum: [unirouter.model-readiness.v2] }",
		"required: [mode, org_id, project_id, requested_model, resolved_model, endpoint, model_operation_id, caller]",
		"required: [gateway_license, registration, lifecycle, catalog, price_authority, deployment, provider_credential, operation, base_route, runtime_slot]",
		"contract_version: { type: string, enum: [unirouter.model-endpoints.canonical-compat.v3] }",
		"schema M79 counts enabled canonical provider_connections, not legacy channels",
		"API zero is accepted only as non-authorable staging state",
	} {
		if !strings.Contains(spec, fragment) {
			t.Fatalf("OpenAPI canonical/retirement contract missing %q", fragment)
		}
	}

	readiness := pathBlock("/api/v1/model-readiness")
	for _, fragment := range []string{
		"Contract `unirouter.model-readiness.v2`",
		"- name: deployment_id",
		"channel_id is rejected",
		"no legacy Router preview is used",
	} {
		if !strings.Contains(readiness, fragment) {
			t.Fatalf("OpenAPI readiness v2 contract missing %q", fragment)
		}
	}
	modelEndpoints := pathBlock("/api/v1/model-endpoints")
	if !strings.Contains(modelEndpoints, "CanonicalModelEndpointProjection") ||
		strings.Contains(modelEndpoints, "get: *operation_list") {
		t.Fatal("OpenAPI model-endpoints GET must use the explicit canonical compat v3 response")
	}
}

func TestFrontendDocsPageCoversRegisteredRoutes(t *testing.T) {
	routes := registeredMuxRoutes(t)
	srcBytes, err := os.ReadFile("../../frontend/src/pages/ApiDocsPage.tsx")
	if err != nil {
		t.Fatalf("read frontend API docs page: %v", err)
	}
	src := string(srcBytes)

	for _, route := range routes {
		operation := fmt.Sprintf("['%s', '%s'", route.Method, route.Path)
		if !strings.Contains(src, operation) {
			t.Fatalf("frontend API docs page missing registered operation: %s %s", route.Method, route.Path)
		}
	}
}

func TestFrontendEntrypointHasRoleAwareUnknownRouteFallback(t *testing.T) {
	srcBytes, err := os.ReadFile("../../frontend/src/main.tsx")
	if err != nil {
		t.Fatalf("read frontend entrypoint: %v", err)
	}
	src := string(srcBytes)
	for _, fragment := range []string{
		"function UnknownRoute()",
		"getDefaultPathForAuthz(authzContextForUser(user))",
		`<Route path="*" element={<UnknownRoute />} />`,
	} {
		if !strings.Contains(src, fragment) {
			t.Fatalf("frontend unknown-route fallback missing %q", fragment)
		}
	}
}

func TestBackendDocsPageUsesCurrentContractLanguage(t *testing.T) {
	srcBytes, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(srcBytes)

	staleTerms := []string{
		"PRD1 API index",
		"Current PRD1 API contract snapshot",
	}
	for _, term := range staleTerms {
		if strings.Contains(src, term) {
			t.Fatalf("backend docs page still contains stale contract wording: %q", term)
		}
	}

	currentMarkers := []string{
		"Current API contract index",
		"/api/v1/usage/analytics",
		"/api/v1/cost-optimization/summary",
		"/api/v1/internal-status",
		"/api/v1/runtime/status",
		"/api/v1/error-docs",
		"/api/v1/notification-webhooks",
		"/api/v1/model-registry",
		"/api/v1/auth/oidc/config",
	}
	for _, marker := range currentMarkers {
		if !strings.Contains(src, marker) {
			t.Fatalf("backend docs page missing current contract marker: %s", marker)
		}
	}
}

func registeredMuxRoutes(t *testing.T) []routeSpec {
	t.Helper()

	re := regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+) ([^"]+)"`)
	matches := re.FindAllStringSubmatch(productionRouteSource(t), -1)
	if len(matches) == 0 {
		t.Fatal("no mux routes found in cmd/uniroute production source")
	}

	seen := map[string]bool{}
	var routes []routeSpec
	for _, match := range matches {
		route := routeSpec{Method: match[1], Path: match[2]}
		key := route.Method + " " + route.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		routes = append(routes, route)
	}
	return routes
}

func productionRouteSource(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/uniroute: %v", err)
	}
	var source strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		contents, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		source.Write(contents)
		source.WriteByte('\n')
	}
	return source.String()
}

func openAPIHasOperation(spec string, route routeSpec) bool {
	pathLine := "  " + route.Path + ":"
	idx := strings.Index(spec, pathLine)
	if idx < 0 {
		return false
	}

	block := spec[idx+len(pathLine):]
	if next := regexp.MustCompile(`\n  /[^:\n]*:`).FindStringIndex(block); next != nil {
		block = block[:next[0]]
	}
	methodLine := "\n    " + strings.ToLower(route.Method) + ":"
	return strings.Contains(block, methodLine)
}
