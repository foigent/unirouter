package runtimegeneration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestLegacyRetirementStillRejectsProductionBeforeDatabase(t *testing.T) {
	input := RetirementInput{InstanceID: "uri_" + strings.Repeat("a", 32), ArtifactDigest: "sha256:" + strings.Repeat("b", 64), ManifestDigest: strings.Repeat("c", 64), CompatibilityEpoch: CompatibilityEpoch, ExpectedSchemaHead: schemaauthority.XInsightCommunicationHead, ExpectedRevision: 1, ExpectedIngressHead: "withdrawal", ExpectedEnvironment: "production", ExpectedDatabaseSystemID: "123456789", HostProofSHA256: "sha256:" + strings.Repeat("d", 64)}
	require.NoError(t, input.validateIdentity())
	_, err := CheckRetirement(context.Background(), nil, input)
	require.ErrorContains(t, err, "exact historical retirement identity and host proof required")
	_, err = ApplyRetirement(context.Background(), nil, input)
	require.ErrorContains(t, err, "exact historical retirement identity and host proof required")
	input.ExpectedEnvironment = "test"
	require.NoError(t, input.validate())
	input.ExpectedSchemaHead = schemaauthority.UpstreamReleaseObservationHead
	// This is a valid generic historical identity, but never the finite
	// production-capable community predecessor.
	_, err = CheckM127MaintenanceRetirementTx(context.Background(), nil, input, false)
	require.ErrorContains(t, err, "exact M127 maintenance predecessor")
}
