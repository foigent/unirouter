//go:build integration

package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func seedFirstCustomerRuntimeShell(t *testing.T) (*pgxpool.Pool, *Handler, int64, int64) {
	t.Helper()
	return seedFirstCustomerRuntimeShellAtEpoch(t, 1)
}

func seedFirstCustomerRuntimeShellAtEpoch(t *testing.T, epoch int64) (*pgxpool.Pool, *Handler, int64, int64) {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
	// The supported reset advances the epoch without replacing the verifier.
	_, err := pool.Exec(t.Context(), `UPDATE users SET auth_epoch=$2 WHERE id=$1`, adminID, epoch)
	require.NoError(t, err)
	baseline, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	require.Equal(t, adminOrgID, baseline.Customer.OrganizationID)
	_, err = pool.Exec(t.Context(), `INSERT INTO audit_logs (
		org_id,user_id,action,resource,resource_id,detail,source,created_at
	) VALUES ($1,$2,'login','user',($2::bigint)::text,'{}'::jsonb,'admin_console',NOW())`, adminOrgID, adminID)
	require.NoError(t, err)
	// Preserve two real-shaped login records, not an artificial login count.
	_, err = pool.Exec(t.Context(), `UPDATE audit_logs SET source='admin_console',
		before_state='{}'::jsonb,after_state='{}'::jsonb,
		detail=(SELECT jsonb_build_object('username',username,
			'organization_role',organization_role,'compatibility_role',role) FROM users WHERE id=$1)
		WHERE user_id=$1 AND action='login'`, adminID)
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	handler := NewHandler(pool, "runtime-bootstrap-fixture-jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRuntimeStatusOptions(RuntimeStatusOptions{Environment: "development", SchemaMigrations: ledger})
	return pool, handler, adminID, adminOrgID
}

func TestFirstCustomerBootstrapAcceptsPostResetRuntimeShellIntegration(t *testing.T) {
	pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
	response := serveFirstCustomerBootstrapAtEpoch(t, handler, adminID, adminOrgID, 1, "platform_admin", "runtime-bootstrap-001", "Runtime Customer")
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var created firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	require.NotEqual(t, adminOrgID, created.OrganizationID)
	require.Zero(t, created.WalletBalance)

	var users, admins, partners, loginAudits, bootstrapAudits, wallets, projects int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM users WHERE id=$1 AND org_id=$2 AND organization_role='platform_admin'),
		(SELECT COUNT(*) FROM orgs WHERE id=$2 AND org_type='partner' AND parent_id IS NULL),
		(SELECT COUNT(*) FROM audit_logs WHERE user_id=$1 AND action='login'),
		(SELECT COUNT(*) FROM audit_logs WHERE action=$3),
		(SELECT COUNT(*) FROM wallets),
		(SELECT COUNT(*) FROM projects)`, adminID, adminOrgID, firstCustomerBootstrapAction).
		Scan(&users, &admins, &partners, &loginAudits, &bootstrapAudits, &wallets, &projects))
	require.EqualValues(t, 1, users)
	require.EqualValues(t, 1, admins)
	require.EqualValues(t, 1, partners)
	require.EqualValues(t, 2, loginAudits)
	require.EqualValues(t, 1, bootstrapAudits)
	require.EqualValues(t, 1, wallets)
	require.Zero(t, projects, "only the exact automatic empty platform shell is normalized")

	replay := serveFirstCustomerBootstrapAtEpoch(t, handler, adminID, adminOrgID, 1, "platform_admin", "runtime-bootstrap-001", "Runtime Customer")
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayed firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	require.True(t, replayed.Idempotent)
	require.Equal(t, created.OrganizationID, replayed.OrganizationID)

	baseline, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	require.Equal(t, created.OrganizationID, baseline.Customer.OrganizationID)
	require.Positive(t, baseline.ProjectID)
	repeated, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	require.Equal(t, baseline.ProjectID, repeated.ProjectID)
}

func runtimeBootstrapState(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'users',(SELECT jsonb_agg(jsonb_build_array(id,org_id,username,organization_role,role,status,auth_epoch) ORDER BY id) FROM users),
		'orgs',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM orgs o),
		'projects',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM projects p),
		'wallets',(SELECT jsonb_agg(to_jsonb(w) ORDER BY org_id) FROM wallets w),
		'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a),
		'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM internal_outbox_events e)
	)::text`).Scan(&state))
	return state
}

