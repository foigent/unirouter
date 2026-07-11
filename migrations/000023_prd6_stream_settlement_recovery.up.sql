BEGIN;

-- P6-F18B records the irreversible boundary before the first billable stream
-- chunk is sent to the caller. Once output_started_at is set, the reservation
-- may only settle; it must never be released by cancellation or retention.
ALTER TABLE freeze_idempotency
    ADD COLUMN IF NOT EXISTS output_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_payload_ready_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS settlement_last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_claimed_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_next_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_last_error VARCHAR(512) NOT NULL DEFAULT '';

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_settlement_attempts_nonnegative
        CHECK (settlement_attempts >= 0),
    ADD CONSTRAINT freeze_idempotency_pending_has_output_marker
        CHECK (status <> 'settlement_pending' OR output_started_at IS NOT NULL),
    ADD CONSTRAINT freeze_idempotency_payload_requires_output_marker
        CHECK (settlement_payload_ready_at IS NULL OR output_started_at IS NOT NULL);

CREATE INDEX IF NOT EXISTS idx_freeze_idempotency_settlement_recovery
    ON freeze_idempotency(settlement_next_attempt_at, settlement_payload_ready_at, created_at)
    WHERE status = 'settlement_pending'
      AND settlement_payload_ready_at IS NOT NULL;

COMMIT;
