package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

const (
	ProviderReconciliationCollect = "collect"
	ProviderReconciliationRefund  = "refund"
	ProviderReconciliationSubsidy = "subsidy"
)

var (
	ErrProviderReconciliationInvalid  = errors.New("provider reconciliation request is invalid")
	ErrProviderReconciliationConflict = errors.New("provider reconciliation conflicts with durable evidence")
	ErrProviderReconciliationUnsafe   = errors.New("provider reconciliation action is not financially authorized")
)

type ProviderReconciliationRequest struct {
	OrgID       int64
	RequestID   string
	AttemptID   string
	Action      string
	ActorUserID int64
	Reason      string
	EvidenceRef string
}

type ProviderReconciliationResult struct {
	OrgID                        int64       `json:"org_id"`
	RequestID                    string      `json:"request_id"`
	AttemptID                    string      `json:"attempt_id"`
	APIKeyID                     int64       `json:"api_key_id"`
	Action                       string      `json:"action"`
	TerminalTransactionID        int64       `json:"terminal_transaction_id"`
	TerminalObservationReceiptID int64       `json:"terminal_observation_receipt_id,omitempty"`
	CustomerChargedMicros        string      `json:"customer_charged_micros"`
	CustomerRefundMicros         string      `json:"customer_refund_micros"`
	Currency                     string      `json:"currency"`
	AmountScale                  int64       `json:"amount_scale"`
	BillingMode                  BillingMode `json:"billing_mode"`
	State                        string      `json:"state"`
}

func newProviderReconciliationResult(chargedMicros, refundMicros int64) ProviderReconciliationResult {
	return ProviderReconciliationResult{
		CustomerChargedMicros: strconv.FormatInt(chargedMicros, 10),
		CustomerRefundMicros:  strconv.FormatInt(refundMicros, 10),
		Currency:              "CNY",
		AmountScale:           1_000_000,
	}
}

func normalizeProviderReconciliation(req ProviderReconciliationRequest) (ProviderReconciliationRequest, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	req.Action = strings.TrimSpace(req.Action)
	req.Reason = strings.TrimSpace(req.Reason)
	req.EvidenceRef = strings.TrimSpace(req.EvidenceRef)
	if req.OrgID <= 0 || req.ActorUserID <= 0 || req.RequestID == "" || len(req.RequestID) > 64 || req.AttemptID == "" || len(req.AttemptID) > 64 {
		return req, fmt.Errorf("%w: bounded positive identities are required", ErrProviderReconciliationInvalid)
	}
	if req.Action != ProviderReconciliationCollect && req.Action != ProviderReconciliationRefund && req.Action != ProviderReconciliationSubsidy {
		return req, fmt.Errorf("%w: unsupported action %q", ErrProviderReconciliationInvalid, req.Action)
	}
	if req.Reason == "" || len(req.Reason) > 500 || req.EvidenceRef == "" || len(req.EvidenceRef) > 255 {
		return req, fmt.Errorf("%w: bounded reason and evidence_ref are required", ErrProviderReconciliationInvalid)
	}
	return req, nil
}

// ReconcileProviderAttempt applies one platform-authorized, amount-free action.
// Collect replays only the immutable stored customer usage/price payload. Refund
// and subsidy release the existing hold; they never invent provider usage or a
// customer debit.
func (b *Billing) ReconcileProviderAttempt(ctx context.Context, request ProviderReconciliationRequest) (*ProviderReconciliationResult, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("billing database is unavailable")
	}
	req, err := normalizeProviderReconciliation(request)
	if err != nil {
		return nil, err
	}
	if result, ok, err := b.completedProviderReconciliation(ctx, req); err != nil || ok {
		return result, err
	}
	if req.Action == ProviderReconciliationCollect {
		return b.collectProviderReconciliation(ctx, req)
	}
	return b.releaseProviderReconciliation(ctx, req)
}

