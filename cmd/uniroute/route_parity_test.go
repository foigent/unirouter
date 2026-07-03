package main

import (
	"os"
	"strings"
	"testing"
)

func TestAdminRoutesRegisteredForFrontendClient(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	main := string(src)

	routes := []string{
		"POST /api/v1/auth/login",
		"GET /api/v1/auth/oidc/config",
		"GET /api/v1/auth/oidc/start",
		"GET /api/v1/auth/oidc/callback",
		"GET /api/v1/me",
		"PUT /api/v1/me/password",
		"GET /api/v1/dashboard/stats",
		"GET /api/v1/channels",
		"POST /api/v1/channels",
		"PUT /api/v1/channels/{id}",
		"DELETE /api/v1/channels/{id}",
		"POST /api/v1/channels/{id}/test",
		"GET /api/v1/keys",
		"POST /api/v1/keys",
		"PUT /api/v1/keys/{id}",
		"POST /api/v1/keys/{id}/disable",
		"GET /api/v1/projects",
		"POST /api/v1/projects",
		"GET /api/v1/projects/{id}",
		"PUT /api/v1/projects/{id}",
		"GET /api/v1/projects/{id}/model-sets",
		"PUT /api/v1/projects/{id}/model-sets",
		"GET /api/v1/channel-keys",
		"POST /api/v1/channel-keys",
		"PUT /api/v1/channel-keys/{id}",
		"DELETE /api/v1/channel-keys/{id}",
		"GET /api/v1/wallet",
		"POST /api/v1/wallet/topup",
		"POST /api/v1/wallet/adjust",
		"GET /api/v1/wallet/transactions",
		"GET /api/v1/logs",
		"GET /api/v1/logs/{id}",
		"GET /api/v1/usage/analytics",
		"GET /api/v1/internal-status",
		"GET /api/v1/error-docs",
		"GET /api/v1/model-prices",
		"POST /api/v1/model-prices",
		"PUT /api/v1/model-prices/{id}",
		"DELETE /api/v1/model-prices/{id}",
		"GET /api/v1/model-registry",
		"POST /api/v1/model-registry",
		"PUT /api/v1/model-registry/{id}",
		"GET /api/v1/model-catalog",
		"GET /api/v1/model-aliases",
		"POST /api/v1/model-aliases",
		"PUT /api/v1/model-aliases/{id}",
		"GET /api/v1/model-sets",
		"POST /api/v1/model-sets",
		"GET /api/v1/model-sets/{id}/items",
		"PUT /api/v1/model-sets/{id}/items",
		"GET /api/v1/model-names",
		"GET /api/v1/org-model-prices",
		"POST /api/v1/org-model-prices",
		"PUT /api/v1/org-model-prices/{id}",
		"DELETE /api/v1/org-model-prices/{id}",
		"GET /api/v1/reconciliation",
		"GET /api/v1/settings",
		"PUT /api/v1/settings/{key}",
		"GET /api/v1/license",
		"PUT /api/v1/license",
		"GET /api/v1/users",
		"POST /api/v1/users",
		"PUT /api/v1/users/{id}",
		"DELETE /api/v1/users/{id}",
		"GET /api/v1/grants",
		"POST /api/v1/grants",
		"GET /api/v1/notification-events",
		"GET /api/v1/notification-deliveries",
		"GET /api/v1/notification-webhooks",
		"POST /api/v1/notification-webhooks",
		"PUT /api/v1/notification-webhooks/{id}",
		"GET /api/v1/audit-logs",
	}

	for _, route := range routes {
		if !strings.Contains(main, route) {
			t.Fatalf("route required by frontend client is not registered: %s", route)
		}
	}
}
