BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- P2 persists the exact, secret-free route proof before an upstream dispatch.
-- Historical attempts remain valid without versioned route evidence; no route
-- identity is inferred from mutable provider or channel display names.
ALTER TABLE billing_provider_attempts
    ALTER COLUMN channel_id DROP NOT NULL,
    ALTER COLUMN channel_key_id DROP NOT NULL,
    DROP CONSTRAINT billing_provider_attempts_identity_check,
    ADD COLUMN route_evidence_version VARCHAR(48),
    ADD COLUMN route_authority_mode VARCHAR(16),
    ADD COLUMN route_execution_contract_source VARCHAR(24),
    ADD COLUMN route_evidence_hash VARCHAR(71),
    ADD COLUMN route_public_model_id BIGINT,
    ADD COLUMN route_connection_id BIGINT,
    ADD COLUMN route_credential_binding_id BIGINT,
    ADD COLUMN route_connection_revision BIGINT,
    ADD COLUMN route_deployment_revision BIGINT,
    ADD COLUMN route_circuit_revision BIGINT,
    ADD COLUMN route_credential_state_revision BIGINT,
    ADD COLUMN route_credential_linked_at TIMESTAMPTZ(6),
    ADD COLUMN route_model_operation_id VARCHAR(80),
    ADD COLUMN route_model_operation_revision BIGINT,
    ADD COLUMN route_protocol_profile_id VARCHAR(100),
    ADD COLUMN route_protocol_profile_version VARCHAR(40),
    ADD COLUMN route_operation_evidence_state VARCHAR(32),
    ADD COLUMN route_operation_evidence_ref VARCHAR(512),
    ADD COLUMN route_operation_verified_at TIMESTAMPTZ(6),
    ADD COLUMN route_capability_evidence JSONB,
    ADD COLUMN route_capability_evidence_hash VARCHAR(71),
    ADD COLUMN route_credential_probe_id BIGINT,
    ADD COLUMN route_credential_probe_tested_at TIMESTAMPTZ(6),
    ADD COLUMN route_runtime_release_id BIGINT,
    ADD COLUMN route_runtime_release_ref VARCHAR(80),
    ADD COLUMN route_runtime_stage VARCHAR(24),
    ADD COLUMN route_slot_version BIGINT,
    ADD COLUMN route_snapshot_hash VARCHAR(69),
    ADD COLUMN route_legacy_routing_revision BIGINT,
    ADD COLUMN route_legacy_circuit_revision BIGINT,
    ADD COLUMN route_legacy_credential_state_revision BIGINT,
    ADD COLUMN route_requires_probe BOOLEAN,
    ADD COLUMN route_probe_lease_token_hash VARCHAR(71);

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_identity_check CHECK (
        length(btrim(attempt_id)) > 0
        AND length(btrim(endpoint)) > 0
        AND length(btrim(provider)) > 0
        AND length(btrim(channel_name)) > 0
        AND api_key_id > 0
        AND (
            (route_evidence_version IS NULL
             AND COALESCE(channel_id,0) > 0 AND COALESCE(channel_key_id,0) > 0)
            OR COALESCE(route_evidence_version,'') = 'unirouter.execution-route.v1'
        )
    ),
    ADD CONSTRAINT billing_provider_attempts_execution_evidence_check CHECK (
        (
            route_evidence_version IS NULL
            AND num_nonnulls(
                route_authority_mode,route_execution_contract_source,route_evidence_hash,
                route_public_model_id,route_connection_id,route_credential_binding_id,
                route_connection_revision,route_deployment_revision,route_circuit_revision,
                route_credential_state_revision,route_credential_linked_at,
                route_model_operation_id,route_model_operation_revision,
                route_protocol_profile_id,route_protocol_profile_version,
                route_operation_evidence_state,route_operation_evidence_ref,
                route_operation_verified_at,route_capability_evidence,
                route_capability_evidence_hash,route_credential_probe_id,
                route_credential_probe_tested_at,route_runtime_release_id,
                route_runtime_release_ref,route_runtime_stage,route_slot_version,
                route_snapshot_hash,route_legacy_routing_revision,
                route_legacy_circuit_revision,route_legacy_credential_state_revision,
                route_requires_probe,route_probe_lease_token_hash
            ) = 0
        )
        OR
        (
            COALESCE(route_evidence_version,'') = 'unirouter.execution-route.v1'
            AND COALESCE(route_authority_mode,'') IN ('legacy','verify','canonical')
            AND COALESCE(route_execution_contract_source,'') IN ('legacy_adapter','canonical_profile')
            AND COALESCE(route_evidence_hash,'') ~ '^sha256:[a-f0-9]{64}$'
            AND route_model_operation_id IS NOT NULL
            AND route_model_operation_id = BTRIM(route_model_operation_id)
            AND BTRIM(route_model_operation_id) <> ''
            AND route_model_operation_id <> 'legacy.any.v1'
            AND route_model_operation_id !~ E'[\\x00\\r\\n]'
            AND route_requires_probe IS NOT NULL
            AND (
                (route_requires_probe AND COALESCE(route_probe_lease_token_hash,'') ~ '^sha256:[a-f0-9]{64}$')
                OR (NOT route_requires_probe AND route_probe_lease_token_hash IS NULL)
            )
            AND (
                route_execution_contract_source = 'legacy_adapter'
                AND route_model_operation_revision IS NULL
                AND route_protocol_profile_id IS NULL
                AND route_protocol_profile_version IS NULL
                AND route_operation_evidence_state IS NULL
                AND route_operation_evidence_ref IS NULL
                AND route_operation_verified_at IS NULL
                AND route_capability_evidence IS NULL
                AND route_capability_evidence_hash IS NULL
                OR
                route_execution_contract_source = 'canonical_profile'
                AND COALESCE(route_model_operation_revision,0) > 0
                AND COALESCE(BTRIM(route_protocol_profile_id),'') <> ''
                AND route_protocol_profile_id NOT IN ('legacy.any.v1','legacy.channel_model_probe')
                AND COALESCE(BTRIM(route_protocol_profile_version),'') <> ''
                AND COALESCE(BTRIM(route_operation_evidence_state),'') <> ''
                AND COALESCE(BTRIM(route_operation_evidence_ref),'') <> ''
                AND route_operation_verified_at IS NOT NULL
                AND route_capability_evidence IS NOT NULL
                AND jsonb_typeof(route_capability_evidence) = 'array'
                AND pg_column_size(route_capability_evidence) <= 65536
                AND COALESCE(route_capability_evidence_hash,'') ~ '^sha256:[a-f0-9]{64}$'
            )
            AND (
                route_authority_mode = 'legacy'
                AND route_execution_contract_source = 'legacy_adapter'
                AND COALESCE(channel_id,0) > 0 AND COALESCE(channel_key_id,0) > 0
                AND COALESCE(route_legacy_routing_revision,0) > 0
                AND COALESCE(route_legacy_circuit_revision,0) > 0
                AND COALESCE(route_legacy_credential_state_revision,0) > 0
                AND route_public_model_id IS NULL AND deployment_id IS NULL
                AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
                AND route_connection_revision IS NULL AND route_deployment_revision IS NULL
                AND route_circuit_revision IS NULL AND route_credential_state_revision IS NULL
                AND route_credential_linked_at IS NULL
                AND route_credential_probe_id IS NULL AND route_credential_probe_tested_at IS NULL
                AND route_runtime_release_id IS NULL AND route_runtime_release_ref IS NULL
                AND route_runtime_stage IS NULL AND route_slot_version IS NULL
                AND route_snapshot_hash IS NULL
                OR
                route_authority_mode = 'verify'
                AND COALESCE(channel_id,0) > 0 AND COALESCE(channel_key_id,0) > 0
                AND COALESCE(route_legacy_routing_revision,0) > 0
                AND COALESCE(route_legacy_circuit_revision,0) > 0
                AND COALESCE(route_legacy_credential_state_revision,0) > 0
                AND COALESCE(route_public_model_id,0) > 0 AND COALESCE(deployment_id,0) > 0
                AND COALESCE(route_connection_id,0) > 0 AND COALESCE(route_credential_binding_id,0) > 0
                AND COALESCE(route_connection_revision,0) > 0 AND COALESCE(route_deployment_revision,0) > 0
                AND COALESCE(route_circuit_revision,0) > 0 AND COALESCE(route_credential_state_revision,0) > 0
                AND route_credential_linked_at IS NOT NULL
                AND COALESCE(route_credential_probe_id,0) > 0 AND route_credential_probe_tested_at IS NOT NULL
                AND COALESCE(route_runtime_release_id,0) > 0 AND COALESCE(BTRIM(route_runtime_release_ref),'') <> ''
                AND COALESCE(route_runtime_stage,'') IN ('active','canary') AND COALESCE(route_slot_version,0) > 0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
                OR
                route_authority_mode = 'canonical'
                AND route_execution_contract_source = 'canonical_profile'
                AND channel_id IS NULL AND channel_key_id IS NULL
                AND route_legacy_routing_revision IS NULL
                AND route_legacy_circuit_revision IS NULL
                AND route_legacy_credential_state_revision IS NULL
                AND COALESCE(route_public_model_id,0) > 0 AND COALESCE(deployment_id,0) > 0
                AND COALESCE(route_connection_id,0) > 0 AND COALESCE(route_credential_binding_id,0) > 0
                AND COALESCE(route_connection_revision,0) > 0 AND COALESCE(route_deployment_revision,0) > 0
                AND COALESCE(route_circuit_revision,0) > 0 AND COALESCE(route_credential_state_revision,0) > 0
                AND route_credential_linked_at IS NOT NULL
                AND COALESCE(route_credential_probe_id,0) > 0 AND route_credential_probe_tested_at IS NOT NULL
                AND COALESCE(route_runtime_release_id,0) > 0 AND COALESCE(BTRIM(route_runtime_release_ref),'') <> ''
                AND COALESCE(route_runtime_stage,'') IN ('active','canary') AND COALESCE(route_slot_version,0) > 0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
            )
        )
    ),
    ADD CONSTRAINT billing_provider_attempts_route_public_model_fk
        FOREIGN KEY (route_public_model_id,org_id)
        REFERENCES model_registry(id,org_id) ON DELETE RESTRICT,
    ADD CONSTRAINT billing_provider_attempts_route_deployment_connection_fk
        FOREIGN KEY (deployment_id,route_connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    ADD CONSTRAINT billing_provider_attempts_route_credential_connection_fk
        FOREIGN KEY (route_credential_binding_id,route_connection_id)
        REFERENCES provider_credential_bindings(id,connection_id) ON DELETE RESTRICT,
    -- route_model_operation_id is the actual requested execution operation.
    -- A v1 compatibility deployment intentionally owns only legacy.any.v1,
    -- so a deployment/operation FK would either reject truthful verify-mode
    -- receipts or force them to masquerade as that compatibility operation.
    -- canonical_profile rows are instead pinned by the complete operation
    -- revision/profile/evidence bundle and are validated before insertion.
    ADD CONSTRAINT billing_provider_attempts_route_probe_fk
        FOREIGN KEY (route_credential_probe_id)
        REFERENCES model_deployment_probes(probe_id) ON DELETE RESTRICT,
    ADD CONSTRAINT billing_provider_attempts_route_release_fk
        FOREIGN KEY (route_runtime_release_id)
        REFERENCES runtime_change_releases(id) ON DELETE RESTRICT;

CREATE INDEX billing_provider_attempts_execution_route_idx
    ON billing_provider_attempts(
        org_id,route_public_model_id,deployment_id,started_at,request_id
    ) WHERE route_evidence_version IS NOT NULL;

CREATE FUNCTION preserve_provider_attempt_execution_identity()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(
        NEW.org_id,NEW.request_id,NEW.attempt_id,NEW.api_key_id,
        NEW.project_id,NEW.access_credential_id,NEW.credential_ref,NEW.access_grant_id,
        NEW.channel_id,NEW.channel_key_id,NEW.deployment_id,
        NEW.route_sequence,NEW.route_total,NEW.endpoint,NEW.provider,NEW.channel_name,NEW.started_at,
        NEW.route_evidence_version,NEW.route_authority_mode,NEW.route_execution_contract_source,
        NEW.route_evidence_hash,NEW.route_public_model_id,NEW.route_connection_id,
        NEW.route_credential_binding_id,NEW.route_connection_revision,NEW.route_deployment_revision,
        NEW.route_circuit_revision,NEW.route_credential_state_revision,NEW.route_credential_linked_at,
        NEW.route_model_operation_id,NEW.route_model_operation_revision,
        NEW.route_protocol_profile_id,NEW.route_protocol_profile_version,
        NEW.route_operation_evidence_state,NEW.route_operation_evidence_ref,NEW.route_operation_verified_at,
        NEW.route_capability_evidence,NEW.route_capability_evidence_hash,
        NEW.route_credential_probe_id,NEW.route_credential_probe_tested_at,
        NEW.route_runtime_release_id,NEW.route_runtime_release_ref,NEW.route_runtime_stage,
        NEW.route_slot_version,NEW.route_snapshot_hash,
        NEW.route_legacy_routing_revision,NEW.route_legacy_circuit_revision,
        NEW.route_legacy_credential_state_revision,NEW.route_requires_probe,NEW.route_probe_lease_token_hash
    ) IS DISTINCT FROM ROW(
        OLD.org_id,OLD.request_id,OLD.attempt_id,OLD.api_key_id,
        OLD.project_id,OLD.access_credential_id,OLD.credential_ref,OLD.access_grant_id,
        OLD.channel_id,OLD.channel_key_id,OLD.deployment_id,
        OLD.route_sequence,OLD.route_total,OLD.endpoint,OLD.provider,OLD.channel_name,OLD.started_at,
        OLD.route_evidence_version,OLD.route_authority_mode,OLD.route_execution_contract_source,
        OLD.route_evidence_hash,OLD.route_public_model_id,OLD.route_connection_id,
        OLD.route_credential_binding_id,OLD.route_connection_revision,OLD.route_deployment_revision,
        OLD.route_circuit_revision,OLD.route_credential_state_revision,OLD.route_credential_linked_at,
        OLD.route_model_operation_id,OLD.route_model_operation_revision,
        OLD.route_protocol_profile_id,OLD.route_protocol_profile_version,
        OLD.route_operation_evidence_state,OLD.route_operation_evidence_ref,OLD.route_operation_verified_at,
        OLD.route_capability_evidence,OLD.route_capability_evidence_hash,
        OLD.route_credential_probe_id,OLD.route_credential_probe_tested_at,
        OLD.route_runtime_release_id,OLD.route_runtime_release_ref,OLD.route_runtime_stage,
        OLD.route_slot_version,OLD.route_snapshot_hash,
        OLD.route_legacy_routing_revision,OLD.route_legacy_circuit_revision,
        OLD.route_legacy_credential_state_revision,OLD.route_requires_probe,OLD.route_probe_lease_token_hash
    ) THEN
        RAISE EXCEPTION 'provider attempt execution identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER billing_provider_attempts_execution_identity_immutable
BEFORE UPDATE ON billing_provider_attempts
FOR EACH ROW EXECUTE FUNCTION preserve_provider_attempt_execution_identity();

COMMENT ON COLUMN billing_provider_attempts.route_evidence_version IS
    'Versioned, secret-free execution route receipt. NULL denotes historical pre-contract evidence only.';
COMMENT ON COLUMN billing_provider_attempts.route_evidence_hash IS
    'SHA-256 of the normalized executionevidence.EvidenceV1 payload used for idempotent replay matching.';
COMMENT ON COLUMN billing_provider_attempts.route_model_operation_id IS
    'Actual requested execution operation; legacy.any.v1 and probe-only profiles are never execution contracts.';

COMMIT;
