package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerWorkloadApplicationRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	orgAdminRoute func(http.HandlerFunc) http.Handler,
) {
	mux.Handle("GET /api/v1/workload-applications/contract", orgAdminRoute(handler.GetWorkloadApplicationContract))
	mux.Handle("GET /api/v1/workload-applications", orgAdminRoute(handler.ListWorkloadApplications))
	mux.Handle("POST /api/v1/workload-applications", orgAdminRoute(handler.CreateWorkloadApplication))
	mux.Handle("GET /api/v1/workload-applications/{ref}", orgAdminRoute(handler.GetWorkloadApplication))
	mux.Handle("PUT /api/v1/workload-applications/{ref}", orgAdminRoute(handler.UpdateWorkloadApplication))
	mux.Handle("POST /api/v1/workload-applications/{ref}/archive", orgAdminRoute(handler.ArchiveWorkloadApplication))
	mux.Handle("GET /api/v1/workload-applications/{ref}/credentials", orgAdminRoute(handler.ListWorkloadApplicationCredentials))
	mux.Handle("POST /api/v1/workload-applications/{ref}/credentials/{key_id}/bind", orgAdminRoute(handler.BindWorkloadApplicationCredential))
	mux.Handle("GET /api/v1/workload-applications/{ref}/readiness", orgAdminRoute(handler.GetWorkloadApplicationReadiness))
}
