BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM request_delivery_receipts)
       OR EXISTS (SELECT 1 FROM freeze_idempotency WHERE delivery_attempt_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM request_delivery_receipt_quarantine)
       OR EXISTS (SELECT 1 FROM delivery_attempt_quarantine) THEN
        RAISE EXCEPTION 'C1.2 rollback refused: attempt-bound or quarantined delivery evidence exists';
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

DROP INDEX idx_request_delivery_receipts_attempt_latest;
ALTER TABLE request_delivery_receipts
    DROP CONSTRAINT request_delivery_receipts_attempt_binding_fk,
    DROP CONSTRAINT request_delivery_receipts_attempt_provider_fk,
    DROP CONSTRAINT request_delivery_receipts_attempt_nonempty,
    DROP COLUMN attempt_id;

DROP TRIGGER freeze_delivery_attempt_immutable ON freeze_idempotency;
DROP FUNCTION enforce_freeze_delivery_attempt_immutable();
DROP INDEX idx_freeze_idempotency_delivery_attempt;
ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_delivery_attempt_provider_fk,
    DROP CONSTRAINT freeze_idempotency_delivery_attempt_unique,
    DROP CONSTRAINT freeze_idempotency_delivery_attempt_nonempty,
    DROP COLUMN delivery_attempt_id;

DROP TABLE request_delivery_receipt_quarantine;
DROP TABLE delivery_attempt_quarantine;

DELETE FROM schema_migrations
WHERE version = '000040_v1_delivery_attempt_binding';

COMMIT;
