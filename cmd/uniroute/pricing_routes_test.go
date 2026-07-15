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

func TestRegisterPricingRoutesEnforcesProductionAuthorityBoundary(t *testing.T) {
	const secret = "p6f34-pricing-route-secret"
	db := activeAdminPrincipalDB()
	handler := admin.NewHandler(db, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(db, secret)(middleware.RequireRoles(roles...)(handler))
	}
	registerPricingRoutes(mux, handler, pricingRouteGuards{
		read: func(handler http.HandlerFunc) http.Handler {
			return roleRoute(handler, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
		},
		write: func(handler http.HandlerFunc) http.Handler {
			return roleRoute(handler, auth.RolePlatformAdmin)
		},
	})

	serve := func(method, target, body, role string) *httptest.ResponseRecorder {
		t.Helper()
		token, err := auth.GenerateJWT(7, 11, role, secret, 1)
		require.NoError(t, err)
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	for _, target := range []string{"/api/v1/model-prices", "/api/v1/org-model-prices"} {
		require.Equal(t, http.StatusForbidden, serve(http.MethodPost, target, "not-json", auth.RoleOrgAdmin).Code)
		require.Equal(t, http.StatusForbidden, serve(http.MethodPost, target, "not-json", auth.RoleBillingAdmin).Code)
		require.Equal(t, http.StatusBadRequest, serve(http.MethodPost, target, "not-json", auth.RolePlatformAdmin).Code)
		require.Equal(t, http.StatusBadRequest, serve(http.MethodPost, target, "not-json", auth.RoleLegacyAdmin).Code)
	}

	require.NotEqual(t, http.StatusForbidden, serve(http.MethodGet, "/api/v1/model-prices", "", auth.RoleOrgAdmin).Code)
	require.NotEqual(t, http.StatusForbidden, serve(http.MethodGet, "/api/v1/org-model-prices", "", auth.RoleOrgAdmin).Code)
	require.Equal(t, http.StatusForbidden, serve(http.MethodGet, "/api/v1/model-prices", "", auth.RoleBillingAdmin).Code)
}
