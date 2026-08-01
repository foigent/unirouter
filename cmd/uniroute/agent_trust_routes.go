package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerAgentTrustRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	trustRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/agent-trust/offline-contract",
		trustRoute(handler.AgentTrustOfflineContract),
	)
	mux.Handle(
		"POST /api/v1/agent-trust/offline-preview",
		trustRoute(handler.PreviewAgentTrust),
	)
}
