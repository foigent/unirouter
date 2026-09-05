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
