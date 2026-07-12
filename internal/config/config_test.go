package config

import (
	"os"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/traceutil"
)

func TestValidate_MissingJWTSecret(t *testing.T) {
	cfg := &Config{
		JWTSecret:        "",
		APIKeyHashSecret: "api-key-secret",
		EncryptionKey:    "0123456789abcdef0123456789abcdef",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty JWT_SECRET")
	}
}

func TestValidate_ShortEncryptionKey(t *testing.T) {
	cfg := &Config{
		JWTSecret:        "secret",
		APIKeyHashSecret: "api-key-secret",
		EncryptionKey:    "short",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for short ENCRYPTION_KEY")
	}
}

func TestValidate_MissingAPIKeyHashSecret(t *testing.T) {
	cfg := &Config{
		JWTSecret:        "secret",
		APIKeyHashSecret: "",
		EncryptionKey:    "0123456789abcdef0123456789abcdef",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty API_KEY_HASH_SECRET")
	}
}

func TestValidate_Exact32ByteKey(t *testing.T) {
	cfg := &Config{
		JWTSecret:        "secret",
		APIKeyHashSecret: "api-key-secret",
		EncryptionKey:    "0123456789abcdef0123456789abcdef", // 32 bytes
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LongEncryptionKey(t *testing.T) {
	cfg := &Config{
		JWTSecret:        "secret",
		APIKeyHashSecret: "api-key-secret",
		EncryptionKey:    "0123456789abcdef0123456789abcdefEXTRA",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for >32 byte ENCRYPTION_KEY")
	}
}

func TestValidate_NegativeLicenseExpiryWarningDays(t *testing.T) {
	cfg := &Config{
		JWTSecret:                "secret",
		APIKeyHashSecret:         "api-key-secret",
		EncryptionKey:            "0123456789abcdef0123456789abcdef",
		LicenseExpiryWarningDays: -1,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative LICENSE_EXPIRY_WARNING_DAYS")
	}
}

func TestValidate_OIDCEnabledRequiresProviderConfig(t *testing.T) {
	cfg := &Config{
		JWTSecret:                "secret",
		APIKeyHashSecret:         "api-key-secret",
		EncryptionKey:            "0123456789abcdef0123456789abcdef",
		LicenseExpiryWarningDays: 30,
		OIDCEnabled:              true,
		OIDCDefaultRole:          "viewer",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for incomplete OIDC config")
	}
}

func TestValidate_RuntimeExposureConfig(t *testing.T) {
	cfg := &Config{
		JWTSecret:                "secret",
		APIKeyHashSecret:         "api-key-secret",
		EncryptionKey:            "0123456789abcdef0123456789abcdef",
		LicenseExpiryWarningDays: 30,
		AdminCORSAllowedOrigins:  []string{"https://console.example.com", "http://localhost:3000/"},
		MetricsAllowedCIDRs:      []string{"10.0.0.0/8"},
		LoginRateLimitWindowSec:  60,
		LoginRateLimitIP:         30,
		LoginRateLimitUser:       10,
		PrometheusURL:            "http://prometheus:9090",
		GrafanaURL:               "https://grafana.example.com",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidRuntimeExposureConfig(t *testing.T) {
	base := func() *Config {
		return &Config{
			JWTSecret:                "secret",
			APIKeyHashSecret:         "api-key-secret",
			EncryptionKey:            "0123456789abcdef0123456789abcdef",
			LicenseExpiryWarningDays: 30,
			LoginRateLimitEnabled:    true,
			LoginRateLimitWindowSec:  60,
		}
	}

	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "invalid origin",
			mutate: func(cfg *Config) {
				cfg.AdminCORSAllowedOrigins = []string{"console.example.com"}
			},
		},
		{
			name: "wildcard origin",
			mutate: func(cfg *Config) {
				cfg.AdminCORSAllowedOrigins = []string{"*"}
			},
		},
		{
			name: "origin with path",
			mutate: func(cfg *Config) {
				cfg.AdminCORSAllowedOrigins = []string{"https://console.example.com/admin"}
			},
		},
		{
			name: "origin with unsupported scheme",
			mutate: func(cfg *Config) {
				cfg.AdminCORSAllowedOrigins = []string{"ftp://console.example.com"}
			},
		},
		{
			name: "invalid metrics cidr",
			mutate: func(cfg *Config) {
				cfg.MetricsAllowedCIDRs = []string{"not-a-cidr"}
			},
		},
		{
			name: "invalid login window",
			mutate: func(cfg *Config) {
				cfg.LoginRateLimitWindowSec = 0
			},
		},
		{
			name: "invalid ip limit",
			mutate: func(cfg *Config) {
				cfg.LoginRateLimitIP = -1
			},
		},
		{
			name: "invalid user limit",
			mutate: func(cfg *Config) {
				cfg.LoginRateLimitUser = -1
			},
		},
		{
			name: "invalid shutdown timeout",
			mutate: func(cfg *Config) {
				cfg.ShutdownTimeoutSecs = -1
			},
		},
		{
			name: "invalid trace sample rate",
			mutate: func(cfg *Config) {
				cfg.TraceEventSampleRate = 1.5
			},
		},
		{
			name: "invalid trace retention",
			mutate: func(cfg *Config) {
				cfg.TraceEventRetentionDays = -1
			},
		},
		{
			name: "invalid log retention",
			mutate: func(cfg *Config) {
				cfg.LogRetentionDays = -1
			},
		},
		{
			name: "invalid payload capture mode",
			mutate: func(cfg *Config) {
				cfg.PayloadCaptureMode = "raw"
			},
		},
		{
			name: "short evidence context trust secret",
			mutate: func(cfg *Config) {
				cfg.EvidenceContextTrustSecret = "short"
			},
		},
		{
			name: "invalid prometheus url",
			mutate: func(cfg *Config) {
				cfg.PrometheusURL = "prometheus:9090"
			},
		},
		{
			name: "invalid grafana url",
			mutate: func(cfg *Config) {
				cfg.GrafanaURL = "https://grafana.example.com/dashboard?orgId=1"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoad_OIDCEnvOverrides(t *testing.T) {
	os.Setenv("JWT_SECRET", "jwt-secret")
	os.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	os.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	os.Setenv("OIDC_ENABLED", "true")
	os.Setenv("OIDC_ISSUER_URL", "https://idp.example.com/")
	os.Setenv("OIDC_CLIENT_ID", "uniroute")
	os.Setenv("OIDC_CLIENT_SECRET", "client-secret")
	os.Setenv("OIDC_REDIRECT_URL", "https://console.example.com/api/v1/auth/oidc/callback")
	os.Setenv("OIDC_PROVIDER_NAME", "Example IDP")
	os.Setenv("OIDC_SCOPES", "openid,email,profile,groups")
	os.Setenv("OIDC_AUTO_PROVISION", "true")
	os.Setenv("OIDC_DEFAULT_ORG_ID", "7")
	os.Setenv("OIDC_DEFAULT_ROLE", "developer")
	defer func() {
		for _, key := range []string{
			"JWT_SECRET", "API_KEY_HASH_SECRET", "ENCRYPTION_KEY", "OIDC_ENABLED", "OIDC_ISSUER_URL",
			"OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_REDIRECT_URL", "OIDC_PROVIDER_NAME", "OIDC_SCOPES",
			"OIDC_AUTO_PROVISION", "OIDC_DEFAULT_ORG_ID", "OIDC_DEFAULT_ROLE",
		} {
			os.Unsetenv(key)
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.OIDCEnabled || cfg.OIDCIssuerURL != "https://idp.example.com" || cfg.OIDCProviderName != "Example IDP" {
		t.Fatalf("unexpected oidc config: %#v", cfg)
	}
	if cfg.OIDCDefaultOrgID != 7 || cfg.OIDCDefaultRole != "developer" {
		t.Fatalf("unexpected oidc defaults: org=%d role=%s", cfg.OIDCDefaultOrgID, cfg.OIDCDefaultRole)
	}
	if len(cfg.OIDCScopes) != 4 || cfg.OIDCScopes[3] != "groups" {
		t.Fatalf("unexpected oidc scopes: %#v", cfg.OIDCScopes)
	}
}

func TestLoad_RuntimeExposureEnvOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("ADMIN_CORS_ALLOWED_ORIGINS", "https://console.example.com, http://localhost:3000")
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("METRICS_ALLOWED_CIDRS", "10.0.0.0/8,127.0.0.1/32")
	t.Setenv("LOGIN_RATE_LIMIT_ENABLED", "false")
	t.Setenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS", "120")
	t.Setenv("LOGIN_RATE_LIMIT_IP_REQUESTS", "50")
	t.Setenv("LOGIN_RATE_LIMIT_USER_REQUESTS", "7")
	t.Setenv("SHUTDOWN_TIMEOUT_SECONDS", "150")
	t.Setenv("TRACE_EVENTS_ENABLED", "false")
	t.Setenv("TRACE_EVENT_SAMPLE_RATE", "0.25")
	t.Setenv("TRACE_EVENT_RETENTION_DAYS", "14")
	t.Setenv("PAYLOAD_CAPTURE_MODE", "disabled")
	t.Setenv("EVIDENCE_CONTEXT_TRUST_SECRET", "shared-secret-123456")
	t.Setenv("LOG_RETENTION_DAYS", "60")
	t.Setenv("RUNTIME_ENVIRONMENT", "staging")
	t.Setenv("RUNTIME_DEPLOYMENT_MODE", "docker-compose")
	t.Setenv("RUNTIME_DEPLOYMENT_ROLE", "canary")
	t.Setenv("RUNTIME_VERSION", "2026.07.07")
	t.Setenv("RUNTIME_COMMIT", "abc1234")
	t.Setenv("PROMETHEUS_URL", "http://prometheus:9090/")
	t.Setenv("GRAFANA_URL", "https://grafana.example.com/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.AdminCORSAllowedOrigins; len(got) != 2 || got[0] != "https://console.example.com" || got[1] != "http://localhost:3000" {
		t.Fatalf("unexpected CORS origins: %#v", got)
	}
	if cfg.MetricsEnabled {
		t.Fatal("expected metrics to be disabled")
	}
	if got := cfg.MetricsAllowedCIDRs; len(got) != 2 || got[0] != "10.0.0.0/8" || got[1] != "127.0.0.1/32" {
		t.Fatalf("unexpected metrics CIDRs: %#v", got)
	}
	if cfg.LoginRateLimitEnabled {
		t.Fatal("expected login limiter disabled override")
	}
	if cfg.LoginRateLimitWindowSec != 120 || cfg.LoginRateLimitIP != 50 || cfg.LoginRateLimitUser != 7 {
		t.Fatalf("unexpected login limits: window=%d ip=%d user=%d", cfg.LoginRateLimitWindowSec, cfg.LoginRateLimitIP, cfg.LoginRateLimitUser)
	}
	if cfg.ShutdownTimeoutSecs != 150 {
		t.Fatalf("expected shutdown timeout 150, got %d", cfg.ShutdownTimeoutSecs)
	}
	if cfg.TraceEventsEnabled {
		t.Fatal("expected trace events disabled override")
	}
	if cfg.TraceEventSampleRate != 0.25 {
		t.Fatalf("expected trace sample rate 0.25, got %f", cfg.TraceEventSampleRate)
	}
	if cfg.TraceEventRetentionDays != 14 || cfg.LogRetentionDays != 60 {
		t.Fatalf("unexpected retention days: trace=%d log=%d", cfg.TraceEventRetentionDays, cfg.LogRetentionDays)
	}
	if cfg.PayloadCaptureMode != traceutil.PayloadCaptureModeDisabled {
		t.Fatalf("expected payload capture mode disabled, got %s", cfg.PayloadCaptureMode)
	}
	if cfg.EvidenceContextTrustSecret != "shared-secret-123456" {
		t.Fatalf("expected evidence context trust secret override, got %s", cfg.EvidenceContextTrustSecret)
	}
	if cfg.RuntimeEnvironment != "staging" || cfg.RuntimeDeploymentMode != "docker-compose" || cfg.RuntimeDeploymentRole != "canary" {
		t.Fatalf("unexpected runtime metadata: env=%s mode=%s role=%s", cfg.RuntimeEnvironment, cfg.RuntimeDeploymentMode, cfg.RuntimeDeploymentRole)
	}
	if cfg.RuntimeVersion != "2026.07.07" || cfg.RuntimeCommit != "abc1234" {
		t.Fatalf("unexpected runtime version metadata: version=%s commit=%s", cfg.RuntimeVersion, cfg.RuntimeCommit)
	}
	if cfg.PrometheusURL != "http://prometheus:9090" || cfg.GrafanaURL != "https://grafana.example.com" {
		t.Fatalf("unexpected observability urls: prometheus=%s grafana=%s", cfg.PrometheusURL, cfg.GrafanaURL)
	}
}

func TestLoad_RoutingPolicyEnvOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("MAX_RETRY_COUNT", "4")
	t.Setenv("CIRCUIT_BREAKER_THRESHOLD", "7")
	t.Setenv("CIRCUIT_BREAKER_RECOVER_SECONDS", "90")
	t.Setenv("CIRCUIT_BREAKER_HALF_OPEN_PERCENT", "25")
	t.Setenv("KEY_COOLDOWN_SECONDS", "45")
	t.Setenv("PROVIDER_AUTH_COOLDOWN_SECONDS", "1800")
	t.Setenv("PROVIDER_KEY_FAILURE_THRESHOLD", "6")
	t.Setenv("ROUTING_SNAPSHOT_REFRESH_SECONDS", "2")
	t.Setenv("ROUTING_SNAPSHOT_STALE_SECONDS", "40")
	t.Setenv("ROUTING_HALF_OPEN_PROBE_LEASE_SECONDS", "12")
	t.Setenv("ROUTING_MAX_CANDIDATES", "24")
	t.Setenv("ROUTING_MAX_SNAPSHOTS", "512")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load routing override: %v", err)
	}
	policy := cfg.RoutingPolicyConfig()
	if policy.MaxRetryCount != 4 || policy.CircuitBreakerThreshold != 7 || policy.CircuitBreakerRecoverSeconds != 90 ||
		policy.CircuitBreakerHalfOpenPct != 25 || policy.RateLimitCooldownSeconds != 45 ||
		policy.AuthFailureCooldownSeconds != 1800 || policy.KeyFailureThreshold != 6 ||
		policy.SnapshotRefreshSeconds != 2 || policy.SnapshotStaleSeconds != 40 ||
		policy.HalfOpenProbeLeaseSeconds != 12 || policy.MaxCandidateCount != 24 || policy.MaxSnapshotCount != 512 {
		t.Fatalf("unexpected effective routing policy: %#v", policy)
	}
}

func TestLoad_InvalidRoutingPolicyFailsClosed(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("MAX_RETRY_COUNT", "11")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid routing policy to fail startup validation")
	}
}

func TestLoad_StreamPolicyEnvOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("STREAM_MAX_SSE_LINE_BYTES", "4194304")
	t.Setenv("STREAM_WRITE_IDLE_TIMEOUT_SECONDS", "45")
	t.Setenv("STREAM_HEARTBEAT_SECONDS", "12")
	t.Setenv("STREAM_MAX_DURATION_SECONDS", "3600")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load stream override: %v", err)
	}
	policy := cfg.StreamPolicyConfig()
	if policy.MaxSSELineBytes != 4*1024*1024 || policy.WriteIdleTimeoutSeconds != 45 ||
		policy.HeartbeatSeconds != 12 || policy.MaxDurationSeconds != 3600 {
		t.Fatalf("unexpected effective stream policy: %#v", policy)
	}
}

func TestLoad_InvalidStreamPolicyFailsClosed(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("STREAM_WRITE_IDLE_TIMEOUT_SECONDS", "15")
	t.Setenv("STREAM_HEARTBEAT_SECONDS", "15")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid stream policy to fail startup validation")
	}
}

func TestLoad_WorkloadAdmissionEnvOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("WORKLOAD_GLOBAL_CAPACITY", "200")
	t.Setenv("WORKLOAD_ORG_CAPACITY", "20")
	t.Setenv("WORKLOAD_GLOBAL_SHORT_RESERVE", "20")
	t.Setenv("WORKLOAD_ORG_SHORT_RESERVE", "4")
	t.Setenv("WORKLOAD_INTERACTIVE_WEIGHT", "1")
	t.Setenv("WORKLOAD_STREAM_WEIGHT", "5")
	t.Setenv("WORKLOAD_EMBEDDING_WEIGHT", "2")
	t.Setenv("WORKLOAD_INTERACTIVE_MAX_WAIT_MS", "75")
	t.Setenv("WORKLOAD_STREAM_MAX_WAIT_MS", "300")
	t.Setenv("WORKLOAD_EMBEDDING_MAX_WAIT_MS", "80")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load workload override: %v", err)
	}
	options := cfg.WorkloadAdmissionOptions()
	if options.GlobalCapacity != 200 || options.OrgCapacity != 20 ||
		options.GlobalShortReserve != 20 || options.OrgShortReserve != 4 ||
		options.Interactive.Weight != 1 || options.Stream.Weight != 5 || options.Embedding.Weight != 2 ||
		options.Interactive.MaxWait != 75*time.Millisecond || options.Stream.MaxWait != 300*time.Millisecond ||
		options.Embedding.MaxWait != 80*time.Millisecond {
		t.Fatalf("unexpected workload options: %#v", options)
	}
}

