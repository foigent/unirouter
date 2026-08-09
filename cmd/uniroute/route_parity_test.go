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
		"GET /api/v1/model-registry",
		"POST /api/v1/model-registry",
		"PUT /api/v1/model-registry/{id}",
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
		"organization: licensedRoute(licensing.FeatureModelGovernance, orgAdminRoute)",
		"projectRead:  licensedRoute(licensing.FeatureModelGovernance, projectReadRoute)",
		"modelSetRead: licensedRoute(licensing.FeatureModelGovernance",
		`mux.Handle("GET /api/v1/projects/{id}/model-sets", licensedRoute(licensing.FeatureModelGovernance, projectReadRoute)(adminHandler.ListProjectModelSets))`,
		`mux.Handle("PUT /api/v1/projects/{id}/model-sets", licensedRoute(licensing.FeatureModelGovernance`,
	} {
		if !strings.Contains(mainSource, fragment) {
			t.Fatalf("main.go model-governance registrar wiring missing %q", fragment)
		}
	}
}

func TestMainRegistersCommercialFeatureGates(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSource := string(src)
	for _, fragment := range []string{
		"licenseRuntime.Gate(licensing.FeatureOIDC)",
		"licensedRoute(licensing.FeatureGateway, platformRoute)(adminHandler.TestChannel)",
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
		`mux.Handle("POST /v1/media/jobs", gatewayLicenseGate(mediaAuth(http.HandlerFunc(mediaHandler.Submit))))`,
		`mux.Handle("GET /v1/media/jobs/{id}", gatewayLicenseGate(mediaAuth(http.HandlerFunc(mediaHandler.Get))))`,
		`mux.Handle("POST /v1/media/jobs/{id}/cancel", gatewayLicenseGate(mediaAuth(http.HandlerFunc(mediaHandler.Cancel))))`,
		`mux.Handle("GET /v1/media/jobs/{id}/content", gatewayLicenseGate(mediaAuth(http.HandlerFunc(mediaHandler.Content))))`,
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
