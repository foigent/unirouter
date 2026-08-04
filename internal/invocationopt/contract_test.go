package invocationopt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func fixturePolicy() Policy {
	draft := DefaultDraft()
	draft.OrgID = 7
	draft.ProjectID = 3
	draft.PolicyRef = "opt_enterprise_default"
	draft.DisplayName = "Enterprise default"
	draft.ResponseCacheTarget = TargetCanary
	draft.ResponseCacheCanaryBPS = 500
	draft.ResponseCachePersistence = true
	draft.CompressionTarget = TargetCanary
	draft.CompressionCanaryBPS = 250
	return Policy{
		ID: 1, ContractVersion: PolicyContractVersion, Draft: draft,
		Stage: StageCanary, Revision: 4, CreatedBy: 11, UpdatedBy: 11,
	}
}

func TestPolicyValidationAndEffectiveModes(t *testing.T) {
	policy := fixturePolicy()
	require.NoError(t, policy.Validate())
	require.Equal(t, EffectiveCanary, policy.EffectiveMode(DriverResponseCache))
	require.Equal(t, EffectiveCanary, policy.EffectiveMode(DriverCompression))
	policy.Stage = StageShadow
	require.Equal(t, EffectiveShadow, policy.EffectiveMode(DriverResponseCache))
	policy.Stage = StageRolledBack
	require.Equal(t, EffectiveDisabled, policy.EffectiveMode(DriverCompression))
}

func TestCanaryRequiresPersistenceAndRequestOptIn(t *testing.T) {
	policy := fixturePolicy()
	policy.ResponseCachePersistence = false
	require.ErrorIs(t, policy.Validate(), ErrInvalid)
	policy = fixturePolicy()
	policy.CompressionRequireOptIn = false
	require.ErrorIs(t, policy.Validate(), ErrInvalid)
}

func TestAssignmentIsDeterministicAndContentFree(t *testing.T) {
	policy := fixturePolicy()
	operationID := "op_0123456789abcdef0123456789abcdef"
	first := Assigned(policy, DriverResponseCache, 99, operationID)
	for index := 0; index < 100; index++ {
		require.Equal(t, first, Assigned(policy, DriverResponseCache, 99, operationID))
	}
	require.Equal(t, AssignmentDigest(policy, DriverCompression, 99, operationID), AssignmentDigest(policy, DriverCompression, 99, operationID))
	require.NotEqual(t, AssignmentDigest(policy, DriverCompression, 99, operationID), AssignmentDigest(policy, DriverCompression, 99, operationID+"0"))
}

func TestNormalizeDraftSuppliesConservativeDefaults(t *testing.T) {
	draft, err := NormalizeDraft(Draft{OrgID: 1, PolicyRef: "opt_default", DisplayName: "Default"})
	require.NoError(t, err)
	require.Equal(t, "*", draft.ModelSelector)
	require.Equal(t, TargetDisabled, draft.ResponseCacheTarget)
	require.Equal(t, TargetDisabled, draft.CompressionTarget)
	require.True(t, draft.ResponseCacheRequireOptIn)
	require.True(t, draft.CompressionRequireOptIn)
}
