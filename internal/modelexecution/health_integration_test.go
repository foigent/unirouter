//go:build integration

package modelexecution

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func TestHealthActionsStayWithinTheirAuthorityAndFailureDomain(t *testing.T) {
	t.Run("legacy success", func(t *testing.T) {
		fixture := newExecutionFixture(t, false, false)
		fixture.setMirroredFailures(t, 2, 2)
		coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeLegacy, "health-legacy-success")
		fixture.completeAcceptedAttempt(t, decision)
		result, err := coordinator.MarkSuccess(t.Context(), decision)
		require.NoError(t, err)
		require.Equal(t, Applied, result.Status)
		legacyBreaker, canonicalBreaker := fixture.breakers(t)
		legacyCredential, canonicalCredential := fixture.credentials(t)
		require.Zero(t, legacyBreaker.failCount)
		require.Equal(t, 2, canonicalBreaker.failCount, "legacy authority must not write canonical health")
		require.Zero(t, legacyCredential.failCount)
		require.Equal(t, 2, canonicalCredential.failCount)
		require.NotNil(t, legacyCredential.lastUsedAt)
		require.Nil(t, canonicalCredential.lastUsedAt)
	})

	t.Run("canonical provider failure", func(t *testing.T) {
		fixture := newExecutionFixture(t, true, false)
		coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-canonical-provider")
		fixture.completeAcceptedAttempt(t, decision)
		result, err := coordinator.MarkProviderFailure(t.Context(), decision)
		require.NoError(t, err)
		require.Equal(t, Applied, result.Status)
		legacyBreaker, canonicalBreaker := fixture.breakers(t)
		legacyCredential, canonicalCredential := fixture.credentials(t)
		require.Zero(t, legacyBreaker.failCount)
		require.Equal(t, 1, canonicalBreaker.failCount)
		require.Zero(t, legacyCredential.failCount)
		require.Zero(t, canonicalCredential.failCount, "provider availability must not penalize credentials")
	})

	for _, test := range []struct {
		name     string
		failure  string
		action   func(*Coordinator, Decision) (HealthResult, error)
		cooldown time.Duration
	}{
		{
			name: "verify auth failure", failure: "auth", cooldown: DefaultConfig().AuthFailureCooldown,
			action: func(coordinator *Coordinator, decision Decision) (HealthResult, error) {
				return coordinator.MarkCredentialAuthFailure(t.Context(), decision)
			},
		},
		{
			name: "verify rate limit", failure: "rate_limit", cooldown: DefaultConfig().RateLimitCooldown,
			action: func(coordinator *Coordinator, decision Decision) (HealthResult, error) {
				return coordinator.MarkCredentialRateLimited(t.Context(), decision)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, false)
			coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-"+test.name)
			fixture.completeRejectedAttempt(t, decision, test.failure)
			beforeLegacyBreaker, beforeCanonicalBreaker := fixture.breakers(t)
			result, err := test.action(coordinator, decision)
			require.NoError(t, err)
			require.Equal(t, Applied, result.Status)
			legacyBreaker, canonicalBreaker := fixture.breakers(t)
			legacyCredential, canonicalCredential := fixture.credentials(t)
			require.Equal(t, beforeLegacyBreaker.failCount, legacyBreaker.failCount)
			require.Equal(t, beforeCanonicalBreaker.failCount, canonicalBreaker.failCount)
			require.Equal(t, beforeLegacyBreaker.circuitRevision, legacyBreaker.circuitRevision)
			require.Equal(t, beforeCanonicalBreaker.circuitRevision, canonicalBreaker.circuitRevision)
			require.Equal(t, legacyCredential.stateRevision, canonicalCredential.stateRevision)
			require.Equal(t, decision.Evidence.LegacyCredentialStateRevision+1, legacyCredential.stateRevision)
			require.NotNil(t, legacyCredential.cooldownUntil)
			require.NotNil(t, canonicalCredential.cooldownUntil)
			require.True(t, legacyCredential.cooldownUntil.Equal(*canonicalCredential.cooldownUntil))
			require.WithinDuration(t, result.DatabaseTime.Add(test.cooldown), *legacyCredential.cooldownUntil, time.Millisecond)
		})
	}
}

