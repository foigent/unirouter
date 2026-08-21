BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

-- Runtime reconciliation locks the reservation before its provider attempt,
-- then appends reconciliation and observation evidence. Freeze those writers
-- in the same order before changing their shared terminal contract.
LOCK TABLE freeze_idempotency, billing_provider_attempts,
    provider_cost_reconciliations, billing_observation_receipts,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE billing_observation_receipts
    ADD CONSTRAINT billing_observation_receipts_reconciliation_identity
        UNIQUE (org_id, request_id, id);

ALTER TABLE provider_cost_reconciliations
    ADD COLUMN terminal_observation_receipt_id BIGINT,
    DROP CONSTRAINT provider_cost_reconciliations_terminal_check,
    ADD CONSTRAINT provider_cost_reconciliations_terminal_check CHECK (
        (
            phase = 'requested'
            AND terminal_transaction_id IS NULL
            AND terminal_observation_receipt_id IS NULL
        )
        OR (
            phase = 'completed'
            AND (
                (terminal_transaction_id IS NOT NULL AND terminal_observation_receipt_id IS NULL)
                OR (terminal_transaction_id IS NULL AND terminal_observation_receipt_id IS NOT NULL)
            )
        )
    ),
    ADD CONSTRAINT provider_cost_reconciliations_observation_receipt_fk
        FOREIGN KEY (org_id, request_id, terminal_observation_receipt_id)
        REFERENCES billing_observation_receipts(org_id, request_id, id)
        ON DELETE NO ACTION
        DEFERRABLE INITIALLY IMMEDIATE;

CREATE UNIQUE INDEX idx_provider_cost_reconciliations_one_completed
    ON provider_cost_reconciliations(org_id, request_id, attempt_id)
    WHERE phase = 'completed';

CREATE FUNCTION enforce_provider_cost_reconciliation_completion()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    requested provider_cost_reconciliations%ROWTYPE;
    reservation_mode VARCHAR(32);
    reservation_status VARCHAR(32);
    reservation_transaction_id BIGINT;
    reservation_observation_receipt_id BIGINT;
    reservation_calculated BIGINT;
    reservation_pricing_version VARCHAR(80);
    reservation_usage_authority VARCHAR(40);
    reservation_usage_purpose VARCHAR(40);
    reservation_usage_finality VARCHAR(24);
    reservation_usage_contract_version VARCHAR(80);
    reservation_usage_estimate_version VARCHAR(80);
    attempt_state VARCHAR(32);
    attempt_transaction_id BIGINT;
    observation_status VARCHAR(16);
    observation_calculated BIGINT;
    observation_charged BIGINT;
    observation_pricing_version VARCHAR(80);
    observation_usage_authority VARCHAR(40);
    observation_usage_purpose VARCHAR(40);
    observation_usage_finality VARCHAR(24);
    observation_usage_contract_version VARCHAR(80);
    observation_usage_estimate_version VARCHAR(80);
