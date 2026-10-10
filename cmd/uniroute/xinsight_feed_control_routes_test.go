package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestXInsightFeedControlRoutesRequirePlatformAuthorityWithoutCommercialLicense(t *testing.T) {
	routes := []struct {
		name      string
		method    string
		target    string
		expansion bool
	}{
		{name: "status", method: http.MethodGet, target: "/api/v1/data-exchange/xinsight/connections/11"},
		{name: "pause", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/pause"},
		{name: "revoke", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/credentials/17/revoke"},
		{name: "cancel", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/rotation/cancel"},
		{name: "connector install", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/install", expansion: true},
		{name: "resume", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/resume", expansion: true},
		{name: "initial create", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/credentials", expansion: true},
		{name: "initial activate", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/credentials/activate", expansion: true},
		{name: "rotation prepare", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/rotation/prepare", expansion: true},
		{name: "rotation activate", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/rotation/activate", expansion: true},
	}

	for _, identity := range []string{"missing", auth.RoleOrgAdmin, auth.RolePlatformAdmin} {
		for _, route := range routes {
			t.Run(identity+"/"+route.name, func(t *testing.T) {
				db := activeAdminPrincipalDB()
				platformRoute := func(handler http.HandlerFunc) http.Handler {
					return middleware.AdminAuth(db, "xinsight-route-secret")(middleware.RequireOrganizationRoles(auth.RolePlatformAdmin)(handler))
				}
				handler := &recordingXInsightFeedControlHandler{}
				mux := http.NewServeMux()
				registerXInsightFeedControlRoutes(mux, handler, xInsightFeedControlRouteGuards{safety: platformRoute, expansion: platformRoute})
				request := httptest.NewRequest(route.method, route.target, strings.NewReader(`{"staged_credential_id":17}`))
				if identity != "missing" {
					request.Header.Set("Authorization", "Bearer "+activeAdminToken(t, 7, 11, identity, "xinsight-route-secret"))
				}
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)
				if identity == auth.RolePlatformAdmin {
					require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
					require.Equal(t, 1, handler.calls)
				} else {
					require.Contains(t, []int{401, 403}, response.Code)
					require.Zero(t, handler.calls)
				}
				require.NotContains(t, response.Body.String(), "license_error")
			})
		}
	}

}

func TestMainWiresXInsightSafetyAndExpansionGuardsSeparately(t *testing.T) {
	source, err := os.ReadFile("main.go")
	require.NoError(t, err)
	require.Contains(t, string(source), `registerXInsightFeedControlRoutes(mux, adminHandler, xInsightFeedControlRouteGuards{
		safety:    platformRoute,
		expansion: dataExchangeRoute,
	})`)
}

func TestXInsightFeedControlOpenAPIDocumentsProjectAndSafetyBoundaries(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	require.NoError(t, err)
	spec := string(raw)
	for _, fragment := range []string{
		"project_status: { type: string, enum: [active, disabled, archived] }",
		"Available for existing active, disabled or archived projects without a commercial License.",
		"Authority-reducing safety action available for existing inactive projects and without a commercial License.",
		"Requires an active project",
		"The complete frozen invitation is validated without extensions",
		"  /api/v1/data-exchange/xinsight/connections/{project_id}/install:\n    x-unirouter-rbac:\n      post: [platform_admin]",
		"  /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/cancel:\n    x-unirouter-rbac:\n      post: [platform_admin]",
	} {
		require.Contains(t, spec, fragment)
	}
}

type recordingXInsightFeedControlHandler struct {
	calls int
}

func (h *recordingXInsightFeedControlHandler) respond(w http.ResponseWriter) {
	h.calls++
	w.WriteHeader(http.StatusNoContent)
}

func (h *recordingXInsightFeedControlHandler) GetXInsightFeedConnection(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) InstallXInsightConnector(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) PauseXInsightFeedConnection(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) ResumeXInsightFeedConnection(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) CreateXInsightFeedCredential(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) ActivateInitialXInsightFeedCredential(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) RevokeXInsightFeedCredential(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) PrepareXInsightFeedRotation(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) ActivateXInsightFeedRotation(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}

func (h *recordingXInsightFeedControlHandler) CancelXInsightFeedRotation(w http.ResponseWriter, _ *http.Request) {
	h.respond(w)
}
