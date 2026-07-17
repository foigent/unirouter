//go:build integration

package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPolicyMutationAuditAtomicityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_policy_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.request_id = 'policy-audit-atomicity-test'
			   AND NEW.action IN (
				'guardrail_policy.create', 'guardrail_policy.update',
				'limit_policy.create', 'limit_policy.update'
			   ) THEN
				RAISE EXCEPTION 'forced policy audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		CREATE TRIGGER reject_policy_audit_for_test
		BEFORE INSERT ON audit_logs
		FOR EACH ROW EXECUTE FUNCTION reject_policy_audit_for_test()`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO orgs (id, name, status) VALUES (1, 'policy-audit-test-org', 'active')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, org_id, username, password_hash, role, status)
		VALUES (7, 1, 'policy-audit-test-admin', 'test-hash', 'platform_admin', 'active')`)
	require.NoError(t, err)

	var guardrailID, limitID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO guardrail_policies
			(name, stage, detector_ref, action, mode, failure_mode, config, version)
		VALUES ('guardrail-existing', 'input', 'builtin:pii_secrets', 'audit', 'shadow', 'degraded_allow', '{}', 1)
		RETURNING id`).Scan(&guardrailID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO distributed_limit_policies
			(name, mode, failure_mode, rpm_limit, version)
		VALUES ('limit-existing', 'shadow', 'fail_closed', 10, 1)
		RETURNING id`).Scan(&limitID))

	t.Run("guardrail_create", func(t *testing.T) {
		state := &policyAuditTransactionState{}
		h := newPolicyAuditIntegrationHandler(pool, state)
		recorder := httptest.NewRecorder()
		h.CreateGuardrailPolicy(recorder, authenticatedPolicyAuditRequest(http.MethodPost, "/api/v1/guardrail-policies",
			`{"name":"guardrail-rollback","stage":"input","detector_ref":"builtin:pii_secrets","action":"redact"}`))

		require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "Audit failed")
		require.Equal(t, 0, policyRowCount(t, pool, "guardrail_policies", "guardrail-rollback"))
		require.Equal(t, 0, policyAuditCount(t, pool, "guardrail_policy.create"))
		require.Zero(t, state.refreshCalls)
	})

	t.Run("guardrail_update", func(t *testing.T) {
		state := &policyAuditTransactionState{}
		h := newPolicyAuditIntegrationHandler(pool, state)
		recorder := httptest.NewRecorder()
		req := authenticatedPolicyAuditRequest(http.MethodPut, fmt.Sprintf("/api/v1/guardrail-policies/%d", guardrailID),
			`{"version":1,"mode":"enforce","action":"redact"}`)
		req.SetPathValue("id", fmt.Sprintf("%d", guardrailID))
		h.UpdateGuardrailPolicy(recorder, req)

		require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "Audit failed")
		var version int64
		var mode, action string
		require.NoError(t, pool.QueryRow(ctx, `SELECT version, mode, action FROM guardrail_policies WHERE id=$1`, guardrailID).Scan(&version, &mode, &action))
		require.Equal(t, int64(1), version)
		require.Equal(t, "shadow", mode)
		require.Equal(t, "audit", action)
		require.Equal(t, 0, policyAuditCount(t, pool, "guardrail_policy.update"))
		require.Zero(t, state.refreshCalls)
	})

	t.Run("limit_create", func(t *testing.T) {
		state := &policyAuditTransactionState{}
		h := newPolicyAuditIntegrationHandler(pool, state)
		recorder := httptest.NewRecorder()
		h.CreateLimitPolicy(recorder, authenticatedPolicyAuditRequest(http.MethodPost, "/api/v1/limit-policies",
			`{"name":"limit-rollback","mode":"enforce","failure_mode":"fail_closed","rpm_limit":10}`))

		require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "Audit failed")
		require.Equal(t, 0, policyRowCount(t, pool, "distributed_limit_policies", "limit-rollback"))
		require.Equal(t, 0, policyAuditCount(t, pool, "limit_policy.create"))
		require.Zero(t, state.refreshCalls)
	})

	t.Run("limit_update", func(t *testing.T) {
		state := &policyAuditTransactionState{}
		h := newPolicyAuditIntegrationHandler(pool, state)
		recorder := httptest.NewRecorder()
		req := authenticatedPolicyAuditRequest(http.MethodPut, fmt.Sprintf("/api/v1/limit-policies/%d", limitID),
			`{"version":1,"mode":"enforce","rpm_limit":20}`)
		req.SetPathValue("id", fmt.Sprintf("%d", limitID))
		h.UpdateLimitPolicy(recorder, req)

		require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "Audit failed")
		var version, rpm int64
		var mode string
		require.NoError(t, pool.QueryRow(ctx, `SELECT version, mode, rpm_limit FROM distributed_limit_policies WHERE id=$1`, limitID).Scan(&version, &mode, &rpm))
		require.Equal(t, int64(1), version)
		require.Equal(t, "shadow", mode)
		require.Equal(t, int64(10), rpm)
		require.Equal(t, 0, policyAuditCount(t, pool, "limit_policy.update"))
		require.Zero(t, state.refreshCalls)
	})
}

func newPolicyAuditIntegrationHandler(db *pgxpool.Pool, state *policyAuditTransactionState) *Handler {
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	h.SetGuardrailController(policyAuditGuardrailController{state: state})
	h.SetLimitPolicyReloader(policyAuditLimitPolicyReloader{state: state})
	return h
}

func policyRowCount(t *testing.T, db *pgxpool.Pool, table, name string) int {
	t.Helper()
	query := ""
	switch table {
	case "guardrail_policies":
		query = `SELECT COUNT(*) FROM guardrail_policies WHERE name=$1`
	case "distributed_limit_policies":
		query = `SELECT COUNT(*) FROM distributed_limit_policies WHERE name=$1`
	default:
		t.Fatalf("unsupported policy table %q", table)
	}
	var count int
	require.NoError(t, db.QueryRow(context.Background(), query, name).Scan(&count))
	return count
}

func policyAuditCount(t *testing.T, db *pgxpool.Pool, action string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action=$1 AND request_id='policy-audit-atomicity-test'`, action).Scan(&count))
	return count
}
