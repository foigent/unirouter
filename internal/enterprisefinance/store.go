package enterprisefinance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	internaldb "github.com/uniroute/unirouter/internal/db"
)

var (
	ErrConflict          = errors.New("enterprisefinance: concurrent or immutable state conflict")
	ErrNotFound          = errors.New("enterprisefinance: record not found")
	ErrOverlappingPeriod = errors.New("enterprisefinance: fiscal periods overlap")
)

type PostgresStore struct {
	db internaldb.DBQuerier
}

func NewPostgresStore(database internaldb.DBQuerier) (*PostgresStore, error) {
	if database == nil {
		return nil, invalid("database")
	}
	return &PostgresStore{db: database}, nil
}

type FiscalPeriodRecord struct {
	OrgID          int64     `json:"org_id"`
	PeriodRef      string    `json:"period_ref"`
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	State          string    `json:"state"`
	Version        int64     `json:"version"`
	CreatedBy      string    `json:"created_by"`
	StateChangedBy string    `json:"state_changed_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CostCenterVersion struct {
	OrgID          int64      `json:"org_id"`
	CostCenterRef  string     `json:"cost_center_ref"`
	Version        int64      `json:"version"`
	Name           string     `json:"name"`
	LegalEntityRef string     `json:"legal_entity_ref"`
	OwnerUnitRef   string     `json:"owner_unit_ref,omitempty"`
	SourceRef      string     `json:"source_ref"`
	SourceRevision string     `json:"source_revision"`
	ValidFrom      time.Time  `json:"valid_from"`
	ValidTo        *time.Time `json:"valid_to,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
}

type BudgetRecord struct {
	Plan              BudgetPlan `json:"plan"`
	CreatedBy         string     `json:"created_by"`
	SupersedesVersion *int64     `json:"supersedes_version,omitempty"`
	RuntimeEffect     string     `json:"runtime_effect"`
	CreatedAt         time.Time  `json:"created_at"`
}

