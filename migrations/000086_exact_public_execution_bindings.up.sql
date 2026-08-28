BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_operation_execution_support_bindings,
    model_operation_public_execution_contract_extensions,
    model_operation_public_execution_bindings,
    runtime_route_revision_items, billing_provider_attempts,
    call_logs, request_trace_events, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- Public protocol support is append-only per exact tuple. The historical
-- operation-current view selected one tuple per operation and therefore could
-- not keep sealed Responses v1 releases executable while a new exact Responses
-- protocol became attachable. Preserve the historical row and append v2.
DROP TRIGGER model_operation_execution_support_bindings_immutable
    ON model_operation_execution_support_bindings;

INSERT INTO model_operation_execution_support_bindings (
    operation_id,public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,introduced_migration)
VALUES (
    'openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v2','1.0.0',
    'unirouter.endpoint-profile-responses-dual-executor.v1',
    '000086_exact_public_execution_bindings'
), (
    'anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v2','1.0.0',
    'unirouter.endpoint-profile-messages-dual-executor.v1',
    '000086_exact_public_execution_bindings'
);

CREATE TRIGGER model_operation_execution_support_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_execution_support_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_execution_support_mutation();

DROP TRIGGER model_operation_public_execution_contract_extensions_immutable
    ON model_operation_public_execution_contract_extensions;

ALTER TABLE model_operation_public_execution_contract_extensions
    DROP CONSTRAINT model_operation_public_execution_contract_extensions_identity_check,
    ADD CONSTRAINT model_operation_public_execution_contract_extensions_identity_check CHECK (
        charge_contract_version='unirouter.provider-final-token-charge.v1'
        AND idempotency_contract_version='unirouter.public-at-most-once.v1'
        AND (
            (
                introduced_migration='000077_public_model_execution_authority'
                AND delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND executor_contract_version='unirouter.endpoint-profile-nonstream-executor.v1'
                AND (
                    (operation_id='openai.responses.v1'
                     AND protocol_profile_id='unirouter.openai.responses.text.v1'
                     AND protocol_profile_version='1.0.0')
                    OR
                    (operation_id='anthropic.messages.v1'
                     AND protocol_profile_id='unirouter.anthropic.messages.text.v1'
                     AND protocol_profile_version='1.0.0')
                )
            )
            OR
            (
                introduced_migration='000086_exact_public_execution_bindings'
                AND protocol_profile_version='1.0.0'
                AND (
                    (
                        operation_id='openai.responses.v1'
                        AND protocol_profile_id='unirouter.openai.responses.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-responses-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-responses-delivery.v2'
                    )
                    OR
                    (
                        operation_id='anthropic.messages.v1'
                        AND protocol_profile_id='unirouter.anthropic.messages.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-messages-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-anthropic-messages-delivery.v2'
                    )
                )
            )
        )
    );

INSERT INTO model_operation_public_execution_contract_extensions (
    operation_id,public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,delivery_contract_version,
    charge_contract_version,idempotency_contract_version,introduced_migration)
VALUES (
    'openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v2','1.0.0',
    'unirouter.endpoint-profile-responses-dual-executor.v1',
    'unirouter.public-responses-delivery.v2',
    'unirouter.provider-final-token-charge.v1',
    'unirouter.public-at-most-once.v1',
    '000086_exact_public_execution_bindings'
), (
    'anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v2','1.0.0',
    'unirouter.endpoint-profile-messages-dual-executor.v1',
    'unirouter.public-anthropic-messages-delivery.v2',
    'unirouter.provider-final-token-charge.v1',
    'unirouter.public-at-most-once.v1',
    '000086_exact_public_execution_bindings'
);

CREATE TRIGGER model_operation_public_execution_contract_extensions_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_public_execution_contract_extensions
FOR EACH STATEMENT
EXECUTE FUNCTION reject_model_operation_public_execution_contract_mutation();

CREATE VIEW model_operation_exact_execution_support_bindings AS
SELECT *
  FROM model_operation_execution_support_bindings;

