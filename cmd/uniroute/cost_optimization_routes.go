package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type costOptimizationRouteGuard func(http.HandlerFunc) http.Handler

// registerCostOptimizationRoutes keeps all read-only cost evidence behind the
// pricing-read authority shared by platform, organization and billing admins.
func registerCostOptimizationRoutes(mux *http.ServeMux, handler *admin.Handler, read costOptimizationRouteGuard) {
	mux.Handle("GET /api/v1/cost-optimization/summary", read(handler.CostOptimizationSummary))
	mux.Handle("GET /api/v1/cost-optimization/trend", read(handler.CostOptimizationTrend))
	mux.Handle("GET /api/v1/cost-optimization/breakdown", read(handler.CostOptimizationBreakdown))
	mux.Handle("GET /api/v1/cost-optimization/opportunities", read(handler.CostOptimizationOpportunities))
}
