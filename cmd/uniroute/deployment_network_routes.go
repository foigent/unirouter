package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type deploymentNetworkRouteGuards struct {
	read  func(http.HandlerFunc) http.Handler
	write func(http.HandlerFunc) http.Handler
}

func registerDeploymentNetworkRoutes(mux *http.ServeMux, handler *admin.Handler, guards deploymentNetworkRouteGuards) {
	mux.Handle("GET /api/v1/deployment/contract", guards.read(handler.GetDeploymentNetworkContract))
	mux.Handle("GET /api/v1/deployment/profiles", guards.write(handler.ListDeploymentProfiles))
	mux.Handle("POST /api/v1/deployment/profiles", guards.write(handler.CreateDeploymentProfile))
	mux.Handle("PUT /api/v1/deployment/profiles/{ref}", guards.write(handler.UpdateDeploymentProfile))
	mux.Handle("GET /api/v1/deployment/endpoints", guards.read(handler.ListGatewayEndpoints))
	mux.Handle("POST /api/v1/deployment/endpoints", guards.write(handler.CreateGatewayEndpoint))
	mux.Handle("PUT /api/v1/deployment/endpoints/{ref}", guards.write(handler.UpdateGatewayEndpoint))
}
