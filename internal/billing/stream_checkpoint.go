package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/metering"
)

const (
	StreamLeaseStateCompleted   = "completed"
	StreamLeaseStateInterrupted = "interrupted"
	streamRecoveryClaimTTL      = 2 * time.Minute
)

var ErrStreamLeaseConflict = errors.New("stream delivery lease authority changed")

type StreamDeliveryStart struct {
	RequestID  string
	AttemptID  string
	OwnerID    string
	OrgID      int64
	Generation int64
	LeaseTTL   time.Duration
}

type StreamLeaseFence struct {
	RequestID  string
	AttemptID  string
	OwnerID    string
	OrgID      int64
	Generation int64
}

func (start StreamDeliveryStart) Fence() StreamLeaseFence {
	return StreamLeaseFence{
		RequestID: start.RequestID, AttemptID: start.AttemptID, OwnerID: start.OwnerID,
		OrgID: start.OrgID, Generation: start.Generation,
	}
}

type StreamDeliveryCheckpoint struct {
	RequestID        string
	AttemptID        string
	OwnerID          string
	OrgID            int64
	Generation       int64
	DeliveredBytes   int64
	DeliveredChunks  int
	PromptTokens     int
	CompletionTokens int
	PolicySetVersion string
	LeaseTTL         time.Duration
}

type StreamCheckpointResult struct {
	Sequence   int
	Idempotent bool
}

type StreamRecoveryResult struct {
	Claimed          int
	Completed        int
	Settled          int
	ProviderCostHeld int
	Failed           int
}

