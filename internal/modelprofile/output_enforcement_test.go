package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderCompletionContractIsExplicitAndVersioned(t *testing.T) {
	old, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v7")
	require.True(t, ok)
	require.Empty(t, old.OutputEnforcement)
	require.EqualValues(t, 1, old.OutputLimitMin)
	next, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v8")
	require.True(t, ok)
	require.NoError(t, validateChatProtocolContractShape(next))
	require.Equal(t, []string{"max_completion_tokens"}, next.OutputLimitAcceptedFields)
	require.EqualValues(t, 11, next.OutputLimitMin)
	require.Equal(t, old.StreamContract, next.StreamContract, "wire mechanics are reused without rewriting v7")
	enforcement, ok := ResolveOutputEnforcement(next.ProfileID)
	require.True(t, ok)
	require.Equal(t, OutputEnforcementProviderCompletionV1, enforcement.Accounting)
	require.Equal(t, "reasoning_and_answer", enforcement.Scope)
	require.Equal(t, 10, enforcement.ProviderDeviationTokens)
	require.EqualValues(t, 16<<20, enforcement.MaxDecodedOutputBytes)
	require.EqualValues(t, 128<<20, enforcement.MaxUpstreamWireBytes)
	oldEnforcement, ok := ResolveOutputEnforcement(old.ProfileID)
	require.True(t, ok)
	require.Equal(t, OutputEnforcementByteUpperBound, oldEnforcement.Accounting)
	_, ok = ResolveOutputEnforcement("unknown-contract")
	require.False(t, ok)
	next.OutputEnforcement = "arbitrary-policy"
	require.Error(t, validateChatProtocolContractShape(next))
}
