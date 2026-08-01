package enterprisefinance

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	AllocationApplyApplied   = "applied"
	AllocationApplyDuplicate = "duplicate_noop"
)

var ErrCloseNotReady = errors.New("enterprisefinance: period close is not reconciliation-ready")

type AllocationRunRecord struct {
	ID                int64                `json:"id"`
	Request           AllocationRunRequest `json:"request"`
	InputHash         string               `json:"input_hash"`
	State             string               `json:"state"`
	SourceRecordCount int64                `json:"source_record_count"`
	ResultCount       int64                `json:"result_count"`
	DirectCount       int64                `json:"direct_count"`
	RuleBasedCount    int64                `json:"rule_based_count"`
	UnallocatedCount  int64                `json:"unallocated_count"`
	ConflictCount     int64                `json:"conflict_count"`
	SourceTotal       Amount               `json:"source_total"`
	AllocatedTotal    Amount               `json:"allocated_total"`
	UnallocatedTotal  Amount               `json:"unallocated_total"`
	ConflictTotal     Amount               `json:"conflict_total"`
	CreatedAt         time.Time            `json:"created_at"`
}

type AllocationApplyResult struct {
	Result string              `json:"result"`
	Run    AllocationRunRecord `json:"run"`
}

type ReconciliationSummary struct {
	OrgID              int64                `json:"org_id"`
	FiscalPeriodRef    string               `json:"fiscal_period_ref"`
	PeriodState        string               `json:"period_state"`
	PeriodVersion      int64                `json:"period_version"`
	SourceRecordCount  int64                `json:"source_record_count"`
	SourceTotal        Amount               `json:"source_total"`
	LatestRun          *AllocationRunRecord `json:"latest_run,omitempty"`
	MissingRecordCount int64                `json:"missing_record_count"`
	CoverageState      string               `json:"coverage_state"`
	CloseReady         bool                 `json:"close_ready"`
	LedgerMutation     string               `json:"ledger_mutation"`
	CustomerDebtAction string               `json:"customer_debt_action"`
	GLExportState      string               `json:"gl_export_state"`
}

