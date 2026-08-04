package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

// --- Mock DBQuerier for middleware tests ---

// --- resolveAPIKey tests ---

func TestResolveAPIKey_Success(t *testing.T) {
	budgetLimit := 10000
	var verifierScheme any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) != 2 {
				t.Fatalf("expected hash and verifier scheme, got %d arguments", len(args))
			}
			verifierScheme = args[1]
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1            // id
					*dest[1].(*int64) = 1            // org_id
					*dest[2].(*int64) = 11           // project_id
					*dest[3].(*string) = "active"    // status
					*dest[4].(*string) = ""          // allowed_ips
					*dest[5].(*string) = ""          // allowed_models
					*dest[6].(*int) = 60             // rpm_limit
					*dest[7].(*int) = 10             // qps_limit
					*dest[8].(**int) = &budgetLimit  // budget_limit
					*dest[9].(*string) = "sk-ab12"   // key_prefix
					*dest[10].(*string) = "test"     // name
					*dest[11].(*int64) = 42          // owner_user_id
					*dest[12].(*string) = "personal" // key_type
					*dest[13].(*string) = ""         // purpose
					*dest[14].(*int64) = 7           // auth_epoch
					*dest[15].(*string) = "active"   // org_status
					return nil
				},
			}
		},
	}

	apiKey, orgStatus, err := resolveAPIKey(context.Background(), db, "hash123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if apiKey.ID != 1 {
		t.Fatalf("expected ID=1, got %d", apiKey.ID)
	}
	if apiKey.OrgID != 1 {
		t.Fatalf("expected OrgID=1, got %d", apiKey.OrgID)
	}
	if apiKey.ProjectID == nil || *apiKey.ProjectID != 11 {
		t.Fatalf("expected ProjectID=11, got %v", apiKey.ProjectID)
	}
	if orgStatus != "active" {
		t.Fatalf("expected orgStatus=active, got %s", orgStatus)
	}
	if apiKey.RPMLimit != 60 {
		t.Fatalf("expected RPMLimit=60, got %d", apiKey.RPMLimit)
	}
	if apiKey.OwnerUserID == nil || *apiKey.OwnerUserID != 42 {
		t.Fatalf("expected OwnerUserID=42, got %v", apiKey.OwnerUserID)
	}
	if apiKey.KeyType != "personal" {
		t.Fatalf("expected KeyType=personal, got %s", apiKey.KeyType)
	}
	if apiKey.AuthEpoch != 7 {
		t.Fatalf("expected AuthEpoch=7, got %d", apiKey.AuthEpoch)
	}
	if verifierScheme != auth.LegacyAPIVerifierScheme {
		t.Fatalf("general gateway resolver accepted non-legacy verifier scheme: %v", verifierScheme)
	}
}

func TestResolveAPIKey_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}

	_, _, err := resolveAPIKey(context.Background(), db, "invalid-hash")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

// --- GatewayAuth tests ---

func TestGatewayAuth_Success(t *testing.T) {
	budgetLimit := 10000
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 11
					*dest[3].(*string) = "active"
					*dest[4].(*string) = ""
					*dest[5].(*string) = ""
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(**int) = &budgetLimit
					*dest[9].(*string) = "sk-ab12"
					*dest[10].(*string) = "test"
					*dest[11].(*int64) = 42
					*dest[12].(*string) = "personal"
					*dest[13].(*string) = ""
					*dest[14].(*int64) = 1
					*dest[15].(*string) = "active"
					return nil
				},
			}
		},
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context values are set
		keyID := GetKeyIDFromContext(r.Context())
		if keyID != 1 {
			t.Errorf("expected keyID=1, got %d", keyID)
		}
		orgID := GetOrgIDFromContext(r.Context())
		if orgID != 1 {
			t.Errorf("expected orgID=1, got %d", orgID)
		}
		projectID := GetProjectIDFromContext(r.Context())
		if projectID != 11 {
			t.Errorf("expected projectID=11, got %d", projectID)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	// Generate a valid API key
	rawKey := "sk-test-key-12345"

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayAuthWithAPIKeyHashSecret_UsesSeparateSecret(t *testing.T) {
	rawKey := "ur-0001-separate-secret"
	expectedHash := hashKey(rawKey, "api-key-secret")
	var capturedHash string
	budgetLimit := 10000
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) == 2 && args[0] == int64(1) && args[1] == int64(1) {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*bool) = false
					return nil
				}}
			}
			capturedHash = args[0].(string)
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 11
					*dest[3].(*string) = "active"
					*dest[4].(*string) = ""
					*dest[5].(*string) = ""
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(**int) = &budgetLimit
					*dest[9].(*string) = "ur-0001"
					*dest[10].(*string) = "test"
					*dest[11].(*int64) = 0
					*dest[12].(*string) = "shared"
					*dest[13].(*string) = ""
					*dest[14].(*int64) = 1
					*dest[15].(*string) = "active"
					return nil
				},
			}
		},
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := GatewayAuthWithAPIKeyHashSecret(db, "api-key-secret")(inner)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedHash != expectedHash {
		t.Fatalf("expected hash from API key secret %s, got %s", expectedHash, capturedHash)
	}
	if capturedHash == hashKey(rawKey, "jwt-secret") {
		t.Fatal("expected API key hash not to use JWT secret")
	}
}

