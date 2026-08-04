package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/telemetryexport"
	"github.com/uniroute/unirouter/internal/traceutil"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

type Config struct {
	// Server
	ListenPort          int
	LogLevel            string
	GinMode             string
	MigrationsDir       string
	ShutdownTimeoutSecs int

	// Database
	DB_DSN           string
	DBMaxConnections int
	DBMaxIdleConns   int

	// Redis
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisMaxConns int

	// Security
	JWTSecret        string
	APIKeyHashSecret string
	EncryptionKey    string
	// Exact response cache uses an isolated HMAC index key and AES key. The
	// runtime stays disabled unless the complete three-part key binding exists.
	ResponseCacheIndexKey      string
	ResponseCacheEncryptionKey string
	ResponseCacheKeyVersion    string
	// NotificationWebhookSigningSecret signs outbound notification Webhook
	// bodies. It is optional for source development, but customer Compose makes
	// it mandatory so receivers can authenticate deliveries.
	NotificationWebhookSigningSecret string

	// Runtime exposure hardening
	AdminCORSAllowedOrigins []string
	MetricsEnabled          bool
	MetricsAllowedCIDRs     []string
	// TrustedProxyCIDRs contains only network hops authorized to supply client
	// forwarding headers. Empty means forwarding headers are ignored.
	TrustedProxyCIDRs                    []string
	LoginRateLimitEnabled                bool
	LoginRateLimitWindowSec              int
	LoginRateLimitIP                     int
	LoginRateLimitUser                   int
	TraceEventsEnabled                   bool
	TraceEventSampleRate                 float64
	TraceEventRetentionDays              int
	PayloadCaptureMode                   string
	EvidenceContextTrustSecret           string
	RuntimeEnvironment                   string
	RuntimeDeploymentMode                string
	RuntimeDeploymentRole                string
	RuntimeVersion                       string
	RuntimeCommit                        string
	PrometheusURL                        string
	GrafanaURL                           string
	CompletedExecutionFeedEnabled        bool
	CompletedExecutionFeedSourceInstance string
	CompletedExecutionFeedReferenceMode  string
	OTelExportEnabled                    bool
	OTelExportEndpoint                   string
	OTelExportQueueSize                  int
	OTelExportBatchSize                  int
	OTelExportFlushIntervalMS            int
	OTelExportTimeoutMS                  int
	OTelExportMaxRetries                 int

	// Isolated asynchronous image job plane. Credentials are resolved by the
	// AWS SDK default provider chain and existing encrypted provider channels;
	// no secret material is represented by these fields.
	AsyncMediaEnabled             bool
	AsyncMediaAdmissionEnabled    bool
	AsyncMediaS3Region            string
	AsyncMediaS3Bucket            string
	AsyncMediaS3Endpoint          string
	AsyncMediaS3Prefix            string
	AsyncMediaS3ForcePathStyle    bool
	AsyncMediaWorkerID            string
	AsyncMediaLeaseSeconds        int
	AsyncMediaPurgeLeaseSeconds   int
	AsyncMediaPollMilliseconds    int
	AsyncMediaProviderTimeoutSecs int
	AsyncMediaMaxAttempts         int

	// License
	LicensePublicKey         string
	LicensePublicKeys        string
	LicenseDeploymentID      string
	LicenseExpiryWarningDays int
	LicenseRefreshSeconds    int

	// Signed offline upgrades use a trust root independent from License signing.
	UpgradePublicKeys string

	// OIDC SSO
	OIDCEnabled            bool
	OIDCIssuerURL          string
	OIDCClientID           string
	OIDCClientSecret       string
	OIDCRedirectURL        string
	OIDCProviderName       string
	OIDCScopes             []string
	OIDCAllowedEmailDomain string
	OIDCAutoProvision      bool
	OIDCDefaultOrgID       int64
	OIDCDefaultRole        string
	OIDCDefaultProjectSlug string
	OIDCUsernameClaim      string
	OIDCEmailClaim         string
	OIDCRoleClaim          string
	OIDCOrgIDClaim         string

	// Billing
	MinFreezeCents     int64
	FreezeSafetyFactor string
	FreezeTimeoutSecs  int
	ExchangeRate       float64

	// Rate Limiting
	DefaultKeyRPM int
	DefaultKeyQPS int

	// Replica-invariant weighted workload admission
	WorkloadGlobalCapacity       int
	WorkloadOrgCapacity          int
	WorkloadGlobalShortReserve   int
	WorkloadOrgShortReserve      int
	WorkloadInteractiveWeight    int
	WorkloadStreamWeight         int
	WorkloadEmbeddingWeight      int
	WorkloadInteractiveMaxWaitMS int
	WorkloadStreamMaxWaitMS      int
	WorkloadEmbeddingMaxWaitMS   int

	// Guardrail active-policy and immutable snapshot cache bounds.
	GuardrailOrgActivePolicyLimit      int
	GuardrailPlatformActivePolicyLimit int
	GuardrailCachedPolicyLimit         int
	GuardrailCachedTenantLimit         int

	// Application-authoritative payload and per-replica memory safety.
	ChatRequestMaxBytes       int
	EmbeddingsRequestMaxBytes int
	NonStreamResponseMaxBytes int
	PayloadNodeInFlightBytes  int
	PayloadOrgInFlightBytes   int

	// Failover
	MaxRetryCount              int
	CircuitBreakerThreshold    int
	CircuitBreakerRecoverSecs  int
	CircuitBreakerHalfOpenPct  int
	RateLimitCooldownSecs      int
	AuthFailureCooldownSecs    int
	ProviderKeyFailThreshold   int
	RoutingSnapshotRefreshSecs int
	RoutingSnapshotStaleSecs   int
	RoutingProbeLeaseSecs      int
	RoutingMaxCandidates       int
	RoutingMaxSnapshots        int

	// Streaming transport
	StreamMaxSSELineBytes      int
	StreamWriteIdleTimeoutSecs int
	StreamHeartbeatSecs        int
	StreamMaxDurationSecs      int

	// Backup
	BackupEnabled    bool
	BackupCron       string
	BackupRetainDays int

	// Token Estimation
	TokenRatioZH float64
	TokenRatioEN float64

	// Cleanup
	LogRetentionDays int

	// Canary
	SkipMigration bool
}

