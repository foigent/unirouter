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
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/licensing"
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
	ID               int64
	OrgID            int64
	Username         string
	OrganizationRole string
	Role             string
	Status           string
	AuthEpoch        int64
}

func (h *Handler) OIDCConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}
	// This public endpoint projects whether OIDC is currently usable, rather
	// than exposing a licensed-but-unavailable login action. Start and callback
	// remain independently gated so a license change after this read fails
	// closed.
	if h.licenseRuntime != nil && !h.licenseRuntime.Authorize(licensing.FeatureOIDC).Allowed {
		writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": false})
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

	token, err := h.issueAdminSession(r.Context(), user.ID, user.OrgID, user.OrganizationRole, user.Role, user.AuthEpoch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create session"})
		return
	}

	auditCtx := context.WithValue(r.Context(), middleware.CtxOrgID, user.OrgID)
	auditCtx = context.WithValue(auditCtx, middleware.CtxUserID, user.ID)
	loginAuditDetail := map[string]interface{}{
		"oidc_identity_ref": oidcAuditReference("identity", identity.Issuer, identity.Subject),
		"username_ref":      oidcAuditReference("username", user.Username),
		"role":              user.Role,
	}
	if strings.TrimSpace(identity.Email) != "" {
		loginAuditDetail["email_ref"] = oidcAuditReference("email", strings.ToLower(strings.TrimSpace(identity.Email)))
	}
	h.recordAuditEntry(auditCtx, "login.oidc", "user", fmt.Sprintf("%d", user.ID), loginAuditDetail, auditMeta{
		OrgID:           user.OrgID,
		IP:              r.RemoteAddr,
		UserAgent:       r.UserAgent(),
		RequestID:       middleware.GetRequestIDFromContext(r.Context()),
		ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
		Source:          "system",
	})

	if r.URL.Query().Get("format") == "json" {
		setAuthTokenResponseHeaders(w)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token":             token,
			"user_id":           user.ID,
			"username":          user.Username,
			"role":              user.Role,
			"organization_role": user.OrganizationRole,
			"org_id":            user.OrgID,
		})
		return
	}
	writeOIDCCallbackHTML(w, token, stored.Redirect)
}

var (
	errOIDCProvisioningDisabled          = errors.New("oidc provisioning disabled")
	errOIDCPlatformProvisioningForbidden = errors.New("oidc platform administrator auto provisioning is forbidden")
)

