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
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
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
	modelBrowserConnection    = "Model Browser DeepSeek"
	modelBrowserPublicModel   = "company-deepseek-flash"
	modelBrowserUpstreamModel = "deepseek-v4-flash"
	modelBrowserProviderKey   = "provider-model-browser-secret"
	modelBrowserEncryptionKey = "0123456789abcdef01234567"
	modelBrowserContextLimit  = 128000
	modelBrowserOutputLimit   = 8192
	modelBrowserTargetOutput  = 4096
)

type modelBrowserFixture struct {
	BaseURL       string `json:"base_url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	ProviderURL   string `json:"provider_url"`
	ProviderKey   string `json:"provider_key"`
	PublicModel   string `json:"public_model"`
	UpstreamModel string `json:"upstream_model"`
}

type modelBrowserProviderObservation struct {
	Method        string
	RequestURI    string
	ContentType   string
	Authorization string
	Model         string
}

func TestModelEndpointOnboardingBrowserE2E(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var providerCalls atomic.Int64
	var observationsMu sync.Mutex
	var providerObservations []modelBrowserProviderObservation
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		observation := modelBrowserProviderObservation{
			Method: r.Method, RequestURI: r.URL.RequestURI(),
			ContentType: r.Header.Get("Content-Type"), Authorization: r.Header.Get("Authorization"),
		}
		defer func() {
			observationsMu.Lock()
			providerObservations = append(providerObservations, observation)
			observationsMu.Unlock()
		}()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RequestURI() != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if observation.ContentType != "application/json" {
			http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
			return
		}
		if observation.Authorization != "Bearer "+modelBrowserProviderKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		observation.Model = payload.Model
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

	require.Equal(t, int64(1), providerCalls.Load(), "only the exact canonical operation probe reaches the provider")
	observationsMu.Lock()
	observations := append([]modelBrowserProviderObservation(nil), providerObservations...)
	observationsMu.Unlock()
	require.Equal(t, []modelBrowserProviderObservation{{
		Method: http.MethodPost, RequestURI: "/v1/chat/completions",
		ContentType: "application/json", Authorization: "Bearer " + modelBrowserProviderKey,
		Model: modelBrowserUpstreamModel,
	}}, observations)

	var publicModelID, deploymentID int64
	var publicContext, publicOutput, deploymentContext, deploymentOutput int
	var catalogLifecycle, operationEvidence string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT registry.id,deployment.id,registry.context_length,registry.output_limit,
		       deployment.max_context,deployment.default_max_output,registry.lifecycle,
		       operation.evidence_state
		  FROM model_registry registry
		  JOIN model_deployments deployment
		    ON deployment.public_model_id=registry.id AND deployment.customer_org_id=registry.org_id
		  JOIN provider_connections connection ON connection.id=deployment.connection_id
		  JOIN model_deployment_operations operation
		    ON operation.deployment_id=deployment.id AND operation.operation_id=$3
		 WHERE registry.org_id=$1 AND registry.model_name=$2
		   AND connection.name=$4 AND deployment.upstream_model=$5`,
		customerOrgID, modelBrowserPublicModel, modeloperation.IDChatCompletions,
		modelBrowserConnection, modelBrowserUpstreamModel,
	).Scan(
		&publicModelID, &deploymentID, &publicContext, &publicOutput,
		&deploymentContext, &deploymentOutput, &catalogLifecycle, &operationEvidence,
	))
	require.Equal(t, "ga", catalogLifecycle)
	require.Equal(t, modelBrowserContextLimit, publicContext)
	require.Equal(t, modelBrowserOutputLimit, publicOutput)
	require.Equal(t, modelBrowserContextLimit, deploymentContext)
	require.Equal(t, modelBrowserTargetOutput, deploymentOutput)
	require.Equal(t, "live_probed", operationEvidence)

	var exactProbeCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM model_deployment_probes
		 WHERE deployment_id=$1 AND operation_id=$2 AND success`,
		deploymentID, modeloperation.IDChatCompletions).Scan(&exactProbeCount))
	require.Equal(t, 1, exactProbeCount)

	var releaseID, releaseVersion int64
	var releaseRef, releaseStage string
	var routeItemsCount int
	var routeItemsSealedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id,release_ref,stage,version,route_items_count,route_items_sealed_at
		  FROM runtime_change_releases
		 WHERE public_model_id=$1
		   AND candidate_version='unirouter.runtime-change-candidate.v2'
		 ORDER BY id DESC LIMIT 1`, publicModelID).Scan(
		&releaseID, &releaseRef, &releaseStage, &releaseVersion,
		&routeItemsCount, &routeItemsSealedAt,
	))
	require.NotEmpty(t, releaseRef)
	require.Equal(t, "active", releaseStage)
	require.Equal(t, int64(3), releaseVersion)
	require.Equal(t, 1, routeItemsCount)
	require.False(t, routeItemsSealedAt.IsZero())

	var exactRouteItems int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM runtime_route_revision_items
		 WHERE release_id=$1 AND deployment_id=$2 AND operation_id=$3
		   AND source_candidate_version='unirouter.runtime-change-candidate.v2'`,
		releaseID, deploymentID, modeloperation.IDChatCompletions).Scan(&exactRouteItems))
	require.Equal(t, 1, exactRouteItems)

	var slotPublicModelID, stableReleaseID, slotVersion int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT public_model_id,stable_release_id,slot_version
		  FROM runtime_change_slots WHERE public_model_id=$1`, publicModelID).Scan(
		&slotPublicModelID, &stableReleaseID, &slotVersion,
	))
	require.Equal(t, publicModelID, slotPublicModelID)
	require.Equal(t, releaseID, stableReleaseID)
	require.Equal(t, int64(1), slotVersion)

	var authoringReceiptCount, lifecycleReceiptCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM model_deployment_release_authoring_receipts receipt
		 JOIN audit_logs audit ON audit.id=receipt.audit_log_id
		 WHERE receipt.customer_org_id=$1 AND receipt.release_id=$2
		   AND receipt.status='completed'
		   AND audit.action='model_deployment.release_authoring.create'`,
		customerOrgID, releaseID).Scan(&authoringReceiptCount))
	require.Equal(t, 1, authoringReceiptCount)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM model_deployment_lifecycle_receipts receipt
		 JOIN audit_logs audit ON audit.id=receipt.audit_log_id
		 WHERE receipt.customer_org_id=$1 AND receipt.target_release_id=$2
		   AND receipt.status='completed' AND receipt.action='activate_initial'
		   AND audit.action='model_deployment.lifecycle.activate_initial'`,
		customerOrgID, releaseID).Scan(&lifecycleReceiptCount))
	require.Equal(t, 1, lifecycleReceiptCount)

	var operatorAuditRows, operatorAuditActions int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*),COUNT(DISTINCT action) FROM audit_logs
		 WHERE user_id IN (SELECT id FROM users WHERE org_id=$1)
		   AND action IN (
		     'model_deployment.create','model_deployment.operation_probe',
		     'model_deployment.release_authoring.create','model_deployment.lifecycle.activate_initial'
		   )`, operatorOrgID).
		Scan(&operatorAuditRows, &operatorAuditActions))
	require.Equal(t, 4, operatorAuditRows)
	require.Equal(t, 4, operatorAuditActions)

	var createEvents, validateEvents, activateEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE action='create'),
		       COUNT(*) FILTER (WHERE action='validate'),
		       COUNT(*) FILTER (WHERE action='promote' AND detail->>'initial_activation'='true')
		  FROM runtime_change_events WHERE release_id=$1`, releaseID).Scan(
		&createEvents, &validateEvents, &activateEvents,
	))
	require.Equal(t, 1, createEvents)
	require.Equal(t, 1, validateEvents)
	require.Equal(t, 1, activateEvents)
}

