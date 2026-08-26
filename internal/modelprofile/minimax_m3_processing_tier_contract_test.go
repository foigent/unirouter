package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM3ProcessingTierCandidateRemainsAppendOnlyAndHistorical(t *testing.T) {
	historical, ok := Lookup("minimax.m3.token-plan", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v6", historical.ProtocolProfile)
	require.Empty(t, historical.ProcessingTierPolicy)
	require.NotContains(t, historical.Capabilities, CapabilityServiceTier)

	candidate, ok := Lookup("minimax.m3.token-plan", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v7", candidate.ProtocolProfile)
	require.Equal(t, ProcessingTierPolicyMiniMaxOpenAIChatV1, candidate.ProcessingTierPolicy)
	require.Contains(t, candidate.Capabilities, CapabilityServiceTier)
	require.Contains(t, candidate.ProviderCapabilities, CapabilityServiceTier)
	require.Empty(t, candidate.CacheUsageProfile)
	require.Contains(t, candidate.Capabilities, CapabilityVision)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, candidate.ProcessingTierPolicy, protocol.ProcessingTierPolicyID)
	policy, ok := ResolveProcessingTierPolicy(protocol.ProcessingTierPolicyID)
	require.True(t, ok)
	require.NoError(t, validateProcessingTierPolicyShape(policy))
	require.Equal(t, "minimax", policy.Provider)
	require.Equal(t, APISurfaceOpenAIChat, policy.APISurface)
	require.Equal(t, ProcessingTierStandard, policy.OmittedNormalizedTier)
	require.Equal(t, 300, policy.OmittedTimeoutSeconds)
	require.Equal(t, []ProcessingTierRequestValue{
		{WireValue: "standard", NormalizedTier: ProcessingTierStandard, RequestTimeoutSeconds: 300},
		{WireValue: "priority", NormalizedTier: ProcessingTierPriority, RequestTimeoutSeconds: 300},
	}, policy.RequestValues)
	require.Empty(t, policy.ObservedResponseHeader)
	require.Empty(t, policy.ObservedNormalizedValues)

	currentVersion := ""
	for _, profile := range All() {
		if profile.ID == candidate.ID && profile.Current {
			currentVersion = profile.Version
		}
	}
	require.Equal(t, "1.7.0", currentVersion)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}

func TestProcessingTierPolicyObservationAuthorityMustBeComplete(t *testing.T) {
	policy, ok := ResolveProcessingTierPolicy(ProcessingTierPolicyMiniMaxOpenAIChatV1)
	require.True(t, ok)
	policy.ObservedNormalizedValues = []string{ProcessingTierStandard}
	require.ErrorContains(t, validateProcessingTierPolicyShape(policy), "must be modeled together")

	policy, ok = ResolveProcessingTierPolicy(ProcessingTierPolicyGeminiOpenAICompatibilityV1)
	require.True(t, ok)
	policy.ObservedResponseHeader = ""
	require.ErrorContains(t, validateProcessingTierPolicyShape(policy), "must be modeled together")
}
