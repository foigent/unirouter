//go:build integration

package modelexecution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type executionRuntimeSnapshotBuilder struct{}

func (executionRuntimeSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

func TestCoordinatorCommitsOneTruthfulAttemptInEveryAuthorityMode(t *testing.T) {
	tests := []struct {
		name       string
		mode       modelrouting.Mode
		selectedV2 bool
	}{
		{name: "legacy", mode: modelrouting.ModeLegacy},
		{name: "verify-v1", mode: modelrouting.ModeVerify},
		{name: "verify-v2", mode: modelrouting.ModeVerify, selectedV2: true},
		{name: "canonical", mode: modelrouting.ModeCanonical, selectedV2: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, test.selectedV2, false)
			plan, route := fixture.plan(t, test.mode, "selection-"+test.name)
			identity := fixture.freeze(t, "request-"+test.name)
			coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
			require.NoError(t, err)
			decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
			require.NoError(t, err)
			require.Equal(t, Applied, decision.Status)
			require.NotEmpty(t, decision.EvidenceHash)
			require.Empty(t, decision.ProbeLeaseToken)
			var attempts int
			var channelID, channelKeyID *int64
			var executionSnapshotHash, routeSnapshotHash *string
			require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*),MAX(channel_id),MAX(channel_key_id),
       MAX(execution_snapshot_hash),MAX(route_snapshot_hash)
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2`, fixture.orgID, identity.RequestID).Scan(
				&attempts, &channelID, &channelKeyID, &executionSnapshotHash, &routeSnapshotHash,
			))
			require.Equal(t, 1, attempts)
			require.NotNil(t, executionSnapshotHash)
			require.Equal(t, plan.SnapshotHash, *executionSnapshotHash)
			if test.mode == modelrouting.ModeCanonical {
				require.Nil(t, channelID)
				require.Nil(t, channelKeyID)
			} else {
				require.NotNil(t, channelID)
				require.NotNil(t, channelKeyID)
			}
			if test.mode == modelrouting.ModeLegacy {
				require.Nil(t, routeSnapshotHash, "legacy route evidence must remain immutable and snapshot-free")
			}
			_, err = fixture.pool.Exec(t.Context(), `
UPDATE billing_provider_attempts
   SET execution_snapshot_hash=$3
 WHERE org_id=$1 AND request_id=$2`, fixture.orgID, identity.RequestID,
				"mrs1_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
			require.ErrorContains(t, err, "model execution Plan snapshot is immutable")
		})
	}
}

func TestRepeatedAuthorizeCannotMintASecondDispatchOwner(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "authorize-owner-replay")
	identity := fixture.freeze(t, "authorize-owner-replay")
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)

	first, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, first.Status)
	require.NotEmpty(t, first.DispatchOwnerToken)

	second, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Stale, second.Status)
	require.Empty(t, second.DispatchOwnerToken)
	fixture.requireAttemptCount(t, identity.RequestID, 1)
}

func TestProviderAttemptPersistsTheFrozenAccessGrantIdentity(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "access-identity")
	identity, credentialID, credentialRef, grantID := fixture.freezeWithAccessIdentity(t, "access-identity")
	identity.AccessCredentialID = credentialID + 1000
	identity.CredentialRef = "cred_untrusted_envelope_0001"
	identity.AccessGrantID = grantID + 1000
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)

	var storedCredentialID, storedGrantID int64
	var storedCredentialRef string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT access_credential_id,credential_ref,access_grant_id
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
		identity.RequestID, identity.AttemptID).Scan(
		&storedCredentialID, &storedCredentialRef, &storedGrantID,
	))
	require.Equal(t, credentialID, storedCredentialID)
	require.Equal(t, credentialRef, storedCredentialRef)
	require.Equal(t, grantID, storedGrantID)
}

// The lifecycle transaction tests prove atomic retargeting. This exercises the
// existing dispatch boundary it relies on: retirement rejects a prepared plan,
// while a committed attempt retains its original identity through completion.
func TestRetirementRejectsPreparedPlanAndPreservesAuthorizedAttemptIntegration(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "retire-dispatch")
	identity := fixture.freeze(t, "authorized-before-retire")
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	authorized, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, authorized.Status)
	preparedIdentity := fixture.freeze(t, "prepared-before-retire")
	_, err = fixture.pool.Exec(t.Context(), `UPDATE model_registry SET lifecycle='retired',approved_catalog=FALSE WHERE id=$1`, plan.PublicModelID)
	require.NoError(t, err)
	denied, err := coordinator.Authorize(t.Context(), plan, route, preparedIdentity)
	require.NoError(t, err)
	require.Equal(t, Disabled, denied.Status)
	fixture.requireAttemptCount(t, preparedIdentity.RequestID, 0)
	require.NoError(t, fixture.billing.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{
		OrgID: authorized.ProviderAttemptKey.OrgID, RequestID: authorized.ProviderAttemptKey.RequestID,
		AttemptID: authorized.ProviderAttemptKey.AttemptID,
		Outcome:   billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	var modelID, releaseID int64
	var hash string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `SELECT route_public_model_id,route_runtime_release_id,route_evidence_hash
 FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		fixture.orgID, identity.RequestID, identity.AttemptID).Scan(&modelID, &releaseID, &hash))
	require.Equal(t, plan.PublicModelID, modelID)
	require.Equal(t, plan.RuntimeReleaseID, releaseID)
	require.Equal(t, authorized.EvidenceHash, hash)
}

