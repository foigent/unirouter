package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerRuntimeChangeRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	platformRoute func(http.HandlerFunc) http.Handler,
	retiredMutation http.HandlerFunc,
) {
	mux.Handle("GET /api/v1/runtime-changes", platformRoute(handler.ListRuntimeChanges))
	mux.Handle("POST /api/v1/runtime-changes", platformRoute(retiredMutation))
	mux.Handle("GET /api/v1/runtime-changes/slot", platformRoute(handler.GetRuntimeChangeSlot))
	mux.Handle("GET /api/v1/runtime-changes/{ref}", platformRoute(handler.GetRuntimeChange))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/validate", platformRoute(retiredMutation))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/activate-initial", platformRoute(retiredMutation))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/shadow", platformRoute(retiredMutation))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/canary", platformRoute(retiredMutation))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/promote", platformRoute(retiredMutation))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/rollback", platformRoute(retiredMutation))
	mux.Handle("POST /api/v1/runtime-changes/{ref}/reject", platformRoute(retiredMutation))
}
