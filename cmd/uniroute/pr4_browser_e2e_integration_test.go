//go:build integration && pr4browser

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const (
	pr4BrowserJWTSecret      = "pr4-browser-jwt-secret-32-bytes"
	pr4BrowserAPIKeySecret   = "pr4-browser-api-key-secret-32-bytes"
	pr4BrowserPassword       = "PR4-browser-password-2026"
	pr4BrowserReadyModel     = "pr4-browser-ready-model"
	pr4BrowserRuntimeModel   = "pr4-browser-runtime-only"
	pr4BrowserEncryptionKey  = "0123456789abcdef01234567"
	pr4BrowserProviderTarget = "https://provider.pr4.invalid"
	pr4BrowserFeedTarget     = "https://feed.pr4.invalid"
	pr4BrowserCanonicalFeed  = "/api/v1/integrations/xinsight/completed-executions"
)

type pr4BrowserTrafficTrap struct {
	name   string
	count  atomic.Int64
	server *httptest.Server
}

func newPR4BrowserTrafficTrap(t *testing.T, name string) *pr4BrowserTrafficTrap {
	t.Helper()
	trap := &pr4BrowserTrafficTrap{name: name}
	trap.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		trap.count.Add(1)
		http.Error(w, name+" traffic is forbidden in PR4 browser acceptance", http.StatusTeapot)
	}))
	t.Cleanup(trap.server.Close)
	return trap
}

type pr4BrowserEgressTransport struct {
	delegate       http.RoundTripper
	providerOrigin string
	feedOrigin     string
	providerTrap   *url.URL
	feedTrap       *url.URL
	upstreamTrap   *url.URL
}

func newPR4BrowserEgressTransport(
	t *testing.T,
	providerTrap, feedTrap, upstreamTrap *pr4BrowserTrafficTrap,
) *pr4BrowserEgressTransport {
	t.Helper()
	parse := func(raw string) *url.URL {
		t.Helper()
		parsed, err := url.Parse(raw)
		require.NoError(t, err)
		return parsed
	}
	delegate := http.DefaultTransport
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := transport.Clone()
		clone.Proxy = nil
		delegate = clone
	}
	return &pr4BrowserEgressTransport{
		delegate:       delegate,
		providerOrigin: pr4BrowserProviderTarget,
		feedOrigin:     pr4BrowserFeedTarget,
		providerTrap:   parse(providerTrap.server.URL),
		feedTrap:       parse(feedTrap.server.URL),
		upstreamTrap:   parse(upstreamTrap.server.URL),
	}
}

func (transport *pr4BrowserEgressTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	target := transport.upstreamTrap
	origin := request.URL.Scheme + "://" + request.URL.Host
	switch origin {
	case transport.providerOrigin:
		target = transport.providerTrap
	case transport.feedOrigin:
		target = transport.feedTrap
	}
	if request.URL.IsAbs() && request.URL.Hostname() != "127.0.0.1" && request.URL.Hostname() != "localhost" {
		clone := request.Clone(request.Context())
		clone.URL = &url.URL{
			Scheme:   target.Scheme,
			Host:     target.Host,
			Path:     "/observed",
			RawQuery: "class=" + url.QueryEscape(origin),
		}
		clone.Host = target.Host
		return transport.delegate.RoundTrip(clone)
	}
	return transport.delegate.RoundTrip(request)
}

