package modelcapabilitypublication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiK3ChatCoreDeclarationIsExactAndDoesNotAuthorizeExcludedCapabilities(t *testing.T) {
	const publicationID = "000102_kimi_k3_chat_core_qualification"
	declaration, predecessor, err := CompiledPublication(publicationID)
	require.NoError(t, err)
	require.Equal(t, "000101_kimi_k27_chat_core_qualification", predecessor.ThroughMigration)
	require.Equal(t, predecessor.ThroughMigration, declaration.ExpectedStaticPredecessor)
	require.Equal(t, predecessor.ThroughMigration, declaration.ExpectedSchemaPredecessor)
	require.Len(t, declaration.BindingAppends, 8)
	require.Len(t, declaration.SelectorTransitions, 8)
	require.Len(t, declaration.DeclaredProjectionEffects, 8)
	require.Equal(t, []ProfileAttachmentGuard{{
		ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.9.0",
	}}, declaration.ProfileAttachmentGuards)

	wantVersions := map[string]string{
		"streaming":            "4.0.0",
		"reasoning":            "2.0.0",
		"reasoning_effort":     "1.0.0",
		"tools":                "4.0.0",
		"function_tools":       "4.0.0",
		"tool_choice_auto":     "4.0.0",
		"tool_choice_required": "4.0.0",
		"json_object":          "2.0.0",
	}
	for _, binding := range declaration.BindingAppends {
		require.Equal(t, "openai.chat_completions.v1", binding.OperationID)
		require.Equal(t, "kimi.k3.api", binding.ExecutionProfileID)
		require.Equal(t, "1.9.0", binding.ExecutionProfileVersion)
		require.Equal(t, "protocol.kimi-openai-chat.k3.v5", binding.ProtocolProfileID)
		require.Equal(t, "1.0.0", binding.ProtocolProfileVersion)
		require.Equal(t, "kimi", binding.ExpectedProvider)
		require.Equal(t, "kimi-k3", binding.ExpectedUpstreamModel)
		require.Equal(t, publicationID, binding.IntroducedMigration)
		require.Equal(t, wantVersions[binding.CapabilityID], binding.ProbeContractVersion)
		delete(wantVersions, binding.CapabilityID)
	}
	require.Empty(t, wantVersions)

	folded, err := Fold(predecessor, []Declaration{declaration})
	require.NoError(t, err)
	require.Equal(t, publicationID, folded.ThroughMigration)
	require.Len(t, folded.Bindings, len(predecessor.Bindings)+8)
	require.Len(t, folded.Selectors, len(predecessor.Selectors)+8)
	for _, selector := range folded.Selectors {
		if selector.ExecutionProfileID != "kimi.k3.api" {
			continue
		}
		require.Equal(t, "1.9.0", selector.ExecutionProfileVersion)
		require.Equal(t, "protocol.kimi-openai-chat.k3.v5", selector.ProtocolProfileID)
		require.NotEqual(t, "openai.chat_completions", selector.ProtocolProfileID)
		require.NotEqual(t, "dynamic_tools", selector.CapabilityID)
		require.NotEqual(t, "json_schema", selector.CapabilityID)
	}
}