func seedModelBrowserFixture(t *testing.T, pool *pgxpool.Pool, providerURL string) (modelBrowserFixture, int64, int64) {
	t.Helper()
	ctx := context.Background()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(modelBrowserPassword), bcrypt.MinCost)
	require.NoError(t, err)

	var operatorOrgID, customerOrgID, operatorID int64
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

	return modelBrowserFixture{
		Username: modelBrowserUsername, Password: modelBrowserPassword,
		ProviderURL: providerURL, ProviderKey: modelBrowserProviderKey,
		PublicModel: modelBrowserPublicModel, UpstreamModel: modelBrowserUpstreamModel,
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
	organizationRoleRoute := func(next http.HandlerFunc, roles ...string) http.Handler {
		return adminAuth(customerContext(middleware.RequireOrganizationRoles(roles...)(http.HandlerFunc(next))))
	}
	platformRoute := func(next http.HandlerFunc) http.Handler {
		return organizationRoleRoute(next, auth.RolePlatformAdmin)
	}
	orgAdminRoute := func(next http.HandlerFunc) http.Handler {
		return organizationRoleRoute(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	}
	licensedRoute := func(feature string, route func(http.HandlerFunc) http.Handler) func(http.HandlerFunc) http.Handler {
		return func(next http.HandlerFunc) http.Handler {
			gated := licenseRuntime.Gate(feature)(http.HandlerFunc(next))
			return route(http.HandlerFunc(gated.ServeHTTP))
		}
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/v1/auth/login", handler.Login)
	apiMux.HandleFunc("POST /api/v1/auth/logout", handler.Logout)
	apiMux.HandleFunc("GET /api/v1/auth/oidc/config", handler.OIDCConfig)
	apiMux.Handle("GET /api/v1/me", authenticated(handler.GetMe))
	apiMux.Handle("GET /api/v1/organizations", authenticated(handler.ListOrganizations))
	apiMux.Handle("GET /api/v1/model-execution-profiles", licensedRoute(licensing.FeatureModelGovernance, func(next http.HandlerFunc) http.Handler {
		return organizationRoleRoute(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer)
	})(handler.ListModelExecutionProfiles))
	registerModelDeploymentRoutes(apiMux, handler, modelDeploymentRouteGuards{
		read: func(next http.HandlerFunc) http.Handler {
			return organizationRoleRoute(next, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer)
		},
		write:   licensedRoute(licensing.FeatureGateway, orgAdminRoute),
		approve: licensedRoute(licensing.FeatureGateway, platformRoute),
		safety:  orgAdminRoute,
	})

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
		Features: []string{licensing.FeatureGateway, licensing.FeatureModelGovernance},
		Limits:   map[string]int64{licensing.LimitActiveChannels: 10, licensing.LimitActiveAPIKeys: 10, licensing.LimitActiveProjects: 10, licensing.LimitActiveUsers: 10},
	}, "model-browser-key", privateKey)
	require.NoError(t, err)
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	status := runtime.Replace(raw)
	require.True(t, status.Valid && status.Enforceable, "test license must be enforceable: %#v", status)
	return runtime
}
