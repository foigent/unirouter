package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"golang.org/x/crypto/bcrypt"
)

const firstCustomerBootstrapAction = "bootstrap.first_customer"

type firstCustomerBootstrapCheckpoint uint8

const (
	firstCustomerBootstrapCheckpointStrictReset firstCustomerBootstrapCheckpoint = iota + 1
	firstCustomerBootstrapCheckpointFreshSeed
)

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
	if !auth.IsPlatformAdmin(actorOrganizationRole(r.Context())) {
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
	if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{
		TargetOrgID:              middleware.GetOrgIDFromContext(r.Context()),
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin},
	}); err != nil {
		if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "First-customer bootstrap admin preflight failed"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Bootstrap authorization failed"})
		}
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

	checkpoint, err := h.verifyFirstCustomerBootstrapCheckpoint(r, tx, adminUserID, adminOrgID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "First-customer bootstrap checkpoint is not satisfied",
		})
		return
	}
	if checkpoint == firstCustomerBootstrapCheckpointFreshSeed {
		if err := normalizeFirstCustomerFreshSeed(r, tx, adminUserID, adminOrgID); err != nil {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error": "First-customer bootstrap checkpoint changed before commit",
			})
			return
		}
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

func (h *Handler) verifyFirstCustomerBootstrapCheckpoint(r *http.Request, tx pgx.Tx, adminUserID, adminOrgID int64) (firstCustomerBootstrapCheckpoint, error) {
	if err := verifyFirstCustomerEmptyCheckpoint(r, tx, adminUserID, adminOrgID); err == nil {
		return firstCustomerBootstrapCheckpointStrictReset, nil
	}
	if err := h.verifyFirstCustomerFreshMigrationCheckpoint(r, tx, adminUserID, adminOrgID); err != nil {
		return 0, err
	}
	return firstCustomerBootstrapCheckpointFreshSeed, nil
}

