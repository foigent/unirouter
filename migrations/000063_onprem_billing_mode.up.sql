BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE freeze_idempotency, billing_reservation_extensions,
    usage_finalization_journal, schema_migrations IN SHARE ROW EXCLUSIVE MODE;

-- Historical reservations were created by the prepaid-only runtime. New
-- request writers always persist an explicit immutable mode snapshot.
ALTER TABLE freeze_idempotency
    ADD COLUMN billing_mode VARCHAR(32) NOT NULL DEFAULT 'prepaid_enforced',
    ADD COLUMN admission_calculated_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT freeze_idempotency_billing_mode_check CHECK (
        billing_mode IN ('cost_observation', 'budget_enforced', 'prepaid_enforced')
    ),
    ADD CONSTRAINT freeze_idempotency_admission_calculated_cost_nonnegative
        CHECK (admission_calculated_cost_micros >= 0);

-- The historical prepaid hold was the exact conservative admission estimate.
-- Preserve it explicitly so idempotent replays share the new result contract.
UPDATE freeze_idempotency
SET admission_calculated_cost_micros = frozen_amount_micros;

-- Observation terminals are audit receipts, not zero-value customer financial
-- transactions. They are independently addressable and append-only.
CREATE TABLE billing_observation_receipts (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    terminal_status VARCHAR(16) NOT NULL,
    calculated_cost_micros BIGINT NOT NULL DEFAULT 0,
    charged_cost_micros BIGINT NOT NULL DEFAULT 0,
    pricing_version VARCHAR(80) NOT NULL,
    usage_authority VARCHAR(40) NOT NULL DEFAULT 'none',
    usage_purpose VARCHAR(40) NOT NULL DEFAULT 'provider_evidence',
    usage_finality VARCHAR(24) NOT NULL DEFAULT 'missing',
    usage_contract_version VARCHAR(80) NOT NULL DEFAULT '',
    usage_estimate_version VARCHAR(80) NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_observation_receipt_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT billing_observation_receipt_identity UNIQUE (org_id, request_id),
    CONSTRAINT billing_observation_receipt_terminal_check
        CHECK (terminal_status IN ('settled', 'cancelled')),
    CONSTRAINT billing_observation_receipt_amounts_check
        CHECK (calculated_cost_micros >= 0 AND charged_cost_micros = 0)
);

CREATE FUNCTION reject_billing_observation_receipt_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'billing observation receipts are append-only';
END;
$$;

CREATE TRIGGER billing_observation_receipt_append_only
BEFORE UPDATE OR DELETE ON billing_observation_receipts
FOR EACH ROW EXECUTE FUNCTION reject_billing_observation_receipt_mutation();

ALTER TABLE freeze_idempotency
    ADD COLUMN terminal_observation_receipt_id BIGINT,
    ADD CONSTRAINT freeze_idempotency_observation_receipt_fk
        FOREIGN KEY (terminal_observation_receipt_id)
        REFERENCES billing_observation_receipts(id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT freeze_idempotency_terminal_authority_check CHECK (
        (
            billing_mode = 'cost_observation'
            AND terminal_transaction_id IS NULL
            AND (
                (status IN ('settled', 'cancelled') AND terminal_observation_receipt_id IS NOT NULL)
                OR (status NOT IN ('settled', 'cancelled') AND terminal_observation_receipt_id IS NULL)
            )
        )
        OR (
            billing_mode IN ('budget_enforced', 'prepaid_enforced')
            AND terminal_observation_receipt_id IS NULL
        )
    );

-- Stream growth in observation mode advances the immutable token/price
-- receipt without pretending that money was reserved.
ALTER TABLE billing_reservation_extensions
    ADD COLUMN calculated_cost_micros BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT billing_reservation_extensions_calculated_cost_nonnegative
        CHECK (calculated_cost_micros >= 0),
    DROP CONSTRAINT billing_reservation_extensions_status_check,
    ADD CONSTRAINT billing_reservation_extensions_status_check CHECK (
        (status = 'reserved' AND error_code = '')
        OR (status = 'observed' AND error_code = ''
            AND reserved_micros = 0 AND reserved_balance_micros = 0
            AND reserved_granted_micros = 0)
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
    IF NEW.status IN ('reserved', 'observed')
       AND (parent_sequence <> NEW.sequence OR parent_reserved_completion <> NEW.target_completion_tokens) THEN
        RAISE EXCEPTION 'successful extension does not match its parent reservation';
    END IF;
    IF NEW.status = 'rejected'
       AND (parent_sequence <> NEW.sequence - 1
            OR parent_reserved_completion + NEW.additional_completion_tokens <> NEW.target_completion_tokens) THEN
        RAISE EXCEPTION 'rejected extension does not match its parent reservation';
    END IF;
    RETURN NEW;
END;
$$;

-- The usage journal carries both calculated and charged values and now also
-- carries the reservation mode, so zero-charge observation remains explicit.
ALTER TABLE usage_finalization_journal
    ADD COLUMN billing_mode VARCHAR(32) NOT NULL DEFAULT 'prepaid_enforced',
    ADD CONSTRAINT usage_finalization_billing_mode_check CHECK (
        billing_mode IN ('cost_observation', 'budget_enforced', 'prepaid_enforced')
    );

COMMENT ON COLUMN freeze_idempotency.billing_mode IS
    'Immutable request billing contract snapshot';
COMMENT ON COLUMN freeze_idempotency.admission_calculated_cost_micros IS
    'Immutable conservative request-cost estimate; never implies a wallet hold or charge';
COMMENT ON COLUMN usage_finalization_journal.billing_mode IS
    'Billing mode copied from the immutable request reservation';

COMMIT;
