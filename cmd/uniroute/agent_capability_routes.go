package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/capapi"
)

type agentCapabilityRouteGuard func(http.Handler) http.Handler

type agentCapabilityRouteGuards struct {
	authorize    agentCapabilityRouteGuard
	authenticate agentCapabilityRouteGuard
	rateLimit    agentCapabilityRouteGuard
}

// registerAgentCapabilityRoutes keeps the high-risk capability control surface
// reviewable outside the application assembly. Both root-key and capability
// calls retain the root API-key rate bucket; individual endpoint handlers then
// enforce whether a root key or parent capability is required.
func registerAgentCapabilityRoutes(mux *http.ServeMux, handler *capapi.Handler, guards agentCapabilityRouteGuards) {
	guard := func(endpoint http.HandlerFunc) http.Handler {
		return guards.authenticate(guards.authorize(guards.rateLimit(endpoint)))
	}
	mux.Handle("POST /v1/agent-capabilities/exchange", guard(handler.Exchange))
	mux.Handle("POST /v1/agent-capabilities/delegate", guard(handler.Delegate))
	mux.Handle("POST /v1/agent-capabilities/refresh", guard(handler.Refresh))
	mux.Handle("POST /v1/agent-capabilities/revoke", guard(handler.Revoke))
}
