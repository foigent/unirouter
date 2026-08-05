package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/resetplan"
	"golang.org/x/crypto/bcrypt"
)

const firstCustomerBootstrapAction = "bootstrap.first_customer"

type firstCustomerBootstrapRequest struct {
	OrganizationName string `json:"organization_name"`
}

type firstCustomerBootstrapResult struct {
	OrganizationID   int64  `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	WalletBalance    int64  `json:"wallet_balance_micros"`
	Idempotent       bool   `json:"idempotent"`
}

// BootstrapFirstCustomer creates only an active customer organization and its
// zero-value wallet. It is intentionally narrower than general tenant setup:
// users, projects, Provider/Feed credentials, application keys, and traffic
// remain separate BOOTSTRAP/TRAFFIC operations.
func (h *Handler) BootstrapFirstCustomer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "Method not allowed"})
		return
	}
	if !auth.IsPlatformAdmin(actorRole(r.Context())) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	var request firstCustomerBootstrapRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}
	request.OrganizationName = strings.TrimSpace(request.OrganizationName)
	if !validFirstCustomerOrganizationName(request.OrganizationName) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "organization_name must contain 1 to 160 printable characters"})
		return
	}
	clientRequestID := middleware.GetClientRequestIDFromContext(r.Context())
	operationID := middleware.GetRequestIDFromContext(r.Context())
	if clientRequestID == "" || operationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "X-Client-Request-ID is required"})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Bootstrap database unavailable"})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := tx.Exec(r.Context(), "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Bootstrap transaction failed"})
		return
	}
	if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", firstCustomerBootstrapAction); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Bootstrap lock failed"})
		return
	}

	adminUserID, adminOrgID, err := lockExactBootstrapAdmin(r, tx)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "First-customer bootstrap admin preflight failed"})
		return
	}
	if replay, found, err := readFirstCustomerBootstrapReplay(r, tx, clientRequestID, adminUserID, adminOrgID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Bootstrap idempotency readback failed"})
		return
	} else if found {
		if replay.OrganizationName != request.OrganizationName {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "X-Client-Request-ID was already used with different input"})
			return
		}
		replay.Idempotent = true
		writeJSON(w, http.StatusOK, replay)
		return
	}

	if err := verifyFirstCustomerEmptyCheckpoint(r, tx, adminUserID, adminOrgID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Strict empty checkpoint is not satisfied",
		})
		return
	}

	var organizationID int64
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO orgs (name, status, settings, parent_id, org_type)
		VALUES ($1, 'active', '{}'::jsonb, NULL, 'customer')
		RETURNING id`, request.OrganizationName).Scan(&organizationID); err != nil {
		if isPostgresUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Organization already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create organization"})
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO wallets (
			org_id, balance_micros, granted_balance_micros, frozen_micros,
			total_spent_micros, currency, auto_freeze, low_balance_threshold_cents
		) VALUES ($1, 0, 0, 0, 0, 'CNY', FALSE, 0)`, organizationID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create zero wallet"})
		return
	}
	afterState := marshalAuditJSON(map[string]interface{}{
		"organization_id": organizationID,
		"name":            request.OrganizationName,
		"status":          "active",
		"wallet":          "zero_micros",
	})
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO audit_logs (
			org_id, user_id, action, resource, resource_id, detail, ip,
			user_agent, request_id, source, before_state, after_state,
			client_request_id, created_at
		) VALUES (
			$1, $2, $3, 'organization', $4, $5::jsonb, $6,
			$7, $8, 'admin_console', '{}'::jsonb, $5::jsonb,
			$9, NOW()
		)`, adminOrgID, adminUserID, firstCustomerBootstrapAction,
		fmt.Sprintf("%d", organizationID), afterState, r.RemoteAddr, r.UserAgent(),
		operationID, clientRequestID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit bootstrap"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Bootstrap commit conflicted; retry with the same X-Client-Request-ID"})
		return
	}
	writeJSON(w, http.StatusCreated, firstCustomerBootstrapResult{
		OrganizationID: organizationID, OrganizationName: request.OrganizationName,
		WalletBalance: 0, Idempotent: false,
	})
}

func lockExactBootstrapAdmin(r *http.Request, tx pgx.Tx) (int64, int64, error) {
	var userID, orgID int64
	var role, userStatus, orgStatus, provider, passwordHash string
	err := tx.QueryRow(r.Context(), `
		WITH candidates AS MATERIALIZED (
			SELECT u.id, u.org_id, u.role, u.status, u.password_hash,
			       COALESCE(NULLIF(u.auth_provider, ''), 'local') AS auth_provider,
			       o.status AS org_status
			  FROM users u
			  JOIN orgs o ON o.id = u.org_id
			 WHERE public.normalize_local_login_id(u.username) = $1
			   AND COALESCE(NULLIF(u.auth_provider, ''), 'local') = 'local'
			 FOR UPDATE OF u, o
		)
		SELECT id, org_id, role, status, password_hash, auth_provider, org_status
		  FROM candidates
		 WHERE (SELECT COUNT(*) FROM candidates) = 1`, resetplan.PreservedLogin,
	).Scan(&userID, &orgID, &role, &userStatus, &passwordHash, &provider, &orgStatus)
	if err != nil || userID != middleware.GetUserIDFromContext(r.Context()) ||
		orgID != middleware.GetOrgIDFromContext(r.Context()) ||
		!auth.IsPlatformAdmin(role) || userStatus != "active" || orgStatus != "active" || provider != "local" {
		return 0, 0, fmt.Errorf("exact selected administrator is not active")
	}
	if _, err := bcrypt.Cost([]byte(passwordHash)); err != nil {
		return 0, 0, fmt.Errorf("exact selected administrator password verifier is invalid")
	}
	return userID, orgID, nil
}