func TestCoordinatorRejectsEveryPinnedAuthorityDriftWithoutAttempt(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		mutate func(*testing.T, *executionFixture)
	}{
		{
			name: "deployment revision", status: Stale,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET priority=priority+1,config_revision=config_revision+1 WHERE id=$1`, fixture.deploymentID)
				require.NoError(t, err)
			},
		},
		{
			name: "administrative disable", status: Disabled,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, fixture.connectionID)
				require.NoError(t, err)
			},
		},
		{
			name: "credential relink", status: Stale,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
DELETE FROM credential_binding_deployments WHERE binding_id=$1 AND deployment_id=$2`, fixture.bindingID, fixture.deploymentID)
				require.NoError(t, err)
				_, err = fixture.pool.Exec(t.Context(), `
INSERT INTO credential_binding_deployments(binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW())`, fixture.bindingID, fixture.deploymentID, fixture.connectionID)
				require.NoError(t, err)
			},
		},
		{
			name: "newer own probe", status: Stale,
			mutate: func(t *testing.T, fixture *executionFixture) {
				fixture.insertProbe(t, "openai.chat_completions.v1", "integration.openai.chat", "1.0.0", "newer-probe")
			},
		},
		{
			name: "slot drift", status: Stale,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE runtime_change_slots SET slot_version=slot_version+1 WHERE model_name=$1`, fixture.model)
				require.NoError(t, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, false)
			plan, route := fixture.plan(t, modelrouting.ModeCanonical, "drift-"+test.name)
			identity := fixture.freeze(t, "drift-"+test.name)
			test.mutate(t, fixture)
			coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
			require.NoError(t, err)
			decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
			require.NoError(t, err)
			require.Equal(t, test.status, decision.Status)
			fixture.requireAttemptCount(t, identity.RequestID, 0)
		})
	}
}

func TestVerifySecondSideFailureRollsBackCanonicalProbeClaim(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	siblingID := fixture.insertSharedDeployment(t)
	plan, route := fixture.plan(t, modelrouting.ModeVerify, "verify-rollback")
	require.True(t, route.RequiresProbe)
	identity := fixture.freeze(t, "verify-rollback")
	_, err := fixture.pool.Exec(t.Context(), `
UPDATE channels SET status='disabled',routing_epoch=routing_epoch+1 WHERE id=$1`, fixture.channelID)
	require.NoError(t, err)
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Disabled, decision.Status)
	var canonicalLease, siblingLease *string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT half_open_probe_token FROM model_deployments WHERE id=$1`, fixture.deploymentID).Scan(&canonicalLease))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT half_open_probe_token FROM model_deployments WHERE id=$1`, siblingID).Scan(&siblingLease))
	require.Nil(t, canonicalLease, "legacy-side rejection must roll back the earlier canonical claim")
	require.Nil(t, siblingLease, "legacy-side rejection must roll back every shared canonical claim")
	fixture.requireAttemptCount(t, identity.RequestID, 0)
}

func TestVerifySharedProbeGroupRejectsDivergenceBeforeAttempt(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		mutate func(*testing.T, *executionFixture, int64)
	}{
		{
			name: "breaker projection", status: Stale,
			mutate: func(t *testing.T, fixture *executionFixture, siblingID int64) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET fail_count=fail_count+1 WHERE id=$1`, siblingID)
				require.NoError(t, err)
			},
		},
		{
			name: "live sibling owner", status: LeaseLost,
			mutate: func(t *testing.T, fixture *executionFixture, siblingID int64) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments
   SET half_open_probe_token='sibling-owner',half_open_probe_until=clock_timestamp()+INTERVAL '30 seconds'
 WHERE id=$1`, siblingID)
				require.NoError(t, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, true)
			siblingID := fixture.insertSharedDeployment(t)
			test.mutate(t, fixture, siblingID)
			plan, route := fixture.plan(t, modelrouting.ModeVerify, "shared-divergence-"+test.name)
			identity := fixture.freeze(t, "shared-divergence-"+test.name)
			coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
			require.NoError(t, err)
			decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
			require.NoError(t, err)
			require.Equal(t, test.status, decision.Status)
			fixture.requireAttemptCount(t, identity.RequestID, 0)
			_, selected := fixture.breakers(t)
			require.Empty(t, selected.probeToken)
		})
	}
}

func TestTwoPoolsContendForOneCanonicalHalfOpenDispatch(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "two-pool-half-open")
	require.True(t, route.RequiresProbe)
	identities := []AttemptIdentity{
		fixture.freeze(t, "half-open-a"),
		fixture.freeze(t, "half-open-b"),
	}
	config, err := pgxpool.ParseConfig(fixture.pool.Config().ConnString())
	require.NoError(t, err)
	secondPool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(secondPool.Close)
	coordinators := make([]*Coordinator, 2)
	coordinators[0], err = New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	coordinators[1], err = New(secondPool, billing.NewBilling(secondPool, nil), DefaultConfig())
	require.NoError(t, err)

	start := make(chan struct{})
	type result struct {
		decision Decision
		err      error
	}
	results := make(chan result, 2)
	var group sync.WaitGroup
	for index := range coordinators {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			decision, authorizeErr := coordinators[index].Authorize(context.Background(), plan, route, identities[index])
			results <- result{decision: decision, err: authorizeErr}
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	counts := map[Status]int{}
	for item := range results {
		require.NoError(t, item.err)
		counts[item.decision.Status]++
		if item.decision.Status == Applied {
			require.NotEmpty(t, item.decision.ProbeLeaseToken)
			require.Equal(t, probeTokenHash(item.decision.ProbeLeaseToken), item.decision.Evidence.ProbeLeaseTokenHash)
		}
	}
	require.Equal(t, 1, counts[Applied])
	require.Equal(t, 1, counts[LeaseLost])
	var attempts int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1`, fixture.orgID).Scan(&attempts))
	require.Equal(t, 1, attempts)
}

