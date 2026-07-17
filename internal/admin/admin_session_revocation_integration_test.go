//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const adminSessionIntegrationSecret = "admin-session-integration-secret"

func TestAdminSessionLoginLogoutAndIdempotentRetryIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	userID := insertAdminSessionTestUser(t, pool, orgID, "session-logout-user", "initial-password-123")
	h := NewHandler(pool, adminSessionIntegrationSecret, []byte("0123456789abcdef01234567"), nil)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{"username":"session-logout-user","password":"initial-password-123"}`))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code, loginRec.Body.String())
	var loginBody struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &loginBody))
	require.NotEmpty(t, loginBody.Token)
	claims, err := auth.ParseJWT(loginBody.Token, adminSessionIntegrationSecret)
	require.NoError(t, err)
	require.NotEmpty(t, claims.SessionID)
	otherToken := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
	otherClaims, err := auth.ParseJWT(otherToken, adminSessionIntegrationSecret)
	require.NoError(t, err)
	require.NotEqual(t, claims.SessionID, otherClaims.SessionID)
	otherHash := auth.HashAdminSessionID(otherClaims.SessionID)

	var storedHash []byte
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT sid_hash FROM admin_sessions WHERE user_id = $1 AND sid_hash = $2`,
		userID, auth.HashAdminSessionID(claims.SessionID)).Scan(&storedHash))
	require.Equal(t, auth.HashAdminSessionID(claims.SessionID), storedHash)
	require.NotEqual(t, []byte(claims.SessionID), storedHash, "raw bearer sid must not be stored")

	protected := middleware.AdminAuth(pool, adminSessionIntegrationSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, userID, middleware.GetUserIDFromContext(r.Context()))
		contextHash := middleware.GetAdminSessionHashFromContext(r.Context())
		require.True(t, bytes.Equal(contextHash, storedHash) || bytes.Equal(contextHash, otherHash))
		w.WriteHeader(http.StatusNoContent)
	}))
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, loginBody.Token).Code)
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, otherToken).Code)

	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+loginBody.Token)
		rec := httptest.NewRecorder()
		h.Logout(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "attempt %d: %s", attempt, rec.Body.String())
	}
	require.Equal(t, http.StatusUnauthorized, serveAdminSessionToken(protected, loginBody.Token).Code)
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, otherToken).Code,
		"logout must revoke only the current sid")

	var revoked, otherRevoked bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE user_id = $1 AND sid_hash = $2`,
		userID, auth.HashAdminSessionID(claims.SessionID)).Scan(&revoked))
	require.True(t, revoked)
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE user_id = $1 AND sid_hash = $2`,
		userID, otherHash).Scan(&otherRevoked))
	require.False(t, otherRevoked)
	var logoutAudits int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM audit_logs WHERE user_id = $1 AND action = 'logout'`, userID).Scan(&logoutAudits))
	require.Equal(t, 1, logoutAudits, "idempotent retry must not duplicate the authoritative audit")
}

func TestAdminSessionLogoutRevokesSignedTokenAfterPrincipalStateChangesIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	h := NewHandler(pool, adminSessionIntegrationSecret, []byte("0123456789abcdef01234567"), nil)
	protected := middleware.AdminAuth(pool, adminSessionIntegrationSecret)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name                      string
		mutate                    func(*testing.T, *pgxpool.Pool, int64, int64)
		restore                   func(*testing.T, *pgxpool.Pool, int64, int64)
		issueControlAfterMutation bool
	}{
		{
			name: "user disabled",
			mutate: func(t *testing.T, pool *pgxpool.Pool, _ int64, userID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET status = 'disabled' WHERE id = $1`, userID)
				require.NoError(t, err)
			},
			restore: func(t *testing.T, pool *pgxpool.Pool, _ int64, userID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET status = 'active' WHERE id = $1`, userID)
				require.NoError(t, err)
			},
		},
		{
			name: "organization disabled",
			mutate: func(t *testing.T, pool *pgxpool.Pool, orgID int64, _ int64) {
				_, err := pool.Exec(t.Context(), `UPDATE orgs SET status = 'disabled' WHERE id = $1`, orgID)
				require.NoError(t, err)
			},
			restore: func(t *testing.T, pool *pgxpool.Pool, orgID int64, _ int64) {
				_, err := pool.Exec(t.Context(), `UPDATE orgs SET status = 'active' WHERE id = $1`, orgID)
				require.NoError(t, err)
			},
		},
		{
			name: "role changed",
			mutate: func(t *testing.T, pool *pgxpool.Pool, _ int64, userID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET role = $1 WHERE id = $2`, auth.RoleOrgViewer, userID)
				require.NoError(t, err)
			},
			restore: func(t *testing.T, pool *pgxpool.Pool, _ int64, userID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET role = $1 WHERE id = $2`, auth.RoleOrgAdmin, userID)
				require.NoError(t, err)
			},
		},
		{
			name: "auth epoch changed",
			mutate: func(t *testing.T, pool *pgxpool.Pool, _ int64, userID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET auth_epoch = auth_epoch + 1 WHERE id = $1`, userID)
				require.NoError(t, err)
			},
			issueControlAfterMutation: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orgID := testutil.NewTestOrg(t, pool)
			userID := insertAdminSessionTestUser(t, pool, orgID, "logout-state-change-"+strings.ReplaceAll(tc.name, " ", "-"), "initial-password-123")
			targetToken := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
			targetClaims, err := auth.ParseJWT(targetToken, adminSessionIntegrationSecret)
			require.NoError(t, err)
			var targetSessionID int64
			require.NoError(t, pool.QueryRow(t.Context(),
				`SELECT id FROM admin_sessions WHERE user_id = $1 AND sid_hash = $2`,
				userID, auth.HashAdminSessionID(targetClaims.SessionID),
			).Scan(&targetSessionID))

			var controlToken string
			if !tc.issueControlAfterMutation {
				controlToken = testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
			}
			tc.mutate(t, pool, orgID, userID)
			if tc.issueControlAfterMutation {
				controlToken = testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
			}
			controlClaims, err := auth.ParseJWT(controlToken, adminSessionIntegrationSecret)
			require.NoError(t, err)

			require.Equal(t, http.StatusUnauthorized, serveAdminSessionToken(protected, targetToken).Code,
				"the changed principal state must already fail closed on protected routes")
			for attempt := 1; attempt <= 2; attempt++ {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
				req.Header.Set("Authorization", "Bearer "+targetToken)
				rec := httptest.NewRecorder()
				h.Logout(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, "attempt %d: %s", attempt, rec.Body.String())
			}
			if tc.restore != nil {
				tc.restore(t, pool, orgID, userID)
			}

			require.Equal(t, http.StatusUnauthorized, serveAdminSessionToken(protected, targetToken).Code,
				"restoring principal state must not resurrect the revoked token")
			require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, controlToken).Code,
				"logout must preserve a separate current session")

			var targetRevoked, controlRevoked bool
			require.NoError(t, pool.QueryRow(t.Context(),
				`SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE id = $1`, targetSessionID,
			).Scan(&targetRevoked))
			require.True(t, targetRevoked)
			require.NoError(t, pool.QueryRow(t.Context(),
				`SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE user_id = $1 AND sid_hash = $2`,
				userID, auth.HashAdminSessionID(controlClaims.SessionID),
			).Scan(&controlRevoked))
			require.False(t, controlRevoked)

			var logoutAudits int
			var auditOrgID int64
			require.NoError(t, pool.QueryRow(t.Context(),
				`SELECT COUNT(*), COALESCE(MAX(org_id), 0)
				   FROM audit_logs
				  WHERE user_id = $1 AND action = 'logout'
				    AND resource = 'admin_session' AND resource_id = $2`,
				userID, fmt.Sprintf("%d", targetSessionID),
			).Scan(&logoutAudits, &auditOrgID))
			require.Equal(t, 1, logoutAudits, "first revoke and audit must be atomic and idempotent")
			require.Equal(t, orgID, auditOrgID, "audit tenant must come from the session's current user row")
		})
	}
}

