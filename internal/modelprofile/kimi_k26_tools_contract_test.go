package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiK26ExactFunctionToolsCandidateIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("kimi.k2.6.api", "1.6.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, "protocol.kimi-openai-chat.v4", historical.ProtocolProfile)
	historicalProtocol, ok := ResolveChatProtocolContract(historical.ProtocolProfile)
	require.True(t, ok)
	require.False(t, historicalProtocol.FunctionToolInvocationCapabilitiesModeled)
	require.False(t, historicalProtocol.ExactFunctionToolHistoryModeled)
	require.False(t, historicalProtocol.ExactFunctionToolResponseModeled)

	candidate, ok := Lookup("kimi.k2.6.api", "1.7.0")
	require.True(t, ok)
	require.True(t, candidate.Current)
	require.Equal(t, "protocol.kimi-openai-chat.v5", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.kimi-k2.6-thinking-toggle.v2", candidate.ReasoningProfile)
	require.Equal(t, []string{EndpointClassKimiPlatformOpenAIV1}, candidate.EndpointClasses)
	require.Equal(t, []string{"kimi-k2.6"}, candidate.UpstreamModelIDs)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.NotContains(t, candidate.Capabilities, CapabilityToolChoiceNamed)
	require.NotContains(t, candidate.Capabilities, CapabilityToolChoiceReq)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"none", "auto"}, protocol.FunctionToolChoices)
	require.Equal(t, "auto", protocol.FunctionToolProviderDefaultChoice)
	require.Equal(t, []string{"auto"}, protocol.FunctionToolChoicesRequiringTools)
	require.False(t, protocol.NamedFunctionToolChoice)
	require.False(t, protocol.ParallelFunctionToolCalls)
	require.True(t, protocol.FunctionToolsRequireNonStream, "tool streaming requires a separate exact delta contract")
	require.True(t, protocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.NoError(t, ValidateExecutableComponents(candidate))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.NoError(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}
