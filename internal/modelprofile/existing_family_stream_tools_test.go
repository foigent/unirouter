package modelprofile

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMiniMaxM3StreamTierKeepsRequestAndMeteringContract(t *testing.T) {
	old, ok := ResolveChatProtocolContract("protocol.minimax-m3-openai-chat.v10")
	require.True(t, ok)
	next, ok := ResolveChatProtocolContract("protocol.minimax-m3-openai-chat.v11")
	require.True(t, ok)
	next.ProfileID = old.ProfileID
	require.Equal(t, old, next, "only exact-scoped adapter response preparation differs")
}

func TestExistingFamilyStreamImplementationsLeaveHistoryUnchanged(t *testing.T) {
	for _, pair := range [][2]string{
		{"protocol.deepseek-openai-chat.v4", "protocol.deepseek-openai-chat.v5"},
		{"protocol.kimi-openai-chat.v5", "protocol.kimi-openai-chat.v6"},
		{"protocol.kimi-openai-chat.k3.v5", "protocol.kimi-openai-chat.k3.v6"},
		{"protocol.minimax-m3-openai-chat.v9", "protocol.minimax-m3-openai-chat.v10"},
	} {
		old, ok := ResolveChatProtocolContract(pair[0])
		require.True(t, ok)
		next, ok := ResolveChatProtocolContract(pair[1])
		require.True(t, ok)
		require.True(t, old.FunctionToolsRequireNonStream)
		require.False(t, old.StreamContract.FunctionToolDeltas)
		require.False(t, next.FunctionToolsRequireNonStream)
		require.True(t, next.StreamContract.FunctionToolDeltas)
		require.Equal(t, old.OutputLimitMax, next.OutputLimitMax)
		require.Equal(t, old.ExactFunctionToolHistoryModeled, next.ExactFunctionToolHistoryModeled)
	}
	old, _ := ResolveReasoningContract("reasoning.minimax-m3-adaptive-embedded.v1")
	next, ok := ResolveReasoningContract("reasoning.minimax-m3-adaptive-embedded.v2")
	require.True(t, ok)
	require.True(t, old.ActiveStreamingUnsupported)
	require.False(t, next.ActiveStreamingUnsupported)
	next.ProfileID = old.ProfileID
	next.ActiveStreamingUnsupported = true
	require.Equal(t, old, next, "only the exact streaming reasoning restriction changes")
}
