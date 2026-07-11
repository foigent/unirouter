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
	if len(auditArgs) != 13 {
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
