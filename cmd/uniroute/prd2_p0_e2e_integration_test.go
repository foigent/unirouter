//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
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
	testutil.NewTestWalletCents(t, pool, orgID, 10000)
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
	chatHandler := middleware.GatewayAuth(pool, prd2E2EJWTSecret)(middleware.IdempotencyGuard(
		middleware.NewPostgresIdempotencyStore(pool), prd2E2EJWTSecret, newGatewayChatHandler(pool, relay),
	))

	requestID := "batch9-p0-e2e-success"
	chatBody := []byte(`{"model":"company-chat","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(chatBody))
	chatReq.Header.Set("Authorization", "Bearer "+createResp.Key)
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("X-Request-ID", requestID)
	chatReq.Header.Set(middleware.IdempotencyKeyHeader, "batch9-p0-business-operation")
	chatRec := httptest.NewRecorder()

	chatHandler.ServeHTTP(chatRec, chatReq)

	require.Equal(t, http.StatusOK, chatRec.Code, "body: %s", chatRec.Body.String())
	operationID := chatRec.Header().Get(middleware.OperationIDHeader)
	require.True(t, strings.HasPrefix(operationID, "op_"), operationID)
	require.NotEqual(t, requestID, operationID)
	require.Equal(t, "gpt-4o", upstreamModel)
	require.Equal(t, operationID, upstreamRequestID)
	require.Equal(t, "Bearer upstream-test-key", upstreamAuth)
	require.Equal(t, 1, upstreamCalls)
	require.Contains(t, chatRec.Body.String(), `"_uniroute"`)

	replayReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(chatBody))
	replayReq.Header.Set("Authorization", "Bearer "+createResp.Key)
	replayReq.Header.Set("Content-Type", "application/json")
	replayReq.Header.Set(middleware.IdempotencyKeyHeader, "batch9-p0-business-operation")
	replayRec := httptest.NewRecorder()
	chatHandler.ServeHTTP(replayRec, replayReq)
	require.Equal(t, http.StatusConflict, replayRec.Code, "body: %s", replayRec.Body.String())
	require.Equal(t, operationID, replayRec.Header().Get(middleware.OriginalOperationHeader))
	require.Contains(t, replayRec.Body.String(), `"code":"idempotency_error.already_completed"`)
	require.NotContains(t, replayRec.Body.String(), `"choices"`, "metadata-only replay must not store or return the model answer")
	require.Equal(t, 1, upstreamCalls, "idempotent replay dispatched the provider twice")

	var logProjectID, logKeyID int64
	var model, requestedModel, usageSource, clientRequestID string
	var statusCode, totalTokens int
	err := pool.QueryRow(ctx,
		`SELECT project_id, api_key_id, model, requested_model, status_code, usage_source, total_tokens,
		        COALESCE(client_request_id, '')
		 FROM call_logs WHERE request_id = $1`,
		operationID,
	).Scan(&logProjectID, &logKeyID, &model, &requestedModel, &statusCode, &usageSource, &totalTokens, &clientRequestID)
	require.NoError(t, err)
	require.Equal(t, projectID, logProjectID)
	require.Equal(t, createResp.ID, logKeyID)
	require.Equal(t, "gpt-4o", model)
	require.Equal(t, "company-chat", requestedModel)
	require.Equal(t, http.StatusOK, statusCode)
	require.Equal(t, "upstream", usageSource)
	require.Equal(t, 15, totalTokens)
	require.Equal(t, requestID, clientRequestID)

	deniedRequestID := "batch9-p0-e2e-api-key-model-denied"
	deniedReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"company-other","messages":[{"role":"user","content":"hello"}],"stream":false}`)))
	deniedReq.Header.Set("Authorization", "Bearer "+createResp.Key)
	deniedReq.Header.Set("Content-Type", "application/json")
	deniedReq.Header.Set("X-Request-ID", deniedRequestID)
	deniedRec := httptest.NewRecorder()

	chatHandler.ServeHTTP(deniedRec, deniedReq)

	require.Equal(t, http.StatusForbidden, deniedRec.Code, "body: %s", deniedRec.Body.String())
	deniedOperationID := deniedRec.Header().Get(middleware.OperationIDHeader)
	require.True(t, strings.HasPrefix(deniedOperationID, "op_"), deniedOperationID)
	require.NotEqual(t, deniedRequestID, deniedOperationID)
	require.Contains(t, deniedRec.Body.String(), `"code":"auth_error.model_not_allowed"`)

	var deniedEventType, deniedStage, deniedCaptureGap, deniedDecisionKind, deniedDecisionResult, deniedDecisionReason string
	var deniedStatusCode int
	err = pool.QueryRow(ctx,
		`SELECT event_type, stage, status_code, capture_gap, decision_kind, decision_result, decision_reason
		 FROM request_trace_events
		 WHERE request_id = $1
		 ORDER BY id
		 LIMIT 1`,
		deniedOperationID,
	).Scan(&deniedEventType, &deniedStage, &deniedStatusCode, &deniedCaptureGap, &deniedDecisionKind, &deniedDecisionResult, &deniedDecisionReason)
	require.NoError(t, err)
	require.Equal(t, "auth.model_not_allowed", deniedEventType)
	require.Equal(t, "auth", deniedStage)
	require.Equal(t, http.StatusForbidden, deniedStatusCode)
	require.Equal(t, "permission_denied", deniedCaptureGap)
	require.Equal(t, "auth", deniedDecisionKind)
	require.Equal(t, "rejected", deniedDecisionResult)
	require.Equal(t, "auth_error.model_not_allowed", deniedDecisionReason)

	var deniedCallLogs, deniedFreezes int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs WHERE request_id = $1`, deniedOperationID).Scan(&deniedCallLogs))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM freeze_idempotency WHERE request_id = $1`, deniedOperationID).Scan(&deniedFreezes))
	require.Zero(t, deniedCallLogs)
	require.Zero(t, deniedFreezes)

	var idemStatus string
	err = pool.QueryRow(ctx,
		`SELECT status FROM freeze_idempotency WHERE request_id = $1`,
		operationID,
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

func TestGatewayUnregisteredModelAllowedButAudited_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-unregistered","object":"chat.completion","model":"unregistered-chat","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`))
	}))
	defer upstream.Close()

	orgID, userID, projectID := seedPRD2P0OrgProjectUser(t, pool, "developer")
	testutil.NewTestWalletCents(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "unregistered-chat", 2, 10)
	seedPRD2P0Channel(t, pool, upstream.URL, "unregistered-chat")

	h := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	createBody := []byte(`{"org_id":` + jsonNumber(orgID) + `,"project_id":` + jsonNumber(projectID) + `,"name":"unregistered-key","allowed_models":"unregistered-chat","rpm_limit":60,"qps_limit":10}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq = createReq.WithContext(projectActorContext(createReq.Context(), orgID, userID, auth.RoleDeveloper, "batch20-create-key"))
	createRec := httptest.NewRecorder()

	h.CreateAPIKey(createRec, createReq)

	require.Equal(t, http.StatusCreated, createRec.Code, "body: %s", createRec.Body.String())
	var createResp struct {
		Key string `json:"key"`
	}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &createResp))
	require.NotEmpty(t, createResp.Key)

	relay := gateway.NewRelay(pool, nil, gateway.NewRouter(pool), billing.NewBilling(pool, nil), filter.NewDFAFilter(nil), prd2E2EEncryptionKey)
	chatHandler := middleware.GatewayAuth(pool, prd2E2EJWTSecret)(newGatewayChatHandler(pool, relay))

	requestID := "batch20-unregistered-model"
	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"unregistered-chat","messages":[{"role":"user","content":"hello"}],"stream":false}`)))
	chatReq.Header.Set("Authorization", "Bearer "+createResp.Key)
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("X-Request-ID", requestID)
	chatRec := httptest.NewRecorder()

	chatHandler.ServeHTTP(chatRec, chatReq)

	require.Equal(t, http.StatusOK, chatRec.Code, "body: %s", chatRec.Body.String())
	operationID := chatRec.Header().Get(middleware.OperationIDHeader)
	require.True(t, strings.HasPrefix(operationID, "op_"), operationID)
	require.NotEqual(t, requestID, operationID)

	var auditProjectID int64
	var auditSource, auditModel, auditKeyPrefix string
	err := pool.QueryRow(ctx,
		`SELECT project_id, source, after_state->>'model', after_state->>'key_prefix'
		 FROM audit_logs
		 WHERE action = 'model.unregistered.call' AND resource_id = 'unregistered-chat' AND request_id = $1`,
		operationID,
	).Scan(&auditProjectID, &auditSource, &auditModel, &auditKeyPrefix)
	require.NoError(t, err)
	require.Equal(t, projectID, auditProjectID)
	require.Equal(t, "gateway_api", auditSource)
	require.Equal(t, "unregistered-chat", auditModel)
	require.NotEmpty(t, auditKeyPrefix)
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

	platformOnly := middleware.AdminAuth(pool, prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
	)
	require.Equal(t, http.StatusForbidden, serveBearer(platformOnly, http.MethodGet, "/api/v1/channels", developerToken).Code)
	require.Equal(t, http.StatusForbidden, serveBearer(platformOnly, http.MethodPost, "/api/v1/channels", billingToken).Code)

	auditRoute := middleware.AdminAuth(pool, prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})),
	)
	require.Equal(t, http.StatusForbidden, serveBearer(auditRoute, http.MethodGet, "/api/v1/audit-logs", developerToken).Code)

	h := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	usageRoute := middleware.AdminAuth(pool, prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)(http.HandlerFunc(h.ListLogs)),
	)
	rec := serveBearer(usageRoute, http.MethodGet, "/api/v1/logs?project_id="+jsonNumber(otherProjectID), projectAdminToken)
	require.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
}

func TestPRD2AuditFiltersJWTBeforeAfterE2E_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	orgID := testutil.NewTestOrg(t, pool)
	_, userID, projectID := seedPRD2P0UserOnly(t, pool, orgID, auth.RoleOrgAdmin)
	_, err := pool.Exec(ctx,
		`INSERT INTO audit_logs (org_id, project_id, user_id, action, resource, resource_id, detail, before_state, after_state, source, request_id, created_at)
		 VALUES ($1, $2, $3, 'channel.update', 'channel', '77',
		         '{"fields":["name"]}'::jsonb,
		         '{"name":"before"}'::jsonb,
		         '{"name":"after"}'::jsonb,
		         'admin_console',
		         'audit-filter-e2e',
		         NOW())`,
		orgID, projectID, userID,
	)
	require.NoError(t, err)

	h := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	auditRoute := middleware.AdminAuth(pool, prd2E2EJWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)(http.HandlerFunc(h.ListAuditLogs)),
	)
	token := mustJWT(t, userID, orgID, auth.RoleOrgAdmin)
	target := "/api/v1/audit-logs?org_id=" + jsonNumber(orgID) +
		"&project_id=" + jsonNumber(projectID) +
		"&actor_id=" + jsonNumber(userID) +
		"&action=channel.update&resource=channel&resource_id=77&source=admin_console"
	rec := serveBearer(auditRoute, http.MethodGet, target, token)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp struct {
		Data []struct {
			ProjectID int64           `json:"project_id"`
			ActorID   int64           `json:"actor_id"`
			Action    string          `json:"action"`
			Resource  string          `json:"resource"`
			Source    string          `json:"source"`
			Before    json.RawMessage `json:"before"`
			After     json.RawMessage `json:"after"`
		} `json:"data"`
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Total)
	require.Len(t, resp.Data, 1)
	require.Equal(t, projectID, resp.Data[0].ProjectID)
	require.Equal(t, userID, resp.Data[0].ActorID)
	require.Equal(t, "channel.update", resp.Data[0].Action)
	require.Equal(t, "channel", resp.Data[0].Resource)
	require.Equal(t, "admin_console", resp.Data[0].Source)
	require.Contains(t, string(resp.Data[0].Before), `"name":"before"`)
	require.Contains(t, string(resp.Data[0].After), `"name":"after"`)
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
		 VALUES ($1, 'company-chat', 'gpt-4o', 'Company Chat', 'active'),
		        ($1, 'company-other', 'gpt-4o', 'Company Other', 'active')`,
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
		 VALUES ($1, 'alias', 'company-chat'),
		        ($1, 'alias', 'company-other')`,
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
