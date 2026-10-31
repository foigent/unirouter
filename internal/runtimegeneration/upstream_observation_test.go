package runtimegeneration

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"slices"
	"testing"
)

func TestUpstreamObservationRequiresExactImplementationAndHead(t *testing.T) {
	for _, head := range []string{schemaauthority.UpstreamReleaseObservationHead, schemaauthority.UpstreamObservationRestoreHead} {
		m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
		require.NoError(t, requireLeaseManifest(m, head))
		absent := m
		absent.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(r modelprofile.ImplementationReference) bool { return r.ID == UpstreamReleaseObservationContract })
		require.ErrorIs(t, requireLeaseManifest(absent, head), ErrIncompatible)
		absent = m
		absent.SchemaHeads = slices.DeleteFunc(slices.Clone(m.SchemaHeads), func(h string) bool { return h == head })
		require.ErrorIs(t, requireLeaseManifest(absent, head), ErrIncompatible)

	}
}