func TestVerifySuccessKeepsLegacyAndCanonicalRowsByteEquivalent(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	fixture.setMirroredFailures(t, 3, 2)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-verify-success")
	fixture.completeAcceptedAttempt(t, decision)
	result, err := coordinator.MarkSuccess(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, result.Status)

	legacyBreaker, canonicalBreaker := fixture.breakers(t)
	legacyCredential, canonicalCredential := fixture.credentials(t)
	requireBreakerEquivalent(t, legacyBreaker, canonicalBreaker)
	requireCredentialEquivalent(t, legacyCredential, canonicalCredential)
	require.Zero(t, legacyBreaker.failCount)
	require.Zero(t, legacyCredential.failCount)
	require.NotNil(t, legacyCredential.lastUsedAt)
}

func TestHalfOpenTerminalEventsRespectProbeOwnership(t *testing.T) {
	t.Run("verify success heals both projections", func(t *testing.T) {
		fixture := newExecutionFixture(t, true, true)
		coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-probe-success")
		fixture.completeAcceptedAttempt(t, decision)
		result, err := coordinator.MarkSuccess(t.Context(), decision)
		require.NoError(t, err)
		require.Equal(t, Applied, result.Status)
		legacy, canonical := fixture.breakers(t)
		requireBreakerEquivalent(t, legacy, canonical)
		require.Equal(t, "healthy", legacy.circuitState)
		require.Zero(t, legacy.failCount)
		require.Nil(t, legacy.halfOpenUntil)
		require.Empty(t, legacy.probeToken)
		require.EqualValues(t, decision.Evidence.LegacyCircuitRevision+1, legacy.circuitRevision)
	})

	t.Run("canonical provider failure reopens only the canonical breaker", func(t *testing.T) {
		fixture := newExecutionFixture(t, true, true)
		coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-probe-provider")
		fixture.completeAcceptedAttempt(t, decision)
		legacyBefore, canonicalBefore := fixture.breakers(t)
		result, err := coordinator.MarkProviderFailure(t.Context(), decision)
		require.NoError(t, err)
		require.Equal(t, Applied, result.Status)
		legacy, canonical := fixture.breakers(t)
		require.Equal(t, legacyBefore, legacy)
		require.Equal(t, "half_open", canonical.circuitState)
		require.Equal(t, canonicalBefore.failCount+1, canonical.failCount)
		require.Equal(t, canonicalBefore.circuitRevision+1, canonical.circuitRevision)
		require.Nil(t, canonical.probeUntil)
		require.Empty(t, canonical.probeToken)
		require.NotNil(t, canonical.halfOpenUntil)
		require.WithinDuration(t, result.DatabaseTime.Add(DefaultConfig().BreakerRecovery), *canonical.halfOpenUntil, time.Millisecond)
	})
}

func TestVerifySecondSideDisableRollsBackCanonicalHealth(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	fixture.setMirroredFailures(t, 2, 2)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-verify-rollback")
	fixture.completeAcceptedAttempt(t, decision)
	beforeBreaker, beforeCredential := fixture.canonicalState(t)
	_, err := fixture.pool.Exec(t.Context(), `UPDATE channels SET status='disabled' WHERE id=$1`, fixture.channelID)
	require.NoError(t, err)

	result, err := coordinator.MarkSuccess(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Disabled, result.Status)
	afterBreaker, afterCredential := fixture.canonicalState(t)
	require.Equal(t, beforeBreaker, afterBreaker, "legacy-side rejection must roll back canonical breaker writes")
	require.Equal(t, beforeCredential, afterCredential, "legacy-side rejection must roll back canonical credential writes")
	fixture.requireAttemptCount(t, decision.ProviderAttemptKey.RequestID, 1)
}