func TestFirstCustomerRuntimeCheckpointReadOnlyIntegration(t *testing.T) {
	pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
	before := runtimeBootstrapState(t, pool)
	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil)
	require.NoError(t, handler.verifyFirstCustomerRuntimeResetCheckpoint(request, tx, adminID, adminOrgID))
	require.NoError(t, tx.Commit(t.Context()))
	require.True(t, before == runtimeBootstrapState(t, pool), "read-only checkpoint must not normalize or create data")
}

func TestFirstCustomerRuntimeBootstrapRejectsDriftIntegration(t *testing.T) {
	for _, test := range []struct{ name, statement string }{
		{"nonzero balance", `UPDATE wallets SET balance_micros=1`},
		{"historical spend", `UPDATE wallets SET total_spent_micros=1`},
		{"project budget", `UPDATE projects SET budget_cents=1`},
		{"customized project", `UPDATE projects SET description='customer data'`},
		{"seed project is not runtime project", `UPDATE projects SET description='Default project created for PRD2 compatibility'`},
		{"project membership", `INSERT INTO project_members(project_id,user_id,role) SELECT p.id,u.id,'project_admin' FROM projects p JOIN users u ON p.org_id=u.org_id`},
		{"business data", `INSERT INTO channels(name,provider,base_url,status) VALUES ('must-retain','fixture','https://example.invalid','disabled')`},
		{"foreign root", `INSERT INTO orgs(name,status,org_type) VALUES ('second-root','active','partner')`},
		{"root type", `UPDATE orgs SET org_type='customer'`},
		{"root settings", `UPDATE orgs SET settings='{"configured":true}'::jsonb`},
		{"disabled admin", `UPDATE users SET status='disabled'`},
		{"current session revoked", `UPDATE admin_sessions SET revoked_at=NOW()`},
		{"non-login audit", `UPDATE audit_logs SET action='fixture.business-change'`},
		{"login project reference", `UPDATE audit_logs SET project_id=(SELECT id FROM projects)`},
		{"login content drift", `UPDATE audit_logs SET detail='{}'::jsonb`},
		{"login resource drift", `UPDATE audit_logs SET resource='project'`},
		{"unapproved setting", `INSERT INTO settings(key,value) VALUES ('fixture.setting','true'::jsonb)`},
		{"ledger gap", `DELETE FROM schema_migrations WHERE version='000010_prd2_model_governance'`},
		{"future ledger", `INSERT INTO schema_migrations(version) VALUES ('000092_future_contract')`},
		{"unknown table", `CREATE TABLE unexpected_business_state(id bigint PRIMARY KEY)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
			_, err := pool.Exec(t.Context(), test.statement)
			require.NoError(t, err)
			before := runtimeBootstrapState(t, pool)
			response := serveFirstCustomerBootstrapAtEpoch(t, handler, adminID, adminOrgID, 1, "platform_admin", "runtime-drift-001", "Must Not Exist")
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			require.True(t, before == runtimeBootstrapState(t, pool), "rejected predecessor must remain byte-for-byte unchanged")
		})
	}
}

func TestFirstCustomerRuntimeBootstrapRejectsUnadvancedEpochIntegration(t *testing.T) {
	pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShellAtEpoch(t, 0)
	before := runtimeBootstrapState(t, pool)
	response := serveFirstCustomerBootstrapAtEpoch(t, handler, adminID, adminOrgID, 0, "platform_admin", "runtime-epoch-001", "Must Not Exist")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.True(t, before == runtimeBootstrapState(t, pool))
}

func runtimeBootstrapLogin(t *testing.T, pool *pgxpool.Pool, handler *Handler) string {
	t.Helper()
	service, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	handler.SetOutbox(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"bootstrap-integration-fixture"}`))
	response := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.Login)).ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var login struct{ Token string }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &login))
	return login.Token
}

