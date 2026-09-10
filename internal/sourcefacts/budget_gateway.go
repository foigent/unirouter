package sourcefacts

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
)

type BudgetCoverage struct {
	State                   string   `json:"state"`
	Reasons                 []string `json:"reasons"`
	CoverageEpoch           string   `json:"coverage_epoch,omitempty"`
	CertifiedFrom           string   `json:"certified_from,omitempty"`
	RestoreBoundary         string   `json:"restore_boundary,omitempty"`
	WriterContract          string   `json:"writer_contract"`
	MaterializationPending  string   `json:"materialization_pending"`
	MaterializedAwaitingAck string   `json:"materialized_awaiting_ack"`
}

type BudgetUnresolved struct {
	State            string       `json:"state"`
	OpenReservations string       `json:"open_reservations"`
	FinancialHolds   string       `json:"financial_holds"`
	ReservedAtRead   *BudgetMoney `json:"reserved_at_read,omitempty"`
	Scope            string       `json:"scope"`
}

type BudgetPeriodSummary struct {
	PeriodState      string           `json:"period_state"`
	PeriodStart      string           `json:"period_start,omitempty"`
	PeriodEnd        string           `json:"period_end,omitempty"`
	AmountCutoff     string           `json:"amount_cutoff"`
	Authority        string           `json:"authority"`
	KnownSubtotal    *BudgetMoney     `json:"known_subtotal,omitempty"`
	Coverage         BudgetCoverage   `json:"coverage"`
	Comparison       BudgetComparison `json:"comparison"`
	Unresolved       BudgetUnresolved `json:"unresolved_economics"`
	Excluded         []string         `json:"excluded"`
	AllocationRunRef string           `json:"allocation_run_ref,omitempty"`
	CommittedState   string           `json:"committed_state"`
	ForecastState    string           `json:"forecast_state"`
}

var budgetFinancialEventTypes = []string{outbox.EventBillingUsagePreparedV1, outbox.EventBillingUsagePrepared, outbox.EventBillingSettledV1, outbox.EventBillingSettled, outbox.EventBillingCancelledV1, outbox.EventBillingCancelled}

const budgetSettledSubtotalSQL = `SELECT COALESCE(SUM(charged_cost_micros),0)::text FROM usage_finalization_journal
 WHERE org_id=$1 AND project_id=$2 AND event_type='settled' AND occurred_at >= $3 AND occurred_at < $4`