func TestVerifyMirrorsSharedLegacyBreakerToEveryNonRetiredDeployment(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	siblingID := fixture.insertSharedDeployment(t)
	_, err := fixture.pool.Exec(t.Context(), `
UPDATE channels SET fail_count=degradation_threshold-1 WHERE id=$1`, fixture.channelID)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET fail_count=degradation_threshold-1
 WHERE legacy_channel_id=$1 AND admin_state<>'retired'`, fixture.channelID)
	require.NoError(t, err)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-shared-breaker")
	fixture.completeAcceptedAttempt(t, decision)

	result, err := coordinator.MarkProviderFailure(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, result.Status)
	legacy, selected := fixture.breakers(t)
	sibling := fixture.breaker(t, siblingID, false)
	requireBreakerEquivalent(t, legacy, selected)
	requireBreakerEquivalent(t, legacy, sibling)
	require.Equal(t, "half_open", legacy.circuitState)
	require.EqualValues(t, decision.Evidence.LegacyCircuitRevision+1, legacy.circuitRevision)
}

func TestVerifyMirrorsSharedLegacyProbeLeaseLifecycle(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	siblingID := fixture.insertSharedDeployment(t)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-shared-probe")

	legacyClaimed, selectedClaimed := fixture.breakers(t)
	siblingClaimed := fixture.breaker(t, siblingID, false)
	requireBreakerEquivalent(t, legacyClaimed, selectedClaimed)
	requireBreakerEquivalent(t, legacyClaimed, siblingClaimed)
	require.Equal(t, decision.ProbeLeaseToken, legacyClaimed.probeToken)

	renewed, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, renewed.Status)
	legacyRenewed, selectedRenewed := fixture.breakers(t)
	siblingRenewed := fixture.breaker(t, siblingID, false)
	requireBreakerEquivalent(t, legacyRenewed, selectedRenewed)
	requireBreakerEquivalent(t, legacyRenewed, siblingRenewed)
	require.True(t, legacyRenewed.probeUntil.After(*legacyClaimed.probeUntil))

	released, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, released.Status)
	legacyReleased, selectedReleased := fixture.breakers(t)
	siblingReleased := fixture.breaker(t, siblingID, false)
	requireBreakerEquivalent(t, legacyReleased, selectedReleased)
	requireBreakerEquivalent(t, legacyReleased, siblingReleased)
	require.Empty(t, legacyReleased.probeToken)
	require.Nil(t, legacyReleased.probeUntil)
}

func TestVerifyCredentialFailuresReleaseEverySharedProbeLease(t *testing.T) {
	for _, test := range []struct {
		name        string
		failureKind string
		action      func(*Coordinator, Decision) (HealthResult, error)
	}{
		{
			name: "auth", failureKind: "auth",
			action: func(coordinator *Coordinator, decision Decision) (HealthResult, error) {
				return coordinator.MarkCredentialAuthFailure(t.Context(), decision)
			},
		},
		{
			name: "rate limit", failureKind: "rate_limit",
			action: func(coordinator *Coordinator, decision Decision) (HealthResult, error) {
				return coordinator.MarkCredentialRateLimited(t.Context(), decision)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, true)
			siblingID := fixture.insertSharedDeployment(t)
			coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "shared-probe-"+test.name)
			fixture.completeRejectedAttempt(t, decision, test.failureKind)
			result, err := test.action(coordinator, decision)
			require.NoError(t, err)
			require.Equal(t, Applied, result.Status)
			legacy, selected := fixture.breakers(t)
			sibling := fixture.breaker(t, siblingID, false)
			requireBreakerEquivalent(t, legacy, selected)
			requireBreakerEquivalent(t, legacy, sibling)
			require.Empty(t, legacy.probeToken)
			require.Nil(t, legacy.probeUntil)
		})
	}
}

func TestProbeLeaseRenewReleaseAndLossAreModeAware(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-probe-lifecycle")
	legacyBefore, canonicalBefore := fixture.breakers(t)
	require.NotNil(t, legacyBefore.probeUntil)
	require.NotNil(t, canonicalBefore.probeUntil)

	renewed, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, renewed.Status)
	legacyRenewed, canonicalRenewed := fixture.breakers(t)
	require.True(t, legacyRenewed.probeUntil.After(*legacyBefore.probeUntil))
	require.True(t, canonicalRenewed.probeUntil.Equal(*legacyRenewed.probeUntil))
	require.Equal(t, legacyBefore.circuitRevision, legacyRenewed.circuitRevision)
	require.Equal(t, canonicalBefore.circuitRevision, canonicalRenewed.circuitRevision)

	released, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, released.Status)
	legacyReleased, canonicalReleased := fixture.breakers(t)
	require.Nil(t, legacyReleased.probeUntil)
	require.Nil(t, canonicalReleased.probeUntil)
	require.Empty(t, legacyReleased.probeToken)
	require.Empty(t, canonicalReleased.probeToken)
	require.Equal(t, legacyBefore.circuitRevision, legacyReleased.circuitRevision)
	require.Equal(t, canonicalBefore.circuitRevision, canonicalReleased.circuitRevision)

	lost, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Stale, lost.Status, "a terminal release fence takes precedence over the former lease state")
}

func TestAcceptedResponseHeadersKeepOnlyRenewalAndTerminalHealthAvailable(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-response-accepted")
	require.NoError(t, fixture.billing.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{
		OrgID: decision.ProviderAttemptKey.OrgID, RequestID: decision.ProviderAttemptKey.RequestID,
		AttemptID: decision.ProviderAttemptKey.AttemptID,
		Outcome:   billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))

	renewed, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, renewed.Status, "an accepted streaming body still owns its live probe lease")

	released, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Stale, released.Status, "accepted headers prove the provider was already dispatched")
	_, stillClaimed := fixture.breakers(t)
	require.Equal(t, decision.ProbeLeaseToken, stillClaimed.probeToken)

	succeeded, err := coordinator.MarkSuccess(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, succeeded.Status)
	afterTerminal, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Stale, afterTerminal.Status, "terminal health prevents every later lease renewal")
}

func TestRenewLeaseRejectsTheWrongAttemptOwnerWithoutProjectionWrites(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-renew-wrong-owner")
	_, before := fixture.breakers(t)
	decision.DispatchOwnerToken = "another-valid-process-owner"

	result, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Stale, result.Status)
	_, after := fixture.breakers(t)
	require.Equal(t, before, after)
}

func TestVerifyNoopRenewStillRejectsADisabledLegacySide(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	_, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "health-probe-noop")
	config := DefaultConfig()
	config.HalfOpenLease = time.Second
	coordinator, err := New(fixture.pool, fixture.billing, config)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), `UPDATE channels SET status='disabled' WHERE id=$1`, fixture.channelID)
	require.NoError(t, err)
	_, canonicalBefore := fixture.breakers(t)

	result, err := coordinator.RenewLease(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Disabled, result.Status)
	_, canonicalAfter := fixture.breakers(t)
	require.Equal(t, canonicalBefore, canonicalAfter)
}

func TestLateHealthResultCannotHealANewerRevisionOrDisabledDeployment(t *testing.T) {
	t.Run("new circuit revision", func(t *testing.T) {
		fixture := newExecutionFixture(t, true, false)
		coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-late-revision")
		fixture.completeAcceptedAttempt(t, decision)
		_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments
   SET fail_count=7,circuit_state='half_open',half_open_until=NOW()+INTERVAL '5 minutes',
       circuit_revision=circuit_revision+1
 WHERE id=$1`, fixture.deploymentID)
		require.NoError(t, err)
		result, err := coordinator.MarkSuccess(t.Context(), decision)
		require.NoError(t, err)
		require.Equal(t, Stale, result.Status)
		current := fixture.breaker(t, fixture.deploymentID, false)
		require.Equal(t, "half_open", current.circuitState)
		require.Equal(t, 7, current.failCount)
	})

	t.Run("administrative disable", func(t *testing.T) {
		fixture := newExecutionFixture(t, true, false)
		coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-late-disable")
		fixture.completeAcceptedAttempt(t, decision)
		_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET admin_state='disabled',config_revision=config_revision+1 WHERE id=$1`, fixture.deploymentID)
		require.NoError(t, err)
		result, err := coordinator.MarkProviderFailure(t.Context(), decision)
		require.NoError(t, err)
		require.Equal(t, Disabled, result.Status)
		current := fixture.breaker(t, fixture.deploymentID, false)
		require.Zero(t, current.failCount)
	})
}

func TestTerminalHealthOutcomeIsAttemptIdempotentAndConflictingOutcomesAreStale(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-terminal-replay")
	fixture.completeAcceptedAttempt(t, decision)

	first, err := coordinator.MarkProviderFailure(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, first.Status)
	_, afterFirst := fixture.breakers(t)
	require.Equal(t, 1, afterFirst.failCount)

	replayed, err := coordinator.MarkProviderFailure(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, replayed.Status)
	_, afterReplay := fixture.breakers(t)
	require.Equal(t, afterFirst, afterReplay, "exact terminal replay must not apply breaker health twice")

	conflict, err := coordinator.MarkSuccess(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Stale, conflict.Status)
	_, afterConflict := fixture.breakers(t)
	require.Equal(t, afterFirst, afterConflict)

	var outcome string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_outcome FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		decision.ProviderAttemptKey.OrgID, decision.ProviderAttemptKey.RequestID,
		decision.ProviderAttemptKey.AttemptID).Scan(&outcome))
	require.Equal(t, "provider_failure", outcome)
}

func TestTerminalHealthRejectsTheWrongAttemptOwnerWithoutProjectionWrites(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-wrong-owner")
	fixture.completeAcceptedAttempt(t, decision)
	decision.DispatchOwnerToken = "another-valid-process-owner"

	result, err := coordinator.MarkProviderFailure(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Stale, result.Status)
	_, breaker := fixture.breakers(t)
	require.Zero(t, breaker.failCount)
	var outcome *string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_outcome FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		decision.ProviderAttemptKey.OrgID, decision.ProviderAttemptKey.RequestID,
		decision.ProviderAttemptKey.AttemptID).Scan(&outcome))
	require.Nil(t, outcome)
}

func TestReleaseUndispatchedReplayDoesNotRequireASecondLiveLease(t *testing.T) {
	fixture := newExecutionFixture(t, true, true)
	coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "health-release-replay")
	first, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, first.Status)
	replayed, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, replayed.Status)
	var state, certainty string
	var financiallyResolved bool
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT state,outcome_certainty,financial_resolved_at IS NOT NULL
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		decision.ProviderAttemptKey.OrgID, decision.ProviderAttemptKey.RequestID,
		decision.ProviderAttemptKey.AttemptID).Scan(&state, &certainty, &financiallyResolved))
	require.Equal(t, billing.ProviderAttemptStateReleasedUndispatched, state)
	require.Equal(t, "not_dispatched", certainty)
	require.True(t, financiallyResolved)
	_, breaker := fixture.breakers(t)
	require.Empty(t, breaker.probeToken)
	require.Nil(t, breaker.probeUntil)
}

func TestHealthyUndispatchedReleaseTerminatesReceiptAndAllowsCancelAndNextAttempt(t *testing.T) {
	fixture := newExecutionFixture(t, true, false)
	plan, route := fixture.plan(t, modelrouting.ModeCanonical, "healthy-release")
	identity := fixture.freeze(t, "healthy-release")
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)
	_, breakerBefore := fixture.breakers(t)

	wrongOwner := decision
	wrongOwner.DispatchOwnerToken = "wrong-but-bounded-dispatch-owner"
	wrong, err := coordinator.ReleaseUndispatched(t.Context(), wrongOwner)
	require.NoError(t, err)
	require.Equal(t, Stale, wrong.Status)

	released, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, released.Status)
	replayed, err := coordinator.ReleaseUndispatched(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, replayed.Status)
	_, breakerAfter := fixture.breakers(t)
	require.Equal(t, breakerBefore, breakerAfter,
		"a healthy release has no probe projection to mutate")
	var state, certainty string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT state,outcome_certainty FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
		identity.RequestID, identity.AttemptID).Scan(&state, &certainty))
	require.Equal(t, billing.ProviderAttemptStateReleasedUndispatched, state)
	require.Equal(t, "not_dispatched", certainty)

	secondIdentity := identity
	secondIdentity.AttemptID = identity.AttemptID + "-next"
	second, err := coordinator.Authorize(t.Context(), plan, route, secondIdentity)
	require.NoError(t, err)
	require.Equal(t, Applied, second.Status,
		"a released receipt must not remain in the unresolved-attempt set")
	secondRelease, err := coordinator.ReleaseUndispatched(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, Applied, secondRelease.Status)
	require.NoError(t, fixture.billing.Cancel(t.Context(), identity.RequestID, fixture.orgID))
	var reservationState string
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT status FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`,
		fixture.orgID, identity.RequestID).Scan(&reservationState))
	require.Equal(t, "cancelled", reservationState)
}

