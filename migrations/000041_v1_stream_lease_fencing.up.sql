BEGIN;

-- C1.3 is a non-rolling writer cutover. An old active stream can still author
-- unfenced state, so schema activation refuses to proceed until it is drained.
-- Acquire the same leading authority order used by stream writers and retain
-- every lock through commit. This closes the check/ALTER window: a pre-cutover
-- writer either finishes before the guard or resumes only after fenced schema
-- and triggers are active, where its unfenced write fails closed.
LOCK TABLE freeze_idempotency IN ACCESS EXCLUSIVE MODE;
LOCK TABLE stream_delivery_leases IN ACCESS EXCLUSIVE MODE;
LOCK TABLE request_delivery_receipts IN ACCESS EXCLUSIVE MODE;
LOCK TABLE stream_delivery_checkpoints IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM stream_delivery_leases
        WHERE state = 'active'
    ) THEN
        RAISE EXCEPTION 'C1.3 migration refused: drain active stream leases and old writers before generation fencing cutover';
    END IF;
END;
$$;

ALTER TABLE stream_delivery_leases
    ADD COLUMN generation BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT stream_delivery_leases_generation_positive
        CHECK (generation > 0);

ALTER TABLE stream_delivery_checkpoints
    ADD COLUMN lease_generation BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT stream_delivery_checkpoints_generation_positive
        CHECK (lease_generation > 0);

-- Historical receipts predate generation authority and intentionally remain
-- nullable. The transition trigger below requires the full fence for every
-- new stream receipt without inventing provenance for old evidence.
ALTER TABLE request_delivery_receipts
    ADD COLUMN stream_owner_id VARCHAR(80),
    ADD COLUMN stream_lease_generation BIGINT,
    ADD CONSTRAINT request_delivery_receipts_stream_fence_pair CHECK (
        (stream_owner_id IS NULL) = (stream_lease_generation IS NULL)
    ),
    ADD CONSTRAINT request_delivery_receipts_stream_fence_valid CHECK (
        stream_owner_id IS NULL
        OR (
            mode = 'stream'
            AND length(btrim(stream_owner_id)) > 0
            AND stream_lease_generation > 0
        )
    ),
    ADD CONSTRAINT request_delivery_receipts_nonstream_unfenced CHECK (
        mode <> 'nonstream'
        OR (stream_owner_id IS NULL AND stream_lease_generation IS NULL)
    );

ALTER TABLE freeze_idempotency
    ADD COLUMN settlement_stream_owner_id VARCHAR(80),
    ADD COLUMN settlement_stream_generation BIGINT,
    ADD CONSTRAINT freeze_idempotency_stream_fence_pair CHECK (
        (settlement_stream_owner_id IS NULL) = (settlement_stream_generation IS NULL)
    ),
    ADD CONSTRAINT freeze_idempotency_stream_fence_valid CHECK (
        settlement_stream_owner_id IS NULL
        OR (
            length(btrim(settlement_stream_owner_id)) > 0
            AND settlement_stream_generation > 0
            AND delivery_attempt_id IS NOT NULL
        )
    );

CREATE FUNCTION enforce_settlement_stream_fence_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.settlement_stream_owner_id IS NOT NULL
       AND (
           NEW.settlement_stream_owner_id IS DISTINCT FROM OLD.settlement_stream_owner_id
           OR NEW.settlement_stream_generation IS DISTINCT FROM OLD.settlement_stream_generation
       ) THEN
        RAISE EXCEPTION 'settlement stream fence is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER settlement_stream_fence_immutable
BEFORE UPDATE OF settlement_stream_owner_id, settlement_stream_generation
ON freeze_idempotency
FOR EACH ROW EXECUTE FUNCTION enforce_settlement_stream_fence_immutable();

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
    IF NEW.mode = 'stream'
       AND (
           NEW.stream_owner_id IS NULL
           OR length(btrim(NEW.stream_owner_id)) = 0
           OR NEW.stream_lease_generation IS NULL
           OR NEW.stream_lease_generation <= 0
       ) THEN
        RAISE EXCEPTION 'stream delivery receipt requires an ownership generation';
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
    IF NEW.mode = 'stream' AND previous.stream_lease_generation IS NOT NULL THEN
        IF NEW.stream_lease_generation < previous.stream_lease_generation
           OR (
               NEW.stream_lease_generation = previous.stream_lease_generation
               AND NEW.stream_owner_id <> previous.stream_owner_id
           ) THEN
            RAISE EXCEPTION 'stream delivery receipt fence cannot regress';
        END IF;
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
       OR NEW.lease_generation < previous.lease_generation
       OR NEW.delivered_bytes < previous.delivered_bytes
       OR NEW.delivered_chunks < previous.delivered_chunks
       OR NEW.prompt_tokens < previous.prompt_tokens
       OR NEW.completion_tokens < previous.completion_tokens THEN
        RAISE EXCEPTION 'stream checkpoint identity, fence, or counters are not monotonic';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
