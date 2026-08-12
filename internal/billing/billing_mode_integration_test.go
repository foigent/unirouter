package billing

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/outbox"
)

func billingModeTestPriceProvider() PriceProvider {
	return PriceProviderFunc(func(context.Context, string) (string, string, error) {
		return "2", "10", nil
	})
}

func TestCostObservationDeliverySettlementPersistsCalculatedNotCharged(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 0, 0)
	_, err := pool.Exec(t.Context(), `DELETE FROM wallets WHERE org_id=$1`, orgID)
	require.NoError(t, err, "observation admission must not require a wallet row")
	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	outboxService, err := outbox.New(pool, options)
	require.NoError(t, err)
	b := NewBilling(pool, nil).WithPriceProvider(billingModeTestPriceProvider()).WithMode(BillingModeCostObservation)
	b.SetOutbox(outboxService)
	require.NoError(t, outboxService.RegisterHandler(outbox.EventBillingUsagePrepared, b.ProcessUsageJournalEvent))
	require.NoError(t, outboxService.RegisterHandler(outbox.EventBillingSettled, b.ProcessUsageJournalEvent))

	const requestID = "op_cost_observation_delivery"
	const attemptID = "att_cost_observation_delivery"
	freeze, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1.5", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	require.Zero(t, freeze.AmountMicros)
	require.Positive(t, freeze.CalculatedCostMicros)
	require.Equal(t, BillingModeCostObservation, freeze.BillingMode)

	// Runtime mode and pricing-authority changes after admission must not
	// change this request or make an idempotent Freeze replay depend on a new
	// price lookup.
	b.WithMode(BillingModePrepaidEnforced).WithPriceProvider(PriceProviderFunc(
		func(context.Context, string) (string, string, error) {
			return "", "", errors.New("current pricing authority unavailable")
		},
	))
	replayedFreeze, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1.5", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, replayedFreeze.Success)
	require.Equal(t, BillingModeCostObservation, replayedFreeze.BillingMode)
	require.Equal(t, freeze.PricingVersion, replayedFreeze.PricingVersion)
	require.Equal(t, freeze.CalculatedCostMicros, replayedFreeze.CalculatedCostMicros)
	require.NoError(t, b.BeginProviderAttempt(t.Context(), ProviderAttemptStart{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		APIKeyID: 77, ChannelID: 88, ChannelKeyID: 99,
		RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions",
		Provider: "test", Channel: "test-channel",
	}))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	_, err = pool.Exec(t.Context(), `
		INSERT INTO request_idempotency (
			org_id, api_key_id, key_hash, request_fingerprint, operation_id,
			state, created_at, updated_at, expires_at
		) VALUES ($1,77,$2,$3,$4,'in_progress',NOW(),NOW(),NOW()+INTERVAL '1 day')`,
		orgID, strings.Repeat("a", 64), strings.Repeat("b", 64), requestID)
	require.NoError(t, err)

	usage := SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		PromptTokens: 20, UncachedPromptTokens: 20, CompletionTokens: 5,
		UsageSource:          metering.UsageSourceUpstream,
		UsageAuthority:       metering.UsageAuthorityProviderReported,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion,
		CacheUsageStatus:     cacheUsageStatusNotReported,
	}
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), usage)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	require.Positive(t, prepared.Preview.ActualCostMicros)
	require.Zero(t, prepared.Preview.ChargedCostMicros)
	require.Equal(t, BillingModeCostObservation, prepared.Preview.BillingMode)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 512, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	settled, err := b.Settle(t.Context(), usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Positive(t, settled.ActualCostMicros)
	require.Zero(t, settled.ChargedCostMicros)
	require.Zero(t, settled.SettleID)
	require.Positive(t, settled.ObservationReceiptID)
	require.Equal(t, BillingModeCostObservation, settled.BillingMode)

	replayed, err := b.Settle(t.Context(), usage)
	require.NoError(t, err)
	require.Equal(t, settled.ObservationReceiptID, replayed.ObservationReceiptID)
	require.Equal(t, settled.ActualCostMicros, replayed.ActualCostMicros)
	require.Zero(t, replayed.ChargedCostMicros)

	var walletCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM wallets WHERE org_id=$1`, orgID).Scan(&walletCount))
	require.Zero(t, walletCount, "observation must neither require nor recreate a wallet")
	var transactionCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2`,
		orgID, requestID).Scan(&transactionCount))
	require.Zero(t, transactionCount, "observation must not manufacture financial transactions")

	var receiptStatus, receiptPricingVersion string
	var receiptCalculated, receiptCharged int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT terminal_status,calculated_cost_micros,charged_cost_micros,pricing_version
		FROM billing_observation_receipts
		WHERE id=$1 AND org_id=$2 AND request_id=$3`,
		settled.ObservationReceiptID, orgID, requestID).Scan(
		&receiptStatus, &receiptCalculated, &receiptCharged, &receiptPricingVersion,
	))
	require.Equal(t, "settled", receiptStatus)
	require.Equal(t, settled.ActualCostMicros, receiptCalculated)
	require.Zero(t, receiptCharged)
	require.Equal(t, freeze.PricingVersion, receiptPricingVersion)
	_, err = pool.Exec(t.Context(), `
		UPDATE billing_observation_receipts
		SET calculated_cost_micros=calculated_cost_micros+1
		WHERE id=$1`, settled.ObservationReceiptID)
	require.ErrorContains(t, err, "append-only")

	processed, err := outboxService.DrainOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	var journalMode string
	var journalCalculated, journalCharged int64
	var journalTransactionID sql.NullInt64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT billing_mode,calculated_cost_micros,charged_cost_micros,terminal_transaction_id
		FROM usage_finalization_journal
		WHERE org_id=$1 AND request_id=$2 AND event_type='settled'`,
		orgID, requestID).Scan(&journalMode, &journalCalculated, &journalCharged, &journalTransactionID))
	require.Equal(t, string(BillingModeCostObservation), journalMode)
	require.Equal(t, settled.ActualCostMicros, journalCalculated)
	require.Zero(t, journalCharged)
	require.False(t, journalTransactionID.Valid)
}

