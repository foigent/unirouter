package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerGovernanceSimulationRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	platformRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/governance-simulation/offline-contract",
		platformRoute(handler.GovernanceSimulationOfflineContract),
	)
	mux.Handle(
		"POST /api/v1/governance-simulation/offline-preview",
		platformRoute(handler.PreviewGovernanceSimulation),
	)
}
