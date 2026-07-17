package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestRegisterModelGovernanceRoutesEnforcesProductionRoleBoundary(t *testing.T) {
	const secret = "p6f36-model-governance-route-secret"
	db := activeAdminPrincipalDB()
	handler := admin.NewHandler(db, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(db, secret)(middleware.RequireRoles(roles...)(handler))
	}
	registerModelGovernanceRoutes(mux, handler, modelGovernanceRouteGuards{
		organization: func(handler http.HandlerFunc) http.Handler {
			return roleRoute(handler, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
		},
		projectRead: func(handler http.HandlerFunc) http.Handler {
			return roleRoute(handler, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)
		},
		modelSetRead: func(handler http.HandlerFunc) http.Handler {
			return roleRoute(handler, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin)
		},
	})

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

	organizationRoutes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodGet, "/api/v1/model-registry", ""},
		{http.MethodPost, "/api/v1/model-registry", "not-json"},
		{http.MethodPut, "/api/v1/model-registry/1", "not-json"},
		{http.MethodGet, "/api/v1/model-aliases", ""},
		{http.MethodPost, "/api/v1/model-aliases", "not-json"},
		{http.MethodPut, "/api/v1/model-aliases/1", "not-json"},
		{http.MethodPost, "/api/v1/model-sets", "not-json"},
		{http.MethodGet, "/api/v1/model-sets/1/items", ""},
		{http.MethodPut, "/api/v1/model-sets/1/items", "not-json"},
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin} {
		require.NotEqual(t, http.StatusForbidden, serve(http.MethodGet, "/api/v1/model-sets", "", role).Code,
			"GET /api/v1/model-sets role=%s", role)
	}
	for _, role := range []string{auth.RoleBillingAdmin, auth.RoleDeveloper, auth.RoleViewer, auth.RoleOrgViewer} {
		require.Equal(t, http.StatusForbidden, serve(http.MethodGet, "/api/v1/model-sets", "", role).Code,
			"GET /api/v1/model-sets role=%s", role)
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin} {
		for _, route := range organizationRoutes {
			require.NotEqual(t, http.StatusForbidden, serve(route.method, route.target, route.body, role).Code,
				"%s %s role=%s", route.method, route.target, role)
		}
	}
	for _, role := range []string{
		auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper,
		auth.RoleViewer, auth.RoleOrgViewer,
	} {
		for _, route := range organizationRoutes {
			require.Equal(t, http.StatusForbidden, serve(route.method, route.target, route.body, role).Code,
				"%s %s role=%s", route.method, route.target, role)
		}
	}

	for _, role := range []string{
		auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin,
		auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer,
	} {
		require.Equal(t, http.StatusBadRequest,
			serve(http.MethodGet, "/api/v1/model-catalog?project_id=bad", "", role).Code,
			"catalog role=%s", role)
	}
	for _, role := range []string{auth.RoleBillingAdmin, auth.RoleOrgViewer} {
		require.Equal(t, http.StatusForbidden,
			serve(http.MethodGet, "/api/v1/model-catalog?project_id=bad", "", role).Code,
			"catalog role=%s", role)
	}

	for _, authorization := range []string{"", "Bearer invalid"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/model-registry", nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}
