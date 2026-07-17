//go:build integration

package billing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func TestV1DeliveryAttemptBindingRejectsSubstitutionAndEmptyIdentity(t *testing.T) {
	const requestID = "v1-delivery-attempt-substitution"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	const otherAttempt = "v1-delivery-attempt-other"
	_, err := b.db.Exec(t.Context(), `
		INSERT INTO billing_provider_attempts (
			org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
			route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty
		) VALUES ($1,$2,$3,11,12,13,1,1,'/v1/chat/completions','test','other-channel',
			'response_accepted','pending')`, orgID, requestID, otherAttempt)
	require.NoError(t, err)

	usage := p6f41ProviderUsage(requestID, attemptID, orgID)
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), usage)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))

	err = b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: otherAttempt, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 64, DeliveredChunks: 1, HTTPStatus: 200,
	})
	require.ErrorIs(t, err, ErrDeliveryAttemptConflict)
	err = b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 64, DeliveredChunks: 1, HTTPStatus: 200,
	})
	require.Error(t, err)

	var binding, otherState string
	var receiptCount, terminalCount int
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT delivery_attempt_id FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&binding))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, otherAttempt).Scan(&otherState))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&receiptCount))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&terminalCount))
	require.Equal(t, attemptID, binding)
	require.Equal(t, ProviderAttemptOutcomeResponseAccepted, otherState)
	require.Equal(t, 1, receiptCount)
	require.Zero(t, terminalCount)

	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 64, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	settled, err := b.Settle(t.Context(), usage)
	require.NoError(t, err)
	require.True(t, settled.Success, "the bound attempt must retain legitimate delivery and settlement behavior")
}

func TestV1ProviderBackedFrozenReservationCannotSettleWithoutAttempt(t *testing.T) {
	const requestID = "v1-provider-settle-missing-attempt"
	b, orgID, _, _ := newP6F41DeliveryFixture(t, requestID)
	usage := p6f41ProviderUsage(requestID, "", orgID)
	_, err := b.Settle(t.Context(), usage)
	require.ErrorIs(t, err, ErrDeliveryAttemptConflict)
	var terminalCount int
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&terminalCount))
	require.Zero(t, terminalCount)
}

func TestV1StreamFenceUsesWallClockAfterTransactionStart(t *testing.T) {
	const requestID = "v1-stream-fence-wall-clock-expiry"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "wall-clock-owner",
		OrgID: orgID, Generation: 1, LeaseTTL: time.Second,
	}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))

	// Establish the transaction timestamp while the lease is still alive. A
	// NOW()-based fence query later in this transaction would continue to see
	// this stale time even after the wall-clock lease deadline has passed.
	tx, err := b.db.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	var transactionStartedAt, observedAt, leaseExpiresAt time.Time
	require.NoError(t, tx.QueryRow(t.Context(), `
		SELECT NOW(),clock_timestamp(),lease_expires_at
		FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&transactionStartedAt, &observedAt, &leaseExpiresAt))
	require.True(t, leaseExpiresAt.After(observedAt), "test transaction must begin before lease expiry")

	waitForExpiry := time.Until(leaseExpiresAt.Add(150 * time.Millisecond))
	if waitForExpiry > 0 {
		time.Sleep(waitForExpiry)
	}
	var transactionClockStillAlive, wallClockExpired bool
	require.NoError(t, tx.QueryRow(t.Context(), `
		SELECT NOW() < lease_expires_at, clock_timestamp() >= lease_expires_at
		FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&transactionClockStillAlive, &wallClockExpired))
	require.True(t, transactionClockStillAlive,
		"the regression setup must prove transaction_timestamp is stale across expiry")
	require.True(t, wallClockExpired)
	require.True(t, transactionStartedAt.Before(leaseExpiresAt))

	require.ErrorIs(t, requireCurrentStreamFenceTx(t.Context(), tx, start.Fence()), ErrStreamLeaseConflict,
		"an expired lease must not remain authoritative because its transaction began before expiry")
}

