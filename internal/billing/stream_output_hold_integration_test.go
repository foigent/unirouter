//go:build integration

package billing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func newStreamOutputHoldFixture(t *testing.T, requestID string) (*Billing, SettleRequest, StreamDeliveryStart) {
	t.Helper()
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	// This fixture freezes the accepted cap before any local stream disclosure.
	_, err := b.db.Exec(t.Context(), `UPDATE freeze_idempotency SET reserved_completion_tokens=100 WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	start := StreamDeliveryStart{OrgID: orgID, RequestID: requestID, AttemptID: attemptID, OwnerID: "output-hold-test", Generation: 1, LeaseTTL: time.Minute}
	require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
	req := p6f41ProviderUsage(requestID, attemptID, orgID)
	req.CompletionTokens = 101
	req.UsagePurpose = metering.UsagePurposeProviderEvidence
	req.StreamFence = testStreamFence(start)
	return b, req, start
}

func TestStreamOutputHoldPreservesKnownUsageWithoutSettlementIntegration(t *testing.T) {
	b, req, start := newStreamOutputHoldFixture(t, "op_stream_overrun")
	_, err := b.CheckpointStreamDelivery(t.Context(), StreamDeliveryCheckpoint{
		OrgID: req.OrgID, RequestID: req.RequestID, AttemptID: req.AttemptID,
		OwnerID: start.OwnerID, Generation: start.Generation, DeliveredBytes: 128,
		DeliveredChunks: 1, PromptTokens: 20, CompletionTokens: 32, PolicySetVersion: "test", LeaseTTL: time.Minute,
	})
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- b.HoldStreamOutput(t.Context(), req, 256, 2) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, b.readStreamOutputHold(t.Context(), req, 256, 2))
	var purpose, state, reason, certainty, leaseState, deliveryOutcome string
	var completion, receipts, terminalCount int
	var charged, deliveredBytes int64
	var nextAttemptNull, retry bool
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT usage_purpose,completion_tokens,status,settlement_pending_reason,settlement_next_attempt_at IS NULL FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&purpose, &completion, &state, &reason, &nextAttemptNull))
	require.Equal(t, string(metering.UsagePurposeProviderEvidence), purpose)
	require.Equal(t, 101, completion)
	require.Equal(t, "settlement_pending", state)
	require.Equal(t, OutputHoldReasonProviderResponseUnusable, reason)
	require.True(t, nextAttemptNull)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state,outcome_certainty,retry_authorized FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&state, &certainty, &retry))
	require.Equal(t, providerAttemptStateDeliveryFailed, state)
	require.Equal(t, providerAttemptCertaintyKnownUsage, certainty)
	require.False(t, retry)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&leaseState))
	require.Equal(t, StreamLeaseStateInterrupted, leaseState)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT delivery_outcome,delivered_bytes FROM request_idempotency WHERE org_id=$1 AND operation_id=$2`, req.OrgID, req.RequestID).Scan(&deliveryOutcome, &deliveredBytes))
	require.Equal(t, DeliveryOutcomeInterrupted, deliveryOutcome)
	require.EqualValues(t, 256, deliveredBytes)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2 AND outcome='interrupted'`, req.OrgID, req.RequestID).Scan(&receipts))
	require.Equal(t, 1, receipts)
	charge := req
	charge.UsagePurpose = metering.UsagePurposeCustomerCharge
	_, err = b.Settle(t.Context(), charge)
	require.Error(t, err)
	require.Error(t, b.PersistSettlementUsage(t.Context(), charge))
	_, err = b.db.Exec(t.Context(), `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 hour' WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID)
	require.NoError(t, err)
	streams, err := b.RecoverExpiredStreamDeliveries(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, streams.Claimed, "committed final usage cannot be replaced by checkpoint recovery")
	settlements, err := b.RecoverPendingSettlements(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, settlements.Claimed)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent_micros FROM wallets WHERE org_id=$1`, req.OrgID).Scan(&charged))
	require.Zero(t, charged)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, req.OrgID, req.RequestID).Scan(&terminalCount))
	require.Zero(t, terminalCount)
	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{OrgID: req.OrgID, RequestID: req.RequestID, AttemptID: req.AttemptID, Action: ProviderReconciliationCollect, ActorUserID: 77, Reason: "must not collect provider-only evidence", EvidenceRef: "test://overrun"})
	require.ErrorIs(t, err, ErrProviderReconciliationUnsafe)
	result, err := b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{OrgID: req.OrgID, RequestID: req.RequestID, AttemptID: req.AttemptID, Action: ProviderReconciliationSubsidy, ActorUserID: 77, Reason: "explicitly resolve supplier overrun", EvidenceRef: "test://overrun"})
	require.NoError(t, err)
	require.Equal(t, "0", result.CustomerChargedMicros)
}

func TestStreamOutputHoldRejectsChangedUsageCountsAndFenceIntegration(t *testing.T) {
	b, req, start := newStreamOutputHoldFixture(t, "op_stream_hold_replay")
	require.NoError(t, b.HoldStreamOutput(t.Context(), req, 128, 1))
	for _, mutate := range []func(*SettleRequest){
		func(r *SettleRequest) { r.CompletionTokens++ },
		func(r *SettleRequest) { r.StreamFence.OwnerID = "stale-owner" },
		func(r *SettleRequest) { r.StreamFence.Generation++ },
	} {
		changed := req
		changed.StreamFence = testStreamFence(start)
		mutate(&changed)
		require.Error(t, b.HoldStreamOutput(t.Context(), changed, 128, 1))
	}
	require.Error(t, b.HoldStreamOutput(t.Context(), req, 129, 1))
	require.Error(t, b.HoldStreamOutput(t.Context(), req, 128, 2))
	require.Error(t, b.HoldStreamOutput(t.Context(), req, 0, 1))
	require.NoError(t, b.HoldStreamOutput(t.Context(), req, 128, 1))
}

// The real DB transaction commits or rolls back, but its acknowledgement is
// replaced with an error, exercising the production ambiguity readback path.
type streamOutputHoldLostAck struct {
	pgx.Tx
	committed bool
}

func (tx streamOutputHoldLostAck) Commit(ctx context.Context) error {
	var err error
	if tx.committed {
		err = tx.Tx.Commit(ctx)
	} else {
		err = tx.Tx.Rollback(ctx)
	}
	if err != nil {
		return err
	}
	return errors.New("injected lost commit acknowledgement")
}

func TestStreamOutputHoldRollbackAndCommitReadbackIntegration(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "acknowledgement_lost"}[commit], func(t *testing.T) {
			b, req, _ := newStreamOutputHoldFixture(t, "op_stream_hold_commit")
			tx, err := b.db.Begin(t.Context())
			require.NoError(t, err)
			require.NoError(t, b.holdStreamOutputTx(t.Context(), tx, req, 128, 1))
			if commit {
				require.NoError(t, b.commitStreamOutputHold(t.Context(), streamOutputHoldLostAck{Tx: tx, committed: true}, req, 128, 1))
				require.NoError(t, b.readStreamOutputHold(t.Context(), req, 128, 1), "exact readback proves the committed state independently of its acknowledgement")
			} else {
				require.ErrorContains(t, b.commitStreamOutputHold(t.Context(), streamOutputHoldLostAck{Tx: tx}, req, 128, 1), "injected lost commit acknowledgement")
				require.Error(t, b.readStreamOutputHold(t.Context(), req, 128, 1), "rolled-back usage is not durable provider evidence")
				var state string
				var payloadReady bool
				require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&state))
				require.Equal(t, "active", state)
				require.NoError(t, b.db.QueryRow(t.Context(), `SELECT settlement_payload_ready_at IS NOT NULL FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&payloadReady))
				require.False(t, payloadReady)
				require.NoError(t, b.HoldStreamOutput(t.Context(), req, 128, 1), "retrying the same local transaction is safe")
			}
		})
	}
}

