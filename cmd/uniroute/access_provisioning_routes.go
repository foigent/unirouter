package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

func registerAccessProvisioningRoutes(
	mux *http.ServeMux,
	handler *admin.Handler,
	authenticated func(http.HandlerFunc) http.Handler,
	organizationAdmin func(http.HandlerFunc) http.Handler,
) {
	mux.Handle("POST /api/v1/access-requests", authenticated(handler.CreateAccessRequest))
	mux.Handle("GET /api/v1/access-requests", authenticated(handler.ListAccessRequests))
	mux.Handle("GET /api/v1/access-requests/{ref}", authenticated(handler.GetAccessRequest))
	mux.Handle("POST /api/v1/access-requests/{ref}/decision", authenticated(handler.DecideAccessRequest))
	mux.Handle("POST /api/v1/access-requests/{ref}/cancel", authenticated(handler.CancelAccessRequest))

	mux.Handle("GET /api/v1/access-grants", authenticated(handler.ListAccessGrants))
	mux.Handle("GET /api/v1/access-grants/{ref}", authenticated(handler.GetAccessGrant))
	mux.Handle("GET /api/v1/access-grants/{ref}/credentials", authenticated(handler.ListAccessGrantCredentials))
	mux.Handle("POST /api/v1/access-grants/{ref}/suspend", authenticated(handler.SuspendAccessGrant))
	mux.Handle("POST /api/v1/access-grants/{ref}/resume", authenticated(handler.ResumeAccessGrant))
	mux.Handle("POST /api/v1/access-grants/{ref}/revoke", authenticated(handler.RevokeAccessGrant))
	mux.Handle("POST /api/v1/access-grants/{ref}/credentials", authenticated(handler.IssueAccessGrantCredential))
	mux.Handle("POST /api/v1/access-grants/{ref}/credentials/{key_id}/rotate", authenticated(handler.RotateAccessGrantCredential))
	mux.Handle("POST /api/v1/access-grants/{ref}/credentials/{key_id}/revoke", authenticated(handler.RevokeAccessGrantCredential))

	mux.Handle("GET /api/v1/access-provisioning/legacy-preview", organizationAdmin(handler.PreviewLegacyAccessClassification))
}
