package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

const adminAuthTestEpoch int64 = 7

func adminSessionToken(t *testing.T, userID, orgID int64, role, secret string) string {
	t.Helper()
	token, _, err := auth.GenerateAdminSessionJWT(userID, orgID, role, adminAuthTestEpoch, secret, 1)
	if err != nil {
		t.Fatalf("generate admin session JWT: %v", err)
	}
	return token
}

func adminPrincipalDB(t *testing.T, wantUserID, wantOrgID int64, wantRole string, active bool, scanErr error) *testutil.MockDB {
	t.Helper()
	return &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		if len(args) != 5 || args[0] != wantUserID || args[1] != wantOrgID || args[2] != wantRole || args[3] != adminAuthTestEpoch {
			t.Fatalf("unexpected principal lookup args: %#v", args)
		}
		if sidHash, ok := args[4].([]byte); !ok || len(sidHash) != 32 {
			t.Fatalf("principal lookup must use a SHA-256 sid hash: %#v", args[4])
		}
		for _, fragment := range []string{"FROM users", "JOIN orgs", "JOIN admin_sessions", "u.auth_epoch", "s.revoked_at IS NULL", "s.expires_at > NOW()", "u.status = 'active'", "o.status = 'active'"} {
			if !strings.Contains(sql, fragment) {
				t.Fatalf("principal lookup missing %q: %s", fragment, sql)
			}
		}
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			if scanErr != nil {
				return scanErr
			}
			*dest[0].(*bool) = active
			return nil
		}}
	}}
}

func unexpectedAdminPrincipalDB(t *testing.T) *testutil.MockDB {
	t.Helper()
	return &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		t.Fatalf("principal state must not be queried before JWT validation: %s %#v", sql, args)
		return &testutil.MockRow{}
	}}
}

func TestAdminAuth_MissingHeader(t *testing.T) {
	handler := AdminAuth(unexpectedAdminPrincipalDB(t), "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminAuth_InvalidToken(t *testing.T) {
	handler := AdminAuth(unexpectedAdminPrincipalDB(t), "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminAuth_WrongSecret(t *testing.T) {
	token := adminSessionToken(t, 1, 1, "admin", "correct-secret")
	handler := AdminAuth(unexpectedAdminPrincipalDB(t), "wrong-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminAuth_ValidToken(t *testing.T) {
	secret := "test-secret"
	token := adminSessionToken(t, 42, 7, "admin", secret)
	handler := AdminAuth(adminPrincipalDB(t, 42, 7, "admin", true, nil), secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := GetOrgIDFromContext(r.Context())
		if orgID != 7 {
			t.Fatalf("expected orgID=7, got %d", orgID)
		}
		if userID := GetUserIDFromContext(r.Context()); userID != 42 {
			t.Fatalf("expected userID=42, got %d", userID)
		}
		if role := GetUserRoleFromContext(r.Context()); role != "admin" {
			t.Fatalf("expected role=admin, got %q", role)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAdminAuth_BearerPrefixRequired(t *testing.T) {
	token := adminSessionToken(t, 1, 1, "admin", "secret")
	handler := AdminAuth(unexpectedAdminPrincipalDB(t), "secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin/test", nil)
	req.Header.Set("Authorization", "Token "+token) // wrong prefix
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminAuth_InactivePrincipal(t *testing.T) {
	const secret = "test-secret"
	token := adminSessionToken(t, 42, 7, auth.RoleOrgAdmin, secret)
	called := false
	handler := AdminAuth(adminPrincipalDB(t, 42, 7, auth.RoleOrgAdmin, false, nil), secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("inactive principal reached protected handler")
	}
}

func TestAdminAuth_PrincipalStateUnavailable(t *testing.T) {
	const secret = "test-secret"
	token := adminSessionToken(t, 42, 7, auth.RoleOrgAdmin, secret)
	called := false
	handler := AdminAuth(adminPrincipalDB(t, 42, 7, auth.RoleOrgAdmin, false, fmt.Errorf("database unavailable")), secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("unavailable principal state reached protected handler")
	}
}

func TestAdminAuth_LegacyJWTRejectedBeforeStateLookup(t *testing.T) {
	now := time.Now().UTC()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: 42,
		OrgID:  7,
		Role:   auth.RoleOrgAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "uniroute",
		},
	}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("generate legacy JWT: %v", err)
	}
	handler := AdminAuth(unexpectedAdminPrincipalDB(t), "test-secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("legacy token reached protected handler")
	}))
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected legacy token to fail closed with 401, got %d", rec.Code)
	}
}

func TestGetRequestIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), CtxRequestID, "req-123")
	if GetRequestIDFromContext(ctx) != "req-123" {
		t.Fatal("should retrieve request ID")
	}
	if GetRequestIDFromContext(context.Background()) != "" {
		t.Fatal("empty context should return empty string")
	}
}

