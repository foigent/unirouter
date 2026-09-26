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

func TestContentBridgePreservesEveryExactPredecessor(t *testing.T) {
	artifact := []string{"000001_init", LegacyCapabilityHead, CapabilityExpandHead, StreamCounterExpandHead, SourceFactsExpandHead, ObservationExpandHead, BudgetSnapshotExpandHead, ContentCaptureExpandHead}
	for end := 2; end <= len(artifact); end++ {
		require.NoError(t, VerifyRuntimeLedger(artifact, artifact[:end]))
	}
	require.Error(t, VerifyRuntimeLedger(artifact[:len(artifact)-1], artifact), "old artifact cannot admit M119")
	require.Error(t, VerifyRuntimeLedger(artifact, append(slices.Clone(artifact), "000120_unknown")))
	for i := 1; i < len(artifact)-1; i++ {
		missing := append(slices.Clone(artifact[:i]), artifact[i+1:]...)
		require.Error(t, VerifyRuntimeLedger(artifact, missing), "head alone cannot replace missing predecessor")
		_, err := RuntimeLedgers(missing)
		require.Error(t, err, "M119 bridge must retain exact full tail")
	}
	bridges, err := RuntimeLedgers(artifact)
	require.NoError(t, err)
	bridges[0][0] = "mutated"
	require.Equal(t, "000001_init", artifact[0], "bridge must not mutate trusted artifact")
	require.Equal(t, "000001_init", bridges[1][0], "bridge snapshots must not alias")
}

func TestReclamationBridgeRequiresExactM119Predecessor(t *testing.T) {
	artifact := []string{"000001_init", LegacyCapabilityHead, CapabilityExpandHead, StreamCounterExpandHead, SourceFactsExpandHead, ObservationExpandHead, BudgetSnapshotExpandHead, ContentCaptureExpandHead, ContentReclaimExpandHead}
	for end := 2; end <= len(artifact); end++ {
		require.NoError(t, VerifyRuntimeLedger(artifact, artifact[:end]))
	}
	for missing := 1; missing < len(artifact)-1; missing++ {
		broken := append(append([]string{}, artifact[:missing]...), artifact[missing+1:]...)
		require.Error(t, VerifyRuntimeLedger(broken, broken))
		require.Error(t, VerifyRuntimeLedger(artifact, broken))
	}
	require.Error(t, VerifyRuntimeLedger(artifact, append(append([]string{}, artifact...), "000121_unknown")))
}

func TestPreDispatchBridgeRequiresExactM120Predecessor(t *testing.T) {
	artifact := []string{"000001_init", LegacyCapabilityHead, CapabilityExpandHead, StreamCounterExpandHead, SourceFactsExpandHead, ObservationExpandHead, BudgetSnapshotExpandHead, ContentCaptureExpandHead, ContentReclaimExpandHead, PreDispatchExpandHead}
	for end := 2; end <= len(artifact); end++ {
		require.NoError(t, VerifyRuntimeLedger(artifact, artifact[:end]))
	}
	for missing := 1; missing < len(artifact)-1; missing++ {
		broken := append(append([]string{}, artifact[:missing]...), artifact[missing+1:]...)
		require.Error(t, VerifyRuntimeLedger(artifact, broken))
	}
	require.Error(t, VerifyRuntimeLedger(artifact[:len(artifact)-1], artifact), "M120 binary must reject M121")
	require.Error(t, VerifyRuntimeLedger(artifact, append(slices.Clone(artifact), "000122_unknown")))
}
