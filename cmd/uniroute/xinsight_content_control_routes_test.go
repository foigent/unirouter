package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

type recordingContentControl struct{ calls int }

func (h *recordingContentControl) GetXInsightContentConnection(w http.ResponseWriter, r *http.Request) {
	h.calls++
	w.WriteHeader(204)
}
func (h *recordingContentControl) ContentControl(string) http.HandlerFunc {
	return h.GetXInsightContentConnection
}

func TestXInsightContentRoutesEnforceAdminBoundariesWithoutCommercialLicense(t *testing.T) {
	for _, identity := range []string{"missing", "source_credential", auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RolePlatformAdmin} {
		for _, action := range []string{"", "pause", "cancel", "revoke", "prepare", "rotate", "activate", "resume"} {
			t.Run(identity+"/"+action, func(t *testing.T) {
				h := &recordingContentControl{}
				database := activeAdminPrincipalDB()
				safety := func(next http.HandlerFunc) http.Handler {
					return middleware.AdminAuth(database, "content-route-secret")(middleware.RequireOrganizationRoles(auth.RolePlatformAdmin)(next))
				}
				expansion := safety
				mux := http.NewServeMux()
				registerXInsightContentControlRoutes(mux, h, xInsightFeedControlRouteGuards{safety: safety, expansion: expansion})
				path := "/api/v1/data-exchange/xinsight/source-connections/11/content"
				method := "GET"
				if action != "" {
					path += "/" + action
					method = "POST"
				}
				r := httptest.NewRequest(method, path, nil)
				if identity == "source_credential" {
					r.Header.Set("Authorization", "Bearer ur-xf1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				} else if identity != "missing" {
					r.Header.Set("Authorization", "Bearer "+activeAdminToken(t, 7, 11, identity, "content-route-secret"))
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				available := identity == auth.RolePlatformAdmin
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