func TestCostObservationCancelAndExtensionNeverTouchWallet(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 0, 0)
	b := NewBilling(pool, nil).WithPriceProvider(billingModeTestPriceProvider()).WithMode(BillingModeCostObservation)

	const extensionRequest = "op_cost_observation_extension"
	freeze, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: extensionRequest, OrgID: orgID, Model: model,
		EstimatedTokens: 110, ReservedCompletionTokens: 10,
		SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	b.WithMode(BillingModePrepaidEnforced)
	extended, err := b.ExtendReservation(t.Context(), ExtendReservationRequest{
		RequestID: extensionRequest, OrgID: orgID, Sequence: 1,
		AdditionalCompletionTokens: 100, TargetCompletionTokens: 110,
	})
	require.NoError(t, err)
	require.True(t, extended.Success)
	require.Zero(t, extended.AmountMicros)
	require.Positive(t, extended.CalculatedCostMicros)
	require.Zero(t, extended.NewFrozenMicros)
	require.Equal(t, BillingModeCostObservation, extended.BillingMode)
	replayedExtension, err := b.ExtendReservation(t.Context(), ExtendReservationRequest{
		RequestID: extensionRequest, OrgID: orgID, Sequence: 1,
		AdditionalCompletionTokens: 100, TargetCompletionTokens: 110,
	})
	require.NoError(t, err)
	require.Equal(t, extended.CalculatedCostMicros, replayedExtension.CalculatedCostMicros)
	require.Zero(t, replayedExtension.AmountMicros)

	const cancelRequest = "op_cost_observation_cancel"
	b.WithMode(BillingModeCostObservation)
	freeze, err = b.Freeze(t.Context(), FreezeRequest{
		RequestID: cancelRequest, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	b.WithMode(BillingModePrepaidEnforced)
	require.NoError(t, b.Cancel(t.Context(), cancelRequest, orgID))
	require.NoError(t, b.Cancel(t.Context(), cancelRequest, orgID))

	var balance, frozen, spent int64
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT balance_micros,frozen_micros,total_spent_micros
		FROM wallets WHERE org_id=$1`, orgID).Scan(&balance, &frozen, &spent))
	require.Zero(t, balance)
	require.Zero(t, frozen)
	require.Zero(t, spent)
	var financialTransactions, cancelReceipts, observedExtensions int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id IN ($2,$3)`,
		orgID, extensionRequest, cancelRequest).Scan(&financialTransactions))
	require.Zero(t, financialTransactions)
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_observation_receipts
		WHERE org_id=$1 AND request_id=$2 AND terminal_status='cancelled'`,
		orgID, cancelRequest).Scan(&cancelReceipts))
	require.Equal(t, 1, cancelReceipts)
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_reservation_extensions
		WHERE org_id=$1 AND request_id=$2 AND status='observed'
		  AND reserved_micros=0 AND calculated_cost_micros>0`,
		orgID, extensionRequest).Scan(&observedExtensions))
	require.Equal(t, 1, observedExtensions)
}