func provePR4BrowserBackendEgressGuard(
	t *testing.T,
	transport http.RoundTripper,
	providerTrap, feedTrap, upstreamTrap *pr4BrowserTrafficTrap,
) {
	t.Helper()
	client := &http.Client{Transport: transport}
	for _, target := range []string{
		pr4BrowserProviderTarget + "/v1/chat/completions",
		pr4BrowserFeedTarget + "/v1/completed-executions",
		"https://unclassified-upstream.pr4.invalid/probe",
	} {
		response, err := client.Get(target)
		require.NoError(t, err)
		require.Equal(t, http.StatusTeapot, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
	require.Equal(t, int64(1), providerTrap.count.Load())
	require.Equal(t, int64(1), feedTrap.count.Load())
	require.Equal(t, int64(1), upstreamTrap.count.Load())
}

type pr4BrowserFixture struct {
	BaseURL            string            `json:"base_url"`
	UnavailableBaseURL string            `json:"unavailable_base_url"`
	Password           string            `json:"password"`
	OrgID              int64             `json:"org_id"`
	ProjectA           int64             `json:"project_a"`
	ProjectB           int64             `json:"project_b"`
	ReadyModel         string            `json:"ready_model"`
	RuntimeOnlyModel   string            `json:"runtime_only_model"`
	LegacyRef          string            `json:"legacy_ref"`
	Users              map[string]int64  `json:"users"`
	Usernames          map[string]string `json:"usernames"`
	TrapURLs           map[string]string `json:"trap_urls"`
	ControlTrapURLs    map[string]string `json:"control_trap_urls"`
	EgressTargets      map[string]string `json:"egress_targets"`
	CanonicalFeedPath  string            `json:"canonical_feed_path"`
}

func TestPR4AccessUXModelReadinessBrowserE2E(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	redisAddress := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisAddress == "" {
		redisAddress = "127.0.0.1:6379"
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddress,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	require.NoError(t, redisClient.Ping(ctx).Err(), "PR4 browser acceptance requires Redis 7")
	require.NoError(t, redisClient.FlushDB(ctx).Err(), "clear isolated PR4 browser Redis")
	t.Cleanup(func() { _ = redisClient.FlushDB(context.Background()).Err() })

	providerTrap := newPR4BrowserTrafficTrap(t, "provider")
	feedTrap := newPR4BrowserTrafficTrap(t, "feed")
	upstreamTrap := newPR4BrowserTrafficTrap(t, "upstream")
	controlProviderTrap := newPR4BrowserTrafficTrap(t, "control-provider")
	controlFeedTrap := newPR4BrowserTrafficTrap(t, "control-feed")
	controlUpstreamTrap := newPR4BrowserTrafficTrap(t, "control-upstream")

	backendEgressGuard := newPR4BrowserEgressTransport(t, providerTrap, feedTrap, upstreamTrap)
	previousDefaultTransport := http.DefaultTransport
	http.DefaultTransport = backendEgressGuard
	t.Cleanup(func() { http.DefaultTransport = previousDefaultTransport })
	controlBackendEgressGuard := newPR4BrowserEgressTransport(
		t, controlProviderTrap, controlFeedTrap, controlUpstreamTrap,
	)
	provePR4BrowserBackendEgressGuard(
		t, controlBackendEgressGuard, controlProviderTrap, controlFeedTrap, controlUpstreamTrap,
	)

	fixture := seedPR4BrowserFixture(t, pool, pr4BrowserProviderTarget)
	fixture.TrapURLs = map[string]string{
		"provider": providerTrap.server.URL,
		"feed":     feedTrap.server.URL,
		"upstream": upstreamTrap.server.URL,
	}
	fixture.ControlTrapURLs = map[string]string{
		"provider": controlProviderTrap.server.URL,
		"feed":     controlFeedTrap.server.URL,
		"upstream": controlUpstreamTrap.server.URL,
	}
	fixture.EgressTargets = map[string]string{
		"provider": pr4BrowserProviderTarget,
		"feed":     pr4BrowserFeedTarget,
	}
	fixture.CanonicalFeedPath = pr4BrowserCanonicalFeed
	routing := gateway.NewRouter(pool)

	evaluator, err := modelauthz.NewEvaluator(pool, "development")
	require.NoError(t, err)
	policyService, err := limitpolicy.NewService(pool, redisClient, limitpolicy.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, policyService.Refresh(ctx))

	fixture.BaseURL = newPR4BrowserApplicationServer(t, pool, redisClient, evaluator, policyService, routing)
	fixture.UnavailableBaseURL = newPR4BrowserApplicationServer(t, pool, redisClient, evaluator, policyService, nil)

	repoRoot := pr4BrowserRepositoryRoot(t)
	distIndex := filepath.Join(repoRoot, "frontend", "dist", "index.html")
	if _, err := os.Stat(distIndex); err != nil {
		t.Fatalf("PR4 browser acceptance requires a production frontend build at %s: %v", distIndex, err)
	}
	script := filepath.Join(repoRoot, "frontend", "e2e", "pr4_access_readiness.mjs")
	fixtureJSON, err := json.Marshal(fixture)
	require.NoError(t, err)

	command := exec.Command("node", script)
	command.Dir = filepath.Join(repoRoot, "frontend")
	command.Env = append(os.Environ(), "PR4_BROWSER_FIXTURE="+string(fixtureJSON))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PR4 browser E2E failed: %v\n%s", err, output)
	}
	t.Logf("PR4 browser output:\n%s", output)
	require.Equal(t, int64(2), controlProviderTrap.count.Load(), "backend and browser provider controls must both be observed")
	require.Equal(t, int64(3), controlFeedTrap.count.Load(), "backend, browser external Feed and browser canonical same-origin Feed controls must all be observed")
	require.Equal(t, int64(2), controlUpstreamTrap.count.Load(), "backend and browser upstream controls must both be observed")
	t.Log("PR4_CAUSAL_EGRESS_CONTROLS backend=observed browser=observed provider=2 feed_external=2 feed_same_origin_canonical=1 upstream=2")

	for _, trap := range []*pr4BrowserTrafficTrap{providerTrap, feedTrap, upstreamTrap} {
		require.Zero(t, trap.count.Load(), "%s traffic must remain zero", trap.name)
	}
	t.Log("PR4_EXTERNAL_TRAFFIC provider=0 feed=0 upstream=0")
}

func seedPR4BrowserFixture(t *testing.T, pool *pgxpool.Pool, providerTarget string) pr4BrowserFixture {
	t.Helper()
	ctx := context.Background()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(pr4BrowserPassword), bcrypt.MinCost)
	require.NoError(t, err)

	var orgID, projectA, projectB int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('PR4 Browser Organization','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'PR4 Alpha','pr4-browser-alpha','active') RETURNING id`, orgID).Scan(&projectA))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'PR4 Beta','pr4-browser-beta','active') RETURNING id`, orgID).Scan(&projectB))

	users := make(map[string]int64)
	usernames := map[string]string{
		"org_admin":      "pr4-browser-org-admin",
		"alice":          "pr4-browser-alice",
		"bob":            "pr4-browser-bob",
		"hybrid":         "pr4-browser-hybrid",
		"project_viewer": "pr4-browser-project-viewer",
		"billing":        "pr4-browser-billing",
		"no_membership":  "pr4-browser-no-membership",
		"oidc":           "pr4-browser-oidc",
	}
	seedUser := func(key, organizationRole, compatibilityRole, provider string) {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (
			  org_id,username,password_hash,organization_role,role,status,auth_provider,email
			) VALUES ($1,$2,$3,$4,$5,'active',$6,$7) RETURNING id`,
			orgID, usernames[key], string(passwordHash), organizationRole, compatibilityRole, provider,
			usernames[key]+"@example.test").Scan(&id))
		users[key] = id
	}
	seedUser("org_admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin, "local")
	seedUser("alice", auth.RoleMember, auth.RoleDeveloper, "local")
	seedUser("bob", auth.RoleMember, auth.RoleDeveloper, "local")
	seedUser("hybrid", auth.RoleMember, auth.RoleProjectAdmin, "local")
	seedUser("project_viewer", auth.RoleMember, auth.RoleViewer, "local")
	seedUser("billing", auth.RoleBillingAdmin, auth.RoleBillingAdmin, "local")
	seedUser("no_membership", auth.RoleMember, auth.RoleDeveloper, "local")
	seedUser("oidc", auth.RoleMember, auth.RoleDeveloper, "oidc")

	for _, membership := range []struct {
		projectID int64
		userID    int64
		role      string
	}{
		{projectA, users["alice"], auth.ProjectRoleUser},
		{projectA, users["bob"], auth.ProjectRoleUser},
		{projectA, users["hybrid"], auth.ProjectRoleAdmin},
		{projectB, users["hybrid"], auth.ProjectRoleUser},
		{projectA, users["project_viewer"], auth.ProjectRoleViewer},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,$3)`,
			membership.projectID, membership.userID, membership.role)
		require.NoError(t, err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO model_registry (
		  org_id,model_name,display_name,provider_family,lifecycle,pricing_model_name,approved_catalog
		) VALUES ($1,$2,'PR4 Browser Ready','test','ga',$2,TRUE)`, orgID, pr4BrowserReadyModel)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO model_prices (model_name,input_price_cents,output_price_cents)
		VALUES ($1,1,2)`, pr4BrowserReadyModel)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros)
		VALUES ($1,1000000,0,0,0)`, orgID)
	require.NoError(t, err)
	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status,circuit_state)
		VALUES ('pr4-browser-safe-aggregate','test',$1,$2,'active','healthy') RETURNING id`,
		providerTarget, pr4BrowserReadyModel+","+pr4BrowserRuntimeModel).Scan(&channelID))
	_, err = pool.Exec(ctx, `
		INSERT INTO channel_keys (channel_id,key_enc,status,allowed_models)
		VALUES ($1,'pr4-browser-never-decrypted','active',$2)`,
		channelID, pr4BrowserReadyModel+","+pr4BrowserRuntimeModel)
	require.NoError(t, err)

	const legacyRef = "app_pr4_browser_legacy"
	_, err = pool.Exec(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,
		  display_name,kind,environment,identity_mode,client_profile,status,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v1','unirouter.workload-identity.v1',$1,$2,$3,
		  'PR4 Browser Legacy','interactive_client','development','personal_development',
		  'client.openai-sdk.chat.v1','active',$4,$4
		)`, orgID, projectA, legacyRef, users["org_admin"])
	require.NoError(t, err)

	return pr4BrowserFixture{
		Password:         pr4BrowserPassword,
		OrgID:            orgID,
		ProjectA:         projectA,
		ProjectB:         projectB,
		ReadyModel:       pr4BrowserReadyModel,
		RuntimeOnlyModel: pr4BrowserRuntimeModel,
		LegacyRef:        legacyRef,
		Users:            users,
		Usernames:        usernames,
	}
}

func newPR4BrowserApplicationServer(
	t *testing.T,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	evaluator *modelauthz.Evaluator,
	policyService *limitpolicy.Service,
	routing *gateway.Router,
) string {
	t.Helper()
	handler := admin.NewHandlerWithAPIKeyHashSecret(
		pool, pr4BrowserJWTSecret, pr4BrowserAPIKeySecret,
		[]byte(pr4BrowserEncryptionKey), nil,
	)
	handler.SetRedisClient(redisClient)
	handler.SetModelAuthorizationEvaluator(evaluator)
	handler.SetLimitPolicyReloader(policyService)
	if routing != nil {
		handler.SetRoutingController(routing)
	}
	licenseRuntime := licensing.NewRuntime(nil)
	handler.SetLicenseRuntime(licenseRuntime)

	adminAuth := middleware.AdminAuth(pool, pr4BrowserJWTSecret)
	adminCustomerContext := middleware.CustomerContext(pool)
	authenticated := func(next http.HandlerFunc) http.Handler {
		return adminAuth(adminCustomerContext(http.HandlerFunc(next)))
	}
	organizationAdmin := func(next http.HandlerFunc) http.Handler {
		return adminAuth(adminCustomerContext(middleware.RequireOrganizationRoles(
			auth.RolePlatformAdmin,
			auth.RoleOrgAdmin,
		)(http.HandlerFunc(next))))
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/v1/auth/login", handler.Login)
	apiMux.HandleFunc("POST /api/v1/auth/logout", handler.Logout)
	apiMux.Handle("GET /api/v1/auth/oidc/config", licenseRuntime.Gate(licensing.FeatureOIDC)(http.HandlerFunc(handler.OIDCConfig)))
	apiMux.Handle("GET /api/v1/me", authenticated(handler.GetMe))
	apiMux.Handle("GET /api/v1/organizations", authenticated(handler.ListOrganizations))
	apiMux.Handle("GET /api/v1/projects", authenticated(handler.ListProjects))
	apiMux.Handle("GET /api/v1/users", organizationAdmin(handler.ListUsers))
	apiMux.Handle("POST /api/v1/users", organizationAdmin(handler.CreateUser))
	apiMux.Handle("PUT /api/v1/users/{id}", organizationAdmin(handler.UpdateUser))
	apiMux.Handle("DELETE /api/v1/users/{id}", organizationAdmin(handler.DeleteUser))
	registerWorkloadApplicationRoutes(apiMux, handler, authenticated)
	registerAccessProvisioningRoutes(apiMux, handler, authenticated, organizationAdmin)

	apiHandler := middleware.SecurityHeaders(
		middleware.OperationIdentityWithSecret(pr4BrowserAPIKeySecret)(apiMux),
	)
	distDir := filepath.Join(pr4BrowserRepositoryRoot(t), "frontend", "dist")
	server := httptest.NewServer(pr4BrowserSPAHandler(distDir, apiHandler))
	t.Cleanup(server.Close)
	return server.URL
}

func pr4BrowserSPAHandler(distDir string, api http.Handler) http.Handler {
	fileServer := http.FileServer(http.Dir(distDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			api.ServeHTTP(w, r)
			return
		}
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		candidate := filepath.Join(distDir, cleanPath)
		if cleanPath != "." && strings.HasPrefix(candidate, distDir+string(os.PathSeparator)) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})
}

func pr4BrowserRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolve PR4 browser test source path")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
