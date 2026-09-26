package sourcefacts

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"time"

	budgetassets "github.com/uniroute/unirouter/contracts/source-budgets/v1"
	"github.com/uniroute/unirouter/internal/money"
)

const (
	BudgetSnapshotVersion   = budgetassets.SchemaVersion
	BudgetDefinitionProfile = "unirouter.budget-definition.v1"
	BudgetSummaryProfile    = "unirouter.budget-period-summary.v1"
	BudgetAlertProfile      = "unirouter.budget-alert.v1"
	BudgetWriterContract    = budgetassets.WriterContract
)

func BudgetProfiles() []string {
	return []string{BudgetDefinitionProfile, BudgetSummaryProfile, BudgetAlertProfile}
}
func validBudgetProfile(profile string) bool {
	return oneOf(profile, BudgetDefinitionProfile, BudgetSummaryProfile, BudgetAlertProfile)
}

// BudgetMoney retains the authority's integer unit. Enterprise allocation
// money is not silently converted into gateway charge micros.
type BudgetMoney struct {
	ValueMinor string `json:"value_minor"`
	Currency   string `json:"currency"`
	Scale      string `json:"scale"`
}

func budgetInteger(value string, negative bool) (*big.Int, bool) {
	if len(value) == 0 || len(value) > 79 {
		return nil, false
	}
	n, ok := new(big.Int).SetString(value, 10)
	return n, ok && n.String() == value && (negative || n.Sign() >= 0) && len(new(big.Int).Abs(n).String()) <= 78
}

func (m BudgetMoney) valid(negative bool) bool {
	if len(m.Currency) != 3 || !positiveID(m.Scale) {
		return false
	}
	for _, c := range m.Currency {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	_, ok := budgetInteger(m.ValueMinor, negative)
	return ok
}

type BudgetDefinition struct {
	Origin          string       `json:"origin"`
	Revision        string       `json:"revision"`
	SettingState    string       `json:"setting_state"`
	OriginalCents   *string      `json:"original_cents,omitempty"`
	OriginalPeriod  string       `json:"original_period,omitempty"`
	PeriodKind      string       `json:"period_kind"`
	FiscalPeriodRef string       `json:"fiscal_period_ref,omitempty"`
	Amount          *BudgetMoney `json:"amount,omitempty"`
	AuthorityRef    string       `json:"authority_ref"`
	ChangedAt       string       `json:"changed_at"`
	ChangeKind      string       `json:"change_kind"`
	ApprovalState   string       `json:"approval_state,omitempty"`
	RuntimeEffect   string       `json:"runtime_effect"`
}

type BudgetComparison struct {
	State                  string       `json:"state"`
	Remaining              *BudgetMoney `json:"remaining,omitempty"`
	UtilizationNumerator   string       `json:"utilization_numerator,omitempty"`
	UtilizationDenominator string       `json:"utilization_denominator,omitempty"`
	ThresholdState         string       `json:"threshold_state,omitempty"`
}

// gatewayBudgetDefinition reads original nullable values, not the old GET's
// COALESCE result. A revision time comes from D's own metadata, never updated_at.
func gatewayBudgetDefinition(cents *int64, period *string, revision int64, changedAt time.Time, changeKind, authorityRef string) (BudgetDefinition, error) {
	d := BudgetDefinition{Origin: "gateway_project", Revision: strconv.FormatInt(revision, 10), SettingState: "unset", PeriodKind: "unknown", AuthorityRef: authorityRef, ChangedAt: changedAt.UTC().Format("2006-01-02T15:04:05.000000Z"), ChangeKind: changeKind, RuntimeEffect: "report_only"}
	if revision < 1 || changedAt.IsZero() || !oneOf(changeKind, "observed_baseline", "created", "value_changed") || !opaque(authorityRef, 256) {
		return d, errors.New("invalid original budget authority")
	}
	if period != nil {
		if oneOf(*period, "daily", "monthly", "none") {
			d.PeriodKind = *period
		} else {
			d.OriginalPeriod = *period
		}
	}
	if cents == nil {
		return d, nil
	}
	value := strconv.FormatInt(*cents, 10)
	d.OriginalCents = &value
	if *cents < 0 || *cents > math.MaxInt64/money.MicrosPerCent {
		d.SettingState = "unsupported"
		return d, nil
	}
	d.Amount = &BudgetMoney{ValueMinor: strconv.FormatInt(*cents*money.MicrosPerCent, 10), Currency: "CNY", Scale: strconv.FormatInt(money.MicrosPerCNY, 10)}
	switch {
	case period == nil || !oneOf(*period, "daily", "monthly", "none"):
		d.SettingState = "unsupported"
	case *cents == 0:
		d.SettingState = "inactive_zero"
	case *period == "none":
		d.SettingState = "inactive_period_none"
	default:
		d.SettingState = "active"
	}
	return d, nil
}

func gatewayBudgetPeriod(kind string, cutoff time.Time) (time.Time, time.Time, error) {
	t := cutoff.UTC()
	switch kind {
	case "daily":
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), nil
	case "monthly":
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, errors.New("budget period is not active")
	}
}

func compareBudget(definition BudgetDefinition, subtotal BudgetMoney, complete bool) BudgetComparison {
	result := BudgetComparison{State: "not_comparable"}
	if definition.SettingState != "active" || definition.Amount == nil {
		result.State = "inactive_budget"
		return result
	}
	if !complete {
		result.State = "coverage_gap"
		return result
	}
	budget := *definition.Amount
	if !budget.valid(false) || !subtotal.valid(false) {
		result.State = "invalid_amount"
		return result
	}
	if budget.Currency != subtotal.Currency || budget.Scale != subtotal.Scale {
		result.State = "unit_mismatch"
		return result
	}
	b, _ := new(big.Int).SetString(budget.ValueMinor, 10)
	s, _ := new(big.Int).SetString(subtotal.ValueMinor, 10)
	if b.Sign() <= 0 {
		result.State = "inactive_budget"
		return result
	}
	remaining := new(big.Int).Sub(b, s)
	result.State = "comparable"
	result.Remaining = &BudgetMoney{ValueMinor: remaining.String(), Currency: budget.Currency, Scale: budget.Scale}
	result.UtilizationNumerator, result.UtilizationDenominator = s.String(), b.String()
	result.ThresholdState = map[int]string{-1: "below", 0: "at", 1: "above"}[s.Cmp(b)]
	return result
}