func (b *Billing) collectProviderReconciliation(ctx context.Context, req ProviderReconciliationRequest) (*ProviderReconciliationResult, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin collect reconciliation: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("lock collect reservation: %w", err)
	}
	if record.BillingMode == BillingModeCostObservation {
		return nil, fmt.Errorf("%w: financial collection is unavailable in cost_observation mode", ErrProviderReconciliationUnsafe)
	}
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return nil, fmt.Errorf("lock collect attempt: %w", err)
	}
	if record.Status == "settled" || attempt.State == providerAttemptStateSettled {
		_ = tx.Rollback(ctx)
		return b.requireCompletedProviderReconciliation(ctx, req)
	}
	if attempt.State != providerAttemptStateFundingPending || attempt.OutcomeCertainty != providerAttemptCertaintyKnownUsage || record.Status != "settlement_pending" ||
		strings.TrimSpace(record.UsageSource) != "upstream" || !record.OutputStartedAt.Valid ||
		!record.SettlementPayloadAt.Valid || record.CalculatedCostMicros <= 0 {
		return nil, ErrProviderReconciliationUnsafe
	}
	if err := authorizeProviderReconciliationTx(ctx, tx, req, attempt.APIKeyID); err != nil {
		return nil, err
	}
	settleReq := SettleRequest{
		RequestID: req.RequestID, AttemptID: req.AttemptID, OrgID: req.OrgID,
		PromptTokens:         record.PromptTokens,
		UncachedPromptTokens: record.UncachedPromptTokens, CacheReadTokens: record.CacheReadTokens,
		CacheCreationTokens: record.CacheCreationTokens, CompletionTokens: record.CompletionTokens,
		UsageSource: record.UsageSource, UsageAuthority: record.UsageAuthority,
		UsagePurpose: record.UsagePurpose, UsageFinality: record.UsageFinality,
		UsageContractVersion: record.UsageContractVersion, UsageEstimateVersion: record.UsageEstimateVersion,
		CacheUsageSchema: record.CacheUsageSchema,
		CacheUsageStatus: record.CacheUsageStatus,
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("authorize collect reconciliation: %w", err)
	}
	settled, err := b.Settle(ctx, settleReq)
	if err != nil {
		return nil, err
	}
	if settled == nil || !settled.Success {
		if settled != nil && settled.ErrorCode == "insufficient_topup" {
			return nil, ErrSettlementFundingPending
		}
		return nil, ErrProviderReconciliationConflict
	}
	if err := b.completeProviderReconciliation(ctx, req, settled.SettleID); err != nil {
		return nil, err
	}
	result := newProviderReconciliationResult(settled.ChargedCostMicros, 0)
	result.OrgID, result.RequestID, result.AttemptID = req.OrgID, req.RequestID, req.AttemptID
	result.APIKeyID, result.Action, result.TerminalTransactionID = attempt.APIKeyID, req.Action, settled.SettleID
	result.BillingMode = settled.BillingMode
	result.State = providerAttemptStateSettled
	return &result, nil
}

