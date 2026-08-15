BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000065_model_endpoint_projection'
    ) THEN
        RAISE EXCEPTION 'Model endpoint projection rollback refused: later migrations must be reversed first';
    END IF;
END;
$$;

DROP TABLE IF EXISTS channel_model_probes;

DELETE FROM schema_migrations
WHERE version = '000065_model_endpoint_projection';

COMMIT;
