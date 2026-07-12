package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type limitPolicyRouteGuard func(http.HandlerFunc) http.Handler

type limitPolicyRouteGuards struct {
	read  limitPolicyRouteGuard
	write limitPolicyRouteGuard
}

// registerLimitPolicyRoutes keeps the distributed-capacity authority boundary
// visible outside the main application assembly. Billing admins may inspect;
// only platform and organization admins may mutate policies.
func registerLimitPolicyRoutes(mux *http.ServeMux, handler *admin.Handler, guards limitPolicyRouteGuards) {
	mux.Handle("GET /api/v1/limit-policies", guards.read(handler.ListLimitPolicies))
	mux.Handle("POST /api/v1/limit-policies", guards.write(handler.CreateLimitPolicy))
	mux.Handle("PUT /api/v1/limit-policies/{id}", guards.write(handler.UpdateLimitPolicy))
}
