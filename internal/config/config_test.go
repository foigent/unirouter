package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/modelrouting"
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
		JWTSecret:          "secret",
		APIKeyHashSecret:   "api-key-secret",
		EncryptionKey:      "0123456789abcdef0123456789abcdef", // 32 bytes
		FreezeSafetyFactor: "1",
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_BillingModeContract(t *testing.T) {
	base := Config{
		JWTSecret: "secret", APIKeyHashSecret: "api-key-secret",
		EncryptionKey: "0123456789abcdef0123456789abcdef", FreezeSafetyFactor: "1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("empty direct-construction mode should retain the observation default: %v", err)
	}
	base.BillingMode = billing.BillingModePrepaidEnforced
	if err := base.Validate(); err != nil {
		t.Fatalf("prepaid mode should remain supported: %v", err)
	}
	base.BillingMode = billing.BillingModeBudgetEnforced
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("budget mode must fail closed until budget admission is complete, got %v", err)
	}
	base.BillingMode = "Cost_Observation"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("billing mode parsing must be exact, got %v", err)
	}
}

func TestValidate_RequestPricingAuthorityMigrationGate(t *testing.T) {
	base := Config{
		JWTSecret: "secret", APIKeyHashSecret: "api-key-secret",
		EncryptionKey: "0123456789abcdef0123456789abcdef", FreezeSafetyFactor: "1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("empty direct construction should retain explicit legacy compatibility: %v", err)
	}
	base.RequestPricingAuthority = billing.RequestPricingAuthorityRateCard
	if err := base.Validate(); err != nil {
		t.Fatalf("strict Rate Card authority should be supported: %v", err)
	}
	base.RequestPricingAuthority = "auto"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "REQUEST_PRICING_AUTHORITY") {
		t.Fatalf("unknown authority must fail startup validation, got %v", err)
	}
}

func TestValidate_ModelRouteAuthority(t *testing.T) {
	base := Config{
		JWTSecret: "secret", APIKeyHashSecret: "api-key-secret",
		EncryptionKey: "0123456789abcdef0123456789abcdef", FreezeSafetyFactor: "1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("empty direct construction should use canonical route authority: %v", err)
	}
	for _, mode := range []modelrouting.Mode{modelrouting.ModeLegacy, modelrouting.ModeVerify} {
		base.ModelRouteAuthority = mode
		base.ModelRouteCompatibilityEnabled = true
		if err := base.Validate(); err != nil {
			t.Fatalf("mode %q should validate: %v", mode, err)
		}
	}
	base.ModelRouteAuthority = modelrouting.ModeCanonical
	base.ModelRouteCompatibilityEnabled = false
	if err := base.Validate(); err != nil {
		t.Fatalf("canonical mode should validate with compatibility disabled: %v", err)
	}
	base.ModelRouteCompatibilityEnabled = true
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "MODEL_ROUTE_COMPATIBILITY_ENABLED") {
		t.Fatalf("canonical mode must reject compatibility runtime, got %v", err)
	}
	for _, mode := range []modelrouting.Mode{modelrouting.ModeLegacy, modelrouting.ModeVerify} {
		base.ModelRouteAuthority = mode
		base.ModelRouteCompatibilityEnabled = false
		if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "MODEL_ROUTE_COMPATIBILITY_ENABLED") {
			t.Fatalf("mode %q must require explicit compatibility runtime, got %v", mode, err)
		}
	}
	base.ModelRouteCompatibilityEnabled = false
	for _, invalid := range []modelrouting.Mode{"auto", "Canonical", " canonical"} {
		base.ModelRouteAuthority = invalid
		if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "MODEL_ROUTE_AUTHORITY") {
			t.Fatalf("route authority %q must fail exact startup validation, got %v", invalid, err)
		}
	}
}

func TestLoadModelRouteCompatibilityIsStrictAndCanonicalByDefault(t *testing.T) {
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load canonical defaults: %v", err)
	}
	if cfg.ModelRouteAuthority != modelrouting.ModeCanonical || cfg.ModelRouteCompatibilityEnabled {
		t.Fatalf("unexpected model-route defaults: authority=%q compatibility=%t", cfg.ModelRouteAuthority, cfg.ModelRouteCompatibilityEnabled)
	}

	t.Setenv("MODEL_ROUTE_COMPATIBILITY_ENABLED", "yes")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MODEL_ROUTE_COMPATIBILITY_ENABLED") {
		t.Fatalf("non-exact compatibility boolean must fail startup, got %v", err)
	}
}

func TestValidateCanonicalRejectsAsyncMedia(t *testing.T) {
	cfg := &Config{
		JWTSecret: "secret", APIKeyHashSecret: "api-key-secret",
		EncryptionKey: "0123456789abcdef0123456789abcdef", FreezeSafetyFactor: "1",
		ModelRouteAuthority: modelrouting.ModeCanonical,
		AsyncMediaEnabled:   true,
		AsyncMediaS3Region:  "us-east-1", AsyncMediaS3Bucket: "canonical-media",
		AsyncMediaS3Prefix: "media", AsyncMediaLeaseSeconds: 300,
		AsyncMediaPurgeLeaseSeconds: 60, AsyncMediaPollMilliseconds: 500,
		AsyncMediaProviderTimeoutSecs: 180, AsyncMediaMaxAttempts: 3,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ASYNC_MEDIA_ENABLED") {
		t.Fatalf("canonical authority must reject legacy async-media routing, got %v", err)
	}
}

func TestLoadRejectsUnknownModelRouteAuthority(t *testing.T) {
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("MODEL_ROUTE_AUTHORITY", "auto")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MODEL_ROUTE_AUTHORITY") {
		t.Fatalf("unknown environment route authority must fail startup, got %v", err)
	}
}

func TestLoadRejectsNonExactModelRouteAuthority(t *testing.T) {
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("MODEL_ROUTE_AUTHORITY", "Canonical")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MODEL_ROUTE_AUTHORITY") {
		t.Fatalf("non-exact environment route authority must fail startup, got %v", err)
	}
}

