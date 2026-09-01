package modelcapabilitypublication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM3GLM53FunctionToolsDeclarationKeepsExactProviderIdentitiesInOneCompatiblePlan(t *testing.T) {
	const publicationID = "000103_minimax_m3_glm53_function_tools_qualification"
	declaration, predecessor, err := CompiledPublication(publicationID)
	require.NoError(t, err)
	require.Equal(t, "000102_kimi_k3_chat_core_qualification", predecessor.ThroughMigration)
	require.Equal(t, predecessor.ThroughMigration, declaration.ExpectedStaticPredecessor)
	require.Equal(t, predecessor.ThroughMigration, declaration.ExpectedSchemaPredecessor)
	require.Len(t, declaration.BindingAppends, 2)
	require.Len(t, declaration.SelectorTransitions, 2)
	require.Len(t, declaration.DeclaredProjectionEffects, 2)
	require.Equal(t, []ProfileAttachmentGuard{{
		ExecutionProfileID: "glm.5.3.api", ExecutionProfileVersion: "1.8.0",
	}}, declaration.ProfileAttachmentGuards)

	want := map[string]Binding{
		"minimax": {
			OperationID: "openai.chat_completions.v1", CapabilityID: "function_tools",
			ExecutionProfileID: "minimax.m3.token-plan", ExecutionProfileVersion: "1.7.0",
			ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: "unirouter.capability-probe.minimax-chat-function-tools.v1", ProbeContractVersion: "1.0.0",
			ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M3", PublicPath: "/v1/chat/completions",
			IntroducedMigration: publicationID,
		},
		"glm": {
			OperationID: "openai.chat_completions.v1", CapabilityID: "function_tools",
			ExecutionProfileID: "glm.5.3.api", ExecutionProfileVersion: "1.8.0",
			ProtocolProfileID: "protocol.glm-openai-chat.v5", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: "unirouter.capability-probe.glm-chat-function-tools.v1", ProbeContractVersion: "1.0.0",
			ExpectedProvider: "glm", ExpectedUpstreamModel: "glm-5.3", PublicPath: "/v1/chat/completions",
			IntroducedMigration: publicationID,
		},
	}
	for _, binding := range declaration.BindingAppends {
		require.Equal(t, want[binding.ExpectedProvider], binding)
		delete(want, binding.ExpectedProvider)
	}
	require.Empty(t, want)

	folded, err := Fold(predecessor, []Declaration{declaration})
	require.NoError(t, err)
	require.Equal(t, publicationID, folded.ThroughMigration)
	require.Len(t, folded.Bindings, len(predecessor.Bindings)+2)
	require.Len(t, folded.Selectors, len(predecessor.Selectors)+2)
}
