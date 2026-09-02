package modelcapabilitypublication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGLMToggleFunctionToolsDeclarationKeepsFiveExactBindingsInOnePlan(t *testing.T) {
	const publicationID = "000105_glm_toggle_function_tools_qualification"
	declaration, predecessor, err := CompiledPublication(publicationID)
	require.NoError(t, err)
	require.Equal(t, "000104_minimax_payg_function_tools_qualification", predecessor.ThroughMigration)
	require.Equal(t, predecessor.ThroughMigration, declaration.ExpectedStaticPredecessor)
	require.Equal(t, predecessor.ThroughMigration, declaration.ExpectedSchemaPredecessor)
	require.Len(t, declaration.BindingAppends, 5)
	require.Len(t, declaration.SelectorTransitions, 5)
	require.Len(t, declaration.DeclaredProjectionEffects, 5)
	require.Len(t, declaration.ProfileAttachmentGuards, 5)

	want := map[string]struct{ profileID, version, protocolID string }{
		"glm-5.2":      {"glm.5.2.api", "1.8.0", "protocol.glm-openai-chat.v6"},
		"glm-5.1":      {"glm.5.1.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		"glm-5-turbo":  {"glm.5-turbo.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		"glm-5":        {"glm.5.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		"glm-5v-turbo": {"glm.5v-turbo.api", "1.11.0", "protocol.glm-openai-chat.glm5v.v5"},
	}
	for _, binding := range declaration.BindingAppends {
		expected, ok := want[binding.ExpectedUpstreamModel]
		require.True(t, ok)
		require.Equal(t, expected.profileID, binding.ExecutionProfileID)
		require.Equal(t, expected.version, binding.ExecutionProfileVersion)
		require.Equal(t, expected.protocolID, binding.ProtocolProfileID)
		require.Equal(t, "unirouter.capability-probe.glm-toggle-chat-function-tools.v1", binding.ProbeContractID)
		require.Equal(t, "1.0.0", binding.ProbeContractVersion)
		require.Equal(t, "glm", binding.ExpectedProvider)
		delete(want, binding.ExpectedUpstreamModel)
	}
	require.Empty(t, want)

	folded, err := Fold(predecessor, []Declaration{declaration})
	require.NoError(t, err)
	require.Equal(t, publicationID, folded.ThroughMigration)
	require.Len(t, folded.Bindings, len(predecessor.Bindings)+5)
	require.Len(t, folded.Selectors, len(predecessor.Selectors)+5)
}
