-- Join the same lifecycle lock as migrate.Run before inspecting the migration
-- ledger; a future migration need not touch the receipt table.
BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

-- Freeze every writer before inspecting the durable evidence. Without this
-- lock, a concurrent create could commit after the emptiness check and before
-- DROP TABLE acquires its own lock, silently destroying the new receipt.
LOCK TABLE model_deployment_create_receipts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000074_model_deployment_create_idempotency'
    ) THEN
        RAISE EXCEPTION 'Model deployment create idempotency rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM model_deployment_create_receipts) THEN
        RAISE EXCEPTION 'Model deployment create idempotency rollback refused: durable mutation receipts exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS model_deployment_create_receipts_immutable
    ON model_deployment_create_receipts;
DROP FUNCTION IF EXISTS preserve_model_deployment_create_receipt();
DROP TABLE model_deployment_create_receipts;

DELETE FROM schema_migrations
WHERE version='000074_model_deployment_create_idempotency';

COMMIT;
