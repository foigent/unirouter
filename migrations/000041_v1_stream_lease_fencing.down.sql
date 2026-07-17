BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM stream_delivery_leases)
       OR EXISTS (
           SELECT 1 FROM request_delivery_receipts
           WHERE stream_owner_id IS NOT NULL OR stream_lease_generation IS NOT NULL
       )
       OR EXISTS (
           SELECT 1 FROM freeze_idempotency
           WHERE settlement_stream_owner_id IS NOT NULL OR settlement_stream_generation IS NOT NULL
       ) THEN
        RAISE EXCEPTION 'C1.3 rollback refused: stream generation or fenced side-effect evidence exists';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_request_delivery_receipt_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    previous request_delivery_receipts%ROWTYPE;
BEGIN
    IF length(btrim(NEW.attempt_id)) = 0 THEN
        RAISE EXCEPTION 'delivery receipt attempt identity is required';
    END IF;

    SELECT * INTO previous
    FROM request_delivery_receipts
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    ORDER BY sequence DESC
    LIMIT 1;

    IF NOT FOUND THEN
        IF NEW.sequence <> 1 OR NEW.outcome <> 'write_started'
           OR NEW.delivered_bytes <> 0 OR NEW.delivered_chunks <> 0 THEN
            RAISE EXCEPTION 'delivery receipt must begin with zero-byte write_started';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.attempt_id <> previous.attempt_id THEN
        RAISE EXCEPTION 'delivery receipt attempt identity is immutable';
    END IF;
    IF previous.outcome IN ('completed', 'write_failed', 'interrupted') THEN
        RAISE EXCEPTION 'delivery receipt is already terminal';
    END IF;
    IF NEW.sequence <> previous.sequence + 1 OR NEW.mode <> previous.mode THEN
        RAISE EXCEPTION 'delivery receipt sequence or mode is not monotonic';
    END IF;
    IF NEW.delivered_bytes < previous.delivered_bytes
       OR NEW.delivered_chunks < previous.delivered_chunks THEN
        RAISE EXCEPTION 'delivery receipt counters cannot decrease';
    END IF;
    IF previous.outcome = 'write_started'
       AND NEW.outcome NOT IN ('partial', 'completed', 'write_failed', 'interrupted') THEN
        RAISE EXCEPTION 'invalid delivery transition after write_started';
    END IF;
    IF previous.outcome = 'partial'
       AND NEW.outcome NOT IN ('completed', 'interrupted') THEN
        RAISE EXCEPTION 'invalid delivery transition after partial';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_stream_delivery_checkpoint_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    previous stream_delivery_checkpoints%ROWTYPE;
BEGIN
    SELECT * INTO previous
    FROM stream_delivery_checkpoints
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    ORDER BY sequence DESC
    LIMIT 1;

    IF NOT FOUND THEN
        IF NEW.sequence <> 1 THEN
            RAISE EXCEPTION 'stream checkpoint sequence must begin at one';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.sequence <> previous.sequence + 1
       OR NEW.attempt_id <> previous.attempt_id
       OR NEW.policy_set_version <> previous.policy_set_version
       OR NEW.delivered_bytes < previous.delivered_bytes
       OR NEW.delivered_chunks < previous.delivered_chunks
       OR NEW.prompt_tokens < previous.prompt_tokens
       OR NEW.completion_tokens < previous.completion_tokens THEN
        RAISE EXCEPTION 'stream checkpoint identity or counters are not monotonic';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER settlement_stream_fence_immutable ON freeze_idempotency;
DROP FUNCTION enforce_settlement_stream_fence_immutable();

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_stream_fence_valid,
    DROP CONSTRAINT freeze_idempotency_stream_fence_pair,
    DROP COLUMN settlement_stream_generation,
    DROP COLUMN settlement_stream_owner_id;

ALTER TABLE request_delivery_receipts
    DROP CONSTRAINT request_delivery_receipts_nonstream_unfenced,
    DROP CONSTRAINT request_delivery_receipts_stream_fence_valid,
    DROP CONSTRAINT request_delivery_receipts_stream_fence_pair,
    DROP COLUMN stream_lease_generation,
    DROP COLUMN stream_owner_id;

ALTER TABLE stream_delivery_checkpoints
    DROP CONSTRAINT stream_delivery_checkpoints_generation_positive,
    DROP COLUMN lease_generation;

ALTER TABLE stream_delivery_leases
    DROP CONSTRAINT stream_delivery_leases_generation_positive,
    DROP COLUMN generation;

DELETE FROM schema_migrations
WHERE version = '000041_v1_stream_lease_fencing';

COMMIT;