func TestV1CheckpointCannotReviveLeaseExpiredAfterInitialFenceCheck(t *testing.T) {
	const requestID = "v1-checkpoint-expiry-after-fence"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	_, err := b.db.Exec(t.Context(), `
		CREATE FUNCTION v1_delay_checkpoint_past_lease_expiry() RETURNS trigger
		LANGUAGE plpgsql AS $function$
		BEGIN
			PERFORM pg_sleep(2.2);
			RETURN NEW;
		END
		$function$;
		CREATE TRIGGER v1_delay_checkpoint_past_lease_expiry
		BEFORE INSERT ON stream_delivery_checkpoints
		FOR EACH ROW EXECUTE FUNCTION v1_delay_checkpoint_past_lease_expiry()`)
	require.NoError(t, err)

	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "checkpoint-expiry-owner",
		OrgID: orgID, Generation: 1, LeaseTTL: 2 * time.Second,
	}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	startedAt := time.Now()
	_, err = b.CheckpointStreamDelivery(t.Context(), StreamDeliveryCheckpoint{
		RequestID: requestID, AttemptID: attemptID, OwnerID: start.OwnerID,
		OrgID: orgID, Generation: start.Generation,
		DeliveredBytes: 64, DeliveredChunks: 1, PromptTokens: 8, CompletionTokens: 1,
		PolicySetVersion: "checkpoint-expiry-v1", LeaseTTL: time.Minute,
	})
	require.ErrorIs(t, err, ErrStreamLeaseConflict)
	require.GreaterOrEqual(t, time.Since(startedAt), 2*time.Second,
		"the delayed insert proves the initial alive check succeeded before the lease expired")

	var checkpointCount int
	var leaseExpired bool
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM stream_delivery_checkpoints
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&checkpointCount))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT lease_expires_at<=clock_timestamp()
		FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&leaseExpired))
	require.Zero(t, checkpointCount, "the checkpoint insert and attempted renewal must roll back atomically")
	require.True(t, leaseExpired, "the expired lease must not be revived by the final checkpoint update")
}

