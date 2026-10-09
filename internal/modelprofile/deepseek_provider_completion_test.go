package modelprofile

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDeepSeekProviderCompletionIsAnExplicitSuccessor(t *testing.T) {
	old, ok := ResolveChatProtocolContract("protocol.deepseek-openai-chat.v5")
	require.True(t, ok)
	next, ok := ResolveChatProtocolContract(DeepSeekProviderCompletionProtocol)
	require.True(t, ok)
	require.NoError(t, validateChatProtocolContractShape(next))
	require.Empty(t, old.OutputEnforcement)
	require.False(t, old.StreamContract.RequireTerminalUsageChoices)
	require.Equal(t, []string{"max_tokens"}, next.OutputLimitAcceptedFields)
	require.EqualValues(t, 1, next.OutputLimitMin)
	require.True(t, next.StreamContract.RequireDone)
	require.True(t, next.StreamContract.RequireTerminalUsage)
	require.True(t, next.StreamContract.RequireTerminalUsageChoices)
	before, after := old.StreamContract, next.StreamContract
	after.RequireTerminalUsageChoices = false
	require.Equal(t, before, after)
	policy, ok := ResolveOutputEnforcement(DeepSeekProviderCompletionProtocol)
	require.True(t, ok)
	require.Equal(t, OutputEnforcementProviderCompletionV1, policy.Accounting)
	require.Equal(t, "generated_completion", policy.Scope)
	require.Zero(t, policy.ProviderDeviationTokens)
	require.EqualValues(t, 16<<20, policy.MaxDecodedOutputBytes)
	require.EqualValues(t, 128<<20, policy.MaxUpstreamWireBytes)
	for _, mutate := range []func(*ChatProtocolContract){
		func(c *ChatProtocolContract) { c.OutputLimitField = "max_completion_tokens" },
		func(c *ChatProtocolContract) {
			c.OutputLimitAcceptedFields = append(c.OutputLimitAcceptedFields, "max_completion_tokens")
		},
		func(c *ChatProtocolContract) { c.OutputLimitMin = 11 },
		func(c *ChatProtocolContract) { c.StreamContract.RequireTerminalUsageChoices = false },
	} {
		bad, _ := ResolveChatProtocolContract(DeepSeekProviderCompletionProtocol)
		mutate(&bad)
		require.Error(t, validateChatProtocolContractShape(bad))
	}
}
