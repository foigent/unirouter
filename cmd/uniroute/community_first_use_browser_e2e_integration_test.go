//go:build integration && pr4browser

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/policybilling"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

// This extends the existing production-SPA browser acceptance with actual
// canonical gateway execution. Only the provider is synthetic. No application
// API response, session, fee, ledger, or membership result is mocked.
func TestCommunityFirstUseBusinessBrowserE2E(t *testing.T) {
	require.Equal(t, "1", os.Getenv("REQUIRE_TEST_DATABASE"), "explicit isolated PostgreSQL is required")
	require.NotEmpty(t, os.Getenv("TEST_DATABASE_URL"))
	require.Equal(t, "1", os.Getenv("FIRST_USE_REDIS_ISOLATED"), "acknowledge task/job-owned disposable Redis")
	address := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	require.NotEmpty(t, address, "never fall back to a shared Redis")
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	rdb := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = rdb.Close() })
	require.NoError(t, rdb.Ping(ctx).Err())
	require.NoError(t, rdb.FlushDB(ctx).Err())
	t.Cleanup(func() { require.NoError(t, rdb.FlushDB(context.Background()).Err()) })
	traps := []*pr4BrowserTrafficTrap{newPR4BrowserTrafficTrap(t, "provider"), newPR4BrowserTrafficTrap(t, "feed"), newPR4BrowserTrafficTrap(t, "other")}
	transport := newPR4BrowserEgressTransport(t, traps[0], traps[1], traps[2])
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	// Prove the egress guard independently; control traffic never enters product counters.
	controls := []*pr4BrowserTrafficTrap{newPR4BrowserTrafficTrap(t, "control-provider"), newPR4BrowserTrafficTrap(t, "control-feed"), newPR4BrowserTrafficTrap(t, "control-other")}
	provePR4BrowserBackendEgressGuard(t, newPR4BrowserEgressTransport(t, controls[0], controls[1], controls[2]), controls[0], controls[1], controls[2])
	var providerCalls, nonStreamCalls, streamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer community-synthetic-provider" {
			http.Error(w, "wrong synthetic provider binding", 400)
			return
		}
		var body struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "deepseek-v4-flash" {
			http.Error(w, "wrong model", 400)
			return
		}
		if body.Stream {
			streamCalls.Add(1)
			content := "community-stream-ok"
			for _, message := range body.Messages {
				if strings.Contains(message.Content, "UNIROUTER_STREAM_PROBE_OK") {
					content = "UNIROUTER_STREAM_PROBE_OK"
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"synthetic-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"%s\"},\"finish_reason\":null}]}\n\n", content)
			w.(http.Flusher).Flush()
			fmt.Fprint(w, "data: {\"id\":\"synthetic-stream\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n\n")
			return
		}
		nonStreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"synthetic-chat","object":"chat.completion","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"community-ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	t.Cleanup(upstream.Close)
	f := seedCommunityBusinessFixture(t, pool)
	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)
	limits, err := limitpolicy.NewService(pool, rdb, limitpolicy.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, limits.Refresh(ctx))
	events, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	financial := billing.NewBilling(pool, rdb).WithMode(billing.BillingModePrepaidEnforced).WithRequestPricingAuthority(billing.RequestPricingAuthorityRateCard)
	financial.SetOutbox(events)
	notifications := billing.NewEventService(pool, "synthetic-community-notification-secret")
	notifications.SetOutbox(events)
	http.DefaultTransport = transport
	hooks, err := newRuntimeTerminalHooks(pool, events, nil, limits)
	require.NoError(t, err)
	shared := policybilling.New(financial, limits)
	shared.SetTerminalHooks(hooks)
	planner, err := modelrouting.NewPlanner(pool, policy)
	require.NoError(t, err)
	executionConfig, err := modelexecution.ConfigFromRoutingPolicy(policy)
	require.NoError(t, err)
	coordinator, err := modelexecution.New(pool, financial, executionConfig)
	require.NoError(t, err)
	relay := gateway.NewRelay(pool, rdb, router, shared, filter.NewDFAFilter(nil), []byte(pr4BrowserEncryptionKey))
	relay.SetBillingAdmissionReadinessInspector(financial)
	relay.SetOutbox(events)
	relay.SetEventService(notifications)
	relay.SetTraceEvents(true, 1)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, policy))
	shared.SetEarlyRateAdmissionEvidenceSink(relay)
	payload, err := payloadpolicy.NewBudget(payloadpolicy.DefaultLimits())
	require.NoError(t, err)
	relay.SetPayloadBudget(payload)
	h := admin.NewHandlerWithAPIKeyHashSecret(pool, pr4BrowserJWTSecret, pr4BrowserAPIKeySecret, []byte(pr4BrowserEncryptionKey), nil)
	h.SetRedisClient(rdb)
	providerTransport := &communityLoopbackTransport{destination: upstream.URL, delegate: transport}
	h.SetModelDeploymentProbeHTTPDoer(&http.Client{Transport: providerTransport})
	relay.SetProviderHTTPDoer(&http.Client{Transport: providerTransport})
	h.SetRoutingController(router)
	h.SetRuntimeChangeManager(manager)
	h.SetBillingReadinessProvider(financial)
	h.SetRequestPricingAuthority("rate_card")
	h.SetOutbox(events)
	h.SetLimitPolicyReloader(limits)
	evaluator, err := modelauthz.NewEvaluator(pool, "development")
	require.NoError(t, err)
	h.SetModelAuthorizationEvaluator(evaluator)
	relay.SetModelAuthorization(evaluator)
	for _, reg := range []struct {
		kind    string
		handler outbox.Handler
	}{
		{outbox.EventBillingUsagePrepared, financial.ProcessUsageJournalEvent}, {outbox.EventBillingSettled, financial.ProcessUsageJournalEvent}, {outbox.EventBillingCancelled, financial.ProcessUsageJournalEvent},
		{outbox.EventBillingUsagePreparedV1, financial.ProcessUsageJournalEvent}, {outbox.EventBillingSettledV1, financial.ProcessUsageJournalEvent}, {outbox.EventBillingCancelledV1, financial.ProcessUsageJournalEvent},
		{outbox.EventCallLog, relay.ProcessCallLogOutbox}, {outbox.EventCallLogV1, relay.ProcessCallLogOutbox}, {outbox.EventTrace, relay.ProcessTraceOutbox}, {outbox.EventAudit, h.ProcessAuditOutbox},
		{outbox.EventBudgetEvaluate, notifications.ProcessBudgetEvaluation}, {outbox.EventNotificationDelivery, notifications.ProcessNotificationDelivery}, {outbox.EventRuntimeTerminal, hooks.ProcessEvent}, {outbox.EventRuntimeTerminalV1, hooks.ProcessEvent},
	} {
		require.NoError(t, events.RegisterHandler(reg.kind, reg.handler))
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); events.Run(workerCtx) }()
	t.Cleanup(func() { cancel(); <-done })
	authenticated := func(next http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(pool, pr4BrowserJWTSecret)(middleware.CustomerContext(pool)(next))
	}
	roles := func(next http.HandlerFunc, allowed ...string) http.Handler {
		return authenticated(func(w http.ResponseWriter, r *http.Request) {
			middleware.RequireOrganizationRoles(allowed...)(next).ServeHTTP(w, r)
		})
	}
	orgAdmin := func(next http.HandlerFunc) http.Handler {
		return roles(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
	mux.HandleFunc("GET /api/v1/auth/oidc/config", h.OIDCConfig)
	for pattern, fn := range map[string]http.HandlerFunc{"GET /api/v1/me": h.GetMe, "GET /api/v1/organizations": h.ListOrganizations, "GET /api/v1/projects": h.ListProjects, "GET /api/v1/dashboard/stats": h.DashboardStats, "GET /api/v1/projects/{id}": h.GetProject, "GET /api/v1/projects/{id}/members": h.ListProjectMembers, "GET /api/v1/projects/{id}/budget": h.GetProjectBudget, "PUT /api/v1/projects/{id}/budget": h.UpdateProjectBudget, "POST /api/v1/keys": h.CreateAPIKey} {
		mux.Handle(pattern, authenticated(fn))
	}
	registerOverviewNavigationRoutes(mux, h, &overviewBrowserFaults{}, authenticated, roles)
	mux.Handle("POST /api/v1/users", orgAdmin(h.CreateUser))
	var failMembership atomic.Bool
	failMembership.Store(true)
	mux.Handle("PUT /api/v1/projects/{id}/members/{user_id}", authenticated(func(w http.ResponseWriter, r *http.Request) {
		// A cancelled SQL context exercises the real handler's partial-create error.
		// No fabricated membership response or successful database write is supplied.
		if failMembership.Swap(false) {
			c, stop := context.WithCancel(r.Context())
			stop()
			r = r.WithContext(c)
		}
		h.UpsertProjectMember(w, r)
	}))
	chat := middleware.IdempotencyGuard(middleware.NewPostgresIdempotencyStore(pool), pr4BrowserAPIKeySecret, newGatewayChatHandler(pool, relay, events))
	gatewayAuth := middleware.GatewayAuth(pool, pr4BrowserAPIKeySecret)
	mux.Handle("POST /v1/chat/completions", middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, payloadpolicy.RequestMiddleware(payload, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext))(middleware.StrictJSONIngress(shared.GatewayRateAdmission(chat))))
	mux.Handle("POST /v1/chat/completions/preflight", chatPreflightOperation(middleware.GatewayAuthAcrossBufferedBody(gatewayAuth, payloadpolicy.RequestMiddleware(payload, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext))(middleware.StrictJSONIngress(newGatewayChatPreflightHandler(pool, relay)))))
	server := httptest.NewServer(pr4BrowserSPAHandler(filepath.Join(pr4BrowserRepositoryRoot(t), "frontend", "dist"), middleware.SecurityHeaders(middleware.OperationIdentityWithSecret(pr4BrowserAPIKeySecret)(mux))))
	t.Cleanup(server.Close)
	f.BaseURL = server.URL
	seedCommunityCanonical(t, pool, f, manager, server.URL)
	require.EqualValues(t, 2, providerCalls.Load(), "real loopback operation and exact streaming probes prepare the canonical baseline")
	artifacts := os.Getenv("FIRST_USE_EVIDENCE_DIR")
	if artifacts == "" {
		artifacts = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(artifacts, 0700))
	fixture, err := json.Marshal(f)
	require.NoError(t, err)
	command := exec.Command("node", filepath.Join(pr4BrowserRepositoryRoot(t), "frontend", "e2e", "community_first_use.mjs"))
	command.Dir = filepath.Join(pr4BrowserRepositoryRoot(t), "frontend")
	// Do not forward host credential or proxy environment variables to Chromium.
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "FIRST_USE_EVIDENCE_DIR"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Env = append(command.Env, "COMMUNITY_FIRST_USE_FIXTURE="+string(fixture), "FIRST_USE_EVIDENCE_DIR="+artifacts)
	processLog, err := os.Create(filepath.Join(artifacts, "browser-process.log"))
	require.NoError(t, err)
	var output bytes.Buffer
	command.Stdout = io.MultiWriter(&output, processLog)
	command.Stderr = command.Stdout
	runErr := command.Run()
	require.NoError(t, processLog.Close())
	t.Logf("%s", output.String())
	verifyCommunityBusinessSQL(t, pool, f, artifacts, runErr == nil)
	for _, trap := range traps {
		require.Zero(t, trap.count.Load(), "external %s traffic", trap.name)
	}
	require.NoError(t, runErr, "real backend browser first-use failed: %s", output.String())
	require.EqualValues(t, 4, providerCalls.Load(), "two setup probes plus two ordinary requests; refusal never dispatches")
	require.EqualValues(t, 2, nonStreamCalls.Load())
	require.EqualValues(t, 2, streamCalls.Load())
	t.Log("COMMUNITY_FIRST_USE canonical=true db_price_provider=true pricing=rate_card synthetic_prepaid=true external_provider=0 XI=0")
}