func (b *Billing) releaseProviderReconciliation(ctx context.Context, req ProviderReconciliationRequest) (*ProviderReconciliationResult, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin release reconciliation: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("lock release reservation: %w", err)
	}
	if record.BillingMode == BillingModeCostObservation {
		return b.releaseObservationProviderReconciliationTx(ctx, tx, record, req)
	}
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return nil, fmt.Errorf("lock release attempt: %w", err)
	}
	if record.Status == "cancelled" || attempt.State == "reconciled" {
		_ = tx.Rollback(ctx)
		return b.requireCompletedProviderReconciliation(ctx, req)
	}
	if record.Status != "settlement_pending" || !isProviderAttemptReleasable(req.Action, attempt) {
		return nil, ErrProviderReconciliationUnsafe
	}
	if err := authorizeProviderReconciliationTx(ctx, tx, req, attempt.APIKeyID); err != nil {
		return nil, err
	}
	if err := lockWalletForBilling(ctx, tx, req.OrgID); err != nil {
		return nil, err
	}
	restoredGrant, forfeitedGrant, err := finalizeGrantAllocations(ctx, tx, req.OrgID, req.RequestID, record.ReservedGrantedMicros, 0)
	if err != nil {
		return nil, err
	}
	refund, err := money.Add(record.ReservedBalanceMicros, restoredGrant)
	if err != nil {
		return nil, fmt.Errorf("calculate reconciled release refund: %w", err)
	}
	var newBalance int64
	if err := tx.QueryRow(ctx,
		`UPDATE wallets
		 SET frozen_micros = frozen_micros - $1, balance_micros = balance_micros + $2,
		     granted_balance_micros = granted_balance_micros + $3, updated_at = NOW()
		 WHERE org_id = $4 AND frozen_micros >= $1 RETURNING balance_micros`,
		record.FrozenAmountMicros, record.ReservedBalanceMicros, restoredGrant, req.OrgID,
	).Scan(&newBalance); err != nil {
		return nil, fmt.Errorf("release reconciled hold: %w", err)
	}
	var transactionID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount_micros, balance_after_micros, note)
		 VALUES ($1, $2, $3, 'completed', $4, $5, $6) RETURNING id`,
		req.RequestID, req.OrgID, "reconcile_"+req.Action, refund, newBalance,
		fmt.Sprintf("Provider cost %s for request %s", req.Action, req.RequestID),
	).Scan(&transactionID); err != nil {
		return nil, fmt.Errorf("record reconciled release: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'cancelled', refund_amount_micros = $3, forfeited_grant_amount_micros = $4,
		     terminal_transaction_id = $5, balance_after_micros = $6, terminal_at = NOW(),
		     provider_cost_pending_at = NULL, settlement_pending_reason = '',
		     settlement_claimed_until = NULL, settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status = 'settlement_pending'`,
		req.RequestID, req.OrgID, refund, forfeitedGrant, transactionID, newBalance,
	); err != nil {
		return nil, fmt.Errorf("terminalize reconciled release: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE billing_provider_attempts
		 SET state = 'reconciled', retry_authorized = FALSE,
		     outcome_certainty = CASE WHEN outcome_certainty = 'pending' THEN 'unknown' ELSE outcome_certainty END,
		     terminal_transaction_id = $4, financial_resolved_at = COALESCE(financial_resolved_at, NOW()),
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`,
		req.OrgID, req.RequestID, req.AttemptID, transactionID,
	); err != nil {
		return nil, fmt.Errorf("resolve provider attempt: %w", err)
	}
	if err := insertProviderReconciliationPhaseTx(ctx, tx, req, attempt.APIKeyID, "completed", transactionID, 0); err != nil {
		return nil, err
	}
	cancelReq := SettleRequest{
		RequestID: req.RequestID, AttemptID: req.AttemptID, OrgID: req.OrgID,
		PromptTokens: record.PromptTokens, UncachedPromptTokens: record.UncachedPromptTokens,
		CacheReadTokens: record.CacheReadTokens, CacheCreationTokens: record.CacheCreationTokens,
		CompletionTokens: record.CompletionTokens, UsageSource: record.UsageSource,
		UsageAuthority: record.UsageAuthority, UsagePurpose: record.UsagePurpose,
		UsageFinality: record.UsageFinality, UsageContractVersion: record.UsageContractVersion,
		UsageEstimateVersion: record.UsageEstimateVersion,
		CacheUsageSchema:     record.CacheUsageSchema, CacheUsageStatus: record.CacheUsageStatus,
	}
	if err := enqueueUsageJournalTx(ctx, tx, b.outbox, outbox.EventBillingCancelled, "cancelled", record, cancelReq, 0, 0, transactionID); err != nil {
		return nil, fmt.Errorf("enqueue reconciled release: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit release reconciliation: %w", err)
	}
	result := newProviderReconciliationResult(0, refund)
	result.OrgID, result.RequestID, result.AttemptID = req.OrgID, req.RequestID, req.AttemptID
	result.APIKeyID, result.Action, result.TerminalTransactionID = attempt.APIKeyID, req.Action, transactionID
	result.BillingMode = record.BillingMode
	result.State = "reconciled"
	return &result, nil
}

func (b *Billing) releaseObservationProviderReconciliationTx(
	ctx context.Context,
	tx pgx.Tx,
	record reservationRecord,
	req ProviderReconciliationRequest,
) (*ProviderReconciliationResult, error) {
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return nil, fmt.Errorf("lock observation release attempt: %w", err)
	}
	if record.Status == "cancelled" || attempt.State == "reconciled" {
		_ = tx.Rollback(ctx)
		return b.requireCompletedProviderReconciliation(ctx, req)
	}
	if record.Status != "settlement_pending" || !isProviderAttemptReleasable(req.Action, attempt) {
		return nil, ErrProviderReconciliationUnsafe
	}
	if err := authorizeProviderReconciliationTx(ctx, tx, req, attempt.APIKeyID); err != nil {
		return nil, err
	}

	cancelReq := SettleRequest{
		RequestID: req.RequestID, AttemptID: req.AttemptID, OrgID: req.OrgID,
		PromptTokens: record.PromptTokens, UncachedPromptTokens: record.UncachedPromptTokens,
		CacheReadTokens: record.CacheReadTokens, CacheCreationTokens: record.CacheCreationTokens,
		CompletionTokens: record.CompletionTokens, UsageSource: record.UsageSource,
		UsageAuthority: record.UsageAuthority, UsagePurpose: record.UsagePurpose,
		UsageFinality: record.UsageFinality, UsageContractVersion: record.UsageContractVersion,
		UsageEstimateVersion: record.UsageEstimateVersion,
		CacheUsageSchema:     record.CacheUsageSchema, CacheUsageStatus: record.CacheUsageStatus,
	}
	receiptID, err := insertObservationReceiptTx(
		ctx, tx, record, cancelReq, "cancelled", record.CalculatedCostMicros,
	)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET status='cancelled', charged_cost_micros=0, uncovered_cost_micros=0,
		    topup_amount_micros=0, refund_amount_micros=0,
		    forfeited_grant_amount_micros=0,
		    terminal_transaction_id=NULL,
		    terminal_observation_receipt_id=$3,
		    balance_after_micros=NULL, terminal_at=NOW(),
		    provider_cost_pending_at=NULL, settlement_pending_reason='',
		    settlement_claimed_until=NULL, settlement_next_attempt_at=NULL,
		    updated_at=NOW()
		WHERE request_id=$1 AND org_id=$2
		  AND billing_mode='cost_observation'
		  AND status='settlement_pending'`,
		req.RequestID, req.OrgID, receiptID,
	)
	if err != nil {
		return nil, fmt.Errorf("terminalize observation reconciliation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, ErrProviderReconciliationConflict
	}
	result, err = tx.Exec(ctx, `
		UPDATE billing_provider_attempts
		SET state='reconciled', retry_authorized=FALSE,
		    outcome_certainty=CASE WHEN outcome_certainty='pending' THEN 'unknown' ELSE outcome_certainty END,
		    terminal_transaction_id=NULL,
		    financial_resolved_at=COALESCE(financial_resolved_at,NOW()),
		    updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
		  AND state IN (
		      'dispatch_started','response_accepted','outcome_unknown',
		      'response_unusable','delivery_pending','delivery_failed','output_withheld'
		  )`, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return nil, fmt.Errorf("resolve observation provider attempt: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, ErrProviderReconciliationConflict
	}
	if err := insertProviderReconciliationPhaseTx(
		ctx, tx, req, attempt.APIKeyID, "completed", 0, receiptID,
	); err != nil {
		return nil, err
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, b.outbox, outbox.EventBillingCancelled, "cancelled",
		record, cancelReq, record.CalculatedCostMicros, 0, 0,
	); err != nil {
		return nil, fmt.Errorf("enqueue observation reconciliation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit observation reconciliation: %w", err)
	}

	response := newProviderReconciliationResult(0, 0)
	response.OrgID, response.RequestID, response.AttemptID = req.OrgID, req.RequestID, req.AttemptID
	response.APIKeyID, response.Action = attempt.APIKeyID, req.Action
	response.TerminalObservationReceiptID = receiptID
	response.BillingMode = record.BillingMode
	response.State = "reconciled"
	return &response, nil
}

func isProviderAttemptReleasable(action string, attempt providerAttemptRecord) bool {
	if action == ProviderReconciliationSubsidy {
		return (attempt.State == providerAttemptStateFundingPending ||
			attempt.State == providerAttemptStateDeliveryPending ||
			attempt.State == providerAttemptStateDeliveryFailed ||
			attempt.State == providerAttemptStateOutputWithheld) &&
			attempt.OutcomeCertainty == providerAttemptCertaintyKnownUsage &&
			attempt.CalculatedCostMicros.Valid && attempt.CalculatedCostMicros.Int64 > 0
	}
	if action != ProviderReconciliationRefund {
		return false
	}
	switch attempt.State {
	case providerAttemptStateDispatchStarted, ProviderAttemptOutcomeResponseAccepted,
		ProviderAttemptOutcomeUnknown, ProviderAttemptOutcomeResponseUnusable,
		providerAttemptStateDeliveryPending, providerAttemptStateDeliveryFailed,
		providerAttemptStateOutputWithheld:
		return true
	default:
		return false
	}
}

func authorizeProviderReconciliationTx(ctx context.Context, tx pgx.Tx, req ProviderReconciliationRequest, apiKeyID int64) error {
	result, err := tx.Exec(ctx,
		`INSERT INTO provider_cost_reconciliations
		 (org_id, request_id, attempt_id, api_key_id, action, phase, actor_user_id, reason, evidence_ref)
		 VALUES ($1, $2, $3, $4, $5, 'requested', $6, $7, $8)
		 ON CONFLICT DO NOTHING`,
		req.OrgID, req.RequestID, req.AttemptID, apiKeyID, req.Action, req.ActorUserID, req.Reason, req.EvidenceRef,
	)
	if err != nil {
		return fmt.Errorf("authorize provider reconciliation: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	return requireProviderReconciliationAuthorityTx(ctx, tx, req)
}

func requireProviderReconciliationAuthorityTx(ctx context.Context, tx pgx.Tx, req ProviderReconciliationRequest) error {
	var action, reason, evidence string
	var actor int64
	if err := tx.QueryRow(ctx,
		`SELECT action, actor_user_id, reason, evidence_ref
		 FROM provider_cost_reconciliations
		 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND phase='requested'`,
		req.OrgID, req.RequestID, req.AttemptID,
	).Scan(&action, &actor, &reason, &evidence); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProviderReconciliationConflict
		}
		return fmt.Errorf("read provider reconciliation authority: %w", err)
	}
	if action != req.Action || actor != req.ActorUserID || reason != req.Reason || evidence != req.EvidenceRef {
		return ErrProviderReconciliationConflict
	}
	return nil
}

func insertProviderReconciliationPhaseTx(
	ctx context.Context,
	tx pgx.Tx,
	req ProviderReconciliationRequest,
	apiKeyID int64,
	phase string,
	transactionID int64,
	observationReceiptID int64,
) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO provider_cost_reconciliations
		 (org_id, request_id, attempt_id, api_key_id, action, phase, actor_user_id,
		  reason, evidence_ref, terminal_transaction_id, terminal_observation_receipt_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10::bigint,0),NULLIF($11::bigint,0))
		 ON CONFLICT (org_id, request_id, attempt_id, action, phase) DO NOTHING`,
		req.OrgID, req.RequestID, req.AttemptID, apiKeyID, req.Action, phase,
		req.ActorUserID, req.Reason, req.EvidenceRef, transactionID, observationReceiptID,
	)
	if err != nil {
		return fmt.Errorf("record provider reconciliation %s: %w", phase, err)
	}
	return nil
}

func (b *Billing) completeProviderReconciliation(ctx context.Context, req ProviderReconciliationRequest, transactionID int64) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return err
	}
	if attempt.State != providerAttemptStateSettled {
		return ErrProviderReconciliationConflict
	}
	if !attempt.TerminalTransactionID.Valid || attempt.TerminalTransactionID.Int64 != transactionID {
		return ErrProviderReconciliationConflict
	}
	if err := insertProviderReconciliationPhaseTx(ctx, tx, req, attempt.APIKeyID, "completed", transactionID, 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (b *Billing) completedProviderReconciliation(ctx context.Context, req ProviderReconciliationRequest) (*ProviderReconciliationResult, bool, error) {
	var result ProviderReconciliationResult
	var action, state, reason, evidence, attemptState, observationStatus string
	var billingMode BillingMode
	var apiKeyID, actor int64
	var transactionID, observationReceiptID sql.NullInt64
	var reservationTransactionID, reservationObservationReceiptID sql.NullInt64
	var attemptTransactionID sql.NullInt64
	var charged, refund sql.NullInt64
	var observationCharged int64
	err := b.db.QueryRow(ctx,
		`SELECT r.api_key_id, r.action, r.actor_user_id, r.reason, r.evidence_ref,
		        r.terminal_transaction_id, r.terminal_observation_receipt_id,
		        COALESCE(f.status,''), f.billing_mode,
		        f.charged_cost_micros, f.refund_amount_micros,
		        f.terminal_transaction_id, f.terminal_observation_receipt_id,
		        a.state, a.terminal_transaction_id,
		        COALESCE(o.terminal_status,''), COALESCE(o.charged_cost_micros,0)
		 FROM provider_cost_reconciliations r
		 JOIN freeze_idempotency f ON f.org_id=r.org_id AND f.request_id=r.request_id
		 JOIN billing_provider_attempts a
		   ON a.org_id=r.org_id AND a.request_id=r.request_id AND a.attempt_id=r.attempt_id
		 LEFT JOIN billing_observation_receipts o
		   ON o.org_id=r.org_id AND o.request_id=r.request_id
		  AND o.id=r.terminal_observation_receipt_id
		 WHERE r.org_id=$1 AND r.request_id=$2 AND r.attempt_id=$3 AND r.phase='completed'`,
		req.OrgID, req.RequestID, req.AttemptID,
	).Scan(&apiKeyID, &action, &actor, &reason, &evidence,
		&transactionID, &observationReceiptID, &state, &billingMode,
		&charged, &refund, &reservationTransactionID, &reservationObservationReceiptID,
		&attemptState, &attemptTransactionID, &observationStatus, &observationCharged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if action != req.Action || actor != req.ActorUserID || reason != req.Reason || evidence != req.EvidenceRef {
		return nil, false, ErrProviderReconciliationConflict
	}
	if transactionID.Valid {
		if observationReceiptID.Valid || billingMode == BillingModeCostObservation ||
			!reservationTransactionID.Valid || reservationTransactionID.Int64 != transactionID.Int64 ||
			!attemptTransactionID.Valid || attemptTransactionID.Int64 != transactionID.Int64 ||
			(action == ProviderReconciliationCollect && (state != "settled" || attemptState != providerAttemptStateSettled)) ||
			(action != ProviderReconciliationCollect && (state != "cancelled" || attemptState != "reconciled")) {
			return nil, false, ErrProviderReconciliationConflict
		}
	} else {
		if !observationReceiptID.Valid || billingMode != BillingModeCostObservation ||
			action == ProviderReconciliationCollect || state != "cancelled" || attemptState != "reconciled" ||
			reservationTransactionID.Valid || attemptTransactionID.Valid ||
			!reservationObservationReceiptID.Valid ||
			reservationObservationReceiptID.Int64 != observationReceiptID.Int64 ||
			observationStatus != "cancelled" || observationCharged != 0 {
			return nil, false, ErrProviderReconciliationConflict
		}
	}
	result = newProviderReconciliationResult(charged.Int64, refund.Int64)
	result.OrgID, result.RequestID, result.AttemptID = req.OrgID, req.RequestID, req.AttemptID
	result.APIKeyID, result.Action, result.State = apiKeyID, action, attemptState
	result.BillingMode = billingMode
	if transactionID.Valid {
		result.TerminalTransactionID = transactionID.Int64
	}
	if observationReceiptID.Valid {
		result.TerminalObservationReceiptID = observationReceiptID.Int64
	}
	return &result, true, nil
}

func (b *Billing) requireCompletedProviderReconciliation(ctx context.Context, req ProviderReconciliationRequest) (*ProviderReconciliationResult, error) {
	result, ok, err := b.completedProviderReconciliation(ctx, req)
	if err != nil {
		return nil, err
	}
	if !ok {
		tx, beginErr := b.db.Begin(ctx)
		if beginErr != nil {
			return nil, beginErr
		}
		defer tx.Rollback(ctx)
		record, lockErr := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
		if lockErr != nil {
			return nil, lockErr
		}
		attempt, lockErr := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
		if lockErr != nil {
			return nil, lockErr
		}
		if req.Action != ProviderReconciliationCollect || record.Status != "settled" ||
			attempt.State != providerAttemptStateSettled || !record.TerminalTransactionID.Valid ||
			!attempt.TerminalTransactionID.Valid || record.TerminalTransactionID.Int64 != attempt.TerminalTransactionID.Int64 {
			return nil, ErrProviderReconciliationConflict
		}
		if authErr := requireProviderReconciliationAuthorityTx(ctx, tx, req); authErr != nil {
			return nil, authErr
		}
		if phaseErr := insertProviderReconciliationPhaseTx(ctx, tx, req, attempt.APIKeyID, "completed", record.TerminalTransactionID.Int64, 0); phaseErr != nil {
			return nil, phaseErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		result, ok, err = b.completedProviderReconciliation(ctx, req)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrProviderReconciliationConflict
		}
	}
	return result, nil
}
