package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerEnterpriseDirectoryRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	directoryRead func(http.HandlerFunc) http.Handler,
) {
	mux.Handle(
		"GET /api/v1/enterprise-directory/sources",
		directoryRead(handler.ListEnterpriseDirectorySources),
	)
	mux.Handle(
		"GET /api/v1/enterprise-directory/tree",
		directoryRead(handler.GetEnterpriseOrganizationTree),
	)
	mux.Handle(
		"GET /api/v1/enterprise-directory/iam-actions",
		directoryRead(handler.ListEnterpriseDirectoryIAMActions),
	)
}
