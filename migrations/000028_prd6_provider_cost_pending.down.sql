BEGIN;

-- An old binary would treat a provider-cost/funding hold as cancellable or
-- would lose the additional prepaid charge. Refuse destructive rollback once
-- either behavior has been exercised.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM freeze_idempotency
        WHERE provider_cost_pending_at IS NOT NULL
           OR topup_amount > 0
    ) THEN
        RAISE EXCEPTION 'P6-F40 down refused: provider-cost pending or top-up evidence exists; resolve rows and roll forward instead';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_freeze_idempotency_provider_cost_pending;
DROP INDEX IF EXISTS idx_freeze_idempotency_settlement_recovery;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_pending_reason_valid,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_topup_nonnegative,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_payload_requires_financial_marker,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_pending_has_financial_marker;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_pending_has_output_marker
        CHECK (status <> 'settlement_pending' OR output_started_at IS NOT NULL),
    ADD CONSTRAINT freeze_idempotency_payload_requires_output_marker
        CHECK (settlement_payload_ready_at IS NULL OR output_started_at IS NOT NULL);

CREATE INDEX idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL;

ALTER TABLE freeze_idempotency
    DROP COLUMN IF EXISTS topup_amount,
    DROP COLUMN IF EXISTS settlement_pending_reason,
    DROP COLUMN IF EXISTS provider_cost_pending_at;

DELETE FROM schema_migrations
WHERE version = '000028_prd6_provider_cost_pending';

COMMIT;
