package enterprisefinance

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
)

const (
	EvidenceExact      = "exact"
	EvidencePartial    = "partial"
	EvidenceNoRun      = "no_allocation_run"
	EvidenceNotModeled = "not_modeled"
	EvidenceUnitGap    = "unit_mismatch"
)

type AmountEvidence struct {
	Status string  `json:"status"`
	Amount *Amount `json:"amount,omitempty"`
}

type BudgetPosition struct {
	BudgetRef        string         `json:"budget_ref"`
	BudgetVersion    int64          `json:"budget_version"`
	Scope            Scope          `json:"scope"`
	FiscalPeriodRef  string         `json:"fiscal_period_ref"`
	PeriodState      string         `json:"period_state"`
	Budget           Amount         `json:"budget"`
	Actual           AmountEvidence `json:"actual"`
	Committed        AmountEvidence `json:"committed"`
	Forecast         AmountEvidence `json:"forecast"`
	VarianceMinor    *string        `json:"variance_minor,omitempty"`
	CoverageState    string         `json:"coverage_state"`
	AllocationRunRef string         `json:"allocation_run_ref,omitempty"`
	RuntimeEffect    string         `json:"runtime_effect"`
}

// GetBudgetPositions is an evidence-status read model. It reports only exact
// resolved allocation as actual. UniRouter has no enterprise commitment or
// forecasting authority yet, so those dimensions are explicit gaps rather
// than zero-valued estimates.
func (s *PostgresStore) GetBudgetPositions(ctx context.Context, orgID int64) ([]BudgetPosition, error) {
	budgets, err := s.ListLatestBudgets(ctx, orgID)
	if err != nil {
		return nil, err
	}
	positions := make([]BudgetPosition, 0, len(budgets))
	for _, budget := range budgets {
		position := BudgetPosition{
			BudgetRef: budget.Plan.BudgetRef, BudgetVersion: budget.Plan.Version,
			Scope: budget.Plan.Scope, FiscalPeriodRef: budget.Plan.FiscalPeriodRef,
			Budget: budget.Plan.Amount, RuntimeEffect: budget.RuntimeEffect,
			Committed: AmountEvidence{Status: EvidenceNotModeled},
			Forecast:  AmountEvidence{Status: EvidenceNotModeled},
		}
		if err := s.db.QueryRow(ctx, `
			SELECT state FROM enterprise_finance_fiscal_periods
			WHERE org_id=$1 AND period_ref=$2`, orgID, budget.Plan.FiscalPeriodRef,
		).Scan(&position.PeriodState); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, fmt.Errorf("load budget-position period: %w", err)
		}
		var runID, sourceCount, resultCount, unallocatedCount, conflictCount int64
		var runCurrency, runSourceTotal, runAllocatedTotal string
		var runScale int64
		err := s.db.QueryRow(ctx, `
			SELECT id,allocation_run_ref,source_record_count,result_count,unallocated_count,conflict_count,
			       currency,amount_scale,source_total_minor::text,allocated_minor::text
			FROM enterprise_finance_allocation_runs
			WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY created_at DESC,id DESC LIMIT 1`,
			orgID, budget.Plan.FiscalPeriodRef,
		).Scan(
			&runID, &position.AllocationRunRef, &sourceCount, &resultCount, &unallocatedCount,
			&conflictCount, &runCurrency, &runScale, &runSourceTotal, &runAllocatedTotal,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			position.Actual = AmountEvidence{Status: EvidenceNoRun}
			position.CoverageState = EvidenceNoRun
			positions = append(positions, position)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load budget-position allocation run: %w", err)
		}
		coverage := EvidencePartial
		if sourceCount == resultCount && unallocatedCount == 0 && conflictCount == 0 && runSourceTotal == runAllocatedTotal {
			coverage = EvidenceExact
		}
		position.CoverageState = coverage

		var matchingCount int64
		var actualMinor string
		var minCurrency, maxCurrency string
		var minScale, maxScale int64
		if err := s.db.QueryRow(ctx, `
			SELECT COUNT(*),COALESCE(SUM(amount_minor),0)::text,
			       COALESCE(MIN(currency),''),COALESCE(MAX(currency),''),
			       COALESCE(MIN(amount_scale),0),COALESCE(MAX(amount_scale),0)
			FROM enterprise_finance_allocation_results
			WHERE allocation_run_id=$1 AND org_id=$2 AND state IN ('direct','rule_based')
			  AND target_kind=$3 AND target_ref=$4`,
			runID, orgID, budget.Plan.Scope.Kind, budget.Plan.Scope.Ref,
		).Scan(&matchingCount, &actualMinor, &minCurrency, &maxCurrency, &minScale, &maxScale); err != nil {
			return nil, fmt.Errorf("load budget-position actual: %w", err)
		}
		actual := Amount{ValueMinor: actualMinor, Currency: budget.Plan.Amount.Currency, Scale: budget.Plan.Amount.Scale}
		if matchingCount > 0 {
			if minCurrency != maxCurrency || minScale != maxScale ||
				minCurrency != budget.Plan.Amount.Currency || minScale != budget.Plan.Amount.Scale ||
				runCurrency != budget.Plan.Amount.Currency || runScale != budget.Plan.Amount.Scale {
				position.Actual = AmountEvidence{Status: EvidenceUnitGap}
				position.CoverageState = EvidenceUnitGap
				positions = append(positions, position)
				continue
			}
			actual.Currency, actual.Scale = minCurrency, minScale
		}
		position.Actual = AmountEvidence{Status: coverage, Amount: &actual}
		budgetValue, budgetOK := new(big.Int).SetString(budget.Plan.Amount.ValueMinor, 10)
		actualValue, actualOK := new(big.Int).SetString(actual.ValueMinor, 10)
		if budgetOK && actualOK {
			variance := new(big.Int).Sub(budgetValue, actualValue).String()
			position.VarianceMinor = &variance
		}
		positions = append(positions, position)
	}
	return positions, nil
}