func TestTwoPoolsContendForOneVerifySharedHalfOpenDispatch(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	siblingID := fixture.insertSharedDeployment(t)
	plan, route := fixture.plan(t, modelrouting.ModeVerify, "two-pool-verify-half-open")
	identities := []AttemptIdentity{
		fixture.freeze(t, "verify-half-open-a"),
		fixture.freeze(t, "verify-half-open-b"),
	}
	config, err := pgxpool.ParseConfig(fixture.pool.Config().ConnString())
	require.NoError(t, err)
	secondPool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(secondPool.Close)
	coordinators := make([]*Coordinator, 2)
	coordinators[0], err = New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	coordinators[1], err = New(secondPool, billing.NewBilling(secondPool, nil), DefaultConfig())
	require.NoError(t, err)

	start := make(chan struct{})
	type result struct {
		decision Decision
		err      error
	}
	results := make(chan result, 2)
	var group sync.WaitGroup
	for index := range coordinators {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			decision, authorizeErr := coordinators[index].Authorize(context.Background(), plan, route, identities[index])
			results <- result{decision: decision, err: authorizeErr}
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	counts := map[Status]int{}
	for item := range results {
		require.NoError(t, item.err)
		counts[item.decision.Status]++
	}
	require.Equal(t, 1, counts[Applied])
	require.Equal(t, 1, counts[LeaseLost])
	var attempts int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1`, fixture.orgID).Scan(&attempts))
	require.Equal(t, 1, attempts)
	legacy, selected := fixture.breakers(t)
	requireBreakerEquivalent(t, legacy, selected)
	requireBreakerEquivalent(t, legacy, fixture.breaker(t, siblingID, false))
}

func TestVerifyProbeClaimUsesOnePostLockWallClock(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	siblingID := fixture.insertSharedDeployment(t)
	plan, route := fixture.plan(t, modelrouting.ModeVerify, "post-lock-claim-clock")
	identity := fixture.freeze(t, "post-lock-claim-clock")
	config := DefaultConfig()
	config.HalfOpenLease = time.Second
	coordinator, err := New(fixture.pool, fixture.billing, config)
	require.NoError(t, err)

	blocker, err := fixture.pool.Begin(t.Context())
	require.NoError(t, err)
	defer blocker.Rollback(t.Context())
	var lockedID int64
	require.NoError(t, blocker.QueryRow(t.Context(), `
SELECT id FROM channels WHERE id=$1 FOR UPDATE`, fixture.channelID).Scan(&lockedID))

	type authorizationResult struct {
		decision Decision
		err      error
	}
	result := make(chan authorizationResult, 1)
	go func() {
		decision, authorizeErr := coordinator.Authorize(context.Background(), plan, route, identity)
		result <- authorizationResult{decision: decision, err: authorizeErr}
	}()
	waitForDatabaseLock(t, fixture.pool)
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, blocker.Commit(t.Context()))

	authorized := <-result
	require.NoError(t, authorized.err)
	require.Equal(t, Applied, authorized.decision.Status)
	legacy, selected := fixture.breakers(t)
	sibling := fixture.breaker(t, siblingID, false)
	requireBreakerEquivalent(t, legacy, selected)
	requireBreakerEquivalent(t, legacy, sibling)
	require.WithinDuration(t, authorized.decision.DatabaseTime.Add(time.Second), *legacy.probeUntil, 10*time.Millisecond)
	var wallClock time.Time
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&wallClock))
	require.True(t, legacy.probeUntil.After(wallClock), "committed dispatch authority must retain a live lease")
}

func TestRenewAfterLockWaitPastLeaseReturnsLeaseLost(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "post-lock-renew-clock")
	identity := fixture.freeze(t, "post-lock-renew-clock")
	config := DefaultConfig()
	config.HalfOpenLease = time.Second
	coordinator, err := New(fixture.pool, fixture.billing, config)
	require.NoError(t, err)
	decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)

	blocker, err := fixture.pool.Begin(t.Context())
	require.NoError(t, err)
	defer blocker.Rollback(t.Context())
	var lockedID int64
	require.NoError(t, blocker.QueryRow(t.Context(), `
SELECT id FROM model_deployments WHERE id=$1 FOR UPDATE`, fixture.deploymentID).Scan(&lockedID))

	type renewalResult struct {
		result HealthResult
		err    error
	}
	renewal := make(chan renewalResult, 1)
	go func() {
		value, renewErr := coordinator.RenewLease(context.Background(), decision)
		renewal <- renewalResult{result: value, err: renewErr}
	}()
	waitForDatabaseLock(t, fixture.pool)
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, blocker.Commit(t.Context()))

	renewed := <-renewal
	require.NoError(t, renewed.err)
	require.Equal(t, LeaseLost, renewed.result.Status)
}

func TestCredentialFailureAfterLockWaitPastLeaseRollsBackTheTerminalFence(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "post-lock-auth-clock")
	identity := fixture.freeze(t, "post-lock-auth-clock")
	config := DefaultConfig()
	config.HalfOpenLease = time.Second
	coordinator, err := New(fixture.pool, fixture.billing, config)
	require.NoError(t, err)
	decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)
	fixture.completeRejectedAttempt(t, decision, "auth")
	_, credentialBefore := fixture.credentials(t)

	blocker, err := fixture.pool.Begin(t.Context())
	require.NoError(t, err)
	defer blocker.Rollback(t.Context())
	var lockedID int64
	require.NoError(t, blocker.QueryRow(t.Context(), `
SELECT id FROM model_deployments WHERE id=$1 FOR UPDATE`, fixture.deploymentID).Scan(&lockedID))

	type healthTransitionResult struct {
		result HealthResult
		err    error
	}
	transition := make(chan healthTransitionResult, 1)
	go func() {
		value, transitionErr := coordinator.MarkCredentialAuthFailure(context.Background(), decision)
		transition <- healthTransitionResult{result: value, err: transitionErr}
	}()
	waitForDatabaseLock(t, fixture.pool)
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, blocker.Commit(t.Context()))

	applied := <-transition
	require.NoError(t, applied.err)
	require.Equal(t, LeaseLost, applied.result.Status)
	_, credentialAfter := fixture.credentials(t)
	require.Equal(t, credentialBefore, credentialAfter)
	var outcome *string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_outcome FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		decision.ProviderAttemptKey.OrgID, decision.ProviderAttemptKey.RequestID,
		decision.ProviderAttemptKey.AttemptID).Scan(&outcome))
	require.Nil(t, outcome, "lease loss must roll back the terminal health fence")
}

func waitForDatabaseLock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		err := pool.QueryRow(t.Context(), `
SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
     WHERE datname=current_database() AND pid<>pg_backend_pid()
       AND wait_event_type='Lock'
)`).Scan(&waiting)
		require.NoError(t, err)
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the execution transaction to block on a row lock")
}

type executionFixture struct {
	pool                                *pgxpool.Pool
	billing                             *billing.Billing
	orgID, userID, apiKeyID             int64
	publicModelID                       int64
	channelID, legacyKeyID              int64
	connectionID, deploymentID          int64
	bindingID                           int64
	legacyReleaseID, canonicalReleaseID int64
	canonicalReleaseRef                 string
	canonicalReleaseVersion             int64
	model                               string
	halfOpen                            bool
	lastPlanSnapshotHash                string
}

func newExecutionFixture(t *testing.T, selectedV2, halfOpen bool) *executionFixture {
	t.Helper()
	return executionFixtureWithPool(t, testutil.SetupMigratedTestDB(t), selectedV2, halfOpen)
}

func newPreRetirementExecutionFixture(t *testing.T, selectedV2, halfOpen bool) *executionFixture {
	t.Helper()
	return executionFixtureWithPool(t, testutil.SetupPreRetirementTestDB(t), selectedV2, halfOpen)
}

func executionFixtureWithPool(t *testing.T, pool *pgxpool.Pool, selectedV2, halfOpen bool) *executionFixture {
	t.Helper()
	fixture := &executionFixture{pool: pool, model: fmt.Sprintf("exec-%d", time.Now().UnixNano()), halfOpen: halfOpen}
	fixture.orgID = testutil.NewTestOrg(t, pool)
	testutil.NewTestWalletCents(t, pool, fixture.orgID, 10000)
	ctx := t.Context()
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users(org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'fixture','platform_admin','active','platform_admin') RETURNING id`,
		fixture.orgID, "exec-owner-"+fixture.model).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO api_keys(org_id,name,key_prefix,key_hash,status)
