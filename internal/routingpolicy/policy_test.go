package routingpolicy

import "testing"

func TestDefaultPolicyIsValidAndStable(t *testing.T) {
	policy := Default()
	if err := policy.Validate(); err != nil {
		t.Fatalf("default policy invalid: %v", err)
	}
	if Version != "routing-config.v1" {
		t.Fatalf("unexpected policy version %q", Version)
	}
	if policy.MaxRetryCount != 1 || policy.CircuitBreakerThreshold != 5 || policy.CircuitBreakerRecoverSeconds != 300 ||
		policy.CircuitBreakerHalfOpenPct != 10 || policy.RateLimitCooldownSeconds != 60 ||
		policy.AuthFailureCooldownSeconds != 3600 || policy.KeyFailureThreshold != 3 {
		t.Fatalf("unexpected defaults: %#v", policy)
	}
}

func TestPolicyValidationBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "negative retries", mutate: func(config *Config) { config.MaxRetryCount = -1 }},
		{name: "excess retries", mutate: func(config *Config) { config.MaxRetryCount = 11 }},
		{name: "zero circuit threshold", mutate: func(config *Config) { config.CircuitBreakerThreshold = 0 }},
		{name: "zero recovery", mutate: func(config *Config) { config.CircuitBreakerRecoverSeconds = 0 }},
		{name: "zero half-open percent", mutate: func(config *Config) { config.CircuitBreakerHalfOpenPct = 0 }},
		{name: "excess half-open percent", mutate: func(config *Config) { config.CircuitBreakerHalfOpenPct = 101 }},
		{name: "zero rate cooldown", mutate: func(config *Config) { config.RateLimitCooldownSeconds = 0 }},
		{name: "zero auth cooldown", mutate: func(config *Config) { config.AuthFailureCooldownSeconds = 0 }},
		{name: "zero key threshold", mutate: func(config *Config) { config.KeyFailureThreshold = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := Default()
			test.mutate(&policy)
			if err := policy.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
