package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerRiskControlRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	riskControlRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/risk-control/offline-contract",
		riskControlRoute(handler.RiskControlOfflineContract),
	)
	mux.Handle(
		"POST /api/v1/risk-control/offline-preview",
		riskControlRoute(handler.PreviewRiskControl),
	)
}
