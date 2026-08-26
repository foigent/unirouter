package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiK27CodeExactFunctionToolsCandidateIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("kimi.k2.7-code.api", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, "protocol.kimi-openai-chat.v4", historical.ProtocolProfile)

	candidate, ok := Lookup("kimi.k2.7-code.api", "1.3.0")
	require.True(t, ok)
	require.True(t, candidate.Current)
	require.Equal(t, "protocol.kimi-openai-chat.v5", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.kimi-k2.7-thinking-required.v2", candidate.ReasoningProfile)
	require.Equal(t, []string{EndpointClassKimiPlatformOpenAIV1}, candidate.EndpointClasses)
	require.Equal(t, []string{"kimi-k2.7-code"}, candidate.UpstreamModelIDs)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.NotContains(t, candidate.Capabilities, CapabilityToolChoiceNamed)
	require.NotContains(t, candidate.Capabilities, CapabilityToolChoiceReq)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"none", "auto"}, protocol.FunctionToolChoices)
	require.Equal(t, "auto", protocol.FunctionToolProviderDefaultChoice)
	require.True(t, protocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)

	reasoning, ok := ResolveReasoningContract(candidate.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeRequired, reasoning.Mode)
	require.Equal(t, []string{"enabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "all", reasoning.ThinkingKeepProviderDefault)
	require.True(t, reasoning.PreserveAssistantReasoningForTools)
	require.NoError(t, ValidateExecutableComponents(candidate))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.NoError(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}