func (s *PostgresStore) CreateFiscalPeriod(ctx context.Context, orgID int64, period FiscalPeriod, actorRef string) (FiscalPeriodRecord, error) {
	actorRef = strings.TrimSpace(actorRef)
	if orgID <= 0 || actorRef == "" {
		return FiscalPeriodRecord{}, invalid("fiscal_period_actor")
	}
	if err := ValidateFiscalPeriod(period); err != nil {
		return FiscalPeriodRecord{}, err
	}
	if period.State != PeriodOpen {
		return FiscalPeriodRecord{}, invalid("fiscal_period_initial_state")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("begin fiscal period: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("enterprise-finance-period:%d", orgID)); err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("lock fiscal calendar: %w", err)
	}
	var overlaps int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM enterprise_finance_fiscal_periods
		WHERE org_id=$1 AND start_at < $3 AND end_at > $2`, orgID, period.StartAt, period.EndAt,
	).Scan(&overlaps); err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("check fiscal period overlap: %w", err)
	}
	if overlaps != 0 {
		return FiscalPeriodRecord{}, ErrOverlappingPeriod
	}
	var record FiscalPeriodRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_fiscal_periods (
		  org_id,period_ref,start_at,end_at,state,created_by,state_changed_by
		) VALUES ($1,$2,$3,$4,'open',$5,$5)
		RETURNING org_id,period_ref,start_at,end_at,state,version,created_by,state_changed_by,created_at,updated_at`,
		orgID, strings.TrimSpace(period.PeriodRef), period.StartAt, period.EndAt, actorRef,
	).Scan(
		&record.OrgID, &record.PeriodRef, &record.StartAt, &record.EndAt, &record.State,
		&record.Version, &record.CreatedBy, &record.StateChangedBy, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("insert fiscal period: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO enterprise_finance_period_events (
		  period_id,org_id,from_state,to_state,version,actor_ref,reason
		) SELECT id,org_id,'none',state,version,$3,'period_created'
		  FROM enterprise_finance_fiscal_periods WHERE org_id=$1 AND period_ref=$2`,
		orgID, record.PeriodRef, actorRef,
	); err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("record fiscal period creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("commit fiscal period: %w", err)
	}
	return normalizeFiscalPeriodRecord(record), nil
}

func (s *PostgresStore) TransitionFiscalPeriod(ctx context.Context, orgID int64, periodRef, toState string, expectedVersion int64, actorRef, reason string) (FiscalPeriodRecord, error) {
	periodRef, actorRef, reason = strings.TrimSpace(periodRef), strings.TrimSpace(actorRef), strings.TrimSpace(reason)
	if orgID <= 0 || periodRef == "" || actorRef == "" || expectedVersion <= 0 || reason == "" {
		return FiscalPeriodRecord{}, invalid("fiscal_period_transition")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("begin fiscal period transition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var periodID int64
	var current FiscalPeriodRecord
	err = tx.QueryRow(ctx, `
		SELECT id,org_id,period_ref,start_at,end_at,state,version,created_by,state_changed_by,created_at,updated_at
		FROM enterprise_finance_fiscal_periods
		WHERE org_id=$1 AND period_ref=$2 FOR UPDATE`, orgID, periodRef,
	).Scan(
		&periodID, &current.OrgID, &current.PeriodRef, &current.StartAt, &current.EndAt,
		&current.State, &current.Version, &current.CreatedBy, &current.StateChangedBy,
		&current.CreatedAt, &current.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return FiscalPeriodRecord{}, ErrNotFound
	}
	if err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("load fiscal period: %w", err)
	}
	if current.Version != expectedVersion {
		return FiscalPeriodRecord{}, ErrConflict
	}
	if err := CanTransitionPeriod(current.State, toState); err != nil {
		return FiscalPeriodRecord{}, err
	}
	if toState == PeriodClosed && current.State != PeriodClosed {
		if err := validatePeriodCloseReady(ctx, tx, orgID, current); err != nil {
			return FiscalPeriodRecord{}, err
		}
	}
	if current.State == toState {
		if err := tx.Commit(ctx); err != nil {
			return FiscalPeriodRecord{}, fmt.Errorf("commit fiscal period no-op: %w", err)
		}
		return normalizeFiscalPeriodRecord(current), nil
	}
	var next FiscalPeriodRecord
	err = tx.QueryRow(ctx, `
		UPDATE enterprise_finance_fiscal_periods
		SET state=$1,version=version+1,state_changed_by=$2,updated_at=NOW()
		WHERE id=$3 AND org_id=$4 AND version=$5
		RETURNING org_id,period_ref,start_at,end_at,state,version,created_by,state_changed_by,created_at,updated_at`,
		toState, actorRef, periodID, orgID, expectedVersion,
	).Scan(
		&next.OrgID, &next.PeriodRef, &next.StartAt, &next.EndAt, &next.State,
		&next.Version, &next.CreatedBy, &next.StateChangedBy, &next.CreatedAt, &next.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return FiscalPeriodRecord{}, ErrConflict
	}
	if err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("transition fiscal period: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO enterprise_finance_period_events (
		  period_id,org_id,from_state,to_state,version,actor_ref,reason
		) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		periodID, orgID, current.State, next.State, next.Version, actorRef, reason,
	); err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("record fiscal period transition: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return FiscalPeriodRecord{}, fmt.Errorf("commit fiscal period transition: %w", err)
	}
	return normalizeFiscalPeriodRecord(next), nil
}

func (s *PostgresStore) ListFiscalPeriods(ctx context.Context, orgID int64) ([]FiscalPeriodRecord, error) {
	if orgID <= 0 {
		return nil, invalid("fiscal_period_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT org_id,period_ref,start_at,end_at,state,version,created_by,state_changed_by,created_at,updated_at
		FROM enterprise_finance_fiscal_periods WHERE org_id=$1 ORDER BY start_at DESC,period_ref`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list fiscal periods: %w", err)
	}
	defer rows.Close()
	records := make([]FiscalPeriodRecord, 0)
	for rows.Next() {
		var record FiscalPeriodRecord
		if err := rows.Scan(
			&record.OrgID, &record.PeriodRef, &record.StartAt, &record.EndAt, &record.State,
			&record.Version, &record.CreatedBy, &record.StateChangedBy, &record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan fiscal period: %w", err)
		}
		records = append(records, normalizeFiscalPeriodRecord(record))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fiscal periods: %w", err)
	}
	return records, nil
}

func ValidateCostCenter(version CostCenterVersion) error {
	if version.OrgID <= 0 || blank(version.CostCenterRef) || version.Version <= 0 || blank(version.Name) ||
		blank(version.LegalEntityRef) || blank(version.SourceRef) || blank(version.SourceRevision) ||
		blank(version.CreatedBy) || !validTime(version.ValidFrom) ||
		(version.ValidTo != nil && (!validTime(*version.ValidTo) || !version.ValidTo.After(version.ValidFrom))) {
		return invalid("cost_center_version")
	}
	return nil
}

