BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000044_v1_xinsight_feed_control'
    ) THEN
        RAISE EXCEPTION 'X-Insight feed control rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM api_keys
        WHERE purpose = 'xinsight.feed.read'
          AND status IN ('feed_active', 'feed_staged')
    ) THEN
        RAISE EXCEPTION 'X-Insight feed control rollback refused: serving or staged feed credentials exist';
    END IF;
END;
$$;

UPDATE api_keys
SET status = 'disabled', updated_at = NOW()
WHERE purpose = 'xinsight.feed.read'
  AND status = 'feed_revoked';

DROP TABLE completed_execution_feed_connections;
DROP INDEX api_keys_one_staged_xinsight_feed_per_project;

ALTER TABLE api_keys
    DROP CONSTRAINT api_keys_completed_feed_status_check,
    ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
        (purpose = '' AND status <> 'feed_active')
        OR (
            purpose = 'xinsight.feed.read'
            AND status IN ('feed_active', 'disabled')
        )
    );

DELETE FROM schema_migrations
WHERE version = '000044_v1_xinsight_feed_control';

COMMIT;
