package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM3ExactFunctionToolsCandidateIsAppendOnlyAndCurrent(t *testing.T) {
	historical, ok := Lookup("minimax.m3.token-plan", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v8", historical.ProtocolProfile)
	historicalProtocol, ok := ResolveChatProtocolContract(historical.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"none", "auto"}, historicalProtocol.FunctionToolChoices)
	require.Empty(t, historicalProtocol.FunctionToolProviderDefaultChoice)
	require.False(t, historicalProtocol.FunctionToolParametersRequired)
	require.False(t, historicalProtocol.FunctionToolInvocationCapabilitiesModeled)
	require.False(t, historicalProtocol.ExactFunctionToolHistoryModeled)
	require.False(t, historicalProtocol.ExactFunctionToolResponseModeled)

	candidate, ok := Lookup("minimax.m3.token-plan", "1.7.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v9", candidate.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", candidate.CacheUsageProfile)
	require.Equal(t, ProcessingTierPolicyMiniMaxOpenAIChatV1, candidate.ProcessingTierPolicy)
	require.Contains(t, candidate.Capabilities, CapabilityTools)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, candidate.AlwaysRequiredCapabilities)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.FunctionToolsModeled)
	require.True(t, protocol.FunctionTools)
	require.Equal(t, 128, protocol.MaxFunctionTools)
	require.Empty(t, protocol.FunctionToolChoices)
	require.Equal(t, "auto", protocol.FunctionToolProviderDefaultChoice)
	require.False(t, protocol.NamedFunctionToolChoice)
	require.False(t, protocol.ParallelFunctionToolCalls)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.FunctionToolParametersRequired)
	require.True(t, protocol.FunctionToolParametersObjectWhenPresent)
	require.True(t, protocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.Equal(t, "content", protocol.AssistantToolResponseReplayField)
	require.True(t, protocol.StreamContract.RequireTerminalUsage)
	require.NoError(t, ValidateExecutableComponents(candidate))

	current, ok := Match("MiniMax-M3")
	require.True(t, ok)
	require.Equal(t, candidate.Version, current.Version)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.NoError(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}

func TestFunctionToolProviderDefaultRequiresModeledFunctionToolsAndCanonicalValue(t *testing.T) {
	for _, contract := range []ChatProtocolContract{
		{ProfileID: "invalid.unmodeled", FunctionToolProviderDefaultChoice: "auto"},
		{ProfileID: "invalid.value", FunctionToolsModeled: true, FunctionTools: true, FunctionToolProviderDefaultChoice: "named"},
	} {
		require.Error(t, validateChatProtocolContractShape(contract))
	}
}
