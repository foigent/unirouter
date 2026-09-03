package modelcapabilitypublication

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRuntimeCapabilityDeclarationIsOneExactNineModelPublication(t *testing.T) {
	const publicationID = "000110_openai_chat_runtime_capability_qualification"
	declaration, _, err := CompiledPublication(publicationID)
	require.NoError(t, err)
	require.Equal(t, "000108_minimax_cache_threshold_qualification", declaration.ExpectedStaticPredecessor)
	require.Equal(t, "000109_staging_model_successor_cutover", declaration.ExpectedSchemaPredecessor)
	require.Len(t, declaration.BindingAppends, 42)
	require.Len(t, declaration.SelectorTransitions, 42)
	require.Len(t, declaration.DeclaredProjectionEffects, 42)
	require.Empty(t, declaration.ProfileAttachmentGuards, "existing Profiles must not become rollback attachment guards")

	models := map[string]map[string]bool{}
	for _, binding := range declaration.BindingAppends {
		require.Equal(t, publicationID, binding.IntroducedMigration)
		require.Equal(t, "2.0.0", binding.ProbeContractVersion)
		if models[binding.ExecutionProfileID] == nil {
			models[binding.ExecutionProfileID] = map[string]bool{}
		}
		require.False(t, models[binding.ExecutionProfileID][binding.CapabilityID], "duplicate capability binding")
		models[binding.ExecutionProfileID][binding.CapabilityID] = true
	}
	require.Len(t, models, 9)
	for profileID, capabilities := range models {
		if len(profileID) >= len("minimax.") && profileID[:len("minimax.")] == "minimax." {
			require.Equal(t, map[string]bool{
				"cache_read_usage": true, "function_tools": true, "prompt_cache": true,
				"reasoning": true, "tool_choice_auto": true, "tools": true,
			}, capabilities)
			continue
		}
		require.Equal(t, map[string]bool{
			"function_tools": true, "reasoning": true, "tool_choice_auto": true, "tools": true,
		}, capabilities)
	}

	for _, transition := range declaration.SelectorTransitions {
		if transition.CapabilityID == "function_tools" {
			require.NotNil(t, transition.ExpectedPredecessor)
			require.Equal(t, "1.0.0", transition.ExpectedPredecessor.ProbeContractVersion)
		} else {
			require.Nil(t, transition.ExpectedPredecessor)
		}
		require.Equal(t, "2.0.0", transition.Final.ProbeContractVersion)
	}
}
