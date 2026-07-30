package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerProviderCOGSOfflineRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	billingRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/provider-cogs/offline-contract",
		billingRoute(handler.ProviderCOGSOfflineContract),
	)
	mux.Handle(
		"POST /api/v1/provider-cogs/offline-preview",
		billingRoute(handler.PreviewProviderCOGSOffline),
	)
}
