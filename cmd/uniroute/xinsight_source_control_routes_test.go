package main

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recordingSourceControl struct{ calls int }

func (h *recordingSourceControl) GetXInsightSourceConnection(w http.ResponseWriter, r *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *recordingSourceControl) SourceControl(string) http.HandlerFunc {
	return h.GetXInsightSourceConnection
}

func TestXInsightSourceRoutesEnforceAdminAndEntitlementBoundaries(t *testing.T) {
	for _, identity := range []string{"missing", "source_credential", auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RolePlatformAdmin} {
		for _, action := range []string{"", "pause", "cancel", "revoke", "prepare", "rotate", "activate", "resume", "profiles"} {
			t.Run(identity+"/"+action, func(t *testing.T) {
				h := &recordingSourceControl{}
				db := activeAdminPrincipalDB()
				runtime := licensing.NewRuntime(licensing.NewCommercialVerifier(nil, nil, 30, "source-route-test"))
				safety := func(next http.HandlerFunc) http.Handler {
					return middleware.AdminAuth(db, "source-route-secret")(middleware.RequireRoles(auth.RolePlatformAdmin)(next))
				}
				expansion := func(next http.HandlerFunc) http.Handler {
					return safety(runtime.Gate(licensing.FeatureDataExchange)(next).ServeHTTP)
				}
				mux := http.NewServeMux()
				registerXInsightSourceControlRoutes(mux, h, xInsightFeedControlRouteGuards{safety: safety, expansion: expansion})
				path := "/api/v1/data-exchange/xinsight/source-connections/11"
				method := "GET"
				if action != "" {
					path += "/" + action
					method = "POST"
				}
				r := httptest.NewRequest(method, path, nil)
				if identity == "source_credential" {
					r.Header.Set("Authorization", "Bearer ur-xf1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				} else if identity != "missing" {
					r.Header.Set("Authorization", "Bearer "+activeAdminToken(t, 7, 11, identity, "source-route-secret"))
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				available := identity == auth.RolePlatformAdmin && (action == "" || action == "pause" || action == "cancel" || action == "revoke")
				if available {
					require.Equal(t, 204, w.Code, w.Body.String())
					require.Equal(t, 1, h.calls)
				} else {
					require.Contains(t, []int{401, 403}, w.Code, w.Body.String())
					require.Zero(t, h.calls)
				}
			})
		}
	}
}
