BEGIN;

-- Removing the recovery fields after billable output has been recorded would
-- make an older binary eligible to refund consumed work. Refuse that rollback
-- and require a forward repair after every pending row has reached settlement.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM freeze_idempotency
        WHERE output_started_at IS NOT NULL
           OR settlement_payload_ready_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'P6-F18B down refused: billable stream evidence exists; settle pending rows and roll forward instead';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_freeze_idempotency_settlement_recovery;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_payload_requires_output_marker,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_pending_has_output_marker,
    DROP CONSTRAINT IF EXISTS freeze_idempotency_settlement_attempts_nonnegative,
    DROP COLUMN IF EXISTS settlement_last_error,
    DROP COLUMN IF EXISTS settlement_next_attempt_at,
    DROP COLUMN IF EXISTS settlement_claimed_until,
    DROP COLUMN IF EXISTS settlement_last_attempt_at,
    DROP COLUMN IF EXISTS settlement_attempts,
    DROP COLUMN IF EXISTS settlement_payload_ready_at,
    DROP COLUMN IF EXISTS output_started_at;

DELETE FROM schema_migrations
WHERE version = '000023_prd6_stream_settlement_recovery';

COMMIT;
