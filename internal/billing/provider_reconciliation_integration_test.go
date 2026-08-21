//go:build integration

package billing

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func TestP6F40CObservationProviderCostRefundCreatesReceiptWithoutFinancialMutationIntegration(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1_000, 0)
	b := NewBilling(pool, nil).
		WithPriceProvider(billingModeTestPriceProvider()).
		WithMode(BillingModeCostObservation)
	const requestID = "op_observation_provider_cost_refund"
	const attemptID = "att_observation_provider_cost_refund"

	frozen, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.Equal(t, BillingModeCostObservation, frozen.BillingMode)
	require.Zero(t, frozen.AmountMicros)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), ProviderAttemptStart{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		APIKeyID: 77, ChannelID: 88, ChannelKeyID: 99,
		RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions",
		Provider: "test", Channel: "observation-provider-cost",
	}))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	require.NoError(t, b.HoldNonStreamOutput(t.Context(), SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		PromptTokens: 53, UncachedPromptTokens: 53, CompletionTokens: 16,
		UsageSource:          metering.UsageSourceUpstream,
		UsageAuthority:       metering.UsageAuthorityProviderReported,
		UsagePurpose:         metering.UsagePurposeProviderEvidence,
		UsageFinality:        metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion,
		CacheUsageStatus:     cacheUsageStatusNotReported,
	}, OutputHoldReasonProviderResponseUnusable))

	var balanceBefore, frozenBefore, spentBefore int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT balance_micros,frozen_micros,total_spent_micros
		FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&balanceBefore, &frozenBefore, &spentBefore))
	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
		Action: ProviderReconciliationCollect, ActorUserID: 8001,
		Reason: "observation cannot collect", EvidenceRef: "case://observation-collect",
	})
	require.ErrorIs(t, err, ErrProviderReconciliationUnsafe)

	reconciliation := ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 8001,
		Reason:      "provider cost reviewed with no customer delivery",
		EvidenceRef: "case://observation-refund",
	}
	result, err := b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.NoError(t, err)
	require.Equal(t, BillingModeCostObservation, result.BillingMode)
	require.Zero(t, result.TerminalTransactionID)
	require.Positive(t, result.TerminalObservationReceiptID)
	require.Equal(t, "0", result.CustomerChargedMicros)
	require.Equal(t, "0", result.CustomerRefundMicros)
	require.Equal(t, "reconciled", result.State)

	replayed, err := b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.NoError(t, err)
	require.Equal(t, result, replayed)
	conflict := reconciliation
	conflict.EvidenceRef = "case://different-evidence"
	_, err = b.ReconcileProviderAttempt(t.Context(), conflict)
	require.ErrorIs(t, err, ErrProviderReconciliationConflict)

	var reservationStatus, reservationMode, attemptState, receiptStatus string
	var reservationReceiptID, receiptCalculated, receiptCharged int64
	var attemptTransactionID sql.NullInt64
	var financialResolved bool
	var balanceAfter, frozenAfter, spentAfter int64
	var transactionCount, receiptCount, evidenceCount, unresolvedCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status,billing_mode,terminal_observation_receipt_id
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&reservationStatus, &reservationMode, &reservationReceiptID))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT state,terminal_transaction_id,financial_resolved_at IS NOT NULL
		FROM billing_provider_attempts
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID,
	).Scan(&attemptState, &attemptTransactionID, &financialResolved))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT terminal_status,calculated_cost_micros,charged_cost_micros
		FROM billing_observation_receipts
		WHERE org_id=$1 AND request_id=$2 AND id=$3`, orgID, requestID, result.TerminalObservationReceiptID,
	).Scan(&receiptStatus, &receiptCalculated, &receiptCharged))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT balance_micros,frozen_micros,total_spent_micros
		FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&balanceAfter, &frozenAfter, &spentAfter))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&transactionCount))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM billing_observation_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&receiptCount))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM provider_cost_reconciliations WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&evidenceCount))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_provider_attempts
		WHERE org_id=$1 AND api_key_id=$2
		  AND state IN ('dispatch_started','response_accepted','outcome_unknown','response_unusable',
		                'funding_pending','delivery_pending','delivery_failed','output_withheld')`, orgID, int64(77),
	).Scan(&unresolvedCount))

	require.Equal(t, "cancelled", reservationStatus)
	require.Equal(t, string(BillingModeCostObservation), reservationMode)
	require.Equal(t, result.TerminalObservationReceiptID, reservationReceiptID)
	require.Equal(t, "reconciled", attemptState)
	require.False(t, attemptTransactionID.Valid)
	require.True(t, financialResolved)
	require.Equal(t, "cancelled", receiptStatus)
	require.Positive(t, receiptCalculated)
	require.Zero(t, receiptCharged)
	require.Equal(t, balanceBefore, balanceAfter)
	require.Equal(t, frozenBefore, frozenAfter)
	require.Equal(t, spentBefore, spentAfter)
	require.Zero(t, transactionCount)
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 2, evidenceCount)
	require.Zero(t, unresolvedCount)

	_, err = pool.Exec(t.Context(), `
		UPDATE provider_cost_reconciliations SET reason='mutated'
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.ErrorContains(t, err, "append-only")
}

func TestP6F40CCollectReplaysStoredAuthoritativeUsageWithoutCallerAmount(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_collect"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_collect", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	settleReq := SettleRequest{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		PromptTokens: 250_000, UsageSource: "upstream",
	}
	require.NoError(t, b.MarkBillableOutput(t.Context(), requestID, orgID))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 1, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	require.NoError(t, b.PersistSettlementUsage(t.Context(), settleReq))
	pending, err := b.Settle(t.Context(), settleReq)
	require.NoError(t, err)
	require.False(t, pending.Success)
	reconciliation := ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationCollect, ActorUserID: 9001,
		Reason: "authoritative usage reviewed", EvidenceRef: "case://collect-1",
	}
	_, err = b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.ErrorIs(t, err, ErrSettlementFundingPending)
	_, err = pool.Exec(t.Context(), `UPDATE wallets SET balance_micros = balance_micros + 4000000 WHERE org_id = $1`, orgID)
	require.NoError(t, err)

	result, err := b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.NoError(t, err)
	require.Equal(t, "5000000", result.CustomerChargedMicros)
	require.Equal(t, "0", result.CustomerRefundMicros)

	duplicate, err := b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.NoError(t, err)
	require.Equal(t, result.TerminalTransactionID, duplicate.TerminalTransactionID)

	var status, state string
	var events, unresolved int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&status))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, requestID, attempt.AttemptID).Scan(&state))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM provider_cost_reconciliations WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&events))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND api_key_id=$2
		 AND state IN ('dispatch_started','response_accepted','outcome_unknown','response_unusable','funding_pending')`,
		orgID, attempt.APIKeyID).Scan(&unresolved))
	require.Equal(t, "settled", status)
	require.Equal(t, providerAttemptStateSettled, state)
	require.Equal(t, 2, events)
	require.Zero(t, unresolved)
}

