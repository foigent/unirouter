BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE freeze_idempotency, billing_observation_receipts,
    billing_reservation_extensions, usage_finalization_journal,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000063_onprem_billing_mode'
    ) THEN
        RAISE EXCEPTION 'on-prem billing mode rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM freeze_idempotency WHERE billing_mode <> 'prepaid_enforced')
       OR EXISTS (SELECT 1 FROM billing_observation_receipts)
       OR EXISTS (
           SELECT 1 FROM billing_reservation_extensions
           WHERE status = 'observed' OR calculated_cost_micros <> 0
       )
       OR EXISTS (SELECT 1 FROM usage_finalization_journal WHERE billing_mode <> 'prepaid_enforced') THEN
        RAISE EXCEPTION 'on-prem billing mode rollback refused: non-prepaid billing evidence exists';
    END IF;
END $$;

ALTER TABLE usage_finalization_journal
    DROP CONSTRAINT usage_finalization_billing_mode_check,
    DROP COLUMN billing_mode;

ALTER TABLE billing_reservation_extensions
    DROP CONSTRAINT billing_reservation_extensions_status_check,
    DROP CONSTRAINT billing_reservation_extensions_calculated_cost_nonnegative,
    DROP COLUMN calculated_cost_micros,
    ADD CONSTRAINT billing_reservation_extensions_status_check CHECK (
        (status = 'reserved' AND error_code = '')
        OR (status = 'rejected' AND reserved_micros = 0
            AND reserved_balance_micros = 0 AND reserved_granted_micros = 0
            AND length(btrim(error_code)) > 0)
    );

CREATE OR REPLACE FUNCTION enforce_billing_reservation_extension_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    parent_reserved_completion INTEGER;
    parent_sequence INTEGER;
    parent_pricing_version VARCHAR(80);
    previous_sequence INTEGER;
BEGIN
    SELECT reserved_completion_tokens, reservation_extension_sequence, pricing_version
    INTO parent_reserved_completion, parent_sequence, parent_pricing_version
    FROM freeze_idempotency
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    FOR UPDATE;

    SELECT COALESCE(MAX(sequence), 0)
    INTO previous_sequence
    FROM billing_reservation_extensions
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id;

    IF NEW.sequence <> previous_sequence + 1 OR NEW.pricing_version <> parent_pricing_version THEN
        RAISE EXCEPTION 'reservation extension sequence or pricing version is not monotonic';
    END IF;
    IF NEW.status = 'reserved'
       AND (parent_sequence <> NEW.sequence OR parent_reserved_completion <> NEW.target_completion_tokens) THEN
        RAISE EXCEPTION 'reserved extension does not match its parent reservation';
    END IF;
    IF NEW.status = 'rejected'
       AND (parent_sequence <> NEW.sequence - 1
            OR parent_reserved_completion + NEW.additional_completion_tokens <> NEW.target_completion_tokens) THEN
        RAISE EXCEPTION 'rejected extension does not match its parent reservation';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_terminal_authority_check,
    DROP CONSTRAINT freeze_idempotency_observation_receipt_fk,
    DROP COLUMN terminal_observation_receipt_id;

DROP TRIGGER billing_observation_receipt_append_only ON billing_observation_receipts;
DROP FUNCTION reject_billing_observation_receipt_mutation();
DROP TABLE billing_observation_receipts;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_billing_mode_check,
    DROP CONSTRAINT freeze_idempotency_admission_calculated_cost_nonnegative,
    DROP COLUMN admission_calculated_cost_micros,
    DROP COLUMN billing_mode;

DELETE FROM schema_migrations
WHERE version = '000063_onprem_billing_mode';

COMMIT;
