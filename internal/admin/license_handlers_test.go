package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
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

func TestGetLicense_Missing(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("GET", "/api/v1/license", nil).WithContext(licenseRequestContext())
	rec := httptest.NewRecorder()

	h.GetLicense(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp licensing.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != licensing.StatusMissing {
		t.Fatalf("expected missing status, got %#v", resp)
	}
}

func TestUpdateLicense_ValidStoresAndAudits(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier := licensing.NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})
	licenseJSON := signedAdminLicense(t, privateKey, "2026-12-31T00:00:00Z")
	var savedSetting string
	var auditArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO settings"):
				savedSetting = args[1].(string)
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				auditArgs = append([]any(nil), args...)
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(globalControlTestDB(db))
	h.SetLicenseVerifier(verifier)

	req := httptest.NewRequest("PUT", "/api/v1/license", strings.NewReader(fmt.Sprintf(`{"license":%s}`, licenseJSON))).WithContext(licenseRequestContext())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "license-test")
	rec := httptest.NewRecorder()

	h.UpdateLicense(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp licensing.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != licensing.StatusActive || !resp.Valid {
		t.Fatalf("expected active license, got %#v", resp)
	}
	if !strings.Contains(savedSetting, `"deployment_id":"prod-cn-1"`) {
		t.Fatalf("expected normalized license saved, got %s", savedSetting)
	}
	if len(auditArgs) != 14 {
		t.Fatalf("expected audit args, got %#v", auditArgs)
	}
	if auditArgs[2] != "license.update" || auditArgs[3] != "license" || auditArgs[4] != "prod-cn-1" {
		t.Fatalf("expected license audit action/resource, got %#v", auditArgs)
	}
	if !strings.Contains(auditArgs[12].(string), `"customer":"Acme Corp"`) {
		t.Fatalf("expected after audit summary to include customer, got %v", auditArgs[12])
	}
	if strings.Contains(auditArgs[12].(string), `"signature":"`) {
		t.Fatalf("audit summary must not include raw signature: %v", auditArgs[12])
	}
}

func TestUpdateLicense_InvalidSignatureRejected(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	licenseJSON := signedAdminLicense(t, otherPrivateKey, "2026-12-31T00:00:00Z")
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			t.Fatalf("invalid license must not write database, SQL=%s", sql)
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)
	h.SetLicenseVerifier(licensing.NewVerifier(publicKey, 30))

	req := httptest.NewRequest("PUT", "/api/v1/license", strings.NewReader(fmt.Sprintf(`{"license":%s}`, licenseJSON))).WithContext(licenseRequestContext())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateLicense(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "signature verification failed") {
		t.Fatalf("expected signature failure response, got %s", rec.Body.String())
	}
}

func TestUpdateLicenseRejectsOversizedUnknownAndTrailingInput(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "oversized", body: `{"license":"` + strings.Repeat("x", maxLicenseImportBytes) + `"}`, status: http.StatusRequestEntityTooLarge},
		{name: "unknown wrapper field", body: `{"license":{},"replace":true}`, status: http.StatusBadRequest},
		{name: "trailing object", body: `{"license":{}} {"license":{}}`, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/license", strings.NewReader(test.body)).WithContext(licenseRequestContext())
			rec := httptest.NewRecorder()
			h.UpdateLicense(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), test.status)
			}
		})
	}
}

func TestUpdateLicense_CommercialPersistsEntitlementsAtomically(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	payload := licensing.Payload{
		LicenseID:    "lic-commercial-test",
		Customer:     "Commercial Customer",
		DeploymentID: "deployment-test",
		Tier:         "enterprise",
		IssuedAt:     now.Add(-time.Hour).Format(time.RFC3339),
		NotBefore:    now.Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt:    now.Add(365 * 24 * time.Hour).Format(time.RFC3339),
		Features:     []string{licensing.FeatureAll},
		Limits: map[string]int64{
			licensing.LimitActiveAPIKeys:  10,
			licensing.LimitActiveUsers:    20,
			licensing.LimitActiveProjects: 5,
			licensing.LimitActiveChannels: 4,
		},
	}
	doc, err := licensing.SignCommercialPayload(payload, "license-commercial-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	licenseJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	verifier := licensing.NewCommercialVerifier(
		map[string]ed25519.PublicKey{"license-commercial-a": publicKey}, nil, 30, "deployment-test",
	).WithNow(func() time.Time { return now })
	runtime := licensing.NewRuntime(verifier)

	var entitlementArgs []any
	var savedSetting bool
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "COUNT(*)") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 0
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO commercial_license_entitlements"):
				entitlementArgs = append([]any(nil), args...)
			case strings.Contains(sql, "INSERT INTO settings"):
				savedSetting = true
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	h.SetLicenseVerifier(verifier)
	h.SetLicenseRuntime(runtime)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/license",
		strings.NewReader(fmt.Sprintf(`{"license":%s}`, licenseJSON))).WithContext(licenseRequestContext())
	rec := httptest.NewRecorder()

	h.UpdateLicense(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !savedSetting || len(entitlementArgs) != 9 {
		t.Fatalf("commercial setting/entitlements not persisted: setting=%v args=%#v", savedSetting, entitlementArgs)
	}
	if entitlementArgs[0] != payload.LicenseID || entitlementArgs[1] != payload.DeploymentID || entitlementArgs[2] != "license-commercial-a" {
		t.Fatalf("unexpected entitlement identity: %#v", entitlementArgs)
	}
	if strings.Contains(fmt.Sprint(entitlementArgs), doc.Signature) {
		t.Fatal("raw License signature must not enter entitlement snapshot")
	}
	if status := runtime.Status(); !status.Enforceable || status.License.LicenseID != payload.LicenseID {
		t.Fatalf("runtime was not hot-reloaded: %#v", status)
	}
}

func TestUpdateLicense_RejectsLimitBelowCurrentUsage(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	payload := licensing.Payload{
		LicenseID: "lic-too-small", Customer: "Customer", DeploymentID: "deployment-test", Tier: "enterprise",
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339), Features: []string{licensing.FeatureGateway},
		Limits: map[string]int64{licensing.LimitActiveAPIKeys: 1},
	}
	doc, err := licensing.SignCommercialPayload(payload, "license-commercial-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	licenseJSON, _ := json.Marshal(doc)
	verifier := licensing.NewCommercialVerifier(
		map[string]ed25519.PublicKey{"license-commercial-a": publicKey}, nil, 30, "deployment-test",
	).WithNow(func() time.Time { return now })
	committed := false
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "COUNT(*) FROM api_keys") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { *dest[0].(*int64) = 2; return nil }}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO settings") || strings.Contains(sql, "INSERT INTO audit_logs") {
				t.Fatalf("conflicting License must not persist or audit as accepted: %s", sql)
			}
			return pgconn.NewCommandTag("SELECT 1"), nil
		},
		CommitFn: func(context.Context) error { committed = true; return nil },
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	h.SetLicenseVerifier(verifier)
	h.SetLicenseRuntime(licensing.NewRuntime(verifier))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/license",
		strings.NewReader(fmt.Sprintf(`{"license":%s}`, licenseJSON))).WithContext(licenseRequestContext())
	rec := httptest.NewRecorder()

	h.UpdateLicense(rec, req)

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "resource_limit_below_current_usage") {
		t.Fatalf("expected bounded conflict, got %d: %s", rec.Code, rec.Body.String())
	}
	if committed {
		t.Fatal("conflicting License transaction committed")
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
