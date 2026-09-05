//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestFirstCustomerBootstrapCreatesOnlyOrganizationZeroWalletAndAuditIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
	handler := NewHandler(pool, "bootstrap-integration-jwt", []byte("0123456789abcdef01234567"), nil)
	assertFirstCustomerCheckpoint(t, pool, adminID, adminOrgID)

	response := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-client-001", "First Customer")
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var created firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	require.False(t, created.Idempotent)
	require.Equal(t, "First Customer", created.OrganizationName)
	require.Zero(t, created.WalletBalance)

	ctx := context.Background()
	var orgCount, walletCount, userCount, projectCount, keyCount, auditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM orgs`).Scan(&orgCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE org_id=$1 AND balance_micros=0 AND granted_balance_micros=0 AND frozen_micros=0 AND total_spent_micros=0`, created.OrganizationID).Scan(&walletCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects`).Scan(&projectCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&keyCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action=$1 AND client_request_id='bootstrap-client-001'`, firstCustomerBootstrapAction).Scan(&auditCount))
	require.Equal(t, 2, orgCount)
	require.Equal(t, 1, walletCount)
	require.Equal(t, 1, userCount)
	require.Zero(t, projectCount)
	require.Zero(t, keyCount)
	require.Equal(t, 1, auditCount)

	replay := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-client-001", "First Customer")
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayed firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	require.True(t, replayed.Idempotent)
	require.Equal(t, created.OrganizationID, replayed.OrganizationID)

	mismatch := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-client-001", "Different Customer")
	require.Equal(t, http.StatusConflict, mismatch.Code)
	second := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-client-002", "Second Customer")
	require.Equal(t, http.StatusConflict, second.Code)
}

