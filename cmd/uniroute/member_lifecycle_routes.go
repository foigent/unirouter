package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerMemberLifecycleRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	directoryRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/member-lifecycle/offline-contract",
		directoryRoute(handler.MemberLifecycleOfflineContract),
	)
	mux.Handle(
		"POST /api/v1/member-lifecycle/offline-preview",
		directoryRoute(handler.PreviewMemberLifecycle),
	)
}
