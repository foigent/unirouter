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

func TestProviderCOGSOfflineRoutesUseFinancialAuthority(t *testing.T) {
	const secret = "p7-provider-cogs-offline-route-secret"
	database := activeAdminPrincipalDB()
	handler := admin.NewHandler(database, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	billingRoute := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(database, secret)(
			middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleBillingAdmin)(handler),
		)
	}
	registerProviderCOGSOfflineRoutes(mux, handler, billingRoute)

	serve := func(method, target, body, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := activeAdminToken(t, 7, 11, role, secret)
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}

	for _, role := range []string{
		auth.RoleOrgAdmin,
		auth.RoleProjectAdmin,
		auth.RoleDeveloper,
		auth.RoleViewer,
		auth.RoleOrgViewer,
	} {
		require.Equal(
			t,
			http.StatusForbidden,
			serve(http.MethodGet, "/api/v1/provider-cogs/offline-contract", "", role).Code,
		)
		require.Equal(
			t,
			http.StatusForbidden,
			serve(http.MethodPost, "/api/v1/provider-cogs/offline-preview", `{}`, role).Code,
		)
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleBillingAdmin, auth.RoleLegacyAdmin} {
		require.Equal(
			t,
			http.StatusOK,
			serve(http.MethodGet, "/api/v1/provider-cogs/offline-contract", "", role).Code,
		)
		require.NotEqual(
			t,
			http.StatusForbidden,
			serve(http.MethodPost, "/api/v1/provider-cogs/offline-preview", `{}`, role).Code,
		)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider-cogs/offline-contract", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}
