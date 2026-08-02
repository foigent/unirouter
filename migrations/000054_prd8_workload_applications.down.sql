BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000054_prd8_workload_applications'
    ) THEN
        RAISE EXCEPTION 'cannot roll back workload applications before later migrations';
    END IF;
    IF EXISTS (SELECT 1 FROM api_keys WHERE workload_application_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM workload_applications) THEN
        RAISE EXCEPTION 'cannot roll back workload applications while application or credential evidence exists';
    END IF;
END $$;

DROP INDEX IF EXISTS api_keys_workload_application_idx;
ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_workload_application_scope_fk;
ALTER TABLE api_keys
    DROP COLUMN IF EXISTS workload_application_id;

DROP TABLE IF EXISTS workload_applications;
DROP INDEX IF EXISTS projects_id_org_unique_idx;

DELETE FROM schema_migrations
WHERE version = '000054_prd8_workload_applications';

COMMIT;
