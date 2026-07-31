package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerProvenancePassportRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	passportRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/provenance-passport/offline-contract",
		passportRoute(handler.ProvenancePassportOfflineContract),
	)
	mux.Handle(
		"POST /api/v1/provenance-passport/offline-preview",
		passportRoute(handler.PreviewProvenancePassport),
	)
}
