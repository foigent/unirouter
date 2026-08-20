package modeldeployment

import (
	"errors"
	"testing"
)

func TestNormalizeIdentityPreservesExactCaseAndRejectsUnsafeValues(t *testing.T) {
	value, err := NormalizeIdentity("  DeepSeek-V4-Flash  ", 100)
	if err != nil || value != "DeepSeek-V4-Flash" {
		t.Fatalf("NormalizeIdentity = %q, %v", value, err)
	}
	for _, input := range []string{"", "line\nbreak", string([]byte{0xff}), "abcd"} {
		limit := 100
		if input == "abcd" {
			limit = 3
		}
		if _, err := NormalizeIdentity(input, limit); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("NormalizeIdentity(%q, %d) error = %v", input, limit, err)
		}
	}
}

func TestOperationCapabilityAndEvidenceStatesAreExplicit(t *testing.T) {
	if operation, err := NormalizeOperationID(" openai.chat_completions.v1 "); err != nil || operation != "openai.chat_completions.v1" {
		t.Fatalf("NormalizeOperationID = %q, %v", operation, err)
	}
	if !ValidSupportState(SupportEnabled) || ValidSupportState("declared") {
		t.Fatal("support state contract drifted")
	}
	if !ValidEvidenceState(EvidenceProductionApproved) || ValidEvidenceState("unsupported") {
		t.Fatal("evidence state contract drifted")
	}
}
