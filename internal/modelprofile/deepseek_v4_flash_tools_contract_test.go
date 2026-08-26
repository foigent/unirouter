package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekV4FlashExactFunctionToolsVersionRemainsImmutable(t *testing.T) {
	historical, ok := Lookup("deepseek.v4-flash.api", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, "protocol.deepseek-openai-chat.v2", historical.ProtocolProfile)
	historicalContract, ok := ResolveChatProtocolContract(historical.ProtocolProfile)
	require.True(t, ok)
	require.False(t, historicalContract.FunctionToolInvocationCapabilitiesModeled)
	require.False(t, historicalContract.ExactFunctionToolResponseModeled)

	candidate, ok := Lookup("deepseek.v4-flash.api", "1.3.0")
	require.True(t, ok)
	require.False(t, candidate.Current)
	require.Equal(t, "protocol.deepseek-openai-chat.v3", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.deepseek-v4-flash-thinking-toggle.v3", candidate.ReasoningProfile)
	require.Equal(t, []string{EndpointClassDeepSeekOfficialOpenAI}, candidate.EndpointClasses)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceNamed)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceReq)

	contract, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.FunctionTools)
	require.Equal(t, 128, contract.MaxFunctionTools)
	require.False(t, contract.FunctionToolStrictSupported, "standard DeepSeek origin must not claim beta strict tools")
	require.False(t, contract.ParallelFunctionToolCalls, "parallel_tool_calls has no current standard-surface contract")
	require.Equal(t, []string{"none", "auto", "required"}, contract.FunctionToolChoices)
	require.True(t, contract.NamedFunctionToolChoice)
	require.Equal(t, []string{"auto", "required", "named"}, contract.FunctionToolChoicesRequiringTools)
	require.True(t, contract.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, contract.FunctionToolsRequireNonStream)
	require.True(t, contract.ExactFunctionToolResponseModeled)
	require.False(t, contract.ExactFunctionToolHistoryModeled, "history expansion is not part of this milestone")

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