func TestGatewayAuth_MissingHeader(t *testing.T) {
	db := &testutil.MockDB{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGatewayAuth_InvalidKey(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-invalid-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGatewayAuth_DisabledKey(t *testing.T) {
	budgetLimit := 10000
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 11
					*dest[3].(*string) = "disabled" // key disabled
					*dest[4].(*string) = ""
					*dest[5].(*string) = ""
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(**int) = &budgetLimit
					*dest[9].(*string) = "sk-ab12"
					*dest[10].(*string) = "test"
					*dest[11].(*int64) = 0
					*dest[12].(*string) = "shared"
					*dest[13].(*string) = ""
					*dest[14].(*int64) = 1
					*dest[15].(*string) = "active"
					return nil
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	rawKey := "sk-test-key-12345"
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGatewayAuth_DisabledOrg(t *testing.T) {
	budgetLimit := 10000
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 11
					*dest[3].(*string) = "active"
					*dest[4].(*string) = ""
					*dest[5].(*string) = ""
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(**int) = &budgetLimit
					*dest[9].(*string) = "sk-ab12"
					*dest[10].(*string) = "test"
					*dest[11].(*int64) = 0
					*dest[12].(*string) = "shared"
					*dest[13].(*string) = ""
					*dest[14].(*int64) = 1
					*dest[15].(*string) = "disabled" // org disabled
					return nil
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	rawKey := "sk-test-key-12345"
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestGatewayAuth_IPWhitelist(t *testing.T) {
	budgetLimit := 10000
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 11
					*dest[3].(*string) = "active"
					*dest[4].(*string) = "10.0.0.0/8" // only allow internal IPs
					*dest[5].(*string) = ""
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(**int) = &budgetLimit
					*dest[9].(*string) = "sk-ab12"
					*dest[10].(*string) = "test"
					*dest[11].(*int64) = 0
					*dest[12].(*string) = "shared"
					*dest[13].(*string) = ""
					*dest[14].(*int64) = 1
					*dest[15].(*string) = "active"
					return nil
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	rawKey := "sk-test-key-12345"
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.RemoteAddr = "198.51.100.20:4321"
	req.Header.Set("X-Forwarded-For", "10.0.0.1") // spoofed allowlisted IP
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an untrusted peer spoofing an allowlisted XFF, got %d", rec.Code)
	}
}

func TestGatewayAuth_IPWhitelistUsesTrustedProxyChain(t *testing.T) {
	budgetLimit := 10000
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 11
					*dest[3].(*string) = "active"
					*dest[4].(*string) = "203.0.113.77"
					*dest[5].(*string) = ""
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(**int) = &budgetLimit
					*dest[9].(*string) = "sk-ab12"
					*dest[10].(*string) = "test"
					*dest[11].(*int64) = 0
					*dest[12].(*string) = "shared"
					*dest[13].(*string) = ""
					*dest[14].(*int64) = 1
					*dest[15].(*string) = "active"
					return nil
				},
			}
		},
	}
	resolver := mustClientIPResolver(t, "172.30.250.10/32", "172.30.250.1/32")
	handler := GatewayAuthWithAgentCapabilitiesAndClientIP(db, "test-secret", nil, resolver)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-test-key-12345")
	req.RemoteAddr = "172.30.250.10:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.77, 172.30.250.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected trusted two-proxy chain to preserve allowlisted browser IP, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Suppress unused import
var _ = pgtype.Int4{}
