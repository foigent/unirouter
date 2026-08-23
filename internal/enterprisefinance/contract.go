// Package enterprisefinance defines UniRouter's product-local enterprise
// budgeting, rating and allocation invariants. It does not replace the existing
// request settlement ledger, supplier invoice authority, ERP or general ledger.
package enterprisefinance

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

const (
	ContractVersion = "unirouter.enterprise-finance.v1"

	ScopeLegalEntity  = "legal_entity"
	ScopeBusinessUnit = "business_unit"
	ScopeDepartment   = "department"
	ScopeCostCenter   = "cost_center"
	ScopeProject      = "project"

	BudgetDraft    = "draft"
	BudgetApproved = "approved"
	BudgetClosed   = "closed"

	SpendWarn     = "warn"
	SpendThrottle = "throttle"
	SpendBlock    = "block"

	RateProviderList       = "provider_list"
	RateSupplierContract   = "supplier_contracted"
	RateSupplierEffective  = "supplier_billed_effective"
	RateInternalShowback   = "internal_showback"
	RateInternalChargeback = "internal_chargeback"

	RateDraft    = "draft"
	RateApproved = "approved"
	RateActive   = "active"
	RateRetired  = "retired"

	PeriodOpen        = "open"
	PeriodPreliminary = "preliminary"
	PeriodReconciled  = "reconciled"
	PeriodClosed      = "closed"

	AllocationDirect      = "direct"
	AllocationRuleBased   = "rule_based"
	AllocationUnallocated = "unallocated"
	AllocationConflict    = "conflict"

	AllocationRunCompleted = "completed"

	JournalAdjustment = "adjustment"
	JournalReversal   = "reversal"
	JournalDraft      = "draft"
	JournalApproved   = "approved"
	GLExportDisabled  = "not_configured"

	LedgerMutationNone  = "none"
	CustomerDebtNone    = "none"
	BudgetDefaultEffect = "report_only"
)

var (
	ErrInvalid            = errors.New("enterprisefinance: invalid contract")
	ErrMakerChecker       = errors.New("enterprisefinance: maker and checker must be independent")
	ErrClosedPeriod       = errors.New("enterprisefinance: closed period requires an adjustment journal")
	decimalIntegerPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)
	currencyPattern       = regexp.MustCompile(`^[A-Z]{3}$`)
	meterPattern          = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)
)

type Amount struct {
	ValueMinor string `json:"value_minor"`
	Currency   string `json:"currency"`
	Scale      int64  `json:"scale"`
}

type Scope struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type FiscalPeriod struct {
	PeriodRef string    `json:"period_ref"`
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	State     string    `json:"state"`
}

// BudgetPlan is a planning/reporting envelope. It never becomes a request
// admission rule without a separate SpendPolicyBinding.
type BudgetPlan struct {
	ContractVersion string `json:"contract_version"`
	BudgetRef       string `json:"budget_ref"`
	OrgID           int64  `json:"org_id"`
	Scope           Scope  `json:"scope"`
	FiscalPeriodRef string `json:"fiscal_period_ref"`
	Amount          Amount `json:"amount"`
	Version         int64  `json:"version"`
	State           string `json:"state"`
	ApprovedBy      string `json:"approved_by,omitempty"`
}

type SpendPolicyBinding struct {
	ContractVersion string     `json:"contract_version"`
	PolicyRef       string     `json:"policy_ref"`
	OrgID           int64      `json:"org_id"`
	Scope           Scope      `json:"scope"`
	Ceiling         Amount     `json:"ceiling"`
	Effect          string     `json:"effect"`
	EffectiveFrom   time.Time  `json:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to,omitempty"`
	ApprovedBy      string     `json:"approved_by"`
}

type RateComponent struct {
	Meter        string `json:"meter"`
	UnitQuantity string `json:"unit_quantity"`
	Amount       Amount `json:"amount"`
}

// DailyPriceBand is one non-cross-midnight UTC segment in a complete weekly
// schedule. Minutes use the half-open interval [start_minute_utc,
// end_minute_utc). An empty weekdays_utc applies the band to every UTC day;
// otherwise Sunday=0 through Saturday=6. A scheduled Rate Card must cover
// 00:00-24:00 exactly once on each applicable UTC weekday.
type DailyPriceBand struct {
	BandRef        string          `json:"band_ref"`
	WeekdaysUTC    []int           `json:"weekdays_utc,omitempty"`
	StartMinuteUTC int             `json:"start_minute_utc"`
	EndMinuteUTC   int             `json:"end_minute_utc"`
	Components     []RateComponent `json:"components"`
}

