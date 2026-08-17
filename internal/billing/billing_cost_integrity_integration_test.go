//go:build integration

package billing

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func TestP6F40SettlementShortfallUsesAvailablePrepaidCash(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40_cash_topup"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:    requestID,
		OrgID:        orgID,
		PromptTokens: 250_000,
		UsageSource:  "upstream",
	})
	require.NoError(t, err)
	require.True(t, result.Success, result.ErrorCode)
	require.Equal(t, testMicros(500), result.ActualCostMicros)
	require.Equal(t, testMicros(500), result.ChargedCostMicros)
	require.Zero(t, result.UncoveredCostMicros)

	var balanceMicros, frozenMicros, totalSpentMicros, chargedMicros, uncoveredMicros, topUpMicros int64
	var status string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros, &totalSpentMicros))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status, charged_cost_micros, uncovered_cost_micros, topup_amount_micros
		 FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &chargedMicros, &uncoveredMicros, &topUpMicros))
	require.Equal(t, testMicros(500), balanceMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(500), totalSpentMicros)
	require.Equal(t, "settled", status)
	require.Equal(t, testMicros(500), chargedMicros)
	require.Zero(t, uncoveredMicros)
	require.Equal(t, testMicros(400), topUpMicros)
	require.Equal(t, testMicros(400), result.TopUpAmountMicros)
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
				RequestID:    requestID,
				OrgID:        orgID,
				PromptTokens: 250_000,
				UsageSource:  "upstream",
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
		require.Equal(t, testMicros(500), result.ChargedCostMicros)
		require.Zero(t, result.UncoveredCostMicros)
		require.Equal(t, testMicros(400), result.TopUpAmountMicros)
		if settleID == 0 {
			settleID = result.SettleID
		}
		require.Equal(t, settleID, result.SettleID)
	}

	var balanceMicros, frozenMicros, totalSpentMicros int64
	var terminalTransactions int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros, &totalSpentMicros))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalTransactions))
	require.Equal(t, testMicros(500), balanceMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(500), totalSpentMicros)
	require.Equal(t, 1, terminalTransactions)
}

func TestP6F40SettlementShortfallWithoutCashStaysNonTerminal(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f40_cash_pending"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:    requestID,
		OrgID:        orgID,
		PromptTokens: 250_000,
		UsageSource:  "upstream",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_topup", result.ErrorCode)

	var balanceMicros, frozenMicros, totalSpentMicros int64
	var terminalTransactions int
	var status, pendingReason string
	var providerCostPending bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros, &totalSpentMicros))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status, provider_cost_pending_at IS NOT NULL, settlement_pending_reason
		 FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&status, &providerCostPending, &pendingReason))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalTransactions))
	require.Zero(t, balanceMicros)
	require.Equal(t, testMicros(100), frozenMicros)
	require.Zero(t, totalSpentMicros)
	require.Equal(t, "settlement_pending", status)
	require.True(t, providerCostPending)
	require.Equal(t, "insufficient_topup", pendingReason)
	require.Zero(t, terminalTransactions)
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)

	recovery, err := b.RecoverPendingSettlements(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, recovery.Claimed, "automatic stream recovery must not charge an undelivered funding-pending response")
}

func TestP6F41AConservativeEstimateRequiresFullFundingAndPersistsTypedAuthority(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f41_estimate_funding_gate"
	freezeForInvariantTest(t, b, orgID, model, requestID, 10)

	result, err := b.Settle(t.Context(), SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		PromptTokens:         10_000,
		UncachedPromptTokens: 10_000,
		UsageSource:          metering.UsageSourceEstimated,
		UsageAuthority:       metering.UsageAuthorityConservativeEstimate,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityEstimated,
		UsageContractVersion: metering.UsageContractVersion,
		UsageEstimateVersion: metering.ConservativeEstimateVersionV1,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_topup", result.ErrorCode)
	require.Zero(t, result.ChargedCostMicros)

	var status, authority, purpose, finality, contractVersion, estimateVersion string
	var chargedCostMicros int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status, charged_cost_micros, usage_authority, usage_purpose, usage_finality,
		       usage_contract_version, usage_estimate_version
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&status, &chargedCostMicros, &authority, &purpose, &finality, &contractVersion, &estimateVersion))
	require.Equal(t, "settlement_pending", status)
	require.Zero(t, chargedCostMicros)
	require.Equal(t, string(metering.UsageAuthorityConservativeEstimate), authority)
	require.Equal(t, string(metering.UsagePurposeCustomerCharge), purpose)
	require.Equal(t, string(metering.UsageFinalityEstimated), finality)
	require.Equal(t, metering.UsageContractVersion, contractVersion)
	require.Equal(t, metering.ConservativeEstimateVersionV1, estimateVersion)
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)
}
