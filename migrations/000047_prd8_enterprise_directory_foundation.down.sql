BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE version > '000047_prd8_enterprise_directory_foundation'
    ) THEN
        RAISE EXCEPTION 'PRD8 enterprise-directory rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM enterprise_directory_sources) THEN
        RAISE EXCEPTION 'PRD8 enterprise-directory rollback refused: directory projection data exists';
    END IF;
END;
$$;

DROP TABLE enterprise_directory_unit_snapshots;
DROP TABLE enterprise_directory_snapshots;
DROP TABLE enterprise_directory_sources;

DELETE FROM schema_migrations
WHERE version = '000047_prd8_enterprise_directory_foundation';

COMMIT;