// verifyFirstCustomerFreshMigrationCheckpoint accepts only the exact seed
// state produced by the current migration chain after the default admin has
// been rotated and logged in once. This is deliberately separate from the
// second-integration reset checkpoint. The exact seed wallet/project are
// normalized only after every invariant has matched, in the same SERIALIZABLE
// transaction that creates the first customer.
func (h *Handler) verifyFirstCustomerFreshMigrationCheckpoint(r *http.Request, tx pgx.Tx, adminUserID, adminOrgID int64) error {
	expectedTables := resetplan.ExpectedPublicTables()
	checks := []struct {
		name  string
		query string
		args  []any
		want  int64
	}{
		{name: "selected fresh user", query: `SELECT COUNT(*) FROM users
			WHERE id=$1 AND org_id=$2 AND public.normalize_local_login_id(username)='admin'
			  AND auth_epoch>=1 AND organization_role='platform_admin'
			  AND role IN ('admin','platform_admin') AND status='active'
			  AND COALESCE(NULLIF(auth_provider,''),'local')='local'
			  AND email IS NULL AND oidc_issuer IS NULL AND oidc_subject IS NULL`, args: []any{adminUserID, adminOrgID}, want: 1},
		{name: "all fresh users", query: "SELECT COUNT(*) FROM users", want: 1},
		{name: "selected fresh org", query: `SELECT COUNT(*) FROM orgs
			WHERE id=1 AND id=$1 AND name='Platform Admin'
			  AND parent_id IS NULL AND status='active' AND org_type='partner'
			  AND settings='{}'::jsonb`, args: []any{adminOrgID}, want: 1},
		{name: "all fresh orgs", query: "SELECT COUNT(*) FROM orgs", want: 1},
		{name: "fresh partner wallet", query: `SELECT COUNT(*) FROM wallets
			WHERE org_id=$1 AND balance_micros=0 AND granted_balance_micros=0
			  AND frozen_micros=0 AND total_spent_micros=0 AND currency='CNY'
			  AND auto_freeze=FALSE AND low_balance_threshold_cents=0
			  AND freeze_expire_at IS NULL`, args: []any{adminOrgID}, want: 1},
		{name: "all fresh wallets", query: "SELECT COUNT(*) FROM wallets", want: 1},
		{name: "fresh default project", query: `SELECT COUNT(*) FROM projects
			WHERE org_id=$1 AND name='Default Project' AND slug='default'
			  AND description='Default project created for PRD2 compatibility'
			  AND status='active' AND budget_cents IS NULL
			  AND budget_period='monthly' AND created_by IS NULL`, args: []any{adminOrgID}, want: 1},
		{name: "all fresh projects", query: "SELECT COUNT(*) FROM projects", want: 1},
		{name: "fresh default membership contamination", query: `SELECT COUNT(*) FROM project_members member
			JOIN projects project ON project.id=member.project_id
			WHERE project.org_id<>$1 OR project.slug<>'default'
			   OR member.user_id<>$2 OR member.role<>'project_admin'`, args: []any{adminOrgID, adminUserID}, want: 0},
		{name: "fresh default membership multiplicity", query: `SELECT GREATEST(COUNT(*)-1,0) FROM project_members`, want: 0},
		{name: "fresh session contamination", query: `SELECT COUNT(*) FROM admin_sessions
			WHERE user_id<>$1 OR revoked_at IS NOT NULL OR expires_at<=NOW()`, args: []any{adminUserID}, want: 0},
		{name: "one fresh session", query: `SELECT COUNT(*) FROM admin_sessions
			WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>NOW()`, args: []any{adminUserID}, want: 1},
		{name: "fresh audit contamination", query: `SELECT COUNT(*) FROM audit_logs
			WHERE user_id<>$1 OR org_id<>$2 OR action NOT IN ('login','user.password_reset')
			   OR (action='user.password_reset' AND (
			       source IS DISTINCT FROM 'system'
			       OR resource IS DISTINCT FROM 'user'
			       OR resource_id IS DISTINCT FROM ($1::bigint)::text
			       OR detail IS DISTINCT FROM '{"reason":"recovery_cli","sessions_revoked":"all"}'::jsonb
			       OR before_state IS DISTINCT FROM '{}'::jsonb
			       OR after_state IS DISTINCT FROM '{"password_reset":true,"sessions_revoked":"all"}'::jsonb
			   )) OR (action='login' AND (
			       source IS DISTINCT FROM 'admin_console'
			       OR resource IS DISTINCT FROM 'user'
			       OR resource_id IS DISTINCT FROM ($1::bigint)::text
			       OR detail IS DISTINCT FROM jsonb_build_object(
			           'username','admin','organization_role','platform_admin',
			           'compatibility_role','admin')
			       OR before_state IS DISTINCT FROM '{}'::jsonb
			       OR after_state IS DISTINCT FROM '{}'::jsonb
			   ))`, args: []any{adminUserID, adminOrgID}, want: 0},
		{name: "fresh login audit multiplicity", query: `SELECT GREATEST(COUNT(*)-1,0) FROM audit_logs
			WHERE user_id=$1 AND org_id=$2 AND action='login'`, args: []any{adminUserID, adminOrgID}, want: 0},
		{name: "one fresh password rotation", query: `SELECT COUNT(*) FROM audit_logs
			WHERE user_id=$1 AND org_id=$2 AND action='user.password_reset'`, args: []any{adminUserID, adminOrgID}, want: 1},
		{name: "fresh login outbox contamination", query: `SELECT COUNT(*) FROM internal_outbox_events
			WHERE event_type<>'evidence.audit.v1' OR org_id<>$1
			   OR (payload->>'action') IS DISTINCT FROM 'login'
			   OR (payload->>'user_id') IS DISTINCT FROM ($2::bigint)::text
			   OR (payload->>'resource') IS DISTINCT FROM 'user'
			   OR (payload->>'resource_id') IS DISTINCT FROM ($2::bigint)::text
			   OR (payload->>'source') IS DISTINCT FROM 'admin_console'
			   OR (payload->'detail') IS DISTINCT FROM jsonb_build_object(
			      'username','admin','organization_role','platform_admin',
			      'compatibility_role','admin')
			   OR (payload->'before_state') IS DISTINCT FROM '{}'::jsonb
			   OR (payload->'after_state') IS DISTINCT FROM '{}'::jsonb
			   OR dead_lettered_at IS NOT NULL`, args: []any{adminOrgID, adminUserID}, want: 0},
		{name: "fresh login outbox multiplicity", query: `SELECT GREATEST(COUNT(*)-1,0) FROM internal_outbox_events`, want: 0},
		{name: "one logical fresh login", query: `SELECT
			(SELECT COUNT(*) FROM audit_logs WHERE user_id=$1 AND org_id=$2 AND action='login')
			+ (SELECT COUNT(*) FROM internal_outbox_events
			    WHERE event_type='evidence.audit.v1' AND org_id=$2
			      AND (payload->>'user_id')=($1::bigint)::text
			      AND (payload->>'action')='login')
			- (SELECT COUNT(*) FROM audit_logs audit
			   JOIN internal_outbox_events event ON event.event_key=audit.outbox_event_key
			   WHERE audit.user_id=$1 AND audit.org_id=$2 AND audit.action='login'
			     AND event.event_type='evidence.audit.v1')`, args: []any{adminUserID, adminOrgID}, want: 1},
		{name: "fresh setting count", query: "SELECT COUNT(*) FROM settings", want: 15},
		{name: "fresh setting drift", query: `SELECT COUNT(*) FROM settings WHERE NOT (
			(key='webhook.feishu' AND value='""'::jsonb) OR
			(key='webhook.dingtalk' AND value='""'::jsonb) OR
			(key='sensitive_words' AND value='[]'::jsonb) OR
			(key='backup.enabled' AND value='true'::jsonb) OR
			(key='backup.cron' AND value='"0 2 * * *"'::jsonb) OR
			(key='backup.retain_days' AND value='7'::jsonb) OR
			(key='site_name' AND value='"UniRoute"'::jsonb) OR
			(key='default_model' AND value='"gpt-3.5-turbo"'::jsonb) OR
			(key='enable_registration' AND value='true'::jsonb) OR
			(key='maintenance_mode' AND value='false'::jsonb) OR
			(key='budget_alert_webhook_url' AND value='""'::jsonb) OR
			(key='budget_alert_threshold_cents' AND value='1000'::jsonb) OR
			(key='token_metering.estimated_ratio_alert_threshold' AND value='0.2'::jsonb) OR
			(key='ledger.version' AND value='"micro-cny.v1"'::jsonb) OR
			(key='ledger.amount_scale' AND value='1000000'::jsonb)
		)`, want: 0},
		{name: "fresh ledger runtime marker", query: "SELECT COUNT(*) FROM micro_ledger_runtime_state WHERE singleton", want: 1},
		{name: "fresh commercial entitlement", query: "SELECT COUNT(*) FROM commercial_license_entitlements", want: 0},
		{name: "fresh deployment profile", query: "SELECT COUNT(*) FROM deployment_profiles", want: 0},
		{name: "fresh gateway endpoint", query: "SELECT COUNT(*) FROM gateway_endpoints", want: 0},
		{name: "fresh compiled execution support", query: `SELECT COUNT(*) FROM model_operation_execution_support_bindings
			WHERE (operation_id,public_path,protocol_profile_id,protocol_profile_version,
			       executor_contract_version,introduced_migration) IN (
			 ('openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v1','1.0.0',
			  'unirouter.endpoint-profile-nonstream-executor.v1','000075_model_operation_production_qualification'),
			 ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v1','1.0.0',
			  'unirouter.endpoint-profile-nonstream-executor.v1','000075_model_operation_production_qualification'),
			 ('openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v2','1.0.0',
			  'unirouter.endpoint-profile-responses-dual-executor.v1','000086_exact_public_execution_bindings'),
			 ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v2','1.0.0',
			  'unirouter.endpoint-profile-messages-dual-executor.v1','000086_exact_public_execution_bindings'),
			 ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text-tools.v3','1.0.0',
			  'unirouter.endpoint-profile-messages-tools-executor.v1','000087_anthropic_messages_tools_public_binding')
			)`, want: 5},
		{name: "fresh compiled execution support count", query: "SELECT COUNT(*) FROM model_operation_execution_support_bindings", want: 5},
		{name: "fresh public execution extension", query: `SELECT COUNT(*) FROM model_operation_public_execution_contract_extensions
			WHERE (operation_id,public_path,protocol_profile_id,protocol_profile_version,
			       executor_contract_version,delivery_contract_version,
			       charge_contract_version,idempotency_contract_version,introduced_migration) IN (
			 ('openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v1','1.0.0',
			  'unirouter.endpoint-profile-nonstream-executor.v1',
			  'unirouter.public-nonstream-delivery.v1','unirouter.provider-final-token-charge.v1',
			  'unirouter.public-at-most-once.v1','000077_public_model_execution_authority'),
			 ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v1','1.0.0',
			  'unirouter.endpoint-profile-nonstream-executor.v1',
			  'unirouter.public-nonstream-delivery.v1','unirouter.provider-final-token-charge.v1',
			  'unirouter.public-at-most-once.v1','000077_public_model_execution_authority'),
			 ('openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v2','1.0.0',
			  'unirouter.endpoint-profile-responses-dual-executor.v1',
			  'unirouter.public-responses-delivery.v2','unirouter.provider-final-token-charge.v1',
			  'unirouter.public-at-most-once.v1','000086_exact_public_execution_bindings'),
			 ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v2','1.0.0',
			  'unirouter.endpoint-profile-messages-dual-executor.v1',
			  'unirouter.public-anthropic-messages-delivery.v2','unirouter.provider-final-token-charge.v1',
			  'unirouter.public-at-most-once.v1','000086_exact_public_execution_bindings'),
			 ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text-tools.v3','1.0.0',
			  'unirouter.endpoint-profile-messages-tools-executor.v1',
			  'unirouter.public-anthropic-messages-tools-delivery.v1','unirouter.provider-final-token-charge.v1',
			  'unirouter.public-at-most-once.v1','000087_anthropic_messages_tools_public_binding')
			)`, want: 5},
		{name: "fresh public execution extension count", query: "SELECT COUNT(*) FROM model_operation_public_execution_contract_extensions", want: 5},
		{name: "fresh known public tables", query: `SELECT COUNT(*) FROM pg_catalog.pg_tables
			WHERE schemaname='public' AND tablename=ANY($1::text[])`, args: []any{expectedTables}, want: int64(len(expectedTables))},
		{name: "fresh exact public table inventory", query: "SELECT COUNT(*) FROM pg_catalog.pg_tables WHERE schemaname='public'", want: int64(len(expectedTables))},
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
	var actualMigrations []string
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(array_agg(version ORDER BY version),ARRAY[]::text[]) FROM schema_migrations`).Scan(&actualMigrations); err != nil {
		return fmt.Errorf("read fresh migration ledger: %w", err)
	}
	if err := schemaauthority.VerifyLedger(h.runtimeOptions.SchemaMigrations, actualMigrations); err != nil {
		return fmt.Errorf("check fresh migration ledger: %w", err)
	}
	if err := h.verifyFirstCustomerFreshReleaseEvidence(r, tx); err != nil {
		return err
	}
	for _, name := range resetplan.BootstrapZeroTables() {
		if name == "projects" || name == "project_members" {
			continue
		}
		query := "SELECT COUNT(*) FROM " + pgx.Identifier{"public", name}.Sanitize()
		var count int64
		if err := tx.QueryRow(r.Context(), query).Scan(&count); err != nil {
			return fmt.Errorf("check fresh empty table %s: %w", name, err)
		}
		if count != 0 {
			return fmt.Errorf("check fresh empty table %s: got %d", name, count)
		}
	}
	return nil
}

func (h *Handler) verifyFirstCustomerFreshReleaseEvidence(r *http.Request, tx pgx.Tx) error {
	requireRecordedRelease, err := freshBootstrapRequiresRecordedRelease(h.runtimeOptions.Environment)
	if err != nil {
		return err
	}

	var count int64
	if requireRecordedRelease {
		err = tx.QueryRow(r.Context(), `
			SELECT COUNT(*)
			  FROM release_state state
			  JOIN release_history history ON
			       history.release_sequence=state.release_sequence
			   AND history.version=state.version
			   AND history.commit=state.commit
			   AND history.package_sha256=state.package_sha256
			   AND history.trusted_key_id=state.trusted_key_id
			   AND history.target_schema=state.target_schema
			   AND history.manifest=state.manifest
			 WHERE state.singleton
			   AND state.release_sequence>0
			   AND BTRIM(state.version)<>'' AND BTRIM(state.commit)<>''
			   AND state.package_sha256 ~ '^[0-9a-f]{64}$'
			   AND BTRIM(state.trusted_key_id)<>''
			   AND state.target_schema=$1
			   AND jsonb_typeof(state.manifest)='object'
			   AND (SELECT COUNT(*) FROM release_state)=1
			   AND (SELECT COUNT(*) FROM release_history)=1`, schemaauthority.LatestSupportedMigration).Scan(&count)
		if err != nil || count != 1 {
			return fmt.Errorf("check recorded fresh release evidence")
		}
		return nil
	}
	err = tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM release_state
		WHERE singleton AND release_sequence=0 AND version='' AND commit=''
		  AND package_sha256 IS NULL AND trusted_key_id='' AND target_schema=''
		  AND manifest IS NULL AND (SELECT COUNT(*) FROM release_history)=0`).Scan(&count)
	if err != nil || count != 1 {
		return fmt.Errorf("check local fresh release baseline")
	}
	return nil
}