func TestHalfOpenReleaseTerminalizesReceiptAcrossRouteDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *executionFixture)
	}{
		{
			name: "administrative disable",
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET admin_state='disabled' WHERE id=$1`, fixture.deploymentID)
				require.NoError(t, err)
			},
		},
		{
			name: "connection administrative disable",
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, fixture.connectionID)
				require.NoError(t, err)
			},
		},
		{
			name: "revision advance",
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET circuit_revision=circuit_revision+1 WHERE id=$1`, fixture.deploymentID)
				require.NoError(t, err)
			},
		},
		{
			name: "lease expiry",
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET half_open_probe_until=clock_timestamp()-INTERVAL '1 second'
 WHERE id=$1`, fixture.deploymentID)
				require.NoError(t, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, true)
			coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeCanonical, "release-drift-"+test.name)
			test.mutate(t, fixture)
			before := fixture.breaker(t, fixture.deploymentID, false)

			result, err := coordinator.ReleaseUndispatched(t.Context(), decision)
			require.NoError(t, err)
			require.Equal(t, Applied, result.Status)
			after := fixture.breaker(t, fixture.deploymentID, false)
			require.Equal(t, before, after,
				"release cleanup must not overwrite a newer or administratively disabled route")
			var state string
			require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT state FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
				decision.ProviderAttemptKey.RequestID, decision.ProviderAttemptKey.AttemptID).Scan(&state))
			require.Equal(t, billing.ProviderAttemptStateReleasedUndispatched, state)
			require.NoError(t, fixture.billing.Cancel(t.Context(), decision.ProviderAttemptKey.RequestID, fixture.orgID))
		})
	}
}

func TestVerifyReleaseTreatsAnyProjectionDriftAsAGroupNoop(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *executionFixture, int64)
	}{
		{
			name: "canonical deployment disabled",
			mutate: func(t *testing.T, fixture *executionFixture, _ int64) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET admin_state='disabled' WHERE id=$1`, fixture.deploymentID)
				require.NoError(t, err)
			},
		},
		{
			name: "legacy channel disabled",
			mutate: func(t *testing.T, fixture *executionFixture, _ int64) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE channels SET status='disabled' WHERE id=$1`, fixture.channelID)
				require.NoError(t, err)
			},
		},
		{
			name: "shared sibling revision advanced",
			mutate: func(t *testing.T, fixture *executionFixture, siblingID int64) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments SET circuit_revision=circuit_revision+1 WHERE id=$1`, siblingID)
				require.NoError(t, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, true, true)
			siblingID := fixture.insertSharedDeployment(t)
			coordinator, decision := fixture.authorizedDecision(t, modelrouting.ModeVerify, "verify-release-drift-"+test.name)
			test.mutate(t, fixture, siblingID)
			legacyBefore := fixture.breaker(t, fixture.channelID, true)
			selectedBefore := fixture.breaker(t, fixture.deploymentID, false)
			siblingBefore := fixture.breaker(t, siblingID, false)

			result, err := coordinator.ReleaseUndispatched(t.Context(), decision)
			require.NoError(t, err)
			require.Equal(t, Applied, result.Status)
			require.Equal(t, legacyBefore, fixture.breaker(t, fixture.channelID, true))
			require.Equal(t, selectedBefore, fixture.breaker(t, fixture.deploymentID, false))
			require.Equal(t, siblingBefore, fixture.breaker(t, siblingID, false))
			var state string
			require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT state FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
				decision.ProviderAttemptKey.RequestID, decision.ProviderAttemptKey.AttemptID).Scan(&state))
			require.Equal(t, billing.ProviderAttemptStateReleasedUndispatched, state)
		})
	}
}

