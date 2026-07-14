package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

const (
	DeliveryModeNonStream = "nonstream"
	DeliveryModeStream    = "stream"

	DeliveryOutcomeWriteStarted = "write_started"
	DeliveryOutcomePartial      = "partial"
	DeliveryOutcomeCompleted    = "completed"
	DeliveryOutcomeWriteFailed  = "write_failed"
	DeliveryOutcomeInterrupted  = "interrupted"
)

var ErrDeliveryNotChargeable = errors.New("billing reservation has no chargeable local delivery outcome")

type PrepareDeliveryResult struct {
	Funded    bool
	Preview   SettleResult
	ErrorCode string
}

type DeliveryEvent struct {
	RequestID       string
	AttemptID       string
	OrgID           int64
	Mode            string
	Outcome         string
	DeliveredBytes  int64
	DeliveredChunks int
	HTTPStatus      int
	ErrorCode       string
}

// PrepareNonStreamDelivery persists the customer-charge usage payload and
// secures any required cash-only top-up before model output can be written.
// It creates no terminal customer charge; settlement follows only after a
// durable local write outcome.
func (b *Billing) PrepareNonStreamDelivery(ctx context.Context, request SettleRequest) (*PrepareDeliveryResult, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("billing database is unavailable")
	}
	req, err := normalizeSettlementUsage(request)
	if err != nil {
		return nil, err
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin delivery preparation: %w", err)
	}
	defer tx.Rollback(ctx)

	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("lock delivery preparation: %w", err)
	}
	record = canonicalizeLegacyReservationUsage(record)
	if record.Status == "settlement_pending" &&
		(record.SettlementPendingReason == "delivery_prepared" || record.SettlementPendingReason == "delivery_completed") {
		if !settlementUsageMatches(record, req) {
			return nil, ErrSettlementUsageConflict
		}
		return prepareDeliveryResult(record, record.CalculatedCostMicros, record.TopUpAmountMicros, true), tx.Commit(ctx)
	}
	if record.Status != "frozen" {
		return nil, fmt.Errorf("cannot prepare delivery in reservation state %s", record.Status)
	}

	calculatedCost, breakdown, err := calculateReservationCost(record, req)
	if err != nil {
		return nil, err
	}
	grantConsumed := minInt64(calculatedCost, record.ReservedGrantedMicros)
	cashNeeded := calculatedCost - grantConsumed
	additionalTopUp := cashNeeded - record.ReservedBalanceMicros
	if additionalTopUp < 0 {
		additionalTopUp = 0
	}
	newFrozenAmount, err := money.Add(record.FrozenAmountMicros, additionalTopUp)
	if err != nil {
		return nil, fmt.Errorf("calculate delivery frozen amount: %w", err)
	}
	newReservedBalance, err := money.Add(record.ReservedBalanceMicros, additionalTopUp)
	if err != nil {
		return nil, fmt.Errorf("calculate delivery reserved balance: %w", err)
	}
	newTopUpAmount, err := money.Add(record.TopUpAmountMicros, additionalTopUp)
	if err != nil {
		return nil, fmt.Errorf("calculate delivery top-up receipt: %w", err)
	}
	availableCash, err := lockWalletCashForBilling(ctx, tx, req.OrgID)
	if err != nil {
		return nil, err
	}
	if availableCash < additionalTopUp {
		if err := persistFundingPendingTx(ctx, tx, b.outbox, record, req, calculatedCost, additionalTopUp, breakdown); err != nil {
			return nil, err
		}
		if err := markProviderAttemptFundingPendingTx(ctx, tx, req, calculatedCost); err != nil {
			return nil, err
		}
		if err := updateRequestIdempotencyUsageTx(ctx, tx, req); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit unfunded delivery preparation: %w", err)
		}
		result := prepareDeliveryResult(record, calculatedCost, 0, false)
		result.ErrorCode = "insufficient_topup"
		return result, nil
	}

	if additionalTopUp > 0 {
		result, err := tx.Exec(ctx, `
			UPDATE wallets
			SET balance_micros = balance_micros - $1, frozen_micros = frozen_micros + $1, updated_at = NOW()
			WHERE org_id = $2 AND balance_micros >= $1`, additionalTopUp, req.OrgID)
		if err != nil {
			return nil, fmt.Errorf("reserve delivery top-up: %w", err)
		}
		if result.RowsAffected() != 1 {
			return nil, fmt.Errorf("reserve delivery top-up: wallet state changed")
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET status = 'settlement_pending',
		    settlement_pending_reason = 'delivery_prepared',
		    frozen_amount_micros = $3,
		    reserved_balance_micros = $22,
		    topup_amount_micros = $23,
		    prompt_tokens = $4,
		    uncached_prompt_tokens = $5,
		    cache_read_tokens = $6,
		    cache_creation_tokens = $7,
		    completion_tokens = $8,
		    usage_source = $9,
		    usage_authority = $10,
		    usage_purpose = $11,
		    usage_finality = $12,
		    usage_contract_version = $13,
		    usage_estimate_version = $14,
		    cache_usage_schema = $15,
		    cache_usage_status = $16,
		    calculated_cost_micros = $17,
		    uncached_input_cost_micros = $18,
		    cache_read_cost_micros = $19,
		    cache_creation_cost_micros = $20,
		    output_cost_micros = $21,
		    settlement_payload_ready_at = NOW(),
		    settlement_claimed_until = NULL,
		    settlement_next_attempt_at = NULL,
		    settlement_last_error = '',
		    updated_at = NOW()
		WHERE request_id = $1 AND org_id = $2 AND status = 'frozen'`,
		req.RequestID, req.OrgID, newFrozenAmount,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource, req.UsageAuthority, req.UsagePurpose,
		req.UsageFinality, req.UsageContractVersion, req.UsageEstimateVersion,
		req.CacheUsageSchema, req.CacheUsageStatus, calculatedCost,
		breakdown.UncachedInputCostMicros, breakdown.CacheReadCostMicros,
		breakdown.CacheCreationCostMicros, breakdown.OutputCostMicros,
		newReservedBalance, newTopUpAmount,
	); err != nil {
		return nil, fmt.Errorf("persist delivery preparation: %w", err)
	}
	if err := markProviderAttemptDeliveryPendingTx(ctx, tx, req, calculatedCost); err != nil {
		return nil, err
	}
	if err := updateRequestIdempotencyUsageTx(ctx, tx, req); err != nil {
		return nil, err
	}
	if err := enqueueUsageJournalTx(ctx, tx, b.outbox, outbox.EventBillingUsagePrepared, "usage_prepared", record, req, calculatedCost, 0, 0); err != nil {
		return nil, fmt.Errorf("enqueue delivery preparation usage: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit delivery preparation: %w", err)
	}
	record.FrozenAmountMicros = newFrozenAmount
	record.ReservedBalanceMicros = newReservedBalance
	record.TopUpAmountMicros = newTopUpAmount
	record.UncachedInputCostMicros = breakdown.UncachedInputCostMicros
	record.CacheReadCostMicros = breakdown.CacheReadCostMicros
	record.CacheCreationCostMicros = breakdown.CacheCreationCostMicros
	record.OutputCostMicros = breakdown.OutputCostMicros
	return prepareDeliveryResult(record, calculatedCost, record.TopUpAmountMicros, true), nil
}

func prepareDeliveryResult(record reservationRecord, calculatedCost, topUp int64, funded bool) *PrepareDeliveryResult {
	return &PrepareDeliveryResult{
		Funded: funded,
		Preview: SettleResult{
			ActualCostMicros: calculatedCost, ChargedCostMicros: calculatedCost, TopUpAmountMicros: topUp,
			PricingVersion:                              record.PricingVersion,
			EffectiveInputPriceMicrosPerMillion:         record.EffectiveInputPriceMicrosPerMillion,
			EffectiveCacheReadPriceMicrosPerMillion:     record.EffectiveCacheReadPriceMicrosPerMillion,
			EffectiveCacheCreationPriceMicrosPerMillion: record.EffectiveCacheCreationPriceMicrosPerMillion,
			EffectiveOutputPriceMicrosPerMillion:        record.EffectiveOutputPriceMicrosPerMillion,
			CacheReadRatio:                              record.CacheReadRatio, CacheCreationRatio: record.CacheCreationRatio,
			UncachedInputCostMicros: record.UncachedInputCostMicros,
			CacheReadCostMicros:     record.CacheReadCostMicros,
			CacheCreationCostMicros: record.CacheCreationCostMicros,
			OutputCostMicros:        record.OutputCostMicros,
			Success:                 funded,
		},
	}
}

func markProviderAttemptDeliveryPendingTx(ctx context.Context, tx pgx.Tx, req SettleRequest, calculatedCost int64) error {
	if strings.TrimSpace(req.AttemptID) == "" {
		return nil
	}
	result, err := tx.Exec(ctx, `
		UPDATE billing_provider_attempts
		SET state = 'delivery_pending', outcome_certainty = 'known_usage',
		    retry_authorized = FALSE, calculated_cost_micros = $4, updated_at = NOW()
		WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3
		  AND state IN ('response_accepted', 'delivery_pending')`,
		req.OrgID, req.RequestID, req.AttemptID, calculatedCost)
	if err != nil {
		return fmt.Errorf("mark provider attempt delivery pending: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("mark provider attempt delivery pending: authority changed")
	}
	return nil
}

func updateRequestIdempotencyUsageTx(ctx context.Context, tx pgx.Tx, req SettleRequest) error {
	_, err := tx.Exec(ctx, `
		UPDATE request_idempotency
		SET usage_authority = $3, usage_purpose = $4, usage_finality = $5,
		    usage_contract_version = $6, usage_estimate_version = $7, updated_at = NOW()
		WHERE org_id = $1 AND operation_id = $2`,
		req.OrgID, req.RequestID, req.UsageAuthority, req.UsagePurpose,
		req.UsageFinality, req.UsageContractVersion, req.UsageEstimateVersion)
	if err != nil {
		return fmt.Errorf("persist idempotency usage receipt: %w", err)
	}
	return nil
}

func requireChargeableDeliveryTx(ctx context.Context, tx pgx.Tx, record reservationRecord, req SettleRequest) error {
	switch record.SettlementPendingReason {
	case "delivery_failed":
		return ErrDeliveryNotChargeable
	case "delivery_prepared", "delivery_completed":
		var mode, outcome string
		var deliveredBytes int64
		var deliveredChunks int
		err := tx.QueryRow(ctx, `
			SELECT mode,outcome,delivered_bytes,delivered_chunks
			FROM request_delivery_receipts
			WHERE org_id=$1 AND request_id=$2
			ORDER BY sequence DESC LIMIT 1`, req.OrgID, req.RequestID,
		).Scan(&mode, &outcome, &deliveredBytes, &deliveredChunks)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrDeliveryNotChargeable
			}
			return fmt.Errorf("read chargeable delivery receipt: %w", err)
		}
		if deliveredBytes <= 0 || deliveredChunks <= 0 {
			return ErrDeliveryNotChargeable
		}
		if mode == DeliveryModeNonStream && outcome == DeliveryOutcomeCompleted {
			return nil
		}
		if mode == DeliveryModeStream && (outcome == DeliveryOutcomePartial || outcome == DeliveryOutcomeCompleted || outcome == DeliveryOutcomeInterrupted) {
			return nil
		}
		return ErrDeliveryNotChargeable
	default:
		// Pre-P6-F41 billable-output recovery remains compatible. New gateway
		// paths move to a delivery_* reason before persisting chargeable usage.
		return nil
	}
}

// RecordDelivery appends one monotonic metadata-only local write outcome.
func (b *Billing) RecordDelivery(ctx context.Context, event DeliveryEvent) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	event.RequestID = strings.TrimSpace(event.RequestID)
	event.AttemptID = strings.TrimSpace(event.AttemptID)
	event.Mode = strings.TrimSpace(event.Mode)
	event.Outcome = strings.TrimSpace(event.Outcome)
	event.ErrorCode = strings.TrimSpace(event.ErrorCode)
	if event.OrgID <= 0 || event.RequestID == "" || len(event.RequestID) > 64 || len(event.AttemptID) > 64 ||
		(event.Mode != DeliveryModeNonStream && event.Mode != DeliveryModeStream) ||
		event.DeliveredBytes < 0 || event.DeliveredChunks < 0 || len(event.ErrorCode) > 80 {
		return fmt.Errorf("delivery event is invalid")
	}
	switch event.Outcome {
	case DeliveryOutcomeWriteStarted, DeliveryOutcomePartial, DeliveryOutcomeCompleted, DeliveryOutcomeWriteFailed, DeliveryOutcomeInterrupted:
	default:
		return fmt.Errorf("delivery outcome %q is invalid", event.Outcome)
	}
	if event.HTTPStatus != 0 && (event.HTTPStatus < 100 || event.HTTPStatus > 599) {
		return fmt.Errorf("delivery HTTP status is invalid")
	}
	if (event.Outcome == DeliveryOutcomePartial || event.Outcome == DeliveryOutcomeCompleted) &&
		(event.DeliveredBytes == 0 || event.DeliveredChunks == 0) {
		return fmt.Errorf("delivered outcome requires positive bounded counters")
	}
	if event.Outcome == DeliveryOutcomeInterrupted &&
		((event.DeliveredBytes == 0) != (event.DeliveredChunks == 0)) {
		return fmt.Errorf("interrupted outcome requires both delivery counters to be zero or positive")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delivery receipt: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, event.RequestID, event.OrgID)
	if err != nil {
		return fmt.Errorf("lock delivery receipt: %w", err)
	}

	var previous DeliveryEvent
	var sequence int
	err = tx.QueryRow(ctx, `
		SELECT sequence, mode, outcome, delivered_bytes, delivered_chunks,
		       COALESCE(http_status, 0), error_code
		FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2
		ORDER BY sequence DESC LIMIT 1`, event.OrgID, event.RequestID,
	).Scan(&sequence, &previous.Mode, &previous.Outcome, &previous.DeliveredBytes,
		&previous.DeliveredChunks, &previous.HTTPStatus, &previous.ErrorCode)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read delivery receipt: %w", err)
	}
	if err == nil && previous.Mode == event.Mode && previous.Outcome == event.Outcome &&
		previous.DeliveredBytes == event.DeliveredBytes && previous.DeliveredChunks == event.DeliveredChunks &&
		previous.HTTPStatus == event.HTTPStatus && previous.ErrorCode == event.ErrorCode {
		return tx.Commit(ctx)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		sequence = 0
	}
	sequence++
	var status any
	if event.HTTPStatus > 0 {
		status = event.HTTPStatus
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id, request_id, sequence, mode, outcome,
			delivered_bytes, delivered_chunks, http_status, error_code, occurred_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event.OrgID, event.RequestID, sequence, event.Mode, event.Outcome,
		event.DeliveredBytes, event.DeliveredChunks, status, event.ErrorCode, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("append delivery receipt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE request_idempotency
		SET delivery_mode=$3, delivery_outcome=$4, delivered_bytes=$5,
		    delivered_chunks=$6, delivery_updated_at=NOW(), updated_at=NOW()
		WHERE org_id=$1 AND operation_id=$2`,
		event.OrgID, event.RequestID, event.Mode, event.Outcome,
		event.DeliveredBytes, event.DeliveredChunks,
	); err != nil {
		return fmt.Errorf("persist idempotency delivery receipt: %w", err)
	}

	// A terminal delivery receipt may be appended after settlement so the
	// gateway can report whether its final protocol frame was accepted. Never
	// regress an already terminal provider attempt while recording that fact.
	if record.Status == "settled" || record.Status == "cancelled" {
		return tx.Commit(ctx)
	}
	if (event.Outcome == DeliveryOutcomeCompleted || event.Outcome == DeliveryOutcomeInterrupted) &&
		(record.SettlementPendingReason == "insufficient_topup" ||
			record.SettlementPendingReason == "ambiguous_attempt" ||
			record.SettlementPendingReason == "provider_response_unusable" ||
			record.SettlementPendingReason == "provider_attempt_unresolved") {
		// Delivery metadata may finish after a financial hold is established.
		// Preserve the stronger funding/provider-liability state while retaining
		// the append-only terminal delivery fact.
		return tx.Commit(ctx)
	}

	switch event.Outcome {
	case DeliveryOutcomeWriteFailed:
		if _, err := tx.Exec(ctx, `
			UPDATE freeze_idempotency
			SET provider_cost_pending_at=COALESCE(provider_cost_pending_at,NOW()),
			    settlement_pending_reason='delivery_failed',
			    settlement_next_attempt_at=NULL, updated_at=NOW()
			WHERE org_id=$1 AND request_id=$2 AND status='settlement_pending'`,
			event.OrgID, event.RequestID); err != nil {
			return fmt.Errorf("mark delivery failure hold: %w", err)
		}
		if err := updateProviderAttemptDeliveryState(ctx, tx, event, "delivery_failed", "known_usage"); err != nil {
			return err
		}
	case DeliveryOutcomeInterrupted:
		if event.DeliveredBytes == 0 && event.DeliveredChunks == 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE freeze_idempotency
				SET provider_cost_pending_at=COALESCE(provider_cost_pending_at,NOW()),
				    settlement_pending_reason='provider_attempt_unresolved',
				    settlement_claimed_until=NULL, settlement_next_attempt_at=NULL,
				    updated_at=NOW()
				WHERE org_id=$1 AND request_id=$2 AND status='settlement_pending'`,
				event.OrgID, event.RequestID); err != nil {
				return fmt.Errorf("mark zero-delivery stream interruption: %w", err)
			}
			if event.AttemptID != "" {
				if _, err := tx.Exec(ctx, `
					UPDATE billing_provider_attempts
					SET state='outcome_unknown', outcome_certainty='unknown',
					    retry_authorized=FALSE, failure_kind=$4, updated_at=NOW()
					WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
					  AND state IN ('response_accepted','delivery_pending','delivery_failed')`,
					event.OrgID, event.RequestID, event.AttemptID, event.ErrorCode); err != nil {
					return fmt.Errorf("mark zero-delivery provider attempt unresolved: %w", err)
				}
			}
			break
		}
		fallthrough
	case DeliveryOutcomePartial, DeliveryOutcomeCompleted:
		reason := "delivery_prepared"
		attemptState := "response_accepted"
		attemptCertainty := "pending"
		if event.Outcome == DeliveryOutcomeCompleted || event.Outcome == DeliveryOutcomeInterrupted {
			reason = "delivery_completed"
			attemptState = "delivery_pending"
			attemptCertainty = "known_usage"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE freeze_idempotency
			SET settlement_pending_reason=$3::varchar(40),
			    settlement_next_attempt_at=CASE WHEN $3::text='delivery_completed' THEN NOW() ELSE settlement_next_attempt_at END,
			    updated_at=NOW()
			WHERE org_id=$1 AND request_id=$2 AND status='settlement_pending'`,
			event.OrgID, event.RequestID, reason); err != nil {
			return fmt.Errorf("mark delivered settlement recovery: %w", err)
		}
		if err := updateProviderAttemptDeliveryState(ctx, tx, event, attemptState, attemptCertainty); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func updateProviderAttemptDeliveryState(ctx context.Context, tx pgx.Tx, event DeliveryEvent, state, certainty string) error {
	if event.AttemptID == "" {
		return nil
	}
	result, err := tx.Exec(ctx, `
		UPDATE billing_provider_attempts
		SET state=$4, outcome_certainty=$5, retry_authorized=FALSE, updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
		  AND state IN ('response_accepted','delivery_pending','delivery_failed')`,
		event.OrgID, event.RequestID, event.AttemptID, state, certainty)
	if err != nil {
		return fmt.Errorf("update provider attempt delivery state: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("update provider attempt delivery state: authority changed")
	}
	return nil
}