// RateCard separates economic layers so a supplier list price cannot silently
// become a contracted rate, internal showback rate or customer charge.
type RateCard struct {
	ContractVersion   string           `json:"contract_version"`
	RateCardRef       string           `json:"rate_card_ref"`
	OrgID             int64            `json:"org_id"`
	ProviderRef       string           `json:"provider_ref"`
	ModelRef          string           `json:"model_ref"`
	Layer             string           `json:"layer"`
	Version           int64            `json:"version"`
	State             string           `json:"state"`
	EffectiveFrom     time.Time        `json:"effective_from"`
	EffectiveTo       *time.Time       `json:"effective_to,omitempty"`
	Components        []RateComponent  `json:"components"`
	DailyPriceBands   []DailyPriceBand `json:"daily_price_bands,omitempty"`
	SourceEvidenceRef string           `json:"source_evidence_ref,omitempty"`
	CreatedBy         string           `json:"created_by"`
	ApprovedBy        string           `json:"approved_by,omitempty"`
}

type PricingBinding struct {
	Meter             string `json:"meter"`
	Quantity          string `json:"quantity"`
	RateCardRef       string `json:"rate_card_ref"`
	RateCardVersion   int64  `json:"rate_card_version"`
	RateComponentHash string `json:"rate_component_hash"`
	Charge            Amount `json:"charge"`
}

// PricingSnapshot is immutable request-time evidence. Later RateCard changes
// create a new snapshot and never rewrite a settled request.
type PricingSnapshot struct {
	ContractVersion string           `json:"contract_version"`
	RequestRef      string           `json:"request_ref"`
	OrgID           int64            `json:"org_id"`
	ProjectRef      string           `json:"project_ref"`
	PricedAt        time.Time        `json:"priced_at"`
	Bindings        []PricingBinding `json:"bindings"`
	SnapshotHash    string           `json:"snapshot_hash"`
}

// AllocationResult changes reporting ownership only. It does not create
// customer debt or mutate the request settlement ledger.
type AllocationResult struct {
	ContractVersion    string `json:"contract_version"`
	AllocationRunRef   string `json:"allocation_run_ref"`
	RequestRef         string `json:"request_ref"`
	FiscalPeriodRef    string `json:"fiscal_period_ref"`
	SourceRevision     string `json:"source_revision"`
	Target             Scope  `json:"target"`
	State              string `json:"state"`
	RuleRef            string `json:"rule_ref,omitempty"`
	RuleRevision       string `json:"rule_revision,omitempty"`
	Amount             Amount `json:"amount"`
	LedgerMutation     string `json:"ledger_mutation"`
	CustomerDebtAction string `json:"customer_debt_action"`
}

// AllocationRunRequest binds a complete reporting projection to exact Source
// and rule-set revisions. Superseding a prior run is explicit; a run never
// rewrites request settlement or creates customer debt.
type AllocationRunRequest struct {
	ContractVersion  string             `json:"contract_version"`
	AllocationRunRef string             `json:"allocation_run_ref"`
	OrgID            int64              `json:"org_id"`
	FiscalPeriodRef  string             `json:"fiscal_period_ref"`
	SourceRevision   string             `json:"source_revision"`
	RuleSetRef       string             `json:"rule_set_ref"`
	RuleSetRevision  string             `json:"rule_set_revision"`
	SupersedesRunRef string             `json:"supersedes_run_ref,omitempty"`
	CreatedBy        string             `json:"created_by"`
	Results          []AllocationResult `json:"results"`
}

