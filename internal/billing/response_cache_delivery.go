package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/outbox"
)

type ResponseCacheDeliveryEvent struct {
	RequestID       string
	OrgID           int64
	Outcome         string
	DeliveredBytes  int64
	DeliveredChunks int
	HTTPStatus      int
	ErrorCode       string
}

func ResponseCacheSettleRequest(requestID string, orgID int64) SettleRequest {
	return SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		UsageSource:          metering.UsageSourceResponseCache,
		UsageAuthority:       metering.UsageAuthorityLocalCache,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersionV2,
		CacheUsageStatus:     cacheUsageStatusNotReported,
	}
}

// PrepareResponseCacheDelivery persists an immutable zero-token, zero-charge
// settlement payload before local cached bytes may be written. It never creates
// a provider attempt and therefore cannot inflate provider request counts.
func (b *Billing) PrepareResponseCacheDelivery(ctx context.Context, request SettleRequest) (*PrepareDeliveryResult, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("billing database is unavailable")
	}
	req, err := normalizeSettlementUsage(request)
	if err != nil {
		return nil, err
	}
	if req.AttemptID != "" || req.UsageAuthority != metering.UsageAuthorityLocalCache {
		return nil, ErrDeliveryAttemptConflict
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin response-cache delivery preparation: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("lock response-cache delivery preparation: %w", err)
	}
	record = canonicalizeLegacyReservationUsage(record)
	if err := requireBoundOrInternalSettlementTx(ctx, tx, record, req); err != nil {
		return nil, err
	}
	if record.Status == "settlement_pending" &&
		strings.HasPrefix(record.SettlementPendingReason, "response_cache_delivery_") {
		if !settlementUsageMatches(record, req) {
			return nil, ErrSettlementUsageConflict
		}
		return prepareDeliveryResult(record, 0, 0, true), tx.Commit(ctx)
	}
	if record.Status != "frozen" {
		return nil, fmt.Errorf("cannot prepare response-cache delivery in reservation state %s", record.Status)
	}
	calculatedCost, breakdown, err := calculateReservationCost(record, req)
	if err != nil {
		return nil, err
	}
	if calculatedCost != 0 {
		return nil, fmt.Errorf("response-cache delivery must have zero calculated cost")
	}
	if _, err := tx.Exec(ctx, `
UPDATE freeze_idempotency
   SET status='settlement_pending',
       settlement_pending_reason='response_cache_delivery_prepared',
       output_started_at=COALESCE(output_started_at,NOW()),
       prompt_tokens=0, uncached_prompt_tokens=0, cache_read_tokens=0,
       cache_creation_tokens=0, completion_tokens=0,
       usage_source=$3, usage_authority=$4, usage_purpose=$5,
       usage_finality=$6, usage_contract_version=$7, usage_estimate_version='',
       cache_usage_schema='', cache_usage_status=$8,
       calculated_cost_micros=0, uncached_input_cost_micros=$9,
       cache_read_cost_micros=$10, cache_creation_cost_micros=$11,
       output_cost_micros=$12, settlement_payload_ready_at=NOW(),
       settlement_claimed_until=NULL, settlement_next_attempt_at=NULL,
       settlement_last_error='', updated_at=NOW()
 WHERE request_id=$1 AND org_id=$2 AND status='frozen'`,
		req.RequestID, req.OrgID, req.UsageSource, req.UsageAuthority,
		req.UsagePurpose, req.UsageFinality, req.UsageContractVersion,
		req.CacheUsageStatus, breakdown.UncachedInputCostMicros,
		breakdown.CacheReadCostMicros, breakdown.CacheCreationCostMicros,
		breakdown.OutputCostMicros); err != nil {
		return nil, fmt.Errorf("persist response-cache delivery preparation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO response_cache_delivery_receipts (
    org_id,request_id,sequence,outcome,delivered_bytes,delivered_chunks,http_status,error_code,occurred_at)
VALUES ($1,$2,1,'write_started',0,0,200,'',NOW())`, req.OrgID, req.RequestID); err != nil {
		return nil, fmt.Errorf("append response-cache delivery start: %w", err)
	}
	if err := updateRequestIdempotencyUsageTx(ctx, tx, req); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE request_idempotency
   SET delivery_mode='nonstream', delivery_outcome='write_started',
       delivered_bytes=0, delivered_chunks=0, delivery_updated_at=NOW(), updated_at=NOW()
 WHERE org_id=$1 AND operation_id=$2`, req.OrgID, req.RequestID); err != nil {
		return nil, fmt.Errorf("persist response-cache delivery start: %w", err)
	}
	if err := enqueueUsageJournalTx(ctx, tx, b.outbox, outbox.EventBillingUsagePrepared,
		"usage_prepared", record, req, 0, 0, 0); err != nil {
		return nil, fmt.Errorf("enqueue response-cache usage preparation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit response-cache delivery preparation: %w", err)
	}
	record.CalculatedCostMicros = 0
	record.UncachedInputCostMicros = 0
	record.CacheReadCostMicros = 0
	record.CacheCreationCostMicros = 0
	record.OutputCostMicros = 0
	return prepareDeliveryResult(record, 0, 0, true), nil
}

