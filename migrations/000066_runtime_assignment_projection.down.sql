BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000066_runtime_assignment_projection'
    ) THEN
        RAISE EXCEPTION 'Runtime assignment projection rollback refused: later migrations must be reversed first';
    END IF;
END;
$$;

DROP VIEW IF EXISTS runtime_channel_assignments_v1;

DELETE FROM schema_migrations
WHERE version = '000066_runtime_assignment_projection';

COMMIT;
