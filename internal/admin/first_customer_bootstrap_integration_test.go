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
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestFirstCustomerBootstrapCreatesOnlyOrganizationZeroWalletAndAudit(t *testing.T) {
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

func TestFirstCustomerBootstrapFailsClosedOnDirtyBaselineAndRole(t *testing.T) {
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

func TestFirstCustomerBootstrapFailsClosedOnSchemaDrift(t *testing.T) {
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
		_, err := pool.Exec(context.Background(), `INSERT INTO schema_migrations (version) VALUES ('000059_future_contract')`)
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
	t.Helper()
	payload, err := json.Marshal(firstCustomerBootstrapRequest{OrganizationName: organizationName})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", bytes.NewReader(payload))
	request.Header.Set(middleware.ClientRequestIDHeader, clientRequestID)
	ctx := context.WithValue(request.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, role)
	ctx = context.WithValue(ctx, middleware.CtxAuthEpoch, int64(0))
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