func TestTerminalHealthHonorsAdministrativeDisableAcrossEveryAuthorityRow(t *testing.T) {
	tests := []struct {
		name   string
		mode   modelrouting.Mode
		mutate func(*testing.T, *executionFixture)
	}{
		{
			name: "canonical connection", mode: modelrouting.ModeCanonical,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE provider_connections
   SET admin_state='disabled',config_revision=config_revision+1
 WHERE id=$1`, fixture.connectionID)
				require.NoError(t, err)
			},
		},
		{
			name: "canonical retired deployment", mode: modelrouting.ModeCanonical,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployments
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1
 WHERE id=$1`, fixture.deploymentID)
				require.NoError(t, err)
			},
		},
		{
			name: "canonical credential", mode: modelrouting.ModeCanonical,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1
 WHERE id=$1`, fixture.bindingID)
				require.NoError(t, err)
			},
		},
		{
			name: "legacy credential", mode: modelrouting.ModeLegacy,
			mutate: func(t *testing.T, fixture *executionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE channel_keys SET status='disabled',state_epoch=state_epoch+1 WHERE id=$1`, fixture.legacyKeyID)
				require.NoError(t, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionFixture(t, test.mode != modelrouting.ModeLegacy, false)
			coordinator, decision := fixture.authorizedDecision(t, test.mode, "health-disable-"+test.name)
			fixture.completeAcceptedAttempt(t, decision)
			test.mutate(t, fixture)
			result, err := coordinator.MarkSuccess(t.Context(), decision)
			require.NoError(t, err)
			require.Equal(t, Disabled, result.Status)
			var outcome *string
			require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_outcome FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
				decision.ProviderAttemptKey.OrgID, decision.ProviderAttemptKey.RequestID,
				decision.ProviderAttemptKey.AttemptID).Scan(&outcome))
			require.Nil(t, outcome, "a rejected projection transition must roll back its health fence")
		})
	}
}

