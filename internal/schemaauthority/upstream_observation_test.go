package schemaauthority

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamObservationExactMaintenanceBoundary(t *testing.T) {
	artifact, err := ExpectedLedger("../../migrations")
	require.NoError(t, err)
	require.Equal(t, AsyncMediaProviderTaskHead, artifact[len(artifact)-1])
	require.Equal(t, CanonicalMediaJobHead, artifact[len(artifact)-2])
	priorArtifact := artifact[:len(artifact)-1]
	require.Error(t, VerifyRuntimeLedger(artifact, priorArtifact))
	require.Error(t, VerifyRuntimeLedger(priorArtifact, artifact))
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	artifact = priorArtifact[:len(priorArtifact)-1]
	require.Equal(t, NativeInlineJPEGHead, artifact[len(artifact)-1])
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	m133 := artifact[:len(artifact)-1]
	require.Equal(t, MediaChargeSourceHead, m133[len(m133)-1])
	require.Error(t, VerifyRuntimeLedger(artifact, m133))
	require.Error(t, VerifyRuntimeLedger(m133, artifact))
	artifact = m133
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	m132 := artifact[:len(artifact)-1]
	require.Equal(t, OpenAIResponsesToolsHead, m132[len(m132)-1])
	require.Error(t, VerifyRuntimeLedger(artifact, m132))
	require.Error(t, VerifyRuntimeLedger(m132, artifact))
	artifact = m132
	m131 := artifact[:len(artifact)-1]
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	require.Error(t, VerifyRuntimeLedger(artifact, m131))
	require.Error(t, VerifyRuntimeLedger(m131, artifact))
	artifact = m131
	require.Equal(t, CustomerStandardPricingHead, artifact[len(artifact)-1])
	m130 := artifact[:len(artifact)-1]
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	require.Error(t, VerifyRuntimeLedger(artifact, m130))
	require.Error(t, VerifyRuntimeLedger(m130, artifact))
	artifact = m130
	require.Equal(t, UpgradeAdmissionRecoveryHead, artifact[len(artifact)-1])
	require.Equal(t, UpstreamObservationRestoreHead, artifact[len(artifact)-2])
	m129 := artifact[:len(artifact)-1]
	require.NoError(t, VerifyRuntimeLedger(artifact, artifact))
	require.Error(t, VerifyRuntimeLedger(artifact, m129))
	require.Error(t, VerifyRuntimeLedger(m129, artifact))
	artifact = m129
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
