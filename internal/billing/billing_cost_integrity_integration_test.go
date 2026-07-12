package billing

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestP6F40SettlementShortfallUsesAvailablePrepaidCash(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40_cash_topup"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:  requestID,
		OrgID:      orgID,
		ActualCost: 500,
	})
	require.NoError(t, err)
	require.True(t, result.Success, result.ErrorCode)
	require.Equal(t, 500, result.ActualCost)
	require.Equal(t, 500, result.ChargedCost)
	require.Zero(t, result.UncoveredCost)

	var balance, frozen, totalSpent, charged, uncovered, topUp int
	var status string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance, frozen, total_spent FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen, &totalSpent))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status, charged_cost, uncovered_cost, topup_amount
		 FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &charged, &uncovered, &topUp))
	require.Equal(t, 500, balance)
	require.Zero(t, frozen)
	require.Equal(t, 500, totalSpent)
	require.Equal(t, "settled", status)
	require.Equal(t, 500, charged)
	require.Zero(t, uncovered)
	require.Equal(t, 400, topUp)
	require.Equal(t, 400, result.TopUpAmount)
}

func TestP6F40ConcurrentTopUpProducesOneTerminalCharge(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40_concurrent_topup"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	start := make(chan struct{})
	results := make(chan *SettleResult, 32)
	errs := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := b.Settle(context.Background(), SettleRequest{
				RequestID:  requestID,
				OrgID:      orgID,
				ActualCost: 500,
			})
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	var settleID int64
	for result := range results {
		require.NotNil(t, result)
		require.True(t, result.Success, result.ErrorCode)
		require.Equal(t, 500, result.ChargedCost)
		require.Zero(t, result.UncoveredCost)
		require.Equal(t, 400, result.TopUpAmount)
		if settleID == 0 {
			settleID = result.SettleID
		}
		require.Equal(t, settleID, result.SettleID)
	}

	var balance, frozen, totalSpent, terminalTransactions int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance, frozen, total_spent FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen, &totalSpent))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalTransactions))
	require.Equal(t, 500, balance)
	require.Zero(t, frozen)
	require.Equal(t, 500, totalSpent)
	require.Equal(t, 1, terminalTransactions)
}

func TestP6F40SettlementShortfallWithoutCashStaysNonTerminal(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40_cash_pending"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:  requestID,
		OrgID:      orgID,
		ActualCost: 500,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_topup", result.ErrorCode)

	var balance, frozen, totalSpent, terminalTransactions int
	var status, pendingReason string
	var providerCostPending bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance, frozen, total_spent FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen, &totalSpent))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status, provider_cost_pending_at IS NOT NULL, settlement_pending_reason
		 FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &providerCostPending, &pendingReason))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalTransactions))
	require.Zero(t, balance)
	require.Equal(t, 100, frozen)
	require.Zero(t, totalSpent)
	require.Equal(t, "settlement_pending", status)
	require.True(t, providerCostPending)
	require.Equal(t, "insufficient_topup", pendingReason)
	require.Zero(t, terminalTransactions)
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)

	recovery, err := b.RecoverPendingSettlements(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, recovery.Claimed, "automatic stream recovery must not charge an undelivered funding-pending response")
}
