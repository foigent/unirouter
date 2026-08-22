BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_registry, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE model_registry
    ADD COLUMN execution_profile_id VARCHAR(100) NOT NULL DEFAULT 'unverified.chat',
    ADD COLUMN execution_profile_version VARCHAR(40) NOT NULL DEFAULT '1.0.0',
    ADD CONSTRAINT model_registry_execution_profile_identity_check CHECK (
        execution_profile_id ~ '^[a-z0-9][a-z0-9._-]{2,99}$'
        AND execution_profile_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
    );

COMMENT ON COLUMN model_registry.execution_profile_id IS
    'Immutable repository-reviewed execution-contract bundle identity; customer context/output values may only narrow the bundle maxima.';
COMMENT ON COLUMN model_registry.execution_profile_version IS
    'Exact execution-contract bundle version included in planner snapshot evidence.';

COMMIT;
