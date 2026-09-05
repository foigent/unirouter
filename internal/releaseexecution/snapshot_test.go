package releaseexecution

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"strings"
	"testing"
)

func TestReleaseSnapshotRequiresExactDefinitionAndCompleteRequirements(t *testing.T) {
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	d, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	s := Snapshot{SchemaVersion: SnapshotSchema, PublicModelID: 7, ProductLineageID: d.ProductLineageID(), ProfileDefinitionDigest: d.Digest(), ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, PublicContextLimit: 100000, PublicOutputLimit: 1024, RouteItemsHash: "sha256:" + strings.Repeat("1", 64), CapabilityPins: []CapabilityPin{}, Requirements: Requirements(d, nil)}
	require.NoError(t, s.ValidateDefinition(d))
	wire, err := s.CanonicalBytes()
	require.NoError(t, err)
	digest, err := s.Digest()
	require.NoError(t, err)
	verified, err := VerifySnapshot(wire, digest)
	require.NoError(t, err)
	require.NoError(t, verified.ValidateDefinition(d))
	_, err = VerifySnapshot(append(wire, ' '), digest)
	require.Error(t, err)
	bad := s
	bad.Requirements = bad.Requirements[1:]
	require.Error(t, bad.ValidateDefinition(d))
	bad = s
	bad.ProfileDefinitionDigest = strings.Repeat("a", 64)
	require.Error(t, bad.ValidateDefinition(d))
	bad = s
	bad.PublicOutputLimit = p.ProviderMaxOutput + 1
	require.Error(t, bad.ValidateDefinition(d))
	_, known := modelprofile.Lookup(p.ID, p.Version)
	require.False(t, known)
}
