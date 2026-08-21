BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE freeze_idempotency, billing_provider_attempts,
    provider_cost_reconciliations, billing_observation_receipts,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000080_observation_provider_cost_reconciliation'
    ) THEN
        RAISE EXCEPTION 'Observation provider-cost reconciliation rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM provider_cost_reconciliations
         WHERE terminal_observation_receipt_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Observation provider-cost reconciliation rollback refused: durable observation reconciliation evidence exists; roll forward instead';
    END IF;
END;
$$;

DROP TRIGGER provider_cost_reconciliation_append_only
    ON provider_cost_reconciliations;
DROP FUNCTION reject_provider_cost_reconciliation_mutation();

DROP TRIGGER provider_cost_reconciliation_completion_guard
    ON provider_cost_reconciliations;
DROP FUNCTION enforce_provider_cost_reconciliation_completion();

DROP INDEX idx_provider_cost_reconciliations_one_completed;

ALTER TABLE provider_cost_reconciliations
    DROP CONSTRAINT provider_cost_reconciliations_observation_receipt_fk,
    DROP CONSTRAINT provider_cost_reconciliations_terminal_check,
    DROP COLUMN terminal_observation_receipt_id,
    ADD CONSTRAINT provider_cost_reconciliations_terminal_check CHECK (
        (phase = 'requested' AND terminal_transaction_id IS NULL)
        OR (phase = 'completed' AND terminal_transaction_id IS NOT NULL)
    );

ALTER TABLE billing_observation_receipts
    DROP CONSTRAINT billing_observation_receipts_reconciliation_identity;

DELETE FROM schema_migrations
WHERE version='000080_observation_provider_cost_reconciliation';

COMMIT;
