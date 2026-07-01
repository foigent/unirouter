//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

const prd2E2EJWTSecret = "test-secret"

var prd2E2EEncryptionKey = []byte("0123456789abcdef01234567")

func TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var upstreamModel, upstreamRequestID, upstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequestID = r.Header.Get("X-Request-ID")
		upstreamAuth = r.Header.Get("Authorization")
		var payload struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		upstreamModel = payload.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-prd2-e2e","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer upstream.Close()

	orgID, userID, projectID := seedPRD2P0OrgProjectUser(t, pool, "developer")
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)
	seedPRD2P0ModelGovernance(t, pool, orgID, projectID)
	seedPRD2P0Channel(t, pool, upstream.URL, "gpt-4o")

	h := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	createBody := []byte(`{"org_id":` + jsonNumber(orgID) + `,"project_id":` + jsonNumber(projectID) + `,"name":"developer-project-key","allowed_models":"company-chat","rpm_limit":60,"qps_limit":10}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq = createReq.WithContext(projectActorContext(createReq.Context(), orgID, userID, auth.RoleDeveloper, "batch9-create-key"))
	createRec := httptest.NewRecorder()

	h.CreateAPIKey(createRec, createReq)

	require.Equal(t, http.StatusCreated, createRec.Code, "body: %s", createRec.Body.String())
	var createResp struct {
		ID        int64  `json:"id"`
		Key       string `json:"key"`
		ProjectID int64  `json:"project_id"`
	}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &createResp))
	require.NotEmpty(t, createResp.Key)
	require.Equal(t, projectID, createResp.ProjectID)

	relay := gateway.NewRelay(pool, nil, gateway.NewRouter(pool), billing.NewBilling(pool, nil), filter.NewDFAFilter(nil), prd2E2EEncryptionKey)
	chatHandler := middleware.GatewayAuth(pool, prd2E2EJWTSecret)(newGatewayChatHandler(pool, relay))

	requestID := "batch9-p0-e2e-success"
	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"stream":false}`)))
	chatReq.Header.Set("Authorization", "Bearer "+createResp.Key)
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("X-Request-ID", requestID)
	chatRec := httptest.NewRecorder()

	chatHandler.ServeHTTP(chatRec, chatReq)

	require.Equal(t, http.StatusOK, chatRec.Code, "body: %s", chatRec.Body.String())
	require.Equal(t, "gpt-4o", upstreamModel)
	require.Equal(t, requestID, upstreamRequestID)
	require.Equal(t, "Bearer upstream-test-key", upstreamAuth)
	require.Contains(t, chatRec.Body.String(), `"_uniroute"`)

	var logProjectID, logKeyID int64
	var model, requestedModel, usageSource string
	var statusCode, totalTokens int
	err := pool.QueryRow(ctx,
		`SELECT project_id, api_key_id, model, requested_model, status_code, usage_source, total_tokens
		 FROM call_logs WHERE request_id = $1`,
		requestID,
	).Scan(&logProjectID, &logKeyID, &model, &requestedModel, &statusCode, &usageSource, &totalTokens)
	require.NoError(t, err)
	require.Equal(t, projectID, logProjectID)
	require.Equal(t, createResp.ID, logKeyID)
	require.Equal(t, "gpt-4o", model)
	require.Equal(t, "company-chat", requestedModel)
	require.Equal(t, http.StatusOK, statusCode)
	require.Equal(t, "upstream", usageSource)
	require.Equal(t, 15, totalTokens)

	var idemStatus string
	err = pool.QueryRow(ctx,
		`SELECT status FROM freeze_idempotency WHERE request_id = $1`,
		requestID,
	).Scan(&idemStatus)
	require.NoError(t, err)
	require.Equal(t, "settled", idemStatus)

	var auditProjectID, auditUserID int64
	var auditSource, auditName string
	err = pool.QueryRow(ctx,
		`SELECT project_id, user_id, source, after_state->>'name'
		 FROM audit_logs WHERE action = 'apikey.create' AND resource_id = $1`,
		jsonNumber(createResp.ID),
	).Scan(&auditProjectID, &auditUserID, &auditSource, &auditName)
	require.NoError(t, err)
	require.Equal(t, projectID, auditProjectID)
	require.Equal(t, userID, auditUserID)
	require.Equal(t, "developer_console", auditSource)
	require.Equal(t, "developer-project-key", auditName)

	assertGatewayModelDenied(t, chatHandler, createResp.Key, "batch9-p0-e2e-denied", "gpt-4o-mini", "project_model_not_allowed")
	assertGatewayModelDenied(t, chatHandler, createResp.Key, "batch9-p0-e2e-retired", "legacy-model", "model_retired")
}

