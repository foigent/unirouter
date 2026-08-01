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

func TestEnterpriseFinanceRoutesSeparateReadAndWriteAuthority(t *testing.T) {
	const secret = "prd8-enterprise-finance-route-secret"
	database := activeAdminPrincipalDB()
	handler := admin.NewHandler(database, secret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	roleRoute := func(roles ...string) func(http.HandlerFunc) http.Handler {
		return func(handler http.HandlerFunc) http.Handler {
			return middleware.AdminAuth(database, secret)(middleware.RequireRoles(roles...)(handler))
		}
	}
	registerEnterpriseFinanceRoutes(mux, handler, enterpriseFinanceRouteGuards{
		read:  roleRoute(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin),
		write: roleRoute(auth.RolePlatformAdmin, auth.RoleBillingAdmin),
	})

	serve := func(method, target, body, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := activeAdminToken(t, 7, 11, role, secret)
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}

	readPaths := []string{
		"/api/v1/enterprise-finance/fiscal-periods",
		"/api/v1/enterprise-finance/cost-centers",
		"/api/v1/enterprise-finance/budgets",
		"/api/v1/enterprise-finance/budget-positions",
		"/api/v1/enterprise-finance/rate-cards",
		"/api/v1/enterprise-finance/pricing-snapshots/request-1",
		"/api/v1/enterprise-finance/allocation-runs?period_ref=FY26-P08",
		"/api/v1/enterprise-finance/allocation-runs/ALLOC-1",
		"/api/v1/enterprise-finance/allocation-summary?period_ref=FY26-P08",
		"/api/v1/enterprise-finance/adjustment-journals?period_ref=FY26-P08",
	}
	for _, path := range readPaths {
		for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin} {
			require.NotEqual(t, http.StatusForbidden, serve(http.MethodGet, path, "", role).Code, path+" "+role)
		}
		for _, role := range []string{auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer, auth.RoleOrgViewer} {
			require.Equal(t, http.StatusForbidden, serve(http.MethodGet, path, "", role).Code, path+" "+role)
		}
	}

	writeTargets := []string{
		"/api/v1/enterprise-finance/fiscal-periods",
		"/api/v1/enterprise-finance/fiscal-periods/FY26-P08/transition",
		"/api/v1/enterprise-finance/cost-centers",
		"/api/v1/enterprise-finance/budgets",
		"/api/v1/enterprise-finance/budgets/BUDGET-1/approve",
		"/api/v1/enterprise-finance/rate-cards",
		"/api/v1/enterprise-finance/rate-cards/RATE-1/approve",
		"/api/v1/enterprise-finance/rate-cards/RATE-1/activate",
		"/api/v1/enterprise-finance/allocation-runs",
		"/api/v1/enterprise-finance/adjustment-journals",
		"/api/v1/enterprise-finance/adjustment-journals/ADJ-1/approve",
	}
	for _, target := range writeTargets {
		for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin, auth.RoleBillingAdmin} {
			require.NotEqual(t, http.StatusForbidden, serve(http.MethodPost, target, "not-json", role).Code, target+" "+role)
		}
		for _, role := range []string{auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer, auth.RoleOrgViewer} {
			require.Equal(t, http.StatusForbidden, serve(http.MethodPost, target, "not-json", role).Code, target+" "+role)
		}
	}
}