func TestP6F40CRefundReleasesOnlyExistingHoldAndConflictingActionFails(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_refund"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_refund", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeUnknown, FailureKind: "transport",
	}))

	reconciliation := ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 9002,
		Reason: "provider confirmed no customer delivery", EvidenceRef: "case://refund-1",
	}
	result, err := b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.NoError(t, err)
	require.Equal(t, "1000000", result.CustomerRefundMicros)
	require.Equal(t, "0", result.CustomerChargedMicros)
	require.Equal(t, "reconciled", result.State)
	replayed, err := b.ReconcileProviderAttempt(t.Context(), reconciliation)
	require.NoError(t, err)
	require.Equal(t, result, replayed)

	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationSubsidy, ActorUserID: 9002,
		Reason: "different action", EvidenceRef: "case://subsidy-1",
	})
	require.ErrorIs(t, err, ErrProviderReconciliationConflict)
	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 9999,
		Reason: "different actor", EvidenceRef: "case://refund-1",
	})
	require.ErrorIs(t, err, ErrProviderReconciliationConflict)

	var balanceMicros, frozenMicros int64
	var events int
	var reservationState, attemptState string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT balance_micros, frozen_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balanceMicros, &frozenMicros))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&reservationState))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attempt.AttemptID).Scan(&attemptState))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM provider_cost_reconciliations WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&events))
	require.Equal(t, testMicros(1000), balanceMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, "cancelled", reservationState)
	require.Equal(t, "reconciled", attemptState)
	require.Equal(t, 2, events)
}

func TestP6F40CCollectRejectsEstimatedOrUnknownUsage(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_estimate"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_estimate", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), SettleRequest{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		PromptTokens: 250_000, UsageSource: "estimated",
	})
	require.NoError(t, err)
	require.False(t, prepared.Funded)
	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationCollect, ActorUserID: 9003,
		Reason: "estimate only", EvidenceRef: "case://estimate-1",
	})
	require.ErrorIs(t, err, ErrProviderReconciliationUnsafe)
}

