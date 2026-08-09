package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerWorkloadApplicationRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	authenticatedRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle("GET /api/v1/workload-applications/contract", authenticatedRoute(handler.GetWorkloadApplicationContract))
	mux.Handle("GET /api/v1/workload-applications", authenticatedRoute(handler.ListWorkloadApplications))
	mux.Handle("POST /api/v1/workload-applications", authenticatedRoute(handler.CreateWorkloadApplication))
	mux.Handle("GET /api/v1/workload-applications/{ref}", authenticatedRoute(handler.GetWorkloadApplication))
	mux.Handle("PUT /api/v1/workload-applications/{ref}", authenticatedRoute(handler.UpdateWorkloadApplication))
	mux.Handle("POST /api/v1/workload-applications/{ref}/archive", authenticatedRoute(handler.ArchiveWorkloadApplication))
	mux.Handle("GET /api/v1/workload-applications/{ref}/credentials", authenticatedRoute(handler.ListWorkloadApplicationCredentials))
	mux.Handle("POST /api/v1/workload-applications/{ref}/credentials/{key_id}/bind", authenticatedRoute(handler.BindWorkloadApplicationCredential))
	mux.Handle("GET /api/v1/workload-applications/{ref}/readiness", authenticatedRoute(handler.GetWorkloadApplicationReadiness))
}
