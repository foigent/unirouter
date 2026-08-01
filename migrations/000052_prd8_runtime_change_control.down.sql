BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000052_prd8_runtime_change_control'
    ) THEN
        RAISE EXCEPTION 'PRD8 runtime-change rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM runtime_change_events)
       OR EXISTS (SELECT 1 FROM runtime_change_slots WHERE stable_release_id IS NOT NULL OR canary_release_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM runtime_change_releases) THEN
        RAISE EXCEPTION 'PRD8 runtime-change rollback refused: release evidence exists';
    END IF;
    IF EXISTS (SELECT 1 FROM channels WHERE status = 'staged') THEN
        RAISE EXCEPTION 'PRD8 runtime-change rollback refused: staged channels exist';
    END IF;
END;
$$;

DROP TABLE runtime_change_events;
DROP TABLE runtime_change_slots;
DROP TABLE runtime_change_releases;

ALTER TABLE channels
    DROP CONSTRAINT channels_runtime_status_check;

DELETE FROM schema_migrations
WHERE version = '000052_prd8_runtime_change_control';

COMMIT;