func (s *PostgresStore) PutCostCenterVersion(ctx context.Context, version CostCenterVersion) (CostCenterVersion, error) {
	version.CostCenterRef = strings.TrimSpace(version.CostCenterRef)
	version.CreatedBy = strings.TrimSpace(version.CreatedBy)
	if err := ValidateCostCenter(version); err != nil {
		return CostCenterVersion{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CostCenterVersion{}, fmt.Errorf("begin cost center version: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("enterprise-finance-cost-center:%d:%s", version.OrgID, version.CostCenterRef)); err != nil {
		return CostCenterVersion{}, fmt.Errorf("lock cost center: %w", err)
	}
	var latest int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version),0) FROM enterprise_finance_cost_center_versions
		WHERE org_id=$1 AND cost_center_ref=$2`, version.OrgID, version.CostCenterRef).Scan(&latest); err != nil {
		return CostCenterVersion{}, fmt.Errorf("load cost center version: %w", err)
	}
	if version.Version != latest+1 {
		return CostCenterVersion{}, ErrConflict
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_cost_center_versions (
		  org_id,cost_center_ref,version,name,legal_entity_ref,owner_unit_ref,
		  source_ref,source_revision,valid_from,valid_to,created_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING created_at`,
		version.OrgID, version.CostCenterRef, version.Version, strings.TrimSpace(version.Name),
		strings.TrimSpace(version.LegalEntityRef), strings.TrimSpace(version.OwnerUnitRef),
		strings.TrimSpace(version.SourceRef), strings.TrimSpace(version.SourceRevision),
		version.ValidFrom, version.ValidTo, version.CreatedBy,
	).Scan(&version.CreatedAt)
	if err != nil {
		return CostCenterVersion{}, fmt.Errorf("insert cost center version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CostCenterVersion{}, fmt.Errorf("commit cost center version: %w", err)
	}
	return normalizeCostCenter(version), nil
}

