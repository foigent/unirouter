package modelcapabilitypublication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiK27ChatCoreDeclarationIsExactAndDoesNotAuthorizeK3OrGeneric(t *testing.T) {
	const publicationID = "000101_kimi_k27_chat_core_qualification"
	declaration, predecessor, err := CompiledPublication(publicationID)
	require.NoError(t, err)
	require.Equal(t, BaselineMigrationM100, predecessor.ThroughMigration)
	require.Equal(t, BaselineMigrationM100, declaration.ExpectedStaticPredecessor)
	require.Equal(t, BaselineMigrationM100, declaration.ExpectedSchemaPredecessor)
	require.Len(t, declaration.BindingAppends, 6)
	require.Len(t, declaration.SelectorTransitions, 6)
	require.Len(t, declaration.DeclaredProjectionEffects, 6)

	wantVersions := map[string]string{
		"streaming":        "4.0.0",
		"reasoning":        "2.0.0",
		"tools":            "4.0.0",
		"function_tools":   "4.0.0",
		"tool_choice_auto": "4.0.0",
		"json_object":      "2.0.0",
	}
	for _, binding := range declaration.BindingAppends {
		require.Equal(t, "openai.chat_completions.v1", binding.OperationID)
		require.Equal(t, "kimi.k2.7-code.api", binding.ExecutionProfileID)
		require.Equal(t, "1.3.0", binding.ExecutionProfileVersion)
		require.Equal(t, "protocol.kimi-openai-chat.v5", binding.ProtocolProfileID)
		require.Equal(t, "1.0.0", binding.ProtocolProfileVersion)
		require.Equal(t, "kimi", binding.ExpectedProvider)
		require.Equal(t, "kimi-k2.7-code", binding.ExpectedUpstreamModel)
		require.Equal(t, publicationID, binding.IntroducedMigration)
		require.Equal(t, wantVersions[binding.CapabilityID], binding.ProbeContractVersion)
		delete(wantVersions, binding.CapabilityID)
	}
	require.Empty(t, wantVersions)

	folded, err := Fold(predecessor, []Declaration{declaration})
	require.NoError(t, err)
	require.Equal(t, publicationID, folded.ThroughMigration)
	require.Len(t, folded.Bindings, len(predecessor.Bindings)+6)
	require.Len(t, folded.Selectors, len(predecessor.Selectors)+6)
	for _, selector := range folded.Selectors {
		if selector.ExecutionProfileID == "kimi.k3.api" || selector.ProtocolProfileID == "openai.chat_completions" && selector.ExecutionProfileID == "kimi.k2.7-code.api" {
			t.Fatalf("unexpected sibling or generic selector: %+v", selector)
		}
	}
}