func freshBootstrapRequiresRecordedRelease(environment string) (bool, error) {
	environment = strings.ToLower(strings.TrimSpace(environment))
	switch environment {
	case "", "unspecified", "local", "dev", "development", "test":
		return false, nil
	case "staging", "prod", "production":
		return true, nil
	default:
		return false, fmt.Errorf("unsupported runtime environment %q", environment)
	}
}

func normalizeFirstCustomerFreshSeed(r *http.Request, tx pgx.Tx, adminUserID, adminOrgID int64) error {
	membership, err := tx.Exec(r.Context(), `DELETE FROM project_members member
		USING projects project
		WHERE member.project_id=project.id AND project.org_id=$1
		  AND project.slug='default' AND member.user_id=$2
		  AND member.role='project_admin'`, adminOrgID, adminUserID)
	if err != nil || membership.RowsAffected() > 1 {
		return fmt.Errorf("normalize fresh project membership")
	}
	project, err := tx.Exec(r.Context(), `DELETE FROM projects
		WHERE org_id=$1 AND name='Default Project' AND slug='default'
		  AND description='Default project created for PRD2 compatibility'
		  AND status='active' AND budget_cents IS NULL
		  AND budget_period='monthly' AND created_by IS NULL`, adminOrgID)
	if err != nil || project.RowsAffected() != 1 {
		return fmt.Errorf("normalize fresh default project")
	}
	wallet, err := tx.Exec(r.Context(), `DELETE FROM wallets
		WHERE org_id=$1 AND balance_micros=0 AND granted_balance_micros=0
		  AND frozen_micros=0 AND total_spent_micros=0 AND currency='CNY'
		  AND auto_freeze=FALSE AND low_balance_threshold_cents=0
		  AND freeze_expire_at IS NULL`, adminOrgID)
	if err != nil || wallet.RowsAffected() != 1 {
		return fmt.Errorf("normalize fresh partner wallet")
	}
	return nil
}