// BeginStreamDelivery atomically establishes the billable-output marker, the
// initial local-write receipt and the process lease. No response content may
// be written before this transaction succeeds.
func (b *Billing) BeginStreamDelivery(ctx context.Context, start StreamDeliveryStart) error {
	start.RequestID = strings.TrimSpace(start.RequestID)
	start.AttemptID = strings.TrimSpace(start.AttemptID)
	start.OwnerID = strings.TrimSpace(start.OwnerID)
	if b == nil || b.db == nil || start.OrgID <= 0 || start.RequestID == "" || len(start.RequestID) > 64 ||
		start.AttemptID == "" || len(start.AttemptID) > 64 || start.OwnerID == "" || len(start.OwnerID) > 80 ||
		start.Generation <= 0 || start.LeaseTTL <= 0 {
		return fmt.Errorf("stream delivery start is invalid")
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stream delivery lease: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, start.RequestID, start.OrgID)
	if err != nil {
		return fmt.Errorf("lock stream delivery reservation: %w", err)
	}
	var existingAttempt, existingOwner, existingState string
	var existingGeneration int64
	var existingAlive bool
	err = tx.QueryRow(ctx, `
		SELECT attempt_id,owner_id,generation,state,lease_expires_at>clock_timestamp()
		FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2 FOR UPDATE`, start.OrgID, start.RequestID,
	).Scan(&existingAttempt, &existingOwner, &existingGeneration, &existingState, &existingAlive)
	if err == nil {
		if record.Status != "settlement_pending" ||
			(record.SettlementPendingReason != "billable_output" && record.SettlementPendingReason != "delivery_prepared") ||
			existingAttempt != start.AttemptID || existingOwner != start.OwnerID || existingGeneration != start.Generation ||
			existingState != "active" || !existingAlive {
			return ErrStreamLeaseConflict
		}
		if err := bindDeliveryAttemptTx(ctx, tx, &record, start.OrgID, start.RequestID, start.AttemptID); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `
			UPDATE stream_delivery_leases
			SET lease_expires_at=clock_timestamp()+($5*INTERVAL '1 millisecond'), updated_at=NOW()
			WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND generation=$4
			  AND state='active' AND lease_expires_at>clock_timestamp()`,
			start.OrgID, start.RequestID, start.AttemptID, start.Generation, ceilDurationMilliseconds(start.LeaseTTL))
		if err != nil {
			return fmt.Errorf("renew idempotent stream delivery lease: %w", err)
		}
		if result.RowsAffected() != 1 {
			return ErrStreamLeaseConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read stream delivery lease: %w", err)
	}
	if record.Status != "frozen" {
		return fmt.Errorf("stream delivery reservation is %s", record.Status)
	}
	if start.Generation != 1 {
		return fmt.Errorf("stream delivery must begin at generation one")
	}
	if err := bindDeliveryAttemptTx(ctx, tx, &record, start.OrgID, start.RequestID, start.AttemptID); err != nil {
		return err
	}

	result, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET status='settlement_pending', output_started_at=COALESCE(output_started_at,NOW()),
		    settlement_pending_reason='billable_output', updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND status='frozen'`, start.OrgID, start.RequestID)
	if err != nil {
		return fmt.Errorf("mark stream billable output: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrStreamLeaseConflict
	}
	var receiptSequence int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequence),0)+1 FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2`, start.OrgID, start.RequestID).Scan(&receiptSequence); err != nil {
		return fmt.Errorf("allocate stream delivery receipt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,
			http_status,error_code,occurred_at,stream_owner_id,stream_lease_generation
		) VALUES ($1,$2,$3,$4,'stream','write_started',0,0,200,'',NOW(),$5,$6)`,
		start.OrgID, start.RequestID, start.AttemptID, receiptSequence, start.OwnerID, start.Generation); err != nil {
		return fmt.Errorf("append stream write-start receipt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE request_idempotency
		SET delivery_mode='stream', delivery_outcome='write_started', delivered_bytes=0,
		    delivered_chunks=0, delivery_updated_at=NOW(), updated_at=NOW()
		WHERE org_id=$1 AND operation_id=$2`, start.OrgID, start.RequestID); err != nil {
		return fmt.Errorf("persist stream write-start idempotency state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO stream_delivery_leases (
			org_id,request_id,attempt_id,owner_id,generation,state,lease_expires_at
		) VALUES ($1,$2,$3,$4,$5,'active',clock_timestamp()+($6*INTERVAL '1 millisecond'))`,
		start.OrgID, start.RequestID, start.AttemptID, start.OwnerID, start.Generation, ceilDurationMilliseconds(start.LeaseTTL)); err != nil {
		return fmt.Errorf("create stream delivery lease: %w", err)
	}
	return tx.Commit(ctx)
}

func (b *Billing) RenewStreamDelivery(ctx context.Context, start StreamDeliveryStart) error {
	if b == nil || b.db == nil || start.OrgID <= 0 || strings.TrimSpace(start.RequestID) == "" ||
		strings.TrimSpace(start.AttemptID) == "" || strings.TrimSpace(start.OwnerID) == "" || start.Generation <= 0 || start.LeaseTTL <= 0 {
		return fmt.Errorf("stream lease renewal is invalid")
	}
	result, err := b.db.Exec(ctx, `
		UPDATE stream_delivery_leases
		SET lease_expires_at=clock_timestamp()+($5*INTERVAL '1 millisecond'), updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND owner_id=$4 AND generation=$6 AND state='active'
		  AND lease_expires_at>clock_timestamp()`,
		start.OrgID, start.RequestID, start.AttemptID, start.OwnerID, ceilDurationMilliseconds(start.LeaseTTL), start.Generation)
	if err != nil {
		return fmt.Errorf("renew stream delivery lease: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrStreamLeaseConflict
	}
	return nil
}

func (b *Billing) CheckpointStreamDelivery(ctx context.Context, checkpoint StreamDeliveryCheckpoint) (*StreamCheckpointResult, error) {
	checkpoint.RequestID = strings.TrimSpace(checkpoint.RequestID)
	checkpoint.AttemptID = strings.TrimSpace(checkpoint.AttemptID)
	checkpoint.OwnerID = strings.TrimSpace(checkpoint.OwnerID)
	checkpoint.PolicySetVersion = strings.TrimSpace(checkpoint.PolicySetVersion)
	if b == nil || b.db == nil || checkpoint.OrgID <= 0 || checkpoint.RequestID == "" || len(checkpoint.RequestID) > 64 ||
		checkpoint.AttemptID == "" || len(checkpoint.AttemptID) > 64 || checkpoint.OwnerID == "" || len(checkpoint.OwnerID) > 80 ||
		checkpoint.Generation <= 0 ||
		checkpoint.DeliveredBytes <= 0 || checkpoint.DeliveredChunks <= 0 || checkpoint.PromptTokens < 0 || checkpoint.CompletionTokens < 0 ||
		checkpoint.PolicySetVersion == "" || len(checkpoint.PolicySetVersion) > 80 || checkpoint.LeaseTTL <= 0 {
		return nil, fmt.Errorf("stream delivery checkpoint is invalid")
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin stream checkpoint: %w", err)
	}
	defer tx.Rollback(ctx)
	var attemptID, ownerID, state string
	var generation int64
	var leaseAlive bool
	if err := tx.QueryRow(ctx, `
		SELECT attempt_id,owner_id,generation,state,lease_expires_at>clock_timestamp()
		FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2 FOR UPDATE`, checkpoint.OrgID, checkpoint.RequestID,
	).Scan(&attemptID, &ownerID, &generation, &state, &leaseAlive); err != nil {
		return nil, fmt.Errorf("lock stream checkpoint lease: %w", err)
	}
	if attemptID != checkpoint.AttemptID || ownerID != checkpoint.OwnerID || generation != checkpoint.Generation || state != "active" || !leaseAlive {
		return nil, ErrStreamLeaseConflict
	}

	var previous StreamDeliveryCheckpoint
	var sequence int
	err = tx.QueryRow(ctx, `
		SELECT sequence,delivered_bytes,delivered_chunks,prompt_tokens,completion_tokens,policy_set_version
		FROM stream_delivery_checkpoints
		WHERE org_id=$1 AND request_id=$2 ORDER BY sequence DESC LIMIT 1`, checkpoint.OrgID, checkpoint.RequestID,
	).Scan(&sequence, &previous.DeliveredBytes, &previous.DeliveredChunks, &previous.PromptTokens,
		&previous.CompletionTokens, &previous.PolicySetVersion)
	if err == nil {
		if previous.DeliveredBytes == checkpoint.DeliveredBytes && previous.DeliveredChunks == checkpoint.DeliveredChunks &&
			previous.PromptTokens == checkpoint.PromptTokens && previous.CompletionTokens == checkpoint.CompletionTokens &&
			previous.PolicySetVersion == checkpoint.PolicySetVersion {
			result, err := tx.Exec(ctx, `UPDATE stream_delivery_leases
				SET lease_expires_at=clock_timestamp()+($6*INTERVAL '1 millisecond'),updated_at=NOW()
				WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND owner_id=$4 AND generation=$5
				  AND state='active' AND lease_expires_at>clock_timestamp()`,
				checkpoint.OrgID, checkpoint.RequestID, checkpoint.AttemptID, checkpoint.OwnerID,
				checkpoint.Generation, ceilDurationMilliseconds(checkpoint.LeaseTTL))
			if err != nil {
				return nil, err
			}
			if result.RowsAffected() != 1 {
				return nil, ErrStreamLeaseConflict
			}
			return &StreamCheckpointResult{Sequence: sequence, Idempotent: true}, tx.Commit(ctx)
		}
		if checkpoint.DeliveredBytes < previous.DeliveredBytes || checkpoint.DeliveredChunks < previous.DeliveredChunks ||
			checkpoint.PromptTokens < previous.PromptTokens || checkpoint.CompletionTokens < previous.CompletionTokens ||
			checkpoint.PolicySetVersion != previous.PolicySetVersion {
			return nil, fmt.Errorf("stream checkpoint is not monotonic")
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read previous stream checkpoint: %w", err)
	}
	sequence++
	if _, err := tx.Exec(ctx, `
		INSERT INTO stream_delivery_checkpoints (
			org_id,request_id,sequence,attempt_id,delivered_bytes,delivered_chunks,
			prompt_tokens,completion_tokens,policy_set_version,lease_generation
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		checkpoint.OrgID, checkpoint.RequestID, sequence, checkpoint.AttemptID,
		checkpoint.DeliveredBytes, checkpoint.DeliveredChunks, checkpoint.PromptTokens,
		checkpoint.CompletionTokens, checkpoint.PolicySetVersion, checkpoint.Generation); err != nil {
		return nil, fmt.Errorf("append stream checkpoint: %w", err)
	}
	result, err := tx.Exec(ctx, `UPDATE stream_delivery_leases
		SET lease_expires_at=clock_timestamp()+($6*INTERVAL '1 millisecond'),updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND owner_id=$4 AND generation=$5
		  AND state='active' AND lease_expires_at>clock_timestamp()`,
		checkpoint.OrgID, checkpoint.RequestID, checkpoint.AttemptID, checkpoint.OwnerID,
		checkpoint.Generation, ceilDurationMilliseconds(checkpoint.LeaseTTL))
	if err != nil {
		return nil, fmt.Errorf("renew stream lease from checkpoint: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, ErrStreamLeaseConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit stream checkpoint: %w", err)
	}
	return &StreamCheckpointResult{Sequence: sequence}, nil
}

