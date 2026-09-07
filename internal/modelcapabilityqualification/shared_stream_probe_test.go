package modelcapabilityqualification

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"testing"
)

func TestSharedProbeContractUsesExistingExactBindings(t *testing.T) {
	all, err := allBindings()
	require.NoError(t, err)
	count := 0
	for _, b := range all {
		if b.ProbeContractID != GLMStreamFunctionToolsProbeContractID && b.ProbeContractID != MiniMaxM27StreamFunctionToolsProbeContractID {
			continue
		}
		key, err := SharedStreamProbeContract(b)
		require.NoError(t, err)
		require.Equal(t, b.ProbeContractID+"@"+b.ProbeContractVersion, key)
		count++
		bad := b
		bad.ExpectedUpstreamModel += "-other"
		_, err = SharedStreamProbeContract(bad)
		require.Error(t, err)
	}
	require.Equal(t, 35, count)
	for _, completion := range []bool{false, true} {
		for _, capability := range []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto} {
			b := qwenDataContractBinding(t, capability, completion)
			key, err := SharedStreamProbeContract(b)
			require.NoError(t, err)
			require.Equal(t, b.ProbeContractID+"@1.0.0", key)
		}
	}
	old, _ := SharedStreamProbeContract(qwenDataContractBinding(t, "streaming", false))
	next, _ := SharedStreamProbeContract(qwenDataContractBinding(t, "streaming", true))
	require.NotEqual(t, old, next)
}
