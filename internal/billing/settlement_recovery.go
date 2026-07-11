package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultSettlementRecoveryLimit = 100
	maxSettlementRecoveryLimit     = 1000
	maxSettlementRecoveryBackoff   = 5 * time.Minute
)

// SettlementRecoveryResult reports one bounded, multi-replica-safe recovery
// pass. A claimed item may already have been settled by another caller; the
// reservation state machine still guarantees a single terminal ledger event.
type SettlementRecoveryResult struct {
	Claimed int
	Settled int
	Failed  int
}

type pendingSettlement struct {
	RequestID        string
	OrgID            int64
	PromptTokens     int
	CompletionTokens int
	UsageSource      string
	Attempt          int
}

// RunSettlementRecoveryLoop performs an immediate startup pass and then
// repeats until ctx is cancelled. It owns no request traffic and is safe to
// run in every replica because RecoverPendingSettlements uses leased claims.
func (b *Billing) RunSettlementRecoveryLoop(ctx context.Context, interval time.Duration, limit int) {
	if b == nil {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	run := func() {
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		result, err := b.RecoverPendingSettlements(runCtx, limit)
		if err != nil {
			log.Printf("[billing] settlement recovery error: %v", err)
		}
		if result.Claimed > 0 {
			log.Printf("[billing] settlement recovery claimed=%d settled=%d failed=%d", result.Claimed, result.Settled, result.Failed)
		}
	}

	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// MarkBillableOutput establishes the irreversible financial boundary before
// the first billable stream chunk is written to the caller. It is idempotent
// only while the reservation remains in settlement_pending.
func (b *Billing) MarkBillableOutput(ctx context.Context, requestID string, orgID int64) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	if strings.TrimSpace(requestID) == "" || orgID <= 0 {
		return fmt.Errorf("billing output marker requires request_id and org_id")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin output marker: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	var outputStartedAt sql.NullTime
	if err := tx.QueryRow(ctx,
		`SELECT status, output_started_at
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2
		 FOR UPDATE`, requestID, orgID,
	).Scan(&status, &outputStartedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("mark billable output: reservation not found")
		}
		return fmt.Errorf("lock output marker: %w", err)
	}

	switch status {
	case "settlement_pending":
		if !outputStartedAt.Valid {
			return fmt.Errorf("settlement_pending reservation has no output marker")
		}
		return tx.Commit(ctx)
	case "frozen":
		if _, err := tx.Exec(ctx,
			`UPDATE freeze_idempotency
			 SET status = 'settlement_pending',
			     output_started_at = COALESCE(output_started_at, NOW()),
			     updated_at = NOW()
			 WHERE request_id = $1 AND org_id = $2 AND status = 'frozen'`,
			requestID, orgID,
		); err != nil {
			return fmt.Errorf("persist output marker: %w", err)
		}
		return tx.Commit(ctx)
	case "settled":
		return fmt.Errorf("cannot mark output for already settled reservation")
	case "cancelled":
		return fmt.Errorf("cannot mark output for cancelled reservation")
	default:
		return fmt.Errorf("cannot mark output for reservation in state %s", status)
	}
}

// PersistSettlementUsage stores the final usage payload before a streaming
// request attempts settlement. Replays with the same payload are idempotent;
// a different payload is rejected so recovery cannot rewrite consumed usage.
func (b *Billing) PersistSettlementUsage(ctx context.Context, req SettleRequest) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	if strings.TrimSpace(req.RequestID) == "" || req.OrgID <= 0 {
		return fmt.Errorf("billing settlement payload requires request_id and org_id")
	}
	if req.PromptTokens < 0 || req.CompletionTokens < 0 || strings.TrimSpace(req.UsageSource) == "" {
		return fmt.Errorf("billing settlement payload requires nonnegative usage and a source")
	}
	if len(req.UsageSource) > 20 {
		return fmt.Errorf("billing settlement usage source exceeds 20 characters")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin settlement payload: %w", err)
	}
	defer tx.Rollback(ctx)

	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("persist settlement payload: reservation not found")
		}
		return fmt.Errorf("lock settlement payload: %w", err)
	}

	if record.Status == "settled" {
		if settlementUsageMatches(record, req) {
			return tx.Commit(ctx)
		}
		return ErrSettlementUsageConflict
	}
	if record.Status != "settlement_pending" {
		return fmt.Errorf("cannot persist settlement payload in state %s", record.Status)
	}
	if record.SettlementPayloadAt.Valid {
		if settlementUsageMatches(record, req) {
			return tx.Commit(ctx)
		}
		return ErrSettlementUsageConflict
	}

	calculatedCost, err := calculateReservationCost(record, req)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET prompt_tokens = $3,
		     completion_tokens = $4,
		     usage_source = $5,
		     calculated_cost = $6,
		     settlement_payload_ready_at = NOW(),
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NOW(),
		     settlement_last_error = '',
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status = 'settlement_pending'`,
		req.RequestID, req.OrgID, req.PromptTokens, req.CompletionTokens,
		req.UsageSource, calculatedCost,
	); err != nil {
		return fmt.Errorf("persist settlement payload: %w", err)
	}
	return tx.Commit(ctx)
}

func settlementUsageMatches(record reservationRecord, req SettleRequest) bool {
	return record.PromptTokens == req.PromptTokens &&
		record.CompletionTokens == req.CompletionTokens &&
		record.UsageSource == req.UsageSource
}

// RecoverPendingSettlements claims at most limit ready rows, then settles each
// through the same idempotent state machine used by the request path. The CTE
// lease prevents a multi-replica thundering herd; an abandoned lease expires.
func (b *Billing) RecoverPendingSettlements(ctx context.Context, limit int) (SettlementRecoveryResult, error) {
	if b == nil || b.db == nil {
		return SettlementRecoveryResult{}, fmt.Errorf("billing database is unavailable")
	}
	if limit <= 0 {
		limit = defaultSettlementRecoveryLimit
	}
	if limit > maxSettlementRecoveryLimit {
		limit = maxSettlementRecoveryLimit
	}

	items, err := b.claimPendingSettlements(ctx, limit)
	if err != nil {
		return SettlementRecoveryResult{}, err
	}
	result := SettlementRecoveryResult{Claimed: len(items)}
	var recoveryErrors []error
	for _, item := range items {
		settled, settleErr := b.Settle(ctx, SettleRequest{
			RequestID:        item.RequestID,
			OrgID:            item.OrgID,
			PromptTokens:     item.PromptTokens,
			CompletionTokens: item.CompletionTokens,
			UsageSource:      item.UsageSource,
		})
		if settleErr == nil && settled != nil && settled.Success {
			result.Settled++
			continue
		}
		if settleErr == nil {
			if settled == nil {
				settleErr = fmt.Errorf("settlement recovery returned no receipt")
			} else {
				settleErr = fmt.Errorf("settlement recovery rejected: %s", settled.ErrorCode)
			}
		}
		result.Failed++
		if recordErr := b.recordSettlementRecoveryFailure(ctx, item, settleErr); recordErr != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("record recovery failure for %s: %w", item.RequestID, recordErr))
		}
		recoveryErrors = append(recoveryErrors, fmt.Errorf("recover %s: %w", item.RequestID, settleErr))
	}
	return result, errors.Join(recoveryErrors...)
}

func (b *Billing) claimPendingSettlements(ctx context.Context, limit int) ([]pendingSettlement, error) {
	rows, err := b.db.Query(ctx,
		`WITH candidates AS (
		    SELECT org_id, request_id
		    FROM freeze_idempotency
		    WHERE status = 'settlement_pending'
		      AND settlement_payload_ready_at IS NOT NULL
		      AND (settlement_claimed_until IS NULL OR settlement_claimed_until <= NOW())
		      AND (settlement_next_attempt_at IS NULL OR settlement_next_attempt_at <= NOW())
		    ORDER BY COALESCE(settlement_next_attempt_at, settlement_payload_ready_at),
		             settlement_payload_ready_at, org_id, request_id
		    FOR UPDATE SKIP LOCKED
		    LIMIT $1
		)
		UPDATE freeze_idempotency f
		SET settlement_claimed_until = NOW() + INTERVAL '30 seconds',
		    settlement_attempts = f.settlement_attempts + 1,
		    settlement_last_attempt_at = NOW(),
		    updated_at = NOW()
		FROM candidates c
		WHERE f.org_id = c.org_id AND f.request_id = c.request_id
		RETURNING f.request_id, f.org_id, f.prompt_tokens, f.completion_tokens,
		          f.usage_source, f.settlement_attempts`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim pending settlements: %w", err)
	}
	defer rows.Close()

	items := make([]pendingSettlement, 0, limit)
	for rows.Next() {
		var item pendingSettlement
		if err := rows.Scan(&item.RequestID, &item.OrgID, &item.PromptTokens,
			&item.CompletionTokens, &item.UsageSource, &item.Attempt); err != nil {
			return nil, fmt.Errorf("scan pending settlement: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending settlements: %w", err)
	}
	return items, nil
}

func (b *Billing) recordSettlementRecoveryFailure(ctx context.Context, item pendingSettlement, cause error) error {
	delay := settlementRecoveryBackoff(item.Attempt)
	message := strings.TrimSpace(cause.Error())
	if len(message) > 512 {
		message = message[:512]
	}
	_, err := b.db.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NOW() + ($3 * INTERVAL '1 second'),
		     settlement_last_error = $4,
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status = 'settlement_pending'`,
		item.RequestID, item.OrgID, int(delay/time.Second), message)
	return err
}

func settlementRecoveryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	delay := time.Duration(1<<attempt) * time.Second
	if delay > maxSettlementRecoveryBackoff {
		return maxSettlementRecoveryBackoff
	}
	return delay
}