func TestFirstCustomerRuntimeBootstrapRetainsLoginEvidenceIntegration(t *testing.T) {
	pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
	token := runtimeBootstrapLogin(t, pool, handler)
	_, err := pool.Exec(t.Context(), `INSERT INTO admin_sessions(user_id,sid_hash,issued_at,expires_at,revoked_at)
		VALUES ($1,decode(repeat('ef',32),'hex'),NOW()-INTERVAL '3 days',NOW()-INTERVAL '1 day',NOW()-INTERVAL '2 days')`, adminID)
	require.NoError(t, err)
	var beforeEvidence, beforeVerifier string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a),
		'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM internal_outbox_events e),
		'sessions',(SELECT jsonb_agg(jsonb_build_array(id,user_id,issued_at,expires_at,revoked_at) ORDER BY id) FROM admin_sessions)
	)::text`).Scan(&beforeEvidence))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT password_hash FROM users WHERE id=$1`, adminID).Scan(&beforeVerifier))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", bytes.NewBufferString(`{"organization_name":"Authenticated Runtime Customer"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(middleware.ClientRequestIDHeader, "runtime-authenticated-001")
	response := httptest.NewRecorder()
	middleware.OperationIdentity(middleware.AdminAuth(pool, "runtime-bootstrap-fixture-jwt")(http.HandlerFunc(handler.BootstrapFirstCustomer))).ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var afterEvidence, afterVerifier string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a WHERE action='login'),
		'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM internal_outbox_events e),
		'sessions',(SELECT jsonb_agg(jsonb_build_array(id,user_id,issued_at,expires_at,revoked_at) ORDER BY id) FROM admin_sessions)
	)::text`).Scan(&afterEvidence))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT password_hash FROM users WHERE id=$1 AND org_id=$2`, adminID, adminOrgID).Scan(&afterVerifier))
	require.True(t, beforeEvidence == afterEvidence, "bootstrap must preserve login/outbox/session evidence exactly")
	require.True(t, beforeVerifier == afterVerifier, "bootstrap must not replace the verifier")
}

func TestFirstCustomerRuntimeBootstrapRejectsOutboxDriftIntegration(t *testing.T) {
	for _, statement := range []string{
		`UPDATE internal_outbox_events SET payload=payload || jsonb_build_object('project_id',(SELECT id FROM projects))`,
		`UPDATE internal_outbox_events SET payload=payload || '{"org_id":999}'::jsonb`,
		`UPDATE internal_outbox_events SET payload=payload || '{"detail":{}}'::jsonb`,
		`UPDATE internal_outbox_events SET dead_lettered_at=NOW()`,
	} {
		pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
		runtimeBootstrapLogin(t, pool, handler)
		_, err := pool.Exec(t.Context(), statement)
		require.NoError(t, err)
		before := runtimeBootstrapState(t, pool)
		response := serveFirstCustomerBootstrapAtEpoch(t, handler, adminID, adminOrgID, 1, "platform_admin", "runtime-outbox-drift", "Must Not Exist")
		require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		require.True(t, before == runtimeBootstrapState(t, pool), "outbox rejection must not alter retained evidence")
	}
}

func runtimeBootstrapAuthenticatedRequest(handler *Handler, pool *pgxpool.Pool, token, body, clientID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(middleware.ClientRequestIDHeader, clientID)
	response := httptest.NewRecorder()
	middleware.OperationIdentity(middleware.AdminAuth(pool, "runtime-bootstrap-fixture-jwt")(http.HandlerFunc(handler.BootstrapFirstCustomer))).ServeHTTP(response, request)
	return response
}

func TestFirstCustomerRuntimeBootstrapDryRunIntegration(t *testing.T) {
	pool, handler, _, _ := seedFirstCustomerRuntimeShell(t)
	token := runtimeBootstrapLogin(t, pool, handler)
	before := runtimeBootstrapState(t, pool)
	var sequenceBefore, sequenceAfter int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT last_value FROM orgs_id_seq`).Scan(&sequenceBefore))
	preview := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
		`{"organization_name":"Preview Customer","dry_run":true}`, "runtime-preview-001")
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var result struct {
		DryRun      bool `json:"dry_run"`
		Ready       bool
		Checkpoint  string
		WouldRemove map[string]int64 `json:"would_remove"`
	}
	// The underscore spelling is part of the public contract.
	var members map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &members))
	require.JSONEq(t, `true`, string(members["dry_run"]))
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &result))
	require.True(t, result.DryRun)
	require.True(t, result.Ready)
	require.Equal(t, "runtime_reset", result.Checkpoint)
	require.Positive(t, result.WouldRemove["project_id"])
	require.Positive(t, result.WouldRemove["zero_wallet_org_id"])
	require.True(t, before == runtimeBootstrapState(t, pool))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT last_value FROM orgs_id_seq`).Scan(&sequenceAfter))
	require.Equal(t, sequenceBefore, sequenceAfter, "dry run must not allocate a customer identity")

	// The preview is informational, not a token that can bypass fresh validation.
	_, err := pool.Exec(t.Context(), `UPDATE wallets SET balance_micros=1`)
	require.NoError(t, err)
	before = runtimeBootstrapState(t, pool)
	apply := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
		`{"organization_name":"Preview Customer"}`, "runtime-preview-001")
	require.Equal(t, http.StatusConflict, apply.Code)
	require.True(t, before == runtimeBootstrapState(t, pool))
}