func TestV1StreamGenerationFencesStaleOwnerAcrossRecoveryRace(t *testing.T) {
	const requestID = "v1-stream-generation-race"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "live-owner-generation-one",
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	require.NoError(t, b.RenewStreamDelivery(t.Context(), start))
	var generation int64
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT generation FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&generation))
	require.Equal(t, int64(1), generation, "renewal must not mint a new generation")
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200,
		StreamFence: testStreamFence(start),
	}))
	_, err := b.CheckpointStreamDelivery(t.Context(), StreamDeliveryCheckpoint{
		RequestID: requestID, AttemptID: attemptID, OwnerID: start.OwnerID,
		OrgID: orgID, Generation: start.Generation,
		DeliveredBytes: 128, DeliveredChunks: 1, PromptTokens: 12, CompletionTokens: 2,
		PolicySetVersion: "stream-fence-test-v1", LeaseTTL: time.Minute,
	})
	require.NoError(t, err)
	usage := SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		StreamFence:  testStreamFence(start),
		PromptTokens: 12, UncachedPromptTokens: 12, CompletionTokens: 2,
		UsageSource:          metering.UsageSourceCounterV2,
		UsageAuthority:       metering.UsageAuthorityGatewayCounter,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityEstimated,
		UsageContractVersion: metering.UsageContractVersion,
		UsageEstimateVersion: metering.GatewayCounterEstimateVersionV2,
	}
	require.NoError(t, b.PersistSettlementUsage(t.Context(), usage))
	require.NoError(t, func() error {
		_, execErr := b.db.Exec(t.Context(), `UPDATE stream_delivery_leases
			SET lease_expires_at=NOW()-INTERVAL '1 second'
			WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
		return execErr
	}())

	// Hold the reservation row so the live writer begins a real PostgreSQL race
	// but cannot reach the lease check until recovery has atomically claimed it.
	blocker, err := b.db.Begin(t.Context())
	require.NoError(t, err)
	defer blocker.Rollback(t.Context())
	var locked string
	require.NoError(t, blocker.QueryRow(t.Context(), `SELECT request_id FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2 FOR UPDATE`, orgID, requestID).Scan(&locked))

	staleStarted := make(chan struct{})
	staleResult := make(chan error, 1)
	go func() {
		close(staleStarted)
		staleResult <- b.RecordDelivery(context.Background(), DeliveryEvent{
			RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
			Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
			DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200,
			ErrorCode: "stale-live-writer", StreamFence: testStreamFence(start),
		})
	}()
	<-staleStarted
	claimed, err := b.claimExpiredStreams(t.Context(), 1, "recovery-owner-generation-two", time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, int64(2), claimed[0].Generation, "recovery claim, not renew, must increment generation")
	require.NoError(t, blocker.Commit(t.Context()))
	require.ErrorIs(t, <-staleResult, ErrStreamLeaseConflict)

	require.ErrorIs(t, b.PersistSettlementUsage(t.Context(), usage), ErrStreamLeaseConflict)
	_, err = b.Settle(t.Context(), usage)
	require.ErrorIs(t, err, ErrStreamLeaseConflict, "a stale owner cannot turn its earlier payload into a terminal charge")
	noFence := usage
	noFence.StreamFence = nil
	_, err = b.Settle(t.Context(), noFence)
	require.ErrorIs(t, err, ErrStreamLeaseConflict, "omitting the fence is not a live-path bypass")

	var receiptCount int
	var payloadCount int
	var terminals int
	var storedOwner string
	var storedGeneration int64
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&receiptCount))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2 AND settlement_payload_ready_at IS NOT NULL`, orgID, requestID).Scan(&payloadCount))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT settlement_stream_owner_id,settlement_stream_generation
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&storedOwner, &storedGeneration))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&terminals))
	require.Equal(t, 2, receiptCount, "stale terminal delivery must have zero effects")
	require.Equal(t, 1, payloadCount, "stale usage replay must not replace the immutable payload")
	require.Equal(t, start.OwnerID, storedOwner)
	require.Equal(t, start.Generation, storedGeneration)
	require.Zero(t, terminals, "stale settlement must have zero financial effects")

	current := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: claimed[0].OwnerID,
		OrgID: orgID, Generation: claimed[0].Generation, LeaseTTL: time.Minute,
	}
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200,
		ErrorCode: "recovered", StreamFence: testStreamFence(current),
	}))
	currentUsage := usage
	currentUsage.StreamFence = testStreamFence(current)
	require.NoError(t, b.PersistSettlementUsage(t.Context(), currentUsage), "the current owner may reuse the immutable prior-generation payload")
	require.ErrorIs(t, b.markExpiredStreamReadback(t.Context(), start.Fence()), ErrStreamLeaseConflict)
	var idempotencyState string
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM request_idempotency
		WHERE org_id=$1 AND operation_id=$2`, orgID, requestID).Scan(&idempotencyState))
	require.Equal(t, "in_progress", idempotencyState, "stale readback finalization must have zero effects")
	require.NoError(t, b.markExpiredStreamReadback(t.Context(), current.Fence()))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM request_idempotency
		WHERE org_id=$1 AND operation_id=$2`, orgID, requestID).Scan(&idempotencyState))
	require.Equal(t, "failed", idempotencyState)

	require.NoError(t, func() error {
		_, execErr := b.db.Exec(t.Context(), `UPDATE stream_delivery_leases
			SET lease_expires_at=NOW()-INTERVAL '1 second'
			WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
		return execErr
	}())
	_, err = b.Settle(t.Context(), currentUsage)
	require.ErrorIs(t, err, ErrStreamLeaseConflict, "even the current generation must not author a terminal after expiry")
	_, err = b.Settle(t.Context(), noFence)
	require.ErrorIs(t, err, ErrStreamLeaseConflict)
	forgedRecovery := noFence
	forgedRecovery.settlementRecovery = &settlementRecoveryAuthority{
		ClaimAttempt: 1, StreamOwnerID: storedOwner, StreamGeneration: storedGeneration,
	}
	_, err = b.Settle(t.Context(), forgedRecovery)
	require.ErrorIs(t, err, ErrStreamLeaseConflict,
		"even package-internal recovery authority must be backed by the current durable claim")
	recovered, err := b.RecoverPendingSettlements(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, SettlementRecoveryResult{Claimed: 1, Settled: 1}, recovered,
		"only generic recovery may settle an immutable fenced payload after lease expiry")
	require.ErrorIs(t, b.FinishStreamDelivery(t.Context(), start, StreamLeaseStateInterrupted, "stale-finish"), ErrStreamLeaseConflict)
	var leaseOwner, leaseState string
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT owner_id,generation,state FROM stream_delivery_leases
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&leaseOwner, &generation, &leaseState))
	require.Equal(t, current.OwnerID, leaseOwner)
	require.Equal(t, current.Generation, generation)
	require.Equal(t, "active", leaseState, "stale terminal-state publication must have zero effects")

	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&terminals))
	require.Equal(t, 1, terminals)
}

func TestV1GenericRecoveryRejectsHistoricalUnfencedStreamPayload(t *testing.T) {
	const requestID = "v1-historical-unfenced-stream-payload"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	start := StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: "historical-stream-owner",
		OrgID: orgID, Generation: 1, LeaseTTL: time.Minute,
	}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 64, DeliveredChunks: 1, HTTPStatus: 200,
		StreamFence: testStreamFence(start),
	}))
	usage := SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		StreamFence:  testStreamFence(start),
		PromptTokens: 8, UncachedPromptTokens: 8, CompletionTokens: 1,
		UsageSource:          metering.UsageSourceCounterV2,
		UsageAuthority:       metering.UsageAuthorityGatewayCounter,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityEstimated,
		UsageContractVersion: metering.UsageContractVersion,
		UsageEstimateVersion: metering.GatewayCounterEstimateVersionV2,
	}
	require.NoError(t, b.PersistSettlementUsage(t.Context(), usage))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeInterrupted,
		DeliveredBytes: 64, DeliveredChunks: 1, HTTPStatus: 200,
		ErrorCode: "historical-cutover", StreamFence: testStreamFence(start),
	}))

	// Simulate a pre-C1.3 payload retained through the additive migration. The
	// migration intentionally does not invent an owner/generation for history.
	tx, err := b.db.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	require.NoError(t, func() error {
		if _, execErr := tx.Exec(t.Context(), `ALTER TABLE freeze_idempotency
			DISABLE TRIGGER settlement_stream_fence_immutable`); execErr != nil {
			return execErr
		}
		if _, execErr := tx.Exec(t.Context(), `UPDATE freeze_idempotency
			SET settlement_stream_owner_id=NULL,settlement_stream_generation=NULL
			WHERE org_id=$1 AND request_id=$2`, orgID, requestID); execErr != nil {
			return execErr
		}
		if _, execErr := tx.Exec(t.Context(), `ALTER TABLE freeze_idempotency
			ENABLE TRIGGER settlement_stream_fence_immutable`); execErr != nil {
			return execErr
		}
		return tx.Commit(t.Context())
	}())
	require.NoError(t, func() error {
		_, execErr := b.db.Exec(t.Context(), `UPDATE stream_delivery_leases
			SET lease_expires_at=NOW()-INTERVAL '1 second'
			WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
		return execErr
	}())

	noFence := usage
	noFence.StreamFence = nil
	_, err = b.Settle(t.Context(), noFence)
	require.ErrorIs(t, err, ErrStreamLeaseConflict)
	recovered, err := b.RecoverPendingSettlements(t.Context(), 1)
	require.ErrorIs(t, err, ErrStreamLeaseConflict)
	require.Equal(t, SettlementRecoveryResult{Claimed: 1, Failed: 1}, recovered)

	var terminals int
	var status string
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&terminals))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&status))
	require.Zero(t, terminals, "unfenced historical stream evidence must never be charged automatically")
	require.Equal(t, "settlement_pending", status)
}