func (b *Billing) RecordResponseCacheDelivery(ctx context.Context, event ResponseCacheDeliveryEvent) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	event.RequestID = strings.TrimSpace(event.RequestID)
	event.ErrorCode = strings.TrimSpace(event.ErrorCode)
	if event.RequestID == "" || len(event.RequestID) > 64 || event.OrgID <= 0 ||
		event.DeliveredBytes < 0 || event.DeliveredChunks < 0 || len(event.ErrorCode) > 80 ||
		(event.HTTPStatus < 100 || event.HTTPStatus > 599) {
		return fmt.Errorf("response-cache delivery event is invalid")
	}
	if event.Outcome == DeliveryOutcomeCompleted {
		if event.DeliveredBytes <= 0 || event.DeliveredChunks != 1 {
			return fmt.Errorf("completed response-cache delivery requires one positive chunk")
		}
	} else if event.Outcome == DeliveryOutcomeWriteFailed {
		if event.DeliveredChunks != 0 {
			return fmt.Errorf("failed response-cache delivery cannot report a completed chunk")
		}
	} else {
		return fmt.Errorf("response-cache delivery outcome is invalid")
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin response-cache delivery receipt: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, event.RequestID, event.OrgID)
	if err != nil {
		return fmt.Errorf("lock response-cache delivery receipt: %w", err)
	}
	if record.DeliveryAttemptID.Valid || record.UsageAuthority != metering.UsageAuthorityLocalCache ||
		record.UsageContractVersion != metering.UsageContractVersionV2 {
		return ErrDeliveryAttemptConflict
	}
	var previousOutcome string
	var previousBytes int64
	var previousChunks int
	var sequence int
	err = tx.QueryRow(ctx, `
SELECT sequence,outcome,delivered_bytes,delivered_chunks
  FROM response_cache_delivery_receipts
 WHERE org_id=$1 AND request_id=$2 ORDER BY sequence DESC LIMIT 1`, event.OrgID, event.RequestID,
	).Scan(&sequence, &previousOutcome, &previousBytes, &previousChunks)
	if err != nil {
		return fmt.Errorf("read response-cache delivery receipt: %w", err)
	}
	if sequence == 2 {
		if previousOutcome == event.Outcome && previousBytes == event.DeliveredBytes && previousChunks == event.DeliveredChunks {
			return tx.Commit(ctx)
		}
		return ErrDeliveryAttemptConflict
	}
	if sequence != 1 || previousOutcome != DeliveryOutcomeWriteStarted {
		return ErrDeliveryAttemptConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO response_cache_delivery_receipts (
    org_id,request_id,sequence,outcome,delivered_bytes,delivered_chunks,http_status,error_code,occurred_at)
VALUES ($1,$2,2,$3,$4,$5,$6,$7,$8)`, event.OrgID, event.RequestID, event.Outcome,
		event.DeliveredBytes, event.DeliveredChunks, event.HTTPStatus, event.ErrorCode, time.Now().UTC()); err != nil {
		return fmt.Errorf("append response-cache delivery receipt: %w", err)
	}
	reason := "response_cache_delivery_completed"
	if event.Outcome == DeliveryOutcomeWriteFailed {
		reason = "response_cache_delivery_failed"
	}
	result, err := tx.Exec(ctx, `
UPDATE freeze_idempotency
   SET settlement_pending_reason=$3, settlement_next_attempt_at=NOW(), updated_at=NOW()
 WHERE org_id=$1 AND request_id=$2 AND status='settlement_pending'
   AND settlement_pending_reason='response_cache_delivery_prepared'`,
		event.OrgID, event.RequestID, reason)
	if err != nil {
		return fmt.Errorf("advance response-cache delivery state: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrDeliveryAttemptConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE request_idempotency
   SET delivery_mode='nonstream',delivery_outcome=$3,delivered_bytes=$4,
       delivered_chunks=$5,delivery_updated_at=NOW(),updated_at=NOW()
 WHERE org_id=$1 AND operation_id=$2`, event.OrgID, event.RequestID, event.Outcome,
		event.DeliveredBytes, event.DeliveredChunks); err != nil {
		return fmt.Errorf("persist response-cache delivery outcome: %w", err)
	}
	return tx.Commit(ctx)
}
