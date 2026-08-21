//go:build integration

package billing

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

func TestP6F10GuardrailWithheldOutputRetainsUsageWithoutCustomerCharge(t *testing.T) {
	const requestID = "op_p6f10_guardrail_withheld"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	req := p6f41ProviderUsage(requestID, attemptID, orgID)
	req.UsagePurpose = metering.UsagePurposeProviderEvidence

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- b.HoldNonStreamOutput(t.Context(), req, "guardrail_withheld")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var reservationState, pendingReason, usageAuthority, usagePurpose, attemptState, attemptCertainty string
	var calculatedCostMicros, totalSpentMicros int64
	var settleTransactions, deliveryReceipts int
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT status,settlement_pending_reason,usage_authority,usage_purpose,calculated_cost_micros
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&reservationState, &pendingReason, &usageAuthority, &usagePurpose, &calculatedCostMicros))
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT state,outcome_certainty FROM billing_provider_attempts
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID,
	).Scan(&attemptState, &attemptCertainty))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&totalSpentMicros))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&settleTransactions))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&deliveryReceipts))
	require.Equal(t, "settlement_pending", reservationState)
	require.Equal(t, "guardrail_withheld", pendingReason)
	require.Equal(t, string(metering.UsageAuthorityProviderReported), usageAuthority)
	require.Equal(t, string(metering.UsagePurposeProviderEvidence), usagePurpose)
	require.Equal(t, ProviderAttemptStateOutputWithheld, attemptState)
	require.Equal(t, providerAttemptCertaintyKnownUsage, attemptCertainty)
	require.Positive(t, calculatedCostMicros)
	require.Zero(t, totalSpentMicros)
	require.Zero(t, settleTransactions)
	require.Zero(t, deliveryReceipts)

	chargeReq := p6f41ProviderUsage(requestID, attemptID, orgID)
	_, err := b.Settle(t.Context(), chargeReq)
	require.Error(t, err, "provider evidence without local delivery cannot create customer debt")

	reconciled, err := b.ReconcileProviderAttempt(t.Context(), ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
		Action: ProviderReconciliationSubsidy, ActorUserID: 77,
		Reason:      "platform explicitly absorbs guardrail-withheld provider cost",
		EvidenceRef: "case://p6f10-guardrail-subsidy",
	})
	require.NoError(t, err)
	require.Equal(t, "reconciled", reconciled.State)
	require.Equal(t, "0", reconciled.CustomerChargedMicros)
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&totalSpentMicros))
	require.Zero(t, totalSpentMicros, "explicit platform subsidy still cannot charge the customer")
}

func TestP6F10FinalResponseSizeHoldUsesTheSameNoDeliveryCostBoundary(t *testing.T) {
	const requestID = "op_p6f10_response_too_large"
	b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
	req := p6f41ProviderUsage(requestID, attemptID, orgID)
	req.UsagePurpose = metering.UsagePurposeProviderEvidence
	require.NoError(t, b.HoldNonStreamOutput(t.Context(), req, "response_too_large"))

	var status, pendingReason, usagePurpose, attemptState string
	var totalSpentMicros int64
	var deliveryReceipts int
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT status,settlement_pending_reason,usage_purpose
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&status, &pendingReason, &usagePurpose))
	require.NoError(t, b.db.QueryRow(t.Context(), `
		SELECT state FROM billing_provider_attempts
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID,
	).Scan(&attemptState))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT total_spent_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&totalSpentMicros))
	require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&deliveryReceipts))
	require.Equal(t, "settlement_pending", status)
	require.Equal(t, "response_too_large", pendingReason)
	require.Equal(t, string(metering.UsagePurposeProviderEvidence), usagePurpose)
	require.Equal(t, ProviderAttemptStateOutputWithheld, attemptState)
	require.Zero(t, totalSpentMicros)
	require.Zero(t, deliveryReceipts)
}

func TestP6F10PostAcceptanceSafetyHoldsUseExistingProviderLiabilityReasons(t *testing.T) {
	for _, reason := range []string{
		OutputHoldReasonProviderResponseUnusable,
		OutputHoldReasonProviderAttemptUnresolved,
	} {
		t.Run(reason, func(t *testing.T) {
			requestID := "op_p6f10_" + reason
			b, orgID, _, attemptID := newP6F41DeliveryFixture(t, requestID)
			req := p6f41ProviderUsage(requestID, attemptID, orgID)
			req.UsagePurpose = metering.UsagePurposeProviderEvidence

			require.NoError(t, b.HoldNonStreamOutput(t.Context(), req, reason))
			require.NoError(t, b.HoldNonStreamOutput(t.Context(), req, reason), "exact replay must be idempotent")

			var reservationState, pendingReason, attemptState, attemptCertainty string
			var deliveryReceipts, settleTransactions int
			require.NoError(t, b.db.QueryRow(t.Context(), `
				SELECT status, settlement_pending_reason
				FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
			).Scan(&reservationState, &pendingReason))
			require.NoError(t, b.db.QueryRow(t.Context(), `
				SELECT state, outcome_certainty
				FROM billing_provider_attempts
				WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID,
			).Scan(&attemptState, &attemptCertainty))
			require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&deliveryReceipts))
			require.NoError(t, b.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&settleTransactions))

			require.Equal(t, "settlement_pending", reservationState)
			require.Equal(t, reason, pendingReason)
			require.Equal(t, ProviderAttemptStateOutputWithheld, attemptState)
			require.Equal(t, providerAttemptCertaintyKnownUsage, attemptCertainty)
			require.Zero(t, deliveryReceipts)
			require.Zero(t, settleTransactions)
		})
	}
}
