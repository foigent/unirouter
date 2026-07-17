package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uniroute/unirouter/internal/auth"
)

func TestRequireRoles_AllowsLegacyAdminAsPlatformAdmin(t *testing.T) {
	handler := RequireRoles(auth.RolePlatformAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), CtxUserRole, auth.RoleLegacyAdmin))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireRoles_DeniesBillingAdminForPlatformOnlyRoute(t *testing.T) {
	handler := RequireRoles(auth.RolePlatformAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels", nil)
	req = req.WithContext(context.WithValue(req.Context(), CtxUserRole, auth.RoleBillingAdmin))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequireRoles_DeniesOrganizationViewerForProjectViewerRoute(t *testing.T) {
	handler := RequireRoles(auth.RoleViewer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		role string
		want int
	}{
		{role: auth.RoleViewer, want: http.StatusNoContent},
		{role: auth.RoleOrgViewer, want: http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		req = req.WithContext(context.WithValue(req.Context(), CtxUserRole, tc.role))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != tc.want {
			t.Fatalf("role %s: expected %d, got %d: %s", tc.role, tc.want, rec.Code, rec.Body.String())
		}
	}
}

func TestRequireExactRoles_DistinguishesOrganizationViewerFromProjectViewer(t *testing.T) {
	handler := RequireExactRoles(auth.RoleOrgViewer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		role string
		want int
	}{
		{role: auth.RoleOrgViewer, want: http.StatusNoContent},
		{role: auth.RoleViewer, want: http.StatusForbidden},
		{role: auth.RoleDeveloper, want: http.StatusForbidden},
		{role: auth.RoleProjectAdmin, want: http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/stats", nil)
		req = req.WithContext(context.WithValue(req.Context(), CtxUserRole, tc.role))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("role %s: expected %d, got %d: %s", tc.role, tc.want, rec.Code, rec.Body.String())
		}
	}
}