func (fixture *executionFixture) authorizedDecision(t *testing.T, mode modelrouting.Mode, suffix string) (*Coordinator, Decision) {
	t.Helper()
	plan, route := fixture.plan(t, mode, suffix)
	identity := fixture.freeze(t, suffix)
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	decision, err := coordinator.Authorize(t.Context(), plan, route, identity)
	require.NoError(t, err)
	require.Equal(t, Applied, decision.Status)
	return coordinator, decision
}

func (fixture *executionFixture) completeAcceptedAttempt(t *testing.T, decision Decision) {
	t.Helper()
	require.NoError(t, fixture.billing.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{
		OrgID: decision.ProviderAttemptKey.OrgID, RequestID: decision.ProviderAttemptKey.RequestID,
		AttemptID: decision.ProviderAttemptKey.AttemptID,
		Outcome:   billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
}

func (fixture *executionFixture) completeRejectedAttempt(t *testing.T, decision Decision, failureKind string) {
	t.Helper()
	status := 503
	if failureKind == "auth" {
		status = 401
	} else if failureKind == "rate_limit" {
		status = 429
	}
	require.NoError(t, fixture.billing.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{
		OrgID: decision.ProviderAttemptKey.OrgID, RequestID: decision.ProviderAttemptKey.RequestID,
		AttemptID: decision.ProviderAttemptKey.AttemptID,
		Outcome:   billing.ProviderAttemptOutcomeRejected, HTTPStatus: status, FailureKind: failureKind,
	}))
}

func (fixture *executionFixture) setMirroredFailures(t *testing.T, breakerFailures, credentialFailures int) {
	t.Helper()
	_, err := fixture.pool.Exec(t.Context(), `UPDATE channels SET fail_count=$2 WHERE id=$1`, fixture.channelID, breakerFailures)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), `UPDATE model_deployments SET fail_count=$2 WHERE id=$1`, fixture.deploymentID, breakerFailures)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), `UPDATE channel_keys SET fail_count=$2 WHERE id=$1`, fixture.legacyKeyID, credentialFailures)
	require.NoError(t, err)
	_, err = fixture.pool.Exec(t.Context(), `UPDATE provider_credential_bindings SET fail_count=$2 WHERE id=$1`, fixture.bindingID, credentialFailures)
	require.NoError(t, err)
}

