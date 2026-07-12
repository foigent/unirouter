BEGIN;

-- P6-F40 distinguishes a provider-cost/funding hold from a stream whose
-- output has already reached the caller. Both states are non-cancellable, but
-- only the latter may claim that delivery started.
-- Rollout boundary: drain old application writers before activating this
-- migration. Old binaries cannot preserve the new funding-pending semantics.
ALTER TABLE freeze_idempotency
    ADD COLUMN IF NOT EXISTS provider_cost_pending_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_pending_reason VARCHAR(40) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS topup_amount INTEGER NOT NULL DEFAULT 0;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_payload_requires_output_marker,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_pending_has_output_marker;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_pending_has_financial_marker
        CHECK (
            status <> 'settlement_pending'
            OR output_started_at IS NOT NULL
            OR provider_cost_pending_at IS NOT NULL
        ),
    ADD CONSTRAINT freeze_idempotency_payload_requires_financial_marker
        CHECK (
            settlement_payload_ready_at IS NULL
            OR output_started_at IS NOT NULL
            OR provider_cost_pending_at IS NOT NULL
        ),
    ADD CONSTRAINT freeze_idempotency_topup_nonnegative
        CHECK (topup_amount >= 0),
    ADD CONSTRAINT freeze_idempotency_pending_reason_valid
        CHECK (
            settlement_pending_reason IN ('', 'billable_output', 'insufficient_topup')
        );

-- Funding-shortfall rows require an explicit operator-owned decision. The
-- existing automatic stream recovery index deliberately excludes them.
DROP INDEX IF EXISTS idx_freeze_idempotency_settlement_recovery;
CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL
      AND settlement_pending_reason <> 'insufficient_topup';

CREATE INDEX idx_freeze_idempotency_provider_cost_pending
    ON freeze_idempotency(provider_cost_pending_at, org_id, request_id)
    WHERE status = 'settlement_pending'
      AND provider_cost_pending_at IS NOT NULL;

UPDATE freeze_idempotency
SET settlement_pending_reason = 'billable_output'
WHERE status = 'settlement_pending'
  AND output_started_at IS NOT NULL
  AND settlement_pending_reason = '';

COMMIT;
