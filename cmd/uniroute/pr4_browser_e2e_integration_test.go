//go:build integration && pr4browser

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const (
	pr4BrowserJWTSecret     = "pr4-browser-jwt-secret-32-bytes"
	pr4BrowserAPIKeySecret  = "pr4-browser-api-key-secret-32-bytes"
	pr4BrowserPassword      = "PR4-browser-password-2026"
	pr4BrowserReadyModel    = "pr4-browser-ready-model"
	pr4BrowserRuntimeModel  = "pr4-browser-runtime-only"
	pr4BrowserEncryptionKey = "0123456789abcdef01234567"
)

type pr4BrowserRoutingPreview struct {
	previews map[string]routingpolicy.Preview
}

func (p *pr4BrowserRoutingPreview) Preview(_ context.Context, model string) (routingpolicy.Preview, error) {
	preview := p.previews[model]
	preview.Model = model
	return preview, nil
}

func (*pr4BrowserRoutingPreview) Invalidate() {}

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

	fixture := seedPR4BrowserFixture(t, pool)
	fixture.TrapURLs = map[string]string{
		"provider": providerTrap.server.URL,
		"feed":     feedTrap.server.URL,
		"upstream": upstreamTrap.server.URL,
	}

	routing := &pr4BrowserRoutingPreview{previews: map[string]routingpolicy.Preview{
		pr4BrowserReadyModel: {
			PolicyVersion: "unirouter.routing-policy.v1",
			Candidates: []routingpolicy.PreviewCandidate{{
				ChannelID: 1, ChannelName: "pr4-browser-safe-aggregate", EligibleKeyCount: 1,
			}},
		},
		pr4BrowserRuntimeModel: {
			PolicyVersion: "unirouter.routing-policy.v1",
			Candidates: []routingpolicy.PreviewCandidate{{
				ChannelID: 1, ChannelName: "pr4-browser-safe-aggregate", EligibleKeyCount: 1,
			}},
		},
	}}

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

	for _, trap := range []*pr4BrowserTrafficTrap{providerTrap, feedTrap, upstreamTrap} {
		require.Zero(t, trap.count.Load(), "%s traffic must remain zero", trap.name)
	}
	t.Log("PR4_EXTERNAL_TRAFFIC provider=0 feed=0 upstream=0")
}

func seedPR4BrowserFixture(t *testing.T, pool *pgxpool.Pool) pr4BrowserFixture {
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
	routing *pr4BrowserRoutingPreview,
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
	authenticated := func(next http.HandlerFunc) http.Handler {
		return adminAuth(http.HandlerFunc(next))
	}
	organizationAdmin := func(next http.HandlerFunc) http.Handler {
		return adminAuth(middleware.RequireOrganizationRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin)(http.HandlerFunc(next)))
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
		if strings.HasPrefix(r.URL.Path, "/api/") {
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
