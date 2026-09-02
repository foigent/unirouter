package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM3CacheReadProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	historical, ok := Lookup("minimax.m3.token-plan", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v7", historical.ProtocolProfile)
	require.Empty(t, historical.CacheUsageProfile)
	require.NotContains(t, historical.Capabilities, CapabilityPromptCache)
	require.NotContains(t, historical.Capabilities, CapabilityCacheReadUsage)
	require.Empty(t, historical.AlwaysRequiredCapabilities)

	candidate, ok := Lookup("minimax.m3.token-plan", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v8", candidate.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", candidate.CacheUsageProfile)
	require.Equal(t, ProcessingTierPolicyMiniMaxOpenAIChatV1, candidate.ProcessingTierPolicy)
	require.Contains(t, candidate.Capabilities, CapabilityVision)
	require.Contains(t, candidate.Capabilities, CapabilityServiceTier)
	require.Contains(t, candidate.Capabilities, CapabilityPromptCache)
	require.Contains(t, candidate.Capabilities, CapabilityCacheReadUsage)
	require.NotContains(t, candidate.Capabilities, CapabilityVideo)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, candidate.AlwaysRequiredCapabilities)

	cache, ok := ResolveCacheUsageContract(candidate.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, cache.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.False(t, cache.ReportsCreation)
	require.True(t, cache.RequireReadCounter)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, candidate.ProcessingTierPolicy, protocol.ProcessingTierPolicyID)
	require.True(t, protocol.ImageInputPolicy.Modeled)
	require.True(t, protocol.StreamContract.RequireIncludeUsage)
	require.True(t, protocol.StreamContract.RequireTerminalUsage)
	require.True(t, protocol.StreamContract.AllowTerminalUsageChoices)
	require.Equal(t, []string{"content"}, protocol.StreamContract.DeltaTextFields)
	require.Equal(t, []string{"stop", "length"}, protocol.StreamContract.FinishReasons)

	current, ok := Match("MiniMax-M3")
	require.True(t, ok)
	require.Equal(t, "minimax.m3.payg", current.ID)
	require.Equal(t, "1.1.0", current.Version)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}
