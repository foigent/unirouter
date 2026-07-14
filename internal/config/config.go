package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/streampolicy"
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

	// Runtime exposure hardening
	AdminCORSAllowedOrigins    []string
	MetricsEnabled             bool
	MetricsAllowedCIDRs        []string
	LoginRateLimitEnabled      bool
	LoginRateLimitWindowSec    int
	LoginRateLimitIP           int
	LoginRateLimitUser         int
	TraceEventsEnabled         bool
	TraceEventSampleRate       float64
	TraceEventRetentionDays    int
	PayloadCaptureMode         string
	EvidenceContextTrustSecret string
	RuntimeEnvironment         string
	RuntimeDeploymentMode      string
	RuntimeDeploymentRole      string
	RuntimeVersion             string
	RuntimeCommit              string
	PrometheusURL              string
	GrafanaURL                 string

	// License
	LicensePublicKey         string
	LicenseExpiryWarningDays int

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
	if c.ShutdownTimeoutSecs < 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be >= 0")
	}
	if c.LicenseExpiryWarningDays < 0 {
		return fmt.Errorf("LICENSE_EXPIRY_WARNING_DAYS must be >= 0")
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
	return nil
}

func Load() (*Config, error) {
	jwtSecret := getEnvStr("JWT_SECRET", "")
	logRetentionDays := getEnvInt("LOG_RETENTION_DAYS", 30)
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

		JWTSecret:        jwtSecret,
		APIKeyHashSecret: getEnvStr("API_KEY_HASH_SECRET", jwtSecret),
		EncryptionKey:    getEnvStr("ENCRYPTION_KEY", ""),

		AdminCORSAllowedOrigins:    splitCSVAllowEmpty(getEnvStr("ADMIN_CORS_ALLOWED_ORIGINS", "")),
		MetricsEnabled:             getEnvBool("METRICS_ENABLED", true),
		MetricsAllowedCIDRs:        splitCSVAllowEmpty(getEnvStr("METRICS_ALLOWED_CIDRS", "")),
		LoginRateLimitEnabled:      getEnvBool("LOGIN_RATE_LIMIT_ENABLED", true),
		LoginRateLimitWindowSec:    getEnvInt("LOGIN_RATE_LIMIT_WINDOW_SECONDS", 60),
		LoginRateLimitIP:           getEnvInt("LOGIN_RATE_LIMIT_IP_REQUESTS", 30),
		LoginRateLimitUser:         getEnvInt("LOGIN_RATE_LIMIT_USER_REQUESTS", 10),
		TraceEventsEnabled:         getEnvBool("TRACE_EVENTS_ENABLED", true),
		TraceEventSampleRate:       getEnvFloat("TRACE_EVENT_SAMPLE_RATE", 1),
		TraceEventRetentionDays:    getEnvInt("TRACE_EVENT_RETENTION_DAYS", logRetentionDays),
		PayloadCaptureMode:         traceutil.NormalizePayloadCaptureMode(getEnvStr("PAYLOAD_CAPTURE_MODE", traceutil.PayloadCaptureModeMetadataOnly)),
		EvidenceContextTrustSecret: getEnvStr("EVIDENCE_CONTEXT_TRUST_SECRET", ""),
		RuntimeEnvironment:         getEnvStr("RUNTIME_ENVIRONMENT", "unspecified"),
		RuntimeDeploymentMode:      getEnvStr("RUNTIME_DEPLOYMENT_MODE", "single-node"),
		RuntimeDeploymentRole:      getEnvStr("RUNTIME_DEPLOYMENT_ROLE", "stable"),
		RuntimeVersion:             getEnvStr("RUNTIME_VERSION", ""),
		RuntimeCommit:              getEnvStr("RUNTIME_COMMIT", ""),
		PrometheusURL:              strings.TrimRight(getEnvStr("PROMETHEUS_URL", ""), "/"),
		GrafanaURL:                 strings.TrimRight(getEnvStr("GRAFANA_URL", ""), "/"),

		LicensePublicKey:         getEnvStr("LICENSE_PUBLIC_KEY", ""),
		LicenseExpiryWarningDays: getEnvInt("LICENSE_EXPIRY_WARNING_DAYS", 30),

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
