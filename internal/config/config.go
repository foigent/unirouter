package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	// Server
	ListenPort    int
	LogLevel      string
	GinMode       string
	MigrationsDir string

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

	// License
	LicensePublicKey         string
	LicenseExpiryWarningDays int

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
	if c.LicenseExpiryWarningDays < 0 {
		return fmt.Errorf("LICENSE_EXPIRY_WARNING_DAYS must be >= 0")
	}
	return nil
}

func Load() (*Config, error) {
	jwtSecret := getEnvStr("JWT_SECRET", "")
	cfg := &Config{
		ListenPort:    getEnvInt("LISTEN_PORT", 8080),
		LogLevel:      getEnvStr("LOG_LEVEL", "info"),
		GinMode:       getEnvStr("GIN_MODE", "release"),
		MigrationsDir: getEnvStr("MIGRATIONS_DIR", ""),

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

		LicensePublicKey:         getEnvStr("LICENSE_PUBLIC_KEY", ""),
		LicenseExpiryWarningDays: getEnvInt("LICENSE_EXPIRY_WARNING_DAYS", 30),

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

		LogRetentionDays: getEnvInt("LOG_RETENTION_DAYS", 30),

		SkipMigration: getEnvBool("SKIP_MIGRATION", false),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