func lockExactBootstrapAdmin(r *http.Request, tx pgx.Tx) (int64, int64, error) {
	var userID, orgID int64
	var organizationRole, role, userStatus, orgStatus, provider, passwordHash string
	err := tx.QueryRow(r.Context(), `
		WITH candidates AS MATERIALIZED (
			SELECT u.id, u.org_id, u.organization_role, u.role, u.status, u.password_hash,
			       COALESCE(NULLIF(u.auth_provider, ''), 'local') AS auth_provider,
			       o.status AS org_status
			  FROM users u
			  JOIN orgs o ON o.id = u.org_id
			 WHERE public.normalize_local_login_id(u.username) = $1
			   AND COALESCE(NULLIF(u.auth_provider, ''), 'local') = 'local'
			 FOR UPDATE OF u, o
		)
		SELECT id, org_id, organization_role, role, status, password_hash, auth_provider, org_status
		  FROM candidates
		 WHERE (SELECT COUNT(*) FROM candidates) = 1`, resetplan.PreservedLogin,
	).Scan(&userID, &orgID, &organizationRole, &role, &userStatus, &passwordHash, &provider, &orgStatus)
	if err != nil || userID != middleware.GetUserIDFromContext(r.Context()) ||
		orgID != middleware.GetOrgIDFromContext(r.Context()) ||
		organizationRole != auth.RolePlatformAdmin || !auth.IsPlatformAdmin(role) ||
		userStatus != "active" || orgStatus != "active" || provider != "local" {
		return 0, 0, fmt.Errorf("exact selected administrator is not active")
	}
	if _, err := bcrypt.Cost([]byte(passwordHash)); err != nil {
		return 0, 0, fmt.Errorf("exact selected administrator password verifier is invalid")
	}
	return userID, orgID, nil
}