// AdjustmentJournal is a product-local reporting adjustment. It is append-only
// and intentionally has no customer-ledger or general-ledger authority.
type AdjustmentJournal struct {
	ContractVersion        string `json:"contract_version"`
	JournalRef             string `json:"journal_ref"`
	OrgID                  int64  `json:"org_id"`
	Version                int64  `json:"version"`
	FiscalPeriodRef        string `json:"fiscal_period_ref"`
	RequestRef             string `json:"request_ref"`
	EntryType              string `json:"entry_type"`
	ReversalOfJournalRef   string `json:"reversal_of_journal_ref,omitempty"`
	Target                 Scope  `json:"target"`
	Amount                 Amount `json:"amount"`
	Reason                 string `json:"reason"`
	SourceAllocationRunRef string `json:"source_allocation_run_ref"`
	State                  string `json:"state"`
	LedgerMutation         string `json:"ledger_mutation"`
	CustomerDebtAction     string `json:"customer_debt_action"`
	GLExportState          string `json:"gl_export_state"`
	CreatedBy              string `json:"created_by"`
	ApprovedBy             string `json:"approved_by,omitempty"`
}

func ValidateBudget(plan BudgetPlan) error {
	if plan.ContractVersion != ContractVersion || plan.OrgID <= 0 || blank(plan.BudgetRef) || blank(plan.FiscalPeriodRef) || plan.Version <= 0 {
		return invalid("budget_identity")
	}
	if err := validateScope(plan.Scope); err != nil {
		return err
	}
	if err := validateNonNegativeAmount(plan.Amount); err != nil {
		return err
	}
	switch plan.State {
	case BudgetDraft:
		if !blank(plan.ApprovedBy) {
			return invalid("draft_budget_approval")
		}
	case BudgetApproved, BudgetClosed:
		if blank(plan.ApprovedBy) {
			return invalid("budget_approval")
		}
	default:
		return invalid("budget_state")
	}
	return nil
}

// DefaultBudgetRuntimeEffect makes the separation explicit for callers and UI
// adapters. A budget never blocks requests by itself.
func DefaultBudgetRuntimeEffect(BudgetPlan) string { return BudgetDefaultEffect }

func ValidateSpendPolicy(policy SpendPolicyBinding) error {
	if policy.ContractVersion != ContractVersion || policy.OrgID <= 0 || blank(policy.PolicyRef) || blank(policy.ApprovedBy) {
		return invalid("spend_policy_identity")
	}
	if err := validateScope(policy.Scope); err != nil {
		return err
	}
	if err := validateNonNegativeAmount(policy.Ceiling); err != nil {
		return err
	}
	if !validTime(policy.EffectiveFrom) || (policy.EffectiveTo != nil && (!validTime(*policy.EffectiveTo) || !policy.EffectiveTo.After(policy.EffectiveFrom))) {
		return invalid("spend_policy_effective_window")
	}
	switch policy.Effect {
	case SpendWarn, SpendThrottle, SpendBlock:
		return nil
	default:
		return invalid("spend_policy_effect")
	}
}

func ValidateRateCard(card RateCard) error {
	if card.ContractVersion != ContractVersion || card.OrgID <= 0 || blank(card.RateCardRef) || blank(card.ProviderRef) || blank(card.ModelRef) || card.Version <= 0 || blank(card.CreatedBy) {
		return invalid("rate_card_identity")
	}
	switch card.Layer {
	case RateProviderList, RateSupplierContract, RateSupplierEffective, RateInternalShowback, RateInternalChargeback:
	default:
		return invalid("rate_layer")
	}
	switch card.State {
	case RateDraft, RateApproved, RateActive, RateRetired:
	default:
		return invalid("rate_state")
	}
	if !validTime(card.EffectiveFrom) || (card.EffectiveTo != nil && (!validTime(*card.EffectiveTo) || !card.EffectiveTo.After(card.EffectiveFrom))) {
		return invalid("rate_effective_window")
	}
	if len(card.Components) == 0 {
		return invalid("rate_components")
	}
	seen := make(map[string]struct{}, len(card.Components))
	for _, component := range card.Components {
		if !meterPattern.MatchString(component.Meter) || !isPositiveInteger(component.UnitQuantity) {
			return invalid("rate_component")
		}
		if err := validateNonNegativeAmount(component.Amount); err != nil {
			return err
		}
		if _, exists := seen[component.Meter]; exists {
			return invalid("duplicate_rate_meter")
		}
		seen[component.Meter] = struct{}{}
	}
	if err := ValidateDailyPriceBands(card.DailyPriceBands); err != nil {
		return err
	}
	if card.State != RateDraft {
		if blank(card.ApprovedBy) {
			return invalid("rate_approval")
		}
		if strings.TrimSpace(card.CreatedBy) == strings.TrimSpace(card.ApprovedBy) {
			return ErrMakerChecker
		}
	}
	if card.Layer == RateSupplierEffective && blank(card.SourceEvidenceRef) {
		return invalid("supplier_effective_evidence")
	}
	return nil
}

