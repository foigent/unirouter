//go:build integration

package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func newResponseCacheDeliveryFixture(t *testing.T, requestID string) (*Billing, int64, int64) {
	t.Helper()
	pool, orgID, model := newReservationFixture(t, 1_000, 0)
	billingService := NewBilling(pool, nil)
	frozen, err := billingService.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1.5", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	return billingService, orgID, frozen.AmountMicros
}

func TestResponseCacheDeliverySettlesZeroWithoutProviderAttempt(t *testing.T) {
	const requestID = "op_response_cache_delivery_complete"
	billingService, orgID, frozen := newResponseCacheDeliveryFixture(t, requestID)
	request := ResponseCacheSettleRequest(requestID, orgID)

	prepared, err := billingService.PrepareResponseCacheDelivery(t.Context(), request)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	require.Zero(t, prepared.Preview.ActualCostMicros)
	require.Zero(t, prepared.Preview.ChargedCostMicros)

	require.NoError(t, billingService.RecordResponseCacheDelivery(t.Context(), ResponseCacheDeliveryEvent{
		RequestID: requestID, OrgID: orgID, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 512, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	// Exact replay is idempotent; a conflicting terminal replay is rejected.
	require.NoError(t, billingService.RecordResponseCacheDelivery(t.Context(), ResponseCacheDeliveryEvent{
		RequestID: requestID, OrgID: orgID, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 512, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	require.ErrorIs(t, billingService.RecordResponseCacheDelivery(t.Context(), ResponseCacheDeliveryEvent{
		RequestID: requestID, OrgID: orgID, Outcome: DeliveryOutcomeWriteFailed,
		HTTPStatus: 200, ErrorCode: "conflicting_replay",
	}), ErrDeliveryAttemptConflict)

	settled, err := billingService.Settle(t.Context(), request)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Zero(t, settled.ActualCostMicros)
	require.Zero(t, settled.ChargedCostMicros)

	var status, source, authority, purpose, finality, version string
	var prompt, completion int
	var balance, frozenBalance, spent int64
	var deliveryReceipts, providerAttempts int
	require.NoError(t, billingService.db.QueryRow(t.Context(), `
		SELECT status,usage_source,usage_authority,usage_purpose,usage_finality,
		       usage_contract_version,prompt_tokens,completion_tokens
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&status, &source, &authority, &purpose, &finality, &version, &prompt, &completion))
	require.NoError(t, billingService.db.QueryRow(t.Context(), `
		SELECT balance_micros,frozen_micros,total_spent_micros FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&balance, &frozenBalance, &spent))
	require.NoError(t, billingService.db.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM response_cache_delivery_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&deliveryReceipts))
	require.NoError(t, billingService.db.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&providerAttempts))

	require.Equal(t, "settled", status)
	require.Equal(t, metering.UsageSourceResponseCache, source)
	require.Equal(t, string(metering.UsageAuthorityLocalCache), authority)
	require.Equal(t, string(metering.UsagePurposeCustomerCharge), purpose)
	require.Equal(t, string(metering.UsageFinalityFinal), finality)
	require.Equal(t, metering.UsageContractVersionV2, version)
	require.Zero(t, prompt)
	require.Zero(t, completion)
	require.Equal(t, testMicros(1_000), balance)
	require.Zero(t, frozenBalance)
	require.Zero(t, spent)
	require.Equal(t, 2, deliveryReceipts)
	require.Zero(t, providerAttempts)
	require.Positive(t, frozen)
}

func TestResponseCacheFailedDeliveryIsRecoveredAsZeroCharge(t *testing.T) {
	const requestID = "op_response_cache_delivery_failed"
	billingService, orgID, _ := newResponseCacheDeliveryFixture(t, requestID)
	request := ResponseCacheSettleRequest(requestID, orgID)
	prepared, err := billingService.PrepareResponseCacheDelivery(t.Context(), request)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	require.NoError(t, billingService.RecordResponseCacheDelivery(t.Context(), ResponseCacheDeliveryEvent{
		RequestID: requestID, OrgID: orgID, Outcome: DeliveryOutcomeWriteFailed,
		DeliveredBytes: 17, HTTPStatus: 200, ErrorCode: "downstream_write_failed",
	}))

	recovery, err := billingService.RecoverPendingSettlements(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, recovery.Claimed)
	require.Equal(t, 1, recovery.Settled)
	require.Zero(t, recovery.Failed)

	var status string
	var charged, spent, frozenBalance int64
	require.NoError(t, billingService.db.QueryRow(t.Context(), `
		SELECT status,charged_cost_micros FROM freeze_idempotency
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&status, &charged))
	require.NoError(t, billingService.db.QueryRow(t.Context(), `
		SELECT total_spent_micros,frozen_micros FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&spent, &frozenBalance))
	require.Equal(t, "settled", status)
	require.Zero(t, charged)
	require.Zero(t, spent)
	require.Zero(t, frozenBalance)
}
