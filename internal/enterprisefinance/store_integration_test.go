//go:build integration

package enterprisefinance

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseFinanceBudgetingIsTenantScopedExactAndReportOnly(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgA := insertFinanceTestOrg(t, ctx, pool, "finance-budget-a")
	orgB := insertFinanceTestOrg(t, ctx, pool, "finance-budget-b")
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros,currency)
		VALUES ($1,900000000,0,0,100000000,'CNY')`, orgA)
	require.NoError(t, err)
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	period, err := store.CreateFiscalPeriod(ctx, orgA, FiscalPeriod{
		PeriodRef: "FY26-P08", StartAt: periodStart, EndAt: periodEnd, State: PeriodOpen,
	}, "user:101")
	require.NoError(t, err)
	require.Equal(t, int64(1), period.Version)
	_, err = store.CreateFiscalPeriod(ctx, orgA, FiscalPeriod{
		PeriodRef: "overlap", StartAt: periodStart.Add(24 * time.Hour), EndAt: periodEnd.Add(24 * time.Hour), State: PeriodOpen,
	}, "user:101")
	require.ErrorIs(t, err, ErrOverlappingPeriod)
	_, err = store.CreateFiscalPeriod(ctx, orgB, FiscalPeriod{
		PeriodRef: "FY26-P08", StartAt: periodStart, EndAt: periodEnd, State: PeriodOpen,
	}, "user:201")
	require.NoError(t, err)

	center, err := store.PutCostCenterVersion(ctx, CostCenterVersion{
		OrgID: orgA, CostCenterRef: "CC-RND", Version: 1, Name: "研发成本中心",
		LegalEntityRef: "LE-CN", OwnerUnitRef: "dept-rnd", SourceRef: "finance-master",
		SourceRevision: "finance-rev-1", ValidFrom: periodStart, CreatedBy: "user:101",
	})
	require.NoError(t, err)
	require.Equal(t, "finance-rev-1", center.SourceRevision)
	_, err = store.PutCostCenterVersion(ctx, CostCenterVersion{
		OrgID: orgA, CostCenterRef: "CC-RND", Version: 3, Name: "skipped",
		LegalEntityRef: "LE-CN", SourceRef: "finance-master", SourceRevision: "finance-rev-3",
		ValidFrom: periodStart, CreatedBy: "user:101",
	})
	require.ErrorIs(t, err, ErrConflict)

	draft, err := store.CreateBudgetDraft(ctx, BudgetPlan{
		ContractVersion: ContractVersion, BudgetRef: "BUDGET-RND-P08", OrgID: orgA,
		Scope: Scope{Kind: ScopeCostCenter, Ref: "CC-RND"}, FiscalPeriodRef: "FY26-P08",
		Amount:  Amount{ValueMinor: "9007199254740993000000", Currency: "CNY", Scale: 1_000_000},
		Version: 1,
	}, "user:101")
	require.NoError(t, err)
	require.Equal(t, BudgetDefaultEffect, draft.RuntimeEffect)
	_, err = store.ApproveBudget(ctx, orgA, draft.Plan.BudgetRef, 1, "user:101")
	require.ErrorIs(t, err, ErrMakerChecker)
	approved, err := store.ApproveBudget(ctx, orgA, draft.Plan.BudgetRef, 1, "user:102")
	require.NoError(t, err)
	require.Equal(t, BudgetApproved, approved.Plan.State)
	require.Equal(t, int64(2), approved.Plan.Version)
	require.Equal(t, "9007199254740993000000", approved.Plan.Amount.ValueMinor)
	require.Equal(t, BudgetDefaultEffect, approved.RuntimeEffect)
	_, err = store.ApproveBudget(ctx, orgA, draft.Plan.BudgetRef, 1, "user:103")
	require.ErrorIs(t, err, ErrConflict)

	budgetsA, err := store.ListLatestBudgets(ctx, orgA)
	require.NoError(t, err)
	require.Len(t, budgetsA, 1)
	require.Equal(t, BudgetApproved, budgetsA[0].Plan.State)
	budgetsB, err := store.ListLatestBudgets(ctx, orgB)
	require.NoError(t, err)
	require.Empty(t, budgetsB)
	centersA, err := store.ListEffectiveCostCenters(ctx, orgA, periodStart.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, centersA, 1)
	centersB, err := store.ListEffectiveCostCenters(ctx, orgB, periodStart.Add(time.Hour))
	require.NoError(t, err)
	require.Empty(t, centersB)

	var balance, spent int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros,total_spent_micros FROM wallets WHERE org_id=$1`, orgA).Scan(&balance, &spent))
	require.Equal(t, int64(900000000), balance)
	require.Equal(t, int64(100000000), spent)
}

func TestEnterpriseFinanceClosedPeriodCannotBeReopenedOrSilentlySkipped(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "finance-period-close")
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	period, err := store.CreateFiscalPeriod(ctx, orgID, FiscalPeriod{
		PeriodRef: "FY26-P08", StartAt: start, EndAt: start.AddDate(0, 1, 0), State: PeriodOpen,
	}, "user:301")
	require.NoError(t, err)
	_, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodClosed, period.Version, "user:302", "invalid skip")
	require.ErrorContains(t, err, "period_transition")
	period, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodPreliminary, 1, "user:302", "pre-close review")
	require.NoError(t, err)
	period, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodReconciled, 2, "user:302", "reconciled")
	require.NoError(t, err)
	period, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodClosed, 3, "user:302", "month close")
	require.NoError(t, err)
	require.Equal(t, int64(4), period.Version)
	_, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodOpen, 4, "user:302", "attempt reopen")
	require.ErrorIs(t, err, ErrClosedPeriod)

	var events int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM enterprise_finance_period_events WHERE org_id=$1`, orgID).Scan(&events))
	require.Equal(t, 4, events)
}

func insertFinanceTestOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`, name).Scan(&orgID))
	return orgID
}
