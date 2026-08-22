BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_registry, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000081_versioned_model_execution_profiles'
    ) THEN
        RAISE EXCEPTION 'Versioned model execution profile rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_registry
         WHERE execution_profile_id <> 'unverified.chat'
            OR execution_profile_version <> '1.0.0'
    ) THEN
        RAISE EXCEPTION 'Versioned model execution profile rollback refused: durable profile selections exist; reset them explicitly or roll forward';
    END IF;
END;
$$;

ALTER TABLE model_registry
    DROP CONSTRAINT model_registry_execution_profile_identity_check,
    DROP COLUMN execution_profile_version,
    DROP COLUMN execution_profile_id;

DELETE FROM schema_migrations
WHERE version='000081_versioned_model_execution_profiles';

COMMIT;
