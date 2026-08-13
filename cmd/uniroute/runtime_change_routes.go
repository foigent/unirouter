package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerRuntimeChangeRoutes(mux *http.ServeMux, handler *admin.Handler, platformRoute func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/runtime-changes", platformRoute(handler.ListRuntimeChanges))
	mux.Handle("POST /api/v1/runtime-changes", platformRoute(handler.CreateRuntimeChange))
	mux.Handle("GET /api/v1/runtime-changes/slot", platformRoute(handler.GetRuntimeChangeSlot))
	mux.Handle("GET /api/v1/runtime-changes/{ref}", platformRoute(handler.GetRuntimeChange))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/validate", platformRoute(handler.ValidateRuntimeChange))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/activate-initial", platformRoute(handler.ActivateInitialRuntimeChange))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/shadow", platformRoute(handler.AdvanceRuntimeChangeShadow))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/canary", platformRoute(handler.StartRuntimeChangeCanary))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/promote", platformRoute(handler.PromoteRuntimeChange))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/rollback", platformRoute(handler.RollbackRuntimeChange))
}