type AdjustmentJournalRecord struct {
	Journal           AdjustmentJournal `json:"journal"`
	SupersedesVersion *int64            `json:"supersedes_version,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
}

func normalizeAllocationRequest(request AllocationRunRequest) AllocationRunRequest {
	request.AllocationRunRef = strings.TrimSpace(request.AllocationRunRef)
	request.FiscalPeriodRef = strings.TrimSpace(request.FiscalPeriodRef)
	request.SourceRevision = strings.TrimSpace(request.SourceRevision)
	request.RuleSetRef = strings.TrimSpace(request.RuleSetRef)
	request.RuleSetRevision = strings.TrimSpace(request.RuleSetRevision)
	request.SupersedesRunRef = strings.TrimSpace(request.SupersedesRunRef)
	request.CreatedBy = strings.TrimSpace(request.CreatedBy)
	request.Results = append([]AllocationResult(nil), request.Results...)
	for i := range request.Results {
		result := &request.Results[i]
		result.AllocationRunRef = strings.TrimSpace(result.AllocationRunRef)
		result.RequestRef = strings.TrimSpace(result.RequestRef)
		result.FiscalPeriodRef = strings.TrimSpace(result.FiscalPeriodRef)
		result.SourceRevision = strings.TrimSpace(result.SourceRevision)
		result.Target.Ref = strings.TrimSpace(result.Target.Ref)
		result.RuleRef = strings.TrimSpace(result.RuleRef)
		result.RuleRevision = strings.TrimSpace(result.RuleRevision)
	}
	return request
}

func (s *PostgresStore) ApplyAllocationRun(ctx context.Context, request AllocationRunRequest) (AllocationApplyResult, error) {
	request = normalizeAllocationRequest(request)
	if err := ValidateAllocationRun(request); err != nil {
		return AllocationApplyResult{}, err
	}
	inputHash, err := AllocationRunIdentity(request)
	if err != nil {
		return AllocationApplyResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AllocationApplyResult{}, fmt.Errorf("begin allocation run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("enterprise-finance-allocation:%d:%s", request.OrgID, request.FiscalPeriodRef)); err != nil {
		return AllocationApplyResult{}, fmt.Errorf("lock allocation period: %w", err)
	}

	var existingHash string
	err = tx.QueryRow(ctx, `
		SELECT input_hash FROM enterprise_finance_allocation_runs
		WHERE org_id=$1 AND allocation_run_ref=$2`, request.OrgID, request.AllocationRunRef,
	).Scan(&existingHash)
	if err == nil {
		if existingHash != inputHash {
			return AllocationApplyResult{}, ErrConflict
		}
		record, loadErr := loadAllocationRun(ctx, tx, request.OrgID, request.AllocationRunRef, true)
		if loadErr != nil {
			return AllocationApplyResult{}, loadErr
		}
		if err := tx.Commit(ctx); err != nil {
			return AllocationApplyResult{}, fmt.Errorf("commit duplicate allocation run: %w", err)
		}
		return AllocationApplyResult{Result: AllocationApplyDuplicate, Run: record}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AllocationApplyResult{}, fmt.Errorf("check allocation replay: %w", err)
	}

	var periodStart, periodEnd time.Time
	var periodState string
	err = tx.QueryRow(ctx, `
		SELECT start_at,end_at,state FROM enterprise_finance_fiscal_periods
		WHERE org_id=$1 AND period_ref=$2 FOR UPDATE`, request.OrgID, request.FiscalPeriodRef,
	).Scan(&periodStart, &periodEnd, &periodState)
	if errors.Is(err, pgx.ErrNoRows) {
		return AllocationApplyResult{}, ErrNotFound
	}
	if err != nil {
		return AllocationApplyResult{}, fmt.Errorf("load allocation fiscal period: %w", err)
	}
	if periodState == PeriodClosed {
		return AllocationApplyResult{}, ErrClosedPeriod
	}

	var latestRunRef string
	err = tx.QueryRow(ctx, `
		SELECT allocation_run_ref FROM enterprise_finance_allocation_runs
		WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY created_at DESC,id DESC LIMIT 1`,
		request.OrgID, request.FiscalPeriodRef,
	).Scan(&latestRunRef)
	switch {
	case errors.Is(err, pgx.ErrNoRows) && request.SupersedesRunRef != "":
		return AllocationApplyResult{}, ErrConflict
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return AllocationApplyResult{}, fmt.Errorf("load latest allocation run: %w", err)
	case request.SupersedesRunRef != latestRunRef:
		return AllocationApplyResult{}, ErrConflict
	}

	var sourceRecordCount int64
	var sourceTotalMinor string
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*),COALESCE(SUM(cost_micros),0)::text
		FROM call_logs WHERE org_id=$1 AND created_at >= $2 AND created_at < $3`,
		request.OrgID, periodStart, periodEnd,
	).Scan(&sourceRecordCount, &sourceTotalMinor); err != nil {
		return AllocationApplyResult{}, fmt.Errorf("load allocation source total: %w", err)
	}
	if sourceRecordCount != int64(len(request.Results)) {
		return AllocationApplyResult{}, fmt.Errorf("%w: every Source request in the fiscal period must have one explicit allocation result", ErrConflict)
	}

	allocated := new(big.Int)
	unallocated := new(big.Int)
	conflicted := new(big.Int)
	var directCount, ruleCount, unallocatedCount, conflictCount int64
	for _, result := range request.Results {
		var matches int64
		var costMinor string
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*),COALESCE(SUM(cost_micros),0)::text
			FROM call_logs
			WHERE org_id=$1 AND request_id=$2 AND created_at >= $3 AND created_at < $4`,
			request.OrgID, result.RequestRef, periodStart, periodEnd,
		).Scan(&matches, &costMinor); err != nil {
			return AllocationApplyResult{}, fmt.Errorf("load allocation Source request: %w", err)
		}
		if matches != 1 || result.Amount.Currency != "CNY" || result.Amount.Scale != 1_000_000 || costMinor != result.Amount.ValueMinor {
			return AllocationApplyResult{}, fmt.Errorf("%w: allocation amount must exactly match one in-period Source request", ErrConflict)
		}
		amount, ok := new(big.Int).SetString(result.Amount.ValueMinor, 10)
		if !ok {
			return AllocationApplyResult{}, invalid("allocation_amount")
		}
		switch result.State {
		case AllocationDirect:
			directCount++
			allocated.Add(allocated, amount)
		case AllocationRuleBased:
			ruleCount++
			allocated.Add(allocated, amount)
		case AllocationUnallocated:
			unallocatedCount++
			unallocated.Add(unallocated, amount)
		case AllocationConflict:
			conflictCount++
			conflicted.Add(conflicted, amount)
		}
	}
	resultTotal := new(big.Int).Add(new(big.Int).Set(allocated), unallocated)
	resultTotal.Add(resultTotal, conflicted)
	sourceTotal, ok := new(big.Int).SetString(sourceTotalMinor, 10)
	if !ok || resultTotal.Cmp(sourceTotal) != 0 {
		return AllocationApplyResult{}, fmt.Errorf("%w: allocation totals do not equal Source period total", ErrConflict)
	}

	var runID int64
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_allocation_runs (
		  org_id,allocation_run_ref,fiscal_period_ref,source_revision,rule_set_ref,rule_set_revision,
		  input_hash,supersedes_run_ref,state,source_record_count,result_count,direct_count,
		  rule_based_count,unallocated_count,conflict_count,source_total_minor,allocated_minor,
		  unallocated_minor,conflict_minor,currency,amount_scale,created_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),'completed',$9,$10,$11,$12,$13,$14,
		          $15::numeric,$16::numeric,$17::numeric,$18::numeric,'CNY',1000000,$19)
		RETURNING id,created_at`,
		request.OrgID, request.AllocationRunRef, request.FiscalPeriodRef, request.SourceRevision,
		request.RuleSetRef, request.RuleSetRevision, inputHash, request.SupersedesRunRef,
		sourceRecordCount, len(request.Results), directCount, ruleCount, unallocatedCount, conflictCount,
		sourceTotalMinor, allocated.String(), unallocated.String(), conflicted.String(), request.CreatedBy,
	).Scan(&runID, &createdAt)
	if err != nil {
		return AllocationApplyResult{}, fmt.Errorf("insert allocation run: %w", err)
	}

	results := append([]AllocationResult(nil), request.Results...)
	sort.Slice(results, func(i, j int) bool { return results[i].RequestRef < results[j].RequestRef })
	for _, result := range results {
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_finance_allocation_results (
			  allocation_run_id,org_id,request_ref,source_revision,state,target_kind,target_ref,
			  rule_ref,rule_revision,amount_minor,currency,amount_scale,ledger_mutation,customer_debt_action
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::numeric,$11,$12,'none','none')`,
			runID, request.OrgID, result.RequestRef, result.SourceRevision, result.State,
			result.Target.Kind, result.Target.Ref, result.RuleRef, result.RuleRevision,
			result.Amount.ValueMinor, result.Amount.Currency, result.Amount.Scale,
		); err != nil {
			return AllocationApplyResult{}, fmt.Errorf("insert allocation result: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AllocationApplyResult{}, fmt.Errorf("commit allocation run: %w", err)
	}
	record := AllocationRunRecord{
		ID: runID, Request: request, InputHash: inputHash, State: AllocationRunCompleted,
		SourceRecordCount: sourceRecordCount, ResultCount: int64(len(request.Results)),
		DirectCount: directCount, RuleBasedCount: ruleCount, UnallocatedCount: unallocatedCount, ConflictCount: conflictCount,
		SourceTotal:      Amount{ValueMinor: sourceTotalMinor, Currency: "CNY", Scale: 1_000_000},
		AllocatedTotal:   Amount{ValueMinor: allocated.String(), Currency: "CNY", Scale: 1_000_000},
		UnallocatedTotal: Amount{ValueMinor: unallocated.String(), Currency: "CNY", Scale: 1_000_000},
		ConflictTotal:    Amount{ValueMinor: conflicted.String(), Currency: "CNY", Scale: 1_000_000},
		CreatedAt:        createdAt.UTC(),
	}
	return AllocationApplyResult{Result: AllocationApplyApplied, Run: record}, nil
}

func (s *PostgresStore) GetAllocationRun(ctx context.Context, orgID int64, runRef string) (AllocationRunRecord, error) {
	runRef = strings.TrimSpace(runRef)
	if orgID <= 0 || runRef == "" {
		return AllocationRunRecord{}, invalid("allocation_run_query")
	}
	return loadAllocationRun(ctx, s.db, orgID, runRef, true)
}

func (s *PostgresStore) ListAllocationRuns(ctx context.Context, orgID int64, periodRef string) ([]AllocationRunRecord, error) {
	periodRef = strings.TrimSpace(periodRef)
	if orgID <= 0 || periodRef == "" {
		return nil, invalid("allocation_run_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT allocation_run_ref FROM enterprise_finance_allocation_runs
		WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY created_at DESC,id DESC`, orgID, periodRef)
	if err != nil {
		return nil, fmt.Errorf("list allocation run identities: %w", err)
	}
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan allocation run identity: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate allocation run identities: %w", err)
	}
	rows.Close()
	records := make([]AllocationRunRecord, 0, len(refs))
	for _, ref := range refs {
		record, err := loadAllocationRun(ctx, s.db, orgID, ref, false)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadAllocationRun(ctx context.Context, query rowQuerier, orgID int64, runRef string, includeResults bool) (AllocationRunRecord, error) {
	var record AllocationRunRecord
	record.Request.ContractVersion = ContractVersion
	record.Request.OrgID = orgID
	err := query.QueryRow(ctx, `
		SELECT id,allocation_run_ref,fiscal_period_ref,source_revision,rule_set_ref,rule_set_revision,
		       COALESCE(supersedes_run_ref,''),created_by,input_hash,state,source_record_count,result_count,
		       direct_count,rule_based_count,unallocated_count,conflict_count,source_total_minor::text,
		       allocated_minor::text,unallocated_minor::text,conflict_minor::text,currency,amount_scale,created_at
		FROM enterprise_finance_allocation_runs WHERE org_id=$1 AND allocation_run_ref=$2`, orgID, runRef,
	).Scan(
		&record.ID, &record.Request.AllocationRunRef, &record.Request.FiscalPeriodRef,
		&record.Request.SourceRevision, &record.Request.RuleSetRef, &record.Request.RuleSetRevision,
		&record.Request.SupersedesRunRef, &record.Request.CreatedBy, &record.InputHash, &record.State,
		&record.SourceRecordCount, &record.ResultCount, &record.DirectCount, &record.RuleBasedCount,
		&record.UnallocatedCount, &record.ConflictCount, &record.SourceTotal.ValueMinor,
		&record.AllocatedTotal.ValueMinor, &record.UnallocatedTotal.ValueMinor, &record.ConflictTotal.ValueMinor,
		&record.SourceTotal.Currency, &record.SourceTotal.Scale, &record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AllocationRunRecord{}, ErrNotFound
	}
	if err != nil {
		return AllocationRunRecord{}, fmt.Errorf("load allocation run: %w", err)
	}
	for _, amount := range []*Amount{&record.AllocatedTotal, &record.UnallocatedTotal, &record.ConflictTotal} {
		amount.Currency, amount.Scale = record.SourceTotal.Currency, record.SourceTotal.Scale
	}
	record.CreatedAt = record.CreatedAt.UTC()
	if !includeResults {
		return record, nil
	}
	rows, err := query.Query(ctx, `
		SELECT request_ref,source_revision,state,target_kind,target_ref,rule_ref,rule_revision,
		       amount_minor::text,currency,amount_scale,ledger_mutation,customer_debt_action
		FROM enterprise_finance_allocation_results
		WHERE allocation_run_id=$1 AND org_id=$2 ORDER BY request_ref`, record.ID, orgID)
	if err != nil {
		return AllocationRunRecord{}, fmt.Errorf("load allocation results: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		result := AllocationResult{
			ContractVersion: ContractVersion, AllocationRunRef: record.Request.AllocationRunRef,
			FiscalPeriodRef: record.Request.FiscalPeriodRef,
		}
		if err := rows.Scan(
			&result.RequestRef, &result.SourceRevision, &result.State, &result.Target.Kind, &result.Target.Ref,
			&result.RuleRef, &result.RuleRevision, &result.Amount.ValueMinor, &result.Amount.Currency,
			&result.Amount.Scale, &result.LedgerMutation, &result.CustomerDebtAction,
		); err != nil {
			return AllocationRunRecord{}, fmt.Errorf("scan allocation result: %w", err)
		}
		record.Request.Results = append(record.Request.Results, result)
	}
	if err := rows.Err(); err != nil {
		return AllocationRunRecord{}, fmt.Errorf("iterate allocation results: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) GetReconciliationSummary(ctx context.Context, orgID int64, periodRef string) (ReconciliationSummary, error) {
	periodRef = strings.TrimSpace(periodRef)
	if orgID <= 0 || periodRef == "" {
		return ReconciliationSummary{}, invalid("reconciliation_summary")
	}
	var summary ReconciliationSummary
	summary.OrgID, summary.FiscalPeriodRef = orgID, periodRef
	summary.SourceTotal = Amount{Currency: "CNY", Scale: 1_000_000}
	summary.LedgerMutation = LedgerMutationNone
	summary.CustomerDebtAction = CustomerDebtNone
	summary.GLExportState = GLExportDisabled
	var startAt, endAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT start_at,end_at,state,version FROM enterprise_finance_fiscal_periods
		WHERE org_id=$1 AND period_ref=$2`, orgID, periodRef,
	).Scan(&startAt, &endAt, &summary.PeriodState, &summary.PeriodVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationSummary{}, ErrNotFound
	}
	if err != nil {
		return ReconciliationSummary{}, fmt.Errorf("load reconciliation period: %w", err)
	}
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*),COALESCE(SUM(cost_micros),0)::text FROM call_logs
		WHERE org_id=$1 AND created_at >= $2 AND created_at < $3`, orgID, startAt, endAt,
	).Scan(&summary.SourceRecordCount, &summary.SourceTotal.ValueMinor); err != nil {
		return ReconciliationSummary{}, fmt.Errorf("load reconciliation source: %w", err)
	}
	var latestRef string
	err = s.db.QueryRow(ctx, `
		SELECT allocation_run_ref FROM enterprise_finance_allocation_runs
		WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, orgID, periodRef,
	).Scan(&latestRef)
	if !errors.Is(err, pgx.ErrNoRows) && err != nil {
		return ReconciliationSummary{}, fmt.Errorf("load reconciliation latest run: %w", err)
	}
	if err == nil {
		record, loadErr := loadAllocationRun(ctx, s.db, orgID, latestRef, false)
		if loadErr != nil {
			return ReconciliationSummary{}, loadErr
		}
		summary.LatestRun = &record
		summary.MissingRecordCount = summary.SourceRecordCount - record.ResultCount
	}
	summary.CloseReady = reconciliationReady(summary)
	switch {
	case summary.SourceRecordCount == 0:
		summary.CoverageState = "no_source_spend"
	case summary.LatestRun == nil:
		summary.CoverageState = "no_allocation_run"
	case summary.CloseReady:
		summary.CoverageState = "complete"
	default:
		summary.CoverageState = "review_required"
	}
	return summary, nil
}

func reconciliationReady(summary ReconciliationSummary) bool {
	if summary.SourceRecordCount == 0 {
		return true
	}
	if summary.LatestRun == nil || summary.MissingRecordCount != 0 {
		return false
	}
	run := summary.LatestRun
	return run.UnallocatedCount == 0 && run.ConflictCount == 0 &&
		run.SourceTotal.ValueMinor == summary.SourceTotal.ValueMinor &&
		run.AllocatedTotal.ValueMinor == summary.SourceTotal.ValueMinor
}

func validatePeriodCloseReady(ctx context.Context, tx pgx.Tx, orgID int64, period FiscalPeriodRecord) error {
	var sourceCount int64
	var sourceTotal string
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*),COALESCE(SUM(cost_micros),0)::text FROM call_logs
		WHERE org_id=$1 AND created_at >= $2 AND created_at < $3`, orgID, period.StartAt, period.EndAt,
	).Scan(&sourceCount, &sourceTotal); err != nil {
		return fmt.Errorf("load close Source total: %w", err)
	}
	if sourceCount == 0 {
		return nil
	}
	var resultCount, unallocatedCount, conflictCount int64
	var runSourceTotal, allocatedTotal string
	err := tx.QueryRow(ctx, `
		SELECT result_count,unallocated_count,conflict_count,source_total_minor::text,allocated_minor::text
		FROM enterprise_finance_allocation_runs
		WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY created_at DESC,id DESC LIMIT 1`,
		orgID, period.PeriodRef,
	).Scan(&resultCount, &unallocatedCount, &conflictCount, &runSourceTotal, &allocatedTotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCloseNotReady
	}
	if err != nil {
		return fmt.Errorf("load close allocation evidence: %w", err)
	}
	if resultCount != sourceCount || unallocatedCount != 0 || conflictCount != 0 || runSourceTotal != sourceTotal || allocatedTotal != sourceTotal {
		return ErrCloseNotReady
	}
	return nil
}

func (s *PostgresStore) CreateAdjustmentJournalDraft(ctx context.Context, journal AdjustmentJournal) (AdjustmentJournalRecord, error) {
	journal.ContractVersion = ContractVersion
	journal.Version = 1
	journal.State = JournalDraft
	journal.ApprovedBy = ""
	journal.LedgerMutation = LedgerMutationNone
	journal.CustomerDebtAction = CustomerDebtNone
	journal.GLExportState = GLExportDisabled
	journal.JournalRef = strings.TrimSpace(journal.JournalRef)
	journal.FiscalPeriodRef = strings.TrimSpace(journal.FiscalPeriodRef)
	journal.RequestRef = strings.TrimSpace(journal.RequestRef)
	journal.Reason = strings.TrimSpace(journal.Reason)
	journal.SourceAllocationRunRef = strings.TrimSpace(journal.SourceAllocationRunRef)
	journal.ReversalOfJournalRef = strings.TrimSpace(journal.ReversalOfJournalRef)
	journal.CreatedBy = strings.TrimSpace(journal.CreatedBy)
	if err := ValidateAdjustmentJournal(journal); err != nil {
		return AdjustmentJournalRecord{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("begin adjustment journal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var periodState string
	if err := tx.QueryRow(ctx, `
		SELECT state FROM enterprise_finance_fiscal_periods
		WHERE org_id=$1 AND period_ref=$2 FOR UPDATE`, journal.OrgID, journal.FiscalPeriodRef,
	).Scan(&periodState); errors.Is(err, pgx.ErrNoRows) {
		return AdjustmentJournalRecord{}, ErrNotFound
	} else if err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("load adjustment period: %w", err)
	}
	if periodState != PeriodClosed {
		return AdjustmentJournalRecord{}, invalid("adjustment_requires_closed_period")
	}
	var sourceMatches int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM enterprise_finance_allocation_runs run
		JOIN enterprise_finance_allocation_results result
		  ON result.allocation_run_id=run.id AND result.org_id=run.org_id
		WHERE run.org_id=$1 AND run.allocation_run_ref=$2 AND run.fiscal_period_ref=$3 AND result.request_ref=$4`,
		journal.OrgID, journal.SourceAllocationRunRef, journal.FiscalPeriodRef, journal.RequestRef,
	).Scan(&sourceMatches); err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("validate adjustment source: %w", err)
	}
	if sourceMatches != 1 {
		return AdjustmentJournalRecord{}, ErrConflict
	}
	if journal.EntryType == JournalReversal {
		var priorReversals int
		var originalPeriod, originalRequest, originalType, originalTargetKind, originalTargetRef string
		var originalAmount, originalCurrency, originalSourceRun string
		var originalScale int64
		if err := tx.QueryRow(ctx, `
			SELECT fiscal_period_ref,request_ref,entry_type,target_kind,target_ref,amount_minor::text,
			       currency,amount_scale,source_allocation_run_ref
			FROM enterprise_finance_adjustment_journal_versions
			WHERE org_id=$1 AND journal_ref=$2 AND state='approved'
			ORDER BY version DESC LIMIT 1`, journal.OrgID, journal.ReversalOfJournalRef,
		).Scan(
			&originalPeriod, &originalRequest, &originalType, &originalTargetKind, &originalTargetRef,
			&originalAmount, &originalCurrency, &originalScale, &originalSourceRun,
		); errors.Is(err, pgx.ErrNoRows) {
			return AdjustmentJournalRecord{}, ErrConflict
		} else if err != nil {
			return AdjustmentJournalRecord{}, fmt.Errorf("validate reversal source: %w", err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM enterprise_finance_adjustment_journal_versions
			WHERE org_id=$1 AND reversal_of_journal_ref=$2`, journal.OrgID, journal.ReversalOfJournalRef,
		).Scan(&priorReversals); err != nil {
			return AdjustmentJournalRecord{}, fmt.Errorf("validate prior reversal: %w", err)
		}
		if priorReversals != 0 || originalType != JournalAdjustment ||
			originalPeriod != journal.FiscalPeriodRef || originalRequest != journal.RequestRef ||
			originalTargetKind != journal.Target.Kind || originalTargetRef != journal.Target.Ref ||
			originalAmount != journal.Amount.ValueMinor || originalCurrency != journal.Amount.Currency ||
			originalScale != journal.Amount.Scale || originalSourceRun != journal.SourceAllocationRunRef {
			return AdjustmentJournalRecord{}, ErrConflict
		}
	}
	record, err := insertAdjustmentJournalVersion(ctx, tx, journal, nil)
	if err != nil {
		return AdjustmentJournalRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("commit adjustment journal: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ApproveAdjustmentJournal(ctx context.Context, orgID int64, journalRef string, expectedVersion int64, approverRef string) (AdjustmentJournalRecord, error) {
	journalRef, approverRef = strings.TrimSpace(journalRef), strings.TrimSpace(approverRef)
	if orgID <= 0 || journalRef == "" || expectedVersion <= 0 || approverRef == "" {
		return AdjustmentJournalRecord{}, invalid("adjustment_approval")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("begin adjustment approval: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := loadLatestAdjustmentJournal(ctx, tx, orgID, journalRef, true)
	if err != nil {
		return AdjustmentJournalRecord{}, err
	}
	if current.Journal.Version != expectedVersion || current.Journal.State != JournalDraft {
		return AdjustmentJournalRecord{}, ErrConflict
	}
	if current.Journal.CreatedBy == approverRef {
		return AdjustmentJournalRecord{}, ErrMakerChecker
	}
	next := current.Journal
	next.Version++
	next.State = JournalApproved
	next.ApprovedBy = approverRef
	if err := ValidateAdjustmentJournal(next); err != nil {
		return AdjustmentJournalRecord{}, err
	}
	record, err := insertAdjustmentJournalVersion(ctx, tx, next, &current.Journal.Version)
	if err != nil {
		return AdjustmentJournalRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("commit adjustment approval: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ListLatestAdjustmentJournals(ctx context.Context, orgID int64, periodRef string) ([]AdjustmentJournalRecord, error) {
	periodRef = strings.TrimSpace(periodRef)
	if orgID <= 0 || periodRef == "" {
		return nil, invalid("adjustment_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (journal_ref) journal_ref
		FROM enterprise_finance_adjustment_journal_versions
		WHERE org_id=$1 AND fiscal_period_ref=$2 ORDER BY journal_ref,version DESC`, orgID, periodRef)
	if err != nil {
		return nil, fmt.Errorf("list adjustment identities: %w", err)
	}
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan adjustment identity: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate adjustment identities: %w", err)
	}
	rows.Close()
	records := make([]AdjustmentJournalRecord, 0, len(refs))
	for _, ref := range refs {
		record, err := loadLatestAdjustmentJournal(ctx, s.db, orgID, ref, false)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func insertAdjustmentJournalVersion(ctx context.Context, tx pgx.Tx, journal AdjustmentJournal, supersedes *int64) (AdjustmentJournalRecord, error) {
	var record AdjustmentJournalRecord
	record.Journal = journal
	record.SupersedesVersion = supersedes
	err := tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_adjustment_journal_versions (
		  org_id,journal_ref,version,fiscal_period_ref,request_ref,entry_type,reversal_of_journal_ref,
		  target_kind,target_ref,amount_minor,currency,amount_scale,reason,source_allocation_run_ref,
		  state,ledger_mutation,customer_debt_action,gl_export_state,created_by,approved_by,supersedes_version
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::numeric,$11,$12,$13,$14,$15,'none','none',
		          'not_configured',$16,$17,$18) RETURNING created_at`,
		journal.OrgID, journal.JournalRef, journal.Version, journal.FiscalPeriodRef, journal.RequestRef,
		journal.EntryType, journal.ReversalOfJournalRef, journal.Target.Kind, journal.Target.Ref,
		journal.Amount.ValueMinor, journal.Amount.Currency, journal.Amount.Scale, journal.Reason,
		journal.SourceAllocationRunRef, journal.State, journal.CreatedBy, journal.ApprovedBy, supersedes,
	).Scan(&record.CreatedAt)
	if err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("insert adjustment journal version: %w", err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

func loadLatestAdjustmentJournal(ctx context.Context, query rowQuerier, orgID int64, journalRef string, lock bool) (AdjustmentJournalRecord, error) {
	statement := `
		SELECT journal_ref,version,fiscal_period_ref,request_ref,entry_type,reversal_of_journal_ref,
		       target_kind,target_ref,amount_minor::text,currency,amount_scale,reason,
		       source_allocation_run_ref,state,ledger_mutation,customer_debt_action,gl_export_state,
		       created_by,approved_by,supersedes_version,created_at
		FROM enterprise_finance_adjustment_journal_versions
		WHERE org_id=$1 AND journal_ref=$2 ORDER BY version DESC LIMIT 1`
	if lock {
		statement += ` FOR UPDATE`
	}
	var record AdjustmentJournalRecord
	record.Journal.ContractVersion = ContractVersion
	record.Journal.OrgID = orgID
	err := query.QueryRow(ctx, statement, orgID, journalRef).Scan(
		&record.Journal.JournalRef, &record.Journal.Version, &record.Journal.FiscalPeriodRef,
		&record.Journal.RequestRef, &record.Journal.EntryType, &record.Journal.ReversalOfJournalRef,
		&record.Journal.Target.Kind, &record.Journal.Target.Ref, &record.Journal.Amount.ValueMinor,
		&record.Journal.Amount.Currency, &record.Journal.Amount.Scale, &record.Journal.Reason,
		&record.Journal.SourceAllocationRunRef, &record.Journal.State, &record.Journal.LedgerMutation,
		&record.Journal.CustomerDebtAction, &record.Journal.GLExportState, &record.Journal.CreatedBy,
		&record.Journal.ApprovedBy, &record.SupersedesVersion, &record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdjustmentJournalRecord{}, ErrNotFound
	}
	if err != nil {
		return AdjustmentJournalRecord{}, fmt.Errorf("load adjustment journal: %w", err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}