func (s *PostgresStore) ListEffectiveCostCenters(ctx context.Context, orgID int64, effectiveAt time.Time) ([]CostCenterVersion, error) {
	if orgID <= 0 || !validTime(effectiveAt) {
		return nil, invalid("cost_center_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (cost_center_ref)
		  org_id,cost_center_ref,version,name,legal_entity_ref,owner_unit_ref,
		  source_ref,source_revision,valid_from,valid_to,created_by,created_at
		FROM enterprise_finance_cost_center_versions
		WHERE org_id=$1 AND valid_from <= $2 AND (valid_to IS NULL OR valid_to > $2)
		ORDER BY cost_center_ref,version DESC`, orgID, effectiveAt)
	if err != nil {
		return nil, fmt.Errorf("list effective cost centers: %w", err)
	}
	defer rows.Close()
	centers := make([]CostCenterVersion, 0)
	for rows.Next() {
		var center CostCenterVersion
		if err := rows.Scan(
			&center.OrgID, &center.CostCenterRef, &center.Version, &center.Name,
			&center.LegalEntityRef, &center.OwnerUnitRef, &center.SourceRef,
			&center.SourceRevision, &center.ValidFrom, &center.ValidTo,
			&center.CreatedBy, &center.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan effective cost center: %w", err)
		}
		centers = append(centers, normalizeCostCenter(center))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective cost centers: %w", err)
	}
	return centers, nil
}

func (s *PostgresStore) CreateBudgetDraft(ctx context.Context, plan BudgetPlan, actorRef string) (BudgetRecord, error) {
	actorRef = strings.TrimSpace(actorRef)
	plan.State = BudgetDraft
	plan.ApprovedBy = ""
	if actorRef == "" || plan.Version != 1 {
		return BudgetRecord{}, invalid("budget_creator")
	}
	if err := ValidateBudget(plan); err != nil {
		return BudgetRecord{}, err
	}
	var record BudgetRecord
	record.Plan = plan
	record.CreatedBy = actorRef
	record.RuntimeEffect = BudgetDefaultEffect
	err := s.db.QueryRow(ctx, `
		INSERT INTO enterprise_finance_budget_versions (
		  org_id,budget_ref,version,scope_kind,scope_ref,fiscal_period_ref,
		  amount_minor,currency,amount_scale,state,runtime_effect,created_by
		) VALUES ($1,$2,1,$3,$4,$5,$6::numeric,$7,$8,'draft','report_only',$9)
		RETURNING created_at`,
		plan.OrgID, strings.TrimSpace(plan.BudgetRef), plan.Scope.Kind, strings.TrimSpace(plan.Scope.Ref),
		strings.TrimSpace(plan.FiscalPeriodRef), plan.Amount.ValueMinor, plan.Amount.Currency,
		plan.Amount.Scale, actorRef,
	).Scan(&record.CreatedAt)
	if err != nil {
		return BudgetRecord{}, fmt.Errorf("insert budget draft: %w", err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

func (s *PostgresStore) ApproveBudget(ctx context.Context, orgID int64, budgetRef string, expectedVersion int64, approverRef string) (BudgetRecord, error) {
	budgetRef, approverRef = strings.TrimSpace(budgetRef), strings.TrimSpace(approverRef)
	if orgID <= 0 || budgetRef == "" || expectedVersion <= 0 || approverRef == "" {
		return BudgetRecord{}, invalid("budget_approval")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return BudgetRecord{}, fmt.Errorf("begin budget approval: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := loadLatestBudget(ctx, tx, orgID, budgetRef, true)
	if err != nil {
		return BudgetRecord{}, err
	}
	if current.Plan.Version != expectedVersion || current.Plan.State != BudgetDraft {
		return BudgetRecord{}, ErrConflict
	}
	if current.CreatedBy == approverRef {
		return BudgetRecord{}, ErrMakerChecker
	}
	next := current
	next.Plan.Version++
	next.Plan.State = BudgetApproved
	next.Plan.ApprovedBy = approverRef
	next.SupersedesVersion = &current.Plan.Version
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_budget_versions (
		  org_id,budget_ref,version,scope_kind,scope_ref,fiscal_period_ref,
		  amount_minor,currency,amount_scale,state,runtime_effect,created_by,approved_by,supersedes_version
		) VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8,$9,'approved','report_only',$10,$11,$12)
		RETURNING created_at`,
		orgID, budgetRef, next.Plan.Version, next.Plan.Scope.Kind, next.Plan.Scope.Ref,
		next.Plan.FiscalPeriodRef, next.Plan.Amount.ValueMinor, next.Plan.Amount.Currency,
		next.Plan.Amount.Scale, next.CreatedBy, approverRef, current.Plan.Version,
	).Scan(&next.CreatedAt)
	if err != nil {
		return BudgetRecord{}, fmt.Errorf("insert approved budget version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BudgetRecord{}, fmt.Errorf("commit budget approval: %w", err)
	}
	next.CreatedAt = next.CreatedAt.UTC()
	return next, nil
}

func (s *PostgresStore) ListLatestBudgets(ctx context.Context, orgID int64) ([]BudgetRecord, error) {
	if orgID <= 0 {
		return nil, invalid("budget_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (budget_ref)
		  budget_ref,version,scope_kind,scope_ref,fiscal_period_ref,amount_minor::text,
		  currency,amount_scale,state,created_by,approved_by,supersedes_version,runtime_effect,created_at
		FROM enterprise_finance_budget_versions
		WHERE org_id=$1 ORDER BY budget_ref,version DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list latest budgets: %w", err)
	}
	defer rows.Close()
	records := make([]BudgetRecord, 0)
	for rows.Next() {
		var record BudgetRecord
		record.Plan.ContractVersion = ContractVersion
		record.Plan.OrgID = orgID
		if err := rows.Scan(
			&record.Plan.BudgetRef, &record.Plan.Version, &record.Plan.Scope.Kind,
			&record.Plan.Scope.Ref, &record.Plan.FiscalPeriodRef, &record.Plan.Amount.ValueMinor,
			&record.Plan.Amount.Currency, &record.Plan.Amount.Scale, &record.Plan.State,
			&record.CreatedBy, &record.Plan.ApprovedBy, &record.SupersedesVersion,
			&record.RuntimeEffect, &record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan latest budget: %w", err)
		}
		record.CreatedAt = record.CreatedAt.UTC()
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest budgets: %w", err)
	}
	return records, nil
}

func loadLatestBudget(ctx context.Context, tx pgx.Tx, orgID int64, budgetRef string, lock bool) (BudgetRecord, error) {
	query := `
		SELECT budget_ref,version,scope_kind,scope_ref,fiscal_period_ref,amount_minor::text,
		       currency,amount_scale,state,created_by,approved_by,supersedes_version,runtime_effect,created_at
		FROM enterprise_finance_budget_versions
		WHERE org_id=$1 AND budget_ref=$2 ORDER BY version DESC LIMIT 1`
	if lock {
		query += ` FOR UPDATE`
	}
	var record BudgetRecord
	record.Plan.ContractVersion = ContractVersion
	record.Plan.OrgID = orgID
	err := tx.QueryRow(ctx, query, orgID, budgetRef).Scan(
		&record.Plan.BudgetRef, &record.Plan.Version, &record.Plan.Scope.Kind,
		&record.Plan.Scope.Ref, &record.Plan.FiscalPeriodRef, &record.Plan.Amount.ValueMinor,
		&record.Plan.Amount.Currency, &record.Plan.Amount.Scale, &record.Plan.State,
		&record.CreatedBy, &record.Plan.ApprovedBy, &record.SupersedesVersion,
		&record.RuntimeEffect, &record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return BudgetRecord{}, ErrNotFound
	}
	if err != nil {
		return BudgetRecord{}, fmt.Errorf("load latest budget: %w", err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

func normalizeFiscalPeriodRecord(record FiscalPeriodRecord) FiscalPeriodRecord {
	record.StartAt = record.StartAt.UTC()
	record.EndAt = record.EndAt.UTC()
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func normalizeCostCenter(center CostCenterVersion) CostCenterVersion {
	center.ValidFrom = center.ValidFrom.UTC()
	if center.ValidTo != nil {
		value := center.ValidTo.UTC()
		center.ValidTo = &value
	}
	center.CreatedAt = center.CreatedAt.UTC()
	return center
}
