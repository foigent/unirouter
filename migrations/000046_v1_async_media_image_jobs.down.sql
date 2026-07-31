BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE version > '000046_v1_async_media_image_jobs'
    ) THEN
        RAISE EXCEPTION 'V1 async-media rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM async_media_jobs) THEN
        RAISE EXCEPTION 'V1 async-media rollback refused: durable jobs exist';
    END IF;
END;
$$;

DROP TABLE async_media_charge_facts;
DROP TABLE async_media_jobs;

DELETE FROM schema_migrations
WHERE version = '000046_v1_async_media_image_jobs';

COMMIT;
