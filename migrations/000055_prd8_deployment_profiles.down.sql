BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000055_prd8_deployment_profiles'
    ) THEN
        RAISE EXCEPTION 'PRD8 deployment-profile rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM gateway_endpoints) OR EXISTS (SELECT 1 FROM deployment_profiles) THEN
        RAISE EXCEPTION 'PRD8 deployment-profile rollback refused: desired-state or endpoint evidence exists';
    END IF;
END;
$$;

DROP TABLE gateway_endpoints;
DROP TABLE deployment_profiles;

DELETE FROM schema_migrations
WHERE version = '000055_prd8_deployment_profiles';

COMMIT;
