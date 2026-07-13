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
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F40CProviderCostRoutesRequirePlatformAuthority(t *testing.T) {
	const secret = "p6f40c-provider-cost-route-secret"
	handler := admin.NewHandler(&testutil.MockDB{}, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	platformRoute := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(secret)(middleware.RequireRoles(auth.RolePlatformAdmin)(handler))
	}
	registerProviderCostRoutes(mux, handler, platformRoute)

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

	routes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v1/provider-cost/reconciliations", "not-json"},
		{http.MethodGet, "/api/v1/provider-cost/pending?org_id=bad", ""},
	}
	for _, role := range []string{
		auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin,
		auth.RoleDeveloper, auth.RoleViewer, auth.RoleOrgViewer, "unknown_role",
	} {
		for _, route := range routes {
			require.Equal(t, http.StatusForbidden, serve(route.method, route.target, route.body, role).Code)
		}
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin} {
		for _, route := range routes {
			require.NotEqual(t, http.StatusForbidden, serve(route.method, route.target, route.body, role).Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/provider-cost/pending?org_id=1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
