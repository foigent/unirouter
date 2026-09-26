package sourcefacts

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

var errBudgetSourceData = errors.New("original budget source data is invalid")

const gatewayBudgetPrefix = "gateway-project:"
const enterpriseBudgetPrefix = "enterprise-budget:"

type BudgetCatalogEntry struct {
	BudgetRef          string `json:"budget_ref"`
	Origin             string `json:"origin"`
	DefinitionRevision string `json:"definition_revision"`
	BindingRevision    string `json:"binding_revision,omitempty"`
}

type budgetRecord struct {
	entry                  BudgetCatalogEntry
	definition             BudgetDefinition
	enterpriseScope        string
	periodStart, periodEnd time.Time
	periodState            string
	bindingRevision        int64
}

// Candidate budgets are derived from the authorized project's explicit
// bindings before inspecting versions; no organization-wide position is read.
const budgetCatalogSQL = `WITH candidates AS (
 SELECT DISTINCT v.budget_ref FROM execution_source_budget_bindings b
 JOIN enterprise_finance_budget_versions v ON v.org_id=b.org_id AND v.scope_kind='project' AND v.scope_ref=b.enterprise_project_scope_ref
 WHERE b.org_id=$1 AND b.project_id=$2 AND b.status='active'
), visible AS (
 SELECT 'gateway-project:'||$2::text AS ref,'gateway_project'::text AS origin,MAX(revision) AS revision,0::bigint AS binding_revision
 FROM execution_source_budget_definitions WHERE org_id=$1 AND project_id=$2 HAVING count(*)>0
 UNION ALL
 SELECT 'enterprise-budget:'||v.budget_ref,'enterprise_project',v.version,b.revision FROM candidates c
 JOIN LATERAL (SELECT * FROM enterprise_finance_budget_versions WHERE org_id=$1 AND budget_ref=c.budget_ref ORDER BY version DESC LIMIT 1) v ON true
 JOIN execution_source_budget_bindings b ON b.org_id=v.org_id AND v.scope_kind='project' AND b.enterprise_project_scope_ref=v.scope_ref
 WHERE b.project_id=$2 AND b.status='active'
)
SELECT ref,origin,revision,binding_revision FROM visible WHERE ref COLLATE "C">$3 COLLATE "C" ORDER BY ref COLLATE "C" LIMIT $4`

