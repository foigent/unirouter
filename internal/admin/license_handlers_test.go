package admin

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func signedAdminLicense(t *testing.T, privateKey ed25519.PrivateKey, expiresAt string) []byte {
	t.Helper()
	doc, err := licensing.SignPayload(licensing.Payload{
		Customer:     "Acme Corp",
		DeploymentID: "prod-cn-1",
		Tier:         "enterprise",
		IssuedAt:     "2026-07-01T00:00:00Z",
		ExpiresAt:    expiresAt,
		Features:     []string{"projects", "native_adapters"},
		Limits:       map[string]int64{"projects": 20},
	}, privateKey)
	if err != nil {
		t.Fatalf("sign license: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal license: %v", err)
	}
	return raw
}

func licenseRequestContext() context.Context {
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-license")
	return ctx
}

func TestRetiredLicenseEndpointsAreAuthorizedNoOps(t *testing.T) {
	// A nil database is deliberate: even a read/transaction would panic.
	h := newTestHandler(nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := httptest.NewRequest(method, "/api/v1/license", strings.NewReader(`{"license":{"signature":"never-echo-me","payload":{"expired":true}}}`)).WithContext(licenseRequestContext())
		rec := httptest.NewRecorder()
		if method == http.MethodGet {
			h.GetLicense(rec, req)
		} else {
			h.UpdateLicense(rec, req)
		}
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", method, rec.Code, rec.Body.String())
		}
		var status licensing.Status
		if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Status != licensing.StatusNotRequired || status.Valid || status.Enforceable || status.SignatureValid || status.Commercial || status.License != nil || status.Warning || status.VerifierConfigured {
			t.Fatalf("false license proof: %+v", status)
		}
		if strings.Contains(rec.Body.String(), "never-echo-me") {
			t.Fatal("document leaked")
		}
		if method == http.MethodPut && !strings.Contains(rec.Body.String(), `"imported":false`) {
			t.Fatal("missing no-op result")
		}
		for _, role := range []string{auth.RoleOrgAdmin, auth.RoleMember, ""} {
			denied := req.WithContext(context.WithValue(licenseRequestContext(), middleware.CtxUserRole, role))
			rr := httptest.NewRecorder()
			if method == http.MethodGet {
				h.GetLicense(rr, denied)
			} else {
				h.UpdateLicense(rr, denied)
			}
			if rr.Code == 200 {
				t.Fatalf("role %q bypassed platform authorization", role)
			}
		}
	}
}
func TestUpdateLicenseRejectsMalformedOrUnboundedInput(t *testing.T) {
	h := newTestHandler(nil)
	cases := []struct {
		body   string
		status int
	}{
		{`{"license":"` + strings.Repeat("x", maxLicenseImportBytes) + `"}`, 413},
		{`{"license":{"old":true},"replace":true}`, 400},
		{`{"license":{"old":true}} {}`, 400},
		{`{"license":{"old":true},"license":{"other":true}}`, 400},
		{`{"license":{"old":true,"old":false}}`, 400},
		{`{"license":null}`, 400}, {`{"license":[]}`, 400}, {`{"license":{}}`, 400},
		{`{}`, 400}, {`null`, 400}, {`{"license":"invalid json"}`, 400},
		{`{"license":"{\"old\":true}"}`, 200},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/license", strings.NewReader(tc.body)).WithContext(licenseRequestContext())
		rec := httptest.NewRecorder()
		h.UpdateLicense(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("got %d want %d: %s", rec.Code, tc.status, rec.Body.String())
		}
	}
}
func TestActiveChannelsCommercialUsageCountsCanonicalEnabledConnections(t *testing.T) {
	var query string
	tx := &testutil.MockTx{QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
		query = sql
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = 3
			return nil
		}}
	}}
	current, err := countCommercialResourceUsage(t.Context(), tx, commercialResource{
		name: licensing.LimitActiveChannels, table: "provider_connections",
	})
	if err != nil || current != 3 {
		t.Fatalf("canonical active connection count = %d, %v", current, err)
	}
	if !strings.Contains(query, "FROM provider_connections") || !strings.Contains(query, "admin_state='enabled'") ||
		strings.Contains(query, "FROM channels") {
		t.Fatalf("active_channels must use only canonical enabled connections: %s", query)
	}
}

func TestWriteCommercialLimitViolationUsesConsistentHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantStatus int
		wantCode   string
	}{
		{name: "capacity", message: "commercial License limit exceeded", wantStatus: http.StatusConflict, wantCode: "license_error.resource_limit_exceeded"},
		{name: "expired", message: "commercial License is expired", wantStatus: http.StatusForbidden, wantCode: "license_error.expired"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handled := writeCommercialLimitViolation(recorder, &pgconn.PgError{
				Message:        test.message,
				ConstraintName: "commercial_license_active_users",
			})
			if !handled || recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), test.wantCode) {
				t.Fatalf("handled=%v status=%d body=%s", handled, recorder.Code, recorder.Body.String())
			}
		})
	}
}
