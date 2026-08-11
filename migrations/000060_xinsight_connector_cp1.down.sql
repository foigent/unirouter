BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE xinsight_connector_operations, xinsight_connector_installations,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000060_xinsight_connector_cp1'
    ) THEN
        RAISE EXCEPTION 'X-Insight Connector CP1 rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM xinsight_connector_operations)
       OR EXISTS (SELECT 1 FROM xinsight_connector_installations) THEN
        RAISE EXCEPTION 'X-Insight Connector CP1 rollback refused: durable installation or operation evidence exists';
    END IF;
END $$;

DROP TABLE xinsight_connector_operations;
DROP TABLE xinsight_connector_installations;

DELETE FROM schema_migrations
WHERE version = '000060_xinsight_connector_cp1';

COMMIT;