VALUES ($1,'execution-key',$2,$3,'active') RETURNING id`, fixture.orgID,
		"ur-"+hashText("prefix-" + fixture.model)[7:19], hashText("api-key-" + fixture.model)[7:]).Scan(&fixture.apiKeyID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents)
VALUES ($1,'custom',2,10)`, fixture.model)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry(
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit)
VALUES ($1,$2,$2,'custom','ga',TRUE,128000,8192) RETURNING id`,
		fixture.orgID, fixture.model).Scan(&fixture.publicModelID))

	circuitState := "healthy"
	var halfOpenUntil any
	if halfOpen {
		circuitState = "half_open"
		halfOpenUntil = time.Now().Add(-time.Minute)
	}
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels(
    name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
    max_context,default_max_output,priority,routing_weight,status,circuit_state,
    half_open_until,routing_epoch,circuit_epoch)
VALUES ('execution-connection','custom','https://provider.invalid',$1,$2::jsonb,
        'openai_compatible','{}',128000,8192,10,100,'active',$3,$4,1,1)
RETURNING id`, fixture.model, fmt.Sprintf(`{"%s":"upstream-model"}`, fixture.model),
		circuitState, halfOpenUntil).Scan(&fixture.channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys(channel_id,key_enc,allowed_models,status,state_epoch)
VALUES ($1,'cipher:execution',$2,'active',1) RETURNING id`, fixture.channelID, fixture.model).Scan(&fixture.legacyKeyID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections(
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'execution-connection','custom','https://provider.invalid','openai_compatible','{}','enabled',1)
RETURNING id`, fixture.channelID).Scan(&fixture.connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments(
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,
    circuit_state,circuit_revision,half_open_until,legacy_channel_id,legacy_public_model)
VALUES ($1,$2,$3,'upstream-model','enabled',1,10,100,128000,8192,$4,1,$5,$6,$7)
RETURNING id`, fixture.orgID, fixture.publicModelID, fixture.connectionID,
		circuitState, halfOpenUntil, fixture.channelID, fixture.model).Scan(&fixture.deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings(
    connection_id,key_enc,status,state_revision,legacy_channel_key_id)
VALUES ($1,'cipher:execution','active',1,$2) RETURNING id`,
		fixture.connectionID, fixture.legacyKeyID).Scan(&fixture.bindingID))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments(binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute')`, fixture.bindingID, fixture.deploymentID, fixture.connectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations(
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared','legacy compatibility',1,
        'legacy.channel_model_probe','v1')`, fixture.deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations(
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','production_approved','integration:chat',NOW(),1,
		'integration.openai.chat','1.0.0')`, fixture.deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations(
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.embeddings.v1','enabled','production_approved','integration:embeddings',NOW(),1,
        'integration.openai.embeddings','1.0.0')`, fixture.deploymentID)
	require.NoError(t, err)
	fixture.insertProbe(t, "legacy.any.v1", "legacy.channel_model_probe", "v1", "legacy-probe")
	fixture.insertProbe(t, "openai.chat_completions.v1", "integration.openai.chat", "1.0.0", "chat-probe")
	fixture.insertProbe(t, "openai.embeddings.v1", "integration.openai.embeddings", "1.0.0", "embeddings-probe")
	fixture.insertReleases(t)
	if selectedV2 {
		fixture.activateCanonical(t)
	}
	fixture.billing = billing.NewBilling(pool, nil)
	return fixture
}

func (fixture *executionFixture) insertProbe(t *testing.T, operation, profile, version, source string) {
	t.Helper()
	_, err := fixture.pool.Exec(t.Context(), `
