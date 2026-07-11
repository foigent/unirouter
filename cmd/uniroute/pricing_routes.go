package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type pricingRouteGuard func(http.HandlerFunc) http.Handler

type pricingRouteGuards struct {
	read  pricingRouteGuard
	write pricingRouteGuard
}

// registerPricingRoutes keeps the pricing authority boundary in one production
// wiring point. Reads remain available to organization administrators; every
// mutation is guarded by platform authority.
func registerPricingRoutes(mux *http.ServeMux, handler *admin.Handler, guards pricingRouteGuards) {
	mux.Handle("GET /api/v1/model-prices", guards.read(handler.ListModelPrices))
	mux.Handle("POST /api/v1/model-prices", guards.write(handler.CreateModelPrice))
	mux.Handle("PUT /api/v1/model-prices/{id}", guards.write(handler.UpdateModelPrice))
	mux.Handle("DELETE /api/v1/model-prices/{id}", guards.write(handler.DeleteModelPrice))

	mux.Handle("GET /api/v1/model-names", guards.read(handler.GetModelNames))
	mux.Handle("GET /api/v1/org-model-prices", guards.read(handler.ListOrgModelPrices))
	mux.Handle("POST /api/v1/org-model-prices", guards.write(handler.CreateOrgModelPrice))
	mux.Handle("PUT /api/v1/org-model-prices/{id}", guards.write(handler.UpdateOrgModelPrice))
	mux.Handle("DELETE /api/v1/org-model-prices/{id}", guards.write(handler.DeleteOrgModelPrice))
}