func (fixture *executionFixture) insertSharedDeployment(t *testing.T) int64 {
	t.Helper()
	var publicModelID, deploymentID int64
	siblingModel := fixture.model + "-sibling"
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
INSERT INTO model_registry(
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit)
VALUES ($1,$2,$2,'custom','ga',TRUE,128000,8192) RETURNING id`, fixture.orgID, siblingModel).Scan(&publicModelID))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
INSERT INTO model_deployments(
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,
    degradation_threshold,circuit_state,circuit_revision,fail_count,half_open_until,
    legacy_channel_id,legacy_public_model)
SELECT $1,$2,$3,'upstream-sibling','enabled',source.config_revision,
       10,100,128000,8192,source.degradation_threshold,source.circuit_state,
       source.circuit_revision,source.fail_count,source.half_open_until,$4,$5
  FROM model_deployments AS source
 WHERE source.id=$6
RETURNING id`, fixture.orgID, publicModelID, fixture.connectionID, fixture.channelID,
		siblingModel, fixture.deploymentID).Scan(&deploymentID))
	return deploymentID
}

func (fixture *executionFixture) breakers(t *testing.T) (breakerSnapshot, breakerSnapshot) {
	t.Helper()
	return fixture.breaker(t, fixture.channelID, true), fixture.breaker(t, fixture.deploymentID, false)
}

