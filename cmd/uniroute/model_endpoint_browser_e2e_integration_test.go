//go:build integration && pr4browser

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const (
	modelBrowserJWTSecret     = "model-browser-jwt-secret-32-bytes"
	modelBrowserGatewaySecret = "model-browser-gateway-secret-32-bytes"
	modelBrowserPassword      = "Model-browser-password-2026"
	modelBrowserUsername      = "model-browser-operator"
	modelBrowserPublicModel   = "company-deepseek-flash"
	modelBrowserUpstreamModel = "deepseek-v4-flash"
	modelBrowserProviderKey   = "provider-model-browser-secret"
	modelBrowserEncryptionKey = "0123456789abcdef01234567"
)

type modelBrowserFixture struct {
	BaseURL       string `json:"base_url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	ProviderURL   string `json:"provider_url"`
	ProviderKey   string `json:"provider_key"`
	PublicModel   string `json:"public_model"`
	UpstreamModel string `json:"upstream_model"`
	GatewayAPIKey string `json:"gateway_api_key"`
}

func TestModelEndpointOnboardingBrowserE2E(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var providerCalls atomic.Int64
	var observationsMu sync.Mutex
	var observedModels, observedAuth []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		var payload struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		observationsMu.Lock()
		observedModels = append(observedModels, payload.Model)
		observedAuth = append(observedAuth, r.Header.Get("Authorization"))
		observationsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-model-browser","object":"chat.completion","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"golden-path-ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`))
	}))
	t.Cleanup(upstream.Close)

	fixture, operatorOrgID, customerOrgID := seedModelBrowserFixture(t, pool, upstream.URL+"/v1")

	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)

	fixture.BaseURL = newModelBrowserApplicationServer(t, pool, router, manager)
	repoRoot := pr4BrowserRepositoryRoot(t)
	distIndex := filepath.Join(repoRoot, "frontend", "dist", "index.html")
	if _, err := os.Stat(distIndex); err != nil {
		t.Fatalf("model browser acceptance requires a production frontend build at %s: %v", distIndex, err)
	}
	fixtureJSON, err := json.Marshal(fixture)
	require.NoError(t, err)
	command := exec.Command("node", filepath.Join(repoRoot, "frontend", "e2e", "model_endpoint_onboarding.mjs"))
	command.Dir = filepath.Join(repoRoot, "frontend")
	command.Env = append(os.Environ(), "MODEL_BROWSER_FIXTURE="+string(fixtureJSON))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("model endpoint browser E2E failed: %v\n%s", err, output)
	}
	t.Logf("model endpoint browser output:\n%s", output)

	require.Equal(t, int64(2), providerCalls.Load(), "one live probe and one gateway call must reach the selected provider")
	observationsMu.Lock()
	require.Equal(t, []string{modelBrowserUpstreamModel, modelBrowserUpstreamModel}, observedModels)
	require.Equal(t, []string{"Bearer " + modelBrowserProviderKey, "Bearer " + modelBrowserProviderKey}, observedAuth)
	observationsMu.Unlock()

	preview, err := router.Preview(ctx, modelBrowserPublicModel)
	require.NoError(t, err)
	require.Len(t, preview.Candidates, 1)
	route, err := router.Route(ctx, modelBrowserPublicModel)
	require.NoError(t, err)
	require.Equal(t, modelBrowserUpstreamModel, route.MappedModel)

	var catalogApproved bool
	var catalogLifecycle string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT approved_catalog,lifecycle FROM model_registry
		 WHERE org_id=$1 AND model_name=$2`, customerOrgID, modelBrowserPublicModel).Scan(&catalogApproved, &catalogLifecycle))
	require.True(t, catalogApproved)
	require.Equal(t, "ga", catalogLifecycle)

	var callCount, totalTokens int
	var requestedModel, routedModel, channel string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*),COALESCE(MAX(requested_model),''),COALESCE(MAX(model),''),
		       COALESCE(MAX(channel),''),COALESCE(MAX(total_tokens),0)
		  FROM call_logs
		 WHERE org_id=$1 AND requested_model=$2`, customerOrgID, modelBrowserPublicModel).
		Scan(&callCount, &requestedModel, &routedModel, &channel, &totalTokens))
	require.Equal(t, 1, callCount)
	require.Equal(t, modelBrowserPublicModel, requestedModel)
	require.Equal(t, modelBrowserPublicModel, routedModel)
	require.Equal(t, "Model Browser DeepSeek", channel)
	require.Equal(t, 10, totalTokens)

	var operatorAuditRows int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs
		 WHERE user_id IN (SELECT id FROM users WHERE org_id=$1)
		   AND action IN ('channel.create','channel.test','model_endpoint.activate_initial')`, operatorOrgID).
		Scan(&operatorAuditRows))
	require.GreaterOrEqual(t, operatorAuditRows, 3)
}

func seedModelBrowserFixture(t *testing.T, pool *pgxpool.Pool, providerURL string) (modelBrowserFixture, int64, int64) {
	t.Helper()
	ctx := context.Background()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(modelBrowserPassword), bcrypt.MinCost)
	require.NoError(t, err)

	var operatorOrgID, customerOrgID, operatorID, customerUserID, projectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('Model Browser Operations','active','partner') RETURNING id`).Scan(&operatorOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('Model Browser Customer','active','customer') RETURNING id`).Scan(&customerOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status,auth_provider)
		VALUES ($1,$2,$3,'platform_admin','platform_admin','active','local') RETURNING id`,
		operatorOrgID, modelBrowserUsername, string(passwordHash)).Scan(&operatorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status,auth_provider)
		VALUES ($1,'model-browser-customer-user',$2,'member','developer','active','local') RETURNING id`,
		customerOrgID, string(passwordHash)).Scan(&customerUserID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,created_by)
		VALUES ($1,'Model Browser Project','model-browser-project','active',$2) RETURNING id`,
		customerOrgID, customerUserID).Scan(&projectID))
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, customerUserID)
	require.NoError(t, err)

	testutil.NewTestWalletCents(t, pool, customerOrgID, 10000)
	testutil.NewTestModelPrice(t, pool, modelBrowserPublicModel, 2, 10)

	rawGatewayKey, gatewayPrefix, err := auth.GenerateAPIKey(customerOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  rpm_limit,qps_limit,allowed_models,allowed_endpoints,purpose
		) VALUES ($1,$2,$3,'personal','model browser gateway key',$4,$5,'active',60,10,$6,'/v1/chat/completions','')`,
		customerOrgID, projectID, customerUserID, auth.HashAPIKey(rawGatewayKey, modelBrowserGatewaySecret), gatewayPrefix, modelBrowserPublicModel)
	require.NoError(t, err)

	return modelBrowserFixture{
		Username: modelBrowserUsername, Password: modelBrowserPassword,
		ProviderURL: providerURL, ProviderKey: modelBrowserProviderKey,
		PublicModel: modelBrowserPublicModel, UpstreamModel: modelBrowserUpstreamModel,
		GatewayAPIKey: rawGatewayKey,
	}, operatorOrgID, customerOrgID
}

func newModelBrowserApplicationServer(t *testing.T, pool *pgxpool.Pool, router *gateway.Router, manager *runtimechange.Manager) string {
	t.Helper()
	encKey := []byte(modelBrowserEncryptionKey)
	handler := admin.NewHandlerWithAPIKeyHashSecret(pool, modelBrowserJWTSecret, modelBrowserGatewaySecret, encKey, nil)
	handler.SetRoutingController(router)
	handler.SetRuntimeChangeManager(manager)
	licenseRuntime := modelBrowserLicenseRuntime(t)
	handler.SetLicenseRuntime(licenseRuntime)

	adminAuth := middleware.AdminAuth(pool, modelBrowserJWTSecret)
	customerContext := middleware.CustomerContext(pool)
	authenticated := func(next http.HandlerFunc) http.Handler {
		return adminAuth(customerContext(http.HandlerFunc(next)))
	}
	platform := func(next http.HandlerFunc) http.Handler {
		return adminAuth(customerContext(middleware.RequireOrganizationRoles(auth.RolePlatformAdmin)(http.HandlerFunc(next))))
	}
	licensedPlatform := func(next http.HandlerFunc) http.Handler {
		return licenseRuntime.Gate(licensing.FeatureGateway)(platform(next))
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/v1/auth/login", handler.Login)
	apiMux.HandleFunc("POST /api/v1/auth/logout", handler.Logout)
	apiMux.HandleFunc("GET /api/v1/auth/oidc/config", handler.OIDCConfig)
	apiMux.Handle("GET /api/v1/me", authenticated(handler.GetMe))
	apiMux.Handle("GET /api/v1/organizations", authenticated(handler.ListOrganizations))
	apiMux.Handle("GET /api/v1/model-endpoints", platform(handler.ListModelEndpoints))
	apiMux.Handle("POST /api/v1/channels", platform(handler.CreateChannel))
	apiMux.Handle("POST /api/v1/channels/{id}/test", licensedPlatform(handler.TestChannel))
	apiMux.Handle("POST /api/v1/model-endpoints/activate", licensedPlatform(handler.ActivateModelEndpoint))

	relay := gateway.NewRelay(pool, nil, router, billing.NewBilling(pool, nil), filter.NewDFAFilter(nil), encKey)
	apiMux.Handle("POST /v1/chat/completions", middleware.GatewayAuth(pool, modelBrowserGatewaySecret)(newGatewayChatHandler(pool, relay)))

	apiHandler := middleware.SecurityHeaders(middleware.OperationIdentityWithSecret(modelBrowserGatewaySecret)(apiMux))
	distDir := filepath.Join(pr4BrowserRepositoryRoot(t), "frontend", "dist")
	server := httptest.NewServer(pr4BrowserSPAHandler(distDir, apiHandler))
	t.Cleanup(server.Close)
	return server.URL
}

func modelBrowserLicenseRuntime(t *testing.T) *licensing.Runtime {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	verifier := licensing.NewCommercialVerifier(map[string]ed25519.PublicKey{"model-browser-key": publicKey}, nil, 30, "model-browser")
	runtime := licensing.NewRuntime(verifier)
	now := time.Now().UTC()
	document, err := licensing.SignCommercialPayload(licensing.Payload{
		LicenseID: "license-model-browser", Customer: "Model Browser Customer", DeploymentID: "model-browser",
		Tier: "enterprise", IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339),
		Features: []string{licensing.FeatureGateway},
		Limits:   map[string]int64{licensing.LimitActiveChannels: 10, licensing.LimitActiveAPIKeys: 10, licensing.LimitActiveProjects: 10, licensing.LimitActiveUsers: 10},
	}, "model-browser-key", privateKey)
	require.NoError(t, err)
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	status := runtime.Replace(raw)
	require.True(t, status.Valid && status.Enforceable, "test license must be enforceable: %#v", status)
	return runtime
}