func TestValidate_ResponseCacheIsDefaultOffAndRequiresCompleteKeySet(t *testing.T) {
	base := Config{
		JWTSecret:          "secret",
		APIKeyHashSecret:   "api-key-secret",
		EncryptionKey:      "0123456789abcdef0123456789abcdef",
		FreezeSafetyFactor: "1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("response cache must remain optional by default: %v", err)
	}
	if base.ResponseCacheConfigured() {
		t.Fatal("response cache must be disabled without the complete key set")
	}

	configured := base
	configured.ResponseCacheIndexKey = strings.Repeat("i", 32)
	configured.ResponseCacheEncryptionKey = strings.Repeat("e", 32)
	configured.ResponseCacheKeyVersion = "cache-key-v1"
	if err := configured.Validate(); err != nil {
		t.Fatalf("complete response-cache key set should validate: %v", err)
	}
	if !configured.ResponseCacheConfigured() {
		t.Fatal("complete response-cache key set should enable runtime readiness")
	}

	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing index key", func(cfg *Config) { cfg.ResponseCacheIndexKey = "" }},
		{"missing encryption key", func(cfg *Config) { cfg.ResponseCacheEncryptionKey = "" }},
		{"missing key version", func(cfg *Config) { cfg.ResponseCacheKeyVersion = "" }},
		{"short index key", func(cfg *Config) { cfg.ResponseCacheIndexKey = "short" }},
		{"short encryption key", func(cfg *Config) { cfg.ResponseCacheEncryptionKey = "short" }},
		{"non-portable version", func(cfg *Config) { cfg.ResponseCacheKeyVersion = "bad version" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := configured
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("expected incomplete or invalid response-cache key set to fail closed")
			}
		})
	}
}

func TestValidateReleaseSecretsRequires32BytesWithoutChangingDevelopmentValidation(t *testing.T) {
	cfg := &Config{
		JWTSecret:          "development-secret",
		APIKeyHashSecret:   "development-api-secret",
		EncryptionKey:      "0123456789abcdef0123456789abcdef",
		FreezeSafetyFactor: "1",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("development validation must remain compatible with non-empty legacy secrets: %v", err)
	}
	if err := cfg.ValidateReleaseSecrets(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("expected short release JWT secret rejection, got %v", err)
	}

	cfg.JWTSecret = strings.Repeat("j", 32)
	if err := cfg.ValidateReleaseSecrets(); err == nil || !strings.Contains(err.Error(), "API_KEY_HASH_SECRET") {
		t.Fatalf("expected short release API-key hash secret rejection, got %v", err)
	}

	cfg.APIKeyHashSecret = strings.Repeat("a", 32)
	if err := cfg.ValidateReleaseSecrets(); err == nil || !strings.Contains(err.Error(), "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET") {
		t.Fatalf("expected missing release Webhook signing secret rejection, got %v", err)
	}

	cfg.NotificationWebhookSigningSecret = strings.Repeat("w", 32)
	if err := cfg.ValidateReleaseSecrets(); err != nil {
		t.Fatalf("expected exact 32-byte release secrets to pass, got %v", err)
	}
}

func TestValidate_NotificationWebhookSigningSecret(t *testing.T) {
	cfg := &Config{
		JWTSecret:          "secret",
		APIKeyHashSecret:   "api-key-secret",
		EncryptionKey:      "0123456789abcdef0123456789abcdef",
		FreezeSafetyFactor: "1",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty development Webhook signing secret should remain optional: %v", err)
	}
	cfg.NotificationWebhookSigningSecret = "too-short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a short Webhook signing secret to fail validation")
	}
	cfg.NotificationWebhookSigningSecret = "0123456789abcdef0123456789abcdef"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected 32-byte Webhook signing secret validation error: %v", err)
	}
}

func TestValidatePayloadSafetyProfiles(t *testing.T) {
	base := Config{
		JWTSecret: "secret", APIKeyHashSecret: "api-key-secret",
		EncryptionKey: "0123456789abcdef0123456789abcdef",
	}
	limits := base.PayloadLimits()
	if limits.ChatRequestBytes != 8*1024*1024 || limits.EmbeddingsRequestBytes != 16*1024*1024 || limits.NonStreamResponseBytes != 16*1024*1024 {
		t.Fatalf("unexpected payload defaults: %+v", limits)
	}
	base.ChatRequestMaxBytes = 65 * 1024 * 1024
	if err := base.Validate(); err == nil {
		t.Fatal("expected platform hard max validation error")
	}
	base.ChatRequestMaxBytes = 8 * 1024 * 1024
	base.PayloadNodeInFlightBytes = 32 * 1024 * 1024
	base.PayloadOrgInFlightBytes = 64 * 1024 * 1024
	if err := base.Validate(); err == nil {
		t.Fatal("expected organization budget to be bounded by node budget")
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

func TestValidateRejectsOverflowingLicenseTiming(t *testing.T) {
	cfg := &Config{
		JWTSecret:                "secret",
		APIKeyHashSecret:         "api-key-secret",
		EncryptionKey:            "0123456789abcdef0123456789abcdef",
		FreezeSafetyFactor:       "1",
		LicenseExpiryWarningDays: 36501,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected excessive License warning window rejection")
	}
	cfg.LicenseExpiryWarningDays = 30
	cfg.LicenseRefreshSeconds = 3601
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected excessive License refresh interval rejection")
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
		TrustedProxyCIDRs:        []string{"172.30.0.10/32"},
		LoginRateLimitWindowSec:  60,
		LoginRateLimitIP:         30,
		LoginRateLimitUser:       10,
		PrometheusURL:            "http://prometheus:9090",
		GrafanaURL:               "https://grafana.example.com",
		FreezeSafetyFactor:       "1",
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
			name: "invalid trusted proxy cidr",
			mutate: func(cfg *Config) {
				cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}
			},
		},
		{
			name: "trust every proxy cidr",
			mutate: func(cfg *Config) {
				cfg.TrustedProxyCIDRs = []string{"0.0.0.0/0"}
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

func TestValidateAcceptsCanonicalAndAliasOIDCProjectRoles(t *testing.T) {
	roles := []string{
		auth.ProjectRoleAdmin,
		auth.ProjectRoleUser,
		auth.ProjectRoleViewer,
		auth.LegacyProjectRoleDeveloper,
		auth.RoleDeveloper,
		auth.RoleViewer,
	}
	base := func(role string) *Config {
		return &Config{
			JWTSecret:                "secret",
			APIKeyHashSecret:         "api-key-secret",
			EncryptionKey:            "0123456789abcdef0123456789abcdef",
			FreezeSafetyFactor:       "1",
			LicenseExpiryWarningDays: 30,
			OIDCEnabled:              true,
			OIDCIssuerURL:            "https://idp.example.test",
			OIDCClientID:             "unirouter",
			OIDCClientSecret:         "secret",
			OIDCRedirectURL:          "https://console.example.test/api/v1/auth/oidc/callback",
			OIDCDefaultRole:          role,
		}
	}
	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			if err := base(role).Validate(); err != nil {
				t.Fatalf("canonical/alias project role %q rejected: %v", role, err)
			}
		})
	}

	if err := base("project_owner").Validate(); err == nil || !strings.Contains(err.Error(), "OIDC_DEFAULT_ROLE is invalid") {
		t.Fatalf("invalid OIDC project role error = %v", err)
	}
}

