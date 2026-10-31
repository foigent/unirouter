package schemaauthority

import (
	"github.com/stretchr/testify/require"
	"slices"
	"testing"
)

func TestUpstreamObservationExactMaintenanceBoundary(t *testing.T) {
	artifact, err := ExpectedLedger("../../migrations")
	require.NoError(t, err)
	require.Equal(t, UpstreamObservationRestoreHead, artifact[len(artifact)-1])
	require.Equal(t, UpstreamReleaseObservationHead, artifact[len(artifact)-2])
	prior := artifact[:len(artifact)-1]
	require.Equal(t, UpstreamReleaseObservationHead, prior[len(prior)-1])
	require.Equal(t, XInsightCommunicationHead, prior[len(prior)-2])
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	require.Error(t, VerifyRuntimeLedger(artifact, prior))
	require.Error(t, VerifyRuntimeLedger(prior, artifact))
	require.NoError(t, VerifyRuntimeLedger(prior, prior))
	for _, bad := range [][]string{append(slices.Clone(artifact), "000129_unknown"), append(slices.Clone(artifact[:20]), artifact[21:]...)} {
		require.Error(t, VerifyRuntimeLedger(artifact, bad))
	}
}
