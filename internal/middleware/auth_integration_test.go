//go:build integration

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

// GatewayAuth integration tests — verify the API key authentication middleware with a real DB.

func TestGatewayAuth_ValidKey_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	// Generate and store a real API key with the legacy 12-character display
	// prefix. Authentication is hash-based, so existing rows remain valid after
	// newly generated display prefixes expand to 20 characters.
	rawKey, _, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")

	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models)
		 VALUES ($1, 'test-key', $2, $3, 'active', 60, 10, '', 'gpt-4,gpt-3.5')`,
		orgID, keyHash, rawKey[:12],
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
	pool := testutil.SetupMigratedTestDB(t)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGatewayAuth_WrongPrefix_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)

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
	pool := testutil.SetupMigratedTestDB(t)

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
	pool := testutil.SetupMigratedTestDB(t)
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
	pool := testutil.SetupMigratedTestDB(t)

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

func TestGatewayAuth_PersonalProjectAuthorityRevocation_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	var userID, projectID int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'gateway-personal-owner', 'unused', $2, 'active') RETURNING id`,
		orgID, auth.RoleDeveloper,
	).Scan(&userID))
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO projects (org_id, name, slug, status)
		 VALUES ($1, 'Gateway authority project', 'gateway-authority-project', 'active') RETURNING id`,
		orgID,
	).Scan(&projectID))
	_, err := pool.Exec(t.Context(),
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_developer')`,
		projectID, userID,
	)
	require.NoError(t, err)

	rawKey, keyPrefix, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	keyHash := auth.HashAPIKey(rawKey, "test-secret")
	_, err = pool.Exec(t.Context(),
		`INSERT INTO api_keys (
			org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
			status, rpm_limit, qps_limit, allowed_ips, allowed_models
		 ) VALUES ($1, $2, $3, 'personal', 'gateway-personal-key', $4, $5, 'active', 60, 10, '', '')`,
		orgID, projectID, userID, keyHash, keyPrefix,
	)
	require.NoError(t, err)

	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serve := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, serve().Code)
	_, err = pool.Exec(t.Context(), `UPDATE projects SET status = 'disabled' WHERE id = $1`, projectID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)
	_, err = pool.Exec(t.Context(), `UPDATE projects SET status = 'active' WHERE id = $1`, projectID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE users SET status = 'disabled' WHERE id = $1`, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)
	_, err = pool.Exec(t.Context(), `UPDATE users SET status = 'active' WHERE id = $1`, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE project_members SET role='project_viewer' WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code, "legacy personal keys must stop calling after viewer downgrade")
	_, err = pool.Exec(t.Context(), `UPDATE project_members SET role='project_user' WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)
	_, err = pool.Exec(t.Context(), `UPDATE project_members SET role='project_admin' WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)

	_, err = pool.Exec(t.Context(), `DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)
}

func TestGatewayAuth_IPBlocked_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
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
	pool := testutil.SetupMigratedTestDB(t)
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
	pool := testutil.SetupMigratedTestDB(t)
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
	var capturedClientReqID string
	handler := GatewayAuth(pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReqID = GetRequestIDFromContext(r.Context())
		capturedClientReqID = GetClientRequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Request-ID", "custom-req-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEqual(t, "custom-req-12345", capturedReqID, "client correlation must not become the server operation identity")
	require.True(t, strings.HasPrefix(capturedReqID, "op_"), capturedReqID)
	require.Equal(t, "custom-req-12345", capturedClientReqID)
}

func TestGatewayAuth_ContextInjection_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
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

// TestAdminAuth_DisabledUserTokenRejected_Integration proves that an already
// issued admin JWT stops authorizing requests as soon as the user is disabled.
func TestAdminAuth_DisabledUserTokenRejected_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	var userID int64
	err := pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'admin-auth-revocation', 'unused', $2, 'active')
		 RETURNING id`,
		orgID, auth.RoleOrgAdmin,
	).Scan(&userID)
	require.NoError(t, err)

	secret := "admin-test-secret"
	token, session, err := auth.GenerateAdminSessionJWT(userID, orgID, auth.RoleOrgAdmin, auth.RoleOrgAdmin, 0, secret, 1)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, session.SIDHash, session.IssuedAt, session.ExpiresAt)
	require.NoError(t, err)

	handler := AdminAuth(pool, secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, orgID, GetOrgIDFromContext(r.Context()))
		require.Equal(t, userID, GetUserIDFromContext(r.Context()))
		require.Equal(t, auth.RoleOrgAdmin, GetUserRoleFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	serve := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, serve().Code)
	_, err = pool.Exec(t.Context(), `UPDATE users SET status = 'disabled' WHERE id = $1`, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE users SET status = 'active' WHERE id = $1`, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE users SET role = $2 WHERE id = $1`, userID, auth.RoleViewer)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE users SET role = $2 WHERE id = $1`, userID, auth.RoleOrgAdmin)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE orgs SET status = 'disabled' WHERE id = $1`, orgID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)

	_, err = pool.Exec(t.Context(), `UPDATE orgs SET status = 'active' WHERE id = $1`, orgID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve().Code)

	_, err = pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve().Code)
}
