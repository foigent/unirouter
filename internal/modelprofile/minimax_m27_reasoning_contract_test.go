package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM27RequiredReasoningHistoryRemainsAppendOnly(t *testing.T) {
	historical, ok := Lookup("minimax.m2.7.token-plan", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v4", historical.ProtocolProfile)
	require.NotContains(t, historical.Capabilities, CapabilityReasoning)

	candidate, ok := Lookup("minimax.m2.7.token-plan", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "reasoning.minimax-m2-required-embedded.v1", candidate.ReasoningProfile)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v5", candidate.ProtocolProfile)
	require.Contains(t, candidate.Capabilities, CapabilityReasoning)
	require.Equal(t, []string{CapabilityReasoning}, candidate.AlwaysRequiredCapabilities)
	require.EqualValues(t, 204_800, candidate.ProviderMaxOutput)

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

func TestMiniMaxM27RequiredReasoningContractsFreezeExactBoundary(t *testing.T) {
	reasoning, ok := ResolveReasoningContract("reasoning.minimax-m2-required-embedded.v1")
	require.True(t, ok)
	require.Equal(t, ReasoningModeRequired, reasoning.Mode)
	require.Empty(t, reasoning.ToggleControl.Field)
	require.Equal(t, ReasoningResponseThinkingEmbedded, reasoning.ResponseThinkingTextMode)
	require.Equal(t, "reasoning_split", reasoning.SplitOutputField)
	require.False(t, reasoning.SplitOutputProviderDefault)
	require.False(t, reasoning.SplitOutputEnabledSupported)
	require.False(t, reasoning.ActiveStreamingUnsupported)
	require.Equal(t, "content", reasoning.AssistantToolHistoryReplayField)

	protocol, ok := ResolveChatProtocolContract("protocol.minimax-m2-openai-chat.v5")
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.AssistantReasoningHistoryModeled)
	require.Empty(t, protocol.AssistantReasoningHistoryFields)
	require.True(t, protocol.AssistantReasoningResponseModeled)
	require.Empty(t, protocol.AssistantReasoningResponseFields)
	require.Equal(t, "content", protocol.AssistantToolResponseReplayField)
	require.EqualValues(t, 204_800, protocol.OutputLimitMax)
}