func (s *Service) gatewayBudgetSummaryTx(ctx context.Context, tx pgx.Tx, a budgetAccess, d BudgetDefinition, cutoff time.Time) (BudgetPeriodSummary, error) {
	result := BudgetPeriodSummary{PeriodState: "not_configured", AmountCutoff: executioncontext.Time(cutoff), Authority: "usage_finalization_journal.settled.charged_cost_micros", Coverage: BudgetCoverage{State: "uncertified", Reasons: []string{}, WriterContract: BudgetWriterContract, MaterializationPending: "0", MaterializedAwaitingAck: "0"}, Unresolved: BudgetUnresolved{State: "not_observed", Scope: "all_current_project_reservations_as_of_read", OpenReservations: "0", FinancialHolds: "0"}, Excluded: []string{"asynchronous_media_charge_facts", "calculated_cost", "wallet_balance", "legacy_project_counter", "historical_call_log_baseline"}, CommittedState: "not_modeled", ForecastState: "not_modeled"}
	start, end, err := gatewayBudgetPeriod(d.PeriodKind, cutoff)
	if err != nil {
		result.Coverage.Reasons = append(result.Coverage.Reasons, "period_not_configured")
		result.Comparison = compareBudget(d, BudgetMoney{}, false)
		return result, nil
	}
	result.PeriodState = "original_utc_period"
	result.PeriodStart = executioncontext.Time(start)
	result.PeriodEnd = executioncontext.Time(end)
	until := cutoff
	if end.Before(until) {
		until = end
	}
	var subtotal string
	if err = tx.QueryRow(ctx, budgetSettledSubtotalSQL, a.org, a.project, start, until).Scan(&subtotal); err != nil {
		return result, err
	}
	known := BudgetMoney{ValueMinor: subtotal, Currency: "CNY", Scale: "1000000"}
	if !known.valid(false) {
		return result, errBudgetSourceData
	}
	result.KnownSubtotal = &known
	certificate, certErr := runtimegeneration.ReadBudgetCoverage(ctx, tx)
	if certErr != nil {
		result.Coverage.Reasons = append(result.Coverage.Reasons, "writer_uncertified")
	} else {
		result.Coverage.CoverageEpoch = executioncontext.ID(certificate.Epoch)
		result.Coverage.CertifiedFrom = executioncontext.Time(certificate.CertifiedFrom)
		if certificate.RestoreBoundary != nil {
			result.Coverage.RestoreBoundary = executioncontext.Time(*certificate.RestoreBoundary)
		}
		if start.Before(certificate.CertifiedFrom) {
			reason := "period_before_certification"
			if certificate.RestoreBoundary != nil {
				reason = "restore_gap"
			}
			result.Coverage.Reasons = append(result.Coverage.Reasons, reason)
		}
	}
	if !s.budgetJournalReady() {
		result.Coverage.Reasons = append(result.Coverage.Reasons, "financial_journal_unavailable")
	}
	pending, acked, unknown, dead, err := readBudgetMaterializationTx(ctx, tx, a, start, until)
	if err != nil {
		return result, err
	}
	result.Coverage.MaterializationPending = strconv.FormatInt(pending, 10)
	result.Coverage.MaterializedAwaitingAck = strconv.FormatInt(acked, 10)
	if unknown {
		result.Coverage.Reasons = append(result.Coverage.Reasons, "unattributable_financial_outbox")
	}
	if pending > 0 {
		result.Coverage.Reasons = append(result.Coverage.Reasons, "financial_materialization_pending")
	}
	if dead {
		result.Coverage.Reasons = append(result.Coverage.Reasons, "financial_materialization_gap")
	}
	var open, holds int64
	var reserved string
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='frozen'),count(*) FILTER(WHERE status='settlement_pending'),
 COALESCE(SUM(frozen_amount_micros),0)::text FROM freeze_idempotency WHERE org_id=$1 AND project_id=$2 AND status IN ('frozen','settlement_pending')`, a.org, a.project).Scan(&open, &holds, &reserved); err != nil {
		return result, err
	}
	result.Unresolved.OpenReservations = strconv.FormatInt(open, 10)
	result.Unresolved.FinancialHolds = strconv.FormatInt(holds, 10)
	result.Unresolved.ReservedAtRead = &BudgetMoney{ValueMinor: reserved, Currency: "CNY", Scale: "1000000"}
	result.Unresolved.State = "none_observed"
	if open+holds > 0 {
		result.Unresolved.State = "unresolved"
	}
	if unknown {
		result.Unresolved.State = "unknown"
	}
	complete := len(result.Coverage.Reasons) == 0
	result.Coverage.State = "known_subtotal_with_gap"
	if complete {
		result.Coverage.State = "complete_settled_as_of"
	} else if unknown || certErr != nil {
		result.Coverage.State = "unknown"
	}
	result.Comparison = compareBudget(d, known, complete)
	return result, nil
}

// Queue availability, claims and retries do not filter financial gaps. The
// journal join distinguishes committed materialization from pending deletion.
const budgetMaterializationSQL = `SELECT o.event_key,o.event_type,o.org_id,o.request_id,o.payload,o.occurred_at,o.dead_lettered_at IS NOT NULL,
 r.project_id,j.event_type,j.org_id,j.project_id,j.request_id,j.occurred_at,j.charged_cost_micros
 FROM internal_outbox_events o
 LEFT JOIN freeze_idempotency r ON r.org_id=o.org_id AND r.request_id=o.request_id
 LEFT JOIN usage_finalization_journal j ON j.event_key=o.event_key
 WHERE o.event_type=ANY($1) AND (o.org_id=$2 OR o.org_id=0 OR o.payload->>'org_id'=$3)
 ORDER BY o.id`

func readBudgetMaterializationTx(ctx context.Context, tx pgx.Tx, a budgetAccess, start, end time.Time) (pending, acked int64, unknown, dead bool, err error) {
	rows, err := tx.Query(ctx, budgetMaterializationSQL, budgetFinancialEventTypes, a.org, strconv.FormatInt(a.org, 10))
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e outbox.Event
		var isDead bool
		var reservationProject, journalOrg, journalProject, journalCharge *int64
		var journalType, journalRequest *string
		var journalTime *time.Time
		if err = rows.Scan(&e.Key, &e.Type, &e.OrgID, &e.RequestID, &e.Payload, &e.OccurredAt, &isDead, &reservationProject, &journalType, &journalOrg, &journalProject, &journalRequest, &journalTime, &journalCharge); err != nil {
			return
		}
		p, decodeErr := billing.InspectBudgetJournalEvent(e)
		if decodeErr != nil || reservationProject == nil || *reservationProject != p.ProjectID {
			unknown = true
			continue
		}
		if p.OrgID != a.org || p.ProjectID != a.project {
			continue
		}
		if p.OccurredAt.Before(start) || !p.OccurredAt.Before(end) {
			continue
		}
		if journalType != nil {
			if journalOrg == nil || journalProject == nil || journalRequest == nil || journalTime == nil || journalCharge == nil || *journalOrg != p.OrgID || *journalProject != p.ProjectID || *journalRequest != p.RequestID || *journalType != p.EventType || !journalTime.Equal(p.OccurredAt) || *journalCharge != p.ChargedMicros {
				unknown = true
				continue
			}
			acked++
			continue
		}
		pending++
		dead = dead || isDead
	}
	err = rows.Err()
	return
}