BEGIN
    IF NEW.phase <> 'completed' THEN
        RETURN NEW;
    END IF;

    SELECT * INTO requested
      FROM provider_cost_reconciliations
     WHERE org_id=NEW.org_id
       AND request_id=NEW.request_id
       AND attempt_id=NEW.attempt_id
       AND phase='requested';
    IF NOT FOUND
       OR requested.api_key_id IS DISTINCT FROM NEW.api_key_id
       OR requested.action IS DISTINCT FROM NEW.action
       OR requested.actor_user_id IS DISTINCT FROM NEW.actor_user_id
       OR requested.reason IS DISTINCT FROM NEW.reason
       OR requested.evidence_ref IS DISTINCT FROM NEW.evidence_ref THEN
        RAISE EXCEPTION 'completed provider reconciliation lacks exact requested authority';
    END IF;

    SELECT reservation.billing_mode, reservation.status,
           reservation.terminal_transaction_id,
           reservation.terminal_observation_receipt_id,
           reservation.calculated_cost_micros, reservation.pricing_version,
           reservation.usage_authority, reservation.usage_purpose,
           reservation.usage_finality, reservation.usage_contract_version,
           reservation.usage_estimate_version,
           attempt.state, attempt.terminal_transaction_id
      INTO reservation_mode, reservation_status,
           reservation_transaction_id,
           reservation_observation_receipt_id,
           reservation_calculated, reservation_pricing_version,
           reservation_usage_authority, reservation_usage_purpose,
           reservation_usage_finality, reservation_usage_contract_version,
           reservation_usage_estimate_version,
           attempt_state, attempt_transaction_id
      FROM freeze_idempotency reservation
      JOIN billing_provider_attempts attempt
        ON attempt.org_id=reservation.org_id
       AND attempt.request_id=reservation.request_id
       AND attempt.attempt_id=NEW.attempt_id
     WHERE reservation.org_id=NEW.org_id
       AND reservation.request_id=NEW.request_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'completed provider reconciliation lacks terminal reservation and attempt evidence';
    END IF;

    IF NEW.terminal_observation_receipt_id IS NOT NULL THEN
        IF NEW.action='collect'
           OR reservation_mode<>'cost_observation'
           OR reservation_status<>'cancelled'
           OR reservation_transaction_id IS NOT NULL
           OR reservation_observation_receipt_id IS DISTINCT FROM NEW.terminal_observation_receipt_id
           OR attempt_state<>'reconciled'
           OR attempt_transaction_id IS NOT NULL THEN
            RAISE EXCEPTION 'observation reconciliation does not match its terminal reservation and attempt';
        END IF;
        SELECT terminal_status, calculated_cost_micros, charged_cost_micros,
               pricing_version, usage_authority, usage_purpose, usage_finality,
               usage_contract_version, usage_estimate_version
          INTO observation_status, observation_calculated, observation_charged,
               observation_pricing_version, observation_usage_authority,
               observation_usage_purpose, observation_usage_finality,
               observation_usage_contract_version, observation_usage_estimate_version
          FROM billing_observation_receipts
         WHERE org_id=NEW.org_id
           AND request_id=NEW.request_id
           AND id=NEW.terminal_observation_receipt_id;
        IF NOT FOUND
           OR observation_status<>'cancelled'
           OR observation_calculated IS DISTINCT FROM reservation_calculated
           OR observation_charged<>0
           OR observation_pricing_version IS DISTINCT FROM reservation_pricing_version
           OR observation_usage_authority IS DISTINCT FROM reservation_usage_authority
           OR observation_usage_purpose IS DISTINCT FROM reservation_usage_purpose
           OR observation_usage_finality IS DISTINCT FROM reservation_usage_finality
           OR observation_usage_contract_version IS DISTINCT FROM reservation_usage_contract_version
           OR observation_usage_estimate_version IS DISTINCT FROM reservation_usage_estimate_version THEN
            RAISE EXCEPTION 'observation reconciliation receipt is not an exact zero-charge cancellation';
        END IF;
        RETURN NEW;
    END IF;

    IF reservation_mode='cost_observation'
       OR reservation_observation_receipt_id IS NOT NULL
       OR reservation_transaction_id IS DISTINCT FROM NEW.terminal_transaction_id
       OR attempt_transaction_id IS DISTINCT FROM NEW.terminal_transaction_id
       OR (
           NEW.action='collect'
           AND (reservation_status<>'settled' OR attempt_state<>'settled')
       )
       OR (
           NEW.action IN ('refund','subsidy')
           AND (reservation_status<>'cancelled' OR attempt_state<>'reconciled')
       ) THEN
        RAISE EXCEPTION 'financial reconciliation does not match its terminal reservation and attempt';
    END IF;
    RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER provider_cost_reconciliation_completion_guard
AFTER INSERT ON provider_cost_reconciliations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_provider_cost_reconciliation_completion();

CREATE FUNCTION reject_provider_cost_reconciliation_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'provider cost reconciliation evidence is append-only';
END;
$$;

CREATE TRIGGER provider_cost_reconciliation_append_only
BEFORE UPDATE OR DELETE ON provider_cost_reconciliations
FOR EACH ROW EXECUTE FUNCTION reject_provider_cost_reconciliation_mutation();

COMMENT ON COLUMN provider_cost_reconciliations.terminal_observation_receipt_id IS
    'Zero-charge observation terminal authority; mutually exclusive with a financial transaction';

COMMIT;
