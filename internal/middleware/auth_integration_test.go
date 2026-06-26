//go:build integration

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

// GatewayAuth integration tests — verify the API key authentication middleware with a real DB.

func TestGatewayAuth_ValidKey_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	// Generate and store a real API key
	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'test-key', $2, 'ur-0001-', 'active', 60, 10, '', 'gpt-4,gpt-3.5')`,
		orgID, keyHash,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context injection
		require.NotNil(t, GetAPIKeyFromContext(r.Context()))
		require.Equal(t, orgID, GetOrgIDFromContext(r.Context()))
		require.NotZero(t, GetKeyIDFromContext(r.Context()))
		require.NotEmpty(t, GetRequestIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestGatewayAuth_MissingHeader_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGatewayAuth_WrongPrefix_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGatewayAuth_InvalidKey_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer ur-0001-deadbeefdeadbeefdeadbeefdeadbeef")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
}

func TestGatewayAuth_DisabledKey_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'disabled-key', $2, 'ur-0001-', 'disabled', 60, 10, '', '')`,
		orgID, keyHash,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "disabled")
}

func TestGatewayAuth_DisabledOrg_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	// Create a disabled org
	disabledOrg := testutil.NewTestOrg(t, pool)
	_, err := pool.Exec(t.Context(), `UPDATE orgs SET status = 'disabled' WHERE id = $1`, disabledOrg)
	require.NoError(t, err)

	rawKey, _, err := auth.GenerateAPIKey(disabledOrg)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'org-disabled-key', $2, 'ur-0001-', 'active', 60, 10, '', '')`,
		disabledOrg, keyHash,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "disabled")
}

func TestGatewayAuth_IPBlocked_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	// Allow only 10.0.0.1
	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'ip-key', $2, 'ur-0001-', 'active', 60, 10, '10.0.0.1', '')`,
		orgID, keyHash,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	// httptest.NewRequest sets RemoteAddr to 192.0.2.1:1234, which is NOT 10.0.0.1
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
}

func TestGatewayAuth_IPAllowed_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	// Allow 192.0.2.1 (the default RemoteAddr in httptest)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'ip-allow-key', $2, 'ur-0001-', 'active', 60, 10, '192.0.2.1', '')`,
		orgID, keyHash,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestGatewayAuth_RequestIDFromHeader_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'reqid-key', $2, 'ur-0001-', 'active', 60, 10, '', '')`,
		orgID, keyHash,
	)
	require.NoError(t, err)

	var capturedReqID string
	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReqID = GetRequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Request-ID", "custom-req-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "custom-req-12345", capturedReqID, "should use X-Request-ID from header")
}

func TestGatewayAuth_ContextInjection_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'ctx-key', $2, 'ur-0001-', 'active', 60, 10, '', 'gpt-4')`,
		orgID, keyHash,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		apiKey := GetAPIKeyFromContext(ctx)
		require.NotNil(t, apiKey)
		require.Equal(t, "ctx-key", apiKey.Name)
		require.Equal(t, orgID, apiKey.OrgID)
		require.Equal(t, int64(60), int64(apiKey.RPMLimit))

		require.Equal(t, orgID, GetOrgIDFromContext(ctx))
		require.NotZero(t, GetKeyIDFromContext(ctx))
		require.NotEmpty(t, GetRequestIDFromContext(ctx))

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

// Test AdminAuth verify context injection after valid token.
func TestAdminAuth_ContextInjection_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	_ = pool // pool not needed for AdminAuth (no DB dependency)

	secret := "admin-test-secret"
	token, err := auth.GenerateJWT(1, orgID, "admin", secret, 1)
	require.NoError(t, err)

	handler := AdminAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, orgID, GetOrgIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}
