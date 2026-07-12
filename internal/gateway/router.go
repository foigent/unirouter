package gateway

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"golang.org/x/sync/singleflight"
)

// Router selects the best channel and key for a given model.
type Router struct {
	db           db.DBQuerier
	policy       routingpolicy.Config
	cacheMu      sync.RWMutex
	cache        map[string]*routeSnapshot
	refreshGroup singleflight.Group
	now          func() time.Time
}

// NewRouter creates a new Router.
func NewRouter(db db.DBQuerier) *Router {
	return newRouter(db, routingpolicy.Default())
}

// NewRouterWithPolicy creates a Router with one validated startup policy.
func NewRouterWithPolicy(db db.DBQuerier, policy routingpolicy.Config) (*Router, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return newRouter(db, policy), nil
}

func newRouter(db db.DBQuerier, policy routingpolicy.Config) *Router {
	return &Router{
		db:     db,
		policy: policy,
		cache:  make(map[string]*routeSnapshot),
		now:    time.Now,
	}
}

func (r *Router) Policy() routingpolicy.Config {
	if r == nil {
		return routingpolicy.Default()
	}
	return r.policy
}

// RouteResult contains the selected channel, key, and mapped model name.
type RouteResult struct {
	Channel              models.Channel
	ChannelKey           models.ChannelKey
	MappedModel          string
	RoutingPolicyVersion string
	SnapshotVersion      string
	SnapshotSource       string
	SnapshotDegraded     bool
	CandidateCount       int
	ExcludedCount        int
	ExclusionReasons     string
	SelectionReason      string
	RequiresProbe        bool
	ProbeToken           string
}

// MarkChannelSuccess records a successful request for a channel.
// Resets circuit breaker state to healthy if it was half_open.
func (r *Router) MarkChannelSuccess(ctx context.Context, channelID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE channels SET fail_count = 0, circuit_state = 'healthy', half_open_until = NULL
		 WHERE id = $1`,
		channelID,
	)
	r.Invalidate()
	return err
}

// MarkChannelFail records a failed request for a channel.
// If failure count reaches the effective threshold, transitions to half_open.
func (r *Router) MarkChannelFail(ctx context.Context, channelID int64) error {
	var failCount int
	err := r.db.QueryRow(ctx,
		`UPDATE channels SET fail_count = fail_count + 1
		 WHERE id = $1
		 RETURNING fail_count`,
		channelID,
	).Scan(&failCount)
	if err != nil {
		return err
	}

	if failCount >= r.policy.CircuitBreakerThreshold {
		halfOpenUntil := time.Now().Add(time.Duration(r.policy.CircuitBreakerRecoverSeconds) * time.Second)
		_, err = r.db.Exec(ctx,
			`UPDATE channels SET circuit_state = 'half_open', half_open_until = $1
			 WHERE id = $2`,
			halfOpenUntil, channelID,
		)
		r.Invalidate()
		return err
	}
	r.Invalidate()
	return nil
}

// MarkKeyCooldown puts a key on cooldown for the specified duration.
func (r *Router) MarkKeyCooldown(ctx context.Context, keyID int64, cooldownSecs int) error {
	cooldownUntil := time.Now().Add(time.Duration(cooldownSecs) * time.Second)
	_, err := r.db.Exec(ctx,
		`UPDATE channel_keys SET cooldown_until = $1 WHERE id = $2`,
		cooldownUntil, keyID,
	)
	r.Invalidate()
	return err
}

func (r *Router) MarkKeyRateLimited(ctx context.Context, keyID int64) error {
	return r.MarkKeyCooldown(ctx, keyID, r.policy.RateLimitCooldownSeconds)
}

func (r *Router) MarkKeyAuthFailure(ctx context.Context, keyID int64) error {
	return r.MarkKeyCooldown(ctx, keyID, r.policy.AuthFailureCooldownSeconds)
}

// resolveModelMapping applies model alias mapping.
// ModelMapping is a JSON string like: {"gpt-4": "gpt-4-0613"}
func resolveModelMapping(requestedModel string, modelMapping json.RawMessage) string {
	if len(modelMapping) == 0 {
		return requestedModel
	}
	mmStr := string(modelMapping)
	if mmStr == "{}" || mmStr == "null" {
		return requestedModel
	}
	var mapping map[string]string
	if err := json.Unmarshal(modelMapping, &mapping); err != nil {
		return requestedModel
	}
	if mapped, ok := mapping[requestedModel]; ok {
		return mapped
	}
	return requestedModel
}

// MarkKeyFail increments fail_count on a channel_key.
// If fail_count reaches the effective threshold, sets status='invalid'.
func (r *Router) MarkKeyFail(ctx context.Context, keyID int64) error {
	var failCount int
	err := r.db.QueryRow(ctx,
		`UPDATE channel_keys SET fail_count = fail_count + 1, updated_at = NOW()
		 WHERE id = $1
		 RETURNING fail_count`,
		keyID,
	).Scan(&failCount)
	if err != nil {
		return err
	}
	if failCount >= r.policy.KeyFailureThreshold {
		_, err = r.db.Exec(ctx,
			`UPDATE channel_keys SET status = 'invalid', updated_at = NOW() WHERE id = $1`,
			keyID,
		)
		log.Printf("Auto-invalidated channel_key %d after %d consecutive failures", keyID, failCount)
		r.Invalidate()
		return err
	}
	r.Invalidate()
	return nil
}

// MarkKeySuccess resets fail_count on a channel_key.
func (r *Router) MarkKeySuccess(ctx context.Context, keyID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE channel_keys SET fail_count = 0, last_used_at = NOW(), updated_at = NOW()
		 WHERE id = $1`,
		keyID,
	)
	r.Invalidate()
	return err
}
