package admin

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// verifyFirstCustomerRuntimeResetCheckpoint accepts the reset-owned empty
// business state after EnsureBaseline has created its automatic runtime shell.
// It is not a permissive fresh-install fallback: the exact runtime project,
// reset settings, partner root, artifact ledger and empty product tables must
// all match. Platform License/Release state remains owned by the reset contract.
// Only the already-authenticated local admin's login evidence and session
// history may remain; the committing session is still checked under its lock.
func (h *Handler) verifyFirstCustomerRuntimeResetCheckpoint(r *http.Request, tx pgx.Tx, adminUserID, adminOrgID int64) error {
	retainedSettingKeys := append(resetplan.ProtectedSettingKeys(), resetplan.PlatformBaselineSettingKeys()...)
	expectedTables := resetplan.ExpectedPublicTables()
	checks := []struct {
		name  string
		query string
		args  []any
		want  int64
	}{
		{"runtime admin", `SELECT COUNT(*) FROM users WHERE id=$1 AND org_id=$2
			AND public.normalize_local_login_id(username)='admin'
			AND auth_epoch>=1
			AND organization_role='platform_admin' AND role IN ('admin','platform_admin')
			AND status='active' AND COALESCE(NULLIF(auth_provider,''),'local')='local'`, []any{adminUserID, adminOrgID}, 1},
		{"runtime user count", `SELECT COUNT(*) FROM users`, nil, 1},
		{"runtime partner root", `SELECT COUNT(*) FROM orgs WHERE id=$1
			AND parent_id IS NULL AND status='active' AND org_type='partner'
			AND settings='{}'::jsonb`, []any{adminOrgID}, 1},
		{"runtime organization count", `SELECT COUNT(*) FROM orgs`, nil, 1},
		{"runtime zero wallet", `SELECT COUNT(*) FROM wallets WHERE org_id=$1
			AND balance_micros=0 AND granted_balance_micros=0 AND frozen_micros=0
			AND total_spent_micros=0 AND currency='CNY' AND auto_freeze=FALSE
			AND low_balance_threshold_cents=0 AND freeze_expire_at IS NULL`, []any{adminOrgID}, 1},
		{"runtime wallet count", `SELECT COUNT(*) FROM wallets`, nil, 1},
		{"runtime automatic project", `SELECT COUNT(*) FROM projects WHERE org_id=$1
			AND name='Default Project' AND slug='default'
			AND description='Default project for this UniRouter deployment'
			AND status='active' AND budget_cents IS NULL AND budget_period='monthly'
			AND created_by IS NULL`, []any{adminOrgID}, 1},
		{"runtime project count", `SELECT COUNT(*) FROM projects`, nil, 1},
		{"runtime foreign session", `SELECT COUNT(*) FROM admin_sessions WHERE user_id IS DISTINCT FROM $1`, []any{adminUserID}, 0},
		{"runtime login audit contamination", `SELECT COUNT(*) FROM audit_logs
			WHERE user_id IS DISTINCT FROM $1 OR org_id IS DISTINCT FROM $2
			OR project_id IS NOT NULL OR action IS DISTINCT FROM 'login'
			OR source IS DISTINCT FROM 'admin_console' OR resource IS DISTINCT FROM 'user'
			OR resource_id IS DISTINCT FROM ($1::bigint)::text
			OR before_state IS DISTINCT FROM '{}'::jsonb OR after_state IS DISTINCT FROM '{}'::jsonb
			OR detail IS DISTINCT FROM (SELECT jsonb_build_object(
				'username',username,'organization_role',organization_role,'compatibility_role',role)
				FROM users WHERE id=$1)`, []any{adminUserID, adminOrgID}, 0},
		{"runtime login outbox contamination", `SELECT COUNT(*) FROM internal_outbox_events
			WHERE event_type IS DISTINCT FROM 'evidence.audit.v1' OR org_id IS DISTINCT FROM $2
			OR (payload->>'action') IS DISTINCT FROM 'login'
			OR (payload->>'user_id') IS DISTINCT FROM ($1::bigint)::text
			OR (payload->>'org_id') IS DISTINCT FROM ($2::bigint)::text
			OR COALESCE(payload->'project_id','null'::jsonb)<>'null'::jsonb
			OR (payload->>'resource') IS DISTINCT FROM 'user'
			OR (payload->>'resource_id') IS DISTINCT FROM ($1::bigint)::text
			OR (payload->>'source') IS DISTINCT FROM 'admin_console'
			OR (payload->'before_state') IS DISTINCT FROM '{}'::jsonb
			OR (payload->'after_state') IS DISTINCT FROM '{}'::jsonb
			OR (payload->'detail') IS DISTINCT FROM (SELECT jsonb_build_object(
				'username',username,'organization_role',organization_role,'compatibility_role',role)
				FROM users WHERE id=$1)
			OR dead_lettered_at IS NOT NULL`, []any{adminUserID, adminOrgID}, 0},
		{"runtime reset settings", `SELECT COUNT(*) FROM settings WHERE NOT (key=ANY($1::text[]))`, []any{retainedSettingKeys}, 0},
		{"runtime ledger settings", `SELECT COUNT(*) FROM settings WHERE key=ANY($1::text[])`, []any{[]string{"ledger.amount_scale", "ledger.version"}}, 2},
		{"runtime empty sensitive words", `SELECT COUNT(*) FROM settings WHERE key='sensitive_words' AND value='[]'::jsonb`, nil, 1},
		{"runtime ledger marker", `SELECT COUNT(*) FROM micro_ledger_runtime_state WHERE singleton`, nil, 1},
		{"runtime known tables", `SELECT COUNT(*) FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename=ANY($1::text[])`, []any{expectedTables}, int64(len(expectedTables))},
		{"runtime table count", `SELECT COUNT(*) FROM pg_catalog.pg_tables WHERE schemaname='public'`, nil, int64(len(expectedTables))},
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
		return fmt.Errorf("read runtime migration ledger: %w", err)
	}
	if err := schemaauthority.VerifyLedger(h.runtimeOptions.SchemaMigrations, actualMigrations); err != nil {
		return fmt.Errorf("check runtime migration ledger: %w", err)
	}
	for _, name := range resetplan.BootstrapZeroTables() {
		if name == "projects" {
			continue
		}
		var count int64
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM "+pgx.Identifier{"public", name}.Sanitize()).Scan(&count); err != nil {
			return fmt.Errorf("check runtime empty table %s: %w", name, err)
		}
		if count != 0 {
			return fmt.Errorf("check runtime empty table %s: got %d", name, count)
		}
	}
	return nil
}

