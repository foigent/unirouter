BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000068_provider_reported_model_identity'
    ) THEN
        RAISE EXCEPTION 'Provider-reported model identity rollback refused: later migrations must be reversed first';
    END IF;
END;
$$;

ALTER TABLE channel_model_probes
    DROP CONSTRAINT IF EXISTS channel_model_probes_reported_model_check,
    DROP COLUMN IF EXISTS reported_model;

DELETE FROM schema_migrations
WHERE version = '000068_provider_reported_model_identity';

COMMIT;
