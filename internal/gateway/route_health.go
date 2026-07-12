package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PrepareRoute claims the only cross-replica lease required on the request
// path: ownership of a half-open probe. Healthy routes need no per-request
// coordination or last_used_at write.
func (r *Router) PrepareRoute(ctx context.Context, route *RouteResult) (bool, error) {
	if route == nil {
		return false, fmt.Errorf("route is required")
	}
	if !route.RequiresProbe {
		return true, nil
	}
	token, err := newProbeToken()
	if err != nil {
		return false, err
	}
	var circuitEpoch int64
	err = r.db.QueryRow(ctx, `
UPDATE channels
   SET half_open_probe_until = NOW() + ($4 * INTERVAL '1 second'),
       half_open_probe_token = $5,
       updated_at = NOW()
 WHERE id = $1
   AND routing_epoch = $2
   AND circuit_epoch = $3
   AND status = 'active'
   AND circuit_state = 'half_open'
   AND half_open_until <= NOW()
   AND (half_open_probe_until IS NULL OR half_open_probe_until <= NOW())
 RETURNING circuit_epoch`,
		route.Channel.ID,
		route.Channel.RoutingEpoch,
		route.Channel.CircuitEpoch,
		r.policy.HalfOpenProbeLeaseSeconds,
		token,
	).Scan(&circuitEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim half-open probe: %w", err)
	}
	route.ProbeToken = token
	return true, nil
}

func newProbeToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate probe token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// MaintainProbeLease renews a claimed half-open lease while a slow upstream is
// still active. This keeps the single-probe invariant for long TTFT, large
// non-stream bodies, and streams that outlive the base crash-recovery lease.
// The returned stop function is idempotent and waits for the renewer to exit.
func (r *Router) MaintainProbeLease(ctx context.Context, route *RouteResult) func() {
	if route == nil || !route.RequiresProbe || route.ProbeToken == "" {
		return func() {}
	}
	leaseCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	interval := r.policy.HalfOpenProbeLeaseTTL() / 3
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				_, err := r.db.Exec(leaseCtx, `
UPDATE channels
   SET half_open_probe_until = NOW() + ($5 * INTERVAL '1 second'),
       updated_at = NOW()
 WHERE id = $1
   AND routing_epoch = $2
   AND circuit_epoch = $3
   AND circuit_state = 'half_open'
   AND half_open_probe_token = $4`,
					route.Channel.ID, route.Channel.RoutingEpoch, route.Channel.CircuitEpoch, route.ProbeToken,
					r.policy.HalfOpenProbeLeaseSeconds,
				)
				if err != nil && leaseCtx.Err() == nil {
					// A transient renewal failure is retried on the next tick. The
					// bounded lease still guarantees crash recovery.
					continue
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

// MarkRouteSuccess applies a response only if the routing/circuit/key epochs
// captured by its immutable plan are still current. A late response therefore
// cannot heal a newer breaker or clear a newer key cooldown.
func (r *Router) MarkRouteSuccess(ctx context.Context, route *RouteResult) error {
	if route == nil {
		return fmt.Errorf("route is required")
	}
	wasProbe := route.RequiresProbe
	var channelErr error
	channelChanged := false
	if route.RequiresProbe {
		if route.ProbeToken == "" {
			return fmt.Errorf("half-open route has no probe ownership")
		}
		var tag pgconn.CommandTag
		tag, channelErr = r.db.Exec(ctx, `
UPDATE channels
   SET fail_count = 0,
       circuit_state = 'healthy',
       half_open_until = NULL,
       half_open_probe_until = NULL,
       half_open_probe_token = NULL,
       circuit_epoch = circuit_epoch + 1,
       updated_at = NOW()
 WHERE id = $1
   AND routing_epoch = $2
   AND circuit_epoch = $3
   AND circuit_state = 'half_open'
   AND half_open_probe_token = $4`,
			route.Channel.ID, route.Channel.RoutingEpoch, route.Channel.CircuitEpoch, route.ProbeToken,
		)
		channelChanged = channelErr == nil && tag.RowsAffected() > 0
		if channelChanged {
			route.Channel.CircuitEpoch++
			route.Channel.CircuitState = "healthy"
			route.RequiresProbe = false
			route.ProbeToken = ""
		}
	} else {
		var tag pgconn.CommandTag
		tag, channelErr = r.db.Exec(ctx, `
UPDATE channels
   SET fail_count = 0, updated_at = NOW()
 WHERE id = $1
   AND routing_epoch = $2
   AND circuit_epoch = $3
   AND circuit_state = 'healthy'`,
			route.Channel.ID, route.Channel.RoutingEpoch, route.Channel.CircuitEpoch,
		)
		channelChanged = channelErr == nil && tag.RowsAffected() > 0
	}
	_, keyErr := r.db.Exec(ctx, `
UPDATE channel_keys AS ck
   SET fail_count = 0, last_used_at = NOW(), updated_at = NOW()
  FROM channels AS c
 WHERE ck.id = $1
   AND ck.state_epoch = $2
   AND ck.status = 'active'
   AND c.id = $3
   AND c.id = ck.channel_id
   AND c.routing_epoch = $4`,
		route.ChannelKey.ID, route.ChannelKey.StateEpoch, route.Channel.ID, route.Channel.RoutingEpoch,
	)
	if wasProbe && channelChanged {
		r.Invalidate()
	}
	return errors.Join(channelErr, keyErr)
}

// MarkRouteFailure records a retryable channel failure and key failure. The
// channel-specific UI threshold is authoritative, with the startup threshold
// already normalized into the route when old/default rows are read.
func (r *Router) MarkRouteFailure(ctx context.Context, route RouteResult) error {
	threshold := route.Channel.DegradationThreshold
	if threshold < 1 {
		threshold = r.policy.CircuitBreakerThreshold
	}
	var channelErr error
	if route.RequiresProbe {
		_, channelErr = r.db.Exec(ctx, `
UPDATE channels
   SET fail_count = fail_count + 1,
       circuit_state = 'half_open',
       half_open_until = NOW() + ($5 * INTERVAL '1 second'),
       half_open_probe_until = NULL,
       half_open_probe_token = NULL,
       circuit_epoch = circuit_epoch + 1,
       updated_at = NOW()
 WHERE id = $1
   AND routing_epoch = $2
   AND circuit_epoch = $3
   AND circuit_state = 'half_open'
   AND half_open_probe_token = $4`,
			route.Channel.ID, route.Channel.RoutingEpoch, route.Channel.CircuitEpoch, route.ProbeToken,
			r.policy.CircuitBreakerRecoverSeconds,
		)
	} else {
		_, channelErr = r.db.Exec(ctx, `
UPDATE channels
   SET fail_count = fail_count + 1,
       circuit_state = CASE WHEN fail_count + 1 >= $4 THEN 'half_open' ELSE circuit_state END,
       half_open_until = CASE WHEN fail_count + 1 >= $4 THEN NOW() + ($5 * INTERVAL '1 second') ELSE half_open_until END,
       half_open_probe_until = CASE WHEN fail_count + 1 >= $4 THEN NULL ELSE half_open_probe_until END,
       half_open_probe_token = CASE WHEN fail_count + 1 >= $4 THEN NULL ELSE half_open_probe_token END,
       circuit_epoch = CASE WHEN fail_count + 1 >= $4 THEN circuit_epoch + 1 ELSE circuit_epoch END,
       updated_at = NOW()
 WHERE id = $1
   AND routing_epoch = $2
   AND circuit_epoch = $3
   AND circuit_state = 'healthy'`,
			route.Channel.ID, route.Channel.RoutingEpoch, route.Channel.CircuitEpoch, threshold,
			r.policy.CircuitBreakerRecoverSeconds,
		)
	}
	keyErr := r.markRouteKeyFailure(ctx, route)
	r.Invalidate()
	return errors.Join(channelErr, keyErr)
}

func (r *Router) markRouteKeyFailure(ctx context.Context, route RouteResult) error {
	_, err := r.db.Exec(ctx, `
UPDATE channel_keys AS ck
   SET fail_count = ck.fail_count + 1,
       status = CASE WHEN ck.fail_count + 1 >= $3 THEN 'invalid' ELSE ck.status END,
       state_epoch = CASE WHEN ck.fail_count + 1 >= $3 THEN ck.state_epoch + 1 ELSE ck.state_epoch END,
       updated_at = NOW()
  FROM channels AS c
 WHERE ck.id = $1
   AND ck.state_epoch = $2
   AND ck.status = 'active'
   AND c.id = $4
   AND c.id = ck.channel_id
   AND c.routing_epoch = $5`,
		route.ChannelKey.ID, route.ChannelKey.StateEpoch, r.policy.KeyFailureThreshold,
		route.Channel.ID, route.Channel.RoutingEpoch,
	)
	return err
}

func (r *Router) MarkRouteKeyRateLimited(ctx context.Context, route RouteResult) error {
	return r.markRouteKeyCooldown(ctx, route, r.policy.RateLimitCooldownSeconds)
}

func (r *Router) MarkRouteKeyAuthFailure(ctx context.Context, route RouteResult) error {
	return r.markRouteKeyCooldown(ctx, route, r.policy.AuthFailureCooldownSeconds)
}

func (r *Router) markRouteKeyCooldown(ctx context.Context, route RouteResult, seconds int) error {
	_, err := r.db.Exec(ctx, `
UPDATE channel_keys AS ck
   SET cooldown_until = NOW() + ($3 * INTERVAL '1 second'),
       state_epoch = ck.state_epoch + 1,
       updated_at = NOW()
  FROM channels AS c
 WHERE ck.id = $1
   AND ck.state_epoch = $2
   AND ck.status = 'active'
   AND c.id = $4
   AND c.id = ck.channel_id
   AND c.routing_epoch = $5`,
		route.ChannelKey.ID, route.ChannelKey.StateEpoch, seconds,
		route.Channel.ID, route.Channel.RoutingEpoch,
	)
	r.Invalidate()
	return err
}