func ValidatePricingSnapshot(snapshot PricingSnapshot) error {
	if snapshot.ContractVersion != ContractVersion || snapshot.OrgID <= 0 || blank(snapshot.RequestRef) || blank(snapshot.ProjectRef) || blank(snapshot.SnapshotHash) || !validTime(snapshot.PricedAt) {
		return invalid("pricing_snapshot_identity")
	}
	if len(snapshot.Bindings) == 0 {
		return invalid("pricing_bindings")
	}
	for _, binding := range snapshot.Bindings {
		if !meterPattern.MatchString(binding.Meter) || !isNonNegativeInteger(binding.Quantity) || blank(binding.RateCardRef) || binding.RateCardVersion <= 0 || blank(binding.RateComponentHash) {
			return invalid("pricing_binding")
		}
		if err := validateNonNegativeAmount(binding.Charge); err != nil {
			return err
		}
	}
	return nil
}

func ValidateAllocation(result AllocationResult) error {
	if result.ContractVersion != ContractVersion || blank(result.AllocationRunRef) || blank(result.RequestRef) || blank(result.FiscalPeriodRef) || blank(result.SourceRevision) {
		return invalid("allocation_identity")
	}
	if err := validateNonNegativeAmount(result.Amount); err != nil {
		return err
	}
	if result.LedgerMutation != LedgerMutationNone || result.CustomerDebtAction != CustomerDebtNone {
		return invalid("allocation_financial_authority")
	}
	switch result.State {
	case AllocationDirect:
		if !blank(result.RuleRef) || !blank(result.RuleRevision) {
			return invalid("direct_allocation_rule")
		}
		return validateScope(result.Target)
	case AllocationRuleBased:
		if blank(result.RuleRef) || blank(result.RuleRevision) {
			return invalid("allocation_rule")
		}
		return validateScope(result.Target)
	case AllocationUnallocated, AllocationConflict:
		if !blank(result.Target.Kind) || !blank(result.Target.Ref) || !blank(result.RuleRef) || !blank(result.RuleRevision) {
			return invalid("unresolved_allocation_target")
		}
		return nil
	default:
		return invalid("allocation_state")
	}
}

func ValidateAllocationRun(request AllocationRunRequest) error {
	if request.ContractVersion != ContractVersion || request.OrgID <= 0 || blank(request.AllocationRunRef) ||
		blank(request.FiscalPeriodRef) || blank(request.SourceRevision) || blank(request.RuleSetRef) ||
		blank(request.RuleSetRevision) || blank(request.CreatedBy) || len(request.Results) == 0 {
		return invalid("allocation_run")
	}
	seen := make(map[string]struct{}, len(request.Results))
	var currency string
	var scale int64
	for _, result := range request.Results {
		if err := ValidateAllocation(result); err != nil {
			return err
		}
		if strings.TrimSpace(result.AllocationRunRef) != strings.TrimSpace(request.AllocationRunRef) ||
			strings.TrimSpace(result.FiscalPeriodRef) != strings.TrimSpace(request.FiscalPeriodRef) ||
			strings.TrimSpace(result.SourceRevision) != strings.TrimSpace(request.SourceRevision) {
			return invalid("allocation_run_binding")
		}
		requestRef := strings.TrimSpace(result.RequestRef)
		if _, exists := seen[requestRef]; exists {
			return invalid("duplicate_allocation_request")
		}
		seen[requestRef] = struct{}{}
		if currency == "" {
			currency, scale = result.Amount.Currency, result.Amount.Scale
		} else if result.Amount.Currency != currency || result.Amount.Scale != scale {
			return invalid("allocation_amount_unit")
		}
	}
	return nil
}

