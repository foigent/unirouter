BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_change_releases, model_registry, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000082_sealed_model_execution_contracts'
    ) THEN
        RAISE EXCEPTION 'Sealed model execution contract rollback refused: later migrations must be reversed first';
    END IF;

    -- M81 resolves execution behavior from the mutable model row. Downgrade is
    -- safe only while every canonical release would resolve to exactly the
    -- same profile and public limits after the pinned columns are removed.
    IF EXISTS (
        SELECT 1
          FROM runtime_change_releases release
          LEFT JOIN model_registry model ON model.id=release.public_model_id
         WHERE release.candidate_version='unirouter.runtime-change-candidate.v2'
           AND (
                model.id IS NULL
                OR release.execution_profile_id IS DISTINCT FROM model.execution_profile_id
                OR release.execution_profile_version IS DISTINCT FROM model.execution_profile_version
                OR release.public_context_limit IS DISTINCT FROM model.context_length
                OR release.public_output_limit IS DISTINCT FROM model.output_limit
           )
    ) THEN
        RAISE EXCEPTION 'Sealed model execution contract rollback refused: durable release pins would be lost';
    END IF;
END;
$$;

DROP TRIGGER runtime_change_releases_execution_contract_immutable
    ON runtime_change_releases;
DROP FUNCTION enforce_runtime_release_execution_contract_v1();

ALTER TABLE runtime_change_releases
    DROP CONSTRAINT runtime_change_releases_execution_contract_check,
    DROP COLUMN public_output_limit,
    DROP COLUMN public_context_limit,
    DROP COLUMN execution_profile_version,
    DROP COLUMN execution_profile_id;

DELETE FROM schema_migrations
WHERE version='000082_sealed_model_execution_contracts';

COMMIT;
