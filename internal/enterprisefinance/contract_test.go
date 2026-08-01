package enterprisefinance

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBudgetIsReportingOnlyUntilSeparateSpendPolicyExists(t *testing.T) {
	budget := validBudget()
	require.NoError(t, ValidateBudget(budget))
	require.Equal(t, BudgetDefaultEffect, DefaultBudgetRuntimeEffect(budget))

	policy := SpendPolicyBinding{
		ContractVersion: ContractVersion, PolicyRef: "policy-1", OrgID: 7,
		Scope:   Scope{Kind: ScopeCostCenter, Ref: "cc-100"},
		Ceiling: Amount{ValueMinor: "100000000", Currency: "CNY", Scale: 1_000_000},
		Effect:  SpendBlock, EffectiveFrom: validTimeValue(), ApprovedBy: "finance-checker",
	}
	require.NoError(t, ValidateSpendPolicy(policy))
}

func TestRateCardSeparatesEconomicLayerAndRequiresMakerChecker(t *testing.T) {
	card := validRateCard()
	card.State = RateActive
	card.ApprovedBy = card.CreatedBy
	err := ValidateRateCard(card)
	require.ErrorIs(t, err, ErrMakerChecker)

	card.ApprovedBy = "finance-checker"
	require.NoError(t, ValidateRateCard(card))

	card.Layer = RateSupplierEffective
	err = ValidateRateCard(card)
	require.ErrorContains(t, err, "supplier_effective_evidence")
	card.SourceEvidenceRef = "supplier-statement-2026-08"
	require.NoError(t, ValidateRateCard(card))
}

func TestPricingSnapshotBindsImmutableRateIdentityWithoutFloat(t *testing.T) {
	snapshot := PricingSnapshot{
		ContractVersion: ContractVersion, RequestRef: "request-1", OrgID: 7, ProjectRef: "project-3",
		PricedAt: validTimeValue(), SnapshotHash: "sha256:pricing-snapshot",
		Bindings: []PricingBinding{{
			Meter: "input_tokens", Quantity: "9223372036854775807",
			RateCardRef: "rate-card-1", RateCardVersion: 17,
			RateComponentHash: "sha256:component", Charge: Amount{ValueMinor: "9007199254740993", Currency: "CNY", Scale: 1_000_000},
		}},
	}
	require.NoError(t, ValidatePricingSnapshot(snapshot))

	variance, err := ForecastVarianceMinor(
		Amount{ValueMinor: "9007199254740993", Currency: "CNY", Scale: 1_000_000},
		Amount{ValueMinor: "9007199254740000", Currency: "CNY", Scale: 1_000_000},
	)
	require.NoError(t, err)
	require.Equal(t, "993", variance)
}

func TestAllocationCannotCreateDebtOrMutateSettlementLedger(t *testing.T) {
	result := AllocationResult{
		ContractVersion: ContractVersion, AllocationRunRef: "run-1", RequestRef: "request-1",
		FiscalPeriodRef: "2026-08", SourceRevision: "source-rev-1", Target: Scope{Kind: ScopeCostCenter, Ref: "cc-100"},
		State: AllocationRuleBased, RuleRef: "rule-1", RuleRevision: "7",
		Amount:         Amount{ValueMinor: "1200000", Currency: "CNY", Scale: 1_000_000},
		LedgerMutation: LedgerMutationNone, CustomerDebtAction: CustomerDebtNone,
	}
	require.NoError(t, ValidateAllocation(result))

	result.LedgerMutation = "append_customer_charge"
	err := ValidateAllocation(result)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalid))
	require.ErrorContains(t, err, "allocation_financial_authority")
}

func TestUnallocatedAndConflictStatesCannotPretendToHaveATarget(t *testing.T) {
	result := AllocationResult{
		ContractVersion: ContractVersion, AllocationRunRef: "run-1", RequestRef: "request-1",
		FiscalPeriodRef: "2026-08", SourceRevision: "source-rev-1", State: AllocationUnallocated,
		Amount:         Amount{ValueMinor: "1200000", Currency: "CNY", Scale: 1_000_000},
		LedgerMutation: LedgerMutationNone, CustomerDebtAction: CustomerDebtNone,
	}
	require.NoError(t, ValidateAllocation(result))
	result.Target = Scope{Kind: ScopeCostCenter, Ref: "guessed-cost-center"}
	require.ErrorContains(t, ValidateAllocation(result), "unresolved_allocation_target")
}