INSERT INTO model_deployment_probes(
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,'upstream-model','upstream-model',1,1,$6,1,TRUE,200,2,'',
        'integration.modelexecution',$7,$8,$9,NOW())`,
		fixture.deploymentID, fixture.connectionID, operation, profile, version,
		fixture.bindingID, source+fixture.model, hashText(source+fixture.model), fixture.userID)
	require.NoError(t, err)
}

func (fixture *executionFixture) insertReleases(t *testing.T) {
	t.Helper()
	override := routingpolicy.ChannelOverride{
		ChannelID: fixture.channelID, ExpectedRoutingEpoch: 1, Admission: routingpolicy.AdmissionAllow,
	}
	legacyCandidate := struct {
		ContractVersion string                          `json:"contract_version"`
		ModelName       string                          `json:"model_name"`
		Overrides       []routingpolicy.ChannelOverride `json:"overrides"`
	}{legacyCandidateVersion, fixture.model, []routingpolicy.ChannelOverride{override}}
	legacyJSON := mustJSON(t, legacyCandidate)
	legacyItem := routeItemDocument{
		DeploymentID: fixture.deploymentID, ModelOperationID: "legacy.any.v1",
		ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1, ExpectedOperationRevision: 1,
		ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1",
		Admission: routingpolicy.AdmissionAllow,
	}
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
INSERT INTO runtime_change_releases(
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ($1,$2,$3,$4,$5,$6::jsonb,'[]','{}','unchanged','active',1,0,
        'execution v1 fixture',$7,$7,$7,NOW(),NOW()) RETURNING id`,
		"rcr_exec_v1_"+fixture.model, fixture.model, legacyCandidateVersion,
		hashBytes(legacyJSON), hashText("baseline-v1"+fixture.model), string(legacyJSON), fixture.userID,
	).Scan(&fixture.legacyReleaseID))
	fixture.insertRouteItem(t, fixture.legacyReleaseID, legacyItem, legacyCandidateVersion)
	_, err := fixture.pool.Exec(t.Context(), `
UPDATE runtime_change_releases
   SET public_model_id=$2,route_items_hash=$3,route_items_count=1,route_items_sealed_at=NOW()
 WHERE id=$1`, fixture.legacyReleaseID, fixture.publicModelID, manifestHash([]routeItemDocument{legacyItem}))
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), `
INSERT INTO runtime_change_slots(model_name,stable_release_id,canary_basis_points,slot_version)
VALUES ($1,$2,0,1)`, fixture.model, fixture.legacyReleaseID)
	require.NoError(t, err)

	planner, err := modelrouting.NewPlanner(fixture.pool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		fixture.pool, executionRuntimeSnapshotBuilder{}, routingpolicy.Default(), planner,
	)
	require.NoError(t, err)
	release := integrationfixture.AuthorSealedDeploymentRelease(
		t, fixture.pool, manager, integrationfixture.AtomicRelease{
			PublicModelID: fixture.publicModelID,
			CustomerOrgID: fixture.orgID,
			ActorUserID:   fixture.userID,
			ActorOrgID:    fixture.orgID,
			Selections: []runtimechange.DeploymentReleaseSelection{
				{DeploymentID: fixture.deploymentID, OperationID: "openai.chat_completions.v1"},
				{DeploymentID: fixture.deploymentID, OperationID: "openai.embeddings.v1"},
			},
			Reason: "execution paired v1 to v2 fixture",
		},
	)
	fixture.canonicalReleaseID = release.ID
	fixture.canonicalReleaseRef = release.ReleaseRef
	fixture.canonicalReleaseVersion = release.Version
}