CREATE VIEW model_operation_exact_public_execution_contracts AS
SELECT support.operation_id,support.public_path,support.protocol_profile_id,
       support.protocol_profile_version,support.executor_contract_version,
       extension.delivery_contract_version,extension.charge_contract_version,
       extension.idempotency_contract_version
  FROM model_operation_exact_execution_support_bindings support
  JOIN model_operation_public_execution_contract_extensions extension
    ON extension.operation_id=support.operation_id
   AND extension.public_path=support.public_path
   AND extension.protocol_profile_id=support.protocol_profile_id
   AND extension.protocol_profile_version=support.protocol_profile_version
   AND extension.executor_contract_version=support.executor_contract_version;

-- Compatibility views retain their names because migration-owned triggers and
-- historical code reference them, but their authority is now exact tuple
-- membership rather than a lossy one-row-per-operation latest projection.
CREATE OR REPLACE VIEW model_operation_current_execution_support_bindings AS
SELECT * FROM model_operation_exact_execution_support_bindings;

CREATE OR REPLACE VIEW model_operation_current_public_execution_contracts AS
SELECT * FROM model_operation_exact_public_execution_contracts;

COMMENT ON VIEW model_operation_exact_execution_support_bindings IS
    'All append-only compiled public operation support tuples; callers must join the exact protocol profile and executor identity.';
COMMENT ON VIEW model_operation_exact_public_execution_contracts IS
    'Exact append-only public execution, delivery, charge and idempotency contract tuples.';
COMMENT ON VIEW model_operation_current_execution_support_bindings IS
    'Compatibility alias for exact support tuples; operation-only latest selection is forbidden.';
COMMENT ON VIEW model_operation_current_public_execution_contracts IS
    'Compatibility alias for exact public execution contracts; operation-only latest selection is forbidden.';

-- Keep the M77 legacy/common predicates unchanged. Public delivery identity is
-- owned by the existing exact binding FK and attempt-matching projection triggers,
-- not a second delivery allowlist. Non-NULL contracts prevent MATCH SIMPLE bypass.
ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_execution_evidence_check,
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
                route_requires_probe,route_probe_lease_token_hash,
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            ) = 0
        )
        OR
        (
            COALESCE(route_evidence_version,'') IN (
                'unirouter.execution-route.v1','unirouter.execution-route.v2'
            )
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
            AND (
                route_evidence_version='unirouter.execution-route.v1'
                AND route_model_operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
                AND route_public_execution_binding_id IS NULL
                AND route_delivery_contract_version IS NULL
                AND route_charge_contract_version IS NULL
                AND route_idempotency_contract_version IS NULL
                OR
                route_evidence_version='unirouter.execution-route.v2'
                AND route_authority_mode='canonical'
                AND route_execution_contract_source='canonical_profile'
                AND route_model_operation_id IN ('openai.responses.v1','anthropic.messages.v1')
                AND COALESCE(route_public_execution_binding_id,0)>0
                AND COALESCE(BTRIM(route_delivery_contract_version),'')<>''
                AND COALESCE(route_charge_contract_version,'')='unirouter.provider-final-token-charge.v1'
                AND COALESCE(route_idempotency_contract_version,'')='unirouter.public-at-most-once.v1'
            )
        )
    );