func TestLoad_RuntimeExposureEnvOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("ADMIN_CORS_ALLOWED_ORIGINS", "https://console.example.com, http://localhost:3000")
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("METRICS_ALLOWED_CIDRS", "10.0.0.0/8,127.0.0.1/32")
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.30.0.10/32, 192.0.2.9/32")
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
	t.Setenv("UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET", "0123456789abcdef0123456789abcdef")
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
	if got := cfg.TrustedProxyCIDRs; len(got) != 2 || got[0] != "172.30.0.10/32" || got[1] != "192.0.2.9/32" {
		t.Fatalf("unexpected trusted proxy CIDRs: %#v", got)
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
	if cfg.NotificationWebhookSigningSecret != "0123456789abcdef0123456789abcdef" {
		t.Fatal("expected typed notification Webhook signing secret override")
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

func TestValidateOTelExportIsDefaultOffAndFailClosedWhenEnabled(t *testing.T) {
	base := func() *Config {
		return &Config{
			JWTSecret:          "secret",
			APIKeyHashSecret:   "api-key-secret",
			EncryptionKey:      "0123456789abcdef0123456789abcdef",
			FreezeSafetyFactor: "1",
			TraceEventsEnabled: true,
			OTelExportEnabled:  true,
			OTelExportEndpoint: "http://otel-collector:4318",
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("expected valid metadata-only exporter config: %v", err)
	}

	missingEndpoint := base()
	missingEndpoint.OTelExportEndpoint = ""
	if err := missingEndpoint.Validate(); err == nil || !strings.Contains(err.Error(), "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") {
		t.Fatalf("expected missing endpoint failure, got %v", err)
	}
	traceDisabled := base()
	traceDisabled.TraceEventsEnabled = false
	if err := traceDisabled.Validate(); err == nil || !strings.Contains(err.Error(), "TRACE_EVENTS_ENABLED") {
		t.Fatalf("expected trace source dependency failure, got %v", err)
	}
	credentialURL := base()
	credentialURL.OTelExportEndpoint = "http://user:secret@collector:4318/v1/traces"
	if err := credentialURL.Validate(); err == nil {
		t.Fatal("expected endpoint userinfo rejection")
	}
	oversizedQueue := base()
	oversizedQueue.OTelExportQueueSize = 65537
	if err := oversizedQueue.Validate(); err == nil {
		t.Fatal("expected bounded queue validation failure")
	}
}

func TestLoadOTelExportDefaultsAndOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if cfg.OTelExportEnabled {
		t.Fatal("OTLP export must be default off")
	}
	if cfg.OTelExportQueueSize != 256 || cfg.OTelExportBatchSize != 32 ||
		cfg.OTelExportFlushIntervalMS != 500 || cfg.OTelExportTimeoutMS != 2000 ||
		cfg.OTelExportMaxRetries != 2 {
		t.Fatalf("unexpected bounded exporter defaults: %+v", cfg.TelemetryExportConfig())
	}

	t.Setenv("UNIROUTE_OTEL_EXPORT_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://otel-collector:4318/v1/traces")
	t.Setenv("UNIROUTE_OTEL_EXPORT_QUEUE_SIZE", "64")
	t.Setenv("UNIROUTE_OTEL_EXPORT_BATCH_SIZE", "8")
	t.Setenv("UNIROUTE_OTEL_EXPORT_FLUSH_INTERVAL_MS", "100")
	t.Setenv("UNIROUTE_OTEL_EXPORT_TIMEOUT_MS", "750")
	t.Setenv("UNIROUTE_OTEL_EXPORT_MAX_RETRIES", "1")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load exporter overrides: %v", err)
	}
	effective := cfg.TelemetryExportConfig()
	if !effective.Enabled || effective.QueueSize != 64 || effective.BatchSize != 8 ||
		effective.FlushInterval != 100*time.Millisecond || effective.Timeout != 750*time.Millisecond ||
		effective.MaxRetries != 1 {
		t.Fatalf("unexpected exporter overrides: %+v", effective)
	}
}

func TestValidateAsyncMediaImageV1IsDefaultOffAndFailClosed(t *testing.T) {
	base := func() *Config {
		return &Config{
			JWTSecret: "secret", APIKeyHashSecret: "api-key-secret",
			EncryptionKey:                  "0123456789abcdef0123456789abcdef",
			FreezeSafetyFactor:             "1",
			RuntimeEnvironment:             "production",
			AsyncMediaEnabled:              true,
			ModelRouteAuthority:            modelrouting.ModeLegacy,
			ModelRouteCompatibilityEnabled: true,
			AsyncMediaAdmissionEnabled:     true,
			AsyncMediaS3Region:             "us-east-1",
			AsyncMediaS3Bucket:             "unirouter-media-prod",
			AsyncMediaS3Endpoint:           "https://s3.example.test",
			AsyncMediaS3Prefix:             "unirouter-media",
			AsyncMediaLeaseSeconds:         300,
			AsyncMediaPurgeLeaseSeconds:    60,
			AsyncMediaPollMilliseconds:     500,
			AsyncMediaProviderTimeoutSecs:  180,
			AsyncMediaMaxAttempts:          3,
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("valid async-media config: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"missing bucket": func(c *Config) { c.AsyncMediaS3Bucket = "" },
		"IP bucket":      func(c *Config) { c.AsyncMediaS3Bucket = "127.0.0.1" },
		"insecure production endpoint": func(c *Config) {
			c.AsyncMediaS3Endpoint = "http://s3.example.test"
		},
		"endpoint path": func(c *Config) {
			c.AsyncMediaS3Endpoint = "https://s3.example.test/private"
		},
		"endpoint userinfo": func(c *Config) {
			c.AsyncMediaS3Endpoint = "https://access:secret@s3.example.test"
		},
		"unsafe prefix":    func(c *Config) { c.AsyncMediaS3Prefix = "../media" },
		"invalid attempts": func(c *Config) { c.AsyncMediaMaxAttempts = 0 },
		"lease shorter than provider budget": func(c *Config) {
			c.AsyncMediaLeaseSeconds = c.AsyncMediaProviderTimeoutSecs + 59
		},
		"admission without runtime": func(c *Config) {
			c.AsyncMediaEnabled = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base()
			mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected async-media validation failure")
			}
		})
	}
	localHTTP := base()
	localHTTP.RuntimeEnvironment = "local"
	localHTTP.AsyncMediaS3Endpoint = "http://127.0.0.1:9000"
	if err := localHTTP.Validate(); err != nil {
		t.Fatalf("loopback S3-compatible development endpoint: %v", err)
	}
}

func TestLoadAsyncMediaDefaultsAndOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if cfg.AsyncMediaEnabled ||
		cfg.AsyncMediaAdmissionEnabled ||
		cfg.AsyncMediaS3Region != "us-east-1" ||
		cfg.AsyncMediaS3Prefix != "unirouter-media" ||
		cfg.AsyncMediaLeaseSeconds != 300 ||
		cfg.AsyncMediaPurgeLeaseSeconds != 60 ||
		cfg.AsyncMediaPollMilliseconds != 500 ||
		cfg.AsyncMediaProviderTimeoutSecs != 180 ||
		cfg.AsyncMediaMaxAttempts != 3 {
		t.Fatalf("unexpected async-media defaults: %+v", cfg)
	}

	t.Setenv("ASYNC_MEDIA_ENABLED", "true")
	t.Setenv("MODEL_ROUTE_AUTHORITY", "legacy")
	t.Setenv("MODEL_ROUTE_COMPATIBILITY_ENABLED", "true")
	t.Setenv("ASYNC_MEDIA_S3_REGION", "us-west-2")
	t.Setenv("ASYNC_MEDIA_S3_BUCKET", "unirouter-media-fixture")
	t.Setenv("ASYNC_MEDIA_S3_ENDPOINT", "https://s3.example.test")
	t.Setenv("ASYNC_MEDIA_S3_PREFIX", "tenant-media")
	t.Setenv("ASYNC_MEDIA_S3_FORCE_PATH_STYLE", "true")
	t.Setenv("ASYNC_MEDIA_WORKER_ID", "worker-1")
	t.Setenv("ASYNC_MEDIA_LEASE_SECONDS", "180")
	t.Setenv("ASYNC_MEDIA_PURGE_LEASE_SECONDS", "30")
	t.Setenv("ASYNC_MEDIA_POLL_MILLISECONDS", "250")
	t.Setenv("ASYNC_MEDIA_PROVIDER_TIMEOUT_SECONDS", "90")
	t.Setenv("ASYNC_MEDIA_MAX_ATTEMPTS", "2")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load async-media overrides: %v", err)
	}
	if !cfg.AsyncMediaEnabled ||
		cfg.AsyncMediaAdmissionEnabled ||
		cfg.AsyncMediaS3Region != "us-west-2" ||
		cfg.AsyncMediaS3Bucket != "unirouter-media-fixture" ||
		!cfg.AsyncMediaS3ForcePathStyle ||
		cfg.AsyncMediaWorkerID != "worker-1" ||
		cfg.AsyncMediaLeaseSeconds != 180 ||
		cfg.AsyncMediaMaxAttempts != 2 {
		t.Fatalf("unexpected async-media overrides: %+v", cfg)
	}

	t.Setenv("ASYNC_MEDIA_ADMISSION_ENABLED", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load active async-media admission: %v", err)
	}
	if !cfg.AsyncMediaEnabled || !cfg.AsyncMediaAdmissionEnabled {
		t.Fatalf("explicit admission enablement was not honored: %+v", cfg)
	}

	t.Setenv("ASYNC_MEDIA_ADMISSION_ENABLED", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load maintenance-mode async-media config: %v", err)
	}
	if !cfg.AsyncMediaEnabled || cfg.AsyncMediaAdmissionEnabled {
		t.Fatalf("runtime cleanup must remain enabled while admission is paused: %+v", cfg)
	}

	t.Setenv("MODEL_ROUTE_AUTHORITY", "canonical")
	t.Setenv("MODEL_ROUTE_COMPATIBILITY_ENABLED", "false")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ASYNC_MEDIA_ENABLED") {
		t.Fatalf("canonical Load must reject the legacy async-media runtime, got %v", err)
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

func TestLoad_GuardrailPolicyQuotaEnvOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GUARDRAIL_ORG_ACTIVE_POLICY_LIMIT", "128")
	t.Setenv("GUARDRAIL_PLATFORM_ACTIVE_POLICY_LIMIT", "64")
	t.Setenv("GUARDRAIL_CACHED_POLICY_LIMIT", "2048")
	t.Setenv("GUARDRAIL_CACHED_TENANT_LIMIT", "1024")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load guardrail quota override: %v", err)
	}
	options := cfg.GuardrailOptions()
	if options.MaxActivePoliciesPerOrg != 128 || options.MaxActivePlatformPolicies != 64 ||
		options.MaxCachedPolicies != 2048 || options.MaxCachedTenantScopes != 1024 {
		t.Fatalf("unexpected guardrail quota options: %#v", options)
	}
}

func TestLoad_InvalidGuardrailPolicyQuotaFailsClosed(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GUARDRAIL_ORG_ACTIVE_POLICY_LIMIT", "257")
	t.Setenv("GUARDRAIL_CACHED_POLICY_LIMIT", "128")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid guardrail quota configuration to fail startup validation")
	}
}

func TestLoadSourceFactsRequiresOwnEnablementAndStableProducer(t *testing.T) {
	for _, tc := range []struct {
		enabled, source string
		valid           bool
	}{
		{"false", "", true}, {"true", "", false}, {"true", "contains whitespace", false},
		{"true", "synthetic.unirouter:stable-1", true},
		{"yes", "synthetic.unirouter:stable-1", false},
		{"TRUE", "synthetic.unirouter:stable-1", false},
	} {
		t.Run(tc.enabled+"/"+tc.source, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "jwt-secret")
			t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
			t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("COMPLETED_EXECUTION_FEED_ENABLED", "false")
			t.Setenv("SOURCE_FACTS_ENABLED", tc.enabled)
			t.Setenv("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", tc.source)
			cfg, err := Load()
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid source identity was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SourceFactsEnabled != (tc.enabled == "true") || cfg.CompletedExecutionFeedEnabled {
				t.Fatal("source and I-01 enablement were conflated")
			}
		})
	}
}

