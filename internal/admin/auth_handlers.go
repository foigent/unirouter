package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

const adminSessionTTLHours = 24

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}
	username, err := auth.NormalizeLocalUsername(req.Username)
	if err != nil || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "username and password are required"})
		return
	}

	var userID, orgID, authEpoch int64
	var storedUsername, passwordHash, role, status, orgName, email, authProvider string
	err = h.db.QueryRow(r.Context(),
		`WITH local_candidates AS MATERIALIZED (
		     SELECT u.id, u.org_id, u.username, u.password_hash, u.role, u.status,
		        COALESCE(o.name, '') AS org_name, COALESCE(u.email, '') AS email,
		        COALESCE(NULLIF(u.auth_provider, ''), 'local') AS auth_provider, u.auth_epoch
		       FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		      WHERE COALESCE(NULLIF(u.auth_provider, ''), 'local') = 'local'
		        AND public.normalize_local_login_id(u.username) = public.normalize_local_login_id($1)
		 )
		 SELECT id, org_id, password_hash, role, status,
		        org_name, email, auth_provider, auth_epoch, username
		 FROM local_candidates
		 WHERE (SELECT COUNT(*) FROM local_candidates) = 1`,
		username,
	).Scan(&userID, &orgID, &passwordHash, &role, &status, &orgName, &email, &authProvider, &authEpoch, &storedUsername)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	if status != "active" {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Account is disabled"})
		return
	}
	if authProvider != "local" {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}
	if isBlockedPublicDefaultPassword(req.Password) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error": "Default admin password is disabled; run admin set-password before login",
		})
		return
	}

	token, err := h.issueAdminSession(r.Context(), userID, orgID, role, authEpoch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create session"})
		return
	}

	auditCtx := context.WithValue(context.Background(), middleware.CtxOrgID, orgID)
	auditCtx = context.WithValue(auditCtx, middleware.CtxUserID, userID)
	h.recordAudit(auditCtx, "login", "user", fmt.Sprintf("%d", userID),
		map[string]interface{}{"username": storedUsername, "role": role}, r.RemoteAddr)

	setAuthTokenResponseHeaders(w)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":         token,
		"user_id":       userID,
		"username":      storedUsername,
		"role":          role,
		"org_id":        orgID,
		"org_name":      orgName,
		"status":        status,
		"email":         email,
		"auth_provider": authProvider,
	})
}

func (h *Handler) issueAdminSession(ctx context.Context, userID, orgID int64, role string, authEpoch int64) (string, error) {
	token, session, err := auth.GenerateAdminSessionJWT(userID, orgID, role, authEpoch, h.jwtSecret, adminSessionTTLHours)
	if err != nil {
		return "", err
	}
	if _, err := h.db.Exec(ctx,
		`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		userID, session.SIDHash, session.IssuedAt, session.ExpiresAt,
	); err != nil {
		return "", fmt.Errorf("persist admin session: %w", err)
	}
	return token, nil
}

// Logout validates the signed, unexpired session identity and revokes only its
// matching server-side session. It intentionally does not require the current
// principal state to match the token: a disabled user or organization, changed
// role, or advanced auth epoch must still be able to destroy the old bearer.
// Already-revoked matching rows remain successful so client retries are
// idempotent, while AdminAuth rejects the revoked token on protected routes.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid or expired token"})
		return
	}
	claims, err := auth.ParseJWT(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid or expired token"})
		return
	}
	sessionHash := auth.HashAdminSessionID(claims.SessionID)
	detail := marshalAuditJSON(map[string]interface{}{"scope": "current_session"})
	var matched, revoked bool
	var auditCount int
	err = h.db.QueryRow(r.Context(),
		`WITH target AS (
		    SELECT s.id, u.id AS user_id, u.org_id
		      FROM admin_sessions s
		      JOIN users u ON u.id = s.user_id
		     WHERE s.sid_hash = $1
		       AND u.id = $2
		), newly_revoked AS (
		    UPDATE admin_sessions s
		       SET revoked_at = NOW()
		      FROM target t
		     WHERE s.id = t.id AND s.revoked_at IS NULL
		 RETURNING s.id
		), audited AS (
		    INSERT INTO audit_logs (
		        org_id, user_id, action, resource, resource_id, detail, ip,
		        user_agent, request_id, source, before_state, after_state,
		        client_request_id, created_at
		    )
		    SELECT t.org_id, t.user_id, 'logout', 'admin_session', t.id::text,
		           $3::jsonb, $4, $5, $6, 'admin_console', '{}'::jsonb,
		           $3::jsonb, $7, NOW()
		      FROM target t JOIN newly_revoked nr ON nr.id = t.id
		 RETURNING 1
		)
		SELECT EXISTS (SELECT 1 FROM target),
		       EXISTS (SELECT 1 FROM newly_revoked),
		       (SELECT COUNT(*) FROM audited)`,
		sessionHash, claims.UserID, detail, r.RemoteAddr, r.UserAgent(),
		middleware.GetRequestIDFromContext(r.Context()),
		nullableString(middleware.GetClientRequestIDFromContext(r.Context())),
	).Scan(&matched, &revoked, &auditCount)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to revoke session"})
		return
	}
	if !matched {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid or expired token"})
		return
	}
	if revoked && auditCount != 1 {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit session revocation"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func isBlockedPublicDefaultPassword(password string) bool {
	if os.Getenv("UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN") == "true" {
		return false
	}
	return password == "admin123456"
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}

	userID := middleware.GetUserIDFromContext(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Not authenticated"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "old_password and new_password are required"})
		return
	}
	if err := auth.ValidateLocalPassword(req.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if req.OldPassword == req.NewPassword {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "new_password must differ from old_password"})
		return
	}

	var passwordHash, authProvider string
	err := h.db.QueryRow(r.Context(),
		`SELECT password_hash, COALESCE(NULLIF(auth_provider, ''), 'local')
		 FROM users WHERE id = $1 AND status = 'active'`,
		userID,
	).Scan(&passwordHash, &authProvider)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}
	if authProvider != "local" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":         "Password is managed by an external identity provider",
			"auth_provider": authProvider,
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.OldPassword)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to hash password"})
		return
	}

	detail := marshalAuditJSON(map[string]interface{}{
		"reason":           "password_changed",
		"sessions_revoked": "all",
	})
	var changed, audited bool
	err = h.db.QueryRow(r.Context(),
		`WITH changed AS (
		    UPDATE users
		       SET password_hash = $1, auth_epoch = auth_epoch + 1, updated_at = NOW()
		     WHERE id = $2
		       AND password_hash = $3
		       AND status = 'active'
		       AND COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
		 RETURNING id, org_id
		), audited AS (
		    INSERT INTO audit_logs (
		        org_id, user_id, action, resource, resource_id, detail, ip,
		        user_agent, request_id, source, before_state, after_state,
		        client_request_id, created_at
		    )
		    SELECT org_id, id, 'change_password', 'user', id::text, $4::jsonb,
		           $5, $6, $7, 'admin_console', '{}'::jsonb, $4::jsonb, $8, NOW()
		      FROM changed
		 RETURNING 1
		)
		SELECT EXISTS (SELECT 1 FROM changed), EXISTS (SELECT 1 FROM audited)`,
		string(newHash), userID, passwordHash, detail, r.RemoteAddr, r.UserAgent(),
		middleware.GetRequestIDFromContext(r.Context()), nullableString(middleware.GetClientRequestIDFromContext(r.Context())),
	).Scan(&changed, &audited)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update password"})
		return
	}
	if !changed {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid credentials"})
		return
	}
	if !audited {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit password change"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "reauthentication_required": true})
}
