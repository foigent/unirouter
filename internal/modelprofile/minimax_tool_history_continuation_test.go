package modelprofile

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMiniMaxToolContinuationReasoningKeepsOtherSemantics(t *testing.T) {
	m27, ok := ResolveReasoningContract("reasoning.minimax-m27-required-embedded.v2")
	require.True(t, ok)
	m2, ok := ResolveReasoningContract("reasoning.minimax-m2-required-embedded.v2")
	require.True(t, ok)
	m2.ProfileID = m27.ProfileID
	require.Equal(t, m27, m2)
	for _, pair := range [][2]string{{"reasoning.minimax-m2-required-embedded.v1", "reasoning.minimax-m2-required-embedded.v2"}, {"reasoning.minimax-m3-adaptive-embedded.v2", "reasoning.minimax-m3-adaptive-embedded.v3"}} {
		old, ok := ResolveReasoningContract(pair[0])
		require.True(t, ok)
		next, ok := ResolveReasoningContract(pair[1])
		require.True(t, ok)
		require.Equal(t, "content", old.AssistantToolHistoryReplayField)
		require.Empty(t, next.AssistantToolHistoryReplayField)
		next.ProfileID = old.ProfileID
		next.AssistantToolHistoryReplayField = "content"
		require.Equal(t, old, next)
	}
}
