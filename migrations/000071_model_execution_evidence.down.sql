BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000071_model_execution_evidence'
    ) THEN
        RAISE EXCEPTION 'Model execution evidence rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM billing_provider_attempts
         WHERE route_evidence_version IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Model execution evidence rollback refused: versioned provider attempt evidence exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS billing_provider_attempts_execution_identity_immutable
    ON billing_provider_attempts;
DROP FUNCTION IF EXISTS preserve_provider_attempt_execution_identity();
DROP INDEX IF EXISTS billing_provider_attempts_execution_route_idx;

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_route_release_fk,
    DROP CONSTRAINT billing_provider_attempts_route_probe_fk,
    DROP CONSTRAINT billing_provider_attempts_route_credential_connection_fk,
    DROP CONSTRAINT billing_provider_attempts_route_deployment_connection_fk,
    DROP CONSTRAINT billing_provider_attempts_route_public_model_fk,
    DROP CONSTRAINT billing_provider_attempts_execution_evidence_check,
    DROP CONSTRAINT billing_provider_attempts_identity_check,
    DROP COLUMN route_probe_lease_token_hash,
    DROP COLUMN route_requires_probe,
    DROP COLUMN route_legacy_credential_state_revision,
    DROP COLUMN route_legacy_circuit_revision,
    DROP COLUMN route_legacy_routing_revision,
    DROP COLUMN route_snapshot_hash,
    DROP COLUMN route_slot_version,
    DROP COLUMN route_runtime_stage,
    DROP COLUMN route_runtime_release_ref,
    DROP COLUMN route_runtime_release_id,
    DROP COLUMN route_credential_probe_tested_at,
    DROP COLUMN route_credential_probe_id,
    DROP COLUMN route_capability_evidence_hash,
    DROP COLUMN route_capability_evidence,
    DROP COLUMN route_operation_verified_at,
    DROP COLUMN route_operation_evidence_ref,
    DROP COLUMN route_operation_evidence_state,
    DROP COLUMN route_protocol_profile_version,
    DROP COLUMN route_protocol_profile_id,
    DROP COLUMN route_model_operation_revision,
    DROP COLUMN route_model_operation_id,
    DROP COLUMN route_credential_linked_at,
    DROP COLUMN route_credential_state_revision,
    DROP COLUMN route_circuit_revision,
    DROP COLUMN route_deployment_revision,
    DROP COLUMN route_connection_revision,
    DROP COLUMN route_credential_binding_id,
    DROP COLUMN route_connection_id,
    DROP COLUMN route_public_model_id,
    DROP COLUMN route_evidence_hash,
    DROP COLUMN route_execution_contract_source,
    DROP COLUMN route_authority_mode,
    DROP COLUMN route_evidence_version,
    ALTER COLUMN channel_id SET NOT NULL,
    ALTER COLUMN channel_key_id SET NOT NULL,
    ADD CONSTRAINT billing_provider_attempts_identity_check CHECK (
        length(btrim(attempt_id)) > 0
        AND length(btrim(endpoint)) > 0
        AND length(btrim(provider)) > 0
        AND length(btrim(channel_name)) > 0
        AND api_key_id > 0
        AND channel_id > 0
        AND channel_key_id > 0
    );

DELETE FROM schema_migrations
WHERE version = '000071_model_execution_evidence';

COMMIT;
