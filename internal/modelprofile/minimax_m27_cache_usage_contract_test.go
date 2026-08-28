package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM27CacheReadCandidateIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("minimax.m2.7.token-plan", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v5", historical.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-m2.v1", historical.CacheUsageProfile)
	require.NotContains(t, historical.Capabilities, CapabilityPromptCache)
	require.NotContains(t, historical.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{CapabilityReasoning}, historical.AlwaysRequiredCapabilities)

	candidate, ok := Lookup("minimax.m2.7.token-plan", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v6", candidate.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", candidate.CacheUsageProfile)
	require.Contains(t, candidate.Capabilities, CapabilityPromptCache)
	require.Contains(t, candidate.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityPromptCache, CapabilityCacheReadUsage,
	}, candidate.AlwaysRequiredCapabilities)

	currentVersion := ""
	for _, profile := range All() {
		if profile.ID == candidate.ID && profile.Current {
			currentVersion = profile.Version
		}
	}
	require.Equal(t, "1.5.0", currentVersion)
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
}

func TestMiniMaxM27CacheReadCandidateFreezesExactContracts(t *testing.T) {
	cache, ok := ResolveCacheUsageContract("cache-usage.minimax-openai.v1")
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, cache.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.False(t, cache.ReportsCreation)
	require.True(t, cache.RequireReadCounter)

	protocol, ok := ResolveChatProtocolContract("protocol.minimax-m2-openai-chat.v6")
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.EqualValues(t, 204_800, protocol.OutputLimitMax)
	require.True(t, protocol.StreamContract.RequestOptionsModeled)
	require.Equal(t, []string{"include_usage"}, protocol.StreamContract.AllowedRequestOptions)
	require.True(t, protocol.StreamContract.RequireIncludeUsage)
	require.True(t, protocol.StreamContract.ResponseModeled)
	require.False(t, protocol.StreamContract.RequireDone)
	require.True(t, protocol.StreamContract.RequireTerminalUsage)
	require.True(t, protocol.StreamContract.AllowTerminalUsageChoices)
	require.Equal(t, []string{"content"}, protocol.StreamContract.DeltaTextFields)
	require.Empty(t, protocol.StreamContract.DeltaOpaqueFields)
	require.Equal(t, []string{"stop", "length"}, protocol.StreamContract.FinishReasons)
}
