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

func TestEnterpriseDirectoryRoutesUseOrganizationAuthority(t *testing.T) {
	const secret = "prd8-enterprise-directory-route-secret"
	database := activeAdminPrincipalDB()
	handler := admin.NewHandler(database, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	directoryRoute := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(database, secret)(
			middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin)(handler),
		)
	}
	registerEnterpriseDirectoryRoutes(mux, handler, directoryRoute)

	serve := func(target, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := activeAdminToken(t, 7, 11, role, secret)
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}

	for _, role := range []string{
		auth.RoleBillingAdmin,
		auth.RoleProjectAdmin,
		auth.RoleDeveloper,
		auth.RoleViewer,
		auth.RoleOrgViewer,
	} {
		require.Equal(t, http.StatusForbidden,
			serve("/api/v1/enterprise-directory/sources", role).Code)
		require.Equal(t, http.StatusForbidden,
			serve("/api/v1/enterprise-directory/tree?source_ref=hcm", role).Code)
		require.Equal(t, http.StatusForbidden,
			serve("/api/v1/enterprise-directory/iam-actions", role).Code)
	}
	for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin} {
		require.NotEqual(t, http.StatusForbidden,
			serve("/api/v1/enterprise-directory/sources", role).Code)
		require.NotEqual(t, http.StatusForbidden,
			serve("/api/v1/enterprise-directory/tree?source_ref=hcm", role).Code)
		require.NotEqual(t, http.StatusForbidden,
			serve("/api/v1/enterprise-directory/iam-actions", role).Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/enterprise-directory/sources", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}
