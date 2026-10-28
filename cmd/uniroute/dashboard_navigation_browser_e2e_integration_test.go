//go:build integration && pr4browser

package main

import (
	"context"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"net/http"
)

// Test-only assembly reuses production registration functions and live PG auth.
// The role lists below mirror main.go; no DTO, session or permission is mocked.
func registerOverviewNavigationRoutes(mux *http.ServeMux, h *admin.Handler, state *overviewBrowserFaults,
	authenticated func(http.HandlerFunc) http.Handler,
	roles func(http.HandlerFunc, ...string) http.Handler) {
	platform := func(next http.HandlerFunc) http.Handler { return roles(next, auth.RolePlatformAdmin) }
	orgAdmin := func(next http.HandlerFunc) http.Handler {
		return roles(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	}
	billing := func(next http.HandlerFunc) http.Handler {
		return roles(next, auth.RolePlatformAdmin, auth.RoleBillingAdmin)
	}
	pricing := func(next http.HandlerFunc) http.Handler {
		return roles(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)
	}
	modelRead := func(next http.HandlerFunc) http.Handler {
		return roles(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer)
	}
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/logs/{id}":       h.GetLogDetail,
		"GET /api/v1/logs/{id}/trace": h.GetRequestTrace,
		"GET /api/v1/usage/analytics": h.UsageAnalytics, "GET /api/v1/error-docs": h.ListErrorDocs,
		"GET /api/v1/notification-events":                   h.ListNotificationEvents,
		"GET /api/v1/notification-deliveries":               h.ListNotificationDeliveries,
		"GET /api/v1/notification-webhooks":                 h.ListNotificationWebhooks,
		"POST /api/v1/notification-events/{id}/acknowledge": h.AcknowledgeNotificationEvent,
		"POST /api/v1/notification-events/{id}/resolve":     h.ResolveNotificationEvent,
		"GET /api/v1/keys":                                  h.ListAPIKeys,
	} {
		mux.Handle(pattern, authenticated(handler))
	}
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/wallet": h.GetWallet, "GET /api/v1/wallet/transactions": h.ListWalletTransactions,
		"GET /api/v1/wallet/transactions/export": h.ExportWalletTransactions,
		"GET /api/v1/grants":                     h.ListGrants, "GET /api/v1/reconciliation": h.ListReconciliation,
	} {
		mux.Handle(pattern, billing(handler))
	}
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/runtime/status": h.RuntimeStatus, "GET /api/v1/users": h.ListUsers,
		"GET /api/v1/audit-logs": h.ListAuditLogs, "GET /api/v2/audit-logs": h.ListAuditLogsV2,
	} {
		mux.Handle(pattern, orgAdmin(handler))
	}
	mux.Handle("GET /api/v1/logs", authenticated(func(w http.ResponseWriter, r *http.Request) {
		if state.failLogs.Swap(false) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			r = r.WithContext(ctx)
		}
		h.ListLogs(w, r)
	}))
	mux.Handle("GET /api/v1/model-endpoints", platform(h.ListModelEndpoints))
	mux.Handle("GET /api/v1/provider-packs", platform(h.ListProviderPacks))
	mux.Handle("GET /api/v1/channel-keys", platform(h.ListChannelKeys))
	mux.Handle("GET /api/v2/organizations/{org_id}/xinsight/setup-peer", authenticated(h.GetXInsightSetupPeer))
	registerModelDeploymentRoutes(mux, h, modelDeploymentRouteGuards{read: modelRead, write: orgAdmin, approve: platform, safety: orgAdmin})
	registerModelGovernanceRoutes(mux, h, modelGovernanceRouteGuards{organizationRead: modelRead, organizationWrite: orgAdmin, projectRead: authenticated, modelSetRead: authenticated})
	registerInvocationOptimizationRoutes(mux, h, orgAdmin)
	registerCostOptimizationRoutes(mux, h, pricing)
	registerProviderCostRoutes(mux, h, platform)
	registerPricingRoutes(mux, h, pricingRouteGuards{read: pricing, write: platform})
	registerEnterpriseFinanceRoutes(mux, h, enterpriseFinanceRouteGuards{read: pricing, write: billing})
	registerDeploymentNetworkRoutes(mux, h, deploymentNetworkRouteGuards{read: orgAdmin, write: platform})
	registerAccessProvisioningRoutes(mux, h, authenticated, orgAdmin)
	registerWorkloadApplicationRoutes(mux, h, authenticated)
	registerMemberLifecycleRoutes(mux, h, orgAdmin)
	registerEnterpriseDirectoryRoutes(mux, h, orgAdmin)
	registerAgentTrustRoutes(mux, h, orgAdmin)
	registerModelAuthorizationRoutes(mux, h, orgAdmin)
	registerGlobalControlRoutes(mux, h, platform, platform)
	registerGuardrailPolicyRoutes(mux, h, guardrailPolicyRouteGuards{read: orgAdmin, write: orgAdmin})
	registerLimitPolicyRoutes(mux, h, limitPolicyRouteGuards{read: orgAdmin, write: orgAdmin})
}