func (b *Billing) FinishStreamDelivery(ctx context.Context, start StreamDeliveryStart, state, reason string) error {
	state = strings.TrimSpace(state)
	reason = strings.TrimSpace(reason)
	if b == nil || b.db == nil || start.OrgID <= 0 || strings.TrimSpace(start.RequestID) == "" ||
		strings.TrimSpace(start.AttemptID) == "" || strings.TrimSpace(start.OwnerID) == "" ||
		start.Generation <= 0 || (state != StreamLeaseStateCompleted && state != StreamLeaseStateInterrupted) || reason == "" || len(reason) > 80 {
		return fmt.Errorf("stream lease terminal state is invalid")
	}
	result, err := b.db.Exec(ctx, `
		UPDATE stream_delivery_leases
		SET state=$6,terminal_reason=$7,updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND owner_id=$4
		  AND generation=$5
		  AND state='active' AND lease_expires_at>clock_timestamp()`,
		start.OrgID, start.RequestID, start.AttemptID, start.OwnerID, start.Generation, state, reason)
	if err != nil {
		return fmt.Errorf("finish stream delivery lease: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var existingState, existingReason string
	if err := b.db.QueryRow(ctx, `SELECT state,terminal_reason FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3 AND owner_id=$4 AND generation=$5`,
		start.OrgID, start.RequestID, start.AttemptID, start.OwnerID, start.Generation).Scan(&existingState, &existingReason); err == nil &&
		existingState == state && existingReason == reason {
		return nil
	}
	return ErrStreamLeaseConflict
}

type expiredStream struct {
	StreamDeliveryCheckpoint
	HasCheckpoint         bool
	LatestDeliveryOutcome string
}

func (b *Billing) RecoverExpiredStreamDeliveries(ctx context.Context, limit int) (StreamRecoveryResult, error) {
	if b == nil || b.db == nil {
		return StreamRecoveryResult{}, fmt.Errorf("billing database is unavailable")
	}
	if limit <= 0 {
		limit = defaultSettlementRecoveryLimit
	}
	if limit > maxSettlementRecoveryLimit {
		limit = maxSettlementRecoveryLimit
	}
	recoveryOwner, err := newStreamRecoveryOwner()
	if err != nil {
		return StreamRecoveryResult{}, err
	}
	items, err := b.claimExpiredStreams(ctx, limit, recoveryOwner, streamRecoveryClaimTTL)
	if err != nil {
		return StreamRecoveryResult{}, err
	}
	result := StreamRecoveryResult{Claimed: len(items)}
	var recoveryErrors []error
	for _, item := range items {
		lease := StreamDeliveryStart{
			RequestID: item.RequestID, AttemptID: item.AttemptID,
			OwnerID: item.OwnerID, OrgID: item.OrgID, Generation: item.Generation, LeaseTTL: streamRecoveryClaimTTL,
		}
		if err := b.RenewStreamDelivery(ctx, lease); err != nil {
			result.Failed++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("renew claimed stream %s: %w", item.RequestID, err))
			continue
		}
		finish := func(state, reason string) error {
			return b.FinishStreamDelivery(ctx, StreamDeliveryStart{
				RequestID: item.RequestID, AttemptID: item.AttemptID,
				OwnerID: item.OwnerID, OrgID: item.OrgID, Generation: item.Generation,
			}, state, reason)
		}
		if item.LatestDeliveryOutcome != DeliveryOutcomeCompleted &&
			item.LatestDeliveryOutcome != DeliveryOutcomeInterrupted &&
			item.LatestDeliveryOutcome != DeliveryOutcomeWriteFailed {
			event := DeliveryEvent{
				RequestID: item.RequestID, AttemptID: item.AttemptID, OrgID: item.OrgID,
				Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
				DeliveredBytes: item.DeliveredBytes, DeliveredChunks: item.DeliveredChunks,
				HTTPStatus: 200, ErrorCode: "process_lost", StreamFence: streamFencePointer(lease.Fence()),
			}
			if err := b.RecordDelivery(ctx, event); err != nil {
				result.Failed++
				recoveryErrors = append(recoveryErrors, fmt.Errorf("record expired stream %s: %w", item.RequestID, err))
				continue
			}
		}
		if err := b.markExpiredStreamReadback(ctx, lease.Fence()); err != nil {
			result.Failed++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("persist expired stream readback %s: %w", item.RequestID, err))
			continue
		}
		if item.LatestDeliveryOutcome == DeliveryOutcomeCompleted {
			if err := finish(StreamLeaseStateCompleted, "completed_receipt_recovered"); err != nil {
				result.Failed++
				recoveryErrors = append(recoveryErrors, fmt.Errorf("finish completed stream %s: %w", item.RequestID, err))
				continue
			}
			result.Completed++
			continue
		}
		if !item.HasCheckpoint {
			if err := finish(StreamLeaseStateInterrupted, "process_lost"); err != nil {
				result.Failed++
				recoveryErrors = append(recoveryErrors, fmt.Errorf("finish zero-checkpoint stream %s: %w", item.RequestID, err))
				continue
			}
			result.ProviderCostHeld++
			continue
		}
		settle := SettleRequest{
			RequestID: item.RequestID, AttemptID: item.AttemptID, OrgID: item.OrgID,
			StreamFence:  streamFencePointer(lease.Fence()),
			PromptTokens: item.PromptTokens, UncachedPromptTokens: item.PromptTokens,
			CompletionTokens:     item.CompletionTokens,
			UsageSource:          metering.UsageSourceCounterV2,
			UsageAuthority:       metering.UsageAuthorityGatewayCounter,
			UsagePurpose:         metering.UsagePurposeCustomerCharge,
			UsageFinality:        metering.UsageFinalityEstimated,
			UsageContractVersion: metering.UsageContractVersion,
			UsageEstimateVersion: metering.GatewayCounterEstimateVersionV2,
		}
		if err := b.PersistSettlementUsage(ctx, settle); err != nil {
			if errors.Is(err, ErrProviderFinalUsageRequired) {
				// Checkpoints prove disclosure, not provider-final charge usage.
				// Retain the pending reservation without a pricing payload or debit.
				if finishErr := finish(StreamLeaseStateInterrupted, "provider_final_usage_missing"); finishErr != nil {
					result.Failed++
					recoveryErrors = append(recoveryErrors, finishErr)
					continue
				}
				result.ProviderCostHeld++
				continue
			}
			result.Failed++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("persist expired stream %s: %w", item.RequestID, err))
			continue
		}
		settled, settleErr := b.Settle(ctx, settle)
		if settleErr != nil || settled == nil || !settled.Success {
			if settleErr == nil {
				settleErr = fmt.Errorf("settlement rejected: %v", settled)
			}
			result.Failed++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("settle expired stream %s: %w", item.RequestID, settleErr))
			continue
		}
		if err := finish(StreamLeaseStateInterrupted, "process_lost"); err != nil {
			result.Failed++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("finish settled stream %s: %w", item.RequestID, err))
			continue
		}
		result.Settled++
	}
	return result, errors.Join(recoveryErrors...)
}