func TestLoad_InvalidWorkloadAdmissionFailsClosed(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("WORKLOAD_ORG_CAPACITY", "5")
	t.Setenv("WORKLOAD_ORG_SHORT_RESERVE", "2")
	t.Setenv("WORKLOAD_STREAM_WEIGHT", "4")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid workload admission configuration to fail startup validation")
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Clear all env vars to test defaults
	envVars := []string{
		"LISTEN_PORT", "LOG_LEVEL", "GIN_MODE", "SHUTDOWN_TIMEOUT_SECONDS", "DB_DSN", "DB_MAX_CONNECTIONS",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "JWT_SECRET", "API_KEY_HASH_SECRET", "ENCRYPTION_KEY",
		"TRACE_EVENTS_ENABLED", "TRACE_EVENT_SAMPLE_RATE", "TRACE_EVENT_RETENTION_DAYS", "LOG_RETENTION_DAYS",
		"PAYLOAD_CAPTURE_MODE", "EVIDENCE_CONTEXT_TRUST_SECRET",
		"STREAM_MAX_SSE_LINE_BYTES", "STREAM_WRITE_IDLE_TIMEOUT_SECONDS", "STREAM_HEARTBEAT_SECONDS", "STREAM_MAX_DURATION_SECONDS",
		"WORKLOAD_GLOBAL_CAPACITY", "WORKLOAD_ORG_CAPACITY", "WORKLOAD_GLOBAL_SHORT_RESERVE", "WORKLOAD_ORG_SHORT_RESERVE",
		"WORKLOAD_INTERACTIVE_WEIGHT", "WORKLOAD_STREAM_WEIGHT", "WORKLOAD_EMBEDDING_WEIGHT",
		"WORKLOAD_INTERACTIVE_MAX_WAIT_MS", "WORKLOAD_STREAM_MAX_WAIT_MS", "WORKLOAD_EMBEDDING_MAX_WAIT_MS",
		"RUNTIME_ENVIRONMENT", "RUNTIME_DEPLOYMENT_MODE",
		"RUNTIME_DEPLOYMENT_ROLE", "RUNTIME_VERSION", "RUNTIME_COMMIT", "PROMETHEUS_URL", "GRAFANA_URL",
		"LICENSE_PUBLIC_KEY", "LICENSE_EXPIRY_WARNING_DAYS", "MIN_FREEZE_AMOUNT", "FREEZE_SAFETY_FACTOR", "DEFAULT_KEY_RPM", "DEFAULT_KEY_QPS",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	// Load() will fail because JWT_SECRET is empty and ENCRYPTION_KEY is empty
	// But we can test that defaults are set before validation
	cfg := &Config{
		ListenPort:           getEnvInt("LISTEN_PORT", 8080),
		LogLevel:             getEnvStr("LOG_LEVEL", "info"),
		GinMode:              getEnvStr("GIN_MODE", "release"),
		ShutdownTimeoutSecs:  getEnvInt("SHUTDOWN_TIMEOUT_SECONDS", 120),
		DB_DSN:               getEnvStr("DB_DSN", "postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable"),
		DBMaxConnections:     getEnvInt("DB_MAX_CONNECTIONS", 25),
		RedisAddr:            getEnvStr("REDIS_ADDR", "localhost:6379"),
		TraceEventsEnabled:   getEnvBool("TRACE_EVENTS_ENABLED", true),
		TraceEventSampleRate: getEnvFloat("TRACE_EVENT_SAMPLE_RATE", 1),
		LogRetentionDays:     getEnvInt("LOG_RETENTION_DAYS", 30),
		TraceEventRetentionDays: getEnvInt(
			"TRACE_EVENT_RETENTION_DAYS",
			getEnvInt("LOG_RETENTION_DAYS", 30),
		),
		PayloadCaptureMode:           traceutil.NormalizePayloadCaptureMode(getEnvStr("PAYLOAD_CAPTURE_MODE", traceutil.PayloadCaptureModeMetadataOnly)),
		EvidenceContextTrustSecret:   getEnvStr("EVIDENCE_CONTEXT_TRUST_SECRET", ""),
		RuntimeEnvironment:           getEnvStr("RUNTIME_ENVIRONMENT", "unspecified"),
		RuntimeDeploymentMode:        getEnvStr("RUNTIME_DEPLOYMENT_MODE", "single-node"),
		RuntimeDeploymentRole:        getEnvStr("RUNTIME_DEPLOYMENT_ROLE", "stable"),
		MinFreezeAmount:              getEnvInt("MIN_FREEZE_AMOUNT", 10),
		FreezeSafetyFactor:           getEnvFloat("FREEZE_SAFETY_FACTOR", 1.5),
		DefaultKeyRPM:                getEnvInt("DEFAULT_KEY_RPM", 60),
		DefaultKeyQPS:                getEnvInt("DEFAULT_KEY_QPS", 10),
		StreamMaxSSELineBytes:        getEnvInt("STREAM_MAX_SSE_LINE_BYTES", 2*1024*1024),
		StreamWriteIdleTimeoutSecs:   getEnvInt("STREAM_WRITE_IDLE_TIMEOUT_SECONDS", 30),
		StreamHeartbeatSecs:          getEnvInt("STREAM_HEARTBEAT_SECONDS", 15),
		StreamMaxDurationSecs:        getEnvInt("STREAM_MAX_DURATION_SECONDS", 30*60),
		WorkloadGlobalCapacity:       getEnvInt("WORKLOAD_GLOBAL_CAPACITY", 100),
		WorkloadOrgCapacity:          getEnvInt("WORKLOAD_ORG_CAPACITY", 10),
		WorkloadGlobalShortReserve:   getEnvInt("WORKLOAD_GLOBAL_SHORT_RESERVE", 10),
		WorkloadOrgShortReserve:      getEnvInt("WORKLOAD_ORG_SHORT_RESERVE", 2),
		WorkloadInteractiveWeight:    getEnvInt("WORKLOAD_INTERACTIVE_WEIGHT", 1),
		WorkloadStreamWeight:         getEnvInt("WORKLOAD_STREAM_WEIGHT", 4),
		WorkloadEmbeddingWeight:      getEnvInt("WORKLOAD_EMBEDDING_WEIGHT", 1),
		WorkloadInteractiveMaxWaitMS: getEnvInt("WORKLOAD_INTERACTIVE_MAX_WAIT_MS", 50),
		WorkloadStreamMaxWaitMS:      getEnvInt("WORKLOAD_STREAM_MAX_WAIT_MS", 250),
		WorkloadEmbeddingMaxWaitMS:   getEnvInt("WORKLOAD_EMBEDDING_MAX_WAIT_MS", 50),
	}

	if cfg.ListenPort != 8080 {
		t.Fatalf("expected port 8080, got %d", cfg.ListenPort)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected log level 'info', got %s", cfg.LogLevel)
	}
	if cfg.GinMode != "release" {
		t.Fatalf("expected gin mode 'release', got %s", cfg.GinMode)
	}
	if cfg.ShutdownTimeoutSecs != 120 {
		t.Fatalf("expected shutdown timeout 120, got %d", cfg.ShutdownTimeoutSecs)
	}
	if cfg.DBMaxConnections != 25 {
		t.Fatalf("expected 25 max conns, got %d", cfg.DBMaxConnections)
	}
	if cfg.MinFreezeAmount != 10 {
		t.Fatalf("expected 10 min freeze, got %d", cfg.MinFreezeAmount)
	}
	if cfg.FreezeSafetyFactor != 1.5 {
		t.Fatalf("expected 1.5 safety factor, got %f", cfg.FreezeSafetyFactor)
	}
	if cfg.DefaultKeyRPM != 60 {
		t.Fatalf("expected 60 RPM, got %d", cfg.DefaultKeyRPM)
	}
	if cfg.DefaultKeyQPS != 10 {
		t.Fatalf("expected 10 QPS, got %d", cfg.DefaultKeyQPS)
	}
	if cfg.StreamMaxSSELineBytes != 2*1024*1024 || cfg.StreamWriteIdleTimeoutSecs != 30 ||
		cfg.StreamHeartbeatSecs != 15 || cfg.StreamMaxDurationSecs != 30*60 {
		t.Fatalf("unexpected stream defaults: %#v", cfg.StreamPolicyConfig())
	}
	workload := cfg.WorkloadAdmissionOptions()
	if workload.GlobalCapacity != 100 || workload.OrgCapacity != 10 || workload.Stream.Weight != 4 ||
		workload.GlobalShortReserve != 10 || workload.OrgShortReserve != 2 || workload.Stream.MaxWait != 250*time.Millisecond {
		t.Fatalf("unexpected workload defaults: %#v", workload)
	}
	if !cfg.TraceEventsEnabled || cfg.TraceEventSampleRate != 1 {
		t.Fatalf("expected trace events enabled with sample rate 1, got enabled=%v sample=%f", cfg.TraceEventsEnabled, cfg.TraceEventSampleRate)
	}
	if cfg.LogRetentionDays != 30 || cfg.TraceEventRetentionDays != 30 {
		t.Fatalf("expected default retention 30/30, got log=%d trace=%d", cfg.LogRetentionDays, cfg.TraceEventRetentionDays)
	}
	if cfg.PayloadCaptureMode != traceutil.PayloadCaptureModeMetadataOnly {
		t.Fatalf("expected default payload capture mode metadata_only, got %s", cfg.PayloadCaptureMode)
	}
	if cfg.EvidenceContextTrustSecret != "" {
		t.Fatalf("expected evidence context trust secret disabled by default, got %s", cfg.EvidenceContextTrustSecret)
	}
	if cfg.RuntimeEnvironment != "unspecified" || cfg.RuntimeDeploymentMode != "single-node" || cfg.RuntimeDeploymentRole != "stable" {
		t.Fatalf("unexpected runtime defaults: env=%s mode=%s role=%s", cfg.RuntimeEnvironment, cfg.RuntimeDeploymentMode, cfg.RuntimeDeploymentRole)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("LISTEN_PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	os.Setenv("LICENSE_PUBLIC_KEY", "test-public-key")
	os.Setenv("LICENSE_EXPIRY_WARNING_DAYS", "45")
	defer func() {
		os.Unsetenv("LISTEN_PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("API_KEY_HASH_SECRET")
		os.Unsetenv("ENCRYPTION_KEY")
		os.Unsetenv("LICENSE_PUBLIC_KEY")
		os.Unsetenv("LICENSE_EXPIRY_WARNING_DAYS")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenPort != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.ListenPort)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected 'debug', got %s", cfg.LogLevel)
	}
	if cfg.APIKeyHashSecret != "test-secret" {
		t.Fatalf("expected APIKeyHashSecret to default to JWT_SECRET, got %s", cfg.APIKeyHashSecret)
	}
	if cfg.LicensePublicKey != "test-public-key" {
		t.Fatalf("expected LicensePublicKey override, got %s", cfg.LicensePublicKey)
	}
	if cfg.LicenseExpiryWarningDays != 45 {
		t.Fatalf("expected LicenseExpiryWarningDays=45, got %d", cfg.LicenseExpiryWarningDays)
	}
}

func TestLoad_APIKeyHashSecretOverride(t *testing.T) {
	os.Setenv("JWT_SECRET", "jwt-secret")
	os.Setenv("API_KEY_HASH_SECRET", "api-key-hash-secret")
	os.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	defer func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("API_KEY_HASH_SECRET")
		os.Unsetenv("ENCRYPTION_KEY")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JWTSecret != "jwt-secret" {
		t.Fatalf("expected JWTSecret override, got %s", cfg.JWTSecret)
	}
	if cfg.APIKeyHashSecret != "api-key-hash-secret" {
		t.Fatalf("expected APIKeyHashSecret override, got %s", cfg.APIKeyHashSecret)
	}
}

func TestGetEnvStr_Fallback(t *testing.T) {
	os.Unsetenv("NONEXISTENT_KEY")
	v := getEnvStr("NONEXISTENT_KEY", "fallback")
	if v != "fallback" {
		t.Fatalf("expected 'fallback', got %s", v)
	}
}

func TestGetEnvStr_Value(t *testing.T) {
	os.Setenv("TEST_KEY", "test_value")
	defer os.Unsetenv("TEST_KEY")
	v := getEnvStr("TEST_KEY", "fallback")
	if v != "test_value" {
		t.Fatalf("expected 'test_value', got %s", v)
	}
}

func TestGetEnvInt_Fallback(t *testing.T) {
	os.Unsetenv("NONEXISTENT_INT")
	v := getEnvInt("NONEXISTENT_INT", 42)
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestGetEnvInt_Value(t *testing.T) {
	os.Setenv("TEST_INT", "123")
	defer os.Unsetenv("TEST_INT")
	v := getEnvInt("TEST_INT", 0)
	if v != 123 {
		t.Fatalf("expected 123, got %d", v)
	}
}

func TestGetEnvInt_InvalidValue(t *testing.T) {
	os.Setenv("TEST_BAD_INT", "not_a_number")
	defer os.Unsetenv("TEST_BAD_INT")
	v := getEnvInt("TEST_BAD_INT", 99)
	if v != 99 {
		t.Fatalf("expected fallback 99, got %d", v)
	}
}

func TestGetEnvFloat_Fallback(t *testing.T) {
	os.Unsetenv("NONEXISTENT_FLOAT")
	v := getEnvFloat("NONEXISTENT_FLOAT", 3.14)
	if v != 3.14 {
		t.Fatalf("expected 3.14, got %f", v)
	}
}

func TestGetEnvFloat_Value(t *testing.T) {
	os.Setenv("TEST_FLOAT", "2.718")
	defer os.Unsetenv("TEST_FLOAT")
	v := getEnvFloat("TEST_FLOAT", 0)
	if v != 2.718 {
		t.Fatalf("expected 2.718, got %f", v)
	}
}

func TestGetEnvBool_Fallback(t *testing.T) {
	os.Unsetenv("NONEXISTENT_BOOL")
	v := getEnvBool("NONEXISTENT_BOOL", true)
	if v != true {
		t.Fatal("expected true")
	}
}

func TestGetEnvBool_Value(t *testing.T) {
	os.Setenv("TEST_BOOL", "false")
	defer os.Unsetenv("TEST_BOOL")
	v := getEnvBool("TEST_BOOL", true)
	if v != false {
		t.Fatal("expected false")
	}
}

func TestGetEnvBool_InvalidValue(t *testing.T) {
	os.Setenv("TEST_BAD_BOOL", "notabool")
	defer os.Unsetenv("TEST_BAD_BOOL")
	v := getEnvBool("TEST_BAD_BOOL", true)
	if v != true {
		t.Fatal("expected fallback true")
	}
}
