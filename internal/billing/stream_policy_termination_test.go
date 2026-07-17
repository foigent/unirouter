package billing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func freezeStreamReservation(t *testing.T, b *Billing, orgID int64, model, requestID string, reservedCompletion int) *FreezeResult {
	t.Helper()
	result, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100 + reservedCompletion, ReservedCompletionTokens: reservedCompletion,
		SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, result.Success, "%s: %s", result.ErrorCode, result.ErrorDetail)
	return result
}

func testStreamFence(start StreamDeliveryStart) *StreamLeaseFence {
	fence := start.Fence()
	return &fence
}

func TestP6F11ConcurrentReservationExtensionChargesOnce(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10_000, 0)
	b := NewBilling(pool, nil)
	const requestID = "op_p6f11_extend_concurrent"
	initial := freezeStreamReservation(t, b, orgID, model, requestID, 100)

	request := ExtendReservationRequest{
		RequestID: requestID, OrgID: orgID, Sequence: 1,
		AdditionalCompletionTokens: 1_000, TargetCompletionTokens: 1_100,
	}
	start := make(chan struct{})
	results := make(chan *ExtendReservationResult, 32)
	errs := make(chan error, 32)
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := b.ExtendReservation(context.Background(), request)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var extensionAmount int64
	for result := range results {
		require.NotNil(t, result)
		require.True(t, result.Success, "%s: %s", result.ErrorCode, result.ErrorDetail)
		if extensionAmount == 0 {
			extensionAmount = result.AmountMicros
		}
		require.Equal(t, extensionAmount, result.AmountMicros)
	}
	require.Positive(t, extensionAmount)

	var balanceMicros, frozenMicros int64
	var reservedCompletion, extensionSequence, extensionRows int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT balance_micros,frozen_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balanceMicros, &frozenMicros))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT reserved_completion_tokens,reservation_extension_sequence
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&reservedCompletion, &extensionSequence))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_reservation_extensions
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&extensionRows))
	require.Equal(t, testMicros(10_000)-initial.AmountMicros-extensionAmount, balanceMicros)
	require.Equal(t, initial.AmountMicros+extensionAmount, frozenMicros)
	require.Equal(t, 1_100, reservedCompletion)
	require.Equal(t, 1, extensionSequence)
	require.Equal(t, 1, extensionRows)
	_, err := pool.Exec(t.Context(), `UPDATE billing_reservation_extensions SET error_code='tampered'
		WHERE org_id=$1 AND request_id=$2 AND sequence=1`, orgID, requestID)
	require.ErrorContains(t, err, "append-only")

	conflict := request
	conflict.AdditionalCompletionTokens = 999
	_, err = b.ExtendReservation(t.Context(), conflict)
	require.ErrorContains(t, err, "conflicts with its durable receipt")
}

