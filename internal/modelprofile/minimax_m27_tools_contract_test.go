package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM27ExactFunctionToolsCandidateRemainsAddressableButParked(t *testing.T) {
	historical, ok := Lookup("minimax.m2.7.token-plan", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v6", historical.ProtocolProfile)
	historicalProtocol, ok := ResolveChatProtocolContract(historical.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"none", "auto"}, historicalProtocol.FunctionToolChoices)
	require.Empty(t, historicalProtocol.FunctionToolProviderDefaultChoice)
	require.False(t, historicalProtocol.ExactFunctionToolHistoryModeled)
	require.False(t, historicalProtocol.ExactFunctionToolResponseModeled)

	candidate, ok := Lookup("minimax.m2.7.token-plan", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v7", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.minimax-m2-required-embedded.v1", candidate.ReasoningProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", candidate.CacheUsageProfile)
	require.Contains(t, candidate.Capabilities, CapabilityTools)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityPromptCache, CapabilityCacheReadUsage,
	}, candidate.AlwaysRequiredCapabilities)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.Empty(t, protocol.FunctionToolChoices)
	require.Equal(t, "auto", protocol.FunctionToolProviderDefaultChoice)
	require.False(t, protocol.NamedFunctionToolChoice)
	require.False(t, protocol.ParallelFunctionToolCalls)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.FunctionToolParametersRequired)
	require.True(t, protocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.Equal(t, "content", protocol.AssistantToolResponseReplayField)
	require.NoError(t, ValidateExecutableComponents(candidate))

	current, ok := Match("MiniMax-M2.7")
	require.True(t, ok)
	require.Equal(t, "minimax.m2.7.payg", current.ID)
	require.Equal(t, "1.3.0", current.Version)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}
