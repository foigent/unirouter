package migrate

import (
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"slices"
	"testing"
)

func TestCommercialRetirementRequiresExactPredecessorAndOwner(t *testing.T) {
	artifact, err := schemaauthority.ExpectedLedger("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	artifact = artifact[:slices.Index(artifact, schemaauthority.CommercialLicenseRetirementHead)+1] // Exact historical M126 artifact.
	prior := artifact[:len(artifact)-1]
	for _, owner := range [][2]bool{{true, false}, {false, true}} {
		if err := validateCommercialRetirement(prior, artifact, owner[0], owner[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		applied            []string
		maintenance, fresh bool
	}{
		{prior, false, false}, {nil, true, false}, {prior[:len(prior)-1], true, false},
		{append(slices.Clone(prior[:40]), prior[41:]...), true, false}, {artifact, true, false},
	} {
		if err := validateCommercialRetirement(tc.applied, artifact, tc.maintenance, tc.fresh); err == nil {
			t.Fatal("accepted unsafe retirement")
		}
	}
}
