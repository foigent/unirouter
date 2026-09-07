package schemaauthority

import (
	"github.com/stretchr/testify/require"
	"slices"
	"testing"
)

func TestRuntimeBridgeAcceptsOnlyTwoCompleteLedgers(t *testing.T) {
	artifact := []string{"000001_init", LegacyCapabilityHead, CapabilityExpandHead}
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact[:2]))
	for _, actual := range [][]string{nil, {LegacyCapabilityHead}, artifact[:1], {artifact[0], CapabilityExpandHead},
		append(slices.Clone(artifact), "000115_future"), {artifact[0], LegacyCapabilityHead, LegacyCapabilityHead},
		{LegacyCapabilityHead, artifact[0]}} {
		require.Error(t, VerifyRuntimeLedger(artifact, actual))
	}
	require.Error(t, VerifyLedger(artifact, artifact[:2]), "strict artifact checks remain strict")
	_, err := RuntimeLedgers([]string{LegacyCapabilityHead})
	require.Error(t, err)
}

func TestCounterBridgeAcceptsThreeFullLedgersOnly(t *testing.T) {
	artifact := []string{"000001_init", LegacyCapabilityHead, CapabilityExpandHead, StreamCounterExpandHead}
	for _, end := range []int{2, 3, 4} {
		require.NoError(t, VerifyRuntimeLedger(artifact, artifact[:end]))
	}
	for _, actual := range [][]string{artifact[:1], {artifact[0], LegacyCapabilityHead, StreamCounterExpandHead}, append(slices.Clone(artifact), "000116_future")} {
		require.Error(t, VerifyRuntimeLedger(artifact, actual))
	}
	require.Error(t, VerifyRuntimeLedger(artifact[:3], artifact), "old artifact may not restart on M115")
	require.Error(t, VerifyLedger(artifact, artifact[:3]), "strict schema owner cannot skip expansion")
}
