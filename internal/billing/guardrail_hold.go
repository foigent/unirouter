package billing

import (
	"context"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/outbox"
)

const ProviderAttemptStateOutputWithheld = "output_withheld"

// HoldNonStreamOutput retains provider-reported usage and the frozen customer
// price when a parseable response cannot be delivered because of a content
// policy or the final response safety limit. It creates no delivery receipt
// and no customer charge. The affected key remains on the existing
// provider-cost reconciliation path until an explicit refund or subsidy action
// resolves the upstream liability.
func (b *Billing) HoldNonStreamOutput(ctx context.Context, request SettleRequest, reason string) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	reason = strings.TrimSpace(reason)
	if reason != "guardrail_withheld" && reason != "response_too_large" {
		return fmt.Errorf("unsupported output hold reason %q", reason)
	}
	req, err := normalizeProviderEvidenceUsage(request)
	if err != nil {
		return err
	}
	if req.AttemptID == "" {
		return fmt.Errorf("output hold requires provider attempt identity")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin output hold: %w", err)
	}
	defer tx.Rollback(ctx)
	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return fmt.Errorf("lock output hold reservation: %w", err)
	}
	record = canonicalizeLegacyReservationUsage(record)
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		return fmt.Errorf("lock output hold attempt: %w", err)
	}
	if record.Status == "settlement_pending" && record.SettlementPendingReason == reason && attempt.State == ProviderAttemptStateOutputWithheld {
		if !settlementUsageMatches(record, req) {
			return ErrSettlementUsageConflict
		}
		return tx.Commit(ctx)
	}
	if record.Status != "frozen" || attempt.State != ProviderAttemptOutcomeResponseAccepted {
		return fmt.Errorf("cannot hold output in reservation=%s attempt=%s", record.Status, attempt.State)
	}
	calculatedCost, breakdown, err := calculateReservationCost(record, req)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET status='settlement_pending', settlement_pending_reason=$3,
		    provider_cost_pending_at=COALESCE(provider_cost_pending_at,NOW()),
		    prompt_tokens=$4, uncached_prompt_tokens=$5, cache_read_tokens=$6,
		    cache_creation_tokens=$7, completion_tokens=$8, usage_source=$9,
		    usage_authority=$10, usage_purpose=$11, usage_finality=$12,
		    usage_contract_version=$13, usage_estimate_version=$14,
		    cache_usage_schema=$15, cache_usage_status=$16,
		    calculated_cost=$17, uncached_input_cost_cents=$18,
		    cache_read_cost_cents=$19, cache_creation_cost_cents=$20,
		    output_cost_cents=$21, settlement_payload_ready_at=NOW(),
		    settlement_claimed_until=NULL, settlement_next_attempt_at=NULL,
		    settlement_last_error='', updated_at=NOW()
		WHERE request_id=$1 AND org_id=$2 AND status='frozen'`,
		req.RequestID, req.OrgID, reason, req.PromptTokens, req.UncachedPromptTokens,
		req.CacheReadTokens, req.CacheCreationTokens, req.CompletionTokens, req.UsageSource,
		req.UsageAuthority, req.UsagePurpose, req.UsageFinality, req.UsageContractVersion,
		req.UsageEstimateVersion, req.CacheUsageSchema, req.CacheUsageStatus, calculatedCost,
		breakdown.UncachedInputCostCents, breakdown.CacheReadCostCents,
		breakdown.CacheCreationCostCents, breakdown.OutputCostCents,
	); err != nil {
		return fmt.Errorf("persist output hold: %w", err)
	}
	result, err := tx.Exec(ctx, `
		UPDATE billing_provider_attempts
		SET state='output_withheld', outcome_certainty='known_usage',
		    retry_authorized=FALSE, calculated_cost=$4, updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
		  AND state='response_accepted'`,
		req.OrgID, req.RequestID, req.AttemptID, calculatedCost)
	if err != nil {
		return fmt.Errorf("mark provider output withheld: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("mark provider output withheld: authority changed")
	}
	if err := updateRequestIdempotencyUsageTx(ctx, tx, req); err != nil {
		return err
	}
	if err := enqueueUsageJournalTx(ctx, tx, b.outbox, outbox.EventBillingUsagePrepared, reason, record, req, calculatedCost, 0, 0); err != nil {
		return fmt.Errorf("enqueue output hold usage: %w", err)
	}
	return tx.Commit(ctx)
}

func normalizeProviderEvidenceUsage(req SettleRequest) (SettleRequest, error) {
	originalPurpose := req.UsagePurpose
	if originalPurpose != metering.UsagePurposeProviderEvidence && originalPurpose != metering.UsagePurposeCustomerCharge {
		return req, fmt.Errorf("output hold requires authoritative provider evidence")
	}
	req.UsagePurpose = metering.UsagePurposeCustomerCharge
	normalized, err := normalizeSettlementUsage(req)
	if err != nil {
		return req, err
	}
	normalized.UsagePurpose = metering.UsagePurposeProviderEvidence
	return normalized, nil
}
