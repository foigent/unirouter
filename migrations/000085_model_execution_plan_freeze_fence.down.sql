BEGIN;

-- Stabilize both evidence tables and the ledger before checking whether this
-- rollback is lossless. A concurrent writer must commit before the guard reads.
SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE freeze_idempotency, billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000085_model_execution_plan_freeze_fence'
    ) THEN
        RAISE EXCEPTION 'Model execution Plan freeze-fence rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM freeze_idempotency WHERE execution_snapshot_hash IS NOT NULL)
       OR EXISTS (SELECT 1 FROM billing_provider_attempts WHERE execution_snapshot_hash IS NOT NULL) THEN
        RAISE EXCEPTION 'Model execution Plan freeze-fence rollback refused: durable snapshot bindings exist';
    END IF;
END;
$$;

DROP TRIGGER provider_attempt_execution_snapshot_immutable ON billing_provider_attempts;
DROP TRIGGER freeze_execution_snapshot_immutable ON freeze_idempotency;
DROP FUNCTION prevent_model_execution_plan_snapshot_mutation();

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_execution_snapshot_hash_check,
    DROP COLUMN execution_snapshot_hash;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT freeze_idempotency_execution_snapshot_hash_check,
    DROP COLUMN execution_snapshot_hash;

DELETE FROM schema_migrations
WHERE version = '000085_model_execution_plan_freeze_fence';

COMMIT;
