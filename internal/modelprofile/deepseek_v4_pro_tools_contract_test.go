package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekV4ProExactFunctionToolsVersionRemainsImmutable(t *testing.T) {
	historical, ok := Lookup("deepseek.v4-pro.api", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, "protocol.deepseek-openai-chat.v2", historical.ProtocolProfile)
	require.Equal(t, "reasoning.deepseek-thinking-toggle.v2", historical.ReasoningProfile)

	candidate, ok := Lookup("deepseek.v4-pro.api", "1.3.0")
	require.True(t, ok)
	require.False(t, candidate.Current)
	require.Equal(t, "protocol.deepseek-openai-chat.v3", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.deepseek-v4-pro-thinking-toggle.v3", candidate.ReasoningProfile)
	require.Equal(t, []string{EndpointClassDeepSeekOfficialOpenAI}, candidate.EndpointClasses)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceNamed)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceReq)
	require.NotContains(t, candidate.Capabilities, CapabilityParallelTools)

	contract, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, contract.ExactFunctionToolResponseModeled)
	require.False(t, contract.ExactFunctionToolHistoryModeled)

	reasoning, ok := ResolveReasoningContract(candidate.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, []string{"low", "high", "max"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, map[string]string{"medium": "high", "xhigh": "high"}, reasoning.EffortControl.CompatibilityValues)
	require.Equal(t, []string{"none", "auto", "required", "named"}, reasoning.AllowedToolChoices)
	require.Empty(t, reasoning.UnsupportedRequestFieldsWhileActive)
	require.NoError(t, ValidateExecutableComponents(candidate))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}
