package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type modelGovernanceRouteGuard func(http.HandlerFunc) http.Handler

type modelGovernanceRouteGuards struct {
	organizationRead  modelGovernanceRouteGuard
	organizationWrite modelGovernanceRouteGuard
	projectRead       modelGovernanceRouteGuard
	modelSetRead      modelGovernanceRouteGuard
}

// registerModelGovernanceRoutes keeps the organization-governance authority
// contract reviewable outside the main application assembly.
func registerModelGovernanceRoutes(mux *http.ServeMux, handler *admin.Handler, guards modelGovernanceRouteGuards) {
	mux.Handle("GET /api/v1/model-registry", guards.organizationRead(handler.ListModelRegistry))
	mux.Handle("POST /api/v1/model-registry", guards.organizationWrite(handler.CreateModelRegistry))
	mux.Handle("PUT /api/v1/model-registry/{id}", guards.organizationWrite(handler.UpdateModelRegistry))
	mux.Handle("GET /api/v1/model-catalog", guards.projectRead(handler.ListApprovedModelCatalog))
	mux.Handle("GET /api/v1/model-aliases", guards.organizationRead(handler.ListModelAliases))
	mux.Handle("POST /api/v1/model-aliases", guards.organizationWrite(handler.CreateModelAlias))
	mux.Handle("PUT /api/v1/model-aliases/{id}", guards.organizationWrite(handler.UpdateModelAlias))
	mux.Handle("GET /api/v1/model-sets", guards.modelSetRead(handler.ListModelSets))
	mux.Handle("POST /api/v1/model-sets", guards.organizationWrite(handler.CreateModelSet))
	mux.Handle("GET /api/v1/model-sets/{id}/items", guards.organizationRead(handler.ListModelSetItems))
	mux.Handle("PUT /api/v1/model-sets/{id}/items", guards.organizationWrite(handler.ReplaceModelSetItems))
}
