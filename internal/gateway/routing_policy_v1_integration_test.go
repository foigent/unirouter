//go:build integration

package gateway

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F08RoutingPolicyV1MultiReplicaLeaseEpochAndWeightIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	policy := routingpolicy.Default()
	policy.CircuitBreakerHalfOpenPct = 100
	policy.CircuitBreakerThreshold = 5
	policy.KeyFailureThreshold = 10
	policy.HalfOpenProbeLeaseSeconds = 1

	insertRoute := func(name, model string, priority, weight, threshold int, state string, recoveryReady bool) (int64, int64) {
		t.Helper()
		var channelID int64
		halfOpenExpression := "NULL"
		if recoveryReady {
			halfOpenExpression = "NOW() - INTERVAL '1 second'"
		}
		err := pool.QueryRow(ctx, fmt.Sprintf(`
INSERT INTO channels
       (name, provider, base_url, models, priority, routing_weight, degradation_threshold, status, circuit_state, half_open_until)
VALUES ($1, 'openai', 'https://provider.invalid', $2, $3, $4, $5, 'active', $6, %s)
RETURNING id`, halfOpenExpression), name, model, priority, weight, threshold, state).Scan(&channelID)
		require.NoError(t, err)
		var keyID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id, key_enc, status)
VALUES ($1, $2, 'active')
RETURNING id`, channelID, "encrypted-"+name).Scan(&keyID))
		return channelID, keyID
	}

	weightedModel := "gpt-p6f08-weighted"
	weight100ID, _ := insertRoute("weight-100", weightedModel, 10, 100, 4, "healthy", false)
	weight300ID, _ := insertRoute("weight-300", weightedModel, 10, 300, 4, "healthy", false)
	routerA, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	routerB, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)

	firstCounts := map[int64]int{}
	for index := 0; index < 1000; index++ {
		requestID := fmt.Sprintf("p6f08-shared-request-%d", index)
		planA, routeErr := routerA.RouteAllWithKey(ctx, weightedModel, requestID)
		require.NoError(t, routeErr)
		planB, routeErr := routerB.RouteAllWithKey(ctx, weightedModel, requestID)
		require.NoError(t, routeErr)
		require.Equal(t, []int64{planA[0].Channel.ID, planA[1].Channel.ID}, []int64{planB[0].Channel.ID, planB[1].Channel.ID}, "replicas must produce the same plan for one server request ID")
		firstCounts[planA[0].Channel.ID]++
	}
	require.Greater(t, firstCounts[weight300ID], 675)
	require.Less(t, firstCounts[weight300ID], 825)
	require.Equal(t, 1000, firstCounts[weight100ID]+firstCounts[weight300ID])

	probeModel := "gpt-p6f08-probe"
	probeChannelID, _ := insertRoute("probe", probeModel, 10, 100, 2, "half_open", true)
	probeRouterA, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	probeRouterB, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	planA, err := probeRouterA.RouteAllWithKey(ctx, probeModel, "p6f08-probe-request")
	require.NoError(t, err)
	planB, err := probeRouterB.RouteAllWithKey(ctx, probeModel, "p6f08-probe-request")
	require.NoError(t, err)
	require.True(t, planA[0].RequiresProbe)
	require.True(t, planB[0].RequiresProbe)

	type claimResult struct {
		route RouteResult
		ok    bool
		err   error
	}
	claims := make(chan claimResult, 2)
	var wait sync.WaitGroup
	for _, input := range []struct {
		router *Router
		route  RouteResult
	}{{probeRouterA, planA[0]}, {probeRouterB, planB[0]}} {
		input := input
		wait.Add(1)
		go func() {
			defer wait.Done()
			ok, claimErr := input.router.PrepareRoute(ctx, &input.route)
			claims <- claimResult{route: input.route, ok: ok, err: claimErr}
		}()
	}
	wait.Wait()
	close(claims)
	claimCount := 0
	var owner RouteResult
	for claim := range claims {
		require.NoError(t, claim.err)
		if claim.ok {
			claimCount++
			owner = claim.route
		}
	}
	require.Equal(t, 1, claimCount, "PostgreSQL lease must allow only one half-open probe across replicas")
	require.NotEmpty(t, owner.ProbeToken)
	stopRenewal := probeRouterA.MaintainProbeLease(ctx, &owner)
	time.Sleep(1500 * time.Millisecond)
	stillExcluded := planB[0]
	claimedAfterBaseTTL, claimErr := probeRouterB.PrepareRoute(ctx, &stillExcluded)
	require.NoError(t, claimErr)
	require.False(t, claimedAfterBaseTTL, "an active probe longer than the base lease must retain ownership through renewal")
	stopRenewal()
	var tokenPresent bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT half_open_probe_token IS NOT NULL FROM channels WHERE id=$1`, probeChannelID).Scan(&tokenPresent))
	require.True(t, tokenPresent)
	require.NoError(t, probeRouterA.MarkRouteSuccess(ctx, &owner))
	var circuitState string
	var circuitEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_state, circuit_epoch FROM channels WHERE id=$1`, probeChannelID).Scan(&circuitState, &circuitEpoch))
	require.Equal(t, "healthy", circuitState)
	require.EqualValues(t, 2, circuitEpoch)

	thresholdModel := "gpt-p6f08-threshold"
	thresholdChannelID, thresholdKeyID := insertRoute("threshold", thresholdModel, 10, 100, 2, "healthy", false)
	thresholdRouter, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	thresholdPlan, err := thresholdRouter.RouteAllWithKey(ctx, thresholdModel, "p6f08-threshold-request")
	require.NoError(t, err)
	staleRoute := thresholdPlan[0]
	require.Equal(t, 2, staleRoute.Channel.DegradationThreshold)
	require.NoError(t, thresholdRouter.MarkRouteFailure(ctx, staleRoute))
	require.NoError(t, thresholdRouter.MarkRouteFailure(ctx, staleRoute))
	var failCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_state, circuit_epoch, fail_count FROM channels WHERE id=$1`, thresholdChannelID).Scan(&circuitState, &circuitEpoch, &failCount))
	require.Equal(t, "half_open", circuitState)
	require.EqualValues(t, 2, circuitEpoch)
	require.Equal(t, 2, failCount, "channel-specific degradation threshold must drive the breaker")
	require.NoError(t, thresholdRouter.MarkRouteSuccess(ctx, &staleRoute))
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_state, fail_count FROM channels WHERE id=$1`, thresholdChannelID).Scan(&circuitState, &failCount))
	require.Equal(t, "half_open", circuitState, "late success from an older circuit epoch must not heal the breaker")
	require.Equal(t, 2, failCount)

	manualModel := "gpt-p6f08-manual-epoch"
	manualChannelID, manualKeyID := insertRoute("manual-epoch", manualModel, 10, 100, 4, "healthy", false)
	manualRouter, err := NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manualPlan, err := manualRouter.RouteAllWithKey(ctx, manualModel, "p6f08-manual-request")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE channels SET fail_count=7, routing_epoch=routing_epoch+1 WHERE id=$1`, manualChannelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE channel_keys SET fail_count=5 WHERE id=$1`, manualKeyID)
	require.NoError(t, err)
	require.NoError(t, manualRouter.MarkRouteSuccess(ctx, &manualPlan[0]))
	require.NoError(t, pool.QueryRow(ctx, `SELECT fail_count FROM channels WHERE id=$1`, manualChannelID).Scan(&failCount))
	require.Equal(t, 7, failCount, "late success from an older routing epoch must not overwrite manual state")
	require.NoError(t, pool.QueryRow(ctx, `SELECT fail_count FROM channel_keys WHERE id=$1`, manualKeyID).Scan(&failCount))
	require.Equal(t, 5, failCount, "late success from an older routing epoch must not overwrite key health")

	manualRouter.Invalidate()
	freshManualPlan, err := manualRouter.RouteAllWithKey(ctx, manualModel, "p6f08-manual-request-fresh")
	require.NoError(t, err)
	require.NoError(t, manualRouter.MarkRouteKeyRateLimited(ctx, freshManualPlan[0]))
	require.NoError(t, manualRouter.MarkRouteSuccess(ctx, &freshManualPlan[0]))
	var keyEpoch int64
	var cooldownUntil time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT fail_count, state_epoch, cooldown_until FROM channel_keys WHERE id=$1`, manualKeyID).Scan(&failCount, &keyEpoch, &cooldownUntil))
	require.Equal(t, 5, failCount, "late success must not clear a newer key cooldown epoch")
	require.EqualValues(t, 2, keyEpoch)
	require.True(t, cooldownUntil.After(time.Now()))

	var thresholdKeyStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM channel_keys WHERE id=$1`, thresholdKeyID).Scan(&thresholdKeyStatus))
	require.Equal(t, "active", thresholdKeyStatus)
}
