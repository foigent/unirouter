package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM3EmbeddedReasoningHistoryRemainsAppendOnly(t *testing.T) {
	historical, ok := Lookup("minimax.m3.token-plan", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v4", historical.ProtocolProfile)
	require.NotContains(t, historical.Capabilities, CapabilityReasoning)

	candidate, ok := Lookup("minimax.m3.token-plan", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "reasoning.minimax-m3-adaptive-embedded.v1", candidate.ReasoningProfile)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v5", candidate.ProtocolProfile)
	require.Contains(t, candidate.Capabilities, CapabilityReasoning)
	require.EqualValues(t, 524_288, candidate.ProviderMaxOutput)
	require.EqualValues(t, 131_072, candidate.DefaultOutputLimit)

	currentVersion := ""
	for _, profile := range All() {
		if profile.ID == "minimax.m3.token-plan" && profile.Current {
			currentVersion = profile.Version
		}
	}
	require.Empty(t, currentVersion, "Token Plan is parked for new attachments")
	require.Error(t, ValidateNewAttachment("minimax.m3.token-plan", "1.3.0"))
	require.Error(t, ValidateNewAttachment("minimax.m3.token-plan", "1.2.0"))
}

func TestMiniMaxM3EmbeddedReasoningContractsFreezeExactBoundary(t *testing.T) {
	reasoning, ok := ResolveReasoningContract("reasoning.minimax-m3-adaptive-embedded.v1")
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, reasoning.Mode)
	require.Equal(t, "thinking", reasoning.ToggleControl.Field)
	require.Equal(t, []string{"adaptive", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "adaptive", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, ReasoningResponseThinkingEmbedded, reasoning.ResponseThinkingTextMode)
	require.Equal(t, "reasoning_split", reasoning.SplitOutputField)
	require.False(t, reasoning.SplitOutputProviderDefault)
	require.False(t, reasoning.SplitOutputEnabledSupported)
	require.True(t, reasoning.ActiveStreamingUnsupported)
	require.Equal(t, "content", reasoning.AssistantToolHistoryReplayField)

	protocol, ok := ResolveChatProtocolContract("protocol.minimax-m3-openai-chat.v5")
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionTools)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.AssistantReasoningHistoryModeled)
	require.Empty(t, protocol.AssistantReasoningHistoryFields)
	require.True(t, protocol.AssistantReasoningResponseModeled)
	require.Empty(t, protocol.AssistantReasoningResponseFields)
	require.Equal(t, "content", protocol.AssistantToolResponseReplayField)
	require.EqualValues(t, 524_288, protocol.OutputLimitMax)
}
