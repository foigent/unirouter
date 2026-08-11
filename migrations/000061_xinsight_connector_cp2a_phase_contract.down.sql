BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE xinsight_connector_phase_operations,
    xinsight_connector_control_bindings,
    xinsight_connector_operations,
    xinsight_connector_installations,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000061_xinsight_connector_cp2a_phase_contract'
    ) THEN
        RAISE EXCEPTION 'X-Insight Connector CP2A rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM xinsight_connector_phase_operations)
       OR EXISTS (SELECT 1 FROM xinsight_connector_control_bindings) THEN
        RAISE EXCEPTION 'X-Insight Connector CP2A rollback refused: durable binding or phase evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM xinsight_connector_installations
        WHERE installation_id !~ '^[a-z0-9][a-z0-9._:-]{0,127}$'
           OR x_deployment_id !~ '^[a-z0-9][a-z0-9._:-]{0,127}$'
    ) THEN
        RAISE EXCEPTION 'X-Insight Connector CP2A rollback refused: exact-case identity cannot be represented by migration 60';
    END IF;
END $$;

DROP TABLE xinsight_connector_phase_operations;
DROP TABLE xinsight_connector_control_bindings;

ALTER TABLE xinsight_connector_installations
    DROP CONSTRAINT xinsight_connector_installation_id_shape,
    DROP CONSTRAINT xinsight_connector_deployment_id_shape;

ALTER TABLE xinsight_connector_installations
    ADD CONSTRAINT xinsight_connector_installation_id_shape
        CHECK (installation_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
    ADD CONSTRAINT xinsight_connector_deployment_id_shape
        CHECK (x_deployment_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$');

DELETE FROM schema_migrations
WHERE version = '000061_xinsight_connector_cp2a_phase_contract';

COMMIT;
