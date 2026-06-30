package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/redisx"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
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
	alertSvc := billing.NewAlertService(postgres.Pool)

	// Wire alert service into relay for post-settle budget checks
	go func() {
		time.Sleep(2 * time.Second)
		alertCtx := context.Background()
		webhookURL, budgetCents := loadAlertConfig(alertCtx, postgres.Pool)
		if webhookURL != "" && budgetCents > 0 {
			relay.SetAlertService(alertSvc, webhookURL, budgetCents)
		}
	}()

	adminHandler := admin.NewHandler(postgres.Pool, cfg.JWTSecret, encKey, sensitiveFilter)

	// Load sensitive words from settings (best effort)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		words := loadSensitiveWords(ctx, postgres.Pool)
		if len(words) > 0 {
			sensitiveFilter.UpdateWords(words)
			log.Printf("Loaded %d sensitive words from settings", len(words))
		}
	}()

	// Start cleanup goroutine
	if cfg.LogRetentionDays > 0 {
		go startCleanupLoop(adminHandler, cfg.LogRetentionDays)
	}

	// Start budget alert goroutine — checks every 5 minutes
	go startBudgetAlertLoop(alertSvc, postgres.Pool)

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
		fmt.Fprintf(w, `{"status":"ready"}`)
	})

	// Root endpoint
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"service":"uniroute","version":"1.0.0","status":"running"}`)
	})

	// Prometheus metrics endpoint
	mux.Handle("GET /metrics", promhttp.Handler())

	// Gateway endpoints (with auth + rate limiting)
	gatewayAuth := middleware.GatewayAuth(postgres.Pool, cfg.JWTSecret)
	rateLimiter := middleware.RateLimit(redisClient.Client)
	admissionMiddleware := middleware.AdmissionFromContext(admissionCtrl)

	// Wrap gateway handler with middleware
	chatHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := middleware.GetAPIKeyFromContext(r.Context())
		if apiKey == nil {
			http.Error(w, `{"error":{"type":"auth_error.invalid_api_key","message":"No API key"}}`, http.StatusUnauthorized)
			return
		}

		if apiKey.AllowedModels != "" && apiKey.AllowedModels != "*" {
			// Buffer body so we can peek at model and still pass it to relay
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, `{"error":{"type":"server_error","message":"Failed to read request body"}}`, http.StatusBadRequest)
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			var req struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(bodyBytes, &req); err == nil && req.Model != "" {
				allowed := false
				for _, m := range strings.Split(apiKey.AllowedModels, ",") {
					if strings.TrimSpace(m) == req.Model {
						allowed = true
						break
					}
				}
				if !allowed {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprintf(w, `{"error":{"type":"auth_error.model_not_allowed","message":"Model %s is not allowed for this API key","model":"%s"}}`, req.Model, req.Model)
					return
				}
			}
		}
		relay.HandleChatCompletion(w, r)
	})

	embeddingsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relay.HandleEmbeddings(w, r)
	})

	mux.Handle("POST /v1/chat/completions", gatewayAuth(admissionMiddleware(rateLimiter(chatHandler))))
	mux.Handle("POST /v1/embeddings", gatewayAuth(admissionMiddleware(rateLimiter(embeddingsHandler))))
	mux.Handle("GET /v1/models", gatewayAuth(admissionMiddleware(http.HandlerFunc(relay.HandleModels))))

	// Admin auth middleware
	adminAuth := middleware.AdminAuth(cfg.JWTSecret)

	// Admin endpoints
	mux.HandleFunc("POST /api/v1/auth/login", adminHandler.Login)
	mux.Handle("GET /api/v1/me", adminAuth(http.HandlerFunc(adminHandler.GetMe)))
	mux.Handle("PUT /api/v1/me/password", adminAuth(http.HandlerFunc(adminHandler.ChangePassword)))
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head><title>UniRouter API Docs</title></head>
<body>
<h1>UniRouter API Documentation</h1>
<p>API documentation is under construction.</p>
<p>Base URL: /api/v1</p>
<h2>Endpoints</h2>
<ul>
<li>POST /api/v1/auth/login - Login</li>
<li>GET /api/v1/me - Get current user</li>
<li>GET /api/v1/channels - List channels</li>
<li>GET /api/v1/keys - List API keys</li>
<li>GET /api/v1/dashboard/stats - Dashboard stats</li>
</ul>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	mux.Handle("GET /api/v1/channels", adminAuth(http.HandlerFunc(adminHandler.ListChannels)))
	mux.Handle("POST /api/v1/channels", adminAuth(http.HandlerFunc(adminHandler.CreateChannel)))
	mux.Handle("GET /api/v1/channels/{id}", adminAuth(http.HandlerFunc(adminHandler.GetChannel)))
	mux.Handle("PUT /api/v1/channels/{id}", adminAuth(http.HandlerFunc(adminHandler.UpdateChannel)))
	mux.Handle("DELETE /api/v1/channels/{id}", adminAuth(http.HandlerFunc(adminHandler.DeleteChannel)))

	mux.Handle("GET /api/v1/keys", adminAuth(http.HandlerFunc(adminHandler.ListAPIKeys)))
	mux.Handle("POST /api/v1/keys", adminAuth(http.HandlerFunc(adminHandler.CreateAPIKey)))
	mux.Handle("GET /api/v1/keys/{id}", adminAuth(http.HandlerFunc(adminHandler.GetAPIKey)))
	mux.Handle("PUT /api/v1/keys/{id}", adminAuth(http.HandlerFunc(adminHandler.UpdateAPIKey)))
	mux.Handle("POST /api/v1/keys/{id}/disable", adminAuth(http.HandlerFunc(adminHandler.DisableAPIKey)))

	mux.Handle("GET /api/v1/channel-keys", adminAuth(http.HandlerFunc(adminHandler.ListChannelKeys)))
	mux.Handle("POST /api/v1/channel-keys", adminAuth(http.HandlerFunc(adminHandler.CreateChannelKey)))
	mux.Handle("PUT /api/v1/channel-keys/{id}", adminAuth(http.HandlerFunc(adminHandler.UpdateChannelKey)))
	mux.Handle("DELETE /api/v1/channel-keys/{id}", adminAuth(http.HandlerFunc(adminHandler.DeleteChannelKey)))

	mux.Handle("GET /api/v1/wallet", adminAuth(http.HandlerFunc(adminHandler.GetWallet)))
	mux.Handle("POST /api/v1/wallet/topup", adminAuth(http.HandlerFunc(adminHandler.TopUp)))
	mux.Handle("POST /api/v1/wallet/adjust", adminAuth(http.HandlerFunc(adminHandler.BalanceAdjust)))
	mux.Handle("GET /api/v1/wallet/transactions", adminAuth(http.HandlerFunc(adminHandler.ListWalletTransactions)))

	mux.Handle("GET /api/v1/logs", adminAuth(http.HandlerFunc(adminHandler.ListLogs)))
	mux.Handle("GET /api/v1/logs/{id}", adminAuth(http.HandlerFunc(adminHandler.GetLogDetail)))
	mux.Handle("GET /api/v1/dashboard/stats", adminAuth(http.HandlerFunc(adminHandler.DashboardStats)))

	mux.Handle("GET /api/v1/model-prices", adminAuth(http.HandlerFunc(adminHandler.ListModelPrices)))
	mux.Handle("GET /api/v1/reconciliation", adminAuth(http.HandlerFunc(adminHandler.ListReconciliation)))
	mux.Handle("POST /api/v1/model-prices", adminAuth(http.HandlerFunc(adminHandler.CreateModelPrice)))
	mux.Handle("PUT /api/v1/model-prices/{id}", adminAuth(http.HandlerFunc(adminHandler.UpdateModelPrice)))
	mux.Handle("DELETE /api/v1/model-prices/{id}", adminAuth(http.HandlerFunc(adminHandler.DeleteModelPrice)))
	// Org model prices (org-level pricing overrides)
	mux.Handle("GET /api/v1/model-names", adminAuth(http.HandlerFunc(adminHandler.GetModelNames)))
	mux.Handle("GET /api/v1/org-model-prices", adminAuth(http.HandlerFunc(adminHandler.ListOrgModelPrices)))
	mux.Handle("POST /api/v1/org-model-prices", adminAuth(http.HandlerFunc(adminHandler.CreateOrgModelPrice)))
	mux.Handle("PUT /api/v1/org-model-prices/{id}", adminAuth(http.HandlerFunc(adminHandler.UpdateOrgModelPrice)))
	mux.Handle("DELETE /api/v1/org-model-prices/{id}", adminAuth(http.HandlerFunc(adminHandler.DeleteOrgModelPrice)))

	mux.Handle("GET /api/v1/users", adminAuth(http.HandlerFunc(adminHandler.ListUsers)))
	mux.Handle("POST /api/v1/users", adminAuth(http.HandlerFunc(adminHandler.CreateUser)))
	mux.Handle("PUT /api/v1/users/{id}", adminAuth(http.HandlerFunc(adminHandler.UpdateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", adminAuth(http.HandlerFunc(adminHandler.DeleteUser)))

	mux.Handle("GET /api/v1/grants", adminAuth(http.HandlerFunc(adminHandler.ListGrants)))
	mux.Handle("POST /api/v1/grants", adminAuth(http.HandlerFunc(adminHandler.CreateGrant)))

	mux.Handle("GET /api/v1/audit-logs", adminAuth(http.HandlerFunc(adminHandler.ListAuditLogs)))

	mux.Handle("GET /api/v1/settings", adminAuth(http.HandlerFunc(adminHandler.ListSettings)))
	mux.Handle("PUT /api/v1/settings/{key}", adminAuth(http.HandlerFunc(adminHandler.UpdateSetting)))

	// Sensitive Words CRUD (PRD F13)
	mux.Handle("GET /api/v1/sensitive-words", adminAuth(http.HandlerFunc(adminHandler.ListSensitiveWords)))
	mux.Handle("POST /api/v1/sensitive-words", adminAuth(http.HandlerFunc(adminHandler.AddSensitiveWords)))
	mux.Handle("PUT /api/v1/sensitive-words", adminAuth(http.HandlerFunc(adminHandler.UpdateSensitiveWords)))
	mux.Handle("DELETE /api/v1/sensitive-words", adminAuth(http.HandlerFunc(adminHandler.DeleteSensitiveWords)))
	mux.Handle("POST /api/v1/channels/{id}/test", adminAuth(http.HandlerFunc(adminHandler.TestChannel)))
	mux.Handle("GET /api/v1/wallet/transactions/export", adminAuth(http.HandlerFunc(adminHandler.ExportWalletTransactions)))
	metricsHandler := middleware.MetricsMiddleware(mux)
	corsHandler := middleware.CORS(metricsHandler)

	// Create server
	addr := fmt.Sprintf(":%d", cfg.ListenPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      corsHandler,
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
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
func startCleanupLoop(handler *admin.Handler, retentionDays int) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	time.Sleep(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := handler.CleanupOldData(ctx, retentionDays); err != nil {
		log.Printf("Cleanup error: %v", err)
	}
	cancel()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := handler.CleanupOldData(ctx, retentionDays); err != nil {
			log.Printf("Cleanup error: %v", err)
		}
		cancel()
	}
}
