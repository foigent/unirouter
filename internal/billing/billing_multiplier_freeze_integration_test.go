package billing

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func setP6F06Multiplier(t *testing.T, pool *pgxpool.Pool, orgID int64, model string) {
	t.Helper()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, $2, 2)`, orgID, model)
	require.NoError(t, err)
}

func freezeP6F06(t *testing.T, b *Billing, orgID int64, model, requestID string) *FreezeResult {
	t.Helper()
	result, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 1000,
		SafetyFactor:    1,
		MinFreezeAmount: 1,
	})
	require.NoError(t, err)
	return result
}

func TestP6F06MultiplierInsufficientBalanceCreatesNoReservation(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 19, 0)
	setP6F06Multiplier(t, pool, orgID, model)
	requestID := "op_p6f06_multiplier_insufficient"

	result := freezeP6F06(t, NewBilling(pool, nil), orgID, model, requestID)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_balance", result.ErrorCode)
	require.Contains(t, result.ErrorDetail, "available=19, required=20")

	var balance, frozen, reservationCount, freezeEvents int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance, frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&reservationCount))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type = 'freeze'`, orgID, requestID,
	).Scan(&freezeEvents))
	require.Equal(t, 19, balance)
	require.Zero(t, frozen)
	require.Zero(t, reservationCount)
	require.Zero(t, freezeEvents)
}

func TestP6F06MultiplierExactFundsConcurrentSettlementHasNoUncoveredCost(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 20, 0)
	setP6F06Multiplier(t, pool, orgID, model)
	b := NewBilling(pool, nil)
	requestID := "op_p6f06_multiplier_exact"

	freeze := freezeP6F06(t, b, orgID, model, requestID)
	require.True(t, freeze.Success, "%s: %s", freeze.ErrorCode, freeze.ErrorDetail)
	require.Equal(t, 20, freeze.Amount)

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
				RequestID:        requestID,
				OrgID:            orgID,
				CompletionTokens: 1000,
				UsageSource:      "upstream",
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
		require.Equal(t, 20, result.ActualCost)
		require.Equal(t, 20, result.ChargedCost)
		require.Zero(t, result.UncoveredCost)
		require.Zero(t, result.Refund)
		if settleID == 0 {
			settleID = result.SettleID
		}
		require.Equal(t, settleID, result.SettleID)
	}

	var balance, frozen, spent, terminalEvents int
	var status string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance, frozen, total_spent FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen, &spent))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalEvents))
	require.Zero(t, balance)
	require.Zero(t, frozen)
	require.Equal(t, 20, spent)
	require.Equal(t, "settled", status)
	require.Equal(t, 1, terminalEvents)
}

func TestP6F06MultiplierCancelRefundsAdjustedHold(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 20, 0)
	setP6F06Multiplier(t, pool, orgID, model)
	b := NewBilling(pool, nil)
	requestID := "op_p6f06_multiplier_cancel"

	freeze := freezeP6F06(t, b, orgID, model, requestID)
	require.True(t, freeze.Success, "%s: %s", freeze.ErrorCode, freeze.ErrorDetail)
	require.Equal(t, 20, freeze.Amount)
	require.NoError(t, b.Cancel(t.Context(), requestID, orgID))
	require.NoError(t, b.Cancel(t.Context(), requestID, orgID), "duplicate cancel must be idempotent")

	var balance, frozen, spent, terminalEvents int
	var status string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance, frozen, total_spent FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen, &spent))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalEvents))
	require.Equal(t, 20, balance)
	require.Zero(t, frozen)
	require.Zero(t, spent)
	require.Equal(t, "cancelled", status)
	require.Equal(t, 1, terminalEvents)
}