func readFirstCustomerBootstrapReplay(r *http.Request, tx pgx.Tx, clientRequestID string, adminUserID, adminOrgID int64) (firstCustomerBootstrapResult, bool, error) {
	var result firstCustomerBootstrapResult
	err := tx.QueryRow(r.Context(), `
		SELECT o.id, o.name, w.balance_micros
		  FROM audit_logs a
		  JOIN orgs o ON o.id::text = a.resource_id
		  JOIN wallets w ON w.org_id = o.id
		 WHERE a.action = $1 AND a.client_request_id = $2
		   AND a.user_id = $3 AND a.org_id = $4
		   AND o.status = 'active' AND o.parent_id IS NULL AND o.org_type = 'customer'
		   AND w.balance_micros = 0 AND w.granted_balance_micros = 0
		   AND w.frozen_micros = 0 AND w.total_spent_micros = 0`,
		firstCustomerBootstrapAction, clientRequestID, adminUserID, adminOrgID,
	).Scan(&result.OrganizationID, &result.OrganizationName, &result.WalletBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		return firstCustomerBootstrapResult{}, false, nil
	}
	return result, err == nil, err
}

func verifyFirstCustomerEmptyCheckpoint(r *http.Request, tx pgx.Tx, adminUserID, adminOrgID int64) error {
	retainedSettingKeys := append(resetplan.ProtectedSettingKeys(), resetplan.PlatformBaselineSettingKeys()...)
	expectedTables := resetplan.ExpectedPublicTables()
	checks := []struct {
		name  string
		query string
		args  []any
		want  int64
	}{
		{name: "selected user", query: "SELECT COUNT(*) FROM users WHERE id = $1 AND org_id = $2", args: []any{adminUserID, adminOrgID}, want: 1},
		{name: "all users", query: "SELECT COUNT(*) FROM users", want: 1},
		{name: "selected org", query: "SELECT COUNT(*) FROM orgs WHERE id = $1 AND parent_id IS NULL AND status = 'active' AND settings = '{}'::jsonb", args: []any{adminOrgID}, want: 1},
		{name: "all orgs", query: "SELECT COUNT(*) FROM orgs", want: 1},
		{name: "wallets", query: "SELECT COUNT(*) FROM wallets", want: 0},
		{name: "session contamination", query: "SELECT COUNT(*) FROM admin_sessions WHERE user_id <> $1 OR revoked_at IS NOT NULL OR expires_at <= NOW()", args: []any{adminUserID}, want: 0},
		{name: "one post-reset session", query: "SELECT COUNT(*) FROM admin_sessions WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()", args: []any{adminUserID}, want: 1},
		{name: "login audit contamination", query: "SELECT COUNT(*) FROM audit_logs WHERE user_id <> $1 OR org_id <> $2 OR action <> 'login'", args: []any{adminUserID, adminOrgID}, want: 0},
		{name: "login outbox contamination", query: `SELECT COUNT(*) FROM internal_outbox_events
			WHERE event_type <> 'evidence.audit.v1' OR org_id <> $1
			   OR (payload->>'action') IS DISTINCT FROM 'login'
			   OR (payload->>'user_id') IS DISTINCT FROM ($2::bigint)::text`, args: []any{adminOrgID, adminUserID}, want: 0},
		{name: "non-platform settings", query: "SELECT COUNT(*) FROM settings WHERE NOT (key = ANY($1::text[]))", args: []any{retainedSettingKeys}, want: 0},
		{name: "ledger unit settings", query: "SELECT COUNT(*) FROM settings WHERE key = ANY($1::text[])", args: []any{[]string{"ledger.amount_scale", "ledger.version"}}, want: 2},
		{name: "sensitive words empty baseline", query: "SELECT COUNT(*) FROM settings WHERE key = 'sensitive_words' AND value = '[]'::jsonb", want: 1},
		{name: "ledger runtime marker", query: "SELECT COUNT(*) FROM micro_ledger_runtime_state WHERE singleton", want: 1},
		{name: "known public tables", query: "SELECT COUNT(*) FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = ANY($1::text[])", args: []any{expectedTables}, want: int64(len(expectedTables))},
		{name: "exact public table inventory", query: "SELECT COUNT(*) FROM pg_catalog.pg_tables WHERE schemaname = 'public'", want: int64(len(expectedTables))},
		{name: "latest reset support migration", query: `SELECT COUNT(*) FROM schema_migrations
			WHERE version = '000057_second_integration_reset_support'
			  AND version = (SELECT MAX(version) FROM schema_migrations)`, want: 1},
	}
	for _, check := range checks {
		var count int64
		if err := tx.QueryRow(r.Context(), check.query, check.args...).Scan(&count); err != nil {
			return fmt.Errorf("check %s: %w", check.name, err)
		}
		if count != check.want {
			return fmt.Errorf("check %s: got %d want %d", check.name, count, check.want)
		}
	}
	for _, name := range resetplan.BootstrapZeroTables() {
		query := "SELECT COUNT(*) FROM " + pgx.Identifier{"public", name}.Sanitize()
		var count int64
		if err := tx.QueryRow(r.Context(), query).Scan(&count); err != nil {
			return fmt.Errorf("check empty table %s: %w", name, err)
		}
		if count != 0 {
			return fmt.Errorf("check empty table %s: got %d", name, count)
		}
	}
	return nil
}

func validFirstCustomerOrganizationName(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 160 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