func readFirstCustomerBootstrapReplay(r *http.Request, tx pgx.Tx, clientRequestID string, adminUserID, adminOrgID int64) (firstCustomerBootstrapResult, bool, error) {
	var candidateCount, validCount int64
	var organizationIDText, organizationName string
	err := tx.QueryRow(r.Context(), `
		WITH candidates AS MATERIALIZED (
			SELECT resource, resource_id, detail, source, before_state, after_state, request_id
			  FROM audit_logs
			 WHERE action=$1 AND client_request_id=$2 AND user_id=$3 AND org_id=$4
		), valid AS MATERIALIZED (
			SELECT resource_id,
			       after_state->>'organization_id' AS organization_id,
			       after_state->>'name' AS organization_name
			  FROM candidates candidate
			 WHERE candidate.resource='organization'
			   AND candidate.source='admin_console'
			   AND BTRIM(candidate.request_id)<>''
			   AND candidate.before_state='{}'::jsonb
			   AND candidate.detail=candidate.after_state
			   AND CASE WHEN jsonb_typeof(candidate.after_state)='object' THEN
			       (SELECT COUNT(*) FROM jsonb_object_keys(candidate.after_state))=4
			       AND jsonb_typeof(candidate.after_state->'organization_id')='number'
			       AND jsonb_typeof(candidate.after_state->'name')='string'
			       AND jsonb_typeof(candidate.after_state->'status')='string'
			       AND jsonb_typeof(candidate.after_state->'wallet')='string'
			       ELSE FALSE
			   END
			   AND candidate.resource_id ~ '^[1-9][0-9]*$'
			   AND candidate.after_state->>'organization_id'=candidate.resource_id
			   AND BTRIM(COALESCE(candidate.after_state->>'name',''))<>''
			   AND candidate.after_state->>'status'='active'
			   AND candidate.after_state->>'wallet'='zero_micros'
		)
		SELECT (SELECT COUNT(*) FROM candidates),
		       (SELECT COUNT(*) FROM valid),
		       COALESCE((SELECT organization_id FROM valid LIMIT 1),''),
		       COALESCE((SELECT organization_name FROM valid LIMIT 1),'')`,
		firstCustomerBootstrapAction, clientRequestID, adminUserID, adminOrgID,
	).Scan(&candidateCount, &validCount, &organizationIDText, &organizationName)
	if err != nil {
		return firstCustomerBootstrapResult{}, false, err
	}
	if candidateCount == 0 {
		return firstCustomerBootstrapResult{}, false, nil
	}
	if candidateCount != 1 || validCount != 1 || !validFirstCustomerOrganizationName(organizationName) {
		return firstCustomerBootstrapResult{}, false, fmt.Errorf("bootstrap replay audit is malformed or ambiguous")
	}
	organizationID, err := strconv.ParseInt(organizationIDText, 10, 64)
	if err != nil || organizationID <= 0 {
		return firstCustomerBootstrapResult{}, false, fmt.Errorf("bootstrap replay organization identity is invalid")
	}
	return firstCustomerBootstrapResult{
		OrganizationID:   organizationID,
		OrganizationName: organizationName,
		WalletBalance:    0,
	}, true, nil
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
		{name: "latest supported migration", query: `SELECT COUNT(*) FROM schema_migrations
			WHERE version = $1
			  AND version = (SELECT MAX(version) FROM schema_migrations)`, args: []any{schemaauthority.LatestSupportedMigration}, want: 1},
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
