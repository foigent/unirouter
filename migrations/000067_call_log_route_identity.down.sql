BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000067_call_log_route_identity'
    ) THEN
        RAISE EXCEPTION 'Call-log route identity rollback refused: later migrations must be reversed first';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_call_logs_org_route_channel_created;

ALTER TABLE call_logs
    DROP COLUMN IF EXISTS route_channel_id;

DELETE FROM schema_migrations
WHERE version = '000067_call_log_route_identity';

COMMIT;