func TestStreamOutputHoldRequiresFinalOverrunAndLiveOwnerIntegration(t *testing.T) {
	for _, variant := range []string{"not_overrun", "estimate", "expired", "zero_disclosure", "partial_first_frame"} {
		t.Run(variant, func(t *testing.T) {
			b, req, _ := newStreamOutputHoldFixture(t, "op_hold_"+variant)
			switch variant {
			case "not_overrun":
				req.CompletionTokens = 100
			case "estimate":
				req.UsageSource = "counter_v2"
				req.UsageAuthority = metering.UsageAuthorityGatewayCounter
				req.UsageFinality = metering.UsageFinalityEstimated
			case "expired":
				_, err := b.db.Exec(t.Context(), `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID)
				require.NoError(t, err)
			}
			if variant == "partial_first_frame" {
				require.NoError(t, b.HoldStreamOutput(t.Context(), req, 7, 0))
				require.NoError(t, b.readStreamOutputHold(t.Context(), req, 7, 0))
			} else if variant == "zero_disclosure" {
				require.NoError(t, b.HoldStreamOutput(t.Context(), req, 0, 0))
				require.NoError(t, b.readStreamOutputHold(t.Context(), req, 0, 0), "write-start does not invent disclosure")
			} else {
				require.Error(t, b.HoldStreamOutput(t.Context(), req, 128, 1))
				var evidenceRows int
				require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2 AND settlement_payload_ready_at IS NOT NULL`, req.OrgID, req.RequestID).Scan(&evidenceRows))
				require.Zero(t, evidenceRows)
			}
		})
	}
}

func TestStreamOutputHoldReceiptFailureRollsBackWholeTransitionIntegration(t *testing.T) {
	b, req, _ := newStreamOutputHoldFixture(t, "op_stream_hold_receipt_failure")
	_, err := b.db.Exec(t.Context(), `CREATE FUNCTION fail_overrun_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.error_code='provider_output_limit_exceeded' THEN RAISE EXCEPTION 'injected receipt failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER fail_overrun_receipt BEFORE INSERT ON request_delivery_receipts FOR EACH ROW EXECUTE FUNCTION fail_overrun_receipt()`)
	require.NoError(t, err)
	err = b.HoldStreamOutput(t.Context(), req, 128, 1)
	require.ErrorContains(t, err, "injected receipt failure")
	require.Error(t, b.readStreamOutputHold(t.Context(), req, 128, 1))
	var status, state string
	var payloadReady bool
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT settlement_pending_reason,settlement_payload_ready_at IS NOT NULL FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&status, &payloadReady))
	require.Equal(t, "billable_output", status)
	require.False(t, payloadReady)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, req.OrgID, req.RequestID).Scan(&state))
	require.Equal(t, ProviderAttemptOutcomeResponseAccepted, state)
}
