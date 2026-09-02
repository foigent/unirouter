package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM25HighspeedRequiredReasoningHistoryRemainsAppendOnly(t *testing.T) {
	historical, ok := Lookup("minimax.m2.5-highspeed.token-plan", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v4", historical.ProtocolProfile)
	require.NotContains(t, historical.Capabilities, CapabilityReasoning)

	candidate, ok := Lookup("minimax.m2.5-highspeed.token-plan", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "reasoning.minimax-m2-required-embedded.v1", candidate.ReasoningProfile)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v5", candidate.ProtocolProfile)
	require.Contains(t, candidate.Capabilities, CapabilityReasoning)
	require.Equal(t, []string{CapabilityReasoning}, candidate.AlwaysRequiredCapabilities)
	require.EqualValues(t, 204_800, candidate.ProviderMaxOutput)
	require.EqualValues(t, 65_536, candidate.DefaultOutputLimit)

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