func TestAdminSessionLogoutAuditFailureRollsBackRevocationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	userID := insertAdminSessionTestUser(t, pool, orgID, "logout-audit-rollback", "initial-password-123")
	token := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
	claims, err := auth.ParseJWT(token, adminSessionIntegrationSecret)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		CREATE FUNCTION reject_logout_audit() RETURNS TRIGGER AS $$
		BEGIN
		    IF NEW.action = 'logout' THEN
		        RAISE EXCEPTION 'reject logout audit';
		    END IF;
		    RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_logout_audit_trigger
		BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_logout_audit();`)
	require.NoError(t, err)

	h := NewHandler(pool, adminSessionIntegrationSecret, []byte("0123456789abcdef01234567"), nil)
	protected := middleware.AdminAuth(pool, adminSessionIntegrationSecret)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, token).Code)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.Logout(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())

	var revoked bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT revoked_at IS NOT NULL FROM admin_sessions WHERE user_id = $1 AND sid_hash = $2`,
		userID, auth.HashAdminSessionID(claims.SessionID)).Scan(&revoked))
	require.False(t, revoked, "audit failure must roll back the session revocation")
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, token).Code,
		"a failed logout must not claim or enforce a revocation that did not commit")
	var logoutAudits int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM audit_logs WHERE user_id = $1 AND action = 'logout'`, userID).Scan(&logoutAudits))
	require.Zero(t, logoutAudits)
}

func TestChangePasswordRevokesEveryOldSessionAndAllowsFreshLoginIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	userID := insertAdminSessionTestUser(t, pool, orgID, "password-epoch-user", "initial-password-123")
	h := NewHandler(pool, adminSessionIntegrationSecret, []byte("0123456789abcdef01234567"), nil)
	oldTokenA := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
	oldTokenB := testutil.MustAdminSessionJWT(t, t.Context(), pool, userID, orgID, auth.RoleOrgAdmin, adminSessionIntegrationSecret)
	protected := middleware.AdminAuth(pool, adminSessionIntegrationSecret)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, oldTokenA).Code)
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, oldTokenB).Code)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password",
		bytes.NewBufferString(`{"old_password":"initial-password-123","new_password":"replacement-password-456"}`))
	ctx := req.Context()
	ctx = adminSessionActorContext(ctx, orgID, userID, auth.RoleOrgAdmin, "password-change-integration")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ChangePassword(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"reauthentication_required":true`)

	var passwordHash string
	var authEpoch int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT password_hash, auth_epoch FROM users WHERE id = $1`, userID).Scan(&passwordHash, &authEpoch))
	require.Equal(t, int64(1), authEpoch)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("replacement-password-456")))
	require.Equal(t, http.StatusUnauthorized, serveAdminSessionToken(protected, oldTokenA).Code)
	require.Equal(t, http.StatusUnauthorized, serveAdminSessionToken(protected, oldTokenB).Code)

	var passwordAudits int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM audit_logs WHERE user_id = $1 AND action = 'change_password' AND request_id = 'password-change-integration'`,
		userID).Scan(&passwordAudits))
	require.Equal(t, 1, passwordAudits)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{"username":"password-epoch-user","password":"replacement-password-456"}`))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code, loginRec.Body.String())
	var loginBody struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &loginBody))
	require.Equal(t, http.StatusNoContent, serveAdminSessionToken(protected, loginBody.Token).Code)
}

func TestChangePasswordAuditFailureRollsBackHashAndEpochIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	userID := insertAdminSessionTestUser(t, pool, orgID, "password-audit-rollback", "initial-password-123")
	_, err := pool.Exec(t.Context(), `
		CREATE FUNCTION reject_password_change_audit() RETURNS TRIGGER AS $$
		BEGIN
		    IF NEW.action = 'change_password' THEN
		        RAISE EXCEPTION 'reject password audit';
		    END IF;
		    RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_password_change_audit_trigger
		BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_password_change_audit();`)
	require.NoError(t, err)
	var beforeHash string
	var beforeEpoch int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT password_hash, auth_epoch FROM users WHERE id = $1`, userID).Scan(&beforeHash, &beforeEpoch))

	h := NewHandler(pool, adminSessionIntegrationSecret, []byte("0123456789abcdef01234567"), nil)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password",
		bytes.NewBufferString(`{"old_password":"initial-password-123","new_password":"replacement-password-456"}`))
	req = req.WithContext(adminSessionActorContext(req.Context(), orgID, userID, auth.RoleOrgAdmin, "password-change-audit-reject"))
	rec := httptest.NewRecorder()
	h.ChangePassword(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())

	var afterHash string
	var afterEpoch int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT password_hash, auth_epoch FROM users WHERE id = $1`, userID).Scan(&afterHash, &afterEpoch))
	require.Equal(t, beforeHash, afterHash)
	require.Equal(t, beforeEpoch, afterEpoch)
}

func TestCleanupOldDataDeletesOnlyExpiredAdminSessionsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	userID := insertAdminSessionTestUser(t, pool, orgID, "session-cleanup-user", "initial-password-123")
	_, err := pool.Exec(t.Context(), `
		INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at, revoked_at)
		VALUES
		    ($1, decode(repeat('01', 32), 'hex'), NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour', NOW() - INTERVAL '90 minutes'),
		    ($1, decode(repeat('02', 32), 'hex'), NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', NOW())`, userID)
	require.NoError(t, err)

	h := NewHandler(pool, adminSessionIntegrationSecret, []byte("0123456789abcdef01234567"), nil)
	require.NoError(t, h.CleanupOldData(t.Context(), 30))
	var expired, live int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FILTER (WHERE expires_at < NOW()), COUNT(*) FILTER (WHERE expires_at > NOW())
		 FROM admin_sessions WHERE user_id = $1`, userID).Scan(&expired, &live))
	require.Zero(t, expired)
	require.Equal(t, 1, live, "a revoked but unexpired row is retained for idempotent logout")
}

func insertAdminSessionTestUser(t *testing.T, pool *pgxpool.Pool, orgID int64, username, password string) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status, auth_provider)
		 VALUES ($1, $2, $3, $4, 'active', 'local') RETURNING id`,
		orgID, username, string(hash), auth.RoleOrgAdmin,
	).Scan(&userID))
	return userID
}

func adminSessionActorContext(ctx context.Context, orgID, userID int64, role, requestID string) context.Context {
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	return ctx
}

func serveAdminSessionToken(handler http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