func readBudgetCatalogTx(ctx context.Context, tx pgx.Tx, a budgetAccess, after string, limit int) ([]BudgetCatalogEntry, error) {
	rows, err := tx.Query(ctx, budgetCatalogSQL, a.org, a.project, after, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []BudgetCatalogEntry{}
	for rows.Next() {
		var item BudgetCatalogEntry
		var rev, binding int64
		if err = rows.Scan(&item.BudgetRef, &item.Origin, &rev, &binding); err != nil {
			return nil, err
		}
		item.DefinitionRevision = executioncontext.ID(rev)
		if binding > 0 {
			item.BindingRevision = executioncontext.ID(binding)
		}
		entries = append(entries, item)
	}
	return entries, rows.Err()
}

func readBudgetRecordTx(ctx context.Context, tx pgx.Tx, a budgetAccess, ref string) (budgetRecord, error) {
	var result budgetRecord
	result.entry.BudgetRef = ref
	if ref == gatewayBudgetPrefix+strconv.FormatInt(a.project, 10) {
		var cents *int64
		var period *string
		var rev int64
		var changed time.Time
		var kind string
		err := tx.QueryRow(ctx, `SELECT budget_cents,budget_period,revision,changed_at,change_kind FROM execution_source_budget_definitions
 WHERE org_id=$1 AND project_id=$2 ORDER BY revision DESC LIMIT 1`, a.org, a.project).Scan(&cents, &period, &rev, &changed, &kind)
		if err != nil {
			return result, err
		}
		result.definition, err = gatewayBudgetDefinition(cents, period, rev, changed, kind, "projects:"+strconv.FormatInt(a.project, 10))
		result.entry.Origin = "gateway_project"
		result.entry.DefinitionRevision = executioncontext.ID(rev)
		return result, err
	}
	if !strings.HasPrefix(ref, enterpriseBudgetPrefix) || len(ref) <= len(enterpriseBudgetPrefix) {
		return result, pgx.ErrNoRows
	}
	rawRef := strings.TrimPrefix(ref, enterpriseBudgetPrefix)
	var id, rev, scale int64
	var amount, currency, state, effect string
	var created time.Time
	err := tx.QueryRow(ctx, `SELECT v.id,v.version,v.amount_minor::text,v.currency,v.amount_scale,v.state,v.runtime_effect,v.created_at,
 v.fiscal_period_ref,b.enterprise_project_scope_ref,b.revision,f.start_at,f.end_at,f.state
 FROM execution_source_budget_bindings b JOIN enterprise_finance_budget_versions v
 ON v.org_id=b.org_id AND v.scope_kind='project' AND v.scope_ref=b.enterprise_project_scope_ref
 JOIN enterprise_finance_fiscal_periods f ON f.org_id=v.org_id AND f.period_ref=v.fiscal_period_ref
 WHERE b.org_id=$1 AND b.project_id=$2 AND b.status='active' AND v.budget_ref=$3
 AND v.version=(SELECT MAX(version) FROM enterprise_finance_budget_versions WHERE org_id=$1 AND budget_ref=$3)`, a.org, a.project, rawRef).Scan(
		&id, &rev, &amount, &currency, &scale, &state, &effect, &created, &result.definition.FiscalPeriodRef, &result.enterpriseScope, &result.bindingRevision, &result.periodStart, &result.periodEnd, &result.periodState)
	if err != nil {
		return result, err
	}
	result.definition.Origin = "enterprise_project"
	result.definition.Revision = executioncontext.ID(rev)
	result.definition.PeriodKind = "fiscal_period"
	result.definition.Amount = &BudgetMoney{amount, strings.TrimSpace(currency), strconv.FormatInt(scale, 10)}
	if !result.definition.Amount.valid(false) || effect != "report_only" {
		return result, errBudgetSourceData
	}
	result.definition.SettingState = "active"
	if state != "approved" {
		result.definition.SettingState = "not_approved"
	} else if amount == "0" {
		result.definition.SettingState = "inactive_zero"
	}
	result.definition.ApprovalState = state
	result.definition.RuntimeEffect = effect
	result.definition.ChangedAt = executioncontext.Time(created)
	result.definition.ChangeKind = "authority_version"
	result.definition.AuthorityRef = "enterprise_finance_budget_versions:" + executioncontext.ID(id)
	result.entry.Origin = "enterprise_project"
	result.entry.DefinitionRevision = executioncontext.ID(rev)
	result.entry.BindingRevision = executioncontext.ID(result.bindingRevision)
	return result, nil
}

func enterpriseBudgetSummaryTx(ctx context.Context, tx pgx.Tx, a budgetAccess, record budgetRecord, cutoff time.Time) (BudgetPeriodSummary, error) {
	d := record.definition
	result := BudgetPeriodSummary{PeriodState: record.periodState, PeriodStart: executioncontext.Time(record.periodStart), PeriodEnd: executioncontext.Time(record.periodEnd), AmountCutoff: executioncontext.Time(cutoff), Authority: "enterprise_finance_allocation_results.project_slice", Coverage: BudgetCoverage{State: "no_allocation_run", Reasons: []string{}, WriterContract: "enterprise_finance.report_only", MaterializationPending: "0", MaterializedAwaitingAck: "0"}, Unresolved: BudgetUnresolved{State: "not_modeled", Scope: "enterprise_project_allocation", OpenReservations: "0", FinancialHolds: "0"}, Excluded: []string{"gateway_journal", "other_allocation_targets", "organization_totals"}, CommittedState: "not_modeled", ForecastState: "not_modeled"}
	// Only the original coverage conclusion leaves this query. Organization
	// totals and source/result counts are never selected into a response object.
	var runID, scale int64
	var currency, coverage string
	err := tx.QueryRow(ctx, `SELECT id,allocation_run_ref,currency,amount_scale,
 CASE WHEN source_record_count=result_count AND unallocated_count=0 AND conflict_count=0 AND source_total_minor=allocated_minor THEN 'exact' ELSE 'partial' END
 FROM enterprise_finance_allocation_runs WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, a.org, d.FiscalPeriodRef).Scan(&runID, &result.AllocationRunRef, &currency, &scale, &coverage)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Coverage.Reasons = []string{"no_allocation_run"}
		result.Comparison = compareBudget(d, BudgetMoney{}, false)
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var count, minScale, maxScale int64
	var actual, minCurrency, maxCurrency string
	err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(SUM(amount_minor),0)::text,COALESCE(MIN(currency),''),COALESCE(MAX(currency),''),COALESCE(MIN(amount_scale),0),COALESCE(MAX(amount_scale),0)
 FROM enterprise_finance_allocation_results WHERE org_id=$1 AND allocation_run_id=$2 AND state IN ('direct','rule_based') AND target_kind='project' AND target_ref=$3`, a.org, runID, record.enterpriseScope).Scan(&count, &actual, &minCurrency, &maxCurrency, &minScale, &maxScale)
	if err != nil {
		return result, err
	}
	// An empty project slice is still denominated in the allocation run's
	// unit. Only the per-row checks require at least one project result.
	if currency != d.Amount.Currency || strconv.FormatInt(scale, 10) != d.Amount.Scale ||
		(count > 0 && (minCurrency != maxCurrency || minScale != maxScale || minCurrency != d.Amount.Currency || strconv.FormatInt(minScale, 10) != d.Amount.Scale)) {
		result.Coverage.State = "unit_mismatch"
		result.Coverage.Reasons = []string{"unit_mismatch"}
		result.Comparison = BudgetComparison{State: "unit_mismatch"}
		return result, nil
	}
	result.KnownSubtotal = &BudgetMoney{actual, d.Amount.Currency, d.Amount.Scale}
	if !result.KnownSubtotal.valid(false) {
		return result, errBudgetSourceData
	}
	result.Coverage.State = coverage
	if coverage != "exact" {
		result.Coverage.Reasons = []string{"allocation_partial"}
	}
	result.Comparison = compareBudget(d, *result.KnownSubtotal, coverage == "exact")
	return result, nil
}