func (fixture *executionFixture) breaker(t *testing.T, id int64, legacy bool) breakerSnapshot {
	t.Helper()
	var value breakerSnapshot
	if legacy {
		require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT id,status,routing_epoch,circuit_state,circuit_epoch,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM channels WHERE id=$1`, id).Scan(
			&value.id, &value.adminState, &value.configRevision, &value.circuitState,
			&value.circuitRevision, &value.failCount, &value.degradationThreshold,
			&value.halfOpenUntil, &value.probeUntil, &value.probeToken,
		))
		return value
	}
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT id,connection_id,public_model_id,legacy_channel_id,admin_state,config_revision,
       circuit_state,circuit_revision,fail_count,degradation_threshold,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,'')
  FROM model_deployments WHERE id=$1`, id).Scan(
		&value.id, &value.connectionID, &value.publicModelID, &value.legacyChannelID,
		&value.adminState, &value.configRevision, &value.circuitState, &value.circuitRevision,
		&value.failCount, &value.degradationThreshold, &value.halfOpenUntil, &value.probeUntil, &value.probeToken,
	))
	return value
}

func (fixture *executionFixture) credentials(t *testing.T) (credentialSnapshot, credentialSnapshot) {
	t.Helper()
	var legacy, canonical credentialSnapshot
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT id,channel_id,status,state_epoch,fail_count,last_used_at,cooldown_until
  FROM channel_keys WHERE id=$1`, fixture.legacyKeyID).Scan(
		&legacy.id, &legacy.connectionID, &legacy.status, &legacy.stateRevision,
		&legacy.failCount, &legacy.lastUsedAt, &legacy.cooldownUntil,
	))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT id,connection_id,legacy_channel_key_id,status,state_revision,fail_count,last_used_at,cooldown_until
  FROM provider_credential_bindings WHERE id=$1`, fixture.bindingID).Scan(
		&canonical.id, &canonical.connectionID, &canonical.legacyKeyID, &canonical.status,
		&canonical.stateRevision, &canonical.failCount, &canonical.lastUsedAt, &canonical.cooldownUntil,
	))
	return legacy, canonical
}

func (fixture *executionFixture) canonicalState(t *testing.T) (breakerSnapshot, credentialSnapshot) {
	t.Helper()
	_, breaker := fixture.breakers(t)
	_, credential := fixture.credentials(t)
	return breaker, credential
}

func requireBreakerEquivalent(t *testing.T, legacy, canonical breakerSnapshot) {
	t.Helper()
	require.Equal(t, legacy.configRevision, canonical.configRevision)
	require.Equal(t, legacy.circuitState, canonical.circuitState)
	require.Equal(t, legacy.circuitRevision, canonical.circuitRevision)
	require.Equal(t, legacy.failCount, canonical.failCount)
	require.Equal(t, legacy.degradationThreshold, canonical.degradationThreshold)
	require.True(t, sameOptionalTime(legacy.halfOpenUntil, canonical.halfOpenUntil))
	require.True(t, sameOptionalTime(legacy.probeUntil, canonical.probeUntil))
	require.Equal(t, legacy.probeToken, canonical.probeToken)
}

func requireCredentialEquivalent(t *testing.T, legacy, canonical credentialSnapshot) {
	t.Helper()
	require.Equal(t, legacy.stateRevision, canonical.stateRevision)
	require.Equal(t, legacy.failCount, canonical.failCount)
	require.True(t, sameOptionalTime(legacy.lastUsedAt, canonical.lastUsedAt))
	require.True(t, sameOptionalTime(legacy.cooldownUntil, canonical.cooldownUntil))
}
