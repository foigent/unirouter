package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/traceutil"
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
	MinFreezeAmount    int
	FreezeSafetyFactor float64
	FreezeTimeoutSecs  int
	ExchangeRate       float64

	// Rate Limiting
	DefaultKeyRPM int
	DefaultKeyQPS int

	// Failover
	MaxRetryCount             int
	CircuitBreakerThreshold   int
	CircuitBreakerRecoverSecs int
	CircuitBreakerHalfOpenPct int
	KeyCooldownSecs           int

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

		MinFreezeAmount:    getEnvInt("MIN_FREEZE_AMOUNT", 10),
		FreezeSafetyFactor: getEnvFloat("FREEZE_SAFETY_FACTOR", 1.5),
		FreezeTimeoutSecs:  getEnvInt("FREEZE_TIMEOUT_SECONDS", 300),
		ExchangeRate:       getEnvFloat("EXCHANGE_RATE", 7.25),

		DefaultKeyRPM: getEnvInt("DEFAULT_KEY_RPM", 60),
		DefaultKeyQPS: getEnvInt("DEFAULT_KEY_QPS", 10),

		MaxRetryCount:             getEnvInt("MAX_RETRY_COUNT", 1),
		CircuitBreakerThreshold:   getEnvInt("CIRCUIT_BREAKER_THRESHOLD", 5),
		CircuitBreakerRecoverSecs: getEnvInt("CIRCUIT_BREAKER_RECOVER_SECONDS", 300),
		CircuitBreakerHalfOpenPct: getEnvInt("CIRCUIT_BREAKER_HALF_OPEN_PERCENT", 10),
		KeyCooldownSecs:           getEnvInt("KEY_COOLDOWN_SECONDS", 60),

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