func TestGetAPIKeyFromContext(t *testing.T) {
	// nil context value
	if GetAPIKeyFromContext(context.Background()) != nil {
		t.Fatal("empty context should return nil")
	}
}

func TestGetOrgIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), CtxOrgID, int64(42))
	if GetOrgIDFromContext(ctx) != 42 {
		t.Fatal("should retrieve org ID")
	}
	if GetOrgIDFromContext(context.Background()) != 0 {
		t.Fatal("empty context should return 0")
	}
}

func TestGetProjectIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), CtxProjectID, int64(77))
	if GetProjectIDFromContext(ctx) != 77 {
		t.Fatal("should retrieve project ID")
	}
	if GetProjectIDFromContext(context.Background()) != 0 {
		t.Fatal("empty context should return 0")
	}
}

func TestGetKeyIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), CtxKeyID, int64(99))
	if GetKeyIDFromContext(ctx) != 99 {
		t.Fatal("should retrieve key ID")
	}
}

func TestHashKey_Deterministic(t *testing.T) {
	h1 := hashKey("ur-test-key", "secret")
	h2 := hashKey("ur-test-key", "secret")
	if h1 != h2 {
		t.Fatal("hashKey should be deterministic")
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex, got %d", len(h1))
	}
}

func TestHashKey_DifferentFromAuthHashKey(t *testing.T) {
	// middleware.hashKey uses HMAC-SHA256 with secret as key
	// auth.HashAPIKey uses HMAC-SHA256 with secret as key
	// They should produce the same result for same inputs
	h1 := hashKey("ur-test-key", "secret")
	h2 := auth.HashAPIKey("ur-test-key", "secret")
	if h1 != h2 {
		t.Fatal("middleware.hashKey and auth.HashAPIKey should produce same result")
	}
}

func TestGenerateRequestID_Format(t *testing.T) {
	id := generateRequestID()
	if len(id) != 35 { // "op_" + 32 hex chars
		t.Fatalf("expected 35-char operation ID, got %d: %s", len(id), id)
	}
	if id[:3] != "op_" {
		t.Fatalf("expected prefix 'op_', got %s", id[:3])
	}
}

func TestGenerateRequestID_Uniqueness(t *testing.T) {
	first := generateRequestID()
	second := generateRequestID()
	if first == "" || second == "" {
		t.Fatal("generateRequestID should not return empty")
	}
	if first == second {
		t.Fatal("secure operation IDs must be unique")
	}
}

func TestUniqueMember_Format(t *testing.T) {
	member := uniqueMember()
	// uniqueMember generates 8 random bytes → 16 hex chars
	if len(member) != 16 {
		t.Fatalf("expected 16-char hex member, got %d: %s", len(member), member)
	}
}

func TestUniqueMember_Uniqueness(t *testing.T) {
	m1 := uniqueMember()
	m2 := uniqueMember()
	// Extremely unlikely to collide
	if m1 == m2 {
		t.Fatal("consecutive uniqueMember calls should produce different values")
	}
}

func TestGetClientIP_XForwardedFor_SpacesOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "   ,  ")
	// After trimSpace, first part is empty string, should fall through to RemoteAddr
	ip := getClientIP(req)
	if ip == "" {
		t.Fatal("should return RemoteAddr when XFF has only spaces")
	}
}

func TestGetClientIP_RemoteAddr_NoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	// Override RemoteAddr with an address that has no port
	req.RemoteAddr = "10.0.0.55"
	ip := getClientIP(req)
	// SplitHostPort returns "", port error → returns raw RemoteAddr
	if ip != "10.0.0.55" {
		t.Fatalf("expected 10.0.0.55 (raw RemoteAddr), got %s", ip)
	}
}

// --- extractAPIKey tests ---

func TestExtractAPIKey_MissingHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := extractAPIKey(r)
	if err == nil {
		t.Fatal("expected error for missing header")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", authErr.Status)
	}
	if authErr.Code != "auth_error.invalid_api_key" {
		t.Fatalf("expected auth_error.invalid_api_key, got %s", authErr.Code)
	}
}

