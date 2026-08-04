package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestRegisterCostOptimizationRoutesUsesReadOnlyEnterpriseAuthority(t *testing.T) {
	const secret = "cost-optimization-route-secret"
	database := activeAdminPrincipalDB()
	handler := admin.NewHandler(database, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	read := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(database, secret)(
			middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)(handler),
		)
	}
	registerCostOptimizationRoutes(mux, handler, read)

	serve := func(target, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := activeAdminToken(t, 7, 11, role, secret)
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	for _, target := range []string{
		"/api/v1/cost-optimization/summary",
		"/api/v1/cost-optimization/trend",
		"/api/v1/cost-optimization/breakdown?group_by=model",
		"/api/v1/cost-optimization/opportunities?group_by=model",
	} {
		for _, role := range []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin} {
			require.NotEqual(t, http.StatusForbidden, serve(target, role).Code, "%s %s", target, role)
		}
		require.Equal(t, http.StatusForbidden, serve(target, auth.RoleDeveloper).Code, target)
	}
}
