package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type invocationOptimizationRouteGuard func(http.HandlerFunc) http.Handler

// registerInvocationOptimizationRoutes keeps the high-impact optimization
// lifecycle behind one organization-governance authority boundary. Runtime
// mutation remains impossible until an evidence-gated canary transition.
func registerInvocationOptimizationRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	guard invocationOptimizationRouteGuard,
) {
	mux.Handle("GET /api/v1/invocation-optimization/contract", guard(handler.InvocationOptimizationContract))
	mux.Handle("GET /api/v1/invocation-optimization/policies", guard(handler.ListInvocationOptimizationPolicies))
	mux.Handle("POST /api/v1/invocation-optimization/policies", guard(handler.CreateInvocationOptimizationPolicy))
	mux.Handle("GET /api/v1/invocation-optimization/policies/{ref}", guard(handler.GetInvocationOptimizationPolicy))
	mux.Handle("PUT /api/v1/invocation-optimization/policies/{ref}", guard(handler.UpdateInvocationOptimizationPolicy))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/validate", guard(handler.ValidateInvocationOptimizationPolicy))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/shadow", guard(handler.ShadowInvocationOptimizationPolicy))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/canary", guard(handler.CanaryInvocationOptimizationPolicy))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/rollback", guard(handler.RollbackInvocationOptimizationPolicy))
}
