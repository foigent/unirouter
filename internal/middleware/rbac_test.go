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
