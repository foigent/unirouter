//go:build integration

package enterprisefinance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseAllocationReplayCloseAndAdjustmentsPreserveSettlement(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "finance-allocation-close")
	otherOrgID := insertFinanceTestOrg(t, ctx, pool, "finance-allocation-other")
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	period, err := store.CreateFiscalPeriod(ctx, orgID, FiscalPeriod{
		PeriodRef: "FY26-P08", StartAt: start, EndAt: end, State: PeriodOpen,
	}, "user:601")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros,currency)
		VALUES ($1,500000000,0,0,3000000,'CNY')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (request_id,org_id,type,status,amount_micros,created_at)
		VALUES ('settled-request-1',$1,'settle','completed',-1200000,$2)`, orgID, start.Add(time.Hour))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO call_logs (request_id,org_id,model,provider,cost_micros,created_at)
		VALUES ('settled-request-1',$1,'model-a','provider-a',1200000,$2),
		       ('settled-request-2',$1,'model-b','provider-b',1800000,$3)`,
		orgID, start.Add(time.Hour), start.Add(2*time.Hour))
	require.NoError(t, err)
	budgetDraft, err := store.CreateBudgetDraft(ctx, BudgetPlan{
		ContractVersion: ContractVersion, BudgetRef: "BUDGET-RND-P08", OrgID: orgID,
		Scope: Scope{Kind: ScopeCostCenter, Ref: "CC-RND"}, FiscalPeriodRef: period.PeriodRef,
		Amount: Amount{ValueMinor: "5000000", Currency: "CNY", Scale: 1_000_000}, Version: 1,
	}, "user:601")
	require.NoError(t, err)
	_, err = store.ApproveBudget(ctx, orgID, budgetDraft.Plan.BudgetRef, 1, "user:602")
	require.NoError(t, err)

	makeResult := func(runRef, requestRef, state, amount string, target Scope, ruleRef, ruleRevision string) AllocationResult {
		return AllocationResult{
			ContractVersion: ContractVersion, AllocationRunRef: runRef, RequestRef: requestRef,
			FiscalPeriodRef: period.PeriodRef, SourceRevision: "call-logs-terminal-rev-1",
			Target: target, State: state, RuleRef: ruleRef, RuleRevision: ruleRevision,
			Amount:         Amount{ValueMinor: amount, Currency: "CNY", Scale: 1_000_000},
			LedgerMutation: LedgerMutationNone, CustomerDebtAction: CustomerDebtNone,
		}
	}
	firstRequest := AllocationRunRequest{
		ContractVersion: ContractVersion, AllocationRunRef: "ALLOC-P08-1", OrgID: orgID,
		FiscalPeriodRef: period.PeriodRef, SourceRevision: "call-logs-terminal-rev-1",
		RuleSetRef: "enterprise-allocation", RuleSetRevision: "rules-7", CreatedBy: "user:601",
		Results: []AllocationResult{
			makeResult("ALLOC-P08-1", "settled-request-1", AllocationDirect, "1200000", Scope{Kind: ScopeProject, Ref: "project-1"}, "", ""),
			makeResult("ALLOC-P08-1", "settled-request-2", AllocationUnallocated, "1800000", Scope{}, "", ""),
		},
	}
	first, err := store.ApplyAllocationRun(ctx, firstRequest)
	require.NoError(t, err)
	require.Equal(t, AllocationApplyApplied, first.Result)
	require.Equal(t, int64(1), first.Run.UnallocatedCount)
	require.Equal(t, "3000000", first.Run.SourceTotal.ValueMinor)

	replay, err := store.ApplyAllocationRun(ctx, firstRequest)
	require.NoError(t, err)
	require.Equal(t, AllocationApplyDuplicate, replay.Result)
	conflictingReplay := firstRequest
	conflictingReplay.Results = append([]AllocationResult(nil), firstRequest.Results...)
	conflictingReplay.Results[1].State = AllocationConflict
	_, err = store.ApplyAllocationRun(ctx, conflictingReplay)
	require.ErrorIs(t, err, ErrConflict)

	crossTenant := firstRequest
	crossTenant.AllocationRunRef = "ALLOC-CROSS-TENANT"
	crossTenant.OrgID = otherOrgID
	for i := range crossTenant.Results {
		crossTenant.Results[i].AllocationRunRef = crossTenant.AllocationRunRef
	}
	_, err = store.ApplyAllocationRun(ctx, crossTenant)
	require.ErrorIs(t, err, ErrNotFound)

	summary, err := store.GetReconciliationSummary(ctx, orgID, period.PeriodRef)
	require.NoError(t, err)
	require.Equal(t, "review_required", summary.CoverageState)
	require.False(t, summary.CloseReady)
	positions, err := store.GetBudgetPositions(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	require.Equal(t, EvidencePartial, positions[0].Actual.Status)
	require.Equal(t, "0", positions[0].Actual.Amount.ValueMinor)
	require.Equal(t, EvidenceNotModeled, positions[0].Committed.Status)
	require.Equal(t, EvidenceNotModeled, positions[0].Forecast.Status)
	period, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodPreliminary, 1, "user:602", "preliminary review")
	require.NoError(t, err)
	period, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodReconciled, 2, "user:602", "reconciliation staged")
	require.NoError(t, err)
	_, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodClosed, 3, "user:602", "must fail with unresolved allocation")
	require.ErrorIs(t, err, ErrCloseNotReady)

	secondRequest := AllocationRunRequest{
		ContractVersion: ContractVersion, AllocationRunRef: "ALLOC-P08-2", OrgID: orgID,
		FiscalPeriodRef: period.PeriodRef, SourceRevision: "call-logs-terminal-rev-1",
		RuleSetRef: "enterprise-allocation", RuleSetRevision: "rules-8",
		SupersedesRunRef: firstRequest.AllocationRunRef, CreatedBy: "user:602",
		Results: []AllocationResult{
			makeResult("ALLOC-P08-2", "settled-request-1", AllocationDirect, "1200000", Scope{Kind: ScopeProject, Ref: "project-1"}, "", ""),
			makeResult("ALLOC-P08-2", "settled-request-2", AllocationRuleBased, "1800000", Scope{Kind: ScopeCostCenter, Ref: "CC-RND"}, "rule-cost-center", "rules-8"),
		},
	}
	second, err := store.ApplyAllocationRun(ctx, secondRequest)
	require.NoError(t, err)
	require.Equal(t, int64(1), second.Run.RuleBasedCount)
	summary, err = store.GetReconciliationSummary(ctx, orgID, period.PeriodRef)
	require.NoError(t, err)
	require.True(t, summary.CloseReady)
	require.Equal(t, "complete", summary.CoverageState)
	positions, err = store.GetBudgetPositions(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, EvidenceExact, positions[0].Actual.Status)
	require.Equal(t, "1800000", positions[0].Actual.Amount.ValueMinor)
	require.Equal(t, "3200000", *positions[0].VarianceMinor)
	period, err = store.TransitionFiscalPeriod(ctx, orgID, period.PeriodRef, PeriodClosed, 3, "user:602", "month close")
	require.NoError(t, err)
	require.Equal(t, PeriodClosed, period.State)

	thirdRequest := secondRequest
	thirdRequest.AllocationRunRef = "ALLOC-P08-3"
	thirdRequest.SupersedesRunRef = secondRequest.AllocationRunRef
	for i := range thirdRequest.Results {
		thirdRequest.Results[i].AllocationRunRef = thirdRequest.AllocationRunRef
	}
	_, err = store.ApplyAllocationRun(ctx, thirdRequest)
	require.ErrorIs(t, err, ErrClosedPeriod)

	draft, err := store.CreateAdjustmentJournalDraft(ctx, AdjustmentJournal{
		JournalRef: "ADJ-P08-1", OrgID: orgID, FiscalPeriodRef: period.PeriodRef,
		RequestRef: "settled-request-2", EntryType: JournalAdjustment,
		Target: Scope{Kind: ScopeCostCenter, Ref: "CC-RND"},
		Amount: Amount{ValueMinor: "1800000", Currency: "CNY", Scale: 1_000_000},
		Reason: "post-close allocation correction", SourceAllocationRunRef: secondRequest.AllocationRunRef,
		CreatedBy: "user:603",
	})
	require.NoError(t, err)
	require.Equal(t, JournalDraft, draft.Journal.State)
	_, err = store.ApproveAdjustmentJournal(ctx, orgID, draft.Journal.JournalRef, 1, "user:603")
	require.ErrorIs(t, err, ErrMakerChecker)
	approved, err := store.ApproveAdjustmentJournal(ctx, orgID, draft.Journal.JournalRef, 1, "user:604")
	require.NoError(t, err)
	require.Equal(t, JournalApproved, approved.Journal.State)
	require.Equal(t, GLExportDisabled, approved.Journal.GLExportState)

	reversalDraft, err := store.CreateAdjustmentJournalDraft(ctx, AdjustmentJournal{
		JournalRef: "REV-P08-1", OrgID: orgID, FiscalPeriodRef: period.PeriodRef,
		RequestRef: "settled-request-2", EntryType: JournalReversal, ReversalOfJournalRef: approved.Journal.JournalRef,
		Target: Scope{Kind: ScopeCostCenter, Ref: "CC-RND"},
		Amount: Amount{ValueMinor: "1800000", Currency: "CNY", Scale: 1_000_000},
		Reason: "reverse reporting adjustment", SourceAllocationRunRef: secondRequest.AllocationRunRef,
		CreatedBy: "user:605",
	})
	require.NoError(t, err)
	_, err = store.ApproveAdjustmentJournal(ctx, orgID, reversalDraft.Journal.JournalRef, 1, "user:606")
	require.NoError(t, err)

	var balance, spent int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros,total_spent_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balance, &spent))
	require.Equal(t, int64(500000000), balance)
	require.Equal(t, int64(3000000), spent)
	var transactionAmount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT amount_micros FROM transactions WHERE org_id=$1 AND request_id='settled-request-1'`, orgID).Scan(&transactionAmount))
	require.Equal(t, int64(-1200000), transactionAmount)
}

func TestAllocationIdentityIsOrderStableAndRevisionBound(t *testing.T) {
	base := AllocationRunRequest{
		ContractVersion: ContractVersion, AllocationRunRef: "run-identity", OrgID: 7,
		FiscalPeriodRef: "FY26-P08", SourceRevision: "source-1", RuleSetRef: "rules", RuleSetRevision: "1",
		CreatedBy: "user:1",
		Results: []AllocationResult{
			{ContractVersion: ContractVersion, AllocationRunRef: "run-identity", RequestRef: "b", FiscalPeriodRef: "FY26-P08", SourceRevision: "source-1", State: AllocationUnallocated, Amount: Amount{ValueMinor: "2", Currency: "CNY", Scale: 1_000_000}, LedgerMutation: LedgerMutationNone, CustomerDebtAction: CustomerDebtNone},
			{ContractVersion: ContractVersion, AllocationRunRef: "run-identity", RequestRef: "a", FiscalPeriodRef: "FY26-P08", SourceRevision: "source-1", State: AllocationDirect, Target: Scope{Kind: ScopeProject, Ref: "p1"}, Amount: Amount{ValueMinor: "1", Currency: "CNY", Scale: 1_000_000}, LedgerMutation: LedgerMutationNone, CustomerDebtAction: CustomerDebtNone},
		},
	}
	first, err := AllocationRunIdentity(base)
	require.NoError(t, err)
	reordered := base
	reordered.Results = []AllocationResult{base.Results[1], base.Results[0]}
	second, err := AllocationRunIdentity(reordered)
	require.NoError(t, err)
	require.Equal(t, first, second)
	revised := base
	revised.RuleSetRevision = "2"
	third, err := AllocationRunIdentity(revised)
	require.NoError(t, err)
	require.NotEqual(t, first, third)
}