func ValidateAdjustmentJournal(journal AdjustmentJournal) error {
	if journal.ContractVersion != ContractVersion || journal.OrgID <= 0 || blank(journal.JournalRef) ||
		journal.Version <= 0 || blank(journal.FiscalPeriodRef) || blank(journal.RequestRef) ||
		blank(journal.Reason) || blank(journal.SourceAllocationRunRef) || blank(journal.CreatedBy) {
		return invalid("adjustment_journal")
	}
	if err := validateScope(journal.Target); err != nil {
		return err
	}
	if err := validateNonNegativeAmount(journal.Amount); err != nil {
		return err
	}
	if journal.LedgerMutation != LedgerMutationNone || journal.CustomerDebtAction != CustomerDebtNone || journal.GLExportState != GLExportDisabled {
		return invalid("adjustment_authority")
	}
	switch journal.EntryType {
	case JournalAdjustment:
		if !blank(journal.ReversalOfJournalRef) {
			return invalid("adjustment_reversal_reference")
		}
	case JournalReversal:
		if blank(journal.ReversalOfJournalRef) {
			return invalid("reversal_reference")
		}
	default:
		return invalid("adjustment_entry_type")
	}
	switch journal.State {
	case JournalDraft:
		if !blank(journal.ApprovedBy) {
			return invalid("draft_adjustment_approval")
		}
	case JournalApproved:
		if blank(journal.ApprovedBy) || strings.TrimSpace(journal.ApprovedBy) == strings.TrimSpace(journal.CreatedBy) {
			return ErrMakerChecker
		}
	default:
		return invalid("adjustment_state")
	}
	return nil
}

func ValidateFiscalPeriod(period FiscalPeriod) error {
	if blank(period.PeriodRef) || !validTime(period.StartAt) || !validTime(period.EndAt) || !period.EndAt.After(period.StartAt) {
		return invalid("fiscal_period")
	}
	switch period.State {
	case PeriodOpen, PeriodPreliminary, PeriodReconciled, PeriodClosed:
		return nil
	default:
		return invalid("period_state")
	}
}

func CanTransitionPeriod(from, to string) error {
	if from == to {
		return nil
	}
	if from == PeriodClosed {
		return ErrClosedPeriod
	}
	allowed := map[string]map[string]bool{
		PeriodOpen:        {PeriodPreliminary: true},
		PeriodPreliminary: {PeriodOpen: true, PeriodReconciled: true},
		PeriodReconciled:  {PeriodPreliminary: true, PeriodClosed: true},
	}
	if !allowed[from][to] {
		return invalid("period_transition")
	}
	return nil
}

// ForecastVarianceMinor returns forecast minus budget using decimal integer
// arithmetic only; it never converts enterprise money to float64.
func ForecastVarianceMinor(forecast, budget Amount) (string, error) {
	if err := validateNonNegativeAmount(forecast); err != nil {
		return "", err
	}
	if err := validateNonNegativeAmount(budget); err != nil {
		return "", err
	}
	if forecast.Currency != budget.Currency || forecast.Scale != budget.Scale {
		return "", invalid("amount_unit_mismatch")
	}
	f, _ := new(big.Int).SetString(forecast.ValueMinor, 10)
	b, _ := new(big.Int).SetString(budget.ValueMinor, 10)
	return new(big.Int).Sub(f, b).String(), nil
}

func validateScope(scope Scope) error {
	if blank(scope.Ref) {
		return invalid("scope_ref")
	}
	switch scope.Kind {
	case ScopeLegalEntity, ScopeBusinessUnit, ScopeDepartment, ScopeCostCenter, ScopeProject:
		return nil
	default:
		return invalid("scope_kind")
	}
}

func validateNonNegativeAmount(amount Amount) error {
	if !currencyPattern.MatchString(amount.Currency) || amount.Scale <= 0 || amount.Scale > 1_000_000_000 || !isNonNegativeInteger(amount.ValueMinor) {
		return invalid("amount")
	}
	return nil
}

func isPositiveInteger(value string) bool {
	if !decimalIntegerPattern.MatchString(value) || strings.HasPrefix(value, "-") {
		return false
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	return ok && parsed.Sign() > 0
}

func isNonNegativeInteger(value string) bool {
	if !decimalIntegerPattern.MatchString(value) || strings.HasPrefix(value, "-") {
		return false
	}
	_, ok := new(big.Int).SetString(value, 10)
	return ok
}

func validTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1_000 == 0
}

func blank(value string) bool { return strings.TrimSpace(value) == "" }

func invalid(field string) error { return fmt.Errorf("%w: %s", ErrInvalid, field) }