func (h *Handler) upsertOIDCUser(ctx context.Context, identity *oidc.Identity) (oidcUser, error) {
	if identity == nil {
		return oidcUser{}, errors.New("missing oidc identity")
	}
	existing, err := h.findOIDCUser(ctx, identity.Issuer, identity.Subject)
	if err == nil {
		return h.reconcileExistingOIDCUser(ctx, existing, identity)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return oidcUser{}, err
	}

	cfg := h.oidcService.Config()
	if !cfg.AutoProvision {
		return oidcUser{}, errOIDCProvisioningDisabled
	}
	// An external identity provider may authenticate an existing deployment
	// operator, but it must never mint a new deployment-wide administrator.
	// Existing issuer/subject bindings return above and keep their stored role.
	if auth.IsPlatformAdmin(identity.OrganizationRole) || auth.IsPlatformAdmin(identity.Role) {
		return oidcUser{}, errOIDCPlatformProvisioningForbidden
	}
	if err := h.ensureOIDCOrg(ctx, identity.OrgID); err != nil {
		return oidcUser{}, err
	}

	usernameBase := sanitizeOIDCUsername(identity.Username)
	if usernameBase == "" {
		usernameBase = "oidc-" + shortOIDCHash(identity.Issuer, identity.Subject)
	}
	passwordHash := "oidc:" + shortOIDCHash(identity.Issuer, identity.Subject)
	candidates := []string{usernameBase, oidcUsernameWithSuffix(usernameBase, shortOIDCHash(identity.Issuer, identity.Subject))}
	for _, username := range candidates {
		tx, err := h.db.Begin(ctx)
		if err != nil {
			return oidcUser{}, err
		}
		var userID, authEpoch int64
		err = tx.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, organization_role, role, status, email, auth_provider, oidc_issuer, oidc_subject)
			 VALUES ($1, $2, $3, $4, $5, 'active', $6, 'oidc', $7, $8)
			 ON CONFLICT DO NOTHING
			 RETURNING id, auth_epoch`,
			identity.OrgID, username, passwordHash, identity.OrganizationRole, identity.Role,
			nullableString(identity.Email), identity.Issuer, identity.Subject,
		).Scan(&userID, &authEpoch)
		if err == nil {
			if err := lockProjectMembershipMutation(ctx, tx, userID); err != nil {
				_ = tx.Rollback(ctx)
				return oidcUser{}, err
			}
			if err := ensureDefaultProjectMembershipTx(ctx, tx, identity.OrgID, userID, identity.ProjectRole, cfg.DefaultProjectSlug); err != nil {
				_ = tx.Rollback(ctx)
				return oidcUser{}, err
			}
			projectIDs, projectErr := listUserProjectIDsTx(ctx, tx, userID, identity.OrgID)
			if projectErr != nil {
				_ = tx.Rollback(ctx)
				return oidcUser{}, projectErr
			}
			provisioned := oidcUser{ID: userID, OrgID: identity.OrgID, Username: username, OrganizationRole: identity.OrganizationRole, Role: identity.Role, Status: "active", AuthEpoch: authEpoch}
			auditCtx := oidcMutationAuditContext(ctx, provisioned)
			if auditErr := insertAuditEntry(auditCtx, tx, "user.oidc_provision", "user", fmt.Sprintf("%d", userID), map[string]interface{}{
				"after": oidcUserAuditState(provisioned, identity.Email, identity, projectIDs),
				"changes": map[string]interface{}{
					"auto_provisioned":                    true,
					"default_project_membership_assigned": len(projectIDs) > 0,
				},
			}, oidcMutationAuditMeta(auditCtx, identity.OrgID)); auditErr != nil {
				_ = tx.Rollback(ctx)
				return oidcUser{}, auditErr
			}
			if err := tx.Commit(ctx); err != nil {
				return oidcUser{}, err
			}
			return provisioned, nil
		}
		_ = tx.Rollback(ctx)
		if !errors.Is(err, pgx.ErrNoRows) {
			return oidcUser{}, err
		}
		// Targetless DO NOTHING safely covers both the external display-name
		// index and the issuer/subject identity index. A concurrent first login
		// for this identity must converge on the committed winner; a pure display
		// collision instead advances to the deterministic suffixed candidate.
		existing, lookupErr := h.findOIDCUser(ctx, identity.Issuer, identity.Subject)
		if lookupErr == nil {
			return h.reconcileExistingOIDCUser(ctx, existing, identity)
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return oidcUser{}, lookupErr
		}
	}
	return oidcUser{}, errors.New("oidc username already exists")
}

func (h *Handler) findOIDCUser(ctx context.Context, issuer, subject string) (oidcUser, error) {
	var existing oidcUser
	err := h.db.QueryRow(ctx,
		`SELECT id, org_id, username, organization_role, role, status, auth_epoch
		 FROM users
		 WHERE oidc_issuer = $1 AND oidc_subject = $2
		 LIMIT 1`,
		issuer, subject,
	).Scan(&existing.ID, &existing.OrgID, &existing.Username, &existing.OrganizationRole, &existing.Role, &existing.Status, &existing.AuthEpoch)
	return existing, err
}

func (h *Handler) reconcileExistingOIDCUser(ctx context.Context, existing oidcUser, identity *oidc.Identity) (oidcUser, error) {
	if identity == nil {
		return oidcUser{}, errors.New("missing oidc identity")
	}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return oidcUser{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockProjectMembershipMutation(ctx, tx, existing.ID); err != nil {
		return oidcUser{}, err
	}
	var existingEmail string
	if err := tx.QueryRow(ctx,
		`SELECT id, org_id, username, organization_role, role, status, auth_epoch, COALESCE(email, '')
		 FROM users WHERE id = $1 FOR UPDATE`, existing.ID,
	).Scan(&existing.ID, &existing.OrgID, &existing.Username, &existing.OrganizationRole, &existing.Role, &existing.Status, &existing.AuthEpoch, &existingEmail); err != nil {
		return oidcUser{}, err
	}
	if existing.Status != "active" {
		return oidcUser{}, errors.New("oidc user is disabled")
	}
	beforeProjectIDs, err := listUserProjectIDsTx(ctx, tx, existing.ID, existing.OrgID)
	if err != nil {
		return oidcUser{}, err
	}
	emailChanged := strings.TrimSpace(identity.Email) != "" && identity.Email != existingEmail
	if emailChanged {
		if _, err := tx.Exec(ctx, `UPDATE users SET email = $1, updated_at = NOW() WHERE id = $2`, identity.Email, existing.ID); err != nil {
			return oidcUser{}, err
		}
	}
	membershipRestored := false
	if existing.OrganizationRole == auth.RoleMember && len(beforeProjectIDs) == 0 {
		canonicalProjectRole, hasLegacyProjectShadow := auth.NormalizeProjectMembershipRole(existing.Role)
		if hasLegacyProjectShadow {
			if err := ensureDefaultProjectMembershipTx(ctx, tx, existing.OrgID, existing.ID, canonicalProjectRole, h.oidcService.Config().DefaultProjectSlug); err != nil {
				return oidcUser{}, err
			}
			membershipRestored = true
		}
	}
	if emailChanged || membershipRestored {
		afterProjectIDs, err := listUserProjectIDsTx(ctx, tx, existing.ID, existing.OrgID)
		if err != nil {
			return oidcUser{}, err
		}
		afterEmail := existingEmail
		if emailChanged {
			afterEmail = identity.Email
		}
		auditCtx := oidcMutationAuditContext(ctx, existing)
		if err := insertAuditEntry(auditCtx, tx, "user.oidc_sync", "user", fmt.Sprintf("%d", existing.ID), map[string]interface{}{
			"before": oidcUserAuditState(existing, existingEmail, identity, beforeProjectIDs),
			"after":  oidcUserAuditState(existing, afterEmail, identity, afterProjectIDs),
			"changes": map[string]interface{}{
				"email_synchronized":                  emailChanged,
				"email_changed":                       emailChanged,
				"default_project_membership_restored": membershipRestored,
			},
		}, oidcMutationAuditMeta(auditCtx, existing.OrgID)); err != nil {
			return oidcUser{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return oidcUser{}, err
	}
	return existing, nil
}

func oidcMutationAuditContext(ctx context.Context, user oidcUser) context.Context {
	ctx = context.WithValue(ctx, middleware.CtxOrgID, user.OrgID)
	ctx = context.WithValue(ctx, middleware.CtxUserID, user.ID)
	return ctx
}

func oidcMutationAuditMeta(ctx context.Context, orgID int64) auditMeta {
	return auditMeta{
		OrgID:           orgID,
		RequestID:       middleware.GetRequestIDFromContext(ctx),
		ClientRequestID: middleware.GetClientRequestIDFromContext(ctx),
		Source:          "system",
	}
}

func oidcUserAuditState(user oidcUser, email string, identity *oidc.Identity, projectIDs []int64) map[string]interface{} {
	state := map[string]interface{}{
		"id":                user.ID,
		"org_id":            user.OrgID,
		"username_ref":      oidcAuditReference("username", user.Username),
		"role":              user.Role,
		"organization_role": user.OrganizationRole,
		"status":            user.Status,
		"auth_provider":     "oidc",
		"oidc_identity_ref": oidcAuditReference("identity", identity.Issuer, identity.Subject),
		"project_ids":       projectIDs,
	}
	if strings.TrimSpace(email) != "" {
		state["email_ref"] = oidcAuditReference("email", strings.ToLower(strings.TrimSpace(email)))
	}
	return state
}

func oidcAuditReference(kind string, parts ...string) string {
	value := append([]string{kind}, parts...)
	hash := sha256.Sum256([]byte(strings.Join(value, "\x00")))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func (h *Handler) ensureOIDCOrg(ctx context.Context, orgID int64) error {
	customer, err := customercontext.Resolve(ctx, h.db)
	if err != nil {
		return fmt.Errorf("resolve OIDC customer organization: %w", err)
	}
	if !customer.Ready() {
		return fmt.Errorf("oidc customer organization is %s", customer.State)
	}
	if orgID != customer.OrganizationID {
		return fmt.Errorf("oidc organization does not match the deployment customer")
	}
	return nil
}

func ensureDefaultProjectMembershipTx(ctx context.Context, tx pgx.Tx, orgID, userID int64, role, projectSlug string) error {
	if strings.TrimSpace(role) == "" {
		return nil
	}
	memberRole, ok := auth.NormalizeProjectMembershipRole(role)
	if !ok || memberRole != role {
		return fmt.Errorf("invalid OIDC project role %q", role)
	}
	if projectSlug == "" {
		projectSlug = "default"
	}
	tag, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role)
		 SELECT id, $2, $3 FROM projects WHERE org_id = $1 AND slug = $4 AND status = 'active'
		 ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = NOW()`,
		orgID, userID, memberRole, projectSlug,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("default project %q is required for active OIDC project role", projectSlug)
	}
	return nil
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

func oidcUsernameWithSuffix(usernameBase, suffix string) string {
	const maxUsernameLength = 180
	suffix = strings.Trim(suffix, ".-_")
	if suffix == "" {
		return trimUsername(usernameBase)
	}
	maxBaseLength := maxUsernameLength - len(suffix) - 1
	if maxBaseLength <= 0 {
		return trimUsername(suffix)
	}
	usernameBase = strings.Trim(usernameBase, ".-_")
	if len(usernameBase) > maxBaseLength {
		usernameBase = usernameBase[:maxBaseLength]
	}
	return trimUsername(usernameBase + "-" + suffix)
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
	setAuthTokenResponseHeaders(w)
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