func TestExtractAPIKey_InvalidFormat(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Basic abc123")
	_, err := extractAPIKey(r)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", authErr.Status)
	}
}

func TestExtractAPIKey_EmptyKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer ")
	_, err := extractAPIKey(r)
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Message != "Empty API key" {
		t.Fatalf("expected 'Empty API key', got %s", authErr.Message)
	}
}

func TestExtractAPIKey_ValidKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer sk-abc123")
	key, err := extractAPIKey(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk-abc123" {
		t.Fatalf("expected sk-abc123, got %s", key)
	}
}

func TestExtractAPIKey_BearerOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer")
	_, err := extractAPIKey(r)
	if err == nil {
		t.Fatal("expected error for Bearer without space")
	}
}

func TestExtractAPIKey_CaseSensitive(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "bearer sk-abc123")
	_, err := extractAPIKey(r)
	if err == nil {
		t.Fatal("expected error for lowercase bearer")
	}
}

// --- validateKeyAndOrgStatus tests ---

func TestValidateKeyAndOrgStatus_InactiveKey(t *testing.T) {
	apiKey := &models.ApiKey{Status: "disabled"}
	err := validateKeyAndOrgStatus(apiKey, "active")
	if err == nil {
		t.Fatal("expected error for inactive key")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", authErr.Status)
	}
	if authErr.Code != "auth_error.api_key_disabled" {
		t.Fatalf("expected auth_error.api_key_disabled, got %s", authErr.Code)
	}
}

func TestValidateKeyAndOrgStatus_InactiveOrg(t *testing.T) {
	apiKey := &models.ApiKey{Status: "active"}
	err := validateKeyAndOrgStatus(apiKey, "suspended")
	if err == nil {
		t.Fatal("expected error for inactive org")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", authErr.Status)
	}
	if authErr.Code != "auth_error.org_suspended" {
		t.Fatalf("expected auth_error.org_suspended, got %s", authErr.Code)
	}
}

func TestValidateKeyAndOrgStatus_Active(t *testing.T) {
	apiKey := &models.ApiKey{Status: "active"}
	err := validateKeyAndOrgStatus(apiKey, "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateKeyAndOrgStatus_ExpiredKey(t *testing.T) {
	apiKey := &models.ApiKey{Status: "expired"}
	err := validateKeyAndOrgStatus(apiKey, "active")
	if err == nil {
		t.Fatal("expected error for expired key")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Message != "API key is expired" {
		t.Fatalf("expected 'API key is expired', got %s", authErr.Message)
	}
}

func TestValidateKeyAndOrgStatus_SuspendedOrg(t *testing.T) {
	apiKey := &models.ApiKey{Status: "active"}
	err := validateKeyAndOrgStatus(apiKey, "suspended")
	if err == nil {
		t.Fatal("expected error for suspended org")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError, got %T", err)
	}
	if authErr.Code != "auth_error.org_suspended" {
		t.Fatalf("expected auth_error.org_suspended, got %s", authErr.Code)
	}
}

// --- AuthError tests ---

func TestAuthError_Error(t *testing.T) {
	err := &AuthError{Code: "test.code", Message: "test message", Status: 400}
	if err.Error() != "test message" {
		t.Fatalf("expected 'test message', got %s", err.Error())
	}
}

func TestWriteGatewayAuthErrorIncludesDocURL(t *testing.T) {
	rec := httptest.NewRecorder()
	writeGatewayAuthError(rec, http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var body map[string]map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errObj := body["error"]
	if errObj["code"] != "auth_error.invalid_api_key" {
		t.Fatalf("expected code, got %#v", errObj["code"])
	}
	if errObj["doc_url"] != "/docs#error-auth_error.invalid_api_key" {
		t.Fatalf("expected doc_url, got %#v", errObj["doc_url"])
	}
}

// --- getClientIP boundary tests ---

func TestGetClientIP_XForwardedFor_MultipleIPs(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 9.10.11.12")
	ip := mustClientIPResolver(t, "192.0.2.1/32", "5.6.7.8/32", "9.10.11.12/32").Resolve(r)
	if ip != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_SingleIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	ip := mustClientIPResolver(t, "192.0.2.1/32").Resolve(r)
	if ip != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1, got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_Empty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "")
	r.RemoteAddr = "192.168.1.1:8080"
	ip := getClientIP(r)
	if ip != "192.168.1.1" {
		t.Fatalf("expected 192.168.1.1, got %s", ip)
	}
}

func TestGetClientIP_XRealIP_Only(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "172.16.0.1")
	ip := mustClientIPResolver(t, "192.0.2.1/32").Resolve(r)
	if ip != "172.16.0.1" {
		t.Fatalf("expected 172.16.0.1, got %s", ip)
	}
}

func TestGetClientIP_XRealIP_WithSpaces(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "  172.16.0.1  ")
	ip := mustClientIPResolver(t, "192.0.2.1/32").Resolve(r)
	if ip != "172.16.0.1" {
		t.Fatalf("expected 172.16.0.1, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddr_WithPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1:12345"
	ip := getClientIP(r)
	if ip != "192.168.1.1" {
		t.Fatalf("expected 192.168.1.1, got %s", ip)
	}
}

func TestGetClientIP_XForwardedFor_TakesPrecedence(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	r.Header.Set("X-Real-IP", "2.2.2.2")
	r.RemoteAddr = "3.3.3.3:8080"
	ip := mustClientIPResolver(t, "3.3.3.3/32").Resolve(r)
	if ip != "1.1.1.1" {
		t.Fatalf("expected 1.1.1.1 (X-Forwarded-For), got %s", ip)
	}
}

func TestGetClientIP_XRealIP_TakesPrecedenceOverRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "2.2.2.2")
	r.RemoteAddr = "3.3.3.3:8080"
	ip := mustClientIPResolver(t, "3.3.3.3/32").Resolve(r)
	if ip != "2.2.2.2" {
		t.Fatalf("expected 2.2.2.2 (X-Real-IP), got %s", ip)
	}
}

