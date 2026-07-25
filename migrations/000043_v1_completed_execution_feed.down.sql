BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000043_v1_completed_execution_feed'
    ) THEN
        RAISE EXCEPTION 'V1 completed-execution feed rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM completed_execution_feed_items)
       OR EXISTS (SELECT 1 FROM completed_execution_feed_states WHERE retained_after_id > 0)
       OR EXISTS (SELECT 1 FROM api_keys WHERE purpose <> '') THEN
        RAISE EXCEPTION 'V1 completed-execution feed rollback refused: retained feed state or scoped credentials exist';
    END IF;
END;
$$;

DROP TABLE completed_execution_feed_states;
DROP TABLE completed_execution_feed_items;

-- If P6-F10 is still installed, restore its original single-index topology.
-- If it was already rolled back, remove the feed-owned uniqueness basis too.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version = '000033_prd6_guardrail_kernel'
    ) THEN
        EXECUTE 'DROP INDEX idx_projects_guardrail_scope';
        EXECUTE 'ALTER INDEX completed_execution_feed_projects_scope_key RENAME TO idx_projects_guardrail_scope';
    ELSE
        EXECUTE 'DROP INDEX completed_execution_feed_projects_scope_key';
    END IF;
END;
$$;

DROP INDEX api_keys_one_active_xinsight_feed_per_project;
ALTER TABLE api_keys
    DROP CONSTRAINT api_keys_completed_feed_shape_check,
    DROP CONSTRAINT api_keys_completed_feed_status_check,
    DROP CONSTRAINT api_keys_completed_feed_purpose_check,
    DROP COLUMN purpose;

DELETE FROM schema_migrations
WHERE version = '000043_v1_completed_execution_feed';

COMMIT;