func TestClosedPeriodCannotBeReopenedOrSilentlyRewritten(t *testing.T) {
	require.NoError(t, CanTransitionPeriod(PeriodOpen, PeriodPreliminary))
	require.NoError(t, CanTransitionPeriod(PeriodPreliminary, PeriodReconciled))
	require.NoError(t, CanTransitionPeriod(PeriodReconciled, PeriodClosed))
	require.ErrorIs(t, CanTransitionPeriod(PeriodClosed, PeriodOpen), ErrClosedPeriod)
	require.ErrorContains(t, CanTransitionPeriod(PeriodOpen, PeriodClosed), "period_transition")
}

func TestMoneyAndTimeFormsRejectUnsafeRepresentations(t *testing.T) {
	budget := validBudget()
	budget.Amount.ValueMinor = "1.25"
	require.ErrorContains(t, ValidateBudget(budget), "amount")
	budget = validBudget()
	budget.Amount.ValueMinor = "01"
	require.ErrorContains(t, ValidateBudget(budget), "amount")

	card := validRateCard()
	card.EffectiveFrom = time.Date(2026, 8, 1, 8, 0, 0, 123456789, time.UTC)
	require.ErrorContains(t, ValidateRateCard(card), "rate_effective_window")
}

func TestRateAndPricingIdentitiesAreOrderStableAndByteBound(t *testing.T) {
	component := validRateCard().Components[0]
	identity, err := RateComponentIdentity(component)
	require.NoError(t, err)
	require.Regexp(t, `^sha256:[a-f0-9]{64}$`, identity)
	component.Amount.ValueMinor = "2000001"
	changed, err := RateComponentIdentity(component)
	require.NoError(t, err)
	require.NotEqual(t, identity, changed)

	pricedAt := validTimeValue()
	first := PricingSnapshot{
		ContractVersion: ContractVersion, RequestRef: "request-identity", OrgID: 7,
		ProjectRef: "project-3", PricedAt: pricedAt, SnapshotHash: "pending",
		Bindings: []PricingBinding{
			{Meter: "output_tokens", Quantity: "2", RateCardRef: "card", RateCardVersion: 3, RateComponentHash: changed, Charge: Amount{ValueMinor: "4", Currency: "CNY", Scale: 1_000_000}},
			{Meter: "input_tokens", Quantity: "1", RateCardRef: "card", RateCardVersion: 3, RateComponentHash: identity, Charge: Amount{ValueMinor: "2", Currency: "CNY", Scale: 1_000_000}},
		},
	}
	firstHash, err := PricingSnapshotIdentity(first)
	require.NoError(t, err)
	first.Bindings[0], first.Bindings[1] = first.Bindings[1], first.Bindings[0]
	secondHash, err := PricingSnapshotIdentity(first)
	require.NoError(t, err)
	require.Equal(t, firstHash, secondHash)
	first.Bindings[0].Charge.ValueMinor = "3"
	thirdHash, err := PricingSnapshotIdentity(first)
	require.NoError(t, err)
	require.NotEqual(t, firstHash, thirdHash)
}

func validBudget() BudgetPlan {
	return BudgetPlan{
		ContractVersion: ContractVersion, BudgetRef: "budget-1", OrgID: 7,
		Scope: Scope{Kind: ScopeCostCenter, Ref: "cc-100"}, FiscalPeriodRef: "2026-08",
		Amount:  Amount{ValueMinor: "100000000", Currency: "CNY", Scale: 1_000_000},
		Version: 1, State: BudgetApproved, ApprovedBy: "finance-checker",
	}
}

func validRateCard() RateCard {
	return RateCard{
		ContractVersion: ContractVersion, RateCardRef: "rate-card-1", OrgID: 7,
		ProviderRef: "provider-1", ModelRef: "model-1", Layer: RateSupplierContract,
		Version: 1, State: RateDraft, EffectiveFrom: validTimeValue(), CreatedBy: "finance-maker",
		Components: []RateComponent{{
			Meter: "input_tokens", UnitQuantity: "1000000",
			Amount: Amount{ValueMinor: "2000000", Currency: "CNY", Scale: 1_000_000},
		}},
	}
}

func validTimeValue() time.Time {
	return time.Date(2026, 8, 1, 8, 0, 0, 123456000, time.UTC)
}