// --- checkIPWhitelist boundary tests ---

func TestCheckIPWhitelist_InvalidCIDR_Entry(t *testing.T) {
	// Invalid CIDR should be skipped
	result := checkIPWhitelist("192.168.1.1", "invalid-cidr")
	if result {
		t.Fatal("expected false for invalid CIDR")
	}
}

func TestCheckIPWhitelist_InvalidCIDR_WithValid(t *testing.T) {
	// Invalid CIDR should be skipped, valid one should match
	result := checkIPWhitelist("192.168.1.1", "invalid-cidr, 192.168.1.0/24")
	if !result {
		t.Fatal("expected true for valid CIDR after invalid one")
	}
}

func TestCheckIPWhitelist_IPv6_ExactMatch(t *testing.T) {
	result := checkIPWhitelist("::1", "::1")
	if !result {
		t.Fatal("expected true for IPv6 exact match")
	}
}

func TestCheckIPWhitelist_IPv6_CIDR(t *testing.T) {
	result := checkIPWhitelist("::1", "::1/128")
	if !result {
		t.Fatal("expected true for IPv6 CIDR match")
	}
}

func TestCheckIPWhitelist_IPv6_NoMatch(t *testing.T) {
	result := checkIPWhitelist("::1", "2001:db8::/32")
	if result {
		t.Fatal("expected false for IPv6 CIDR mismatch")
	}
}

func TestCheckIPWhitelist_MixedEntries(t *testing.T) {
	result := checkIPWhitelist("10.0.0.1", "192.168.1.1, 10.0.0.0/8, 172.16.0.1")
	if !result {
		t.Fatal("expected true for mixed entries with CIDR match")
	}
}

func TestCheckIPWhitelist_AllEmpty(t *testing.T) {
	result := checkIPWhitelist("", "")
	if !result {
		t.Fatal("expected true for empty whitelist")
	}
}

func TestCheckIPWhitelist_IPv4_ExactMismatch(t *testing.T) {
	result := checkIPWhitelist("192.168.1.2", "192.168.1.1")
	if result {
		t.Fatal("expected false for exact IP mismatch")
	}
}

func TestCheckIPWhitelist_IPv4_CIDR_Broadcast(t *testing.T) {
	// 192.168.1.255 is the broadcast address for 192.168.1.0/24
	result := checkIPWhitelist("192.168.1.255", "192.168.1.0/24")
	if !result {
		t.Fatal("expected true for broadcast address in /24")
	}
}

func TestCheckIPWhitelist_IPv4_CIDR_NetworkAddress(t *testing.T) {
	// 192.168.1.0 is the network address for 192.168.1.0/24
	result := checkIPWhitelist("192.168.1.0", "192.168.1.0/24")
	if !result {
		t.Fatal("expected true for network address in /24")
	}
}
