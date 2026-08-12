package billing

import "fmt"

// BillingMode is snapshotted onto every reservation. Runtime configuration
// changes therefore affect only requests that have not started yet.
type BillingMode string

const (
	BillingModeCostObservation BillingMode = "cost_observation"
	BillingModeBudgetEnforced  BillingMode = "budget_enforced"
	BillingModePrepaidEnforced BillingMode = "prepaid_enforced"
)

// ValidateBillingMode rejects unknown modes and modes whose complete runtime
// admission contract is not implemented. budget_enforced remains a reserved,
// explicit value so it cannot silently degrade to observation or prepaid.
func ValidateBillingMode(mode BillingMode) error {
	switch mode {
	case BillingModeCostObservation, BillingModePrepaidEnforced:
		return nil
	case BillingModeBudgetEnforced:
		return fmt.Errorf("billing mode %q is recognized but unsupported", mode)
	default:
		return fmt.Errorf("billing mode %q is invalid", mode)
	}
}