func TestP6F11RejectedReservationExtensionIsDurablyIdempotent(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10, 0)
	b := NewBilling(pool, nil)
	const requestID = "op_p6f11_extend_rejected"
	initial := freezeStreamReservation(t, b, orgID, model, requestID, 100)
	require.Equal(t, testMicros(10), initial.AmountMicros)

	request := ExtendReservationRequest{
		RequestID: requestID, OrgID: orgID, Sequence: 1,
		AdditionalCompletionTokens: 1_000, TargetCompletionTokens: 1_100,
	}
	for range 2 {
		result, err := b.ExtendReservation(t.Context(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.Success)
		require.Equal(t, "insufficient_balance", result.ErrorCode)
	}
	var balanceMicros, frozenMicros int64
	var reservedCompletion, extensionSequence, extensionRows int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT balance_micros,frozen_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balanceMicros, &frozenMicros))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT reserved_completion_tokens,reservation_extension_sequence
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&reservedCompletion, &extensionSequence))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_reservation_extensions
		WHERE org_id=$1 AND request_id=$2 AND status='rejected'`, orgID, requestID,
	).Scan(&extensionRows))
	require.Zero(t, balanceMicros)
	require.Equal(t, initial.AmountMicros, frozenMicros)
	require.Equal(t, 100, reservedCompletion)
	require.Equal(t, 1, extensionSequence)
	require.Equal(t, 1, extensionRows)
}

func TestP6F11ReservationExtensionRequiresActiveStreamState(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10_000, 0)
	b := NewBilling(pool, nil)

	const requestID = "op_p6f11_extend_active_stream"
	const attemptID = "att_p6f11_extend_active_stream"
	freezeStreamReservation(t, b, orgID, model, requestID, 100)
	beginAcceptedStreamAttempt(t, b, orgID, requestID, attemptID)
	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "active-stream-owner",
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200, StreamFence: testStreamFence(start),
	}))
	extension := ExtendReservationRequest{
		RequestID: requestID, OrgID: orgID, Sequence: 1,
		AdditionalCompletionTokens: 1_000, TargetCompletionTokens: 1_100,
	}
	result, err := b.ExtendReservation(t.Context(), extension)
	require.NoError(t, err)
	require.True(t, result.Success)

	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200, ErrorCode: "test_terminal", StreamFence: testStreamFence(start),
	}))
	result, err = b.ExtendReservation(t.Context(), ExtendReservationRequest{
		RequestID: requestID, OrgID: orgID, Sequence: 2,
		AdditionalCompletionTokens: 1_000, TargetCompletionTokens: 2_100,
	})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "reservation_not_extendable", result.ErrorCode)
}

func beginAcceptedStreamAttempt(t *testing.T, b *Billing, orgID int64, requestID, attemptID string) {
	t.Helper()
	start := providerAttemptFixture(requestID, attemptID, orgID, 1, 1)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), start))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
}

func TestP6F11ExpiredStreamRecoveryUsesLastCheckpointOnly(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10_000, 0)
	b := NewBilling(pool, nil)
	ownerID := fmt.Sprintf("test-owner-%d", time.Now().UnixNano())

	const checkpointedRequest = "op_p6f11_recover_checkpoint"
	const checkpointedAttempt = "att_p6f11_recover_checkpoint"
	freezeStreamReservation(t, b, orgID, model, checkpointedRequest, 100)
	beginAcceptedStreamAttempt(t, b, orgID, checkpointedRequest, checkpointedAttempt)
	checkpointedStart := StreamDeliveryStart{
		RequestID: checkpointedRequest, AttemptID: checkpointedAttempt, OwnerID: ownerID,
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), checkpointedStart))
	checkpoint := StreamDeliveryCheckpoint{
		RequestID: checkpointedRequest, AttemptID: checkpointedAttempt, OwnerID: ownerID,
		OrgID: orgID, Generation: checkpointedStart.Generation, DeliveredBytes: 512, DeliveredChunks: 3,
		PromptTokens: 20, CompletionTokens: 5, PolicySetVersion: "guardrail-test-v1",
		LeaseTTL: time.Minute,
	}
	firstCheckpoint, err := b.CheckpointStreamDelivery(t.Context(), checkpoint)
	require.NoError(t, err)
	require.Equal(t, 1, firstCheckpoint.Sequence)
	replayedCheckpoint, err := b.CheckpointStreamDelivery(t.Context(), checkpoint)
	require.NoError(t, err)
	require.True(t, replayedCheckpoint.Idempotent)
	require.Equal(t, firstCheckpoint.Sequence, replayedCheckpoint.Sequence)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: checkpointedRequest, AttemptID: checkpointedAttempt, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 512, DeliveredChunks: 3, HTTPStatus: 200, StreamFence: testStreamFence(checkpointedStart),
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: checkpointedRequest, AttemptID: checkpointedAttempt, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
		DeliveredBytes: 512, DeliveredChunks: 3, HTTPStatus: 200,
		ErrorCode: "settlement_usage_persist_failed", StreamFence: testStreamFence(checkpointedStart),
	}), "recovery must reuse a pre-existing terminal receipt instead of appending another")

	const uncheckpointedRequest = "op_p6f11_recover_no_checkpoint"
	const uncheckpointedAttempt = "att_p6f11_recover_no_checkpoint"
	freezeStreamReservation(t, b, orgID, model, uncheckpointedRequest, 100)
	beginAcceptedStreamAttempt(t, b, orgID, uncheckpointedRequest, uncheckpointedAttempt)
	require.NoError(t, b.BeginStreamDelivery(t.Context(), StreamDeliveryStart{
		RequestID: uncheckpointedRequest, AttemptID: uncheckpointedAttempt, OwnerID: ownerID,
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}))

	_, err = pool.Exec(t.Context(), `
		UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 second'
		WHERE org_id=$1 AND request_id IN ($2,$3)`, orgID, checkpointedRequest, uncheckpointedRequest)
	require.NoError(t, err)

	startRecovery := make(chan struct{})
	recoveryResults := make(chan StreamRecoveryResult, 16)
	recoveryErrors := make(chan error, 16)
	var recoveryWait sync.WaitGroup
	for range 16 {
		recoveryWait.Add(1)
		go func() {
			defer recoveryWait.Done()
			<-startRecovery
			result, recoverErr := b.RecoverExpiredStreamDeliveries(context.Background(), 10)
			recoveryResults <- result
			recoveryErrors <- recoverErr
		}()
	}
	close(startRecovery)
	recoveryWait.Wait()
	close(recoveryResults)
	close(recoveryErrors)
	var recovered StreamRecoveryResult
	for recoverErr := range recoveryErrors {
		require.NoError(t, recoverErr)
	}
	for result := range recoveryResults {
		recovered.Claimed += result.Claimed
		recovered.Settled += result.Settled
		recovered.ProviderCostHeld += result.ProviderCostHeld
		recovered.Failed += result.Failed
	}
	require.Equal(t, StreamRecoveryResult{Claimed: 2, Settled: 1, ProviderCostHeld: 1}, recovered)

	var checkpointedStatus, uncheckpointedStatus, uncheckpointedReason string
	var checkpointedPrompt, checkpointedCompletion int
	var uncheckpointedChargedMicros int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status,prompt_tokens,completion_tokens FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2`, orgID, checkpointedRequest,
	).Scan(&checkpointedStatus, &checkpointedPrompt, &checkpointedCompletion))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status,settlement_pending_reason,charged_cost_micros FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2`, orgID, uncheckpointedRequest,
	).Scan(&uncheckpointedStatus, &uncheckpointedReason, &uncheckpointedChargedMicros))
	require.Equal(t, "settled", checkpointedStatus)
	require.Equal(t, 20, checkpointedPrompt)
	require.Equal(t, 5, checkpointedCompletion)
	require.Equal(t, "settlement_pending", uncheckpointedStatus)
	require.Equal(t, pendingReasonProviderAttemptUnresolved, uncheckpointedReason)
	require.Zero(t, uncheckpointedChargedMicros, "no checkpoint means no provable customer usage and no customer charge")

	var zeroInterruptionReceipts int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2 AND outcome='interrupted'
		  AND delivered_bytes=0 AND delivered_chunks=0`, orgID, uncheckpointedRequest,
	).Scan(&zeroInterruptionReceipts))
	require.Equal(t, 1, zeroInterruptionReceipts)
	var checkpointedInterruptionReceipts int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2 AND outcome='interrupted'`, orgID, checkpointedRequest,
	).Scan(&checkpointedInterruptionReceipts))
	require.Equal(t, 1, checkpointedInterruptionReceipts,
		"recovery must not append a conflicting second terminal receipt")
	require.ErrorContains(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: uncheckpointedRequest, AttemptID: uncheckpointedAttempt, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
		DeliveredBytes: 0, DeliveredChunks: 1, HTTPStatus: 200,
	}), "both delivery counters")
}

func TestP6F11ExpiredRecoveryFinishesExistingCompletedReceipt(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10_000, 0)
	b := NewBilling(pool, nil)
	const requestID = "op_p6f11_recover_completed_receipt"
	const attemptID = "att_p6f11_recover_completed_receipt"
	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "completed-stream-owner",
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}
	freezeStreamReservation(t, b, orgID, model, requestID, 100)
	beginAcceptedStreamAttempt(t, b, orgID, requestID, attemptID)
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	_, err := b.CheckpointStreamDelivery(t.Context(), StreamDeliveryCheckpoint{
		RequestID: requestID, AttemptID: attemptID, OwnerID: start.OwnerID, OrgID: orgID,
		Generation: start.Generation, DeliveredBytes: 256, DeliveredChunks: 2, PromptTokens: 10, CompletionTokens: 4,
		PolicySetVersion: "guardrail-completed-v1", LeaseTTL: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200, StreamFence: testStreamFence(start),
	}))
	settle := SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		StreamFence:  testStreamFence(start),
		PromptTokens: 10, UncachedPromptTokens: 10, CompletionTokens: 4,
		UsageSource:          metering.UsageSourceCounterV2,
		UsageAuthority:       metering.UsageAuthorityGatewayCounter,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityEstimated,
		UsageContractVersion: metering.UsageContractVersion,
		UsageEstimateVersion: metering.GatewayCounterEstimateVersionV2,
	}
	require.NoError(t, b.PersistSettlementUsage(t.Context(), settle))
	settled, err := b.Settle(t.Context(), settle)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 256, DeliveredChunks: 2, HTTPStatus: 200, StreamFence: testStreamFence(start),
	}))
	_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases
		SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)

	result, err := b.RecoverExpiredStreamDeliveries(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, StreamRecoveryResult{Claimed: 1, Completed: 1}, result)
	var state, reason string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT state,terminal_reason FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&state, &reason))
	require.Equal(t, StreamLeaseStateCompleted, state)
	require.Equal(t, "completed_receipt_recovered", reason)
	var receiptCount int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&receiptCount))
	require.Equal(t, 3, receiptCount)
}

func TestP6F11ExpiredRecoveryClaimCanBeReclaimedAfterClaimerCrash(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10_000, 0)
	b := NewBilling(pool, nil)
	const requestID = "op_p6f11_recovery_claim_crash"
	const attemptID = "att_p6f11_recovery_claim_crash"
	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "original-stream-owner",
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}
	freezeStreamReservation(t, b, orgID, model, requestID, 100)
	beginAcceptedStreamAttempt(t, b, orgID, requestID, attemptID)
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	_, err := b.CheckpointStreamDelivery(t.Context(), StreamDeliveryCheckpoint{
		RequestID: requestID, AttemptID: attemptID, OwnerID: start.OwnerID, OrgID: orgID,
		Generation: start.Generation, DeliveredBytes: 256, DeliveredChunks: 2, PromptTokens: 10, CompletionTokens: 4,
		PolicySetVersion: "guardrail-recovery-v1", LeaseTTL: time.Minute,
	})
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases
		SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	require.ErrorIs(t, b.BeginStreamDelivery(t.Context(), start), ErrStreamLeaseConflict,
		"an expired original owner cannot resurrect its lease")
	require.ErrorIs(t, b.FinishStreamDelivery(t.Context(), start, StreamLeaseStateInterrupted, "late_owner"), ErrStreamLeaseConflict,
		"an expired original owner cannot publish a terminal state")

	claimed, err := b.claimExpiredStreams(t.Context(), 1, "stream-recovery-crashed-test", time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "stream-recovery-crashed-test", claimed[0].OwnerID)
	var state, owner string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT state,owner_id FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&state, &owner))
	require.Equal(t, "active", state, "claiming is not terminal until recovery completes")
	require.Equal(t, "stream-recovery-crashed-test", owner)

	result, err := b.RecoverExpiredStreamDeliveries(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, result.Claimed, "an unexpired recovery claim is exclusive")
	_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases
		SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	result, err = b.RecoverExpiredStreamDeliveries(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, StreamRecoveryResult{Claimed: 1, Settled: 1}, result)
	var terminalReason string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT state,terminal_reason FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&state, &terminalReason))
	require.Equal(t, StreamLeaseStateInterrupted, state)
	require.Equal(t, "process_lost", terminalReason)
}
