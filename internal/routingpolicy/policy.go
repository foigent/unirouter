package routingpolicy

import "fmt"

const Version = "routing-config.v1"

const (
	maxProviderRetryCount      = 10
	maxCircuitFailureThreshold = 100
	maxCircuitRecoverySeconds  = 86400
	maxKeyCooldownSeconds      = 604800
	maxKeyFailureThreshold     = 100
)

// Config is the single effective source for bounded provider retries and
// persisted channel/key health transitions. It is loaded once at startup;
// dynamic routing policy is intentionally outside P6-F05.
type Config struct {
	MaxRetryCount                int `json:"max_retry_count"`
	CircuitBreakerThreshold      int `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverSeconds int `json:"circuit_breaker_recover_seconds"`
	CircuitBreakerHalfOpenPct    int `json:"circuit_breaker_half_open_percent"`
	RateLimitCooldownSeconds     int `json:"rate_limit_cooldown_seconds"`
	AuthFailureCooldownSeconds   int `json:"auth_failure_cooldown_seconds"`
	KeyFailureThreshold          int `json:"key_failure_threshold"`
}

func Default() Config {
	return Config{
		MaxRetryCount:                1,
		CircuitBreakerThreshold:      5,
		CircuitBreakerRecoverSeconds: 300,
		CircuitBreakerHalfOpenPct:    10,
		RateLimitCooldownSeconds:     60,
		AuthFailureCooldownSeconds:   3600,
		KeyFailureThreshold:          3,
	}
}

func (config Config) Validate() error {
	switch {
	case config.MaxRetryCount < 0 || config.MaxRetryCount > maxProviderRetryCount:
		return fmt.Errorf("max retry count must be between 0 and %d", maxProviderRetryCount)
	case config.CircuitBreakerThreshold < 1 || config.CircuitBreakerThreshold > maxCircuitFailureThreshold:
		return fmt.Errorf("circuit breaker threshold must be between 1 and %d", maxCircuitFailureThreshold)
	case config.CircuitBreakerRecoverSeconds < 1 || config.CircuitBreakerRecoverSeconds > maxCircuitRecoverySeconds:
		return fmt.Errorf("circuit breaker recovery seconds must be between 1 and %d", maxCircuitRecoverySeconds)
	case config.CircuitBreakerHalfOpenPct < 1 || config.CircuitBreakerHalfOpenPct > 100:
		return fmt.Errorf("circuit breaker half-open percent must be between 1 and 100")
	case config.RateLimitCooldownSeconds < 1 || config.RateLimitCooldownSeconds > maxKeyCooldownSeconds:
		return fmt.Errorf("rate-limit cooldown seconds must be between 1 and %d", maxKeyCooldownSeconds)
	case config.AuthFailureCooldownSeconds < 1 || config.AuthFailureCooldownSeconds > maxKeyCooldownSeconds:
		return fmt.Errorf("auth-failure cooldown seconds must be between 1 and %d", maxKeyCooldownSeconds)
	case config.KeyFailureThreshold < 1 || config.KeyFailureThreshold > maxKeyFailureThreshold:
		return fmt.Errorf("key failure threshold must be between 1 and %d", maxKeyFailureThreshold)
	default:
		return nil
	}
}
