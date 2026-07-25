package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestXInsightFeedControlRoutesKeepSafetyActionsAvailableWithoutEntitlement(t *testing.T) {
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
		{name: "resume", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/resume", expansion: true},
		{name: "initial create", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/credentials", expansion: true},
		{name: "initial activate", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/credentials/activate", expansion: true},
		{name: "rotation prepare", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/rotation/prepare", expansion: true},
		{name: "rotation activate", method: http.MethodPost, target: "/api/v1/data-exchange/xinsight/connections/11/rotation/activate", expansion: true},
	}
	licenseStates := []struct {
		name string
		code string
		new  func(*testing.T) *licensing.Runtime
	}{
		{
			name: "missing",
			code: "license_error.missing",
			new: func(t *testing.T) *licensing.Runtime {
				t.Helper()
				return licensing.NewRuntime(licensing.NewCommercialVerifier(nil, nil, 30, "route-test"))
			},
		},
		{
			name: "expired",
			code: "license_error.expired",
			new:  expiredDataExchangeLicenseRuntime,
		},
	}

	for _, licenseState := range licenseStates {
		for _, route := range routes {
			t.Run(licenseState.name+"/"+route.name, func(t *testing.T) {
				runtime := licenseState.new(t)
				db := activeAdminPrincipalDB()
				platformRoute := func(handler http.HandlerFunc) http.Handler {
					return middleware.AdminAuth(db, "xinsight-route-license-secret")(
						middleware.RequireRoles(auth.RolePlatformAdmin)(handler),
					)
				}
				licensedPlatformRoute := func(handler http.HandlerFunc) http.Handler {
					gated := runtime.Gate(licensing.FeatureDataExchange)(http.HandlerFunc(handler))
					return platformRoute(http.HandlerFunc(gated.ServeHTTP))
				}
				handler := &recordingXInsightFeedControlHandler{}
				mux := http.NewServeMux()
				registerXInsightFeedControlRoutes(mux, handler, xInsightFeedControlRouteGuards{
					safety:    platformRoute,
					expansion: licensedPlatformRoute,
				})

				request := httptest.NewRequest(route.method, route.target, strings.NewReader(`{"staged_credential_id":17}`))
				request.Header.Set("Authorization", "Bearer "+activeAdminToken(
					t, 7, 11, auth.RolePlatformAdmin, "xinsight-route-license-secret",
				))
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)

				if route.expansion {
					require.Equal(t, http.StatusForbidden, response.Code)
					require.Contains(t, response.Body.String(), licenseState.code)
					require.Zero(t, handler.calls, "unlicensed expansion must not reach its handler")
					return
				}
				require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
				require.Equal(t, 1, handler.calls, "safety action must bypass only the feature entitlement gate")
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
		"Available for existing active, disabled or archived projects independent of the current Data Exchange entitlement.",
		"Authority-reducing safety action available for existing inactive projects and without the current Data Exchange entitlement.",
		"Requires an active project, current Data Exchange entitlement",
		"  /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/cancel:\n    x-unirouter-rbac:\n      post: [platform_admin]",
	} {
		require.Contains(t, spec, fragment)
	}
}

func expiredDataExchangeLicenseRuntime(t *testing.T) *licensing.Runtime {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	verifier := licensing.NewCommercialVerifier(
		map[string]ed25519.PublicKey{"route-test-key": publicKey},
		nil,
		30,
		"route-test",
	).WithNow(func() time.Time { return now })
	runtime := licensing.NewRuntime(verifier)
	document, err := licensing.SignCommercialPayload(licensing.Payload{
		LicenseID:    "route-test-expired",
		Customer:     "Route Test",
		DeploymentID: "route-test",
		Tier:         "enterprise",
		IssuedAt:     now.Add(-48 * time.Hour).Format(time.RFC3339),
		NotBefore:    now.Add(-48 * time.Hour).Format(time.RFC3339),
		ExpiresAt:    now.Add(-time.Hour).Format(time.RFC3339),
		Features:     []string{licensing.FeatureDataExchange},
	}, "route-test-key", privateKey)
	require.NoError(t, err)
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	require.Equal(t, licensing.StatusExpired, runtime.Replace(raw).Status)
	return runtime
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