func TestFirstCustomerBootstrapAcceptsExactFreshMigrationSeedAfterPasswordRotationIntegration(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	migrationsDir := testutil.MigrationsDir(t)
	require.NoError(t, testutil.InstallExactTestArtifact(t.Context(), pool, migrationsDir))
	baseline, err := customercontext.EnsureBaseline(t.Context(), pool)
	require.NoError(t, err)
	require.EqualValues(t, 1, baseline.Customer.OrganizationID)

	var adminID, adminOrgID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT id,org_id FROM users
		 WHERE public.normalize_local_login_id(username)='admin'`).Scan(&adminID, &adminOrgID))
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("fresh-bootstrap-password"), bcrypt.MinCost)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		WITH changed AS (
			UPDATE users SET password_hash=$1,auth_epoch=auth_epoch+1,updated_at=NOW()
			 WHERE id=$2 AND org_id=$3 RETURNING id,org_id
		)
		INSERT INTO audit_logs (
			org_id,user_id,action,resource,resource_id,detail,source,
			before_state,after_state,created_at
		)
		SELECT org_id,id,'user.password_reset','user',id::text,
		       '{"reason":"recovery_cli","sessions_revoked":"all"}'::jsonb,'system',
		       '{}'::jsonb,'{"password_reset":true,"sessions_revoked":"all"}'::jsonb,NOW()
		  FROM changed`, string(passwordHash), adminID, adminOrgID)
	require.NoError(t, err)
	expectedLedger, err := schemaauthority.ExpectedLedger(migrationsDir)
	require.NoError(t, err)

	const jwtSecret = "fresh-bootstrap-jwt"
	handler := NewHandler(pool, jwtSecret, []byte("0123456789abcdef01234567"), nil)
	handler.SetRuntimeStatusOptions(RuntimeStatusOptions{
		Environment:      "production",
		SchemaMigrations: expectedLedger,
	})
	outboxService, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	handler.SetOutbox(outboxService)
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{"username":"admin","password":"fresh-bootstrap-password"}`))
	loginResponse := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.Login)).ServeHTTP(loginResponse, loginRequest)
	require.Equal(t, http.StatusOK, loginResponse.Code, loginResponse.Body.String())
	var login struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(loginResponse.Body.Bytes(), &login))
	require.NotEmpty(t, login.Token)

	protected := middleware.OperationIdentity(middleware.AdminAuth(pool, jwtSecret)(http.HandlerFunc(handler.BootstrapFirstCustomer)))
	serve := func(clientRequestID string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer",
			bytes.NewBufferString(`{"organization_name":"Fresh Customer"}`))
		request.Header.Set("Authorization", "Bearer "+login.Token)
		request.Header.Set(middleware.ClientRequestIDHeader, clientRequestID)
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		return response
	}

	missingRelease := serve("fresh-bootstrap-client-001")
	require.Equal(t, http.StatusConflict, missingRelease.Code, missingRelease.Body.String())
	var unchangedOrgs, unchangedWallets, unchangedProjects int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT COUNT(*) FROM orgs),
		(SELECT COUNT(*) FROM wallets),
		(SELECT COUNT(*) FROM projects)`).Scan(&unchangedOrgs, &unchangedWallets, &unchangedProjects))
	require.EqualValues(t, 1, unchangedOrgs)
	require.EqualValues(t, 1, unchangedWallets)
	require.EqualValues(t, 1, unchangedProjects)

	seedFreshBootstrapReleaseEvidence(t, pool)
	for _, corruption := range []struct {
		name, table, trigger, statement, failedCheck string
	}{
		{
			name:    "same-count support provenance drift",
			table:   "model_operation_execution_support_bindings",
			trigger: "model_operation_execution_support_bindings_immutable",
			statement: `UPDATE model_operation_execution_support_bindings
			 SET introduced_migration='000999_unapproved_fixture'
			 WHERE protocol_profile_id='unirouter.openai.responses.text.v2'`,
			failedCheck: "check fresh compiled execution support: got 4 want 5",
		},
		{
			name:    "extra support tuple",
			table:   "model_operation_execution_support_bindings",
			trigger: "model_operation_execution_support_bindings_immutable",
			statement: `INSERT INTO model_operation_execution_support_bindings (
			 operation_id,public_path,protocol_profile_id,protocol_profile_version,
			 executor_contract_version,introduced_migration)
			 VALUES ('fixture.future.v1','/v1/future','fixture.future','1.0.0',
			 'fixture.future-executor.v1','000999_unapproved_fixture')`,
			failedCheck: "check fresh compiled execution support count: got 6 want 5",
		},
		{
			name:    "missing public extension",
			table:   "model_operation_public_execution_contract_extensions",
			trigger: "model_operation_public_execution_contract_extensions_immutable",
			statement: `DELETE FROM model_operation_public_execution_contract_extensions
			 WHERE protocol_profile_id='unirouter.anthropic.messages.text-tools.v3'`,
			failedCheck: "check fresh public execution extension: got 4 want 5",
		},
		{
			name:    "same-count capability probe contract drift",
			table:   "model_capability_probe_contract_bindings",
			trigger: "model_capability_probe_contract_bindings_immutable",
			statement: `UPDATE model_capability_probe_contract_bindings
			 SET expected_upstream_model='deepseek-v4-flash-drift'
			 WHERE probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'`,
			failedCheck: "check fresh capability-contract static projection",
		},
		{
			name:    "extra capability probe contract binding",
			table:   "model_capability_probe_contract_bindings",
			trigger: "model_capability_probe_contract_bindings_immutable",
			statement: `INSERT INTO model_capability_probe_contract_bindings (
			 operation_id,capability_id,execution_profile_id,execution_profile_version,
			 protocol_profile_id,protocol_profile_version,probe_contract_id,
			 probe_contract_version,expected_provider,expected_upstream_model,
			 public_path,introduced_migration)
			 VALUES ('fixture.future.v1','streaming','fixture.future.api','1.0.0',
			 'fixture.future.protocol','1.0.0','fixture.future.probe','1.0.0',
			 'fixture','fixture-future','/v1/future',
			 '000088_model_capability_execution_qualification')`,
			failedCheck: "check fresh capability-contract static projection",
		},
		{
			name:    "missing current capability probe contract",
			table:   "model_capability_current_probe_contracts",
			trigger: "model_capability_current_probe_contracts_immutable",
			statement: `DELETE FROM model_capability_current_probe_contracts
			 WHERE probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'`,
			failedCheck: "check fresh capability-contract static projection",
		},
		{
			name:    "missing tools current capability probe contract",
			table:   "model_capability_current_probe_contracts",
			trigger: "model_capability_current_probe_contracts_immutable",
			statement: `DELETE FROM model_capability_current_probe_contracts
			 WHERE capability_id='tools'
			   AND protocol_profile_id='protocol.deepseek-openai-chat.v4'`,
			failedCheck: "check fresh capability-contract static projection",
		},
	} {
		t.Run(corruption.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(t.Context()) }()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil)
			require.NoError(t, handler.verifyFirstCustomerFreshMigrationCheckpoint(request, tx, adminID, adminOrgID))
			// Fault injection is transaction-local in an isolated database. Rollback
			// restores both the immutable trigger and the exact migration-owned rows.
			_, err = tx.Exec(t.Context(), "ALTER TABLE "+corruption.table+" DISABLE TRIGGER "+corruption.trigger)
			require.NoError(t, err)
			if corruption.table == "model_capability_probe_contract_bindings" {
				// M114 adds a second append-only writer guard. Disable it only
				// inside this rolled-back corruption fixture, not the real owner.
				_, err = tx.Exec(t.Context(), "ALTER TABLE model_capability_probe_contract_bindings DISABLE TRIGGER capability_binding_writer_epoch")
				require.NoError(t, err)
			}
			_, err = tx.Exec(t.Context(), corruption.statement)
			require.NoError(t, err)
			require.ErrorContains(t, handler.verifyFirstCustomerFreshMigrationCheckpoint(request, tx, adminID, adminOrgID), corruption.failedCheck)
			require.NoError(t, tx.Rollback(t.Context()))
		})
	}
	response := serve("fresh-bootstrap-client-001")
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var created firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	require.Equal(t, "Fresh Customer", created.OrganizationName)

	var orgs, wallets, projects, members, bootstrapAudits, pendingLoginAudits int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT
		  (SELECT COUNT(*) FROM orgs),
		  (SELECT COUNT(*) FROM wallets),
		  (SELECT COUNT(*) FROM projects),
		  (SELECT COUNT(*) FROM project_members),
		  (SELECT COUNT(*) FROM audit_logs WHERE action=$1),
		  (SELECT COUNT(*) FROM internal_outbox_events
		    WHERE event_type='evidence.audit.v1' AND payload->>'action'='login')`, firstCustomerBootstrapAction).
		Scan(&orgs, &wallets, &projects, &members, &bootstrapAudits, &pendingLoginAudits))
	require.EqualValues(t, 2, orgs)
	require.EqualValues(t, 1, wallets, "the platform seed wallet must be removed before the customer wallet is created")
	require.Zero(t, projects, "the platform seed project must not consume a customer License seat")
	require.Zero(t, members)
	require.EqualValues(t, 1, bootstrapAudits)
	require.EqualValues(t, 1, pendingLoginAudits, "bootstrap must preserve the durable login audit evidence")
	replay := serve("fresh-bootstrap-client-001")
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	var replayed firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	require.True(t, replayed.Idempotent)
	require.Equal(t, created.OrganizationID, replayed.OrganizationID)

	_, err = pool.Exec(t.Context(), `INSERT INTO commercial_license_entitlements (
		singleton,license_id,deployment_id,key_id,expires_at,features,active_projects
	) VALUES (TRUE,'fresh-bootstrap-license','fresh-bootstrap-deployment',
		'fresh-bootstrap-license-key',NOW()+INTERVAL '1 year','[]'::jsonb,1)`)
	require.NoError(t, err)
	var reviewedProjectID int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO projects (
		org_id,name,slug,description,status,budget_cents,budget_period
	) VALUES ($1,'Reviewed Customer Project','default','Reviewed during isolated first install',
		'active',NULL,'monthly') RETURNING id`, created.OrganizationID).Scan(&reviewedProjectID))
	type baselineResult struct {
		baseline customercontext.Baseline
		err      error
	}
	restarts := make(chan baselineResult, 2)
	for range 2 {
		go func() {
			baseline, baselineErr := customercontext.EnsureBaseline(t.Context(), pool)
			restarts <- baselineResult{baseline: baseline, err: baselineErr}
		}()
	}
	for range 2 {
		restarted := <-restarts
		require.NoError(t, restarted.err)
		require.Equal(t, reviewedProjectID, restarted.baseline.ProjectID)
	}
	var activeProjects int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM projects WHERE status='active'`).Scan(&activeProjects))
	require.EqualValues(t, 1, activeProjects, "canonical restart must not consume a second active-project License seat")

	_, err = pool.Exec(t.Context(), `UPDATE wallets SET
		balance_micros=17,granted_balance_micros=23,total_spent_micros=5
		WHERE org_id=$1`, created.OrganizationID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE orgs SET name='Renamed Customer',status='disabled'
		WHERE id=$1`, created.OrganizationID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE audit_logs SET created_at=NOW()-INTERVAL '100 days'
		WHERE action=$1 AND client_request_id='fresh-bootstrap-client-001'`, firstCustomerBootstrapAction)
	require.NoError(t, err)
	require.NoError(t, handler.CleanupOldData(t.Context(), 30))
	durableReplay := serve("fresh-bootstrap-client-001")
	require.Equal(t, http.StatusOK, durableReplay.Code, durableReplay.Body.String())
	var durableResult firstCustomerBootstrapResult
	require.NoError(t, json.Unmarshal(durableReplay.Body.Bytes(), &durableResult))
	require.Equal(t, created.OrganizationID, durableResult.OrganizationID)
	require.Equal(t, "Fresh Customer", durableResult.OrganizationName)
	require.Zero(t, durableResult.WalletBalance)
	require.True(t, durableResult.Idempotent)
}

func seedFreshBootstrapReleaseEvidence(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	const (
		sequence = int64(1)
		version  = "v5-fresh-bootstrap-fixture"
		commit   = "0123456789abcdef0123456789abcdef01234567"
		sha256   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		keyID    = "fresh-bootstrap-fixture-key"
	)
	manifest := `{"contract_version":"unirouter.upgrade-bundle.v1","release_sequence":1,"version":"v5-fresh-bootstrap-fixture","commit":"0123456789abcdef0123456789abcdef01234567","target_schema":"` + schemaauthority.LatestSupportedMigration + `"}`
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(t.Context(), `
		INSERT INTO release_history (
			release_sequence,version,commit,package_sha256,trusted_key_id,
			target_schema,manifest,recorded_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,'fresh-bootstrap-integration')`,
		sequence, version, commit, sha256, keyID,
		schemaauthority.LatestSupportedMigration, manifest)
	require.NoError(t, err)
	result, err := tx.Exec(t.Context(), `UPDATE release_state SET
			release_sequence=$1,version=$2,commit=$3,package_sha256=$4,
			trusted_key_id=$5,target_schema=$6,manifest=$7::jsonb,updated_at=NOW()
		WHERE singleton`, sequence, version, commit, sha256, keyID,
		schemaauthority.LatestSupportedMigration, manifest)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.RowsAffected())
	require.NoError(t, tx.Commit(t.Context()))
}

func TestFirstCustomerBootstrapRejectsCommittedExactSessionLogoutIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `DELETE FROM admin_sessions WHERE user_id=$1`, adminID)
	require.NoError(t, err)

	const jwtSecret = "bootstrap-session-reauthorization-secret"
	token := testutil.MustAdminSessionJWT(t, ctx, pool, adminID, adminOrgID, "platform_admin", jwtSecret)
	beginReached := make(chan struct{})
	allowBegin := make(chan struct{})
	interleavingDB := &apiKeyInterleavingDB{pool: pool, beginReached: beginReached, allowBegin: allowBegin}
	mutationHandler := NewHandler(interleavingDB, jwtSecret, []byte("0123456789abcdef01234567"), nil)
	liveHandler := NewHandler(pool, jwtSecret, []byte("0123456789abcdef01234567"), nil)
	protected := middleware.OperationIdentity(middleware.AdminAuth(pool, jwtSecret)(http.HandlerFunc(mutationHandler.BootstrapFirstCustomer)))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer",
		bytes.NewBufferString(`{"organization_name":"Must Not Commit"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(middleware.ClientRequestIDHeader, "bootstrap-session-revoked")
	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		protected.ServeHTTP(response, request)
		responseCh <- response
	}()

	select {
	case <-beginReached:
	case <-time.After(5 * time.Second):
		t.Fatal("first-customer bootstrap did not reach its committing transaction after authenticated admission")
	}
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+token)
	logoutResponse := httptest.NewRecorder()
	liveHandler.Logout(logoutResponse, logoutRequest)
	require.Equal(t, http.StatusOK, logoutResponse.Code, logoutResponse.Body.String())
	close(allowBegin)

	select {
	case response := <-responseCh:
		require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		require.JSONEq(t, `{"error":"First-customer bootstrap admin preflight failed"}`, response.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("first-customer bootstrap did not finish after logout committed")
	}
	var orgCount, walletCount, bootstrapAuditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM orgs`).Scan(&orgCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action=$1`, firstCustomerBootstrapAction,
	).Scan(&bootstrapAuditCount))
	require.Equal(t, 1, orgCount)
	require.Zero(t, walletCount)
	require.Zero(t, bootstrapAuditCount)
}

