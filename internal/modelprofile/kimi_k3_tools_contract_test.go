package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiK3ExactDynamicFunctionToolsPromotionIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("kimi.k3.api", "1.6.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, "protocol.kimi-openai-chat.k3.v3", historical.ProtocolProfile)
	require.Contains(t, historical.Capabilities, CapabilityDynamicTools)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))

	candidate, ok := Lookup("kimi.k3.api", "1.7.0")
	require.True(t, ok)
	require.False(t, candidate.Current, "Dynamic Tools closure is required before a later candidate can be promoted")
	require.Equal(t, "protocol.kimi-openai-chat.k3.v4", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.kimi-k3-effort.v2", candidate.ReasoningProfile)
	require.Equal(t, []string{EndpointClassKimiPlatformOpenAIV1}, candidate.EndpointClasses)
	require.Equal(t, []string{"kimi-k3"}, candidate.UpstreamModelIDs)
	require.Contains(t, candidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceAuto)
	require.Contains(t, candidate.Capabilities, CapabilityToolChoiceReq)
	require.NotContains(t, candidate.Capabilities, CapabilityToolChoiceNamed)
	require.NotContains(t, candidate.Capabilities, CapabilityDynamicTools)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"none", "auto", "required"}, protocol.FunctionToolChoices)
	require.Equal(t, "auto", protocol.FunctionToolProviderDefaultChoice)
	require.False(t, protocol.NamedFunctionToolChoice)
	require.False(t, protocol.DynamicSystemTools)
	require.True(t, protocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.NoError(t, ValidateExecutableComponents(candidate))
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))

	dynamicCandidate, ok := Lookup("kimi.k3.api", "1.8.0")
	require.True(t, ok)
	require.True(t, dynamicCandidate.Current)
	require.Equal(t, "protocol.kimi-openai-chat.k3.v5", dynamicCandidate.ProtocolProfile)
	require.Contains(t, dynamicCandidate.Capabilities, CapabilityDynamicTools)
	require.Contains(t, dynamicCandidate.Capabilities, CapabilityFunctionTools)
	require.Contains(t, dynamicCandidate.Capabilities, CapabilityToolChoiceAuto)
	require.Contains(t, dynamicCandidate.Capabilities, CapabilityToolChoiceReq)
	require.NotContains(t, dynamicCandidate.Capabilities, CapabilityToolChoiceNamed)

	dynamicProtocol, ok := ResolveChatProtocolContract(dynamicCandidate.ProtocolProfile)
	require.True(t, ok)
	require.True(t, dynamicProtocol.DynamicSystemTools)
	require.True(t, dynamicProtocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, dynamicProtocol.FunctionToolsRequireNonStream)
	require.True(t, dynamicProtocol.ExactFunctionToolHistoryModeled)
	require.True(t, dynamicProtocol.ExactFunctionToolResponseModeled)
	require.NoError(t, ValidateExecutableComponents(dynamicCandidate))
	require.NoError(t, ValidateNewAttachment(dynamicCandidate.ID, dynamicCandidate.Version))
}
