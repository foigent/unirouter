package runtimegeneration

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestCommunicationManifestRequiresCurrentAndInheritedImplementations(t *testing.T) {
	m := CurrentManifest(schemaauthority.SupportedSchemaHeads())
	require.NoError(t, requireLeaseManifest(m, schemaauthority.XInsightCommunicationHead))
	refs := append(XInsightCommunicationRequirements(), XInsightCredentialRequirements()...)
	refs = append(refs, XInsightAdoptionRequirements()...)
	refs = append(refs, XInsightPolicyRequirements()...)
	refs = append(refs, ScopedContentRequirements()...)
	refs = append(refs, ContentLifecycleRequirements()...)
	for _, ref := range refs {
		missing := m
		missing.Implementations = slices.DeleteFunc(slices.Clone(m.Implementations), func(x modelprofile.ImplementationReference) bool { return x == ref })
		require.ErrorIs(t, requireLeaseManifest(missing, schemaauthority.XInsightCommunicationHead), ErrIncompatible, ref.ID)
	}
	missing := m
	missing.SchemaHeads = slices.DeleteFunc(slices.Clone(m.SchemaHeads), func(h string) bool { return h == schemaauthority.XInsightCommunicationHead })
	require.ErrorIs(t, requireLeaseManifest(missing, schemaauthority.XInsightCommunicationHead), ErrIncompatible)
}
