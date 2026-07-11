//go:build integration

package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRoutingPolicyPostgresStateTransitions(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name, provider, base_url, models, priority, status, circuit_state)
VALUES ('routing-policy-it', 'openai', 'https://provider.invalid', 'gpt-policy-it', 1, 'active', 'healthy')
RETURNING id`).Scan(&channelID))
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id, key_enc, status)
VALUES ($1, 'encrypted-test-key', 'active')
RETURNING id`, channelID).Scan(&keyID))

	policy := routingpolicy.Default()
	policy.CircuitBreakerThreshold = 2
	policy.CircuitBreakerRecoverSeconds = 1
	policy.CircuitBreakerHalfOpenPct = 100
	policy.RateLimitCooldownSeconds = 2
	policy.AuthFailureCooldownSeconds = 3
	policy.KeyFailureThreshold = 4
	router, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)

	routes, err := router.RouteAll(ctx, "gpt-policy-it")
	require.NoError(t, err)
	require.Len(t, routes, 1)

	require.NoError(t, router.MarkChannelFail(ctx, channelID))
	var state string
	var failCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_state, fail_count FROM channels WHERE id = $1`, channelID).Scan(&state, &failCount))
	require.Equal(t, "healthy", state)
	require.Equal(t, 1, failCount)

	require.NoError(t, router.MarkChannelFail(ctx, channelID))
	var recoveryDeadline time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_state, fail_count, half_open_until FROM channels WHERE id = $1`, channelID).Scan(&state, &failCount, &recoveryDeadline))
	require.Equal(t, "half_open", state)
	require.Equal(t, 2, failCount)
	require.True(t, recoveryDeadline.After(time.Now()), "configured recovery deadline must be in the future")

	_, err = router.RouteAll(ctx, "gpt-policy-it")
	require.Error(t, err, "half-open route must be excluded before its recovery deadline")
	require.Eventually(t, func() bool {
		routes, routeErr := router.RouteAll(ctx, "gpt-policy-it")
		return routeErr == nil && len(routes) == 1
	}, 3*time.Second, 50*time.Millisecond, "half-open route must become eligible after the real PostgreSQL deadline")

	require.NoError(t, router.MarkChannelSuccess(ctx, channelID))
	var deadlineCleared bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_state, fail_count, half_open_until IS NULL FROM channels WHERE id = $1`, channelID).Scan(&state, &failCount, &deadlineCleared))
	require.Equal(t, "healthy", state)
	require.Zero(t, failCount)
	require.True(t, deadlineCleared)

	rateStarted := time.Now()
	require.NoError(t, router.MarkKeyRateLimited(ctx, keyID))
	var cooldownUntil time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT cooldown_until FROM channel_keys WHERE id = $1`, keyID).Scan(&cooldownUntil))
	require.WithinDuration(t, rateStarted.Add(2*time.Second), cooldownUntil, time.Second)
	_, err = router.RouteAll(ctx, "gpt-policy-it")
	require.Error(t, err, "rate-limited key must be excluded during configured cooldown")

	_, err = pool.Exec(ctx, `UPDATE channel_keys SET cooldown_until = NULL WHERE id = $1`, keyID)
	require.NoError(t, err)
	authStarted := time.Now()
	require.NoError(t, router.MarkKeyAuthFailure(ctx, keyID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT cooldown_until FROM channel_keys WHERE id = $1`, keyID).Scan(&cooldownUntil))
	require.WithinDuration(t, authStarted.Add(3*time.Second), cooldownUntil, time.Second)

	_, err = pool.Exec(ctx, `UPDATE channel_keys SET cooldown_until = NULL, fail_count = 0, status = 'active' WHERE id = $1`, keyID)
	require.NoError(t, err)
	for attempt := 1; attempt <= 3; attempt++ {
		require.NoError(t, router.MarkKeyFail(ctx, keyID))
	}
	var keyStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status, fail_count FROM channel_keys WHERE id = $1`, keyID).Scan(&keyStatus, &failCount))
	require.Equal(t, "active", keyStatus)
	require.Equal(t, 3, failCount)
	require.NoError(t, router.MarkKeyFail(ctx, keyID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status, fail_count FROM channel_keys WHERE id = $1`, keyID).Scan(&keyStatus, &failCount))
	require.Equal(t, "invalid", keyStatus)
	require.Equal(t, 4, failCount)
}
