package migrate

import (
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"slices"
	"testing"
)

func TestFreshExpansionRequiresExactProgressAndSeparateM118M119Verifiers(t *testing.T) {
	artifact := []string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead, schemaauthority.StreamCounterExpandHead, schemaauthority.SourceFactsExpandHead, schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead}
	for i := 1; i < len(artifact); i++ {
		needsVerifier := i >= 5
		if err := validateFreshExpansion(artifact, artifact[:i], artifact[i], needsVerifier); err != nil {
			t.Fatalf("valid progression to %s: %v", artifact[i], err)
		}
		if err := validateFreshExpansion(artifact, artifact[:i], artifact[i], !needsVerifier); err == nil {
			t.Fatalf("accepted wrong verifier boundary at %s", artifact[i])
		}
	}
	for _, tc := range []struct {
		applied []string
		version string
	}{
		{artifact, schemaauthority.ContentCaptureExpandHead},
		{artifact[:4], schemaauthority.ContentCaptureExpandHead},
		{artifact[:5], schemaauthority.ObservationExpandHead},
		{[]string{}, schemaauthority.ContentCaptureExpandHead},
		{artifact[:6], "000120_unknown"},
	} {
		if validateFreshExpansion(artifact, tc.applied, tc.version, true) == nil {
			t.Fatalf("accepted invalid progression %+v", tc)
		}
	}
	drift := slices.Clone(artifact[:6])
	drift[0] = "000113_different"
	if validateFreshExpansion(artifact, drift, schemaauthority.ContentCaptureExpandHead, true) == nil {
		t.Fatal("accepted same-length divergent predecessor")
	}
}
