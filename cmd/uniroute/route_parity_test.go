package main

import (
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
		"GET /api/v1/keys",
		"POST /api/v1/keys",
		"PUT /api/v1/keys/{id}",
		"POST /api/v1/keys/{id}/disable",
		"GET /api/v1/projects",
		"POST /api/v1/projects",
		"GET /api/v1/projects/{id}",
		"PUT /api/v1/projects/{id}",
		"GET /api/v1/projects/{id}/model-sets",
		"PUT /api/v1/projects/{id}/model-sets",
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
	if !strings.Contains(string(src), "registerPricingRoutes(mux, adminHandler") {
		t.Fatal("main.go does not call the extracted pricing route registrar")
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
		if !strings.Contains(src, route.Path) {
			t.Fatalf("frontend API docs page missing registered route path: %s %s", route.Method, route.Path)
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
