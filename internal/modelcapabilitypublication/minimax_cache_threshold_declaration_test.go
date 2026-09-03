package modelcapabilitypublication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxCacheThresholdDeclarationKeepsThreeExactSuccessorsInOnePlan(t *testing.T) {
	const publicationID = "000108_minimax_cache_threshold_qualification"
	declaration, predecessor, err := CompiledPublication(publicationID)
	require.NoError(t, err)
	require.Equal(t, "000106_minimax_official_cn_payg_endpoint_qualification", declaration.ExpectedStaticPredecessor)
	require.Equal(t, "000107_stable_model_successor_cutover", declaration.ExpectedSchemaPredecessor)
	require.Equal(t, declaration.ExpectedStaticPredecessor, predecessor.ThroughMigration)
	require.Len(t, declaration.BindingAppends, 3)
	require.Len(t, declaration.SelectorTransitions, 3)
	require.Len(t, declaration.DeclaredProjectionEffects, 3)
	require.ElementsMatch(t, []ProfileAttachmentGuard{
		{ExecutionProfileID: "minimax.m3.payg", ExecutionProfileVersion: "1.2.0"},
		{ExecutionProfileID: "minimax.m2.7.payg", ExecutionProfileVersion: "1.2.0"},
		{ExecutionProfileID: "minimax.m2.7-highspeed.payg", ExecutionProfileVersion: "1.2.0"},
	}, declaration.ProfileAttachmentGuards)

	want := map[string]struct {
		profileID, protocolID, contractID string
	}{
		"MiniMax-M3":             {"minimax.m3.payg", "protocol.minimax-m3-openai-chat.v9", "unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1"},
		"MiniMax-M2.7":           {"minimax.m2.7.payg", "protocol.minimax-m2-openai-chat.v7", "unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1"},
		"MiniMax-M2.7-highspeed": {"minimax.m2.7-highspeed.payg", "protocol.minimax-m2-openai-chat.v7", "unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1"},
	}
	for _, binding := range declaration.BindingAppends {
		expected, ok := want[binding.ExpectedUpstreamModel]
		require.True(t, ok)
		require.Equal(t, expected.profileID, binding.ExecutionProfileID)
		require.Equal(t, "1.2.0", binding.ExecutionProfileVersion)
		require.Equal(t, expected.protocolID, binding.ProtocolProfileID)
		require.Equal(t, expected.contractID, binding.ProbeContractID)
		require.Equal(t, publicationID, binding.IntroducedMigration)
		delete(want, binding.ExpectedUpstreamModel)
	}
	require.Empty(t, want)

	folded, err := Fold(predecessor, []Declaration{declaration})
	require.NoError(t, err)
	require.Equal(t, publicationID, folded.ThroughMigration)
	require.Len(t, folded.Bindings, len(predecessor.Bindings)+3)
	require.Len(t, folded.Selectors, len(predecessor.Selectors)+3)
}