func TestPRD2P0RouteRBACNegativeE2E_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)

	orgID, developerID, _ := seedPRD2P0OrgProjectUser(t, pool, "developer")
	_, billingAdminID, _ := seedPRD2P0UserOnly(t, pool, orgID, auth.RoleBillingAdmin)
	_, projectAdminID, ownedProjectID := seedPRD2P0UserOnly(t, pool, orgID, auth.RoleProjectAdmin)
	otherProjectID := seedProject(t, pool, orgID, "other-project")
	_, err := pool.Exec(context.Background(),
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_admin')`,
		ownedProjectID, projectAdminID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO call_logs (request_id, org_id, project_id, model, provider, channel, status_code)
		 VALUES ('other-project-log', $1, $2, 'gpt-4o', 'mock', 'mock-channel', 200)`,
		orgID, otherProjectID,
	)
	require.NoError(t, err)

	developerToken := mustJWT(t, developerID, orgID, auth.RoleDeveloper)
	billingToken := mustJWT(t, billingAdminID, orgID, auth.RoleBillingAdmin)
	projectAdminToken := mustJWT(t, projectAdminID, orgID, auth.RoleProjectAdmin)

	platformOnly := middleware.AdminAuth(prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
	)
	require.Equal(t, http.StatusForbidden, serveBearer(platformOnly, http.MethodGet, "/api/v1/channels", developerToken).Code)
	require.Equal(t, http.StatusForbidden, serveBearer(platformOnly, http.MethodPost, "/api/v1/channels", billingToken).Code)

	auditRoute := middleware.AdminAuth(prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
	)
	require.Equal(t, http.StatusForbidden, serveBearer(auditRoute, http.MethodGet, "/api/v1/audit-logs", developerToken).Code)

	h := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	usageRoute := middleware.AdminAuth(prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)(http.HandlerFunc(h.ListLogs)),
	)
	rec := serveBearer(usageRoute, http.MethodGet, "/api/v1/logs?project_id="+jsonNumber(otherProjectID), projectAdminToken)
	require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
}

func assertGatewayModelDenied(t *testing.T, handler http.Handler, rawKey, requestID, model, reason string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"`+model+`","messages":[{"role":"user","content":"hello"}],"stream":false}`)))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), reason)
}

func seedPRD2P0OrgProjectUser(t *testing.T, pool *pgxpool.Pool, role string) (orgID, userID, projectID int64) {
	t.Helper()
	orgID = testutil.NewTestOrg(t, pool)
	_, userID, projectID = seedPRD2P0UserOnly(t, pool, orgID, role)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_developer')`,
		projectID, userID,
	)
	require.NoError(t, err)
	return orgID, userID, projectID
}

func seedPRD2P0UserOnly(t *testing.T, pool *pgxpool.Pool, orgID int64, role string) (username string, userID, projectID int64) {
	t.Helper()
	username = "user-" + role + "-" + jsonNumber(orgID)
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, $2, 'hash', $3, 'active')
		 RETURNING id`,
		orgID, username, role,
	).Scan(&userID)
	require.NoError(t, err)
	projectID = seedProject(t, pool, orgID, "project-"+role+"-"+jsonNumber(userID))
	return username, userID, projectID
}

func seedProject(t *testing.T, pool *pgxpool.Pool, orgID int64, slug string) int64 {
	t.Helper()
	var projectID int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO projects (org_id, name, slug, status)
		 VALUES ($1, $2, $2, 'active')
		 RETURNING id`,
		orgID, slug,
	).Scan(&projectID)
	require.NoError(t, err)
	return projectID
}

func seedPRD2P0ModelGovernance(t *testing.T, pool *pgxpool.Pool, orgID, projectID int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO model_registry (org_id, model_name, display_name, provider_family, lifecycle, approved_catalog, pricing_model_name)
		 VALUES ($1, 'gpt-4o', 'GPT-4o', 'mock', 'ga', TRUE, 'gpt-4o'),
		        ($1, 'gpt-4o-mini', 'GPT-4o Mini', 'mock', 'ga', TRUE, 'gpt-4o-mini'),
		        ($1, 'legacy-model', 'Legacy Model', 'mock', 'retired', FALSE, 'legacy-model')`,
		orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, status)
		 VALUES ($1, 'company-chat', 'gpt-4o', 'Company Chat', 'active')`,
		orgID,
	)
	require.NoError(t, err)
	var modelSetID int64
	err = pool.QueryRow(context.Background(),
		`INSERT INTO model_sets (org_id, name, slug, status)
		 VALUES ($1, 'Approved Chat', 'approved-chat', 'active')
		 RETURNING id`,
		orgID,
	).Scan(&modelSetID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO model_set_items (model_set_id, item_type, item_name)
		 VALUES ($1, 'alias', 'company-chat')`,
		modelSetID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO project_model_sets (project_id, model_set_id) VALUES ($1, $2)`,
		projectID, modelSetID,
	)
	require.NoError(t, err)
}

func seedPRD2P0Channel(t *testing.T, pool *pgxpool.Pool, baseURL, models string) {
	t.Helper()
	keyEnc, err := auth.EncryptKey([]byte("upstream-test-key"), prd2E2EEncryptionKey)
	require.NoError(t, err)
	var channelID int64
	err = pool.QueryRow(context.Background(),
		`INSERT INTO channels (name, provider, base_url, models, model_mapping, adapter_type, adapter_config, priority, status, circuit_state)
		 VALUES ('mock-openai-compatible', 'mock', $1, $2, '{}'::jsonb, 'openai_compatible', '{}'::jsonb, 1, 'active', 'healthy')
		 RETURNING id`,
		baseURL, models,
	).Scan(&channelID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO channel_keys (channel_id, key_enc, status) VALUES ($1, $2, 'active')`,
		channelID, keyEnc,
	)
	require.NoError(t, err)
}

func projectActorContext(ctx context.Context, orgID, userID int64, role, requestID string) context.Context {
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	return ctx
}

func mustJWT(t *testing.T, userID, orgID int64, role string) string {
	t.Helper()
	token, err := auth.GenerateJWT(userID, orgID, role, prd2E2EJWTSecret, 1)
	require.NoError(t, err)
	return token
}

func serveBearer(handler http.Handler, method, target, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func jsonNumber(value int64) string {
	return json.Number(strconv.FormatInt(value, 10)).String()
}
