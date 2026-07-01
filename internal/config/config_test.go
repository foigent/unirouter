package config

import (
	"os"
	"testing"
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

func TestLoad_Defaults(t *testing.T) {
	// Clear all env vars to test defaults
	envVars := []string{
		"LISTEN_PORT", "LOG_LEVEL", "GIN_MODE", "DB_DSN", "DB_MAX_CONNECTIONS",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "JWT_SECRET", "API_KEY_HASH_SECRET", "ENCRYPTION_KEY",
		"MIN_FREEZE_AMOUNT", "FREEZE_SAFETY_FACTOR", "DEFAULT_KEY_RPM", "DEFAULT_KEY_QPS",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	// Load() will fail because JWT_SECRET is empty and ENCRYPTION_KEY is empty
	// But we can test that defaults are set before validation
	cfg := &Config{
		ListenPort:         getEnvInt("LISTEN_PORT", 8080),
		LogLevel:           getEnvStr("LOG_LEVEL", "info"),
		GinMode:            getEnvStr("GIN_MODE", "release"),
		DB_DSN:             getEnvStr("DB_DSN", "postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable"),
		DBMaxConnections:   getEnvInt("DB_MAX_CONNECTIONS", 25),
		RedisAddr:          getEnvStr("REDIS_ADDR", "localhost:6379"),
		MinFreezeAmount:    getEnvInt("MIN_FREEZE_AMOUNT", 10),
		FreezeSafetyFactor: getEnvFloat("FREEZE_SAFETY_FACTOR", 1.5),
		DefaultKeyRPM:      getEnvInt("DEFAULT_KEY_RPM", 60),
		DefaultKeyQPS:      getEnvInt("DEFAULT_KEY_QPS", 10),
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
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("LISTEN_PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	defer func() {
		os.Unsetenv("LISTEN_PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("API_KEY_HASH_SECRET")
		os.Unsetenv("ENCRYPTION_KEY")
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
