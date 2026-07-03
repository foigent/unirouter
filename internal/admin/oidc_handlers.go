package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/oidc"
)

const oidcStateCookieName = "uniroute_oidc_state"

type oidcStateCookie struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Redirect  string `json:"redirect"`
	ExpiresAt int64  `json:"expires_at"`
}

type oidcUser struct {
	ID       int64
	OrgID    int64
	Username string
	Role     string
	Status   string
}

func (h *Handler) OIDCConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}
	if h.oidcService == nil || !h.oidcService.Enabled() {
		writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, h.oidcService.PublicConfig())
}

func (h *Handler) OIDCStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}
	if h.oidcService == nil || !h.oidcService.Enabled() {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "OIDC is not enabled"})
		return
	}

	state, err := randomURLToken(24)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create OIDC state"})
		return
	}
	nonce, err := randomURLToken(24)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create OIDC nonce"})
		return
	}
	redirectPath := sanitizeOIDCRedirect(r.URL.Query().Get("redirect"))
	cookieValue, err := h.encodeOIDCState(oidcStateCookie{
		State:     state,
		Nonce:     nonce,
		Redirect:  redirectPath,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to sign OIDC state"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    cookieValue,
		Path:     "/api/v1/auth/oidc",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
	})

	authURL, err := h.oidcService.AuthCodeURL(r.Context(), state, nonce)
	if err != nil {
		clearOIDCStateCookie(w, r)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Failed to start OIDC login"})
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}
	if h.oidcService == nil || !h.oidcService.Enabled() {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "OIDC is not enabled"})
		return
	}
	if oidcErr := r.URL.Query().Get("error"); oidcErr != "" {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"error":             oidcErr,
			"error_description": r.URL.Query().Get("error_description"),
		})
		return
	}

	stored, err := h.readOIDCState(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Invalid or expired OIDC state"})
		return
	}
	clearOIDCStateCookie(w, r)
	if r.URL.Query().Get("state") == "" || !hmac.Equal([]byte(r.URL.Query().Get("state")), []byte(stored.State)) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "OIDC state mismatch"})
		return
	}

	identity, err := h.oidcService.Exchange(r.Context(), r.URL.Query().Get("code"), stored.Nonce)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "OIDC login failed"})
		return
	}
	user, err := h.upsertOIDCUser(r.Context(), identity)
	if err != nil {
		if errors.Is(err, errOIDCProvisioningDisabled) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "OIDC user is not provisioned"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "OIDC user mapping failed"})
		return
	}

	token, err := auth.GenerateJWT(user.ID, user.OrgID, user.Role, h.jwtSecret, 24)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate token"})
		return
	}

	auditCtx := context.WithValue(context.Background(), middleware.CtxOrgID, user.OrgID)
	auditCtx = context.WithValue(auditCtx, middleware.CtxUserID, user.ID)
	auditCtx = context.WithValue(auditCtx, middleware.CtxUserRole, user.Role)
	h.recordAudit(auditCtx, "login.oidc", "user", fmt.Sprintf("%d", user.ID), map[string]interface{}{
		"issuer":   identity.Issuer,
		"subject":  identity.Subject,
		"email":    identity.Email,
		"username": user.Username,
		"role":     user.Role,
	}, r.RemoteAddr)

	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token":    token,
			"user_id":  user.ID,
			"username": user.Username,
			"role":     user.Role,
			"org_id":   user.OrgID,
		})
		return
	}
	writeOIDCCallbackHTML(w, token, stored.Redirect)
}

var errOIDCProvisioningDisabled = errors.New("oidc provisioning disabled")

