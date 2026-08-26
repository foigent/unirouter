package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekV4ProToolHistoryPromotionIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("deepseek.v4-pro.api", "1.3.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, "protocol.deepseek-openai-chat.v3", historical.ProtocolProfile)

	candidate, ok := Lookup("deepseek.v4-pro.api", "1.4.0")
	require.True(t, ok)
	require.True(t, candidate.Current)
	require.Equal(t, "protocol.deepseek-openai-chat.v4", candidate.ProtocolProfile)
	require.Equal(t, "reasoning.deepseek-v4-pro-thinking-toggle.v4", candidate.ReasoningProfile)
	require.Equal(t, []string{EndpointClassDeepSeekOfficialOpenAI}, candidate.EndpointClasses)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	reasoning, ok := ResolveReasoningContract(candidate.ReasoningProfile)
	require.True(t, ok)
	require.True(t, reasoning.PreserveAssistantReasoningForToolRequests)
	require.Equal(t, []string{"low", "high", "max"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, map[string]string{"medium": "high", "xhigh": "high"}, reasoning.EffortControl.CompatibilityValues)
	require.NoError(t, ValidateExecutableComponents(candidate))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
	require.NoError(t, ValidateNewAttachment(candidate.ID, candidate.Version))
}