// ValidateReleaseSecrets applies the stronger symmetric-secret floor required
// by vendor-signed customer release binaries. Source-development builds keep
// Validate's historical non-empty requirement for local compatibility.
func (c *Config) ValidateReleaseSecrets() error {
	if c == nil {
		return fmt.Errorf("configuration is required")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes in release mode, got %d bytes", len(c.JWTSecret))
	}
	if len(c.APIKeyHashSecret) < 32 {
		return fmt.Errorf("API_KEY_HASH_SECRET must be at least 32 bytes in release mode, got %d bytes", len(c.APIKeyHashSecret))
	}
	if len(c.NotificationWebhookSigningSecret) < 32 {
		return fmt.Errorf("UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET must be at least 32 bytes in release mode, got %d bytes", len(c.NotificationWebhookSigningSecret))
	}
	return nil
}

// Validate checks that all required configuration values are valid.
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET environment variable is required and cannot be empty")
	}
	if c.APIKeyHashSecret == "" {
		return fmt.Errorf("API_KEY_HASH_SECRET environment variable is required and cannot be empty")
	}
	if len(c.EncryptionKey) != 32 {
		return fmt.Errorf("ENCRYPTION_KEY must be exactly 32 bytes for AES-256, got %d bytes", len(c.EncryptionKey))
	}
	cacheConfigured := c.ResponseCacheIndexKey != "" || c.ResponseCacheEncryptionKey != "" || c.ResponseCacheKeyVersion != ""
	if cacheConfigured {
		if len(c.ResponseCacheIndexKey) != 32 {
			return fmt.Errorf("RESPONSE_CACHE_INDEX_KEY must be exactly 32 bytes when response cache is configured")
		}
		if len(c.ResponseCacheEncryptionKey) != 32 {
			return fmt.Errorf("RESPONSE_CACHE_ENCRYPTION_KEY must be exactly 32 bytes when response cache is configured")
		}
		if !completedExecutionSourceInstancePattern.MatchString(c.ResponseCacheKeyVersion) {
			return fmt.Errorf("RESPONSE_CACHE_KEY_VERSION must use a portable 1-128 character identifier")
		}
	}
	if c.NotificationWebhookSigningSecret != "" && len(c.NotificationWebhookSigningSecret) < 32 {
		return fmt.Errorf("UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET must be at least 32 bytes when set")
	}
	if c.ShutdownTimeoutSecs < 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be >= 0")
	}
	if c.LicenseExpiryWarningDays < 0 || c.LicenseExpiryWarningDays > 36500 {
		return fmt.Errorf("LICENSE_EXPIRY_WARNING_DAYS must be between 0 and 36500")
	}
	if c.LicenseRefreshSeconds < 0 || c.LicenseRefreshSeconds > 3600 {
		return fmt.Errorf("LICENSE_REFRESH_SECONDS must be between 0 and 3600")
	}
	if len(c.LicenseDeploymentID) > 128 {
		return fmt.Errorf("LICENSE_DEPLOYMENT_ID must be at most 128 characters")
	}
	if c.LicensePublicKey != "" {
		if _, err := signing.ParsePublicKey(c.LicensePublicKey); err != nil {
			return fmt.Errorf("LICENSE_PUBLIC_KEY: %w", err)
		}
	}
	if c.LicensePublicKeys != "" {
		if _, err := signing.ParsePublicKeyRing(c.LicensePublicKeys, "LICENSE_PUBLIC_KEYS"); err != nil {
			return err
		}
	}
	if c.UpgradePublicKeys != "" {
		if _, err := signing.ParsePublicKeyRing(c.UpgradePublicKeys, "UPGRADE_PUBLIC_KEYS"); err != nil {
			return err
		}
	}
	for _, origin := range c.AdminCORSAllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil ||
			parsed.Scheme == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" ||
			(parsed.Path != "" && parsed.Path != "/") ||
			parsed.RawQuery != "" ||
			parsed.Fragment != "" {
			return fmt.Errorf("ADMIN_CORS_ALLOWED_ORIGINS contains invalid origin: %s", origin)
		}
	}
	for _, cidr := range c.MetricsAllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("METRICS_ALLOWED_CIDRS contains invalid CIDR: %s", cidr)
		}
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid CIDR: %s", cidr)
		}
		if ones, _ := network.Mask.Size(); ones == 0 {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS must not trust every address: %s", cidr)
		}
	}
	if c.LoginRateLimitEnabled && c.LoginRateLimitWindowSec <= 0 {
		return fmt.Errorf("LOGIN_RATE_LIMIT_WINDOW_SECONDS must be > 0")
	}
	if c.LoginRateLimitIP < 0 {
		return fmt.Errorf("LOGIN_RATE_LIMIT_IP_REQUESTS must be >= 0")
	}
	if c.LoginRateLimitUser < 0 {
		return fmt.Errorf("LOGIN_RATE_LIMIT_USER_REQUESTS must be >= 0")
	}
	if c.TraceEventSampleRate < 0 || c.TraceEventSampleRate > 1 {
		return fmt.Errorf("TRACE_EVENT_SAMPLE_RATE must be between 0 and 1")
	}
	if c.TraceEventRetentionDays < 0 {
		return fmt.Errorf("TRACE_EVENT_RETENTION_DAYS must be >= 0")
	}
	if !traceutil.IsSupportedPayloadCaptureMode(c.PayloadCaptureMode) {
		return fmt.Errorf("PAYLOAD_CAPTURE_MODE must be one of: %s", traceutil.SupportedPayloadCaptureModes())
	}
	if c.EvidenceContextTrustSecret != "" && len(c.EvidenceContextTrustSecret) < 16 {
		return fmt.Errorf("EVIDENCE_CONTEXT_TRUST_SECRET must be at least 16 bytes when set")
	}
	if c.OTelExportEnabled && !c.TraceEventsEnabled {
		return fmt.Errorf("UNIROUTE_OTEL_EXPORT_ENABLED requires TRACE_EVENTS_ENABLED=true")
	}
	if err := c.TelemetryExportConfig().Validate(); err != nil {
		return err
	}
	if c.AsyncMediaAdmissionEnabled && !c.AsyncMediaEnabled {
		return fmt.Errorf("ASYNC_MEDIA_ADMISSION_ENABLED requires ASYNC_MEDIA_ENABLED=true")
	}
	if c.AsyncMediaEnabled {
		if strings.TrimSpace(c.AsyncMediaS3Region) == "" {
			return fmt.Errorf("ASYNC_MEDIA_S3_REGION is required when async media is enabled")
		}
		if !validS3BucketName(c.AsyncMediaS3Bucket) {
			return fmt.Errorf("ASYNC_MEDIA_S3_BUCKET is invalid")
		}
		if err := validateOptionalHTTPURL("ASYNC_MEDIA_S3_ENDPOINT", c.AsyncMediaS3Endpoint); err != nil {
			return err
		}
		if c.AsyncMediaS3Endpoint != "" {
			endpoint, _ := url.Parse(c.AsyncMediaS3Endpoint)
			if endpoint != nil && endpoint.User != nil {
				return fmt.Errorf("ASYNC_MEDIA_S3_ENDPOINT must not include userinfo or credentials")
			}
			if endpoint != nil && endpoint.Path != "" && endpoint.Path != "/" {
				return fmt.Errorf("ASYNC_MEDIA_S3_ENDPOINT must be an origin without a path")
			}
			if endpoint != nil && endpoint.Scheme == "http" &&
				!isLocalRuntimeEnvironment(c.RuntimeEnvironment) &&
				!isLoopbackHost(endpoint.Hostname()) {
				return fmt.Errorf("ASYNC_MEDIA_S3_ENDPOINT must use https outside local development")
			}
		}
		if c.AsyncMediaS3Prefix == "" ||
			strings.HasPrefix(c.AsyncMediaS3Prefix, "/") ||
			strings.HasSuffix(c.AsyncMediaS3Prefix, "/") ||
			strings.Contains(c.AsyncMediaS3Prefix, "..") ||
			len(c.AsyncMediaS3Prefix) > 128 {
			return fmt.Errorf("ASYNC_MEDIA_S3_PREFIX is invalid")
		}
		if c.AsyncMediaWorkerID != "" &&
			!completedExecutionSourceInstancePattern.MatchString(c.AsyncMediaWorkerID) {
			return fmt.Errorf("ASYNC_MEDIA_WORKER_ID must use a portable 1-128 character identifier")
		}
		if c.AsyncMediaLeaseSeconds < 60 || c.AsyncMediaLeaseSeconds > 1800 {
			return fmt.Errorf("ASYNC_MEDIA_LEASE_SECONDS must be between 60 and 1800")
		}
		if c.AsyncMediaPurgeLeaseSeconds < 5 || c.AsyncMediaPurgeLeaseSeconds > 300 {
			return fmt.Errorf("ASYNC_MEDIA_PURGE_LEASE_SECONDS must be between 5 and 300")
		}
		if c.AsyncMediaPollMilliseconds < 50 || c.AsyncMediaPollMilliseconds > 60000 {
			return fmt.Errorf("ASYNC_MEDIA_POLL_MILLISECONDS must be between 50 and 60000")
		}
		if c.AsyncMediaProviderTimeoutSecs < 1 || c.AsyncMediaProviderTimeoutSecs > 900 {
			return fmt.Errorf("ASYNC_MEDIA_PROVIDER_TIMEOUT_SECONDS must be between 1 and 900")
		}
		if c.AsyncMediaLeaseSeconds < c.AsyncMediaProviderTimeoutSecs+60 {
			return fmt.Errorf("ASYNC_MEDIA_LEASE_SECONDS must exceed ASYNC_MEDIA_PROVIDER_TIMEOUT_SECONDS by at least 60 seconds")
		}
		if c.AsyncMediaMaxAttempts < 1 || c.AsyncMediaMaxAttempts > 10 {
			return fmt.Errorf("ASYNC_MEDIA_MAX_ATTEMPTS must be between 1 and 10")
		}
	}
	if c.LogRetentionDays < 0 {
		return fmt.Errorf("LOG_RETENTION_DAYS must be >= 0")
	}
	if c.MinFreezeCents < 0 {
		return fmt.Errorf("MIN_FREEZE_AMOUNT must be >= 0 cents")
	}
	safetyFactor, err := money.ParseDecimal(c.FreezeSafetyFactor)
	if err != nil || safetyFactor.Sign() <= 0 {
		return fmt.Errorf("FREEZE_SAFETY_FACTOR must be an exact positive decimal")
	}
	policy := c.RoutingPolicyConfig()
	if policy != (routingpolicy.Config{}) {
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("invalid routing configuration: %w", err)
		}
	}
	streamPolicy := c.StreamPolicyConfig()
	if streamPolicy != (streampolicy.Config{}) {
		if err := streamPolicy.Validate(); err != nil {
			return fmt.Errorf("invalid streaming transport configuration: %w", err)
		}
	}
	if c.WorkloadGlobalCapacity != 0 || c.WorkloadOrgCapacity != 0 {
		if err := c.WorkloadAdmissionOptions().Validate(); err != nil {
			return fmt.Errorf("invalid weighted workload admission configuration: %w", err)
		}
	}
	if err := c.PayloadLimits().Validate(); err != nil {
		return fmt.Errorf("invalid payload safety configuration: %w", err)
	}
	if err := c.GuardrailOptions().Validate(); err != nil {
		return fmt.Errorf("invalid guardrail snapshot configuration: %w", err)
	}
	if err := validateOptionalHTTPURL("PROMETHEUS_URL", c.PrometheusURL); err != nil {
		return err
	}
	if err := validateOptionalHTTPURL("GRAFANA_URL", c.GrafanaURL); err != nil {
		return err
	}
	if c.OIDCEnabled {
		if c.OIDCIssuerURL == "" {
			return fmt.Errorf("OIDC_ISSUER_URL is required when OIDC is enabled")
		}
		if c.OIDCClientID == "" {
			return fmt.Errorf("OIDC_CLIENT_ID is required when OIDC is enabled")
		}
		if c.OIDCClientSecret == "" {
			return fmt.Errorf("OIDC_CLIENT_SECRET is required when OIDC is enabled")
		}
		if c.OIDCRedirectURL == "" {
			return fmt.Errorf("OIDC_REDIRECT_URL is required when OIDC is enabled")
		}
		if !auth.IsValidRole(c.OIDCDefaultRole) {
			return fmt.Errorf("OIDC_DEFAULT_ROLE is invalid: %s", c.OIDCDefaultRole)
		}
		if c.OIDCAutoProvision && c.OIDCDefaultOrgID <= 0 {
			return fmt.Errorf("OIDC_DEFAULT_ORG_ID must be set when OIDC auto provisioning is enabled")
		}
	}
	if c.CompletedExecutionFeedEnabled {
		if !completedExecutionSourceInstancePattern.MatchString(c.CompletedExecutionFeedSourceInstance) {
			return fmt.Errorf("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE must use 1-128 letters, numbers, dot, underscore, colon, or dash")
		}
	}
	switch c.CompletedExecutionFeedReferenceMode {
	case "", CompletedExecutionFeedReferenceModeLegacy:
	case CompletedExecutionFeedReferenceModeShadow,
		CompletedExecutionFeedReferenceModeNeutralCanary:
		if !c.CompletedExecutionFeedEnabled {
			return fmt.Errorf("COMPLETED_EXECUTION_FEED_REFERENCE_MODE requires COMPLETED_EXECUTION_FEED_ENABLED=true")
		}
	default:
		return fmt.Errorf(
			"COMPLETED_EXECUTION_FEED_REFERENCE_MODE must be legacy, shadow, or neutral_canary",
		)
	}
	return nil
}