func TestLoadCompletedExecutionFeedRequiresStableSourceIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		valid  bool
	}{
		{name: "missing", source: "", valid: false},
		{name: "invalid", source: "contains whitespace", valid: false},
		{name: "valid", source: "dev.unirouter:stable-1", valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "jwt-secret")
			t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
			t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("COMPLETED_EXECUTION_FEED_ENABLED", "true")
			t.Setenv("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", test.source)

			cfg, err := Load()
			if !test.valid {
				if err == nil {
					t.Fatal("expected enabled feed without a valid source identity to fail")
				}
				return
			}
			if err != nil {
				t.Fatalf("load enabled completed-execution feed: %v", err)
			}
			if !cfg.CompletedExecutionFeedEnabled || cfg.CompletedExecutionFeedSourceInstance != test.source {
				t.Fatalf("unexpected completed-execution feed configuration: %#v", cfg)
			}
		})
	}
}

func TestLoadCompletedExecutionFeedReferenceMode(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		enabled string
		mode    string
		valid   bool
	}{
		{name: "default legacy", enabled: "false", mode: "", valid: true},
		{name: "explicit legacy", enabled: "false", mode: "legacy", valid: true},
		{name: "shadow", enabled: "true", mode: "shadow", valid: true},
		{name: "neutral canary", enabled: "true", mode: "neutral_canary", valid: true},
		{name: "unknown", enabled: "true", mode: "canary", valid: false},
		{name: "whitespace malformed", enabled: "true", mode: " shadow ", valid: false},
		{name: "shadow requires enabled feed", enabled: "false", mode: "shadow", valid: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "jwt-secret")
			t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
			t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("COMPLETED_EXECUTION_FEED_ENABLED", testCase.enabled)
			t.Setenv("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "dev.unirouter:stable-1")
			t.Setenv("COMPLETED_EXECUTION_FEED_REFERENCE_MODE", testCase.mode)

			cfg, err := Load()
			if !testCase.valid {
				if err == nil {
					t.Fatal("expected invalid Reference mode to fail startup validation")
				}
				return
			}
			if err != nil {
				t.Fatalf("load completed-execution Reference mode: %v", err)
			}
			expected := testCase.mode
			if expected == "" {
				expected = CompletedExecutionFeedReferenceModeLegacy
			}
			if cfg.CompletedExecutionFeedReferenceMode != expected {
				t.Fatalf(
					"completed-execution Reference mode=%q want=%q",
					cfg.CompletedExecutionFeedReferenceMode,
					expected,
				)
			}
		})
	}
}

