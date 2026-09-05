package runtimegeneration

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestSupportIsImplementationInclusionNotCatalogOrWholeDigest(t *testing.T) {
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	d, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	m := CurrentManifest([]string{"M113", "M114"})
	require.NoError(t, m.Require("M113", d.Requirements()))
	require.NoError(t, m.Require("M114", d.Requirements()))
	require.Error(t, m.Require("M115", d.Requirements()))
	small := m
	small.Implementations = d.Requirements()
	a, err := m.Digest()
	require.NoError(t, err)
	b, err := small.Digest()
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	require.NoError(t, small.Require("M113", d.Requirements()))
	small.Implementations = slices.Clone(small.Implementations[1:])
	require.Error(t, small.Require("M113", d.Requirements()))
	wire, err := m.CanonicalBytes()
	require.NoError(t, err)
	verified, err := VerifyManifest(wire, a)
	require.NoError(t, err)
	require.NoError(t, verified.Require("M113", d.Requirements()))
	_, err = VerifyManifest(append(wire, ' '), a)
	require.Error(t, err)
	_, err = VerifyManifest(wire, b)
	require.Error(t, err)
	// Advertisement and request lifetime no longer need a compiled model entry.
	_, ok = modelprofile.Lookup(p.ID, p.Version)
	require.False(t, ok)
	require.GreaterOrEqual(t, modelprofile.MaximumImplementedRequestTimeoutSeconds(), 1800)
}