func TestCostObservationResponseCacheRemainsZeroCost(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 0, 0)
	b := NewBilling(pool, nil).WithPriceProvider(billingModeTestPriceProvider()).WithMode(BillingModeCostObservation)
	const requestID = "op_cost_observation_response_cache"

	frozen, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.Zero(t, frozen.AmountMicros)

	request := ResponseCacheSettleRequest(requestID, orgID)
	prepared, err := b.PrepareResponseCacheDelivery(t.Context(), request)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	require.Zero(t, prepared.Preview.ActualCostMicros)
	require.Zero(t, prepared.Preview.ChargedCostMicros)
	require.NoError(t, b.RecordResponseCacheDelivery(t.Context(), ResponseCacheDeliveryEvent{
		RequestID: requestID, OrgID: orgID, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200,
	}))

	settled, err := b.Settle(t.Context(), request)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Zero(t, settled.ActualCostMicros)
	require.Zero(t, settled.ChargedCostMicros)
	require.Zero(t, settled.SettleID)
	require.Positive(t, settled.ObservationReceiptID)

	var financialTransactions int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2`,
		orgID, requestID).Scan(&financialTransactions))
	require.Zero(t, financialTransactions)
}

func TestBillingModeBudgetFailsClosedAndDefaultRemainsPrepaid(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 0, 0)
	defaultBilling := NewBilling(pool, nil).WithPriceProvider(billingModeTestPriceProvider())
	require.Equal(t, BillingModePrepaidEnforced, defaultBilling.mode)
	result, err := defaultBilling.Freeze(t.Context(), FreezeRequest{
		RequestID: "op_default_prepaid", OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_balance", result.ErrorCode)

	_, err = NewBilling(pool, nil).WithPriceProvider(billingModeTestPriceProvider()).WithMode(BillingModeBudgetEnforced).Freeze(t.Context(), FreezeRequest{
		RequestID: "op_budget_unsupported", OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.ErrorContains(t, err, "unsupported")
}

func TestPrepaidModeFinancialSemanticsRemainIntact(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1_000, 0)
	b := NewBilling(pool, nil).WithPriceProvider(billingModeTestPriceProvider())

	const settledRequest = "op_prepaid_mode_regression_settle"
	frozen, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: settledRequest, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.Positive(t, frozen.AmountMicros)
	require.Equal(t, frozen.AmountMicros, frozen.CalculatedCostMicros)
	require.Equal(t, BillingModePrepaidEnforced, frozen.BillingMode)
	settled, err := b.Settle(t.Context(), SettleRequest{
		RequestID: settledRequest, OrgID: orgID,
		PromptTokens: 20, CompletionTokens: 5, UsageSource: metering.UsageSourceUpstream,
	})
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Positive(t, settled.ChargedCostMicros)
	require.Positive(t, settled.SettleID)
	require.Zero(t, settled.ObservationReceiptID)

	const cancelledRequest = "op_prepaid_mode_regression_cancel"
	frozen, err = b.Freeze(t.Context(), FreezeRequest{
		RequestID: cancelledRequest, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.NoError(t, b.Cancel(t.Context(), cancelledRequest, orgID))

	var settleTransactions, cancelTransactions, observationReceipts int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='settle'`,
		orgID, settledRequest).Scan(&settleTransactions))
	require.Equal(t, 1, settleTransactions)
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM transactions
		WHERE org_id=$1 AND request_id=$2 AND type='cancel'`,
		orgID, cancelledRequest).Scan(&cancelTransactions))
	require.Equal(t, 1, cancelTransactions)
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_observation_receipts
		WHERE org_id=$1 AND request_id IN ($2,$3)`,
		orgID, settledRequest, cancelledRequest).Scan(&observationReceipts))
	require.Zero(t, observationReceipts)
}