func (b *Billing) claimExpiredStreams(ctx context.Context, limit int, ownerID string, leaseTTL time.Duration) ([]expiredStream, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" || len(ownerID) > 80 || leaseTTL <= 0 {
		return nil, fmt.Errorf("expired stream recovery claim is invalid")
	}
	rows, err := b.db.Query(ctx, `
		WITH candidates AS (
			SELECT org_id,request_id FROM stream_delivery_leases
			WHERE state='active' AND lease_expires_at<=clock_timestamp()
			ORDER BY lease_expires_at,org_id,request_id FOR UPDATE SKIP LOCKED LIMIT $1
		), claimed AS (
			UPDATE stream_delivery_leases l
			SET owner_id=$2,generation=l.generation+1,
			    lease_expires_at=clock_timestamp()+($3*INTERVAL '1 millisecond'),updated_at=NOW()
			FROM candidates c WHERE l.org_id=c.org_id AND l.request_id=c.request_id
			RETURNING l.org_id,l.request_id,l.attempt_id,l.owner_id,l.generation
		)
		SELECT c.org_id,c.request_id,c.attempt_id,c.owner_id,c.generation,
		       COALESCE(cp.delivered_bytes,0),COALESCE(cp.delivered_chunks,0),
		       COALESCE(cp.prompt_tokens,0),COALESCE(cp.completion_tokens,0),
		       COALESCE(cp.policy_set_version,''),cp.sequence IS NOT NULL,
		       COALESCE(dr.outcome,'')
		FROM claimed c
		LEFT JOIN LATERAL (
			SELECT * FROM stream_delivery_checkpoints
			WHERE org_id=c.org_id AND request_id=c.request_id
			ORDER BY sequence DESC LIMIT 1
		) cp ON TRUE
		LEFT JOIN LATERAL (
			SELECT outcome FROM request_delivery_receipts
			WHERE org_id=c.org_id AND request_id=c.request_id AND attempt_id=c.attempt_id
			ORDER BY sequence DESC LIMIT 1
		) dr ON TRUE`, limit, ownerID, ceilDurationMilliseconds(leaseTTL))
	if err != nil {
		return nil, fmt.Errorf("claim expired stream leases: %w", err)
	}
	defer rows.Close()
	items := make([]expiredStream, 0, limit)
	for rows.Next() {
		var item expiredStream
		if err := rows.Scan(&item.OrgID, &item.RequestID, &item.AttemptID, &item.OwnerID, &item.Generation,
			&item.DeliveredBytes, &item.DeliveredChunks, &item.PromptTokens, &item.CompletionTokens,
			&item.PolicySetVersion, &item.HasCheckpoint, &item.LatestDeliveryOutcome); err != nil {
			return nil, fmt.Errorf("scan expired stream lease: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func newStreamRecoveryOwner() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate stream recovery owner: %w", err)
	}
	return "stream-recovery-" + hex.EncodeToString(entropy[:]), nil
}

