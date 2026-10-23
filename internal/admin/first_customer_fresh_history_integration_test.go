//go:build integration

package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func freshHistoryCheckpointFixture(t *testing.T) (*pgxpool.Pool, *Handler, int64, int64) {
	t.Helper()
	pool := testutil.SetupIsolatedTestDB(t)
	require.NoError(t, testutil.InstallExactTestArtifact(t.Context(), pool, testutil.MigrationsDir(t)))
	_, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	var user, org int64
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT id,org_id FROM users WHERE username='admin'").Scan(&user, &org))
	hash, err := bcrypt.GenerateFromPassword([]byte("fresh-history-password"), bcrypt.MinCost)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), "UPDATE users SET password_hash=$1,auth_epoch=auth_epoch+1 WHERE id=$2", string(hash), user)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), "INSERT INTO audit_logs(org_id,user_id,action,resource,resource_id,detail,source,before_state,after_state) "+
		"VALUES($1,$2,'user.password_reset','user',$2::bigint::text,"+
		"jsonb_build_object('reason','recovery_cli','sessions_revoked','all'),'system','{}'::jsonb,"+
		"jsonb_build_object('password_reset',true,'sessions_revoked','all'))", org, user)
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	h := NewHandler(pool, "fresh-history-jwt-fixture", []byte("0123456789abcdef01234567"), nil)
	h.SetRuntimeStatusOptions(RuntimeStatusOptions{Environment: "production", SchemaMigrations: ledger})
	events, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	h.SetOutbox(events)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "fresh-history-password"})
	w := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(h.Login)).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code)
	seedFreshBootstrapReleaseEvidence(t, pool)
	return pool, h, user, org
}

func freshHistoryRowsState(t *testing.T, pool *pgxpool.Pool) [32]byte {
	t.Helper()
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(runtimeBootstrapState(t, pool)), &state))
	// Fault-injection INSERTs consume PostgreSQL sequences even when rolled
	// back. Check those separately around the actual read-only checkpoint.
	delete(state, "sequences")
	wire, err := json.Marshal(state)
	require.NoError(t, err)
	return sha256.Sum256(wire)
}

func freshHistorySequenceState(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	var wire string
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT COALESCE(
	 jsonb_agg(jsonb_build_array(sequencename,last_value) ORDER BY sequencename),
	 '[]'::jsonb)::text FROM pg_sequences WHERE schemaname='public'`).Scan(&wire))
	return wire
}

func TestFreshBootstrapAuthenticationHistoryStillRefusesContaminationIntegration(t *testing.T) {
	pool, h, user, org := freshHistoryCheckpointFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil)
	cases := []struct{ name, sql, check string }{
		{"reset detail", "UPDATE audit_logs SET detail='{}'::jsonb WHERE action='user.password_reset'", "fresh audit contamination"},
		{"reset resource", "UPDATE audit_logs SET resource_id='999999' WHERE action='user.password_reset'", "fresh audit contamination"},
		{"login detail", "UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{detail}','{}'::jsonb)", "fresh login outbox contamination"},
		{"foreign outbox organization", "UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{org_id}','999'::jsonb)", "fresh login outbox contamination"},
		{"project-scoped outbox", "UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{project_id}','1'::jsonb)", "fresh login outbox contamination"},
		{"outbox action", "UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{action}','\"user.create\"'::jsonb)", "fresh login outbox contamination"},
		{"dead letter", "UPDATE internal_outbox_events SET dead_lettered_at=now()", "fresh login outbox contamination"},
		{"no rotation", "DELETE FROM audit_logs WHERE action='user.password_reset'", "fresh password rotation"},
		{"no live session", "UPDATE admin_sessions SET issued_at=now()-interval '3 days',expires_at=now()-interval '2 days'", "fresh active session"},
		{"noncanonical logout", "INSERT INTO audit_logs(org_id,user_id,action,resource,resource_id,detail,source,before_state,after_state) SELECT org_id,id,'logout','admin_session','999999',jsonb_build_object('scope','current_session'),'admin_console','{}'::jsonb,jsonb_build_object('scope','current_session') FROM users WHERE username='admin'", "fresh audit contamination"},
		{"logout active session", "INSERT INTO audit_logs(org_id,user_id,action,resource,resource_id,detail,source,before_state,after_state) SELECT u.org_id,u.id,'logout','admin_session',s.id::text,jsonb_build_object('scope','current_session'),'admin_console','{}'::jsonb,jsonb_build_object('scope','current_session') FROM users u JOIN admin_sessions s ON s.user_id=u.id", "fresh audit contamination"},
		{"business audit", "INSERT INTO audit_logs(org_id,user_id,action,resource,resource_id,detail,source,before_state,after_state) SELECT org_id,id,'user.create','user',id::text,'{}'::jsonb,'admin_console','{}'::jsonb,'{}'::jsonb FROM users", "fresh audit contamination"},
		{"project-scoped audit", "UPDATE audit_logs SET project_id=(SELECT id FROM projects LIMIT 1)", "fresh audit contamination"},
		{"nonzero wallet", "UPDATE wallets SET balance_micros=1", "fresh partner wallet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := freshHistoryRowsState(t, pool)
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(t.Context())
			require.NoError(t, h.verifyFirstCustomerFreshMigrationCheckpoint(request, tx, user, org))
			_, err = tx.Exec(t.Context(), c.sql)
			require.NoError(t, err)
			sequences := freshHistorySequenceState(t, tx)
			require.ErrorContains(t, h.verifyFirstCustomerFreshMigrationCheckpoint(request, tx, user, org), c.check)
			require.Equal(t, sequences, freshHistorySequenceState(t, tx))
			require.NoError(t, tx.Rollback(t.Context()))
			require.Equal(t, before, freshHistoryRowsState(t, pool))
		})
	}
}

func TestFreshBootstrapReleaseEvidenceStillRequiresExactCurrentHistoryIntegration(t *testing.T) {
	pool, h, _, _ := freshHistoryCheckpointFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil)
	cases := []struct{ name, sql string }{
		{"sequence", "UPDATE release_state SET release_sequence=2"},
		{"version", "UPDATE release_state SET version='different-version'"},
		{"commit", "UPDATE release_state SET commit=repeat('f',40)"},
		{"package", "UPDATE release_state SET package_sha256=repeat('b',64)"},
		{"key", "UPDATE release_state SET trusted_key_id='different-key'"},
		{"schema", "UPDATE release_state SET target_schema='000125_xinsight_connector_credential_lifecycle'"},
		{"manifest", "UPDATE release_state SET manifest=manifest||jsonb_build_object('changed',true)"},
		{"missing current", "DELETE FROM release_history"},
		{"higher history", "INSERT INTO release_history(release_sequence,version,commit,package_sha256,trusted_key_id,target_schema,manifest,recorded_by) SELECT release_sequence+1,version,commit,repeat('b',64),trusted_key_id,target_schema,manifest,'unmatched-future-fixture' FROM release_history"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := freshHistoryRowsState(t, pool)
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(t.Context())
			require.NoError(t, h.verifyFirstCustomerFreshReleaseEvidence(request, tx))
			_, err = tx.Exec(t.Context(), c.sql)
			require.NoError(t, err)
			sequences := freshHistorySequenceState(t, tx)
			require.ErrorContains(t, h.verifyFirstCustomerFreshReleaseEvidence(request, tx), "recorded fresh release evidence")
			require.Equal(t, sequences, freshHistorySequenceState(t, tx))
			require.NoError(t, tx.Rollback(t.Context()))
			require.Equal(t, before, freshHistoryRowsState(t, pool))
		})
	}
}
