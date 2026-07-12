package billing

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func providerAttemptFixture(requestID, attemptID string, orgID int64, sequence, total int) ProviderAttemptStart {
	return ProviderAttemptStart{
		RequestID:     requestID,
		AttemptID:     attemptID,
		OrgID:         orgID,
		APIKeyID:      101,
		ChannelID:     int64(200 + sequence),
		ChannelKeyID:  int64(300 + sequence),
		RouteSequence: sequence,
		RouteTotal:    total,
		Endpoint:      "/v1/chat/completions",
		Provider:      "test-provider",
		Channel:       "test-channel",
	}
}

func TestP6F40BProviderAttemptBeginIsIdempotentAndConflictSafe(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_begin"
	attemptID := "att_p6f40b_begin"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	start := providerAttemptFixture(requestID, attemptID, orgID, 1, 2)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), start))
	require.NoError(t, b.BeginProviderAttempt(t.Context(), start), "pre-dispatch commit ambiguity must be replay-safe")

	conflict := start
	conflict.ChannelID++
	require.ErrorIs(t, b.BeginProviderAttempt(t.Context(), conflict), ErrProviderAttemptConflict)

	var rows int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`,
		orgID, requestID, attemptID,
	).Scan(&rows))
	require.Equal(t, 1, rows)
}

func TestP6F40BRejectedAttemptAllowsBoundedRetryAndSafeCancel(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_rejected"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	first := providerAttemptFixture(requestID, "att_p6f40b_rejected_1", orgID, 1, 2)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), first))
	completion := ProviderAttemptCompletion{
		RequestID:         requestID,
		AttemptID:         first.AttemptID,
		OrgID:             orgID,
		Outcome:           ProviderAttemptOutcomeRejected,
		RetryAuthorized:   true,
		HTTPStatus:        503,
		FailureKind:       "channel_failure",
		UpstreamRequestID: "provider-rejected-1",
	}

	start := make(chan struct{})
	errs := make(chan error, 24)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- b.CompleteProviderAttempt(context.Background(), completion)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	second := providerAttemptFixture(requestID, "att_p6f40b_rejected_2", orgID, 2, 2)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), second), "durable known rejection authorizes the next attempt")
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:   requestID,
		AttemptID:   second.AttemptID,
		OrgID:       orgID,
		Outcome:     ProviderAttemptOutcomeRejected,
		HTTPStatus:  429,
		FailureKind: "rate_limit",
	}))
	require.NoError(t, b.Cancel(t.Context(), requestID, orgID), "only known-rejected attempts make refund safe")

	var status string
	var terminals int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminals))
	require.Equal(t, "cancelled", status)
	require.Equal(t, 1, terminals)
}

func TestP6F40BAmbiguousTransportHoldsFundsAndBlocksDuplicateDispatch(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_ambiguous"
	first := providerAttemptFixture(requestID, "att_p6f40b_ambiguous_1", orgID, 1, 2)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), first))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:   requestID,
		AttemptID:   first.AttemptID,
		OrgID:       orgID,
		Outcome:     ProviderAttemptOutcomeUnknown,
		FailureKind: "channel_failure",
	}))

	second := providerAttemptFixture(requestID, "att_p6f40b_ambiguous_2", orgID, 2, 2)
	require.Error(t, b.BeginProviderAttempt(t.Context(), second))
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)

	var reservationStatus, reason, attemptState, certainty string
	var providerCostPending bool
	var terminals int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status, settlement_pending_reason, provider_cost_pending_at IS NOT NULL
		 FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&reservationStatus, &reason, &providerCostPending))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state, outcome_certainty FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`, orgID, requestID, first.AttemptID,
	).Scan(&attemptState, &certainty))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminals))
	require.Equal(t, "settlement_pending", reservationStatus)
	require.Equal(t, pendingReasonAmbiguousAttempt, reason)
	require.True(t, providerCostPending)
	require.Equal(t, ProviderAttemptOutcomeUnknown, attemptState)
	require.Equal(t, providerAttemptCertaintyUnknown, certainty)
	require.Zero(t, terminals)
}

