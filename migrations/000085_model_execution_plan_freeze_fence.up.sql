BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE freeze_idempotency, billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE freeze_idempotency
    ADD COLUMN execution_snapshot_hash VARCHAR(69),
    ADD CONSTRAINT freeze_idempotency_execution_snapshot_hash_check
        CHECK (
            execution_snapshot_hash IS NULL OR
            execution_snapshot_hash ~ '^mrs1_[0-9a-f]{64}$'
        );

COMMENT ON COLUMN freeze_idempotency.execution_snapshot_hash IS
    'Immutable modelrouting Plan snapshot bound at pricing snapshot/wallet Freeze';

ALTER TABLE billing_provider_attempts
    ADD COLUMN execution_snapshot_hash VARCHAR(69),
    ADD CONSTRAINT billing_provider_attempts_execution_snapshot_hash_check
        CHECK (
            execution_snapshot_hash IS NULL OR
            execution_snapshot_hash ~ '^mrs1_[0-9a-f]{64}$'
        );

COMMENT ON COLUMN billing_provider_attempts.execution_snapshot_hash IS
    'Immutable modelrouting Plan snapshot carried independently from legacy/canonical route-evidence format';

CREATE FUNCTION prevent_model_execution_plan_snapshot_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.execution_snapshot_hash IS DISTINCT FROM OLD.execution_snapshot_hash THEN
        RAISE EXCEPTION 'model execution Plan snapshot is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER freeze_execution_snapshot_immutable
BEFORE UPDATE OF execution_snapshot_hash ON freeze_idempotency
FOR EACH ROW EXECUTE FUNCTION prevent_model_execution_plan_snapshot_mutation();

CREATE TRIGGER provider_attempt_execution_snapshot_immutable
BEFORE UPDATE OF execution_snapshot_hash ON billing_provider_attempts
FOR EACH ROW EXECUTE FUNCTION prevent_model_execution_plan_snapshot_mutation();

COMMIT;
