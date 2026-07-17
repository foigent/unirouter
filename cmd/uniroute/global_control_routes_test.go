package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestRegisterGlobalControlRoutesEnforcesProductionAuthorityBoundary(t *testing.T) {
	const secret = "p6f35-global-control-route-secret"
	db := activeAdminPrincipalDB()
	handler := admin.NewHandler(db, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	platform := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(db, secret)(middleware.RequireRoles(auth.RolePlatformAdmin)(handler))
	}
	registerGlobalControlRoutes(mux, handler, platform, platform)

	serve := func(method, target, body, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := activeAdminToken(t, 7, 11, role, secret)
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	routes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodGet, "/api/v1/settings", ""},
		{http.MethodPut, "/api/v1/settings/site_name", "not-json"},
		{http.MethodGet, "/api/v1/license", ""},
		{http.MethodPut, "/api/v1/license", "not-json"},
		{http.MethodGet, "/api/v1/sensitive-words", ""},
		{http.MethodPost, "/api/v1/sensitive-words", "not-json"},
		{http.MethodPut, "/api/v1/sensitive-words", "not-json"},
		{http.MethodDelete, "/api/v1/sensitive-words", "not-json"},
	}
	tenantRoles := []string{
		auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin,
		auth.RoleDeveloper, auth.RoleViewer, auth.RoleOrgViewer,
	}
	for _, role := range tenantRoles {
		for _, route := range routes {
			require.Equal(t, http.StatusForbidden, serve(route.method, route.target, route.body, role).Code,
				"%s %s role=%s", route.method, route.target, role)
		}
	}

	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin} {
		for _, route := range routes {
			require.NotEqual(t, http.StatusForbidden, serve(route.method, route.target, route.body, role).Code,
				"%s %s role=%s", route.method, route.target, role)
		}
	}

	for _, authorization := range []string{"", "Bearer invalid"} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/site_name", strings.NewReader("not-json"))
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestOpenAPIDocumentsGlobalControlPlatformAuthority(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	require.NoError(t, err)
	spec := string(raw)
	fragments := []string{
		"  /api/v1/settings:\n    x-unirouter-rbac:\n      get: [platform_admin]",
		"  /api/v1/settings/{key}:\n    x-unirouter-rbac:\n      put: [platform_admin]",
		"  /api/v1/license:\n    x-unirouter-rbac:\n      get: [platform_admin]\n      put: [platform_admin]",
		"  /api/v1/sensitive-words:\n    x-unirouter-rbac:\n      get: [platform_admin]\n      post: [platform_admin]\n      put: [platform_admin]\n      delete: [platform_admin]",
		"required: [license]",
		"required: [words]",
	}
	for _, fragment := range fragments {
		require.Contains(t, spec, fragment)
	}
}