func TestLoadXInsightConnectorAgentConfiguration(t *testing.T) {
	const (
		installationID = "xci_AbCdEfGhIjKlMnOpQrStUv"
		deploymentID   = "xid_ZyXwVuTsRqPoNmLkJiHgFe"
		bindingSHA     = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("COMPLETED_EXECUTION_FEED_ENABLED", "true")
	t.Setenv("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "dev.unirouter:stable-1")
	t.Setenv("XINSIGHT_CONNECTOR_AGENT_ENABLED", "true")
	t.Setenv("XINSIGHT_CONNECTOR_CONTROL_ORIGIN", "https://control.example.test:8443")
	t.Setenv("XINSIGHT_CONNECTOR_POLL_MILLISECONDS", "750")
	t.Setenv("XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS", "3000")
	t.Setenv("XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON", `[{"installation_id":"`+installationID+`","x_deployment_id":"`+deploymentID+`","network_binding_sha256":"`+bindingSHA+`","allowed_ip":"203.0.113.7/32"}]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load Connector agent config: %v", err)
	}
	if !cfg.XInsightConnectorAgentEnabled || cfg.XInsightConnectorControlOrigin != "https://control.example.test:8443" ||
		cfg.XInsightConnectorPollMilliseconds != 750 || cfg.XInsightConnectorRequestTimeoutMilliseconds != 3000 {
		t.Fatalf("unexpected Connector agent config: %#v", cfg)
	}
	allowedIP, ok := cfg.ResolveXInsightConnectorReceiverBinding(installationID, deploymentID, bindingSHA)
	if !ok || allowedIP != "203.0.113.7/32" {
		t.Fatalf("resolve exact receiver binding=(%q,%t)", allowedIP, ok)
	}
	if _, ok := cfg.ResolveXInsightConnectorReceiverBinding(strings.ToLower(installationID), deploymentID, bindingSHA); ok {
		t.Fatal("case-normalized management identity must not resolve")
	}
}

func TestLoadXInsightConnectorDefaultsDisabledAndInert(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("XINSIGHT_CONNECTOR_AGENT_ENABLED", "")
	t.Setenv("XINSIGHT_CONNECTOR_CONTROL_ORIGIN", "")
	t.Setenv("XINSIGHT_CONNECTOR_POLL_MILLISECONDS", "")
	t.Setenv("XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS", "")
	t.Setenv("XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load disabled Connector defaults: %v", err)
	}
	if cfg.XInsightConnectorAgentEnabled || cfg.XInsightConnectorControlOrigin != "" ||
		cfg.XInsightConnectorPollMilliseconds != XInsightConnectorDefaultPollMilliseconds ||
		cfg.XInsightConnectorRequestTimeoutMilliseconds != XInsightConnectorDefaultRequestTimeoutMilliseconds ||
		len(cfg.XInsightConnectorReceiverBindings) != 0 {
		t.Fatalf("unexpected disabled Connector defaults: %#v", cfg)
	}
}

func TestValidateXInsightConnectorFailsClosed(t *testing.T) {
	validBinding := XInsightConnectorReceiverBinding{
		InstallationID:       "xci_AbCdEfGhIjKlMnOpQrStUv",
		XDeploymentID:        "xid_ZyXwVuTsRqPoNmLkJiHgFe",
		NetworkBindingSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AllowedIP:            "2001:db8::7/128",
	}
	base := func() Config {
		return Config{
			JWTSecret:                                   "jwt-secret",
			APIKeyHashSecret:                            "api-key-secret",
			EncryptionKey:                               "0123456789abcdef0123456789abcdef",
			RuntimeDeploymentRole:                       "stable",
			CompletedExecutionFeedEnabled:               true,
			CompletedExecutionFeedSourceInstance:        "dev.unirouter:stable-1",
			CompletedExecutionFeedReferenceMode:         CompletedExecutionFeedReferenceModeLegacy,
			XInsightConnectorAgentEnabled:               true,
			XInsightConnectorControlOrigin:              "https://control.example.test",
			XInsightConnectorPollMilliseconds:           XInsightConnectorDefaultPollMilliseconds,
			XInsightConnectorRequestTimeoutMilliseconds: XInsightConnectorDefaultRequestTimeoutMilliseconds,
			XInsightConnectorReceiverBindings:           []XInsightConnectorReceiverBinding{validBinding},
		}
	}
	valid := base()
	if err := valid.validateXInsightConnector(); err != nil {
		t.Fatalf("valid Connector config: %v", err)
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "feed disabled", mutate: func(cfg *Config) { cfg.CompletedExecutionFeedEnabled = false }},
		{name: "canary writer", mutate: func(cfg *Config) { cfg.RuntimeDeploymentRole = "canary" }},
		{name: "origin missing", mutate: func(cfg *Config) { cfg.XInsightConnectorControlOrigin = "" }},
		{name: "binding missing", mutate: func(cfg *Config) { cfg.XInsightConnectorReceiverBindings = nil }},
		{name: "http origin", mutate: func(cfg *Config) { cfg.XInsightConnectorControlOrigin = "http://control.example.test" }},
		{name: "origin path", mutate: func(cfg *Config) { cfg.XInsightConnectorControlOrigin = "https://control.example.test/api" }},
		{name: "origin trailing slash", mutate: func(cfg *Config) { cfg.XInsightConnectorControlOrigin = "https://control.example.test/" }},
		{name: "origin userinfo", mutate: func(cfg *Config) { cfg.XInsightConnectorControlOrigin = "https://user@control.example.test" }},
		{name: "origin query", mutate: func(cfg *Config) { cfg.XInsightConnectorControlOrigin = "https://control.example.test?next=evil" }},
		{name: "poll too small", mutate: func(cfg *Config) { cfg.XInsightConnectorPollMilliseconds = 249 }},
		{name: "timeout too large", mutate: func(cfg *Config) { cfg.XInsightConnectorRequestTimeoutMilliseconds = 30001 }},
		{name: "unscoped network", mutate: func(cfg *Config) { cfg.XInsightConnectorReceiverBindings[0].AllowedIP = "203.0.113.0/24" }},
		{name: "bare address", mutate: func(cfg *Config) { cfg.XInsightConnectorReceiverBindings[0].AllowedIP = "203.0.113.7" }},
		{name: "uppercase digest", mutate: func(cfg *Config) {
			cfg.XInsightConnectorReceiverBindings[0].NetworkBindingSHA256 = "sha256:" + strings.Repeat("A", 64)
		}},
		{name: "duplicate tuple", mutate: func(cfg *Config) {
			cfg.XInsightConnectorReceiverBindings = append(cfg.XInsightConnectorReceiverBindings, cfg.XInsightConnectorReceiverBindings[0])
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := base()
			testCase.mutate(&cfg)
			if err := cfg.validateXInsightConnector(); err == nil {
				t.Fatal("expected Connector configuration to fail closed")
			}
		})
	}
}

func TestXInsightConnectorReleaseConfigurationSurfaces(t *testing.T) {
	for _, testCase := range []struct {
		path         string
		enabledValue string
	}{
		{path: "../../docker-compose.yml", enabledValue: "XINSIGHT_CONNECTOR_AGENT_ENABLED: ${XINSIGHT_CONNECTOR_AGENT_ENABLED:-false}"},
		{path: "../../docker-compose.canary.yml", enabledValue: `XINSIGHT_CONNECTOR_AGENT_ENABLED: "false"`},
		{path: "../../deploy/customer/docker-compose.yml", enabledValue: "XINSIGHT_CONNECTOR_AGENT_ENABLED: ${XINSIGHT_CONNECTOR_AGENT_ENABLED:-false}"},
	} {
		raw, err := os.ReadFile(testCase.path)
		if err != nil {
			t.Fatalf("read %s: %v", testCase.path, err)
		}
		text := string(raw)
		for _, required := range []string{
			testCase.enabledValue,
			"XINSIGHT_CONNECTOR_CONTROL_ORIGIN: ${XINSIGHT_CONNECTOR_CONTROL_ORIGIN:-}",
			"XINSIGHT_CONNECTOR_POLL_MILLISECONDS: ${XINSIGHT_CONNECTOR_POLL_MILLISECONDS:-1000}",
			"XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS: ${XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS:-5000}",
			"XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON: '${XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON:-[]}'",
		} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s missing Connector release binding %q", testCase.path, required)
			}
		}
		if strings.Contains(testCase.path, "canary") && strings.Contains(text, "XINSIGHT_CONNECTOR_AGENT_ENABLED: ${") {
			t.Fatal("canary must not inherit the stable Connector writer enablement")
		}
	}

	for _, path := range []string{"../../.env.example", "../../deploy/customer/.env.example"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		for _, required := range []string{
			"XINSIGHT_CONNECTOR_AGENT_ENABLED=false",
			"XINSIGHT_CONNECTOR_CONTROL_ORIGIN=",
			"XINSIGHT_CONNECTOR_POLL_MILLISECONDS=1000",
			"XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS=5000",
			"XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON=[]",
		} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s missing Connector environment contract %q", path, required)
			}
		}
	}
}

func TestLoadXInsightConnectorRejectsMalformedTypedEnvironment(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "invalid enabled boolean", key: "XINSIGHT_CONNECTOR_AGENT_ENABLED", value: "sometimes"},
		{name: "noncanonical enabled boolean", key: "XINSIGHT_CONNECTOR_AGENT_ENABLED", value: "TRUE"},
		{name: "invalid poll integer", key: "XINSIGHT_CONNECTOR_POLL_MILLISECONDS", value: "fast"},
		{name: "invalid timeout integer", key: "XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS", value: "slow"},
		{name: "null bindings", key: "XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON", value: "null"},
		{name: "unknown secret field", key: "XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON", value: `[{"installation_id":"xci_AbCdEfGhIjKlMnOpQrStUv","x_deployment_id":"xid_ZyXwVuTsRqPoNmLkJiHgFe","network_binding_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","allowed_ip":"203.0.113.7/32","token":"forbidden"}]`},
		{name: "trailing object", key: "XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON", value: `[] {}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "jwt-secret")
			t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
			t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("XINSIGHT_CONNECTOR_AGENT_ENABLED", "")
			t.Setenv("XINSIGHT_CONNECTOR_POLL_MILLISECONDS", "")
			t.Setenv("XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS", "")
			t.Setenv("XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON", "")
			t.Setenv(testCase.key, testCase.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected malformed Connector environment to fail startup")
			}
		})
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Clear all env vars to test defaults
	envVars := []string{
		"LISTEN_PORT", "LOG_LEVEL", "GIN_MODE", "SHUTDOWN_TIMEOUT_SECONDS", "DB_DSN", "DB_MAX_CONNECTIONS",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "JWT_SECRET", "API_KEY_HASH_SECRET", "ENCRYPTION_KEY",
		"RESPONSE_CACHE_INDEX_KEY", "RESPONSE_CACHE_ENCRYPTION_KEY", "RESPONSE_CACHE_KEY_VERSION",
		"TRACE_EVENTS_ENABLED", "TRACE_EVENT_SAMPLE_RATE", "TRACE_EVENT_RETENTION_DAYS", "LOG_RETENTION_DAYS",
		"PAYLOAD_CAPTURE_MODE", "EVIDENCE_CONTEXT_TRUST_SECRET", "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET",
		"STREAM_MAX_SSE_LINE_BYTES", "STREAM_WRITE_IDLE_TIMEOUT_SECONDS", "STREAM_HEARTBEAT_SECONDS", "STREAM_MAX_DURATION_SECONDS",
		"WORKLOAD_GLOBAL_CAPACITY", "WORKLOAD_ORG_CAPACITY", "WORKLOAD_GLOBAL_SHORT_RESERVE", "WORKLOAD_ORG_SHORT_RESERVE",
		"WORKLOAD_INTERACTIVE_WEIGHT", "WORKLOAD_STREAM_WEIGHT", "WORKLOAD_EMBEDDING_WEIGHT",
		"WORKLOAD_INTERACTIVE_MAX_WAIT_MS", "WORKLOAD_STREAM_MAX_WAIT_MS", "WORKLOAD_EMBEDDING_MAX_WAIT_MS",
		"GUARDRAIL_ORG_ACTIVE_POLICY_LIMIT", "GUARDRAIL_PLATFORM_ACTIVE_POLICY_LIMIT", "GUARDRAIL_CACHED_POLICY_LIMIT", "GUARDRAIL_CACHED_TENANT_LIMIT",
		"RUNTIME_ENVIRONMENT", "RUNTIME_DEPLOYMENT_MODE", "MODEL_ROUTE_AUTHORITY", "MODEL_ROUTE_COMPATIBILITY_ENABLED", "TRUSTED_PROXY_CIDRS",
		"RUNTIME_DEPLOYMENT_ROLE", "RUNTIME_VERSION", "RUNTIME_COMMIT", "PROMETHEUS_URL", "GRAFANA_URL",
		"COMPLETED_EXECUTION_FEED_ENABLED", "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "COMPLETED_EXECUTION_FEED_REFERENCE_MODE", "SOURCE_FACTS_ENABLED",
		"XINSIGHT_CONNECTOR_AGENT_ENABLED", "XINSIGHT_CONNECTOR_CONTROL_ORIGIN", "XINSIGHT_CONNECTOR_POLL_MILLISECONDS",
		"XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS", "XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON",
		"LICENSE_PUBLIC_KEY", "LICENSE_PUBLIC_KEYS", "LICENSE_DEPLOYMENT_ID", "LICENSE_EXPIRY_WARNING_DAYS", "LICENSE_REFRESH_SECONDS", "UPGRADE_PUBLIC_KEYS", "BILLING_MODE", "REQUEST_PRICING_AUTHORITY", "MIN_FREEZE_AMOUNT", "FREEZE_SAFETY_FACTOR", "DEFAULT_KEY_RPM", "DEFAULT_KEY_QPS",
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
		PayloadCaptureMode:               traceutil.NormalizePayloadCaptureMode(getEnvStr("PAYLOAD_CAPTURE_MODE", traceutil.PayloadCaptureModeMetadataOnly)),
		EvidenceContextTrustSecret:       getEnvStr("EVIDENCE_CONTEXT_TRUST_SECRET", ""),
		NotificationWebhookSigningSecret: getEnvStr("UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET", ""),
		RuntimeEnvironment:               getEnvStr("RUNTIME_ENVIRONMENT", "unspecified"),
		RuntimeDeploymentMode:            getEnvStr("RUNTIME_DEPLOYMENT_MODE", "single-node"),
		RuntimeDeploymentRole:            getEnvStr("RUNTIME_DEPLOYMENT_ROLE", "stable"),
		ModelRouteAuthority:              modelrouting.Mode(getEnvStr("MODEL_ROUTE_AUTHORITY", string(modelrouting.ModeCanonical))),
		ModelRouteCompatibilityEnabled:   getEnvBool("MODEL_ROUTE_COMPATIBILITY_ENABLED", false),
		CompletedExecutionFeedReferenceMode: getEnvStr(
			"COMPLETED_EXECUTION_FEED_REFERENCE_MODE",
			CompletedExecutionFeedReferenceModeLegacy,
		),
		XInsightConnectorPollMilliseconds:           XInsightConnectorDefaultPollMilliseconds,
		XInsightConnectorRequestTimeoutMilliseconds: XInsightConnectorDefaultRequestTimeoutMilliseconds,
		BillingMode:                        billing.BillingMode(getEnvStr("BILLING_MODE", string(billing.BillingModeCostObservation))),
		RequestPricingAuthority:            billing.RequestPricingAuthority(getEnvStr("REQUEST_PRICING_AUTHORITY", string(billing.RequestPricingAuthorityLegacy))),
		MinFreezeCents:                     int64(getEnvInt("MIN_FREEZE_AMOUNT", 10)),
		FreezeSafetyFactor:                 getEnvStr("FREEZE_SAFETY_FACTOR", "1.5"),
		DefaultKeyRPM:                      getEnvInt("DEFAULT_KEY_RPM", 60),
		DefaultKeyQPS:                      getEnvInt("DEFAULT_KEY_QPS", 10),
		StreamMaxSSELineBytes:              getEnvInt("STREAM_MAX_SSE_LINE_BYTES", 2*1024*1024),
		StreamWriteIdleTimeoutSecs:         getEnvInt("STREAM_WRITE_IDLE_TIMEOUT_SECONDS", 30),
		StreamHeartbeatSecs:                getEnvInt("STREAM_HEARTBEAT_SECONDS", 15),
		StreamMaxDurationSecs:              getEnvInt("STREAM_MAX_DURATION_SECONDS", 30*60),
		WorkloadGlobalCapacity:             getEnvInt("WORKLOAD_GLOBAL_CAPACITY", 100),
		WorkloadOrgCapacity:                getEnvInt("WORKLOAD_ORG_CAPACITY", 10),
		WorkloadGlobalShortReserve:         getEnvInt("WORKLOAD_GLOBAL_SHORT_RESERVE", 10),
		WorkloadOrgShortReserve:            getEnvInt("WORKLOAD_ORG_SHORT_RESERVE", 2),
		WorkloadInteractiveWeight:          getEnvInt("WORKLOAD_INTERACTIVE_WEIGHT", 1),
		WorkloadStreamWeight:               getEnvInt("WORKLOAD_STREAM_WEIGHT", 4),
		WorkloadEmbeddingWeight:            getEnvInt("WORKLOAD_EMBEDDING_WEIGHT", 1),
		WorkloadInteractiveMaxWaitMS:       getEnvInt("WORKLOAD_INTERACTIVE_MAX_WAIT_MS", 50),
		WorkloadStreamMaxWaitMS:            getEnvInt("WORKLOAD_STREAM_MAX_WAIT_MS", 250),
		WorkloadEmbeddingMaxWaitMS:         getEnvInt("WORKLOAD_EMBEDDING_MAX_WAIT_MS", 50),
		GuardrailOrgActivePolicyLimit:      getEnvInt("GUARDRAIL_ORG_ACTIVE_POLICY_LIMIT", 256),
		GuardrailPlatformActivePolicyLimit: getEnvInt("GUARDRAIL_PLATFORM_ACTIVE_POLICY_LIMIT", 256),
		GuardrailCachedPolicyLimit:         getEnvInt("GUARDRAIL_CACHED_POLICY_LIMIT", 4096),
		GuardrailCachedTenantLimit:         getEnvInt("GUARDRAIL_CACHED_TENANT_LIMIT", 4096),
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
	if cfg.MinFreezeCents != 10 {
		t.Fatalf("expected 10 cent min freeze, got %d", cfg.MinFreezeCents)
	}
	if cfg.BillingMode != billing.BillingModeCostObservation {
		t.Fatalf("expected cost-observation billing default, got %q", cfg.BillingMode)
	}
	if cfg.RequestPricingAuthority != billing.RequestPricingAuthorityLegacy {
		t.Fatalf("expected legacy request-pricing upgrade default, got %q", cfg.RequestPricingAuthority)
	}
	if cfg.ModelRouteAuthority != modelrouting.ModeCanonical || cfg.ModelRouteCompatibilityEnabled {
		t.Fatalf("expected canonical model-route authority with compatibility disabled, got %q/%t", cfg.ModelRouteAuthority, cfg.ModelRouteCompatibilityEnabled)
	}
	if cfg.FreezeSafetyFactor != "1.5" {
		t.Fatalf("expected exact 1.5 safety factor, got %q", cfg.FreezeSafetyFactor)
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
	guardrailOptions := cfg.GuardrailOptions()
	if guardrailOptions.MaxActivePoliciesPerOrg != 256 || guardrailOptions.MaxActivePlatformPolicies != 256 ||
		guardrailOptions.MaxCachedPolicies != 4096 || guardrailOptions.MaxCachedTenantScopes != 4096 {
		t.Fatalf("unexpected guardrail quota defaults: %#v", guardrailOptions)
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
	if cfg.NotificationWebhookSigningSecret != "" {
		t.Fatal("expected notification Webhook signing to be disabled by default")
	}
	if cfg.RuntimeEnvironment != "unspecified" || cfg.RuntimeDeploymentMode != "single-node" || cfg.RuntimeDeploymentRole != "stable" {
		t.Fatalf("unexpected runtime defaults: env=%s mode=%s role=%s", cfg.RuntimeEnvironment, cfg.RuntimeDeploymentMode, cfg.RuntimeDeploymentRole)
	}
	if cfg.CompletedExecutionFeedReferenceMode != CompletedExecutionFeedReferenceModeLegacy {
		t.Fatalf("unexpected completed-execution Reference mode default: %q", cfg.CompletedExecutionFeedReferenceMode)
	}
	if cfg.XInsightConnectorAgentEnabled || cfg.XInsightConnectorControlOrigin != "" ||
		cfg.XInsightConnectorPollMilliseconds != XInsightConnectorDefaultPollMilliseconds ||
		cfg.XInsightConnectorRequestTimeoutMilliseconds != XInsightConnectorDefaultRequestTimeoutMilliseconds ||
		len(cfg.XInsightConnectorReceiverBindings) != 0 {
		t.Fatalf("unexpected Connector defaults: %#v", cfg)
	}
}

func TestValidateUpgradePublicKeys(t *testing.T) {
	cfg := &Config{
		JWTSecret:          "secret",
		APIKeyHashSecret:   "api-key-secret",
		EncryptionKey:      "0123456789abcdef0123456789abcdef",
		FreezeSafetyFactor: "1",
		UpgradePublicKeys:  `{"bad key":"not-a-key"}`,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid UPGRADE_PUBLIC_KEYS to fail startup validation")
	}
}

func TestValidateLicensePublicKeys(t *testing.T) {
	cfg := &Config{
		JWTSecret:          "secret",
		APIKeyHashSecret:   "api-key-secret",
		EncryptionKey:      "0123456789abcdef0123456789abcdef",
		FreezeSafetyFactor: "1",
		LicensePublicKeys:  `{"bad key":"not-a-key"}`,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid LICENSE_PUBLIC_KEYS to fail startup validation")
	}
	cfg.LicensePublicKeys = ""
	cfg.LicensePublicKey = "not-a-key"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid LICENSE_PUBLIC_KEY to fail startup validation")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("LISTEN_PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	licensePublicKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	os.Setenv("LICENSE_PUBLIC_KEY", licensePublicKey)
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
	if cfg.LicensePublicKey != licensePublicKey {
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
