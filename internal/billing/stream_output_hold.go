package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/outbox"
)

const ProviderOutputLimitExceeded = "provider_output_limit_exceeded"

// HoldStreamOutput preserves known supplier usage and actual disclosure when a
// supplier exceeds the frozen completion limit. It does not charge, authorize a
// retry, or substitute an estimate. Usage, delivery and lease termination commit
// together so checkpoint recovery cannot reinterpret a committed hold.
func (b *Billing) HoldStreamOutput(ctx context.Context, request SettleRequest, deliveredBytes int64, deliveredChunks int) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	req, err := normalizeProviderEvidenceUsage(request)
	if err != nil {
		return err
	}
	if req.UsageSource != metering.UsageSourceUpstream || req.UsageAuthority != metering.UsageAuthorityProviderReported ||
		req.UsageFinality != metering.UsageFinalityFinal || req.StreamFence == nil ||
		deliveredBytes < 0 || deliveredChunks < 0 || (deliveredBytes == 0 && deliveredChunks != 0) {
		return fmt.Errorf("stream output hold requires final provider usage, a stream fence and exact delivery counts")
	}
	fence, err := normalizeStreamLeaseFence(*req.StreamFence)
	if err != nil || fence.OrgID != req.OrgID || fence.RequestID != req.RequestID || fence.AttemptID != req.AttemptID {
		return fmt.Errorf("stream output hold fence is invalid")
	}
	req.StreamFence = &fence
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stream output hold: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := b.holdStreamOutputTx(ctx, tx, req, deliveredBytes, deliveredChunks); err != nil {
		return err
	}
	return b.commitStreamOutputHold(ctx, tx, req, deliveredBytes, deliveredChunks)
}

func (b *Billing) commitStreamOutputHold(ctx context.Context, tx pgx.Tx, req SettleRequest, deliveredBytes int64, deliveredChunks int) error {
	if err := tx.Commit(ctx); err != nil {
		// The command may have committed despite losing its acknowledgement. A
		// bounded exact readback may prove success; no new financial command or
		// provider call is issued to resolve uncertainty.
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if readErr := b.readStreamOutputHold(readCtx, req, deliveredBytes, deliveredChunks); readErr != nil {
			return fmt.Errorf("commit stream output hold: %w (readback: %v)", err, readErr)
		}
	}
	return nil
}

func (b *Billing) holdStreamOutputTx(ctx context.Context, tx pgx.Tx, req SettleRequest, deliveredBytes int64, deliveredChunks int) error {
	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return fmt.Errorf("lock stream output hold reservation: %w", err)
	}
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return err
	}
	if record.SettlementPendingReason == OutputHoldReasonProviderResponseUnusable {
		return matchStreamOutputHoldTx(ctx, tx, record, attempt, req, deliveredBytes, deliveredChunks)
	}
	if record.Status != "settlement_pending" || !record.OutputStartedAt.Valid || record.SettlementPayloadAt.Valid ||
		(record.SettlementPendingReason != "billable_output" && record.SettlementPendingReason != "delivery_prepared") ||
		attempt.State != ProviderAttemptOutcomeResponseAccepted ||
		!record.DeliveryAttemptID.Valid || record.DeliveryAttemptID.String != req.AttemptID {
		return fmt.Errorf("cannot hold stream output in reservation=%s/%s attempt=%s", record.Status, record.SettlementPendingReason, attempt.State)
	}
	if err := requireCurrentStreamFenceTx(ctx, tx, *req.StreamFence); err != nil {
		return err
	}
	var limit int
	if err := tx.QueryRow(ctx, `SELECT reserved_completion_tokens FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&limit); err != nil {
		return err
	}
	if limit <= 0 || req.CompletionTokens <= limit {
		return fmt.Errorf("stream output hold requires usage above the frozen completion limit")
	}
	calculatedCost, breakdown, err := calculateReservationCost(record, req)
	if err != nil {
		return err
	}
	if err := persistReservationMeterEvidenceTx(ctx, tx, req.RequestID, req.OrgID, req.MeterUsage, breakdown.NonTokenCostMicros); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE freeze_idempotency
SET settlement_pending_reason='provider_response_unusable',
    provider_cost_pending_at=COALESCE(provider_cost_pending_at,NOW()),
    prompt_tokens=$3,uncached_prompt_tokens=$4,cache_read_tokens=$5,cache_creation_tokens=$6,
    completion_tokens=$7,usage_source=$8,usage_authority=$9,usage_purpose=$10,usage_finality=$11,
    usage_contract_version=$12,usage_estimate_version=$13,cache_usage_schema=$14,cache_usage_status=$15,
    calculated_cost_micros=$16,uncached_input_cost_micros=$17,cache_read_cost_micros=$18,
    cache_creation_cost_micros=$19,output_cost_micros=$20,settlement_payload_ready_at=NOW(),
    settlement_claimed_until=NULL,settlement_next_attempt_at=NULL,settlement_last_error='',
    settlement_stream_owner_id=$21,settlement_stream_generation=$22,updated_at=NOW()
WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource, req.UsageAuthority, req.UsagePurpose, req.UsageFinality,
		req.UsageContractVersion, req.UsageEstimateVersion, req.CacheUsageSchema, req.CacheUsageStatus,
		calculatedCost, breakdown.UncachedInputCostMicros, breakdown.CacheReadCostMicros,
		breakdown.CacheCreationCostMicros, breakdown.OutputCostMicros, req.StreamFence.OwnerID, req.StreamFence.Generation); err != nil {
		return fmt.Errorf("persist stream output hold: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_provider_attempts
SET state='delivery_failed',outcome_certainty='known_usage',retry_authorized=FALSE,
    failure_kind=$4,calculated_cost_micros=$5,updated_at=NOW()
WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		req.OrgID, req.RequestID, req.AttemptID, ProviderOutputLimitExceeded, calculatedCost); err != nil {
		return fmt.Errorf("mark supplier completion overrun: %w", err)
	}
	if err := updateRequestIdempotencyUsageTx(ctx, tx, req); err != nil {
		return err
	}
	if err := recordDeliveryTx(ctx, tx, DeliveryEvent{
		OrgID: req.OrgID, RequestID: req.RequestID, AttemptID: req.AttemptID,
		Mode: DeliveryModeStream, Outcome: streamOutputHoldOutcome(deliveredBytes, deliveredChunks),
		DeliveredBytes: deliveredBytes, DeliveredChunks: deliveredChunks,
		HTTPStatus: 200, ErrorCode: ProviderOutputLimitExceeded, StreamFence: req.StreamFence,
	}); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE stream_delivery_leases
SET state='interrupted',terminal_reason=$6,updated_at=NOW()
WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND owner_id=$4 AND generation=$5
  AND state='active' AND lease_expires_at>clock_timestamp()`, req.OrgID, req.RequestID,
		req.AttemptID, req.StreamFence.OwnerID, req.StreamFence.Generation, ProviderOutputLimitExceeded)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrStreamLeaseConflict
	}
	return enqueueUsageJournalTx(ctx, tx, b.outbox, outbox.EventBillingUsagePrepared, "usage_prepared", record, req, calculatedCost, 0, 0)
}

