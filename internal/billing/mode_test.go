package billing

import (
	"strings"
	"testing"
)

func TestValidateBillingMode(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeCostObservation, BillingModePrepaidEnforced} {
		if err := ValidateBillingMode(mode); err != nil {
			t.Fatalf("expected %q to be supported: %v", mode, err)
		}
	}
	if err := ValidateBillingMode(BillingModeBudgetEnforced); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("budget mode must fail closed as unsupported, got %v", err)
	}
	if err := ValidateBillingMode("OBSERVE"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unknown mode must fail closed, got %v", err)
	}
}