func seedCommunityBusinessFixture(t *testing.T, pool *pgxpool.Pool) pr4BrowserFixture {
	t.Helper()
	ctx := t.Context()
	f := pr4BrowserFixture{Password: pr4BrowserPassword, ReadyModel: "community-first-use", Users: map[string]int64{}, Usernames: map[string]string{"org_admin": "community-org-admin", "maker": "community-billing-maker", "checker": "community-billing-checker", "platform": "community-platform"}}
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('Community First Use','active','customer') RETURNING id`).Scan(&f.OrgID))
	for _, p := range []struct {
		name, slug string
		id         *int64
	}{{"Community Alpha", "community-alpha", &f.ProjectA}, {"Community Beta", "community-beta", &f.ProjectB}} {
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status)VALUES($1,$2,$3,'active')RETURNING id`, f.OrgID, p.name, p.slug).Scan(p.id))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(f.Password), bcrypt.MinCost)
	require.NoError(t, err)
	for key, name := range f.Usernames {
		role := auth.RoleBillingAdmin
		if key == "org_admin" {
			role = auth.RoleOrgAdmin
		}
		if key == "platform" {
			role = auth.RolePlatformAdmin
		}
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status,auth_provider)VALUES($1,$2,$3,$4,$4,'active','local')RETURNING id`, f.OrgID, name, string(hash), role).Scan(&id))
		f.Users[key] = id
	}
	_, err = pool.Exec(ctx, `INSERT INTO wallets(org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros)VALUES($1,100000000,0,0,0)`, f.OrgID)
	require.NoError(t, err)
	return f
}

func seedCommunityCanonical(t *testing.T, pool *pgxpool.Pool, f pr4BrowserFixture, manager *runtimechange.Manager, base string) {
	t.Helper()
	token := testutil.MustAdminSessionJWT(t, t.Context(), pool, f.Users["platform"], f.OrgID, auth.RolePlatformAdmin, pr4BrowserJWTSecret)
	post := func(path string, body any, status int) map[string]any {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		req, err := http.NewRequest("POST", base+path, bytes.NewReader(data))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", "community-setup-"+path)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		data, err = io.ReadAll(res.Body)
		require.NoError(t, err)
		require.Equal(t, status, res.StatusCode, "setup %s: %s", path, data)
		var result map[string]any
		require.NoError(t, json.Unmarshal(data, &result))
		return result
	}
	created := post("/api/v1/model-deployments", map[string]any{"connection": map[string]any{"name": "Community Loopback", "provider": "deepseek", "base_url": "https://api.deepseek.com", "adapter_type": "openai_compatible", "adapter_config": map[string]any{"endpoint_path_mode": "base_relative"}}, "public_model": map[string]any{"model_name": f.ReadyModel, "display_name": "Community First Use", "provider_family": "deepseek", "execution_profile_id": "deepseek.v4-flash.api", "execution_profile_version": "1.5.0", "lifecycle": "ga", "approved_catalog": true, "context_length": 128000, "output_limit": 8192}, "deployment": map[string]any{"upstream_model": "deepseek-v4-flash", "max_context": 128000, "default_max_output": 8192}, "credential": map[string]any{"api_key": "community-synthetic-provider"}, "operation": map[string]any{"operation_id": "openai.chat_completions.v1"}}, 201)
	app := created["application"].(map[string]any)
	deployment := int64(app["deployment"].(map[string]any)["id"].(float64))
	model := int64(app["public_model"].(map[string]any)["id"].(float64))
	binding := int64(app["credential"].(map[string]any)["bindings"].([]any)[0].(map[string]any)["id"].(float64))
	probe := post(fmt.Sprintf("/api/v1/model-deployments/%d/operations/openai.chat_completions.v1/probes", deployment), map[string]any{"credential_binding_id": binding}, 200)
	require.Equal(t, true, probe["success"])
	require.Equal(t, true, probe["current"])
	capabilityPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/openai.chat_completions.v1/capabilities/streaming", deployment)
	capability := post(capabilityPath+"/probes", map[string]any{"credential_binding_id": binding}, 200)
	require.Equal(t, true, capability["success"], "exact synthetic stream probe: %v", capability)
	approved := post(capabilityPath+"/routing-approvals", map[string]any{"capability_probe_id": capability["capability_probe_id"], "reason": "reviewed synthetic loopback stream contract"}, 201)
	require.Equal(t, true, approved["qualified"])

	a := integrationfixture.AtomicRelease{PublicModelID: model, CustomerOrgID: f.OrgID, ActorUserID: f.Users["platform"], ActorOrgID: f.OrgID, Selections: []runtimechange.DeploymentReleaseSelection{{DeploymentID: deployment, OperationID: "openai.chat_completions.v1"}}, Reason: "community browser canonical baseline"}
	release := integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, a)
	active := integrationfixture.ActivateInitialDeploymentRelease(t, manager, a, release)
	require.Len(t, active.Candidate.DeploymentRoutes, 1)
	t.Logf("CANONICAL_BASELINE model_id=%d deployment=%d release=%s sealed=true slot=active probe=real_loopback", model, deployment, release.ReleaseRef)
}

func verifyCommunityBusinessSQL(t *testing.T, pool *pgxpool.Pool, f pr4BrowserFixture, artifacts string, complete bool) {
	t.Helper()
	ctx := t.Context()
	facts := map[string]json.RawMessage{}
	queries := map[string]string{
		"members":           `SELECT u.id,u.username,u.organization_role,pm.project_id,pm.role AS project_role FROM users u LEFT JOIN project_members pm ON pm.user_id=u.id WHERE u.org_id=$1 AND u.username LIKE 'community-%' ORDER BY u.id,pm.project_id`,
		"keys":              `SELECT id,project_id,name,status FROM api_keys WHERE org_id=$1 ORDER BY id`,
		"budgets":           `SELECT id,budget_cents,budget_period FROM projects WHERE org_id=$1 ORDER BY id`,
		"rate_versions":     `SELECT rate_card_ref,version,provider_ref,model_ref,state,created_by,approved_by,activated_by FROM enterprise_finance_rate_card_versions WHERE org_id=$1 ORDER BY rate_card_ref,version`,
		"traces":            `SELECT request_id,event_type,stage,status_code,metadata FROM request_trace_events WHERE org_id=$1 ORDER BY id`,
		"logs":              `SELECT request_id,project_id,api_key_id,owner_user_id,model,requested_model,status_code,prompt_tokens,completion_tokens,total_tokens,cost_micros,is_stream,usage_source,pricing_version,route_channel_id FROM call_logs WHERE org_id=$1 ORDER BY id`,
		"journal":           `SELECT request_id,event_type,project_id,prompt_tokens,completion_tokens,usage_source,calculated_cost_micros,charged_cost_micros,pricing_version,billing_mode FROM usage_finalization_journal WHERE org_id=$1 ORDER BY id`,
		"budget_usage":      `SELECT project_id,budget_period,spent_micros FROM project_budget_usage WHERE org_id=$1 ORDER BY project_id`,
		"notifications":     `SELECT id,project_id,event_type,delivery_state FROM notification_events WHERE org_id=$1 ORDER BY id`,
		"limits":            `SELECT id,name,project_id,mode,tpm_limit,version FROM distributed_limit_policies WHERE org_id=$1 ORDER BY id`,
		"wallet":            `SELECT balance_micros,frozen_micros,total_spent_micros FROM wallets WHERE org_id=$1`,
		"pricing_snapshots": `SELECT request_ref,snapshot_hash,priced_at FROM enterprise_finance_pricing_snapshots WHERE org_id=$1 ORDER BY id`,
	}
	queries["frozen_request_prices"] = `SELECT request_id,project_id,api_key_id,access_credential_id,credential_ref,access_grant_id,model_name,status,pricing_version,effective_input_price_micros_per_million,effective_output_price_micros_per_million,effective_cache_read_price_micros_per_million,effective_cache_creation_price_micros_per_million FROM freeze_idempotency WHERE org_id=$1 ORDER BY request_id`
	capture := func() {
		for name, query := range queries {
			var data []byte
			err := pool.QueryRow(ctx, "SELECT coalesce(jsonb_agg(to_jsonb(r)),'[]') FROM ("+query+") r", f.OrgID).Scan(&data)
			require.NoError(t, err, "capture %s", name)
			facts[name] = data
		}
		encoded, err := json.MarshalIndent(facts, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(artifacts, "sql-authority.json"), append(encoded, '\n'), 0600))
	}
	defer capture()
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE org_id=$1 AND username='community-cancelled'`, f.OrgID).Scan(&count))
	require.Zero(t, count, "cancelled create writes no user")
	if !complete {
		return
	}
	var result struct {
		Requests []struct {
			RequestID string `json:"request_id"`
			Status    int    `json:"status"`
		} `json:"requests"`
	}
	data, err := os.ReadFile(filepath.Join(artifacts, "browser-result.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &result))
	require.Len(t, result.Requests, 3)
	var member int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM users WHERE org_id=$1 AND username='community-new-member' AND organization_role='member'`, f.OrgID).Scan(&member))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE user_id=$1`, member).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE user_id=$1 AND project_id=$2 AND role='project_user'`, member, f.ProjectA).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM enterprise_finance_rate_card_versions WHERE org_id=$1 AND version=3 AND state='active' AND created_by<>approved_by AND approved_by<>''`, f.OrgID).Scan(&count))
	require.Equal(t, 1, count)
	var budget int64
	var period string
	require.NoError(t, pool.QueryRow(ctx, `SELECT budget_cents,budget_period FROM projects WHERE org_id=$1 AND id=$2`, f.OrgID, f.ProjectA).Scan(&budget, &period))
	require.EqualValues(t, 1, budget)
	require.Equal(t, "monthly", period)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM distributed_limit_policies WHERE org_id=$1 AND project_id=$2 AND name='community-first-use-hard-limit' AND mode='enforce' AND tpm_limit=1`, f.OrgID, f.ProjectA).Scan(&count))
	require.Equal(t, 1, count)
	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	quote, err := store.ResolveRequestPriceQuote(ctx, f.OrgID, f.ReadyModel, time.Now().UTC())
	require.NoError(t, err)
	require.EqualValues(t, 3, quote.RateCardVersion)
	require.Equal(t, f.ReadyModel, quote.ModelRef)
	var browserResult struct {
		RateCardRef   string `json:"rate_card_ref"`
		CredentialRef string `json:"credential_ref"`
		LimitPolicy   struct {
			ID      int64 `json:"id"`
			Version int64 `json:"version"`
		} `json:"limit_policy"`
	}
	require.NoError(t, json.Unmarshal(data, &browserResult))
	require.Equal(t, browserResult.RateCardRef, quote.RateCardRef)
	require.NotEmpty(t, browserResult.CredentialRef)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM freeze_idempotency WHERE org_id=$1`, f.OrgID).Scan(&count))
	require.Equal(t, 2, count, "preflight and refusal must not create reservations")
	quoteWire, err := json.MarshalIndent(quote, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(artifacts, "resolved-rate-quote.json"), append(quoteWire, '\n'), 0600))
	for _, request := range result.Requests {
		if request.Status == 200 {
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM freeze_idempotency f JOIN api_keys k ON k.id=f.api_key_id WHERE f.org_id=$1 AND f.request_id=$2 AND f.credential_ref=$3 AND f.access_credential_id>0 AND f.access_grant_id>0 AND k.name='community-first-use-member' AND k.project_id=$4`, f.OrgID, request.RequestID, browserResult.CredentialRef, f.ProjectA).Scan(&count))
			require.Equal(t, 1, count, "the issued member credential must own the actual reservation")

			var frozenVersion string
			var frozenProject, input, output, read, creation int64
			var model string
			require.NoError(t, pool.QueryRow(ctx, `SELECT project_id,model_name,pricing_version,effective_input_price_micros_per_million,effective_output_price_micros_per_million,effective_cache_read_price_micros_per_million,effective_cache_creation_price_micros_per_million FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, f.OrgID, request.RequestID).Scan(&frozenProject, &model, &frozenVersion, &input, &output, &read, &creation))
			require.Equal(t, f.ProjectA, frozenProject)
			require.Equal(t, f.ReadyModel, model)
			require.Equal(t, quote.PricingVersion, frozenVersion)
			require.EqualValues(t, 1000000000, input)
			require.EqualValues(t, 2000000000, output)
			require.EqualValues(t, 1000000000, read)
			require.EqualValues(t, 1000000000, creation)

			require.Eventually(t, func() bool {
				return pool.QueryRow(ctx, `SELECT count(*) FROM usage_finalization_journal WHERE org_id=$1 AND request_id=$2 AND event_type='settled' AND prompt_tokens=10 AND completion_tokens=5 AND charged_cost_micros=20000 AND billing_mode='prepaid_enforced'`, f.OrgID, request.RequestID).Scan(&count) == nil && count == 1
			}, 10*time.Second, 50*time.Millisecond, "same request must reach real settlement journal")
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM call_logs WHERE org_id=$1 AND request_id=$2 AND project_id=$3 AND owner_user_id=$4 AND total_tokens=15 AND cost_micros=20000 AND status_code=200 AND route_channel_id IS NULL`, f.OrgID, request.RequestID, f.ProjectA, member).Scan(&count))
			require.Equal(t, 1, count, "canonical requests must not use legacy channels")
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2 AND status='settled'`, f.OrgID, request.RequestID).Scan(&count))
			require.Equal(t, 1, count)
		} else {
			require.Equal(t, 429, request.Status)
			require.Positive(t, browserResult.LimitPolicy.ID)
			require.Positive(t, browserResult.LimitPolicy.Version)
			policyEvidence := fmt.Sprintf("%%policy:%d@%d#%%=deny/tpm_exceeded%%", browserResult.LimitPolicy.ID, browserResult.LimitPolicy.Version)
			require.Eventually(t, func() bool {
				return pool.QueryRow(ctx, `SELECT count(*) FROM request_trace_events WHERE org_id=$1 AND request_id=$2 AND project_id=$3 AND owner_user_id=$4 AND event_type='limit_policy.admission' AND metadata->>'limit_policy_result'='rejected' AND metadata->>'limit_policy_reason'='tpm_exceeded' AND metadata->>'limit_policy_chain' LIKE $5`, f.OrgID, request.RequestID, f.ProjectA, member, policyEvidence).Scan(&count) == nil && count == 1
			}, 10*time.Second, 50*time.Millisecond, "same denial ID must retain exact policy/version admission evidence")
			require.Eventually(t, func() bool {
				return pool.QueryRow(ctx, `SELECT count(*) FROM request_trace_events WHERE org_id=$1 AND request_id=$2 AND project_id=$3 AND owner_user_id=$4 AND event_type='billing.freeze.rejected' AND metadata->>'error_code'='limit_policy.tpm_exceeded'`, f.OrgID, request.RequestID, f.ProjectA, member).Scan(&count) == nil && count == 1
			}, 10*time.Second, 50*time.Millisecond, "same denial ID must retain freeze rejection")

			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, f.OrgID, request.RequestID).Scan(&count))
			require.Zero(t, count, "shared limit refuses before financial reservation")
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM usage_finalization_journal WHERE org_id=$1 AND request_id=$2`, f.OrgID, request.RequestID).Scan(&count))
			require.Zero(t, count, "denial creates no invented usage or financial terminal")
		}
	}
	var spent int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT spent_micros FROM project_budget_usage WHERE org_id=$1 AND project_id=$2 AND budget_period='monthly'`, f.OrgID, f.ProjectA).Scan(&spent))
	require.EqualValues(t, 40000, spent, "settled journal increments project usage once per request")
	require.Eventually(t, func() bool {
		return pool.QueryRow(ctx, `SELECT count(*) FROM notification_events WHERE org_id=$1 AND project_id=$2 AND event_type='project_budget.threshold'`, f.OrgID, f.ProjectA).Scan(&count) == nil && count == 1
	}, 10*time.Second, 50*time.Millisecond, "real budget threshold alert")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM notification_webhooks WHERE org_id=$1`, f.OrgID).Scan(&count))
	require.Zero(t, count, "no external notification subscription")
	var balance, frozen, total int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros,frozen_micros,total_spent_micros FROM wallets WHERE org_id=$1`, f.OrgID).Scan(&balance, &frozen, &total))
	require.EqualValues(t, 99960000, balance)
	require.Zero(t, frozen)
	require.EqualValues(t, 40000, total)
	t.Log("SQL_AUTHORITY member=isolated canonical=active fee=version3 charge_micros=40000 usage_tokens=30 budget_alert=1 refusal_reservation=0")
}

// This transport substitutes a loopback provider wire endpoint for one exact
// reviewed fixture identity. It never returns a fabricated HTTP response and
// never permits a provider socket outside the task-local server. All other
// destinations retain the existing deny-and-observe egress guard.
type communityLoopbackTransport struct {
	destination string
	delegate    http.RoundTripper
}

func (t *communityLoopbackTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme == "https" && r.URL.Host == "api.deepseek.com" {
		target, err := url.Parse(t.destination)
		if err != nil {
			return nil, err
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Scheme, u.Host = target.Scheme, target.Host
		clone.URL = &u
		clone.Host = target.Host
		return t.delegate.RoundTrip(clone)
	}
	return t.delegate.RoundTrip(r)
}
