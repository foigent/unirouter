package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerProviderCostRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	platformRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle("POST /api/v1/provider-cost/reconciliations", platformRoute(handler.ReconcileProviderCost))
	mux.Handle("GET /api/v1/provider-cost/pending", platformRoute(handler.ListPendingProviderCosts))
}
