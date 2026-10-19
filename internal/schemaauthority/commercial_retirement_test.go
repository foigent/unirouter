package schemaauthority

import (
	"slices"
	"testing"
)

func TestCommercialRetirementIsNotARollingBridge(t *testing.T) {
	artifact, err := ExpectedLedger("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	artifact = artifact[:len(artifact)-1] // This test remains bound to the historical M126 artifact.
	prior := artifact[:len(artifact)-1]
	if prior[len(prior)-1] != XInsightCredentialExpandHead {
		t.Fatal("unexpected predecessor")
	}
	if err := VerifyRuntimeLedger(artifact, artifact); err != nil {
		t.Fatal(err)
	}
	for _, actual := range [][]string{prior, append(slices.Clone(artifact), "000127_future"), append(slices.Clone(artifact[:50]), artifact[51:]...)} {
		if err := VerifyRuntimeLedger(artifact, actual); err == nil {
			t.Fatal("accepted wrong complete ledger")
		}
	}
	if err := VerifyRuntimeLedger(prior, artifact); err == nil {
		t.Fatal("old artifact accepted M126")
	}
	if err := VerifyRuntimeLedger(prior, prior); err != nil {
		t.Fatal("historical artifact no longer verifies")
	}
}
