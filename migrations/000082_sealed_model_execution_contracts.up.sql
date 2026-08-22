BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

-- Release authoring takes runtime_change_releases before reading
-- model_registry. Preserve that order so the non-rolling schema transition
-- cannot form a release/model lock cycle with an in-flight writer.
LOCK TABLE runtime_change_releases, model_registry, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE runtime_change_releases
    ADD COLUMN execution_profile_id VARCHAR(100),
    ADD COLUMN execution_profile_version VARCHAR(40),
    ADD COLUMN public_context_limit INTEGER,
    ADD COLUMN public_output_limit INTEGER;

-- M81 made the current model profile explicit but did not pin it to releases.
-- The upgrade therefore freezes the exact behavior that was effective at the
-- migration boundary. Historical receipts remain historical; this backfill is
-- only the transition from mutable lookup to immutable release authority.
UPDATE runtime_change_releases release
   SET execution_profile_id = model.execution_profile_id,
       execution_profile_version = model.execution_profile_version,
       public_context_limit = model.context_length,
       public_output_limit = model.output_limit
  FROM model_registry model
 WHERE release.candidate_version='unirouter.runtime-change-candidate.v2'
   AND release.public_model_id=model.id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM runtime_change_releases
         WHERE candidate_version='unirouter.runtime-change-candidate.v2'
           AND (
                execution_profile_id IS NULL
                OR execution_profile_version IS NULL
                OR public_context_limit IS NULL
                OR public_output_limit IS NULL
           )
    ) THEN
        RAISE EXCEPTION 'Sealed model execution contract migration refused: a canonical release has no exact public model profile';
    END IF;
END;
$$;

ALTER TABLE runtime_change_releases
    ADD CONSTRAINT runtime_change_releases_execution_contract_check CHECK (
        (
            candidate_version='unirouter.runtime-change-candidate.v2'
            AND execution_profile_id ~ '^[a-z0-9][a-z0-9._-]{2,99}$'
            AND execution_profile_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
            AND public_context_limit > 0
            AND public_output_limit > 0
            AND public_output_limit <= public_context_limit
        )
        OR
        (
            candidate_version<>'unirouter.runtime-change-candidate.v2'
            AND execution_profile_id IS NULL
            AND execution_profile_version IS NULL
            AND public_context_limit IS NULL
            AND public_output_limit IS NULL
        )
    );

CREATE OR REPLACE FUNCTION enforce_runtime_release_execution_contract_v1()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP='INSERT'
       AND NEW.candidate_version='unirouter.runtime-change-candidate.v2' THEN
        PERFORM 1
          FROM model_registry model
         WHERE model.id=NEW.public_model_id
           AND model.model_name=NEW.model_name
           AND model.execution_profile_id=NEW.execution_profile_id
           AND model.execution_profile_version=NEW.execution_profile_version
           AND model.context_length=NEW.public_context_limit
           AND model.output_limit=NEW.public_output_limit
         FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'canonical release execution contract must equal the locked public model profile';
        END IF;
    END IF;
    IF TG_OP='UPDATE' AND (
        NEW.execution_profile_id IS DISTINCT FROM OLD.execution_profile_id
        OR NEW.execution_profile_version IS DISTINCT FROM OLD.execution_profile_version
        OR NEW.public_context_limit IS DISTINCT FROM OLD.public_context_limit
        OR NEW.public_output_limit IS DISTINCT FROM OLD.public_output_limit
    ) THEN
        RAISE EXCEPTION 'sealed model execution contract is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_change_releases_execution_contract_immutable
BEFORE INSERT OR UPDATE ON runtime_change_releases
FOR EACH ROW
EXECUTE FUNCTION enforce_runtime_release_execution_contract_v1();

COMMENT ON COLUMN runtime_change_releases.execution_profile_id IS
    'Exact repository-reviewed model execution profile pinned when the canonical release is authored; model_registry is only the source for a future release.';
COMMENT ON COLUMN runtime_change_releases.execution_profile_version IS
    'Exact model execution profile version pinned to this release.';
COMMENT ON COLUMN runtime_change_releases.public_context_limit IS
    'Customer-visible context upper bound pinned to this release; runtime safety may only narrow it.';
COMMENT ON COLUMN runtime_change_releases.public_output_limit IS
    'Customer-visible output upper bound pinned to this release; runtime safety may only narrow it.';

COMMIT;
