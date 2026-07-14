package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type globalControlRouteGuard func(http.HandlerFunc) http.Handler

// registerGlobalControlRoutes keeps deployment-wide settings, license and
// sensitive-word policy behind one production platform-authority boundary.
func registerGlobalControlRoutes(mux *http.ServeMux, handler *admin.Handler, platform, guardrail globalControlRouteGuard) {
	mux.Handle("GET /api/v1/settings", platform(handler.ListSettings))
	mux.Handle("PUT /api/v1/settings/{key}", platform(handler.UpdateSetting))
	mux.Handle("GET /api/v1/license", platform(handler.GetLicense))
	mux.Handle("PUT /api/v1/license", platform(handler.UpdateLicense))
	mux.Handle("GET /api/v1/sensitive-words", guardrail(handler.ListSensitiveWords))
	mux.Handle("POST /api/v1/sensitive-words", guardrail(handler.AddSensitiveWords))
	mux.Handle("PUT /api/v1/sensitive-words", guardrail(handler.UpdateSensitiveWords))
	mux.Handle("DELETE /api/v1/sensitive-words", guardrail(handler.DeleteSensitiveWords))
}
