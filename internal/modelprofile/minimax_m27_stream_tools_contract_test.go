package modelprofile

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMiniMaxM27StreamSuccessorIsExactAndAppendOnly(t *testing.T) {
	old, ok := Lookup("minimax.m2.7.payg", "1.2.0")
	require.True(t, ok)
	current, ok := Lookup("minimax.m2.7.payg", "1.3.0")
	require.True(t, ok)
	require.Equal(t, old.EndpointClasses, current.EndpointClasses)
	require.Equal(t, old.UpstreamModelIDs, current.UpstreamModelIDs)
	require.Equal(t, old.TokenizerProfileID, current.TokenizerProfileID)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v7", old.ProtocolProfile)
	require.Equal(t, "protocol.minimax-m27-openai-chat.v1", current.ProtocolProfile)
	prior, _ := ResolveChatProtocolContract(old.ProtocolProfile)
	next, _ := ResolveChatProtocolContract(current.ProtocolProfile)
	require.True(t, prior.FunctionToolsRequireNonStream)
	require.Empty(t, prior.FunctionToolChoices)
	require.False(t, prior.StreamContract.FunctionToolDeltas)
	require.False(t, next.FunctionToolsRequireNonStream)
	require.Equal(t, []string{"auto"}, next.FunctionToolChoices)
	require.True(t, next.StreamContract.FunctionToolDeltas)
	r1, _ := ResolveReasoningContract(old.ReasoningProfile)
	r2, _ := ResolveReasoningContract(current.ReasoningProfile)
	require.Equal(t, "content", r1.AssistantToolHistoryReplayField)
	require.Empty(t, r2.AssistantToolHistoryReplayField)
	require.Equal(t, ReasoningModeRequired, r2.Mode)
	require.False(t, r2.SplitOutputEnabledSupported)
	c1, _ := ResolveCacheUsageContract(old.CacheUsageProfile)
	c2, _ := ResolveCacheUsageContract(current.CacheUsageProfile)
	require.False(t, c1.StreamReadCounterOptional)
	require.True(t, c2.StreamReadCounterOptional)
	require.Equal(t, 512, c2.ReadCounterRequiredAtOrAbovePromptTokens)
	identity, found, err := ResolveOperationProtocolIdentity(current.ID, current.Version, OperationOpenAIChat)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, current.ProtocolProfile, identity.ProtocolProfileID)
	for _, id := range []string{"minimax.m3.payg", "minimax.m2.7-highspeed.payg"} {
		profile, ok := Lookup(id, "1.2.0")
		require.True(t, ok)
		contract, _ := ResolveChatProtocolContract(profile.ProtocolProfile)
		require.True(t, contract.FunctionToolsRequireNonStream)
		require.False(t, contract.StreamContract.FunctionToolDeltas)
	}
}
