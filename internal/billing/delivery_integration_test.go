//go:build integration

package billing

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func newP6F41DeliveryFixture(t *testing.T, requestID string) (*Billing, int64, string, string) {
	t.Helper()
	pool, orgID, model := newReservationFixture(t, 1_000, 0)
	b := NewBilling(pool, nil)
	freeze, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: 1.5, MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	attemptID := "att_" + requestID
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
	require.NoError(t, func() error {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO request_idempotency (
				org_id, api_key_id, key_hash, request_fingerprint, operation_id,
				state, created_at, updated_at, expires_at
			) VALUES ($1,77,$2,$3,$4,'in_progress',NOW(),NOW(),NOW()+INTERVAL '1 day')`,
			orgID, strings.Repeat("a", 64), strings.Repeat("b", 64), requestID)
		return err
	}())
	return b, orgID, model, attemptID
}

func p6f41ProviderUsage(requestID, attemptID string, orgID int64) SettleRequest {
	return SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		PromptTokens: 20, UncachedPromptTokens: 20, CompletionTokens: 5,
		UsageSource:          metering.UsageSourceUpstream,
		UsageAuthority:       metering.UsageAuthorityProviderReported,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion,
		CacheUsageStatus:     cacheUsageStatusNotReported,
	}
}

func TestP6F41BNonStreamChargeRequiresCompletedLocalDelivery(t *testing.T) {
	const requestID = "op_p6f41_delivery_complete"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	req := p6f41ProviderUsage(requestID, attemptID, orgID)

	_, err := b.Settle(t.Context(), req)
	require.ErrorIs(t, err, ErrDeliveryNotChargeable, "a provider attempt cannot bypass local delivery preparation")
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), req)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	_, err = b.Settle(t.Context(), req)
	require.ErrorIs(t, err, ErrDeliveryNotChargeable, "a funded hold without local delivery cannot charge the customer")
	var totalSpent, terminalCount int
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent FROM wallets WHERE org_id=$1`, orgID).Scan(&totalSpent))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&terminalCount))
	require.Zero(t, totalSpent, "delivery preparation is a hold, not a customer charge")
	require.Zero(t, terminalCount)

	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	_, err = b.Settle(t.Context(), req)
	require.ErrorIs(t, err, ErrDeliveryNotChargeable, "write_started alone is not customer delivery")
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 512, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	var preSettlementAttemptState string
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID).Scan(&preSettlementAttemptState))
	require.Equal(t, providerAttemptStateDeliveryPending, preSettlementAttemptState, "completed delivery must stay blocked until the customer ledger terminal exists")
	settled, err := b.Settle(t.Context(), req)
	require.NoError(t, err)
	require.True(t, settled.Success)

	var reservationState, attemptState, pendingReason string
	var receiptCount int
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT status,settlement_pending_reason FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&reservationState, &pendingReason))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID).Scan(&attemptState))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&receiptCount))
	require.Equal(t, "settled", reservationState)
	require.Empty(t, pendingReason)
	require.Equal(t, providerAttemptStateSettled, attemptState)
	require.Equal(t, 2, receiptCount)

	var authority, purpose, finality, deliveryMode, deliveryOutcome string
	var deliveredBytes int64
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT usage_authority,usage_purpose,usage_finality,delivery_mode,delivery_outcome,delivered_bytes
		FROM request_idempotency WHERE org_id=$1 AND operation_id=$2`, orgID, requestID,
	).Scan(&authority, &purpose, &finality, &deliveryMode, &deliveryOutcome, &deliveredBytes))
	require.Equal(t, string(metering.UsageAuthorityProviderReported), authority)
	require.Equal(t, string(metering.UsagePurposeCustomerCharge), purpose)
	require.Equal(t, string(metering.UsageFinalityFinal), finality)
	require.Equal(t, DeliveryModeNonStream, deliveryMode)
	require.Equal(t, DeliveryOutcomeCompleted, deliveryOutcome)
	require.EqualValues(t, 512, deliveredBytes)
}

func TestP6F41BWriterFailureDoesNotChargeAndRequiresExplicitRelease(t *testing.T) {
	const requestID = "op_p6f41_delivery_failed"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	req := p6f41ProviderUsage(requestID, attemptID, orgID)
	prepared, err := b.PrepareNonStreamDelivery(t.Context(), req)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteFailed,
		ErrorCode: "downstream_write_failed", HTTPStatus: 200,
	}))

	var totalSpent int
	var reservationState, attemptState, pendingReason string
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent FROM wallets WHERE org_id=$1`, orgID).Scan(&totalSpent))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT status,settlement_pending_reason FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&reservationState, &pendingReason))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID).Scan(&attemptState))
	require.Zero(t, totalSpent)
	require.Equal(t, "settlement_pending", reservationState)
	require.Equal(t, "delivery_failed", pendingReason)
	require.Equal(t, providerAttemptStateDeliveryFailed, attemptState)
	require.ErrorIs(t, b.Cancel(t.Context(), requestID, orgID), ErrSettlementPending)

	_, err = b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
		Action: ProviderReconciliationRefund, ActorUserID: 9,
		Reason: "local response writer failed", EvidenceRef: "delivery://writer-failure",
	})
	require.NoError(t, err)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent FROM wallets WHERE org_id=$1`, orgID).Scan(&totalSpent))
	require.Zero(t, totalSpent)
}

func TestP6F41BStreamPartialDeliveryChargesOnlyDurableCounterUsage(t *testing.T) {
	const requestID = "op_p6f41_stream_partial"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	req := SettleRequest{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		PromptTokens: 12, UncachedPromptTokens: 12, CompletionTokens: 2,
		UsageSource:          metering.UsageSourceCounterV2,
		UsageAuthority:       metering.UsageAuthorityGatewayCounter,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityEstimated,
		UsageContractVersion: metering.UsageContractVersion,
		UsageEstimateVersion: metering.GatewayCounterEstimateVersionV2,
		CacheUsageStatus:     cacheUsageStatusNotReported,
	}
	require.NoError(t, b.MarkBillableOutput(t.Context(), requestID, orgID))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	require.NoError(t, b.PersistSettlementUsage(t.Context(), req))
	settled, err := b.Settle(t.Context(), req)
	require.NoError(t, err)
	require.True(t, settled.Success)

	var authority, outcome string
	var promptTokens, completionTokens int
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT f.usage_authority,f.prompt_tokens,f.completion_tokens,r.outcome
		FROM freeze_idempotency f
		JOIN request_delivery_receipts r ON r.org_id=f.org_id AND r.request_id=f.request_id
		WHERE f.org_id=$1 AND f.request_id=$2
		ORDER BY r.sequence DESC LIMIT 1`, orgID, requestID,
	).Scan(&authority, &promptTokens, &completionTokens, &outcome))
	require.Equal(t, string(metering.UsageAuthorityGatewayCounter), authority)
	require.Equal(t, 12, promptTokens)
	require.Equal(t, 2, completionTokens)
	require.Equal(t, DeliveryOutcomePartial, outcome)
}

