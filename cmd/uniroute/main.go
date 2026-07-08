package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/oidc"
	"github.com/uniroute/unirouter/internal/redisx"
)

// buildVersion and buildCommit are set by release builds through -ldflags.
var (
	buildVersion string
	buildCommit  string
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	applyBuildRuntimeMetadata(cfg)
	if handled, err := runCLI(context.Background(), os.Args[1:], cfg); handled {
		if err != nil {
			log.Fatalf("Command failed: %v", err)
		}
		return
	}

	// Initialize PostgreSQL
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer postgres.Close()
	log.Println("Connected to PostgreSQL")

	// Run migrations (skip for canary instances sharing the same DB)
	if cfg.SkipMigration {
		log.Println("SKIP_MIGRATION=true, skipping database migrations")
	} else {
		migrationsDir := cfg.MigrationsDir
		if migrationsDir == "" {
			migrationsDir = findMigrationsDir()
		}
		if err := migrate.Run(context.Background(), postgres.Pool, migrationsDir); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		log.Println("Migrations complete")
	}

	// Initialize Redis
	redisClient, err := redisx.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisMaxConns)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()
	log.Println("Connected to Redis")

	// Initialize core components
	router := gateway.NewRouter(postgres.Pool)
	billingSvc := billing.NewBilling(postgres.Pool, redisClient.Client)
	admissionCtrl := middleware.NewDefaultAdmissionController()
	sensitiveFilter := filter.NewDFAFilter([]string{})
	encKey := []byte(cfg.EncryptionKey)
	relay := gateway.NewRelay(postgres.Pool, redisClient.Client, router, billingSvc, sensitiveFilter, encKey)
	relay.SetTraceEvents(cfg.TraceEventsEnabled, cfg.TraceEventSampleRate)
	relay.SetPayloadCaptureMode(cfg.PayloadCaptureMode)
	alertSvc := billing.NewAlertService(postgres.Pool)
	eventSvc := billing.NewEventService(postgres.Pool)
	relay.SetEventService(eventSvc)

	// Wire alert service into relay for post-settle budget checks
	go func() {
		time.Sleep(2 * time.Second)
		alertCtx := context.Background()
		webhookURL, budgetCents := loadAlertConfig(alertCtx, postgres.Pool)
		if webhookURL != "" && budgetCents > 0 {
			relay.SetAlertService(alertSvc, webhookURL, budgetCents)
		}
	}()

	adminHandler := admin.NewHandlerWithAPIKeyHashSecret(postgres.Pool, cfg.JWTSecret, cfg.APIKeyHashSecret, encKey, sensitiveFilter)
	licenseVerifier, err := licensing.NewVerifierFromPublicKey(cfg.LicensePublicKey, cfg.LicenseExpiryWarningDays)
	if err != nil {
		log.Fatalf("Invalid license configuration: %v", err)
	}
	adminHandler.SetLicenseVerifier(licenseVerifier)
	adminHandler.SetEventService(eventSvc)
	adminHandler.SetRedisClient(redisClient.Client)
	adminHandler.SetRuntimeStatusOptions(admin.RuntimeStatusOptions{
		Environment:    cfg.RuntimeEnvironment,
		DeploymentMode: cfg.RuntimeDeploymentMode,
		DeploymentRole: cfg.RuntimeDeploymentRole,
		Version:        cfg.RuntimeVersion,
		Commit:         cfg.RuntimeCommit,
		PrometheusURL:  cfg.PrometheusURL,
		GrafanaURL:     cfg.GrafanaURL,
	})
	oidcService, err := oidc.NewService(oidc.Config{
		Enabled:            cfg.OIDCEnabled,
		IssuerURL:          cfg.OIDCIssuerURL,
		ClientID:           cfg.OIDCClientID,
		ClientSecret:       cfg.OIDCClientSecret,
		RedirectURL:        cfg.OIDCRedirectURL,
		ProviderName:       cfg.OIDCProviderName,
		Scopes:             cfg.OIDCScopes,
		AllowedEmailDomain: cfg.OIDCAllowedEmailDomain,
		AutoProvision:      cfg.OIDCAutoProvision,
		DefaultOrgID:       cfg.OIDCDefaultOrgID,
		DefaultRole:        cfg.OIDCDefaultRole,
		DefaultProjectSlug: cfg.OIDCDefaultProjectSlug,
		UsernameClaim:      cfg.OIDCUsernameClaim,
		EmailClaim:         cfg.OIDCEmailClaim,
		RoleClaim:          cfg.OIDCRoleClaim,
		OrgIDClaim:         cfg.OIDCOrgIDClaim,
	})
	if err != nil {
		log.Fatalf("Invalid OIDC configuration: %v", err)
	}
	adminHandler.SetOIDCService(oidcService)

	// Load sensitive words from settings and periodically refresh all instances.
	go startSensitiveWordsReloadLoop(postgres.Pool, sensitiveFilter, time.Minute)

	// Start cleanup goroutine
	if cfg.LogRetentionDays > 0 || cfg.TraceEventRetentionDays > 0 {
		go startCleanupLoop(adminHandler, cfg.LogRetentionDays, cfg.TraceEventRetentionDays)
	}

	// Start budget alert goroutine — checks every 5 minutes
	go startBudgetAlertLoop(alertSvc, postgres.Pool)
	go startNotificationRetryLoop(eventSvc)

	// Build HTTP mux
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","service":"uniroute"}`)
	})

	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := postgres.Health(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"not_ready","error":"database"}`)
			return
		}
		if err := redisClient.Health(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"not_ready","error":"redis"}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","db":"ok","redis":"ok"}`)
	})

	// Root endpoint
	mux.HandleFunc("GET /", rootHandler)

	// Prometheus metrics endpoint. It remains registered for contract parity, but
	// can be disabled or CIDR-restricted by runtime exposure policy.
	metricsEndpoint := promhttp.Handler()
	if !cfg.MetricsEnabled {
		metricsEndpoint = http.NotFoundHandler()
	}
	if len(cfg.MetricsAllowedCIDRs) > 0 {
		metricsEndpoint = middleware.MetricsCIDRAllowlist(cfg.MetricsAllowedCIDRs)(metricsEndpoint)
	}
	mux.Handle("GET /metrics", metricsEndpoint)

	// Gateway endpoints (with auth + rate limiting)
	gatewayAuth := middleware.GatewayAuthWithAPIKeyHashSecret(postgres.Pool, cfg.APIKeyHashSecret)
	rateLimiter := middleware.RateLimit(redisClient.Client)
	admissionMiddleware := middleware.AdmissionFromContext(admissionCtrl)

	chatHandler := newGatewayChatHandler(postgres.Pool, relay)
	embeddingsHandler := newGatewayEmbeddingsHandler(postgres.Pool, relay)

	mux.Handle("POST /v1/chat/completions", gatewayAuth(admissionMiddleware(rateLimiter(chatHandler))))
	mux.Handle("POST /v1/embeddings", gatewayAuth(admissionMiddleware(rateLimiter(embeddingsHandler))))
	mux.Handle("GET /v1/models", gatewayAuth(admissionMiddleware(http.HandlerFunc(relay.HandleModels))))

	// Admin auth middleware
	adminAuth := middleware.AdminAuth(cfg.JWTSecret)
	adminRoute := func(h http.HandlerFunc) http.Handler {
		return adminAuth(http.HandlerFunc(h))
	}
	roleRoute := func(h http.HandlerFunc, roles ...string) http.Handler {
		return adminAuth(middleware.RequireRoles(roles...)(http.HandlerFunc(h)))
	}
	platformRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin)
	}
	orgAdminRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	}
	billingRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleBillingAdmin)
	}
	usageRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)
	}
	notificationReadRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin)
	}
	notificationWriteRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin)
	}
	projectReadRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)
	}
	projectWriteRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper)
	}

	loginRateLimiter := middleware.LoginRateLimit(redisClient.Client, middleware.LoginRateLimitOptions{
		Enabled:      cfg.LoginRateLimitEnabled,
		Window:       time.Duration(cfg.LoginRateLimitWindowSec) * time.Second,
		IPRequests:   cfg.LoginRateLimitIP,
		UserRequests: cfg.LoginRateLimitUser,
	})

	// Admin endpoints
	mux.Handle("POST /api/v1/auth/login", loginRateLimiter(http.HandlerFunc(adminHandler.Login)))
	mux.HandleFunc("GET /api/v1/auth/oidc/config", adminHandler.OIDCConfig)
	mux.HandleFunc("GET /api/v1/auth/oidc/start", adminHandler.OIDCStart)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", adminHandler.OIDCCallback)
	mux.Handle("GET /api/v1/me", adminRoute(adminHandler.GetMe))
	mux.Handle("PUT /api/v1/me/password", adminRoute(adminHandler.ChangePassword))
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
<title>UniRouter API Docs</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;max-width:960px;margin:40px auto;padding:0 24px;line-height:1.6;color:#1f2937}
h1{margin-bottom:4px}.muted{color:#6b7280}code{background:#f3f4f6;padding:2px 6px;border-radius:4px}
table{border-collapse:collapse;width:100%;margin-top:16px}th,td{border:1px solid #e5e7eb;padding:10px;text-align:left}th{background:#f9fafb}
.method{font-weight:700}
</style>
</head>
<body>
<h1>UniRouter API Documentation</h1>
<p class="muted">Current API contract index. Admin APIs use JWT Bearer auth. Gateway APIs use UniRouter API keys.</p>
<p>Admin Base URL: <code>/api/v1</code> &nbsp; Gateway Base URL: <code>/v1</code></p>
<table>
<thead><tr><th>Method</th><th>Path</th><th>Description</th></tr></thead>
<tbody>
<tr><td class="method">GET</td><td><code>/</code></td><td>Service metadata</td></tr>
<tr><td class="method">GET</td><td><code>/docs</code></td><td>Backend API contract index</td></tr>
<tr><td class="method">GET</td><td><code>/health/live</code></td><td>Liveness check</td></tr>
<tr><td class="method">GET</td><td><code>/health/ready</code></td><td>Readiness check</td></tr>
<tr><td class="method">GET</td><td><code>/metrics</code></td><td>Prometheus metrics (runtime-configurable)</td></tr>
<tr><td class="method">GET</td><td><code>/v1/models</code></td><td>OpenAI-compatible model list</td></tr>
<tr><td class="method">POST</td><td><code>/v1/chat/completions</code></td><td>OpenAI-compatible chat completions</td></tr>
<tr><td class="method">POST</td><td><code>/v1/embeddings</code></td><td>OpenAI-compatible embeddings</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/auth/login</code></td><td>Admin login</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/auth/oidc/config</code></td><td>OIDC login configuration</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/auth/oidc/start</code></td><td>OIDC login start</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/auth/oidc/callback</code></td><td>OIDC login callback</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/me</code></td><td>Current admin user</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/me/password</code></td><td>Change current password</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/dashboard/stats</code></td><td>Dashboard statistics</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/channels</code></td><td>List channels</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/channels</code></td><td>Create channel</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/channels/{id}</code></td><td>Get channel</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/channels/{id}</code></td><td>Update channel</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/channels/{id}</code></td><td>Delete channel</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/channels/{id}/test</code></td><td>Test channel</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/channel-keys</code></td><td>List upstream credentials</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/channel-keys</code></td><td>Create upstream credential</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/channel-keys/{id}</code></td><td>Update upstream credential</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/channel-keys/{id}</code></td><td>Delete upstream credential</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/keys</code></td><td>List API keys</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/keys</code></td><td>Create API key</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/keys/{id}</code></td><td>Get API key</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/keys/{id}</code></td><td>Update API key</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/keys/{id}/disable</code></td><td>Disable API key</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects</code></td><td>List projects</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/projects</code></td><td>Create project</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects/{id}</code></td><td>Get project</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/projects/{id}</code></td><td>Update project</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects/{id}/model-sets</code></td><td>List project model sets</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/projects/{id}/model-sets</code></td><td>Replace project model sets</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/logs</code></td><td>Call logs with filters</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/logs/{id}</code></td><td>Call log detail</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/logs/{id}/trace</code></td><td>Request trace events</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/usage/analytics</code></td><td>Usage analytics and CSV export</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/internal-status</code></td><td>Event-derived internal status</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/runtime/status</code></td><td>Runtime environment, capacity, and external monitoring status</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/error-docs</code></td><td>Error documentation registry</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/wallet</code></td><td>Wallet information</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/wallet/topup</code></td><td>Top up wallet</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/wallet/adjust</code></td><td>Adjust wallet</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/wallet/transactions</code></td><td>Wallet transactions</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/wallet/transactions/export</code></td><td>Export wallet transactions</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/grants</code></td><td>List grants</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/grants</code></td><td>Create grant</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/audit-logs</code></td><td>Audit logs</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/settings</code></td><td>List settings</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/settings/{key}</code></td><td>Update setting</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/license</code></td><td>License status</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/license</code></td><td>Import offline license</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/users</code></td><td>List users</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/users</code></td><td>Create user</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/users/{id}</code></td><td>Update user</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/users/{id}</code></td><td>Delete user</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-prices</code></td><td>List model prices</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-prices</code></td><td>Create model price</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-prices/{id}</code></td><td>Update model price</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/model-prices/{id}</code></td><td>Delete model price</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-names</code></td><td>List model names</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-registry</code></td><td>List model registry</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-registry</code></td><td>Create model registry entry</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-registry/{id}</code></td><td>Update model registry entry</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-catalog</code></td><td>Approved model catalog</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-aliases</code></td><td>List model aliases</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-aliases</code></td><td>Create model alias</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-aliases/{id}</code></td><td>Update model alias</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-sets</code></td><td>List model sets</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-sets</code></td><td>Create model set</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-sets/{id}/items</code></td><td>List model set items</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-sets/{id}/items</code></td><td>Replace model set items</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/org-model-prices</code></td><td>List org model prices</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/org-model-prices</code></td><td>Create org model price</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/org-model-prices/{id}</code></td><td>Update org model price</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/org-model-prices/{id}</code></td><td>Delete org model price</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/reconciliation</code></td><td>Usage reconciliation</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/notification-events</code></td><td>Notification events</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/notification-deliveries</code></td><td>Notification deliveries</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/notification-webhooks</code></td><td>Notification webhooks</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/notification-webhooks</code></td><td>Create notification webhook</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/notification-webhooks/{id}</code></td><td>Update notification webhook</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/sensitive-words</code></td><td>List sensitive words</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/sensitive-words</code></td><td>Add sensitive words</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/sensitive-words</code></td><td>Replace sensitive words</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/sensitive-words</code></td><td>Delete sensitive words</td></tr>
</tbody>
</table>
<p class="muted">Repository source of truth: <code>docs/tech-ref/05-openapi-spec.yaml</code>.</p>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	mux.Handle("GET /api/v1/channels", platformRoute(adminHandler.ListChannels))
	mux.Handle("POST /api/v1/channels", platformRoute(adminHandler.CreateChannel))
	mux.Handle("GET /api/v1/channels/{id}", platformRoute(adminHandler.GetChannel))
	mux.Handle("PUT /api/v1/channels/{id}", platformRoute(adminHandler.UpdateChannel))
	mux.Handle("DELETE /api/v1/channels/{id}", platformRoute(adminHandler.DeleteChannel))

	mux.Handle("GET /api/v1/keys", projectReadRoute(adminHandler.ListAPIKeys))
	mux.Handle("POST /api/v1/keys", projectWriteRoute(adminHandler.CreateAPIKey))
	mux.Handle("GET /api/v1/keys/{id}", projectReadRoute(adminHandler.GetAPIKey))
	mux.Handle("PUT /api/v1/keys/{id}", roleRoute(adminHandler.UpdateAPIKey, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	mux.Handle("POST /api/v1/keys/{id}/disable", roleRoute(adminHandler.DisableAPIKey, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))

	mux.Handle("GET /api/v1/projects", projectReadRoute(adminHandler.ListProjects))
	mux.Handle("POST /api/v1/projects", orgAdminRoute(adminHandler.CreateProject))
	mux.Handle("GET /api/v1/projects/{id}", projectReadRoute(adminHandler.GetProject))
	mux.Handle("PUT /api/v1/projects/{id}", roleRoute(adminHandler.UpdateProject, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	mux.Handle("GET /api/v1/projects/{id}/model-sets", projectReadRoute(adminHandler.ListProjectModelSets))
	mux.Handle("PUT /api/v1/projects/{id}/model-sets", roleRoute(adminHandler.ReplaceProjectModelSets, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))

	mux.Handle("GET /api/v1/channel-keys", platformRoute(adminHandler.ListChannelKeys))
	mux.Handle("POST /api/v1/channel-keys", platformRoute(adminHandler.CreateChannelKey))
	mux.Handle("PUT /api/v1/channel-keys/{id}", platformRoute(adminHandler.UpdateChannelKey))
	mux.Handle("DELETE /api/v1/channel-keys/{id}", platformRoute(adminHandler.DeleteChannelKey))

	mux.Handle("GET /api/v1/wallet", billingRoute(adminHandler.GetWallet))
	mux.Handle("POST /api/v1/wallet/topup", billingRoute(adminHandler.TopUp))
	mux.Handle("POST /api/v1/wallet/adjust", billingRoute(adminHandler.BalanceAdjust))
	mux.Handle("GET /api/v1/wallet/transactions", billingRoute(adminHandler.ListWalletTransactions))

	mux.Handle("GET /api/v1/logs", usageRoute(adminHandler.ListLogs))
	mux.Handle("GET /api/v1/logs/{id}", usageRoute(adminHandler.GetLogDetail))
	mux.Handle("GET /api/v1/logs/{id}/trace", usageRoute(adminHandler.GetRequestTrace))
	mux.Handle("GET /api/v1/dashboard/stats", usageRoute(adminHandler.DashboardStats))
	mux.Handle("GET /api/v1/usage/analytics", usageRoute(adminHandler.UsageAnalytics))
	mux.Handle("GET /api/v1/internal-status", notificationReadRoute(adminHandler.InternalStatus))
	mux.Handle("GET /api/v1/runtime/status", roleRoute(adminHandler.RuntimeStatus, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin))
	mux.Handle("GET /api/v1/error-docs", usageRoute(adminHandler.ListErrorDocs))

	mux.Handle("GET /api/v1/model-prices", orgAdminRoute(adminHandler.ListModelPrices))
	mux.Handle("GET /api/v1/reconciliation", billingRoute(adminHandler.ListReconciliation))
	mux.Handle("POST /api/v1/model-prices", orgAdminRoute(adminHandler.CreateModelPrice))
	mux.Handle("PUT /api/v1/model-prices/{id}", orgAdminRoute(adminHandler.UpdateModelPrice))
	mux.Handle("DELETE /api/v1/model-prices/{id}", orgAdminRoute(adminHandler.DeleteModelPrice))
	mux.Handle("GET /api/v1/model-registry", orgAdminRoute(adminHandler.ListModelRegistry))
	mux.Handle("POST /api/v1/model-registry", orgAdminRoute(adminHandler.CreateModelRegistry))
	mux.Handle("PUT /api/v1/model-registry/{id}", orgAdminRoute(adminHandler.UpdateModelRegistry))
	mux.Handle("GET /api/v1/model-catalog", projectReadRoute(adminHandler.ListApprovedModelCatalog))
	mux.Handle("GET /api/v1/model-aliases", orgAdminRoute(adminHandler.ListModelAliases))
	mux.Handle("POST /api/v1/model-aliases", orgAdminRoute(adminHandler.CreateModelAlias))
	mux.Handle("PUT /api/v1/model-aliases/{id}", orgAdminRoute(adminHandler.UpdateModelAlias))
	mux.Handle("GET /api/v1/model-sets", orgAdminRoute(adminHandler.ListModelSets))
	mux.Handle("POST /api/v1/model-sets", orgAdminRoute(adminHandler.CreateModelSet))
	mux.Handle("GET /api/v1/model-sets/{id}/items", orgAdminRoute(adminHandler.ListModelSetItems))
	mux.Handle("PUT /api/v1/model-sets/{id}/items", orgAdminRoute(adminHandler.ReplaceModelSetItems))
	// Org model prices (org-level pricing overrides)
	mux.Handle("GET /api/v1/model-names", orgAdminRoute(adminHandler.GetModelNames))
	mux.Handle("GET /api/v1/org-model-prices", orgAdminRoute(adminHandler.ListOrgModelPrices))
	mux.Handle("POST /api/v1/org-model-prices", orgAdminRoute(adminHandler.CreateOrgModelPrice))
	mux.Handle("PUT /api/v1/org-model-prices/{id}", orgAdminRoute(adminHandler.UpdateOrgModelPrice))
	mux.Handle("DELETE /api/v1/org-model-prices/{id}", orgAdminRoute(adminHandler.DeleteOrgModelPrice))

	mux.Handle("GET /api/v1/users", orgAdminRoute(adminHandler.ListUsers))
	mux.Handle("POST /api/v1/users", orgAdminRoute(adminHandler.CreateUser))
	mux.Handle("PUT /api/v1/users/{id}", orgAdminRoute(adminHandler.UpdateUser))
	mux.Handle("DELETE /api/v1/users/{id}", orgAdminRoute(adminHandler.DeleteUser))

	mux.Handle("GET /api/v1/grants", billingRoute(adminHandler.ListGrants))
	mux.Handle("POST /api/v1/grants", billingRoute(adminHandler.CreateGrant))

	mux.Handle("GET /api/v1/notification-events", notificationReadRoute(adminHandler.ListNotificationEvents))
	mux.Handle("GET /api/v1/notification-deliveries", notificationReadRoute(adminHandler.ListNotificationDeliveries))
	mux.Handle("GET /api/v1/notification-webhooks", notificationReadRoute(adminHandler.ListNotificationWebhooks))
	mux.Handle("POST /api/v1/notification-webhooks", notificationWriteRoute(adminHandler.CreateNotificationWebhook))
	mux.Handle("PUT /api/v1/notification-webhooks/{id}", notificationWriteRoute(adminHandler.UpdateNotificationWebhook))

	mux.Handle("GET /api/v1/audit-logs", roleRoute(adminHandler.ListAuditLogs, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin))

	mux.Handle("GET /api/v1/settings", orgAdminRoute(adminHandler.ListSettings))
	mux.Handle("PUT /api/v1/settings/{key}", orgAdminRoute(adminHandler.UpdateSetting))
	mux.Handle("GET /api/v1/license", platformRoute(adminHandler.GetLicense))
	mux.Handle("PUT /api/v1/license", platformRoute(adminHandler.UpdateLicense))

	// Sensitive Words CRUD (PRD F13)
	mux.Handle("GET /api/v1/sensitive-words", orgAdminRoute(adminHandler.ListSensitiveWords))
	mux.Handle("POST /api/v1/sensitive-words", orgAdminRoute(adminHandler.AddSensitiveWords))
	mux.Handle("PUT /api/v1/sensitive-words", orgAdminRoute(adminHandler.UpdateSensitiveWords))
	mux.Handle("DELETE /api/v1/sensitive-words", orgAdminRoute(adminHandler.DeleteSensitiveWords))
	mux.Handle("POST /api/v1/channels/{id}/test", platformRoute(adminHandler.TestChannel))
	mux.Handle("GET /api/v1/wallet/transactions/export", billingRoute(adminHandler.ExportWalletTransactions))
	metricsHandler := middleware.MetricsMiddleware(mux)
	corsHandler := middleware.CORSWithOptions(middleware.CORSOptions{
		AllowedOrigins: cfg.AdminCORSAllowedOrigins,
	})(metricsHandler)
	securityHandler := middleware.SecurityHeaders(corsHandler)

	// Create server
	addr := fmt.Sprintf(":%d", cfg.ListenPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      securityHandler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("UniRoute server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSecs)*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":"not_found"}`)
		return
	}
	fmt.Fprintf(w, `{"service":"uniroute","status":"running"}`)
}

// findMigrationsDir locates the migrations directory.
func findMigrationsDir() string {
	// Try relative to binary
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "migrations")
		if info, e := os.Stat(dir); e == nil && info.IsDir() {
			return dir
		}
	}
	// Try relative to CWD
	dir := "migrations"
	if info, e := os.Stat(dir); e == nil && info.IsDir() {
		return dir
	}
	return "./migrations"
}

// loadSensitiveWords loads sensitive words from the settings table.
// The value is stored as a JSON array by admin handlers.
func loadSensitiveWords(ctx context.Context, pool *pgxpool.Pool) []string {
	var value string
	err := pool.QueryRow(ctx,
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&value)
	if err != nil {
		return nil
	}
	var words []string
	if err := json.Unmarshal([]byte(value), &words); err != nil {
		log.Printf("[filter] failed to parse sensitive_words JSON: %v", err)
		return nil
	}
	return words
}

// startSensitiveWordsReloadLoop keeps each gateway instance converged with DB-backed settings.
func startSensitiveWordsReloadLoop(pool *pgxpool.Pool, sensitiveFilter *filter.DFAFilter, interval time.Duration) {
	if sensitiveFilter == nil || interval <= 0 {
		return
	}
	reload := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		words := loadSensitiveWords(ctx, pool)
		if words != nil {
			sensitiveFilter.UpdateWords(words)
			log.Printf("Loaded %d sensitive words from settings", len(words))
		}
	}
	reload()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		reload()
	}
}

func applyBuildRuntimeMetadata(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.RuntimeVersion = runtimeMetadataValue(cfg.RuntimeVersion, buildVersion)
	cfg.RuntimeCommit = runtimeMetadataValue(cfg.RuntimeCommit, buildCommit)
}

func runtimeMetadataValue(configured, injected string) string {
	if value := strings.TrimSpace(configured); value != "" {
		return value
	}
	return strings.TrimSpace(injected)
}

// trimQuotes removes surrounding double quotes from a string.
func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// splitModels splits a comma-separated string of models.
func splitModels(models string) []string {
	var result []string
	for _, m := range splitByComma(models) {
		m = trimSpace(m)
		if m != "" {
			result = append(result, m)
		}
	}
	return result
}

func splitByComma(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	result = append(result, current)
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

// loadAlertConfig loads webhook URL and threshold from the settings table.
func loadAlertConfig(ctx context.Context, pool *pgxpool.Pool) (string, int) {
	var webhookURL string
	var budgetStr string
	pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'budget_alert_webhook_url'`).Scan(&webhookURL)
	pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'budget_cents'`).Scan(&budgetStr)
	budgetCents := 0
	if budgetStr != "" {
		fmt.Sscanf(budgetStr, "%d", &budgetCents)
	}
	return webhookURL, budgetCents
}

// startNotificationRetryLoop periodically retries failed notification webhook deliveries.
func startNotificationRetryLoop(eventSvc *billing.EventService) {
	if eventSvc == nil {
		return
	}
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		retried, err := eventSvc.RetryDueDeliveries(ctx, 50)
		cancel()
		if err != nil {
			log.Printf("Notification retry worker error: %v", err)
		} else if retried > 0 {
			log.Printf("Notification retry worker retried %d failed webhook deliveries", retried)
		}
		<-ticker.C
	}
}

// startBudgetAlertLoop periodically checks wallet balance and sends webhook alerts.
func startBudgetAlertLoop(alertSvc *billing.AlertService, pool *pgxpool.Pool) {
	time.Sleep(30 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	webhookURL, budgetCents := loadAlertConfig(ctx, pool)
	if webhookURL != "" && budgetCents > 0 {
		alertSvc.CheckAndNotify(ctx, webhookURL, budgetCents)
	}
	cancel()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		webhookURL, budgetCents := loadAlertConfig(ctx, pool)
		if webhookURL != "" && budgetCents > 0 {
			alertSvc.CheckAndNotify(ctx, webhookURL, budgetCents)
		}
		cancel()
	}
}

// startCleanupLoop runs the cleanup task periodically.
func startCleanupLoop(handler *admin.Handler, retentionDays, traceEventRetentionDays int) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	time.Sleep(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := handler.CleanupOldDataWithTraceRetention(ctx, retentionDays, traceEventRetentionDays); err != nil {
		log.Printf("Cleanup error: %v", err)
	}
	cancel()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := handler.CleanupOldDataWithTraceRetention(ctx, retentionDays, traceEventRetentionDays); err != nil {
			log.Printf("Cleanup error: %v", err)
		}
		cancel()
	}
}
