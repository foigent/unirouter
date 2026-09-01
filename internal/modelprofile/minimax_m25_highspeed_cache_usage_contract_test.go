package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM25HighspeedCacheReadCandidateIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("minimax.m2.5-highspeed.token-plan", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v5", historical.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-m2.v1", historical.CacheUsageProfile)
	require.NotContains(t, historical.Capabilities, CapabilityPromptCache)
	require.NotContains(t, historical.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{CapabilityReasoning}, historical.AlwaysRequiredCapabilities)

	candidate, ok := Lookup("minimax.m2.5-highspeed.token-plan", "1.4.0")
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
	require.Empty(t, currentVersion, "Token Plan is parked for new attachments")
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
}