func TestP6F41BTerminalDeliveryDoesNotOverwriteFundingHold(t *testing.T) {
	const requestID = "op_p6f41_stream_funding_hold"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	req := p6f41ProviderUsage(requestID, attemptID, orgID)
	req.PromptTokens = 1_000_000
	req.UncachedPromptTokens = 1_000_000
	req.CompletionTokens = 0
	require.NoError(t, b.MarkBillableOutput(t.Context(), requestID, orgID))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200,
	}))
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomePartial,
		DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200,
	}))
	require.NoError(t, b.PersistSettlementUsage(t.Context(), req))
	result, err := b.Settle(t.Context(), req)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_topup", result.ErrorCode)
	require.NoError(t, b.RecordDelivery(t.Context(), DeliveryEvent{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Mode: DeliveryModeStream, Outcome: DeliveryOutcomeCompleted,
		DeliveredBytes: 256, DeliveredChunks: 2, HTTPStatus: 200,
	}))

	var reason, attemptState, outcome string
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT settlement_pending_reason FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&reason))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT state FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID).Scan(&attemptState))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT outcome FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2 ORDER BY sequence DESC LIMIT 1`, orgID, requestID).Scan(&outcome))
	require.Equal(t, "insufficient_topup", reason)
	require.Equal(t, providerAttemptStateFundingPending, attemptState)
	require.Equal(t, DeliveryOutcomeCompleted, outcome)
}

func TestP6F41BDeliveryReceiptConcurrentReplayAndTenantIsolation(t *testing.T) {
	const requestID = "op_p6f41_delivery_concurrent"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	require.NoError(t, func() error {
		_, err := b.PrepareNonStreamDelivery(t.Context(), p6f41ProviderUsage(requestID, attemptID, orgID))
		return err
	}())
	event := DeliveryEvent{RequestID: requestID, AttemptID: attemptID, OrgID: orgID, Mode: DeliveryModeNonStream, Outcome: DeliveryOutcomeWriteStarted, HTTPStatus: 200}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- b.RecordDelivery(t.Context(), event)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&count))
	require.Equal(t, 1, count)

	wrongTenant := event
	wrongTenant.OrgID = orgID + 1
	require.Error(t, b.RecordDelivery(t.Context(), wrongTenant))
	var wrongCount int
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, wrongTenant.OrgID, requestID).Scan(&wrongCount))
	require.Zero(t, wrongCount)
}

func TestP6F41BDeliveryReceiptCarriesNoPayloadColumns(t *testing.T) {
	b, _, _, _ := newP6F41DeliveryFixture(t, "op_p6f41_metadata_only")
	var forbidden int
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='request_delivery_receipts'
		  AND column_name IN ('prompt','request_body','response_body','body','content','payload','credential')`,
	).Scan(&forbidden))
	require.Zero(t, forbidden)
}