const (
	CompletedExecutionFeedReferenceModeLegacy        = "legacy"
	CompletedExecutionFeedReferenceModeShadow        = "shadow"
	CompletedExecutionFeedReferenceModeNeutralCanary = "neutral_canary"
)

var completedExecutionSourceInstancePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func Load() (*Config, error) {
	jwtSecret := getEnvStr("JWT_SECRET", "")
	logRetentionDays := getEnvInt("LOG_RETENTION_DAYS", 30)
	asyncMediaEnabled := getEnvBool("ASYNC_MEDIA_ENABLED", false)
	cfg := &Config{
		ListenPort:          getEnvInt("LISTEN_PORT", 8080),
		LogLevel:            getEnvStr("LOG_LEVEL", "info"),
		GinMode:             getEnvStr("GIN_MODE", "release"),
		MigrationsDir:       getEnvStr("MIGRATIONS_DIR", ""),
		ShutdownTimeoutSecs: getEnvInt("SHUTDOWN_TIMEOUT_SECONDS", 120),

		DB_DSN:           getEnvStr("DB_DSN", "postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable"),
		DBMaxConnections: getEnvInt("DB_MAX_CONNECTIONS", 25),
		DBMaxIdleConns:   getEnvInt("DB_MAX_IDLE_CONNECTIONS", 5),

		RedisAddr:     getEnvStr("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnvStr("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		RedisMaxConns: getEnvInt("REDIS_MAX_CONNECTIONS", 10),

		JWTSecret:                        jwtSecret,
		APIKeyHashSecret:                 getEnvStr("API_KEY_HASH_SECRET", jwtSecret),
		EncryptionKey:                    getEnvStr("ENCRYPTION_KEY", ""),
		ResponseCacheIndexKey:            getEnvStr("RESPONSE_CACHE_INDEX_KEY", ""),
		ResponseCacheEncryptionKey:       getEnvStr("RESPONSE_CACHE_ENCRYPTION_KEY", ""),
		ResponseCacheKeyVersion:          strings.TrimSpace(getEnvStr("RESPONSE_CACHE_KEY_VERSION", "")),
		NotificationWebhookSigningSecret: getEnvStr("UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET", ""),

		AdminCORSAllowedOrigins:              splitCSVAllowEmpty(getEnvStr("ADMIN_CORS_ALLOWED_ORIGINS", "")),
		MetricsEnabled:                       getEnvBool("METRICS_ENABLED", true),
		MetricsAllowedCIDRs:                  splitCSVAllowEmpty(getEnvStr("METRICS_ALLOWED_CIDRS", "")),
		TrustedProxyCIDRs:                    splitCSVAllowEmpty(getEnvStr("TRUSTED_PROXY_CIDRS", "")),
		LoginRateLimitEnabled:                getEnvBool("LOGIN_RATE_LIMIT_ENABLED", true),
		LoginRateLimitWindowSec:              getEnvInt("LOGIN_RATE_LIMIT_WINDOW_SECONDS", 60),
		LoginRateLimitIP:                     getEnvInt("LOGIN_RATE_LIMIT_IP_REQUESTS", 30),
		LoginRateLimitUser:                   getEnvInt("LOGIN_RATE_LIMIT_USER_REQUESTS", 10),
		TraceEventsEnabled:                   getEnvBool("TRACE_EVENTS_ENABLED", true),
		TraceEventSampleRate:                 getEnvFloat("TRACE_EVENT_SAMPLE_RATE", 1),
		TraceEventRetentionDays:              getEnvInt("TRACE_EVENT_RETENTION_DAYS", logRetentionDays),
		PayloadCaptureMode:                   traceutil.NormalizePayloadCaptureMode(getEnvStr("PAYLOAD_CAPTURE_MODE", traceutil.PayloadCaptureModeMetadataOnly)),
		EvidenceContextTrustSecret:           getEnvStr("EVIDENCE_CONTEXT_TRUST_SECRET", ""),
		RuntimeEnvironment:                   getEnvStr("RUNTIME_ENVIRONMENT", "unspecified"),
		RuntimeDeploymentMode:                getEnvStr("RUNTIME_DEPLOYMENT_MODE", "single-node"),
		RuntimeDeploymentRole:                getEnvStr("RUNTIME_DEPLOYMENT_ROLE", "stable"),
		RuntimeVersion:                       getEnvStr("RUNTIME_VERSION", ""),
		RuntimeCommit:                        getEnvStr("RUNTIME_COMMIT", ""),
		PrometheusURL:                        strings.TrimRight(getEnvStr("PROMETHEUS_URL", ""), "/"),
		GrafanaURL:                           strings.TrimRight(getEnvStr("GRAFANA_URL", ""), "/"),
		CompletedExecutionFeedEnabled:        getEnvBool("COMPLETED_EXECUTION_FEED_ENABLED", false),
		CompletedExecutionFeedSourceInstance: strings.TrimSpace(getEnvStr("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "")),
		CompletedExecutionFeedReferenceMode: getEnvStr(
			"COMPLETED_EXECUTION_FEED_REFERENCE_MODE",
			CompletedExecutionFeedReferenceModeLegacy,
		),
		OTelExportEnabled:         getEnvBool("UNIROUTE_OTEL_EXPORT_ENABLED", false),
		OTelExportEndpoint:        strings.TrimSpace(getEnvStr("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")),
		OTelExportQueueSize:       getEnvInt("UNIROUTE_OTEL_EXPORT_QUEUE_SIZE", 256),
		OTelExportBatchSize:       getEnvInt("UNIROUTE_OTEL_EXPORT_BATCH_SIZE", 32),
		OTelExportFlushIntervalMS: getEnvInt("UNIROUTE_OTEL_EXPORT_FLUSH_INTERVAL_MS", 500),
		OTelExportTimeoutMS:       getEnvInt("UNIROUTE_OTEL_EXPORT_TIMEOUT_MS", 2000),
		OTelExportMaxRetries:      getEnvInt("UNIROUTE_OTEL_EXPORT_MAX_RETRIES", 2),

		AsyncMediaEnabled:             asyncMediaEnabled,
		AsyncMediaAdmissionEnabled:    getEnvBool("ASYNC_MEDIA_ADMISSION_ENABLED", false),
		AsyncMediaS3Region:            strings.TrimSpace(getEnvStr("ASYNC_MEDIA_S3_REGION", "us-east-1")),
		AsyncMediaS3Bucket:            strings.TrimSpace(getEnvStr("ASYNC_MEDIA_S3_BUCKET", "")),
		AsyncMediaS3Endpoint:          strings.TrimRight(strings.TrimSpace(getEnvStr("ASYNC_MEDIA_S3_ENDPOINT", "")), "/"),
		AsyncMediaS3Prefix:            strings.Trim(strings.TrimSpace(getEnvStr("ASYNC_MEDIA_S3_PREFIX", "unirouter-media")), "/"),
		AsyncMediaS3ForcePathStyle:    getEnvBool("ASYNC_MEDIA_S3_FORCE_PATH_STYLE", false),
		AsyncMediaWorkerID:            strings.TrimSpace(getEnvStr("ASYNC_MEDIA_WORKER_ID", "")),
		AsyncMediaLeaseSeconds:        getEnvInt("ASYNC_MEDIA_LEASE_SECONDS", 300),
		AsyncMediaPurgeLeaseSeconds:   getEnvInt("ASYNC_MEDIA_PURGE_LEASE_SECONDS", 60),
		AsyncMediaPollMilliseconds:    getEnvInt("ASYNC_MEDIA_POLL_MILLISECONDS", 500),
		AsyncMediaProviderTimeoutSecs: getEnvInt("ASYNC_MEDIA_PROVIDER_TIMEOUT_SECONDS", 180),
		AsyncMediaMaxAttempts:         getEnvInt("ASYNC_MEDIA_MAX_ATTEMPTS", 3),

		LicensePublicKey:         getEnvStr("LICENSE_PUBLIC_KEY", ""),
		LicensePublicKeys:        getEnvStr("LICENSE_PUBLIC_KEYS", ""),
		LicenseDeploymentID:      strings.TrimSpace(getEnvStr("LICENSE_DEPLOYMENT_ID", "")),
		LicenseExpiryWarningDays: getEnvInt("LICENSE_EXPIRY_WARNING_DAYS", 30),
		LicenseRefreshSeconds:    getEnvInt("LICENSE_REFRESH_SECONDS", 30),
		UpgradePublicKeys:        getEnvStr("UPGRADE_PUBLIC_KEYS", ""),

		OIDCEnabled:            getEnvBool("OIDC_ENABLED", false),
		OIDCIssuerURL:          strings.TrimRight(getEnvStr("OIDC_ISSUER_URL", ""), "/"),
		OIDCClientID:           getEnvStr("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:       getEnvStr("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:        getEnvStr("OIDC_REDIRECT_URL", ""),
		OIDCProviderName:       getEnvStr("OIDC_PROVIDER_NAME", "OIDC"),
		OIDCScopes:             splitCSV(getEnvStr("OIDC_SCOPES", "openid,email,profile")),
		OIDCAllowedEmailDomain: getEnvStr("OIDC_ALLOWED_EMAIL_DOMAIN", ""),
		OIDCAutoProvision:      getEnvBool("OIDC_AUTO_PROVISION", false),
		OIDCDefaultOrgID:       int64(getEnvInt("OIDC_DEFAULT_ORG_ID", 0)),
		OIDCDefaultRole:        auth.NormalizeRole(getEnvStr("OIDC_DEFAULT_ROLE", auth.RoleViewer)),
		OIDCDefaultProjectSlug: getEnvStr("OIDC_DEFAULT_PROJECT_SLUG", "default"),
		OIDCUsernameClaim:      getEnvStr("OIDC_USERNAME_CLAIM", "preferred_username"),
		OIDCEmailClaim:         getEnvStr("OIDC_EMAIL_CLAIM", "email"),
		OIDCRoleClaim:          getEnvStr("OIDC_ROLE_CLAIM", "uniroute_role"),
		OIDCOrgIDClaim:         getEnvStr("OIDC_ORG_ID_CLAIM", "uniroute_org_id"),

		MinFreezeCents:     int64(getEnvInt("MIN_FREEZE_AMOUNT", 10)),
		FreezeSafetyFactor: getEnvStr("FREEZE_SAFETY_FACTOR", "1.5"),
		FreezeTimeoutSecs:  getEnvInt("FREEZE_TIMEOUT_SECONDS", 300),
		ExchangeRate:       getEnvFloat("EXCHANGE_RATE", 7.25),

		DefaultKeyRPM: getEnvInt("DEFAULT_KEY_RPM", 60),
		DefaultKeyQPS: getEnvInt("DEFAULT_KEY_QPS", 10),

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

		GuardrailOrgActivePolicyLimit:      getEnvInt("GUARDRAIL_ORG_ACTIVE_POLICY_LIMIT", 256),
		GuardrailPlatformActivePolicyLimit: getEnvInt("GUARDRAIL_PLATFORM_ACTIVE_POLICY_LIMIT", 256),
		GuardrailCachedPolicyLimit:         getEnvInt("GUARDRAIL_CACHED_POLICY_LIMIT", 4096),
		GuardrailCachedTenantLimit:         getEnvInt("GUARDRAIL_CACHED_TENANT_LIMIT", 4096),

		ChatRequestMaxBytes:       getEnvInt("CHAT_REQUEST_MAX_BYTES", int(8*payloadpolicy.MiB)),
		EmbeddingsRequestMaxBytes: getEnvInt("EMBEDDINGS_REQUEST_MAX_BYTES", int(16*payloadpolicy.MiB)),
		NonStreamResponseMaxBytes: getEnvInt("NONSTREAM_RESPONSE_MAX_BYTES", int(16*payloadpolicy.MiB)),
		PayloadNodeInFlightBytes:  getEnvInt("PAYLOAD_NODE_INFLIGHT_BYTES", int(256*payloadpolicy.MiB)),
		PayloadOrgInFlightBytes:   getEnvInt("PAYLOAD_ORG_INFLIGHT_BYTES", int(64*payloadpolicy.MiB)),

		MaxRetryCount:              getEnvInt("MAX_RETRY_COUNT", 1),
		CircuitBreakerThreshold:    getEnvInt("CIRCUIT_BREAKER_THRESHOLD", 5),
		CircuitBreakerRecoverSecs:  getEnvInt("CIRCUIT_BREAKER_RECOVER_SECONDS", 300),
		CircuitBreakerHalfOpenPct:  getEnvInt("CIRCUIT_BREAKER_HALF_OPEN_PERCENT", 10),
		RateLimitCooldownSecs:      getEnvInt("KEY_COOLDOWN_SECONDS", 60),
		AuthFailureCooldownSecs:    getEnvInt("PROVIDER_AUTH_COOLDOWN_SECONDS", 3600),
		ProviderKeyFailThreshold:   getEnvInt("PROVIDER_KEY_FAILURE_THRESHOLD", 3),
		RoutingSnapshotRefreshSecs: getEnvInt("ROUTING_SNAPSHOT_REFRESH_SECONDS", 1),
		RoutingSnapshotStaleSecs:   getEnvInt("ROUTING_SNAPSHOT_STALE_SECONDS", 30),
		RoutingProbeLeaseSecs:      getEnvInt("ROUTING_HALF_OPEN_PROBE_LEASE_SECONDS", 15),
		RoutingMaxCandidates:       getEnvInt("ROUTING_MAX_CANDIDATES", 32),
		RoutingMaxSnapshots:        getEnvInt("ROUTING_MAX_SNAPSHOTS", 1024),

		StreamMaxSSELineBytes:      getEnvInt("STREAM_MAX_SSE_LINE_BYTES", 2*1024*1024),
		StreamWriteIdleTimeoutSecs: getEnvInt("STREAM_WRITE_IDLE_TIMEOUT_SECONDS", 30),
		StreamHeartbeatSecs:        getEnvInt("STREAM_HEARTBEAT_SECONDS", 15),
		StreamMaxDurationSecs:      getEnvInt("STREAM_MAX_DURATION_SECONDS", 30*60),

		BackupEnabled:    getEnvBool("BACKUP_ENABLED", true),
		BackupCron:       getEnvStr("BACKUP_CRON", "0 2 * * *"),
		BackupRetainDays: getEnvInt("BACKUP_RETAIN_DAYS", 7),

		TokenRatioZH: getEnvFloat("TOKEN_RATIO_ZH", 1.5),
		TokenRatioEN: getEnvFloat("TOKEN_RATIO_EN", 4),

		LogRetentionDays: logRetentionDays,

		SkipMigration: getEnvBool("SKIP_MIGRATION", false),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c Config) ResponseCacheConfigured() bool {
	return len(c.ResponseCacheIndexKey) == 32 && len(c.ResponseCacheEncryptionKey) == 32 && c.ResponseCacheKeyVersion != ""
}

func (c Config) GuardrailOptions() guardrail.Options {
	options := guardrail.DefaultOptions()
	if c.GuardrailOrgActivePolicyLimit != 0 {
		options.MaxActivePoliciesPerOrg = c.GuardrailOrgActivePolicyLimit
	}
	if c.GuardrailPlatformActivePolicyLimit != 0 {
		options.MaxActivePlatformPolicies = c.GuardrailPlatformActivePolicyLimit
	}
	if c.GuardrailCachedPolicyLimit != 0 {
		options.MaxCachedPolicies = c.GuardrailCachedPolicyLimit
	}
	if c.GuardrailCachedTenantLimit != 0 {
		options.MaxCachedTenantScopes = c.GuardrailCachedTenantLimit
	}
	return options
}

func (c Config) RoutingPolicyConfig() routingpolicy.Config {
	return routingpolicy.Config{
		MaxRetryCount:                c.MaxRetryCount,
		CircuitBreakerThreshold:      c.CircuitBreakerThreshold,
		CircuitBreakerRecoverSeconds: c.CircuitBreakerRecoverSecs,
		CircuitBreakerHalfOpenPct:    c.CircuitBreakerHalfOpenPct,
		RateLimitCooldownSeconds:     c.RateLimitCooldownSecs,
		AuthFailureCooldownSeconds:   c.AuthFailureCooldownSecs,
		KeyFailureThreshold:          c.ProviderKeyFailThreshold,
		SnapshotRefreshSeconds:       c.RoutingSnapshotRefreshSecs,
		SnapshotStaleSeconds:         c.RoutingSnapshotStaleSecs,
		HalfOpenProbeLeaseSeconds:    c.RoutingProbeLeaseSecs,
		MaxCandidateCount:            c.RoutingMaxCandidates,
		MaxSnapshotCount:             c.RoutingMaxSnapshots,
	}
}

func (c Config) StreamPolicyConfig() streampolicy.Config {
	return streampolicy.Config{
		MaxSSELineBytes:         c.StreamMaxSSELineBytes,
		WriteIdleTimeoutSeconds: c.StreamWriteIdleTimeoutSecs,
		HeartbeatSeconds:        c.StreamHeartbeatSecs,
		MaxDurationSeconds:      c.StreamMaxDurationSecs,
	}
}

func (c Config) WorkloadAdmissionOptions() workloadadmission.Options {
	options := workloadadmission.DefaultOptions()
	options.GlobalCapacity = int64(c.WorkloadGlobalCapacity)
	options.OrgCapacity = int64(c.WorkloadOrgCapacity)
	options.GlobalShortReserve = int64(c.WorkloadGlobalShortReserve)
	options.OrgShortReserve = int64(c.WorkloadOrgShortReserve)
	options.Interactive = workloadadmission.ClassPolicy{
		Weight:  int64(c.WorkloadInteractiveWeight),
		MaxWait: time.Duration(c.WorkloadInteractiveMaxWaitMS) * time.Millisecond,
	}
	options.Stream = workloadadmission.ClassPolicy{
		Weight:  int64(c.WorkloadStreamWeight),
		MaxWait: time.Duration(c.WorkloadStreamMaxWaitMS) * time.Millisecond,
	}
	options.Embedding = workloadadmission.ClassPolicy{
		Weight:  int64(c.WorkloadEmbeddingWeight),
		MaxWait: time.Duration(c.WorkloadEmbeddingMaxWaitMS) * time.Millisecond,
	}
	return options
}

func (c Config) PayloadLimits() payloadpolicy.Limits {
	limits := payloadpolicy.DefaultLimits()
	if c.ChatRequestMaxBytes != 0 {
		limits.ChatRequestBytes = int64(c.ChatRequestMaxBytes)
	}
	if c.EmbeddingsRequestMaxBytes != 0 {
		limits.EmbeddingsRequestBytes = int64(c.EmbeddingsRequestMaxBytes)
	}
	if c.NonStreamResponseMaxBytes != 0 {
		limits.NonStreamResponseBytes = int64(c.NonStreamResponseMaxBytes)
	}
	if c.PayloadNodeInFlightBytes != 0 {
		limits.NodeInFlightBytes = int64(c.PayloadNodeInFlightBytes)
	}
	if c.PayloadOrgInFlightBytes != 0 {
		limits.OrgInFlightBytes = int64(c.PayloadOrgInFlightBytes)
	}
	return limits
}

func (c Config) TelemetryExportConfig() telemetryexport.Config {
	defaults := telemetryexport.DefaultConfig()
	options := telemetryexport.Config{
		Enabled:          c.OTelExportEnabled,
		Endpoint:         c.OTelExportEndpoint,
		QueueSize:        c.OTelExportQueueSize,
		BatchSize:        c.OTelExportBatchSize,
		MaxRetries:       c.OTelExportMaxRetries,
		ServiceName:      "unirouter",
		ServiceNamespace: "foigent",
		ServiceVersion:   c.RuntimeVersion,
		DeploymentEnv:    c.RuntimeEnvironment,
	}
	if options.QueueSize == 0 {
		options.QueueSize = defaults.QueueSize
	}
	if options.BatchSize == 0 {
		options.BatchSize = defaults.BatchSize
	}
	if c.OTelExportFlushIntervalMS == 0 {
		options.FlushInterval = defaults.FlushInterval
	} else {
		options.FlushInterval = time.Duration(c.OTelExportFlushIntervalMS) * time.Millisecond
	}
	if c.OTelExportTimeoutMS == 0 {
		options.Timeout = defaults.Timeout
	} else {
		options.Timeout = time.Duration(c.OTelExportTimeoutMS) * time.Millisecond
	}
	return options
}

func (c Config) LicenseRefreshInterval() time.Duration {
	seconds := c.LicenseRefreshSeconds
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	if len(out) == 0 {
		return []string{"openid", "email", "profile"}
	}
	return out
}

func splitCSVAllowEmpty(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func validateOptionalHTTPURL(name, raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid http or https URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not include query string or fragment", name)
	}
	return nil
}

var s3BucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func validS3BucketName(value string) bool {
	value = strings.TrimSpace(value)
	if !s3BucketNamePattern.MatchString(value) ||
		strings.Contains(value, "..") ||
		strings.Contains(value, ".-") ||
		strings.Contains(value, "-.") {
		return false
	}
	return net.ParseIP(value) == nil
}

func isLocalRuntimeEnvironment(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "local", "development", "dev", "test":
		return true
	default:
		return false
	}
}

func isLoopbackHost(value string) bool {
	if strings.EqualFold(value, "localhost") {
		return true
	}
	address := net.ParseIP(value)
	return address != nil && address.IsLoopback()
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