func TestFirstCustomerBootstrapFailsClosedOnDirtyBaselineAndRoleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
	handler := NewHandler(pool, "bootstrap-integration-jwt", []byte("0123456789abcdef01234567"), nil)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO channels (name, provider, base_url, status)
		VALUES ('dirty', 'fixture', 'https://example.invalid', 'disabled')`)
	require.NoError(t, err)

	dirty := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-dirty-001", "First Customer")
	require.Equal(t, http.StatusConflict, dirty.Code)
	nonPlatform := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "org_admin", "bootstrap-role-001", "First Customer")
	require.Equal(t, http.StatusForbidden, nonPlatform.Code)
}

func TestFirstCustomerBootstrapFailsClosedOnSchemaDriftIntegration(t *testing.T) {
	t.Run("unknown public table", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
		handler := NewHandler(pool, "bootstrap-integration-jwt", []byte("0123456789abcdef01234567"), nil)
		_, err := pool.Exec(context.Background(), `CREATE TABLE future_product_state (id BIGINT PRIMARY KEY)`)
		require.NoError(t, err)

		response := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-schema-001", "First Customer")
		require.Equal(t, http.StatusConflict, response.Code)
	})

	t.Run("later migration", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
		handler := NewHandler(pool, "bootstrap-integration-jwt", []byte("0123456789abcdef01234567"), nil)
		_, err := pool.Exec(context.Background(), `INSERT INTO schema_migrations (version) VALUES ('999999_future_contract')`)
		require.NoError(t, err)

		response := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-schema-002", "First Customer")
		require.Equal(t, http.StatusConflict, response.Code)
	})

	t.Run("previous migration as max", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		adminID, adminOrgID := seedFirstCustomerBootstrapAdmin(t, pool)
		handler := NewHandler(pool, "bootstrap-integration-jwt", []byte("0123456789abcdef01234567"), nil)
		_, err := pool.Exec(context.Background(), `DELETE FROM schema_migrations WHERE version=$1`, schemaauthority.LatestSupportedMigration)
		require.NoError(t, err)

		response := serveFirstCustomerBootstrap(t, handler, adminID, adminOrgID, "platform_admin", "bootstrap-schema-003", "First Customer")
		require.Equal(t, http.StatusConflict, response.Code)
	})
}

func seedFirstCustomerBootstrapAdmin(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("bootstrap-integration-fixture"), bcrypt.MinCost)
	require.NoError(t, err)
	retainedSettingKeys := append(resetplan.ProtectedSettingKeys(), resetplan.PlatformBaselineSettingKeys()...)
	_, err = pool.Exec(ctx, `DELETE FROM settings WHERE NOT (key = ANY($1::text[]))`, retainedSettingKeys)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE settings SET value='[]'::jsonb WHERE key='sensitive_words'`)
	require.NoError(t, err)
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('Bootstrap Admin Shell', 'active', 'partner') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, organization_role, role, status, auth_provider)
		VALUES ($1, 'ADMIN', $2, 'platform_admin', 'platform_admin', 'active', 'local') RETURNING id`,
		orgID, string(passwordHash)).Scan(&userID))
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
		VALUES ($1, decode(repeat('cd', 32), 'hex'), NOW(), NOW() + INTERVAL '1 hour')`, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, created_at)
		VALUES ($1, $2, 'login', 'user', ($2::bigint)::text, '{}'::jsonb, $3)`, orgID, userID, time.Now().UTC())
	require.NoError(t, err)
	return userID, orgID
}

func serveFirstCustomerBootstrap(t *testing.T, handler *Handler, userID, orgID int64, role, clientRequestID, organizationName string) *httptest.ResponseRecorder {
	return serveFirstCustomerBootstrapAtEpoch(t, handler, userID, orgID, 0, role, clientRequestID, organizationName)
}

func serveFirstCustomerBootstrapAtEpoch(t *testing.T, handler *Handler, userID, orgID, authEpoch int64, role, clientRequestID, organizationName string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(firstCustomerBootstrapRequest{OrganizationName: organizationName})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", bytes.NewReader(payload))
	request.Header.Set(middleware.ClientRequestIDHeader, clientRequestID)
	ctx := context.WithValue(request.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, role)
	ctx = context.WithValue(ctx, middleware.CtxAuthEpoch, authEpoch)
	ctx = context.WithValue(ctx, middleware.CtxAdminSessionHash, bytes.Repeat([]byte{0xcd}, 32))
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.BootstrapFirstCustomer)).ServeHTTP(recorder, request)
	return recorder
}

func assertFirstCustomerCheckpoint(t *testing.T, pool *pgxpool.Pool, userID, orgID int64) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil)
	ctx := context.WithValue(request.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, "platform_admin")
	request = request.WithContext(ctx)
	require.NoError(t, verifyFirstCustomerEmptyCheckpoint(request, tx, userID, orgID))
}
