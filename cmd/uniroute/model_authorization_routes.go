package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerModelAuthorizationRoutes(mux *http.ServeMux, handler *admin.Handler, orgAdminRoute func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/model-call-authorizations/contract", orgAdminRoute(handler.GetModelAuthorizationContract))
	mux.Handle("GET /api/v1/model-call-authorizations", orgAdminRoute(handler.ListModelAuthorizations))
	mux.Handle("POST /api/v1/model-call-authorizations", orgAdminRoute(handler.CreateModelAuthorization))
	mux.Handle("POST /api/v1/model-call-authorizations/{ref}/activate", orgAdminRoute(handler.ActivateModelAuthorization))
	mux.Handle("POST /api/v1/model-call-authorizations/{ref}/revoke", orgAdminRoute(handler.RevokeModelAuthorization))
}
