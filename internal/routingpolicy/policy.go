package routingpolicy

import (
	"fmt"
	"time"
)

const (
	Version          = "routing-config.v2"
	SelectionVersion = "routing-policy.v1"
)

const (
	maxProviderRetryCount      = 10
	maxCircuitFailureThreshold = 100
	maxCircuitRecoverySeconds  = 86400
	maxKeyCooldownSeconds      = 604800
	maxKeyFailureThreshold     = 100
	maxSnapshotRefreshSeconds  = 60
	maxSnapshotStaleSeconds    = 300
	maxProbeLeaseSeconds       = 300
	maxRouteCandidates         = 128
	maxRouteSnapshots          = 10000
)

// Config is the startup source for bounded provider retries, snapshots,
// probe leases and channel/key health transitions. Dynamic channel priority,
// weight and epochs are persisted separately in PostgreSQL.
type Config struct {
	MaxRetryCount                int `json:"max_retry_count"`
	CircuitBreakerThreshold      int `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverSeconds int `json:"circuit_breaker_recover_seconds"`
	CircuitBreakerHalfOpenPct    int `json:"circuit_breaker_half_open_percent"`
	RateLimitCooldownSeconds     int `json:"rate_limit_cooldown_seconds"`
	AuthFailureCooldownSeconds   int `json:"auth_failure_cooldown_seconds"`
	KeyFailureThreshold          int `json:"key_failure_threshold"`
	SnapshotRefreshSeconds       int `json:"snapshot_refresh_seconds"`
	SnapshotStaleSeconds         int `json:"snapshot_stale_seconds"`
	HalfOpenProbeLeaseSeconds    int `json:"half_open_probe_lease_seconds"`
	MaxCandidateCount            int `json:"max_candidate_count"`
	MaxSnapshotCount             int `json:"max_snapshot_count"`
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
		SnapshotRefreshSeconds:       1,
		SnapshotStaleSeconds:         30,
		HalfOpenProbeLeaseSeconds:    15,
		MaxCandidateCount:            32,
		MaxSnapshotCount:             1024,
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
	case config.SnapshotRefreshSeconds < 1 || config.SnapshotRefreshSeconds > maxSnapshotRefreshSeconds:
		return fmt.Errorf("snapshot refresh seconds must be between 1 and %d", maxSnapshotRefreshSeconds)
	case config.SnapshotStaleSeconds < config.SnapshotRefreshSeconds || config.SnapshotStaleSeconds > maxSnapshotStaleSeconds:
		return fmt.Errorf("snapshot stale seconds must be between snapshot refresh seconds and %d", maxSnapshotStaleSeconds)
	case config.HalfOpenProbeLeaseSeconds < 1 || config.HalfOpenProbeLeaseSeconds > maxProbeLeaseSeconds:
		return fmt.Errorf("half-open probe lease seconds must be between 1 and %d", maxProbeLeaseSeconds)
	case config.MaxCandidateCount < 1 || config.MaxCandidateCount > maxRouteCandidates:
		return fmt.Errorf("max candidate count must be between 1 and %d", maxRouteCandidates)
	case config.MaxSnapshotCount < 1 || config.MaxSnapshotCount > maxRouteSnapshots:
		return fmt.Errorf("max snapshot count must be between 1 and %d", maxRouteSnapshots)
	default:
		return nil
	}
}

func (config Config) SnapshotRefreshTTL() time.Duration {
	return time.Duration(config.SnapshotRefreshSeconds) * time.Second
}

func (config Config) SnapshotStaleTTL() time.Duration {
	return time.Duration(config.SnapshotStaleSeconds) * time.Second
}

func (config Config) HalfOpenProbeLeaseTTL() time.Duration {
	return time.Duration(config.HalfOpenProbeLeaseSeconds) * time.Second
}

// Preview is a credential-free explanation of one immutable routing snapshot.
// It is safe for the platform-operator API and never contains an upstream URL,
// encrypted credential, request payload, or raw provider response.
type Preview struct {
	PolicyVersion   string             `json:"policy_version"`
	ConfigVersion   string             `json:"config_version"`
	SnapshotVersion string             `json:"snapshot_version"`
	SnapshotSource  string             `json:"snapshot_source"`
	Degraded        bool               `json:"degraded"`
	Model           string             `json:"model"`
	Candidates      []PreviewCandidate `json:"candidates"`
	Exclusions      []PreviewCandidate `json:"exclusions"`
}

type PreviewCandidate struct {
	ChannelID        int64  `json:"channel_id"`
	ChannelName      string `json:"channel_name"`
	Provider         string `json:"provider"`
	Priority         int    `json:"priority"`
	Weight           int    `json:"weight"`
	Order            int    `json:"order,omitempty"`
	CircuitState     string `json:"circuit_state"`
	EligibleKeyCount int    `json:"eligible_key_count"`
	Reason           string `json:"reason"`
	RequiresProbe    bool   `json:"requires_probe"`
}
