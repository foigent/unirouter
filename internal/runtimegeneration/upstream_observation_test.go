package runtimegeneration

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"slices"
	"testing"
)

func TestUpstreamObservationRequiresExactImplementationAndHead(t *testing.T) {
	m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	require.NoError(t, requireLeaseManifest(m, schemaauthority.UpstreamReleaseObservationHead))
	absent := m
	absent.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(r modelprofile.ImplementationReference) bool { return r.ID == UpstreamReleaseObservationContract })
	require.ErrorIs(t, requireLeaseManifest(absent, schemaauthority.UpstreamReleaseObservationHead), ErrIncompatible)
	absent = m
	absent.SchemaHeads = slices.DeleteFunc(slices.Clone(m.SchemaHeads), func(h string) bool { return h == schemaauthority.UpstreamReleaseObservationHead })
	require.ErrorIs(t, requireLeaseManifest(absent, schemaauthority.UpstreamReleaseObservationHead), ErrIncompatible)
}