func (fixture *executionFixture) insertRouteItem(t *testing.T, releaseID int64, item routeItemDocument, version string) {
	t.Helper()
	_, err := fixture.pool.Exec(t.Context(), `
INSERT INTO runtime_route_revision_items(
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,expected_operation_revision,protocol_profile_id,
    protocol_profile_version,admission,priority,weight,source_candidate_version)
VALUES ($1,1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		releaseID, item.DeploymentID, item.ModelOperationID, item.ExpectedConnectionRevision,
		item.ExpectedDeploymentRevision, item.ExpectedOperationRevision, item.ProtocolProfileID,
		item.ProtocolProfileVersion, item.Admission, item.Priority, item.Weight, version)
	require.NoError(t, err)
}

func (fixture *executionFixture) activateCanonical(t *testing.T) {
	t.Helper()
	policy := routingpolicy.Default()
	if fixture.halfOpen {
		policy.CircuitBreakerHalfOpenPct = 100
	}
	planner, err := modelrouting.NewPlanner(fixture.pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		fixture.pool, executionRuntimeSnapshotBuilder{}, policy, planner,
	)
	require.NoError(t, err)
	receipt := integrationfixture.ApplyDeploymentLifecycle(
		t, manager, integrationfixture.AtomicRelease{
			PublicModelID: fixture.publicModelID,
			CustomerOrgID: fixture.orgID,
			ActorUserID:   fixture.userID,
			ActorOrgID:    fixture.orgID,
		}, runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             fixture.canonicalReleaseRef,
			ExpectedReleaseVersion: fixture.canonicalReleaseVersion,
			ExpectedSlotVersion:    1,
			Reason:                 "execution fixture canonical cutover",
		},
	)
	require.Equal(t, receipt.LegacyPlanHash, receipt.DeploymentPlanHash)
}

func (fixture *executionFixture) plan(t *testing.T, mode modelrouting.Mode, selection string) (modelrouting.Plan, modelrouting.Route) {
	t.Helper()
	policy := routingpolicy.Default()
	if fixture.halfOpen {
		policy.CircuitBreakerHalfOpenPct = 100
	}
	planner, err := modelrouting.NewPlanner(fixture.pool, policy)
	require.NoError(t, err)
	plan, err := planner.Plan(t.Context(), modelrouting.Request{
		Mode: mode, CustomerOrgID: fixture.orgID, PublicModel: fixture.model,
		ModelOperationID: "openai.chat_completions.v1", SelectionKey: selection,
	})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Routes)
	fixture.lastPlanSnapshotHash = plan.SnapshotHash
	return plan, plan.Routes[0]
}

func (fixture *executionFixture) freeze(t *testing.T, suffix string) AttemptIdentity {
	t.Helper()
	requestID := "exec-" + suffix
	result, err := fixture.billing.Freeze(t.Context(), billing.FreezeRequest{
		RequestID: requestID, OrgID: fixture.orgID, APIKeyID: fixture.apiKeyID,
		Model: fixture.model, EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 1,
		ExecutionSnapshotHash: fixture.lastPlanSnapshotHash,
	})
	require.NoError(t, err)
	require.True(t, result.Success, result.ErrorDetail)
	return AttemptIdentity{
		RequestID: requestID, AttemptID: "attempt-" + suffix,
		OrgID: fixture.orgID, APIKeyID: fixture.apiKeyID,
		RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions",
	}
}

func (fixture *executionFixture) freezeWithAccessIdentity(
	t *testing.T,
	suffix string,
) (AttemptIdentity, int64, string, int64) {
	t.Helper()
	ctx := t.Context()
	var projectID, accessRequestID, grantID, credentialID, apiKeyID int64
	require.NoError(t, fixture.pool.QueryRow(ctx, `
INSERT INTO projects(org_id,name,slug,status,budget_period)
VALUES ($1,'Execution Access','execution-access','active','none') RETURNING id`,
		fixture.orgID).Scan(&projectID))
	_, err := fixture.pool.Exec(ctx, `
INSERT INTO project_members(project_id,user_id,role) VALUES ($1,$2,'project_admin')`,
		projectID, fixture.userID)
	require.NoError(t, err)
	require.NoError(t, fixture.pool.QueryRow(ctx, `
INSERT INTO access_requests(
    request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
    subject_user_id,request_kind,requested_project_role,requested_scope_mode,
    requested_models,requested_endpoints,environment,submission_mode,status,
    decision_mode,policy_reason_codes,decision_reason_codes,policy_version,
    submitted_by,decided_by,decided_at)
VALUES ('req_execution_access_000001','execution-access',repeat('a',64),$1,$2,'user',$3,
		'human_personal','project_admin','explicit',jsonb_build_array($4::text),
        '["/v1/chat/completions"]','personal','direct_grant','approved','direct',
        ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
        'unirouter.enterprise-access-policy.v1',$3,$3,NOW())
RETURNING id`, fixture.orgID, projectID, fixture.userID, fixture.model).Scan(&accessRequestID))
	require.NoError(t, fixture.pool.QueryRow(ctx, `
INSERT INTO access_grants(
    grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
    project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,environment,
    status,valid_from,decision_mode,policy_reason_codes,decision_reason_codes,
    policy_version,semantic_hash,created_by,updated_by)
VALUES ('grant_execution_access_00001',$1,$2,$3,'user',$4,'project_admin','explicit',
		jsonb_build_array($5::text),'["/v1/chat/completions"]','personal','active',NOW(),
        'direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
        'unirouter.enterprise-access-policy.v1',repeat('a',64),$4,$4)
RETURNING id`, accessRequestID, fixture.orgID, projectID, fixture.userID, fixture.model).Scan(&grantID))
	credentialRef := "cred_execution_access_0001"
	require.NoError(t, fixture.pool.QueryRow(ctx, `
INSERT INTO access_credentials(
    credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,
    allowed_models,allowed_endpoints,rpm_limit,rps_limit,created_by)
VALUES ($1,$2,$3,$4,'execution access','',$6,
		'/v1/chat/completions',60,10,$5) RETURNING id`, credentialRef, grantID,
		fixture.orgID, projectID, fixture.userID, fixture.model).Scan(&credentialID))
	require.NoError(t, fixture.pool.QueryRow(ctx, `
INSERT INTO api_keys(
    org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
    allowed_ips,allowed_models,allowed_endpoints,rpm_limit,qps_limit,purpose,
    access_grant_id,access_credential_id,credential_ref)
VALUES ($1,$2,$3,'personal','execution access',$4,$5,'active','',$6,
        '/v1/chat/completions',60,10,'',$7,$8,$9) RETURNING id`, fixture.orgID,
		projectID, fixture.userID, hashText("execution-access-key")[7:],
		"ur-exec-access", fixture.model, grantID, credentialID, credentialRef).Scan(&apiKeyID))

	requestID := "exec-" + suffix
	result, err := fixture.billing.Freeze(ctx, billing.FreezeRequest{
		RequestID: requestID, OrgID: fixture.orgID, ProjectID: projectID, APIKeyID: apiKeyID,
		AccessCredentialID: credentialID, CredentialRef: credentialRef, AccessGrantID: grantID,
		Model: fixture.model, EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 1,
		ExecutionSnapshotHash: fixture.lastPlanSnapshotHash,
	})
	require.NoError(t, err)
	require.True(t, result.Success, result.ErrorDetail)
	return AttemptIdentity{
		RequestID: requestID, AttemptID: "attempt-" + suffix,
		OrgID: fixture.orgID, APIKeyID: apiKeyID,
		RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions",
	}, credentialID, credentialRef, grantID
}

func (fixture *executionFixture) requireAttemptCount(t *testing.T, requestID string, wanted int) {
	t.Helper()
	var count int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`,
		fixture.orgID, requestID).Scan(&count))
	require.Equal(t, wanted, count)
}

type routeItemDocument struct {
	DeploymentID               int64  `json:"deployment_id"`
	ModelOperationID           string `json:"model_operation_id"`
	ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
	ExpectedOperationRevision  int64  `json:"expected_operation_revision"`
	ProtocolProfileID          string `json:"protocol_profile_id"`
	ProtocolProfileVersion     string `json:"protocol_profile_version"`
	Admission                  string `json:"admission"`
	Priority                   *int   `json:"priority,omitempty"`
	Weight                     *int   `json:"weight,omitempty"`
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}

func manifestHash(items []routeItemDocument) string {
	body, _ := json.Marshal(items)
	return hashBytes(body)
}

func hashText(value string) string { return hashBytes([]byte(value)) }

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
