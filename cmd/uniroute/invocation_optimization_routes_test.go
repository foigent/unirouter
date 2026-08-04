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

func TestRegisterInvocationOptimizationRoutesUsesOrganizationAdminAuthority(t *testing.T) {
	const secret = "invocation-optimization-route-secret"
	database := activeAdminPrincipalDB()
	handler := admin.NewHandler(database, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	guard := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(database, secret)(
			middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin)(handler),
		)
	}
	registerInvocationOptimizationRoutes(mux, handler, guard)

	type route struct{ method, target, body string }
	routes := []route{
		{http.MethodGet, "/api/v1/invocation-optimization/contract", ""},
		{http.MethodGet, "/api/v1/invocation-optimization/policies", ""},
		{http.MethodPost, "/api/v1/invocation-optimization/policies", "{}"},
		{http.MethodGet, "/api/v1/invocation-optimization/policies/opt_company_chat", ""},
		{http.MethodGet, "/api/v1/invocation-optimization/policies/opt_company_chat/readiness", ""},
		{http.MethodPut, "/api/v1/invocation-optimization/policies/opt_company_chat", "{}"},
		{http.MethodPost, "/api/v1/invocation-optimization/policies/opt_company_chat/validate", "{}"},
		{http.MethodPost, "/api/v1/invocation-optimization/policies/opt_company_chat/shadow", "{}"},
		{http.MethodPost, "/api/v1/invocation-optimization/policies/opt_company_chat/canary", "{}"},
		{http.MethodPost, "/api/v1/invocation-optimization/policies/opt_company_chat/rollback", "{}"},
	}
	serve := func(route route, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := activeAdminToken(t, 7, 11, role, secret)
		req := httptest.NewRequest(route.method, route.target, strings.NewReader(route.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	for _, item := range routes {
		for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin} {
			require.NotEqual(t, http.StatusForbidden, serve(item, role).Code, "%s %s role=%s", item.method, item.target, role)
		}
		for _, role := range []string{auth.RoleProjectAdmin, auth.RoleBillingAdmin, auth.RoleDeveloper, auth.RoleViewer} {
			require.Equal(t, http.StatusForbidden, serve(item, role).Code, "%s %s role=%s", item.method, item.target, role)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/invocation-optimization/contract", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