func (h *Handler) upsertOIDCUser(ctx context.Context, identity *oidc.Identity) (oidcUser, error) {
	if identity == nil {
		return oidcUser{}, errors.New("missing oidc identity")
	}
	var existing oidcUser
	err := h.db.QueryRow(ctx,
		`SELECT id, org_id, username, role, status
		 FROM users
		 WHERE oidc_issuer = $1 AND oidc_subject = $2
		 LIMIT 1`,
		identity.Issuer, identity.Subject,
	).Scan(&existing.ID, &existing.OrgID, &existing.Username, &existing.Role, &existing.Status)
	if err == nil {
		if existing.Status != "active" {
			return oidcUser{}, errors.New("oidc user is disabled")
		}
		if identity.Email != "" {
			_, _ = h.db.Exec(ctx, `UPDATE users SET email = $1, updated_at = NOW() WHERE id = $2`, identity.Email, existing.ID)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return oidcUser{}, err
	}

	cfg := h.oidcService.Config()
	if !cfg.AutoProvision {
		return oidcUser{}, errOIDCProvisioningDisabled
	}
	if err := h.ensureOIDCOrg(ctx, identity.OrgID); err != nil {
		return oidcUser{}, err
	}

	usernameBase := sanitizeOIDCUsername(identity.Username)
	if usernameBase == "" {
		usernameBase = "oidc-" + shortOIDCHash(identity.Issuer, identity.Subject)
	}
	passwordHash := "oidc:" + shortOIDCHash(identity.Issuer, identity.Subject)
	candidates := []string{usernameBase, trimUsername(usernameBase + "-" + shortOIDCHash(identity.Issuer, identity.Subject))}
	for _, username := range candidates {
		var userID int64
		err := h.db.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, role, status, email, auth_provider, oidc_issuer, oidc_subject)
			 VALUES ($1, $2, $3, $4, 'active', $5, 'oidc', $6, $7)
			 ON CONFLICT (org_id, username) DO NOTHING
			 RETURNING id`,
			identity.OrgID, username, passwordHash, identity.Role, nullableString(identity.Email), identity.Issuer, identity.Subject,
		).Scan(&userID)
		if err == nil {
			h.ensureDefaultProjectMembership(ctx, identity.OrgID, userID, identity.Role, cfg.DefaultProjectSlug)
			return oidcUser{ID: userID, OrgID: identity.OrgID, Username: username, Role: identity.Role, Status: "active"}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return oidcUser{}, err
		}
	}
	return oidcUser{}, errors.New("oidc username already exists")
}

func (h *Handler) ensureOIDCOrg(ctx context.Context, orgID int64) error {
	var status string
	err := h.db.QueryRow(ctx, `SELECT status FROM orgs WHERE id = $1`, orgID).Scan(&status)
	if err != nil {
		return fmt.Errorf("oidc org not found: %w", err)
	}
	if status != "active" {
		return fmt.Errorf("oidc org is %s", status)
	}
	return nil
}

func (h *Handler) ensureDefaultProjectMembership(ctx context.Context, orgID, userID int64, role, projectSlug string) {
	memberRole := ""
	switch auth.NormalizeRole(role) {
	case auth.RoleProjectAdmin:
		memberRole = projectMemberAdmin
	case auth.RoleDeveloper:
		memberRole = projectMemberDeveloper
	case auth.RoleViewer, auth.RoleOrgViewer:
		memberRole = projectMemberViewer
	default:
		return
	}
	if projectSlug == "" {
		projectSlug = "default"
	}
	_, _ = h.db.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role)
		 SELECT id, $2, $3 FROM projects WHERE org_id = $1 AND slug = $4
		 ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = NOW()`,
		orgID, userID, memberRole, projectSlug,
	)
}

func (h *Handler) encodeOIDCState(state oidcStateCookie) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(h.jwtSecret))
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

func (h *Handler) readOIDCState(r *http.Request) (oidcStateCookie, error) {
	cookie, err := r.Cookie(oidcStateCookieName)
	if err != nil {
		return oidcStateCookie{}, err
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return oidcStateCookie{}, errors.New("malformed oidc state")
	}
	mac := hmac.New(sha256.New, []byte(h.jwtSecret))
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return oidcStateCookie{}, errors.New("invalid oidc state signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oidcStateCookie{}, err
	}
	var state oidcStateCookie
	if err := json.Unmarshal(payload, &state); err != nil {
		return oidcStateCookie{}, err
	}
	if state.ExpiresAt < time.Now().Unix() {
		return oidcStateCookie{}, errors.New("oidc state expired")
	}
	return state, nil
}

func randomURLToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clearOIDCStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     "/api/v1/auth/oidc",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
	})
}

func sanitizeOIDCRedirect(raw string) string {
	if raw == "" {
		return "/"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "/"
	}
	if strings.HasPrefix(parsed.Path, "/api/") {
		return "/"
	}
	if parsed.Path == "/login" {
		return "/"
	}
	if parsed.RawQuery != "" {
		return parsed.Path + "?" + parsed.RawQuery
	}
	return parsed.Path
}

func sanitizeOIDCUsername(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range username {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '@' || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return trimUsername(b.String())
}

func trimUsername(username string) string {
	username = strings.Trim(username, ".-_")
	if len(username) > 180 {
		return username[:180]
	}
	return username
}

func shortOIDCHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])[:16]
}

func nullableString(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func writeOIDCCallbackHTML(w http.ResponseWriter, token, redirectPath string) {
	if redirectPath == "" {
		redirectPath = "/"
	}
	tokenJS, _ := json.Marshal(token)
	redirectJS, _ := json.Marshal(redirectPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html>
<html>
<head><meta charset="utf-8"><title>UniRoute SSO</title></head>
<body>
<script>
localStorage.setItem('token', %s);
window.location.replace(%s);
</script>
<noscript>%s</noscript>
</body>
</html>`, tokenJS, redirectJS, template.HTMLEscapeString("SSO login completed. JavaScript is required to continue."))
}