// readStreamOutputHold is read-only and also rejects a rolled-back command. The
// row locks provide one consistent view of the same atomic terminal envelope.
func (b *Billing) readStreamOutputHold(ctx context.Context, req SettleRequest, deliveredBytes int64, deliveredChunks int) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return err
	}
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return err
	}
	return matchStreamOutputHoldTx(ctx, tx, record, attempt, req, deliveredBytes, deliveredChunks)
}

func matchStreamOutputHoldTx(ctx context.Context, tx pgx.Tx, record reservationRecord, attempt providerAttemptRecord, req SettleRequest, deliveredBytes int64, deliveredChunks int) error {
	if record.Status != "settlement_pending" || record.SettlementPendingReason != OutputHoldReasonProviderResponseUnusable ||
		!record.SettlementPayloadAt.Valid || !settlementUsageMatches(record, req) ||
		!record.DeliveryAttemptID.Valid || record.DeliveryAttemptID.String != req.AttemptID ||
		!record.SettlementStreamOwnerID.Valid || record.SettlementStreamOwnerID.String != req.StreamFence.OwnerID ||
		!record.SettlementStreamGeneration.Valid || record.SettlementStreamGeneration.Int64 != req.StreamFence.Generation ||
		attempt.State != providerAttemptStateDeliveryFailed || attempt.OutcomeCertainty != providerAttemptCertaintyKnownUsage ||
		attempt.RetryAuthorized || !attempt.FailureKind.Valid || attempt.FailureKind.String != ProviderOutputLimitExceeded {
		return ErrSettlementUsageConflict
	}
	var exact bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM stream_delivery_leases l
JOIN LATERAL (SELECT * FROM request_delivery_receipts WHERE org_id=l.org_id AND request_id=l.request_id ORDER BY sequence DESC LIMIT 1) r ON TRUE
WHERE l.org_id=$1 AND l.request_id=$2 AND l.attempt_id=$3 AND l.owner_id=$4 AND l.generation=$5
  AND l.state='interrupted' AND l.terminal_reason=$6
  AND r.attempt_id=l.attempt_id AND r.stream_owner_id=l.owner_id AND r.stream_lease_generation=l.generation
  AND r.mode='stream' AND r.outcome=$9 AND r.error_code=$6
  AND r.http_status=200 AND r.delivered_bytes=$7 AND r.delivered_chunks=$8)`,
		req.OrgID, req.RequestID, req.AttemptID, req.StreamFence.OwnerID, req.StreamFence.Generation,
		ProviderOutputLimitExceeded, deliveredBytes, deliveredChunks, streamOutputHoldOutcome(deliveredBytes, deliveredChunks)).Scan(&exact)
	if err != nil {
		return err
	}
	if !exact {
		return errors.Join(ErrSettlementUsageConflict, ErrStreamLeaseConflict)
	}
	return nil
}

func streamOutputHoldOutcome(deliveredBytes int64, deliveredChunks int) string {
	if deliveredBytes > 0 && deliveredChunks == 0 {
		return DeliveryOutcomeWriteFailed
	}
	return DeliveryOutcomeInterrupted
}