func TestFirstCustomerRuntimeBootstrapRejectsInvalidDryRunIntegration(t *testing.T) {
	pool, handler, _, _ := seedFirstCustomerRuntimeShell(t)
	token := runtimeBootstrapLogin(t, pool, handler)
	before := runtimeBootstrapState(t, pool)
	for _, value := range []string{"null", `"true"`, "1"} {
		response := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
			`{"organization_name":"Must Not Exist","dry_run":`+value+`}`, "runtime-invalid-preview")
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.True(t, before == runtimeBootstrapState(t, pool))
	}
	duplicate := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
		`{"organization_name":"Must Not Exist","dry_run":true,"dry_run":false}`, "runtime-duplicate-preview")
	require.Equal(t, http.StatusBadRequest, duplicate.Code)
	require.True(t, before == runtimeBootstrapState(t, pool))
}

func TestFirstCustomerRuntimeBootstrapIsNotFreshSeedFallbackIntegration(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	require.NoError(t, migrate.Run(t.Context(), pool, testutil.MigrationsDir(t)))
	_, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	var adminID, adminOrgID int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT id,org_id FROM users WHERE username='admin'`).Scan(&adminID, &adminOrgID))
	// Even a rotated-looking epoch and runtime-shaped project cannot turn the
	// fresh seed into reset scope or bypass fresh password/Release evidence.
	_, err = pool.Exec(t.Context(), `UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=$1`, adminID)
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	handler := NewHandler(pool, "runtime-bootstrap-fixture-jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetRuntimeStatusOptions(RuntimeStatusOptions{Environment: "production", SchemaMigrations: ledger})
	token := testutil.MustAdminSessionJWT(t, t.Context(), pool, adminID, adminOrgID, "admin", "runtime-bootstrap-fixture-jwt")
	for _, description := range []string{"Default project created for PRD2 compatibility", "Default project for this UniRouter deployment"} {
		_, err = pool.Exec(t.Context(), `UPDATE projects SET description=$1`, description)
		require.NoError(t, err)
		before := runtimeBootstrapState(t, pool)
		response := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
			`{"organization_name":"Must Not Exist"}`, "runtime-fresh-fallback")
		require.Equal(t, http.StatusConflict, response.Code)
		require.True(t, before == runtimeBootstrapState(t, pool))
	}
}

func TestFirstCustomerRuntimeBootstrapConcurrentBaselineIntegration(t *testing.T) {
	pool, handler, _, _ := seedFirstCustomerRuntimeShell(t)
	token := runtimeBootstrapLogin(t, pool, handler)
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	baselineDone := make(chan error, 1)
	for range 2 {
		go func() {
			<-start
			responses <- runtimeBootstrapAuthenticatedRequest(handler, pool, token,
				`{"organization_name":"Concurrent Customer"}`, "runtime-concurrent-001")
		}()
	}
	go func() {
		<-start
		_, err := customercontext.EnsureBaseline(t.Context(), pool)
		baselineDone <- err
	}()
	close(start)
	for range 2 {
		response := <-responses
		require.Contains(t, []int{http.StatusCreated, http.StatusOK, http.StatusConflict}, response.Code, response.Body.String())
	}
	// An overlapping restart can lose its customer-context snapshot; it may
	// reject, but must not recreate the old partner shell or duplicate a customer.
	if baselineErr := <-baselineDone; baselineErr != nil {
		var postgresErr *pgconn.PgError
		serializationConflict := errors.As(baselineErr, &postgresErr) && postgresErr.Code == "40001"
		require.True(t, serializationConflict || strings.Contains(baselineErr.Error(), "customer context changed"), "unexpected baseline failure: %v", baselineErr)
	}
	replay := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
		`{"organization_name":"Concurrent Customer"}`, "runtime-concurrent-001")
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, replay.Code, replay.Body.String())
	baseline, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	var customers, projects, wallets, receipts int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT COUNT(*) FROM orgs WHERE org_type='customer'),
		(SELECT COUNT(*) FROM projects), (SELECT COUNT(*) FROM wallets),
		(SELECT COUNT(*) FROM audit_logs WHERE action=$1)`, firstCustomerBootstrapAction).
		Scan(&customers, &projects, &wallets, &receipts))
	require.EqualValues(t, 1, customers)
	require.EqualValues(t, 1, projects)
	require.EqualValues(t, 1, wallets)
	require.EqualValues(t, 1, receipts)
	require.Positive(t, baseline.ProjectID)
}
