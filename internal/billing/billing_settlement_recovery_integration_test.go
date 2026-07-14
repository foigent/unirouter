package billing

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestP6F18BBillableOutputCannotBeCancelledAndUsageIsImmutable(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	b := NewBilling(pool, nil)
	requestID := "op_p6f18b_non_cancellable"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	require.NoError(t, b.MarkBillableOutput(ctx, requestID, orgID))
	require.NoError(t, b.MarkBillableOutput(ctx, requestID, orgID), "output marker replay must be idempotent")
	require.ErrorIs(t, b.Cancel(ctx, requestID, orgID), ErrSettlementPending)

	var status string
	var outputMarked bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, output_started_at IS NOT NULL
		 FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &outputMarked))
	require.Equal(t, "settlement_pending", status)
	require.True(t, outputMarked)

	usage := SettleRequest{
		RequestID: requestID, OrgID: orgID,
		PromptTokens: 1000, CompletionTokens: 100, UsageSource: "upstream",
	}
	require.NoError(t, b.PersistSettlementUsage(ctx, usage))
	require.NoError(t, b.PersistSettlementUsage(ctx, usage), "identical usage replay must be idempotent")

	conflict := usage
	conflict.CompletionTokens++
	require.ErrorIs(t, b.PersistSettlementUsage(ctx, conflict), ErrSettlementUsageConflict)
	_, err := b.Settle(ctx, conflict)
	require.ErrorIs(t, err, ErrSettlementUsageConflict)

	settled, err := b.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Equal(t, testMicros(3), settled.ActualCostMicros)
	require.Equal(t, testMicros(3), settled.ChargedCostMicros)

	var balanceMicros, frozenMicros, spentMicros int64
	var terminals int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros, &spentMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminals))
	require.Equal(t, testMicros(997), balanceMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(3), spentMicros)
	require.Equal(t, 1, terminals)
}

func TestP6F18BPostChunkDatabaseFailureRecoversExactlyOnceAfterRestart(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	b := NewBilling(pool, nil)
	requestID := "op_p6f18b_restart_recovery"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.MarkBillableOutput(ctx, requestID, orgID))
	usage := SettleRequest{
		RequestID: requestID, OrgID: orgID,
		PromptTokens: 1000, CompletionTokens: 100, UsageSource: "upstream",
	}
	require.NoError(t, b.PersistSettlementUsage(ctx, usage))

	// Simulate the request process losing its DB pool after the last upstream
	// chunk but before its settlement transaction can begin.
	failurePool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	failurePool.Close()
	_, err = NewBilling(failurePool, nil).Settle(ctx, usage)
	require.Error(t, err)

	var pendingStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&pendingStatus))
	require.Equal(t, "settlement_pending", pendingStatus)
	require.ErrorIs(t, b.Cancel(ctx, requestID, orgID), ErrSettlementPending)

	// This represents the best-effort gateway log written before a restart. The
	// recovery settlement must converge its usage and cost to the ledger receipt.
	_, err = pool.Exec(ctx,
		`INSERT INTO call_logs (request_id, org_id, model, cost_micros, is_stream, usage_source)
		 VALUES ($1, $2, $3, 0, TRUE, 'counter_v2')`, requestID, orgID, model)
	require.NoError(t, err)

	restarted := NewBilling(pool, nil)
	start := make(chan struct{})
	results := make(chan SettlementRecoveryResult, 20)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, recoveryErr := restarted.RecoverPendingSettlements(context.Background(), 1)
			results <- result
			errs <- recoveryErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	claimed, settledCount := 0, 0
	for recoveryErr := range errs {
		require.NoError(t, recoveryErr)
	}
	for result := range results {
		claimed += result.Claimed
		settledCount += result.Settled
	}
	require.Equal(t, 1, claimed)
	require.Equal(t, 1, settledCount)

	var state, usageSource string
	var promptTokens, completionTokens, terminalEvents int
	var costMicros int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, prompt_tokens, completion_tokens, usage_source
		 FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&state, &promptTokens, &completionTokens, &usageSource))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT prompt_tokens, completion_tokens, cost_micros, usage_source
		 FROM call_logs WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&promptTokens, &completionTokens, &costMicros, &usageSource))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalEvents))
	require.Equal(t, "settled", state)
	require.Equal(t, 1000, promptTokens)
	require.Equal(t, 100, completionTokens)
	require.Equal(t, testMicros(3), costMicros)
	require.Equal(t, "upstream", usageSource)
	require.Equal(t, 1, terminalEvents)

	again, err := restarted.RecoverPendingSettlements(ctx, 10)
	require.NoError(t, err)
	require.Zero(t, again.Claimed)
}

func TestP6F18BPendingWithoutFinalUsageIsHeldForManualReconciliation(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f18b_usage_not_ready"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.MarkBillableOutput(t.Context(), requestID, orgID))

	result, err := b.RecoverPendingSettlements(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, result.Claimed)
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)
	_, err = b.Settle(t.Context(), SettleRequest{RequestID: requestID, OrgID: orgID})
	require.True(t, errors.Is(err, ErrSettlementUsageNotReady))

	var status string
	var balanceMicros, frozenMicros int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, frozen_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros))
	require.Equal(t, "settlement_pending", status)
	require.Equal(t, testMicros(900), balanceMicros)
	require.Equal(t, testMicros(100), frozenMicros)
}

func TestP6F18BRecoveryFailureUsesBackoffAndKeepsReservationPending(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	b := NewBilling(pool, nil)
	requestID := "op_p6f18b_recovery_backoff"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.NoError(t, b.MarkBillableOutput(ctx, requestID, orgID))
	usage := SettleRequest{
		RequestID: requestID, OrgID: orgID,
		PromptTokens: 1000, CompletionTokens: 100, UsageSource: "upstream",
	}
	require.NoError(t, b.PersistSettlementUsage(ctx, usage))

	// Remove the wallet to force a deterministic settlement transaction
	// failure after the row has been claimed. The reservation itself remains.
	_, err := pool.Exec(ctx, `DELETE FROM wallets WHERE org_id = $1`, orgID)
	require.NoError(t, err)
	failed, err := b.RecoverPendingSettlements(ctx, 1)
	require.Error(t, err)
	require.Equal(t, 1, failed.Claimed)
	require.Equal(t, 1, failed.Failed)

	var status, lastError string
	var attempts int
	var claimReleased, backoffScheduled bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, settlement_attempts, settlement_last_error,
		        settlement_claimed_until IS NULL,
		        settlement_next_attempt_at > NOW()
		 FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &attempts, &lastError, &claimReleased, &backoffScheduled))
	require.Equal(t, "settlement_pending", status)
	require.Equal(t, 1, attempts)
	require.NotEmpty(t, lastError)
	require.True(t, claimReleased)
	require.True(t, backoffScheduled)

	immediate, err := b.RecoverPendingSettlements(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, immediate.Claimed, "backoff must suppress an immediate hot retry")

	_, err = pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, 9000000, 0, 1000000, 0)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`UPDATE freeze_idempotency SET settlement_next_attempt_at = NOW()
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID)
	require.NoError(t, err)

	recovered, err := b.RecoverPendingSettlements(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, recovered.Claimed)
	require.Equal(t, 1, recovered.Settled)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, settlement_attempts FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &attempts))
	require.Equal(t, "settled", status)
	require.Equal(t, 2, attempts)
}
