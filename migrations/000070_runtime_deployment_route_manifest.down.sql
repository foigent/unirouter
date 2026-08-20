BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_change_releases, runtime_change_slots,
    runtime_route_revision_items, model_deployment_operations,
    model_deployment_capabilities, model_deployments,
    provider_connections, provider_credential_bindings,
    credential_binding_deployments, model_deployment_probes,
    schema_migrations IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000070_runtime_deployment_route_manifest'
    ) THEN
        RAISE EXCEPTION 'Runtime deployment route manifest rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM runtime_change_releases
         WHERE candidate_version = 'unirouter.runtime-change-candidate.v2'
    ) OR EXISTS (
        SELECT 1 FROM runtime_change_slots WHERE public_model_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM runtime_route_revision_items
         WHERE source_candidate_version = 'unirouter.runtime-change-candidate.v2'
    ) THEN
        RAISE EXCEPTION 'Runtime deployment route manifest rollback refused: canonical release, current slot, or canonical-only route evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_deployment_operations
         WHERE operation_id <> 'legacy.any.v1'
    ) THEN
        RAISE EXCEPTION 'Runtime deployment route manifest rollback refused: explicit non-legacy protocol contracts would be lost';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS runtime_change_slots_public_model_guard ON runtime_change_slots;
DROP FUNCTION IF EXISTS enforce_runtime_slot_public_model();
DROP TRIGGER IF EXISTS runtime_change_releases_manifest_seal_guard ON runtime_change_releases;
DROP FUNCTION IF EXISTS enforce_runtime_release_manifest_seal();
DROP TRIGGER IF EXISTS runtime_route_revision_items_seal_guard ON runtime_route_revision_items;
DROP FUNCTION IF EXISTS enforce_runtime_route_item_identity();
DROP TRIGGER IF EXISTS credential_binding_deployments_update_guard ON credential_binding_deployments;
DROP FUNCTION IF EXISTS reject_credential_deployment_link_update();

-- A sealed v1 compatibility projection contains no new routing authority: its
-- original v1 candidate JSON and P1 normalized patch rows remain intact. Once
-- the seal guards are removed, discard only the 000070 metadata so the prior
-- binary can resume its v1 authority without guessing or deleting evidence.
UPDATE runtime_change_releases
   SET public_model_id=NULL,route_items_hash=NULL,route_items_count=NULL,
       route_items_sealed_at=NULL
 WHERE candidate_version='unirouter.runtime-change-candidate.v1'
   AND route_items_sealed_at IS NOT NULL;

ALTER TABLE runtime_route_revision_items
    DROP CONSTRAINT runtime_route_revision_items_protocol_profile_check,
    DROP CONSTRAINT runtime_route_revision_items_operation_revision_check,
    DROP COLUMN protocol_profile_version,
    DROP COLUMN protocol_profile_id,
    DROP COLUMN expected_operation_revision;

DROP TRIGGER IF EXISTS model_deployment_operations_revision_guard ON model_deployment_operations;
DROP FUNCTION IF EXISTS enforce_model_deployment_operation_revision();
DROP TRIGGER IF EXISTS model_deployment_capabilities_revision_guard ON model_deployment_capabilities;
DROP FUNCTION IF EXISTS enforce_model_deployment_capability_revision();
DROP TRIGGER IF EXISTS provider_credential_bindings_revision_guard ON provider_credential_bindings;
DROP FUNCTION IF EXISTS enforce_provider_credential_binding_revision();
DROP TRIGGER IF EXISTS model_deployments_probe_lease_guard ON model_deployments;
DROP FUNCTION IF EXISTS enforce_model_deployment_probe_lease();
ALTER TABLE model_deployments
    DROP CONSTRAINT IF EXISTS model_deployments_probe_lease_check;
DROP TRIGGER IF EXISTS model_deployments_revision_guard ON model_deployments;
DROP FUNCTION IF EXISTS enforce_model_deployment_revisions();
DROP TRIGGER IF EXISTS provider_connections_revision_guard ON provider_connections;
DROP FUNCTION IF EXISTS enforce_provider_connection_revision();

ALTER TABLE model_deployment_operations
    DROP CONSTRAINT model_deployment_operations_protocol_profile_check,
    DROP COLUMN protocol_profile_version,
    DROP COLUMN protocol_profile_id;

DROP INDEX IF EXISTS model_deployment_probes_binding_latest_idx;

DROP INDEX IF EXISTS runtime_change_slots_public_model_unique;
ALTER TABLE runtime_change_slots
    DROP COLUMN public_model_id;

DROP INDEX IF EXISTS runtime_change_releases_public_model_stage_idx;
ALTER TABLE runtime_change_releases
    DROP CONSTRAINT runtime_change_releases_identity_check,
    DROP COLUMN route_items_sealed_at,
    DROP COLUMN route_items_count,
    DROP COLUMN route_items_hash,
    DROP COLUMN public_model_id,
    ADD CONSTRAINT runtime_change_releases_identity_check CHECK (
        release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND model_name <> ''
        AND candidate_version = 'unirouter.runtime-change-candidate.v1'
        AND candidate_hash ~ '^sha256:[a-f0-9]{64}$'
        AND baseline_hash ~ '^sha256:[a-f0-9]{64}$'
        AND jsonb_typeof(candidate) = 'object'
        AND jsonb_typeof(provider_pack_pins) = 'array'
        AND jsonb_typeof(simulation_report) = 'object'
        AND reason <> ''
    );

DELETE FROM schema_migrations
WHERE version = '000070_runtime_deployment_route_manifest';

COMMIT;
