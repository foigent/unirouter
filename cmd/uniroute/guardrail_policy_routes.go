package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type guardrailPolicyRouteGuard func(http.HandlerFunc) http.Handler

type guardrailPolicyRouteGuards struct {
	read  guardrailPolicyRouteGuard
	write guardrailPolicyRouteGuard
}

// registerGuardrailPolicyRoutes keeps platform mandatory authority and
// tenant-only tightening visible outside the main application assembly.
func registerGuardrailPolicyRoutes(mux *http.ServeMux, handler *admin.Handler, guards guardrailPolicyRouteGuards) {
	mux.Handle("GET /api/v1/guardrail-policies", guards.read(handler.ListGuardrailPolicies))
	mux.Handle("POST /api/v1/guardrail-policies", guards.write(handler.CreateGuardrailPolicy))
	mux.Handle("PUT /api/v1/guardrail-policies/{id}", guards.write(handler.UpdateGuardrailPolicy))
}
