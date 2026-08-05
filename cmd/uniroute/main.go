package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capapi"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/eventconnectorshadow"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/oidc"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/policybilling"
	"github.com/uniroute/unirouter/internal/redisx"
	"github.com/uniroute/unirouter/internal/releasetrust"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimeconfig"
	"github.com/uniroute/unirouter/internal/telemetryexport"
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
	trustBundle, err := applyReleaseTrust(cfg)
	if err != nil {
		log.Fatalf("Failed to load release trust: %v", err)
	}
	log.Printf("Release trust mode: %s license_keys=%d upgrade_keys=%d", trustBundle.Mode, trustBundle.LicenseKeyCount, trustBundle.UpgradeKeyCount)
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
	invocationOptimizationManager, err := invocationopt.NewManager(postgres.Pool)
	if err != nil {
		log.Fatalf("Failed to initialize invocation-optimization policy manager: %v", err)
	}
	var responseCacheService *responsecache.Service
	if cfg.ResponseCacheConfigured() {
		responseCacheBackend, backendErr := responsecache.NewRedisBackend(redisClient.Client, "uniroute:response-cache:v2")
		if backendErr != nil {
			log.Fatalf("Failed to initialize exact response-cache backend: %v", backendErr)
		}
		responseCacheKeyring, keyringErr := responsecache.NewStaticKeyring(
			cfg.ResponseCacheKeyVersion,
			map[string][]byte{cfg.ResponseCacheKeyVersion: []byte(cfg.ResponseCacheEncryptionKey)},
		)
		if keyringErr != nil {
			log.Fatalf("Failed to initialize exact response-cache encryption keyring: %v", keyringErr)
		}
		responseCacheService, err = responsecache.NewService(
			responseCacheBackend,
			[]byte(cfg.ResponseCacheIndexKey),
			responseCacheKeyring,
			responsecache.DefaultConfig(),
		)
		if err != nil {
			log.Fatalf("Failed to initialize exact response-cache service: %v", err)
		}
		log.Printf("Exact response-cache runtime ready: contract=%s key_version=%s policies_default=disabled", responsecache.Version, cfg.ResponseCacheKeyVersion)
	} else {
		log.Printf("Exact response-cache runtime disabled: encryption/index key references are not configured")
	}
	agentCapabilityService, err := agentcap.NewService(postgres.Pool, redisClient.Client, cfg.APIKeyHashSecret)
	if err != nil {
		log.Fatalf("Failed to initialize Agent capability service: %v", err)
	}
	agentCapabilityHandler := capapi.NewHandler(agentCapabilityService)
	limitPolicyOptions := limitpolicy.DefaultOptions()
	limitPolicyOptions.Workload = cfg.WorkloadAdmissionOptions()
	limitPolicyOptions.GlobalConcurrency = int64(cfg.WorkloadGlobalCapacity)
	limitPolicyOptions.OrgConcurrency = int64(cfg.WorkloadOrgCapacity)
	limitPolicyService, err := limitpolicy.NewService(postgres.Pool, redisClient.Client, limitPolicyOptions)
	if err != nil {
		log.Fatalf("Failed to initialize distributed limit-policy service: %v", err)
	}
	initialLimitPolicyCtx, cancelInitialLimitPolicyLoad := context.WithTimeout(context.Background(), 10*time.Second)
	if err := limitPolicyService.Refresh(initialLimitPolicyCtx); err != nil {
		cancelInitialLimitPolicyLoad()
		log.Fatalf("Failed to load distributed limit-policy snapshot: %v", err)
	}
	cancelInitialLimitPolicyLoad()
	limitPolicyRefreshCtx, stopLimitPolicyRefresh := context.WithCancel(context.Background())
	defer stopLimitPolicyRefresh()
	go limitPolicyService.RunRefreshLoop(limitPolicyRefreshCtx)

	// Initialize core components from one typed, read-only effective
	// configuration registry. Dynamic policy rollout remains a later contract.
	effectiveConfig, err := runtimeconfig.New(cfg.RoutingPolicyConfig(), cfg.StreamPolicyConfig())
	if err != nil {
		log.Fatalf("Invalid effective runtime configuration: %v", err)
	}
	router, err := gateway.NewRouterWithPolicy(postgres.Pool, effectiveConfig.RoutingPolicy.Config)
	if err != nil {
		log.Fatalf("Invalid effective routing configuration: %v", err)
	}
	modelAuthorizationEvaluator, err := modelauthz.NewEvaluator(postgres.Pool, cfg.RuntimeEnvironment)
	if err != nil {
		log.Fatalf("Failed to initialize model-call authorization: %v", err)
	}
	runtimeChangeManager, err := runtimechange.NewManager(postgres.Pool, router, effectiveConfig.RoutingPolicy.Config)
	if err != nil {
		log.Fatalf("Failed to initialize runtime change control: %v", err)
	}
	router.SetRuntimeResolver(runtimeChangeManager)
	mediaStore, err := mediajob.NewPostgresStore(postgres.Pool)
	if err != nil {
		log.Fatalf("Failed to initialize async-media store: %v", err)
	}
	mediaCipher, err := mediajob.NewApplicationCipher([]byte(cfg.EncryptionKey))
	if err != nil {
		log.Fatalf("Failed to initialize async-media payload protection: %v", err)
	}
	mediaIdentitySecret := sha256.Sum256([]byte(cfg.APIKeyHashSecret))
	var mediaObjects mediajob.ObjectStore = mediajob.UnavailableObjectStore{}
	if cfg.AsyncMediaEnabled {
		mediaObjects, err = mediajob.NewS3ObjectStore(context.Background(), mediajob.S3Config{
			Region: cfg.AsyncMediaS3Region, Bucket: cfg.AsyncMediaS3Bucket,
			Endpoint: cfg.AsyncMediaS3Endpoint, Prefix: cfg.AsyncMediaS3Prefix,
			ForcePathStyle: cfg.AsyncMediaS3ForcePathStyle,
			KeySecret:      mediaIdentitySecret[:],
		})
		if err != nil {
			log.Fatalf("Failed to initialize async-media object store: %v", err)
		}
	}
	mediaAuthorizer := mediajob.ModelAuthorizerFunc(func(
		ctx context.Context,
		scope mediajob.Scope,
		requestedModel string,
	) (mediajob.ModelAuthorization, error) {
		access, accessErr := modelgov.CheckAccess(
			ctx, postgres.Pool, scope.OrgID, scope.ProjectID, requestedModel,
		)
		switch {
		case errors.Is(accessErr, modelgov.ErrModelRetired),
			errors.Is(accessErr, modelgov.ErrModelDenied),
			errors.Is(accessErr, modelgov.ErrModelRequired):
			return mediajob.ModelAuthorization{}, mediajob.ErrForbidden
		case accessErr != nil:
			return mediajob.ModelAuthorization{}, mediajob.ErrUnavailable
		default:
			routingModel := access.ResolvedModel
			if routingModel == "" {
				routingModel = requestedModel
			}
			apiKey := middleware.GetAPIKeyFromContext(ctx)
			if apiKey == nil {
				return mediajob.ModelAuthorization{}, mediajob.ErrForbidden
			}
			input := modelauthz.Input{
				OrgID: scope.OrgID, ProjectID: scope.ProjectID, APIKeyID: apiKey.ID,
				Endpoint: "/v1/media/jobs", RequestedModel: requestedModel, ResolvedModel: routingModel,
			}
			if apiKey.OwnerUserID != nil {
				input.OwnerUserID = *apiKey.OwnerUserID
			}
			decision, decisionErr := modelAuthorizationEvaluator.Evaluate(ctx, input)
			if decisionErr != nil {
				return mediajob.ModelAuthorization{}, mediajob.ErrUnavailable
			}
			if !decision.Allowed {
				return mediajob.ModelAuthorization{}, mediajob.ErrForbidden
			}
			return mediajob.ModelAuthorization{RoutingModel: routingModel}, nil
		}
	})
	mediaService, err := mediajob.NewService(
		mediaStore, mediaObjects, mediaCipher, mediaAuthorizer,
		mediaIdentitySecret[:],
		mediajob.ServiceOptions{
			Retention:        mediajob.DefaultRetention,
			MaxAttempts:      cfg.AsyncMediaMaxAttempts,
			RuntimeEnabled:   cfg.AsyncMediaEnabled,
			AdmissionEnabled: cfg.AsyncMediaAdmissionEnabled,
		},
	)
	if err != nil {
		log.Fatalf("Failed to initialize async-media service: %v", err)
	}
	mediaHandler, err := mediajob.NewHTTPHandler(mediaService, postgres.Pool)
	if err != nil {
		log.Fatalf("Failed to initialize async-media HTTP boundary: %v", err)
	}
	var stopAsyncMedia context.CancelFunc
	var asyncMediaDone chan struct{}
	if cfg.AsyncMediaEnabled {
		mediaProvider, providerErr := mediajob.NewGPTImageProvider(
			router,
			[]byte(cfg.EncryptionKey),
			mediajob.GPTImageProviderOptions{
				Timeout: time.Duration(cfg.AsyncMediaProviderTimeoutSecs) * time.Second,
			},
		)
		if providerErr != nil {
			log.Fatalf("Failed to initialize GPT Image provider: %v", providerErr)
		}
		mediaWorker, workerErr := mediajob.NewWorker(
			mediaStore, mediaObjects, mediaProvider, mediaCipher,
			mediajob.WorkerOptions{
				Owner:              asyncMediaWorkerID(cfg.AsyncMediaWorkerID),
				LeaseDuration:      time.Duration(cfg.AsyncMediaLeaseSeconds) * time.Second,
				PurgeLeaseDuration: time.Duration(cfg.AsyncMediaPurgeLeaseSeconds) * time.Second,
				PollInterval:       time.Duration(cfg.AsyncMediaPollMilliseconds) * time.Millisecond,
				OnError: func(workerErr error) {
					log.Printf("Async-media worker operation failed: %v", workerErr)
				},
			},
		)
		if workerErr != nil {
			log.Fatalf("Failed to initialize async-media worker: %v", workerErr)
		}
		asyncMediaCtx, cancelAsyncMedia := context.WithCancel(context.Background())
		stopAsyncMedia = cancelAsyncMedia
		asyncMediaDone = make(chan struct{})
		go func() {
			defer close(asyncMediaDone)
			runErr := mediaWorker.Run(asyncMediaCtx)
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				log.Printf("Async-media worker stopped: %v", runErr)
			}
		}()
		log.Printf(
			"Async-media Image V1 enabled: profile=%s retention=P7D video=false admission=%t",
			mediajob.Evidence(true, cfg.AsyncMediaAdmissionEnabled).ProfileID,
			cfg.AsyncMediaAdmissionEnabled,
		)
	} else {
		log.Printf("Async-media Image V1 runtime disabled; contract and existing metadata remain readable")
	}
	billingSvc := billing.NewBilling(postgres.Pool, redisClient.Client)
	completedFeedOptions := make([]completedfeed.ServiceOption, 0, 1)
	if cfg.CompletedExecutionFeedReferenceMode != config.CompletedExecutionFeedReferenceModeLegacy {
		completedFeedOptions = append(
			completedFeedOptions,
			completedfeed.WithReferenceProjection(
				cfg.CompletedExecutionFeedReferenceMode,
				func(
					snapshotBytes []byte,
					sourceInstance string,
				) (completedfeed.ReferenceEnvelopeV1Result, error) {
					result, connectorErr := eventconnectorshadow.BuildNeutralReferenceEnvelopeV1(
						snapshotBytes,
						sourceInstance,
					)
					if connectorErr != nil {
						return completedfeed.ReferenceEnvelopeV1Result{}, connectorErr
					}
					return result, nil
				},
				eventconnectorshadow.NeutralReferenceRuntimeBindingV1(),
			),
		)
	}
	completedFeedSvc, err := completedfeed.NewService(
		postgres.Pool,
		cfg.APIKeyHashSecret,
		cfg.CompletedExecutionFeedSourceInstance,
		cfg.CompletedExecutionFeedEnabled,
		completedFeedOptions...,
	)
	if err != nil {
		log.Fatalf("Failed to initialize completed-execution feed: %v", err)
	}
	log.Printf(
		"Completed-execution Feed Reference runtime: mode=%s protocol_version=%s connector_id=%s",
		cfg.CompletedExecutionFeedReferenceMode,
		completedfeed.ReferenceProtocolVersionV1,
		completedfeed.ReferenceConnectorIDV1,
	)
	billingSvc.SetTerminalMaterializer(completedFeedSvc)
	outboxSvc, err := outbox.New(postgres.Pool, outbox.DefaultOptions())
	if err != nil {
		log.Fatalf("Failed to initialize internal outbox: %v", err)
	}
	billingSvc.SetOutbox(outboxSvc)
	terminalHookSvc, err := newRuntimeTerminalHooks(postgres.Pool, outboxSvc, agentCapabilityService, limitPolicyService)
	if err != nil {
		log.Fatalf("Failed to initialize durable runtime terminal hooks: %v", err)
	}
	agentBudgetBilling := capbilling.New(billingSvc, agentCapabilityService)
	agentBudgetBilling.SetTerminalHooks(terminalHookSvc)
	sharedPolicyBilling := policybilling.New(agentBudgetBilling, limitPolicyService)
	sharedPolicyBilling.SetTerminalHooks(terminalHookSvc)
	settlementRecoveryCtx, stopSettlementRecovery := context.WithCancel(context.Background())
	defer stopSettlementRecovery()
	go billingSvc.RunSettlementRecoveryLoop(settlementRecoveryCtx, 10*time.Second, 100)
	admissionCtrl := middleware.NewDefaultAdmissionController()
	initialFilterCtx, cancelInitialFilterLoad := context.WithTimeout(context.Background(), 10*time.Second)
	initialSensitiveWords, initialSensitiveVersion, err := loadSensitiveWords(initialFilterCtx, postgres.Pool)
	cancelInitialFilterLoad()
	if err != nil {
		log.Fatalf("Failed to load deployment sensitive-word policy: %v", err)
	}
	sensitiveFilter := filter.NewDFAFilter([]string{})
	if !sensitiveFilter.UpdateWordsVersioned(initialSensitiveWords, initialSensitiveVersion) {
		log.Fatal("Failed to initialize deployment sensitive-word policy version")
	}
	encKey := []byte(cfg.EncryptionKey)
	guardrailService, stopGuardrailRefresh, err := startGuardrailRuntime(postgres.Pool, sensitiveFilter, cfg.APIKeyHashSecret, cfg.GuardrailOptions())
	if err != nil {
		log.Fatalf("Failed to initialize guardrail kernel: %v", err)
	}
	defer stopGuardrailRefresh()
	payloadBudget, err := payloadpolicy.NewBudget(cfg.PayloadLimits())
	if err != nil {
		log.Fatalf("Invalid payload safety configuration: %v", err)
	}
	relay := gateway.NewRelay(postgres.Pool, redisClient.Client, router, sharedPolicyBilling, sensitiveFilter, encKey)
	if err := relay.SetBillingFreezePolicy(cfg.FreezeSafetyFactor, cfg.MinFreezeCents); err != nil {
		log.Fatalf("Invalid billing freeze policy: %v", err)
	}
	relay.SetPayloadBudget(payloadBudget)
	relay.SetGuardrail(guardrailService)
	relay.SetOutbox(outboxSvc)
	relay.SetModelAuthorization(modelAuthorizationEvaluator)
	if err := relay.SetStreamPolicy(effectiveConfig.StreamTransport.Config); err != nil {
		log.Fatalf("Invalid effective stream transport configuration: %v", err)
	}
	relay.SetTraceEvents(cfg.TraceEventsEnabled, cfg.TraceEventSampleRate)
	relay.SetPayloadCaptureMode(cfg.PayloadCaptureMode)
	relay.SetEvidenceContextTrustSecret(cfg.EvidenceContextTrustSecret)
	relay.SetInvocationOptimization(
		invocationOptimizationManager,
		responseCacheService,
		cfg.RuntimeVersion,
		cfg.RuntimeCommit,
		cfg.RuntimeEnvironment,
	)
	var otelExporter *telemetryexport.Exporter
	if cfg.OTelExportEnabled {
		otelExporter, err = telemetryexport.New(cfg.TelemetryExportConfig(), nil)
		if err != nil {
			log.Fatalf("Failed to initialize metadata-only OTLP trace exporter: %v", err)
		}
		relay.SetTraceExporter(otelExporter)
		log.Printf(
			"Metadata-only OTLP trace export enabled: queue=%d batch=%d",
			cfg.OTelExportQueueSize,
			cfg.OTelExportBatchSize,
		)
	}
	eventSvc := billing.NewEventService(postgres.Pool, cfg.NotificationWebhookSigningSecret)
	eventSvc.SetOutbox(outboxSvc)
	relay.SetEventService(eventSvc)

	adminHandler := admin.NewHandlerWithAPIKeyHashSecret(postgres.Pool, cfg.JWTSecret, cfg.APIKeyHashSecret, encKey, sensitiveFilter)
	adminHandler.SetOutbox(outboxSvc)
	adminHandler.SetCompletedFeedController(completedFeedSvc)
	adminHandler.SetRoutingController(router)
	adminHandler.SetRuntimeChangeManager(runtimeChangeManager)
	adminHandler.SetModelAuthorizationEvaluator(modelAuthorizationEvaluator)
	adminHandler.SetGuardrailController(guardrailService)
	adminHandler.SetProviderCostReconciler(billingSvc)
	adminHandler.SetLimitPolicyReloader(limitPolicyService)
	adminHandler.SetInvocationOptimization(invocationOptimizationManager, responseCacheService != nil)
	legacyLicenseKey, err := licensing.ParsePublicKey(cfg.LicensePublicKey)
	if err != nil {
		log.Fatalf("Invalid license configuration: %v", err)
	}
	licenseKeyRing, err := licensing.ParsePublicKeyRing(cfg.LicensePublicKeys)
	if err != nil {
		log.Fatalf("Invalid commercial license key ring: %v", err)
	}
	licenseVerifier := licensing.NewCommercialVerifier(licenseKeyRing, legacyLicenseKey, cfg.LicenseExpiryWarningDays, cfg.LicenseDeploymentID)
	licenseRuntime := licensing.NewRuntime(licenseVerifier)
	adminHandler.SetLicenseVerifier(licenseVerifier)
	adminHandler.SetLicenseRuntime(licenseRuntime)
	initialLicenseStatus, err := adminHandler.ReconcileStoredCommercialLicense(context.Background())
	if err != nil {
		licenseRuntime.Replace(nil)
		log.Printf("Commercial License startup reconciliation failed; runtime remains fail-closed: %v", err)
		initialLicenseStatus = licenseRuntime.Status()
	}
	log.Printf("Commercial license status: %s enforceable=%t", initialLicenseStatus.Status, initialLicenseStatus.Enforceable)
	eventSvc.SetAdmission(func() bool {
		return licenseRuntime.Authorize(licensing.FeatureNotifications).Allowed
	})
	adminHandler.SetEventService(eventSvc)
	adminHandler.SetRedisClient(redisClient.Client)
	adminHandler.SetRuntimeStatusOptions(admin.RuntimeStatusOptions{
		Environment:     cfg.RuntimeEnvironment,
		DeploymentMode:  cfg.RuntimeDeploymentMode,
		DeploymentRole:  cfg.RuntimeDeploymentRole,
		Version:         cfg.RuntimeVersion,
		Commit:          cfg.RuntimeCommit,
		PrometheusURL:   cfg.PrometheusURL,
		GrafanaURL:      cfg.GrafanaURL,
		EffectiveConfig: effectiveConfig,
	})
	for _, registration := range []struct {
		eventType string
		handler   outbox.Handler
	}{
		{outbox.EventBillingUsagePrepared, billingSvc.ProcessUsageJournalEvent},
		{outbox.EventBillingSettled, billingSvc.ProcessUsageJournalEvent},
		{outbox.EventBillingCancelled, billingSvc.ProcessUsageJournalEvent},
		{outbox.EventBillingUsagePreparedV1, billingSvc.ProcessUsageJournalEvent},
		{outbox.EventBillingSettledV1, billingSvc.ProcessUsageJournalEvent},
		{outbox.EventBillingCancelledV1, billingSvc.ProcessUsageJournalEvent},
		{outbox.EventCallLog, relay.ProcessCallLogOutbox},
		{outbox.EventCallLogV1, relay.ProcessCallLogOutbox},
		{outbox.EventTrace, relay.ProcessTraceOutbox},
		{outbox.EventAudit, adminHandler.ProcessAuditOutbox},
		{outbox.EventBudgetEvaluate, eventSvc.ProcessBudgetEvaluation},
		{outbox.EventNotificationDelivery, eventSvc.ProcessNotificationDelivery},
		{outbox.EventRuntimeTerminal, terminalHookSvc.ProcessEvent},
		{outbox.EventRuntimeTerminalV1, terminalHookSvc.ProcessEvent},
	} {
		if err := outboxSvc.RegisterHandler(registration.eventType, registration.handler); err != nil {
			log.Fatalf("Failed to register internal outbox handler %s: %v", registration.eventType, err)
		}
	}
	outboxCtx, stopOutbox := context.WithCancel(context.Background())
	outboxDone := make(chan struct{})
	go func() {
		defer close(outboxDone)
		outboxSvc.Run(outboxCtx)
	}()
	completedFeedRetentionCtx, stopCompletedFeedRetention := context.WithCancel(context.Background())
	completedFeedRetentionDone := make(chan struct{})
	go func() {
		defer close(completedFeedRetentionDone)
		completedFeedSvc.RunRetention(completedFeedRetentionCtx)
	}()
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
	licenseRefreshCtx, stopLicenseRefresh := context.WithCancel(context.Background())
	defer stopLicenseRefresh()
	go runLicenseRefreshLoop(licenseRefreshCtx, adminHandler, cfg.LicenseRefreshInterval())

	// Load sensitive words from settings and periodically refresh all instances.
	go startSensitiveWordsReloadLoop(postgres.Pool, sensitiveFilter, time.Minute)

	// Start cleanup goroutine
	if cfg.LogRetentionDays > 0 || cfg.TraceEventRetentionDays > 0 {
		go startCleanupLoop(adminHandler, cfg.LogRetentionDays, cfg.TraceEventRetentionDays)
	}

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
	clientIPResolver, err := middleware.NewClientIPResolver(cfg.TrustedProxyCIDRs)
	if err != nil {
		log.Fatalf("Invalid trusted proxy configuration: %v", err)
	}
	gatewayAuth := middleware.GatewayAuthWithAgentCapabilitiesAndClientIP(postgres.Pool, cfg.APIKeyHashSecret, agentCapabilityService, clientIPResolver)
	mediaAuth := middleware.GatewayAuthWithAgentCapabilitiesAndClientIP(postgres.Pool, cfg.APIKeyHashSecret, nil, clientIPResolver)
	completedFeedAuth := middleware.CompletedExecutionFeedAuth(postgres.Pool, cfg.APIKeyHashSecret, clientIPResolver)
	gatewayLicenseGate := licenseRuntime.Gate(licensing.FeatureGateway)
	agentLicenseGate := licenseRuntime.GateAll(licensing.FeatureGateway, licensing.FeatureAgentCapabilities)
	rateLimiter := middleware.RateLimit(redisClient.Client)
	admissionMiddleware := middleware.AdmissionFromContext(admissionCtrl)
	idempotencyStore := middleware.NewPostgresIdempotencyStore(postgres.Pool)
	idempotencyGuard := func(handler http.Handler) http.Handler {
		return middleware.IdempotencyGuard(idempotencyStore, cfg.APIKeyHashSecret, handler)
	}

	chatHandler := newGatewayChatHandler(postgres.Pool, relay, outboxSvc)
	embeddingsHandler := newGatewayEmbeddingsHandler(postgres.Pool, relay, outboxSvc)
	chatPayloadGuard := payloadpolicy.RequestMiddleware(payloadBudget, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext)
	embeddingsPayloadGuard := payloadpolicy.RequestMiddleware(payloadBudget, payloadpolicy.ProfileEmbeddingsRequest, middleware.GetOrgIDFromContext)

	mux.Handle("POST /v1/chat/completions", gatewayLicenseGate(gatewayAuth(chatPayloadGuard(idempotencyGuard(chatHandler)))))
	mux.Handle("POST /v1/embeddings", gatewayLicenseGate(gatewayAuth(embeddingsPayloadGuard(idempotencyGuard(embeddingsHandler)))))
	mux.Handle("GET /v1/models", gatewayLicenseGate(gatewayAuth(admissionMiddleware(http.HandlerFunc(relay.HandleModels)))))
	mux.Handle("POST /v1/media/jobs", gatewayLicenseGate(mediaAuth(rateLimiter(http.HandlerFunc(mediaHandler.Submit)))))
	mux.Handle("GET /v1/media/jobs/{id}", gatewayLicenseGate(mediaAuth(rateLimiter(http.HandlerFunc(mediaHandler.Get)))))
	mux.Handle("POST /v1/media/jobs/{id}/cancel", gatewayLicenseGate(mediaAuth(rateLimiter(http.HandlerFunc(mediaHandler.Cancel)))))
	mux.Handle("GET /v1/media/jobs/{id}/content", gatewayLicenseGate(mediaAuth(rateLimiter(http.HandlerFunc(mediaHandler.Content)))))
	mux.Handle(
		"GET /api/v1/integrations/xinsight/completed-executions",
		completedFeedAuth(completedFeedSvc.Handler()),
	)
	registerAgentCapabilityRoutes(mux, agentCapabilityHandler, agentCapabilityRouteGuards{
		authorize:    agentLicenseGate,
		authenticate: gatewayAuth,
		rateLimit:    rateLimiter,
	})

	// Admin auth middleware
	adminAuth := middleware.AdminAuth(postgres.Pool, cfg.JWTSecret)
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
	pricingReadRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)
	}
	billingRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleBillingAdmin)
	}
	usageRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)
	}
	dashboardRoute := func(h http.HandlerFunc) http.Handler {
		return adminAuth(middleware.RequireExactRoles(
			auth.RoleLegacyAdmin,
			auth.RolePlatformAdmin,
			auth.RoleOrgAdmin,
			auth.RoleBillingAdmin,
			auth.RoleOrgViewer,
		)(http.HandlerFunc(h)))
	}
	notificationEventReadRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin)
	}
	notificationEventWriteRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin)
	}
	notificationOperatorRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin)
	}
	projectListRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)
	}
	projectReadRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer)
	}
	projectManagementRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin)
	}
	projectBudgetRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin)
	}
	projectWriteRoute := func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper)
	}
	licensedRoute := func(feature string, route func(http.HandlerFunc) http.Handler) func(http.HandlerFunc) http.Handler {
		return func(h http.HandlerFunc) http.Handler {
			gated := licenseRuntime.Gate(feature)(http.HandlerFunc(h))
			return route(http.HandlerFunc(gated.ServeHTTP))
		}
	}

	loginRateLimiter := middleware.LoginRateLimit(redisClient.Client, middleware.LoginRateLimitOptions{
		Enabled:      cfg.LoginRateLimitEnabled,
		Window:       time.Duration(cfg.LoginRateLimitWindowSec) * time.Second,
		IPRequests:   cfg.LoginRateLimitIP,
		UserRequests: cfg.LoginRateLimitUser,
		ClientIP:     clientIPResolver,
	})

	// Admin endpoints
	mux.Handle("POST /api/v1/auth/login", loginRateLimiter(http.HandlerFunc(adminHandler.Login)))
	mux.HandleFunc("POST /api/v1/auth/logout", adminHandler.Logout)
	mux.Handle("GET /api/v1/auth/oidc/config", licenseRuntime.Gate(licensing.FeatureOIDC)(http.HandlerFunc(adminHandler.OIDCConfig)))
	mux.Handle("GET /api/v1/auth/oidc/start", licenseRuntime.Gate(licensing.FeatureOIDC)(http.HandlerFunc(adminHandler.OIDCStart)))
	mux.Handle("GET /api/v1/auth/oidc/callback", licenseRuntime.Gate(licensing.FeatureOIDC)(http.HandlerFunc(adminHandler.OIDCCallback)))
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
<p class="muted">Current API contract index. Admin APIs use JWT Bearer auth. Gateway model APIs accept UniRouter API keys or endpoint-scoped Agent capabilities. The private completed-execution feed accepts only its project- and network-bound feed service key.</p>
<p>Admin Base URL: <code>/api/v1</code> &nbsp; Gateway Base URL: <code>/v1</code></p>
<p><code>X-Request-ID</code> in every response is a server operation ID. Send caller correlation in <code>X-UniRouter-Client-Request-ID</code>. Chat and Embeddings accept <code>Idempotency-Key</code> for tenant/key/payload-bound metadata-only deduplication. Async image admission requires it for a persistent project/key/payload-bound job identity. Admission responses never replay model output bytes.</p>
<p>Image V1 is an isolated job plane using <code>unirouter.openai.images.gpt-image.v1</code>. Output is available only through authenticated proxy routes for exactly seven days after durable persistence. Provider success plus durable output records an observed charge-eligibility fact; the current text-token rate table is not reused and no wallet amount is invented.</p>
<p>Agent discovery is explicit: a supervisor exchanges its root API key, then registers each child with an attenuation-only capability. Capabilities last at most five minutes, normally refresh near minute three, grant model-gateway authority only, and never grant Tool, IAM, database or export authority. Model calls reserve cost, tokens, rolling rate, and concurrency on every signed ancestor before any upstream dispatch; Redis uncertainty denies new capability admission.</p>
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
<tr><td class="method">POST</td><td><code>/v1/media/jobs</code></td><td>Submit one asynchronous GPT Image V1 job using a project root API key</td></tr>
<tr><td class="method">GET</td><td><code>/v1/media/jobs/{id}</code></td><td>Read tenant-scoped image job metadata</td></tr>
<tr><td class="method">POST</td><td><code>/v1/media/jobs/{id}/cancel</code></td><td>Request cancellation without refund semantics</td></tr>
<tr><td class="method">GET</td><td><code>/v1/media/jobs/{id}/content</code></td><td>Read authenticated image bytes before fixed expiry</td></tr>
<tr><td class="method">POST</td><td><code>/v1/agent-capabilities/exchange</code></td><td>Register root Agent workflow using a root API key</td></tr>
<tr><td class="method">POST</td><td><code>/v1/agent-capabilities/delegate</code></td><td>Register one child Agent with attenuation-only authority</td></tr>
<tr><td class="method">POST</td><td><code>/v1/agent-capabilities/refresh</code></td><td>Refresh a five-minute Agent capability</td></tr>
<tr><td class="method">POST</td><td><code>/v1/agent-capabilities/revoke</code></td><td>Revoke a workflow for new admissions</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/integrations/xinsight/completed-executions</code></td><td>Private final completed-execution metadata feed for X-Insight</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/auth/login</code></td><td>Admin login</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/auth/logout</code></td><td>Revoke current admin session</td></tr>
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
<tr><td class="method">GET</td><td><code>/api/v1/provider-packs</code></td><td>List immutable fixture-verified Provider Conformance Packs</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/routing/preview</code></td><td>Preview routing candidates and exclusions</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/channel-keys</code></td><td>List upstream credentials</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/channel-keys</code></td><td>Create upstream credential</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/channel-keys/{id}</code></td><td>Update upstream credential</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/channel-keys/{id}</code></td><td>Delete upstream credential</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/keys</code></td><td>List API keys</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/keys</code></td><td>Create API key</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/keys/{id}</code></td><td>Get API key</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/keys/{id}</code></td><td>Update API key</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/keys/{id}/disable</code></td><td>Disable API key</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/organizations</code></td><td>List visible organizations</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects</code></td><td>List projects</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/projects</code></td><td>Create project</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects/{id}</code></td><td>Get project</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/projects/{id}</code></td><td>Update project</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects/{id}/model-sets</code></td><td>List project model sets</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/projects/{id}/model-sets</code></td><td>Replace project model sets</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects/{id}/members</code></td><td>List project members and eligible users</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/projects/{id}/members/{user_id}</code></td><td>Assign a project member</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/projects/{id}/members/{user_id}</code></td><td>Remove a project member</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/projects/{id}/budget</code></td><td>Get project budget</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/projects/{id}/budget</code></td><td>Update project budget fields</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/logs</code></td><td>Call logs with filters</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/logs/{id}</code></td><td>Call log detail</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/logs/{id}/trace</code></td><td>Request trace events</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/usage/analytics</code></td><td>Usage analytics and CSV export</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/cost-optimization/summary</code></td><td>Evidence-bounded Prompt Cache savings summary</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/cost-optimization/trend</code></td><td>Daily Prompt Cache savings trend</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/cost-optimization/breakdown</code></td><td>Prompt Cache savings breakdown</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/cost-optimization/opportunities</code></td><td>Deterministic cost optimization review candidates</td></tr>
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
<tr><td class="method">GET</td><td><code>/api/v1/media-jobs/contract</code></td><td>Image V1 runtime and billing boundary</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/media-jobs</code></td><td>List image jobs in one authorized project</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/media-jobs/{id}</code></td><td>Inspect one image job</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/media-jobs/{id}/cancel</code></td><td>Request image job cancellation</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/media-jobs/{id}/content</code></td><td>Proxy authenticated image bytes</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/data-exchange/items</code></td><td>Data exchange approval candidates</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/items</code></td><td>Create data exchange approval candidate</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/data-exchange/items/{id}</code></td><td>Update data exchange approval status</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/data-exchange/exchanges</code></td><td>Data exchange export ledger</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/exchanges</code></td><td>Register generated exchange artifact</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/data-exchange/exchanges/{id}/activity</code></td><td>Exchange members, delivery history, and receipt history</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/exchanges/{id}/deliveries</code></td><td>Record exchange handoff or re-delivery</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/exchanges/{id}/receipts</code></td><td>Record X-Insight import receipt</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/exchanges/{id}/artifact-delete</code></td><td>Mark exchange artifact deleted in ledger</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}</code></td><td>X-Insight live feed connection status</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/pause</code></td><td>Pause live feed delivery without disabling its credential</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/resume</code></td><td>Resume live feed delivery</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/credentials</code></td><td>Create the initial staged feed-only credential</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/credentials/activate</code></td><td>Activate the installed initial credential while paused</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/credentials/{credential_id}/revoke</code></td><td>Terminally revoke one feed credential and pause</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/rotation/prepare</code></td><td>Prepare one staged replacement</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/rotation/activate</code></td><td>Atomically promote staged and revoke old</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/data-exchange/xinsight/connections/{project_id}/rotation/cancel</code></td><td>Terminally cancel staged replacement</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/settings</code></td><td>List deployment settings (platform admin)</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/settings/{key}</code></td><td>Update deployment setting (platform admin)</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/license</code></td><td>License status (platform admin)</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/license</code></td><td>Import offline license (platform admin)</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/users</code></td><td>List users</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/users</code></td><td>Create user</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/bootstrap/first-customer</code></td><td>Create first customer organization and zero wallet from a strict empty baseline</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/users/{id}</code></td><td>Update user</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/users/{id}</code></td><td>Delete user</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-prices</code></td><td>List model prices (platform, organization, or billing admin)</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-prices</code></td><td>Create/upsert model price (platform admin)</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-prices/{id}</code></td><td>Update model price (platform admin)</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/model-prices/{id}</code></td><td>Delete model price (platform admin)</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-names</code></td><td>List model names</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-registry</code></td><td>List model registry</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-registry</code></td><td>Create model registry entry</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-registry/{id}</code></td><td>Update model registry entry</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/invocation-optimization/contract</code></td><td>Read opt-in, canary and runtime-readiness contract</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/invocation-optimization/policies</code></td><td>List tenant-scoped invocation optimization policies</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/invocation-optimization/policies</code></td><td>Create a default-off policy draft</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/invocation-optimization/policies/{ref}</code></td><td>Read one exact policy revision</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/invocation-optimization/policies/{ref}/readiness</code></td><td>Read exact current-revision Canary admission gates without granting activation authority</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/invocation-optimization/policies/{ref}</code></td><td>Update a draft or rolled-back policy</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/invocation-optimization/policies/{ref}/validate</code></td><td>Validate policy without runtime mutation</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/invocation-optimization/policies/{ref}/shadow</code></td><td>Start metadata-only shadow evidence</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/invocation-optimization/policies/{ref}/canary</code></td><td>Start an evidence-gated maximum 10% canary</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/invocation-optimization/policies/{ref}/rollback</code></td><td>Stop future assignments and preserve evidence</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-catalog</code></td><td>Approved model catalog</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-aliases</code></td><td>List model aliases</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-aliases</code></td><td>Create model alias</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-aliases/{id}</code></td><td>Update model alias</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-sets</code></td><td>List model sets</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-sets</code></td><td>Create model set</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-sets/{id}/items</code></td><td>List model set items</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/model-sets/{id}/items</code></td><td>Replace model set items</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/org-model-prices</code></td><td>List own org multipliers; platform may select org</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/org-model-prices</code></td><td>Create/upsert org multiplier (platform admin)</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/org-model-prices/{id}</code></td><td>Update org multiplier (platform admin)</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/org-model-prices/{id}</code></td><td>Delete org multiplier (platform admin)</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/reconciliation</code></td><td>Usage reconciliation</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/provider-cost/reconciliations</code></td><td>Platform-authorized provider cost reconciliation without a caller-supplied amount</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/provider-cost/pending</code></td><td>List bounded unresolved provider attempts for one explicit organization</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/provider-cogs/offline-contract</code></td><td>Read the zero-persistence supplier-cost preview contract</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/provider-cogs/offline-preview</code></td><td>Evaluate bounded supplier-cost evidence without network, persistence or customer-ledger mutation</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/governance-simulation/offline-contract</code></td><td>Read the zero-persistence route/policy and Provider Change Firewall contract</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/governance-simulation/offline-preview</code></td><td>Compare immutable route, policy and Provider Pack snapshots without runtime promotion</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/runtime-changes</code></td><td>List bounded route-assignment releases</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/runtime-changes</code></td><td>Create an epoch- and Provider-Pack-pinned release draft</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/runtime-changes/slot</code></td><td>Read stable and canary assignment for one model</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/runtime-changes/{ref}</code></td><td>Read release evidence and lifecycle</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/runtime-changes/{ref}/validate</code></td><td>Revalidate same-clock snapshots and block breaking changes</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/runtime-changes/{ref}/shadow</code></td><td>Advance a validated release to zero-traffic shadow</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/runtime-changes/{ref}/canary</code></td><td>Start a deterministic bounded canary</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/runtime-changes/{ref}/promote</code></td><td>Promote the current canary to stable</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/runtime-changes/{ref}/rollback</code></td><td>Restore the parent stable assignment</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-call-authorizations/contract</code></td><td>Read narrow-only model-call authorization semantics</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/model-call-authorizations</code></td><td>List tenant-scoped model-call authorization policies</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-call-authorizations</code></td><td>Create a non-enforcing authorization draft</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-call-authorizations/{ref}/activate</code></td><td>Activate deny-first, allow-intersection model-call authorization</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/model-call-authorizations/{ref}/revoke</code></td><td>Revoke one model-call authorization policy</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/member-lifecycle/offline-contract</code></td><td>Read the IdP-neutral project-member lifecycle contract</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/member-lifecycle/offline-preview</code></td><td>Evaluate one bounded lifecycle event without user, membership, key, session or IdP mutation</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/agent-trust/offline-contract</code></td><td>Read the metadata-only Agent Trust, signed receipt and Governance Digital Twin contract</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/agent-trust/offline-preview</code></td><td>Verify bounded authority and signed receipt evidence without runtime trust or enforcement</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/provenance-passport/offline-contract</code></td><td>Read the exact Source provenance, Capture-Gap and Connector attestation contract</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/provenance-passport/offline-preview</code></td><td>Build a metadata-only passport without Feed, Connector registry or runtime mutation</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/risk-control/offline-contract</code></td><td>Read the bounded routing-advice and human-reviewed control-proposal contract</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/risk-control/offline-preview</code></td><td>Compose B8/B10/B11 evidence without traffic, promotion or runtime enforcement</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/limit-policies</code></td><td>List versioned shared RPM/RPS/TPM/concurrency policies</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/limit-policies</code></td><td>Create a disabled, shadow or enforced shared limit policy</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/limit-policies/{id}</code></td><td>Update a shared limit policy and increment its version</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/guardrail-policies</code></td><td>List versioned input and non-stream output guardrail policies</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/guardrail-policies</code></td><td>Create an audit, redact, or block guardrail policy</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/guardrail-policies/{id}</code></td><td>Update a guardrail policy with optimistic versioning</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/notification-events</code></td><td>Notification events</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/notification-events/{id}/acknowledge</code></td><td>Acknowledge notification event</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/notification-events/{id}/resolve</code></td><td>Resolve notification event</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/notification-deliveries</code></td><td>Notification deliveries</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/notification-webhooks</code></td><td>Notification webhooks</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/notification-webhooks</code></td><td>Create notification webhook</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/notification-webhooks/{id}</code></td><td>Update notification webhook</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/notification-webhooks/{id}/verify-scope</code></td><td>Verify a quarantined notification webhook scope</td></tr>
<tr><td class="method">GET</td><td><code>/api/v1/sensitive-words</code></td><td>List global input blocking words (platform admin)</td></tr>
<tr><td class="method">POST</td><td><code>/api/v1/sensitive-words</code></td><td>Add global input blocking words (platform admin)</td></tr>
<tr><td class="method">PUT</td><td><code>/api/v1/sensitive-words</code></td><td>Replace global input blocking words (platform admin)</td></tr>
<tr><td class="method">DELETE</td><td><code>/api/v1/sensitive-words</code></td><td>Delete global input blocking words (platform admin)</td></tr>
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
	mux.Handle("GET /api/v1/provider-packs", platformRoute(adminHandler.ListProviderPacks))
	mux.Handle("GET /api/v1/routing/preview", platformRoute(adminHandler.PreviewRouting))

	mux.Handle("GET /api/v1/keys", projectReadRoute(adminHandler.ListAPIKeys))
	mux.Handle("POST /api/v1/keys", projectWriteRoute(adminHandler.CreateAPIKey))
	mux.Handle("GET /api/v1/keys/{id}", projectReadRoute(adminHandler.GetAPIKey))
	mux.Handle("PUT /api/v1/keys/{id}", roleRoute(adminHandler.UpdateAPIKey, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	mux.Handle("POST /api/v1/keys/{id}/disable", roleRoute(adminHandler.DisableAPIKey, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper))

	mux.Handle("GET /api/v1/organizations", usageRoute(adminHandler.ListOrganizations))
	mux.Handle("POST /api/v1/bootstrap/first-customer", platformRoute(adminHandler.BootstrapFirstCustomer))
	mux.Handle("GET /api/v1/projects", projectListRoute(adminHandler.ListProjects))
	mux.Handle("POST /api/v1/projects", orgAdminRoute(adminHandler.CreateProject))
	mux.Handle("GET /api/v1/projects/{id}", projectReadRoute(adminHandler.GetProject))
	mux.Handle("PUT /api/v1/projects/{id}", roleRoute(adminHandler.UpdateProject, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	mux.Handle("GET /api/v1/projects/{id}/model-sets", licensedRoute(licensing.FeatureModelGovernance, projectReadRoute)(adminHandler.ListProjectModelSets))
	mux.Handle("PUT /api/v1/projects/{id}/model-sets", licensedRoute(licensing.FeatureModelGovernance, func(h http.HandlerFunc) http.Handler {
		return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin)
	})(adminHandler.ReplaceProjectModelSets))
	mux.Handle("GET /api/v1/projects/{id}/members", projectManagementRoute(adminHandler.ListProjectMembers))
	mux.Handle("PUT /api/v1/projects/{id}/members/{user_id}", projectManagementRoute(adminHandler.UpsertProjectMember))
	mux.Handle("DELETE /api/v1/projects/{id}/members/{user_id}", projectManagementRoute(adminHandler.DeleteProjectMember))
	mux.Handle("GET /api/v1/projects/{id}/budget", projectBudgetRoute(adminHandler.GetProjectBudget))
	mux.Handle("PUT /api/v1/projects/{id}/budget", projectBudgetRoute(adminHandler.UpdateProjectBudget))

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
	mux.Handle("GET /api/v1/dashboard/stats", dashboardRoute(adminHandler.DashboardStats))
	mux.Handle("GET /api/v1/usage/analytics", usageRoute(adminHandler.UsageAnalytics))
	registerCostOptimizationRoutes(mux, adminHandler, pricingReadRoute)
	mux.Handle("GET /api/v1/internal-status", licensedRoute(licensing.FeatureNotifications, orgAdminRoute)(adminHandler.InternalStatus))
	mux.Handle("GET /api/v1/runtime/status", roleRoute(adminHandler.RuntimeStatus, auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	mux.Handle("GET /api/v1/error-docs", usageRoute(adminHandler.ListErrorDocs))

	mux.Handle("GET /api/v1/reconciliation", billingRoute(adminHandler.ListReconciliation))
	registerProviderCostRoutes(mux, adminHandler, platformRoute)
	registerProviderCOGSOfflineRoutes(mux, adminHandler, billingRoute)
	registerGovernanceSimulationRoutes(mux, adminHandler, platformRoute)
	registerRuntimeChangeRoutes(mux, adminHandler, platformRoute)
	registerModelAuthorizationRoutes(mux, adminHandler, orgAdminRoute)
	registerWorkloadApplicationRoutes(mux, adminHandler, orgAdminRoute)
	registerDeploymentNetworkRoutes(mux, adminHandler, deploymentNetworkRouteGuards{read: orgAdminRoute, write: platformRoute})
	registerMemberLifecycleRoutes(mux, adminHandler, orgAdminRoute)
	registerEnterpriseDirectoryRoutes(mux, adminHandler, orgAdminRoute)
	registerEnterpriseFinanceRoutes(mux, adminHandler, enterpriseFinanceRouteGuards{
		read: pricingReadRoute, write: billingRoute,
	})
	registerAgentTrustRoutes(mux, adminHandler, orgAdminRoute)
	registerProvenancePassportRoutes(mux, adminHandler, orgAdminRoute)
	registerRiskControlRoutes(mux, adminHandler, orgAdminRoute)
	registerModelGovernanceRoutes(mux, adminHandler, modelGovernanceRouteGuards{
		organization: licensedRoute(licensing.FeatureModelGovernance, orgAdminRoute),
		projectRead:  licensedRoute(licensing.FeatureModelGovernance, projectReadRoute),
		modelSetRead: licensedRoute(licensing.FeatureModelGovernance, func(h http.HandlerFunc) http.Handler {
			return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin)
		}),
	})
	registerInvocationOptimizationRoutes(
		mux,
		adminHandler,
		licensedRoute(licensing.FeatureModelGovernance, orgAdminRoute),
	)
	registerPricingRoutes(mux, adminHandler, pricingRouteGuards{read: pricingReadRoute, write: platformRoute})
	registerLimitPolicyRoutes(mux, adminHandler, limitPolicyRouteGuards{
		read: func(h http.HandlerFunc) http.Handler {
			return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
		},
		write: orgAdminRoute,
	})
	registerGuardrailPolicyRoutes(mux, adminHandler, guardrailPolicyRouteGuards{
		read: licensedRoute(licensing.FeatureGuardrails, func(h http.HandlerFunc) http.Handler {
			return roleRoute(h, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
		}),
		write: licensedRoute(licensing.FeatureGuardrails, orgAdminRoute),
	})

	mux.Handle("GET /api/v1/users", orgAdminRoute(adminHandler.ListUsers))
	mux.Handle("POST /api/v1/users", orgAdminRoute(adminHandler.CreateUser))
	mux.Handle("PUT /api/v1/users/{id}", orgAdminRoute(adminHandler.UpdateUser))
	mux.Handle("DELETE /api/v1/users/{id}", orgAdminRoute(adminHandler.DeleteUser))

	mux.Handle("GET /api/v1/grants", billingRoute(adminHandler.ListGrants))
	mux.Handle("POST /api/v1/grants", billingRoute(adminHandler.CreateGrant))

	notificationEventReadLicensedRoute := licensedRoute(licensing.FeatureNotifications, notificationEventReadRoute)
	notificationEventWriteLicensedRoute := licensedRoute(licensing.FeatureNotifications, notificationEventWriteRoute)
	notificationOperatorLicensedRoute := licensedRoute(licensing.FeatureNotifications, notificationOperatorRoute)
	mux.Handle("GET /api/v1/notification-events", notificationEventReadLicensedRoute(adminHandler.ListNotificationEvents))
	mux.Handle("POST /api/v1/notification-events/{id}/acknowledge", notificationEventWriteLicensedRoute(adminHandler.AcknowledgeNotificationEvent))
	mux.Handle("POST /api/v1/notification-events/{id}/resolve", notificationEventWriteLicensedRoute(adminHandler.ResolveNotificationEvent))
	mux.Handle("GET /api/v1/notification-deliveries", notificationEventReadLicensedRoute(adminHandler.ListNotificationDeliveries))
	mux.Handle("GET /api/v1/notification-webhooks", notificationOperatorLicensedRoute(adminHandler.ListNotificationWebhooks))
	mux.Handle("POST /api/v1/notification-webhooks", notificationOperatorLicensedRoute(adminHandler.CreateNotificationWebhook))
	mux.Handle("PUT /api/v1/notification-webhooks/{id}", notificationOperatorLicensedRoute(adminHandler.UpdateNotificationWebhook))
	mux.Handle("POST /api/v1/notification-webhooks/{id}/verify-scope", notificationOperatorLicensedRoute(adminHandler.VerifyNotificationWebhookScope))

	mux.Handle("GET /api/v1/audit-logs", roleRoute(adminHandler.ListAuditLogs, auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	mediaReadRoute := licensedRoute(licensing.FeatureGateway, projectReadRoute)
	mediaWriteRoute := licensedRoute(licensing.FeatureGateway, projectWriteRoute)
	mux.Handle("GET /api/v1/media-jobs/contract", mediaReadRoute(mediaHandler.Contract))
	mux.Handle("GET /api/v1/media-jobs", mediaReadRoute(mediaHandler.AdminList))
	mux.Handle("GET /api/v1/media-jobs/{id}", mediaReadRoute(mediaHandler.AdminGet))
	mux.Handle("GET /api/v1/media-jobs/{id}/content", mediaReadRoute(mediaHandler.AdminContent))
	mux.Handle("POST /api/v1/media-jobs/{id}/cancel", mediaWriteRoute(mediaHandler.AdminCancel))
	dataExchangeRoute := licensedRoute(licensing.FeatureDataExchange, platformRoute)
	mux.Handle("GET /api/v1/data-exchange/items", dataExchangeRoute(adminHandler.ListDataExchangeItems))
	mux.Handle("POST /api/v1/data-exchange/items", dataExchangeRoute(adminHandler.CreateDataExchangeItem))
	mux.Handle("PUT /api/v1/data-exchange/items/{id}", dataExchangeRoute(adminHandler.UpdateDataExchangeItem))
	mux.Handle("GET /api/v1/data-exchange/exchanges", dataExchangeRoute(adminHandler.ListDataExchanges))
	mux.Handle("POST /api/v1/data-exchange/exchanges", dataExchangeRoute(adminHandler.CreateDataExchange))
	mux.Handle("GET /api/v1/data-exchange/exchanges/{id}/activity", dataExchangeRoute(adminHandler.GetDataExchangeActivity))
	mux.Handle("POST /api/v1/data-exchange/exchanges/{id}/deliveries", dataExchangeRoute(adminHandler.CreateDataExchangeDelivery))
	mux.Handle("POST /api/v1/data-exchange/exchanges/{id}/receipts", dataExchangeRoute(adminHandler.CreateDataExchangeReceipt))
	mux.Handle("POST /api/v1/data-exchange/exchanges/{id}/artifact-delete", dataExchangeRoute(adminHandler.DeleteDataExchangeArtifact))
	registerXInsightFeedControlRoutes(mux, adminHandler, xInsightFeedControlRouteGuards{
		safety:    platformRoute,
		expansion: dataExchangeRoute,
	})

	registerGlobalControlRoutes(mux, adminHandler, platformRoute, licensedRoute(licensing.FeatureGuardrails, platformRoute))
	mux.Handle("POST /api/v1/channels/{id}/test", licensedRoute(licensing.FeatureGateway, platformRoute)(adminHandler.TestChannel))
	mux.Handle("GET /api/v1/wallet/transactions/export", billingRoute(adminHandler.ExportWalletTransactions))
	metricsHandler := middleware.MetricsMiddleware(mux)
	corsHandler := middleware.CORSWithOptions(middleware.CORSOptions{
		AllowedOrigins: cfg.AdminCORSAllowedOrigins,
	})(metricsHandler)
	identityHandler := middleware.OperationIdentityWithSecret(cfg.APIKeyHashSecret)(corsHandler)
	securityHandler := middleware.SecurityHeaders(identityHandler)

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
	stopSettlementRecovery()
	stopCompletedFeedRetention()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSecs)*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	if stopAsyncMedia != nil {
		stopAsyncMedia()
		select {
		case <-asyncMediaDone:
		case <-ctx.Done():
			log.Printf("Async-media worker did not stop before shutdown timeout")
		}
	}
	if err := gateway.ShutdownStreamAdvisoryScheduler(ctx); err != nil {
		log.Printf("Guardrail advisory workers did not flush before shutdown timeout: %v", err)
	}
	if otelExporter != nil {
		if err := otelExporter.Close(ctx); err != nil {
			log.Printf("OTLP trace exporter did not flush before shutdown timeout: %v", err)
		}
		stats := otelExporter.Stats()
		log.Printf(
			"OTLP trace exporter stopped: queued=%d exported=%d partial_rejected=%d dropped=%d failures=%d retries=%d",
			stats.Queued,
			stats.Exported,
			stats.PartialRejected,
			stats.DroppedRestricted+stats.DroppedInvalid+stats.DroppedQueueFull+stats.DroppedAfterClose,
			stats.ExportFailures,
			stats.Retries,
		)
	}
	stopOutbox()
	select {
	case <-outboxDone:
	case <-ctx.Done():
		log.Printf("Internal outbox workers did not stop before shutdown timeout")
	}
	select {
	case <-completedFeedRetentionDone:
	case <-ctx.Done():
		log.Printf("Completed-execution retention worker did not stop before shutdown timeout")
	}
	log.Println("Server exited")
}

func asyncMediaWorkerID(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	var portable strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '.', r == '_', r == ':', r == '-':
			portable.WriteRune(r)
		default:
			portable.WriteByte('-')
		}
		if portable.Len() >= 96 {
			break
		}
	}
	if portable.Len() == 0 {
		portable.WriteString("unirouter")
	}
	return fmt.Sprintf("%s:%d", portable.String(), os.Getpid())
}

func applyReleaseTrust(cfg *config.Config) (releasetrust.Bundle, error) {
	if cfg == nil {
		return releasetrust.Bundle{}, fmt.Errorf("configuration is required")
	}
	bundle, err := releasetrust.Resolve(cfg.LicensePublicKeys, cfg.UpgradePublicKeys)
	if err != nil {
		return releasetrust.Bundle{}, err
	}
	if bundle.Mode == releasetrust.ModeRelease && strings.TrimSpace(cfg.LicensePublicKey) != "" {
		return releasetrust.Bundle{}, fmt.Errorf("release binaries forbid the legacy runtime LICENSE_PUBLIC_KEY trust root")
	}
	if err := validateReleaseModeConfig(cfg, bundle.Mode); err != nil {
		return releasetrust.Bundle{}, fmt.Errorf("release security configuration: %w", err)
	}
	cfg.LicensePublicKeys = bundle.LicensePublicKeys
	cfg.UpgradePublicKeys = bundle.UpgradePublicKeys
	return bundle, nil
}

func validateReleaseModeConfig(cfg *config.Config, trustMode string) error {
	if trustMode != releasetrust.ModeRelease {
		return nil
	}
	return cfg.ValidateReleaseSecrets()
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

func runLicenseRefreshLoop(ctx context.Context, handler *admin.Handler, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := handler.RefreshStoredCommercialLicense(ctx); err != nil {
				log.Printf("Commercial license refresh failed: %v", err)
			}
		}
	}
}

// loadSensitiveWords loads the versioned deployment-wide input blocking policy.
func loadSensitiveWords(ctx context.Context, pool *pgxpool.Pool) ([]string, time.Time, error) {
	var value string
	var updatedAt time.Time
	err := pool.QueryRow(ctx,
		`SELECT value, updated_at FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&value, &updatedAt)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read sensitive_words: %w", err)
	}
	var words []string
	if err := json.Unmarshal([]byte(value), &words); err != nil {
		return nil, time.Time{}, fmt.Errorf("parse sensitive_words: %w", err)
	}
	if updatedAt.IsZero() {
		return nil, time.Time{}, fmt.Errorf("sensitive_words has no policy version")
	}
	return words, updatedAt, nil
}

// startSensitiveWordsReloadLoop keeps each gateway instance converged with DB-backed settings.
func startSensitiveWordsReloadLoop(pool *pgxpool.Pool, sensitiveFilter *filter.DFAFilter, interval time.Duration) {
	if sensitiveFilter == nil || interval <= 0 {
		return
	}
	reload := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		words, version, err := loadSensitiveWords(ctx, pool)
		if err != nil {
			log.Printf("[filter] failed to reload sensitive_words: %v", err)
			return
		}
		if sensitiveFilter.UpdateWordsVersioned(words, version) {
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
	// Release identity is immutable build evidence. Environment values are a
	// source-build/dev fallback only and must not override -ldflags metadata.
	if value := strings.TrimSpace(injected); value != "" {
		return value
	}
	return strings.TrimSpace(configured)
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