func TestP6F40BUnusableAcceptedResponseCannotBeRefunded(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_unusable"
	attempt := providerAttemptFixture(requestID, "att_p6f40b_unusable", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:         requestID,
		AttemptID:         attempt.AttemptID,
		OrgID:             orgID,
		Outcome:           ProviderAttemptOutcomeResponseAccepted,
		HTTPStatus:        200,
		UpstreamRequestID: "provider-accepted-unusable",
	}))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:         requestID,
		AttemptID:         attempt.AttemptID,
		OrgID:             orgID,
		Outcome:           ProviderAttemptOutcomeResponseUnusable,
		HTTPStatus:        200,
		FailureKind:       "response_parse",
		UpstreamRequestID: "provider-accepted-unusable",
	}))
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)

	var reason, state string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT settlement_pending_reason FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&reason))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`, orgID, requestID, attempt.AttemptID,
	).Scan(&state))
	require.Equal(t, pendingReasonResponseUnusable, reason)
	require.Equal(t, ProviderAttemptOutcomeResponseUnusable, state)
}

func TestP6F40BAcceptedResponseGenericCancelBecomesProviderHold(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_accepted_cancel"
	attempt := providerAttemptFixture(requestID, "att_p6f40b_accepted_cancel", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:  requestID,
		AttemptID:  attempt.AttemptID,
		OrgID:      orgID,
		Outcome:    ProviderAttemptOutcomeResponseAccepted,
		HTTPStatus: 200,
	}))
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrProviderAttemptUnresolved)

	var status, reason string
	var terminals int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status, settlement_pending_reason FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &reason))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminals))
	require.Equal(t, "settlement_pending", status)
	require.Equal(t, pendingReasonProviderAttemptUnresolved, reason)
	require.Zero(t, terminals)
}

func TestP6F40BAcceptedAttemptSettlesAtomicallyWithReceipt(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_settled"
	attempt := providerAttemptFixture(requestID, "att_p6f40b_settled", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:         requestID,
		AttemptID:         attempt.AttemptID,
		OrgID:             orgID,
		Outcome:           ProviderAttemptOutcomeResponseAccepted,
		HTTPStatus:        200,
		UpstreamRequestID: "provider-settled-1",
	}))
	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:  requestID,
		AttemptID:  attempt.AttemptID,
		OrgID:      orgID,
		ActualCost: 100,
	})
	require.NoError(t, err)
	require.True(t, result.Success)

	var state, certainty string
	var calculated, charged int
	var transactionID int64
	var resolved bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state, outcome_certainty, calculated_cost, charged_cost,
		        terminal_transaction_id, financial_resolved_at IS NOT NULL
		 FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`, orgID, requestID, attempt.AttemptID,
	).Scan(&state, &certainty, &calculated, &charged, &transactionID, &resolved))
	require.Equal(t, providerAttemptStateSettled, state)
	require.Equal(t, providerAttemptCertaintyKnownUsage, certainty)
	require.Equal(t, 100, calculated)
	require.Equal(t, 100, charged)
	require.Equal(t, result.SettleID, transactionID)
	require.True(t, resolved)
}

func TestP6F40BFundingPendingRetainsAttemptCost(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40b_funding_pending"
	attempt := providerAttemptFixture(requestID, "att_p6f40b_funding_pending", orgID, 1, 1)
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), attempt))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID:  requestID,
		AttemptID:  attempt.AttemptID,
		OrgID:      orgID,
		Outcome:    ProviderAttemptOutcomeResponseAccepted,
		HTTPStatus: 200,
	}))
	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:  requestID,
		AttemptID:  attempt.AttemptID,
		OrgID:      orgID,
		ActualCost: 500,
	})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_topup", result.ErrorCode)

	var state, certainty string
	var calculated, charged int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT state, outcome_certainty, calculated_cost, charged_cost
		 FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`, orgID, requestID, attempt.AttemptID,
	).Scan(&state, &certainty, &calculated, &charged))
	require.Equal(t, providerAttemptStateFundingPending, state)
	require.Equal(t, providerAttemptCertaintyKnownUsage, certainty)
	require.Equal(t, 500, calculated)
	require.Zero(t, charged)
}