func TestP6F40CCollectRejectsAuthoritativeUsageWithoutLocalDelivery(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_undelivered"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_undelivered", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), SettleRequest{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		PromptTokens: 250_000, UsageSource: "upstream",
	})
	require.NoError(t, err)
	require.False(t, prepared.Funded)
	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationCollect, ActorUserID: 9006,
		Reason: "provider usage exists but output was withheld", EvidenceRef: "case://undelivered",
	})
	require.ErrorIs(t, err, ErrProviderReconciliationUnsafe)
}

func TestP6F40CCollectRecoversCompletionEvidenceAfterSettlementCrashGap(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_collect_recovery"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_collect_recovery", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	settleReq := SettleRequest{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		PromptTokens: 250_000, UsageSource: "upstream",
	}
	require.NoError(t, b.MarkBillableOutput(t.Context(), requestID, orgID))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 1, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	require.NoError(t, b.PersistSettlementUsage(t.Context(), settleReq))
	pending, err := b.Settle(t.Context(), settleReq)
	require.NoError(t, err)
	require.False(t, pending.Success)
	req := ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationCollect, ActorUserID: 9007,
		Reason: "resume durable collect", EvidenceRef: "case://collect-recovery",
	}
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, authorizeProviderReconciliationTx(t.Context(), tx, req, attempt.APIKeyID))
	require.NoError(t, tx.Commit(t.Context()))
	_, err = pool.Exec(t.Context(), `UPDATE wallets SET balance_micros = balance_micros + 4000000 WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	settled, err := b.Settle(t.Context(), settleReq)
	require.NoError(t, err)
	require.True(t, settled.Success)

	recovered, err := b.ReconcileProviderAttempt(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, settled.SettleID, recovered.TerminalTransactionID)
	var evidence int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM provider_cost_reconciliations WHERE org_id=$1 AND request_id=$2`,
		orgID, requestID).Scan(&evidence))
	require.Equal(t, 2, evidence)
}

func TestP6F40CRefundNormalizesPendingOutcomeCertainty(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_pending_refund"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_pending_refund", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrProviderAttemptUnresolved)
	_, err := b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 9008,
		Reason: "provider confirmed no billable delivery", EvidenceRef: "case://pending-refund",
	})
	require.NoError(t, err)
	var state, certainty string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state,outcome_certainty FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, requestID, attempt.AttemptID).Scan(&state, &certainty))
	require.Equal(t, "reconciled", state)
	require.Equal(t, "unknown", certainty)
}

func TestP6F40CSubsidyRequiresKnownBoundedCost(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_subsidy"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_subsidy", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), SettleRequest{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		PromptTokens: 250_000, UsageSource: "upstream",
	})
	require.NoError(t, err)
	require.False(t, prepared.Funded)
	result, err := b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationSubsidy, ActorUserID: 9004,
		Reason: "bounded platform subsidy approved", EvidenceRef: "case://subsidy-1",
	})
	require.NoError(t, err)
	require.Equal(t, "1000000", result.CustomerRefundMicros)
}

func TestP6F40CConcurrentRefundProducesOneTerminalAndTwoEvidencePhases(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_refund_race"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_refund_race", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeUnknown, FailureKind: "transport",
	}))
	req := ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 9005,
		Reason: "provider confirmed no delivery", EvidenceRef: "case://refund-race",
	}
	start := make(chan struct{})
	errs := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := b.ReconcileProviderAttempt(context.Background(), req)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var terminals, evidence int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='reconcile_refund'`, orgID, requestID).Scan(&terminals))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM provider_cost_reconciliations WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&evidence))
	require.Equal(t, 1, terminals)
	require.Equal(t, 2, evidence)
}

func TestP6F40CReconciliationCannotCrossOrganizationBoundary(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40c_tenant"
	attempt := providerAttemptFixture(requestID, "att_p6f40c_tenant", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeUnknown, FailureKind: "transport",
	}))
	var otherOrgID int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`,
		"p6f40c-other-org").Scan(&otherOrgID))
	_, err := b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: otherOrgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 9009,
		Reason: "cross organization attempt", EvidenceRef: "case://tenant",
	})
	require.Error(t, err)
	var evidence int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM provider_cost_reconciliations WHERE request_id=$1`, requestID).Scan(&evidence))
	require.Zero(t, evidence)
	var state string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, requestID, attempt.AttemptID).Scan(&state))
	require.Equal(t, ProviderAttemptOutcomeUnknown, state)
}