func ceilDurationMilliseconds(value time.Duration) int64 {
	return int64((value + time.Millisecond - 1) / time.Millisecond)
}

func normalizeStreamLeaseFence(fence StreamLeaseFence) (StreamLeaseFence, error) {
	fence.RequestID = strings.TrimSpace(fence.RequestID)
	fence.AttemptID = strings.TrimSpace(fence.AttemptID)
	fence.OwnerID = strings.TrimSpace(fence.OwnerID)
	if fence.OrgID <= 0 || fence.RequestID == "" || len(fence.RequestID) > 64 ||
		fence.AttemptID == "" || len(fence.AttemptID) > 64 || fence.OwnerID == "" || len(fence.OwnerID) > 80 ||
		fence.Generation <= 0 {
		return StreamLeaseFence{}, fmt.Errorf("stream lease fence is invalid")
	}
	return fence, nil
}

func requireCurrentStreamFenceTx(ctx context.Context, tx pgx.Tx, fence StreamLeaseFence) error {
	fence, err := normalizeStreamLeaseFence(fence)
	if err != nil {
		return err
	}
	var current int
	err = tx.QueryRow(ctx, `
		SELECT 1
		FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
		  AND owner_id=$4 AND generation=$5
		  AND state='active' AND lease_expires_at>clock_timestamp()
		FOR UPDATE`,
		fence.OrgID, fence.RequestID, fence.AttemptID, fence.OwnerID, fence.Generation,
	).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStreamLeaseConflict
	}
	if err != nil {
		return fmt.Errorf("verify stream delivery lease fence: %w", err)
	}
	return nil
}

func (b *Billing) markExpiredStreamReadback(ctx context.Context, fence StreamLeaseFence) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin expired stream readback: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := requireCurrentStreamFenceTx(ctx, tx, fence); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE request_idempotency
		SET state='failed',original_http_status=200,updated_at=NOW()
		WHERE org_id=$1 AND operation_id=$2 AND state='in_progress'`, fence.OrgID, fence.RequestID); err != nil {
		return fmt.Errorf("update expired stream readback: %w", err)
	}
	return tx.Commit(ctx)
}

func streamFencePointer(fence StreamLeaseFence) *StreamLeaseFence {
	return &fence
}