// Called only after the complete checkpoint and committing-actor checks, in
// the same SERIALIZABLE transaction as customer creation and its audit receipt.
// No login/audit/session or platform identity is deleted or reassigned.
func normalizeFirstCustomerRuntimeShell(r *http.Request, tx pgx.Tx, adminOrgID int64) error {
	project, err := tx.Exec(r.Context(), `DELETE FROM projects WHERE org_id=$1
		AND name='Default Project' AND slug='default'
		AND description='Default project for this UniRouter deployment'
		AND status='active' AND budget_cents IS NULL AND budget_period='monthly'
		AND created_by IS NULL`, adminOrgID)
	if err != nil || project.RowsAffected() != 1 {
		return fmt.Errorf("normalize runtime default project")
	}
	wallet, err := tx.Exec(r.Context(), `DELETE FROM wallets WHERE org_id=$1
		AND balance_micros=0 AND granted_balance_micros=0 AND frozen_micros=0
		AND total_spent_micros=0 AND currency='CNY' AND auto_freeze=FALSE
		AND low_balance_threshold_cents=0 AND freeze_expire_at IS NULL`, adminOrgID)
	if err != nil || wallet.RowsAffected() != 1 {
		return fmt.Errorf("normalize runtime zero wallet")
	}
	return nil
}