ALTER TABLE call_logs
    DROP CONSTRAINT call_logs_execution_projection_check,
    ADD CONSTRAINT call_logs_execution_projection_check CHECK (
        (
            route_evidence_source IS NULL
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
            AND route_model_operation_id IS NULL AND route_runtime_release_id IS NULL
            AND route_snapshot_hash IS NULL
            AND num_nonnulls(
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            )=0
        )
        OR (
            route_evidence_source IN ('response_cache','no_provider_attempt')
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
            AND route_model_operation_id IS NULL AND route_runtime_release_id IS NULL
            AND route_snapshot_hash IS NULL
            AND num_nonnulls(
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            )=0
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'precontract_provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND COALESCE(route_channel_id,0) > 0
            AND num_nonnulls(
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            )=0
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version IN (
                'unirouter.execution-route.v1','unirouter.execution-route.v2'
            )
            AND route_authority_mode IN ('legacy','verify','canonical')
            AND route_evidence_hash ~ '^sha256:[a-f0-9]{64}$'
            AND COALESCE(BTRIM(route_model_operation_id),'') <> ''
            AND route_model_operation_id = BTRIM(route_model_operation_id)
            AND route_model_operation_id <> 'legacy.any.v1'
            AND route_model_operation_id !~ E'[\\x00\\r\\n]'
            AND source_event_hash ~ '^[a-f0-9]{64}$'
            AND (
                route_authority_mode='legacy'
                AND COALESCE(route_channel_id,0)>0
                AND route_public_model_id IS NULL AND route_deployment_id IS NULL
                AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
                AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
                OR route_authority_mode='verify'
                AND COALESCE(route_channel_id,0)>0 AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
                OR route_authority_mode='canonical'
                AND route_channel_id IS NULL AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
            )
            AND (
                route_evidence_version='unirouter.execution-route.v1'
                AND route_model_operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
                AND num_nonnulls(
                    route_public_execution_binding_id,route_delivery_contract_version,
                    route_charge_contract_version,route_idempotency_contract_version
                )=0
                OR route_evidence_version='unirouter.execution-route.v2'
                AND route_authority_mode='canonical'
                AND route_model_operation_id IN ('openai.responses.v1','anthropic.messages.v1')
                AND COALESCE(route_public_execution_binding_id,0)>0
                AND COALESCE(BTRIM(route_delivery_contract_version),'')<>''
                AND COALESCE(route_charge_contract_version,'')='unirouter.provider-final-token-charge.v1'
                AND COALESCE(route_idempotency_contract_version,'')='unirouter.public-at-most-once.v1'
            )
        )
    );

ALTER TABLE request_trace_events
    DROP CONSTRAINT request_trace_events_execution_projection_check,
    ADD CONSTRAINT request_trace_events_execution_projection_check CHECK (
        (
            route_evidence_source IS NULL
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND route_channel_id IS NULL
            AND num_nonnulls(
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            )=0
        )
        OR (
            route_evidence_source IN ('response_cache','no_provider_attempt')
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND route_channel_id IS NULL
            AND num_nonnulls(
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            )=0
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'precontract_provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND COALESCE(route_channel_id,0) > 0
            AND num_nonnulls(
                route_public_execution_binding_id,route_delivery_contract_version,
                route_charge_contract_version,route_idempotency_contract_version
            )=0
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version IN (
                'unirouter.execution-route.v1','unirouter.execution-route.v2'
            )
            AND route_authority_mode IN ('legacy','verify','canonical')
            AND route_evidence_hash ~ '^sha256:[a-f0-9]{64}$'
            AND COALESCE(BTRIM(route_model_operation_id),'') <> ''
            AND route_model_operation_id = BTRIM(route_model_operation_id)
            AND route_model_operation_id <> 'legacy.any.v1'
            AND route_model_operation_id !~ E'[\\x00\\r\\n]'
            AND source_event_hash ~ '^[a-f0-9]{64}$'
            AND (
                route_authority_mode='legacy'
                AND COALESCE(route_channel_id,0)>0
                AND route_public_model_id IS NULL AND route_deployment_id IS NULL
                AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
                AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
                OR route_authority_mode='verify'
                AND COALESCE(route_channel_id,0)>0 AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
                OR route_authority_mode='canonical'
                AND route_channel_id IS NULL AND COALESCE(route_public_model_id,0)>0
                AND COALESCE(route_deployment_id,0)>0 AND COALESCE(route_connection_id,0)>0
                AND COALESCE(route_credential_binding_id,0)>0 AND COALESCE(route_runtime_release_id,0)>0
                AND COALESCE(route_snapshot_hash,'') ~ '^mrs1_[a-f0-9]{64}$'
            )
            AND (
                route_evidence_version='unirouter.execution-route.v1'
                AND route_model_operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
                AND num_nonnulls(
                    route_public_execution_binding_id,route_delivery_contract_version,
                    route_charge_contract_version,route_idempotency_contract_version
                )=0
                OR route_evidence_version='unirouter.execution-route.v2'
                AND route_authority_mode='canonical'
                AND route_model_operation_id IN ('openai.responses.v1','anthropic.messages.v1')
                AND COALESCE(route_public_execution_binding_id,0)>0
                AND COALESCE(BTRIM(route_delivery_contract_version),'')<>''
                AND COALESCE(route_charge_contract_version,'')='unirouter.provider-final-token-charge.v1'
                AND COALESCE(route_idempotency_contract_version,'')='unirouter.public-at-most-once.v1'
            )
        )
    );

COMMIT;
