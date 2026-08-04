package invocationopt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCanaryReadinessFailsClosedWithDeterministicDriverEvidence(t *testing.T) {
	policy := fixturePolicy()
	policy.Stage = StageShadow
	policy.Revision = 3
	policy.MinimumShadowSamples = 10

	result := ResolveCanaryReadiness(CanaryReadinessInput{
		Policy: policy,
		Evidence: map[string]ShadowEvidence{
			DriverResponseCache: {Samples: 9, Eligible: 1},
			DriverCompression:   {Samples: 10, Eligible: 2, Failures: 1},
		},
	})

	require.Equal(t, ReadinessContractVersion, result.ContractVersion)
	require.Equal(t, ReadinessBlocked, result.Overall)
	require.False(t, result.CanaryAdmissionReady)
	require.False(t, result.GrantsActivationAuthority)
	require.False(t, result.GrantsTrafficAuthority)
	require.Equal(t, "none", result.RuntimeMutation)
	require.Equal(t, []string{DriverResponseCache, DriverCompression}, []string{result.Drivers[0].Driver, result.Drivers[1].Driver})
	require.Equal(t, []string{
		"response_cache_runtime_unavailable",
		"shadow_samples_insufficient",
		"shadow_failures_present",
	}, []string{result.Blockers[0].Code, result.Blockers[1].Code, result.Blockers[2].Code})
}

func TestResolveCanaryReadinessAdmitsOnlyExactCurrentRevisionZeroFailureEvidence(t *testing.T) {
	policy := fixturePolicy()
	policy.Stage = StageShadow
	policy.Revision = 3
	policy.MinimumShadowSamples = 10
	evidence := map[string]ShadowEvidence{
		DriverResponseCache: {Samples: 10, Eligible: 1},
		DriverCompression:   {Samples: 12, Eligible: 2},
	}

	result := ResolveCanaryReadiness(CanaryReadinessInput{
		Policy: policy, ResponseCacheRuntimeReady: true, Evidence: evidence,
	})

	require.Equal(t, ReadinessReady, result.Overall)
	require.True(t, result.CanaryAdmissionReady)
	require.Empty(t, result.Blockers)
	for _, driver := range result.Drivers {
		require.Equal(t, DriverReadinessReady, driver.State)
		require.Empty(t, driver.Blockers)
	}
}

func TestResolveCanaryReadinessSeparatesLifecycleFromActivationAuthority(t *testing.T) {
	policy := fixturePolicy()
	policy.Stage = StageDraft
	policy.Revision = 1

	draft := ResolveCanaryReadiness(CanaryReadinessInput{Policy: policy})
	require.Equal(t, ReadinessBlocked, draft.Overall)
	require.False(t, draft.CanaryAdmissionReady)
	require.Equal(t, "policy_stage_not_shadow", draft.Blockers[len(draft.Blockers)-1].Code)
	require.Equal(t, DriverReadinessPendingShadow, draft.Drivers[0].State)

	policy.Stage = StageCanary
	policy.Revision = 4
	active := ResolveCanaryReadiness(CanaryReadinessInput{Policy: policy, ResponseCacheRuntimeReady: true})
	require.Equal(t, ReadinessNotApplicable, active.Overall)
	require.False(t, active.CanaryAdmissionReady)
	require.Equal(t, DriverReadinessCanaryActive, active.Drivers[0].State)
	require.False(t, active.GrantsActivationAuthority)
	require.False(t, active.GrantsTrafficAuthority)
}

func TestResolveCanaryReadinessDoesNotCoupleCompressionToCacheKeys(t *testing.T) {
	policy := fixturePolicy()
	policy.Stage = StageShadow
	policy.Revision = 3
	policy.MinimumShadowSamples = 10
	policy.ResponseCacheTarget = TargetDisabled
	policy.ResponseCacheCanaryBPS = 0
	policy.ResponseCachePersistence = false

	result := ResolveCanaryReadiness(CanaryReadinessInput{
		Policy: policy,
		Evidence: map[string]ShadowEvidence{
			DriverCompression: {Samples: 10, Eligible: 1},
		},
	})

	require.Equal(t, ReadinessReady, result.Overall)
	require.True(t, result.CanaryAdmissionReady)
	require.Equal(t, DriverReadinessNotTargeted, result.Drivers[0].State)
	require.Equal(t, DriverReadinessReady, result.Drivers[1].State)
}
