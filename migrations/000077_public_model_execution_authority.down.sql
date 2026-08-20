BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_operation_public_execution_bindings,
    model_operation_public_execution_contract_extensions,
    runtime_change_releases, runtime_route_revision_items,
    billing_provider_attempts, call_logs, request_trace_events,
    access_requests, access_grants, access_grant_revisions,
    access_credentials, api_keys, model_call_authorizations,
    gateway_endpoints, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000077_public_model_execution_authority'
    ) THEN
        RAISE EXCEPTION 'Public model execution authority rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM model_operation_public_execution_bindings) THEN
        RAISE EXCEPTION 'Public model execution authority rollback refused: immutable public execution bindings exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM billing_provider_attempts
         WHERE route_evidence_version='unirouter.execution-route.v2'
    ) OR EXISTS (
        SELECT 1 FROM call_logs
         WHERE route_evidence_version='unirouter.execution-route.v2'
    ) OR EXISTS (
        SELECT 1 FROM request_trace_events
         WHERE route_evidence_version='unirouter.execution-route.v2'
    ) THEN
        RAISE EXCEPTION 'Public model execution authority rollback refused: immutable V2 execution evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM runtime_route_revision_items
         WHERE public_execution_binding_id IS NOT NULL
            OR delivery_contract_version IS NOT NULL
            OR charge_contract_version IS NOT NULL
            OR idempotency_contract_version IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Public model execution authority rollback refused: frozen M77 route membership exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM access_requests
         WHERE requested_endpoints ?| ARRAY['/v1/responses','/v1/messages']
    ) OR EXISTS (
        SELECT 1 FROM access_grants
         WHERE allowed_endpoints ?| ARRAY['/v1/responses','/v1/messages']
    ) OR EXISTS (
        SELECT 1 FROM access_grant_revisions
         WHERE allowed_endpoints ?| ARRAY['/v1/responses','/v1/messages']
    ) OR EXISTS (
        SELECT 1 FROM model_call_authorizations
         WHERE allowed_endpoints ?| ARRAY['/v1/responses','/v1/messages']
    ) OR EXISTS (
        SELECT 1 FROM access_credentials
         WHERE string_to_array(replace(allowed_endpoints,' ',''), ',')
               && ARRAY['/v1/responses','/v1/messages']
    ) OR EXISTS (
        SELECT 1 FROM api_keys
         WHERE string_to_array(replace(allowed_endpoints,' ',''), ',')
               && ARRAY['/v1/responses','/v1/messages']
    ) THEN
        RAISE EXCEPTION 'Public model execution authority rollback refused: durable public endpoint scope evidence exists';
    END IF;
    IF EXISTS (
        SELECT 1 FROM gateway_endpoints
         WHERE endpoint_profile='endpoint.anthropic-messages.v1'
    ) THEN
        RAISE EXCEPTION 'Public model execution authority rollback refused: Anthropic Messages Gateway Endpoint profile evidence exists';
    END IF;
END;
$$;

ALTER TABLE gateway_endpoints
    DROP CONSTRAINT gateway_endpoints_profile_check,
    ADD CONSTRAINT gateway_endpoints_profile_check CHECK (
        endpoint_profile IN (
            'endpoint.openai-chat.v1',
            'endpoint.openai-responses.contract.v1'
        )
    );

ALTER TABLE access_requests
    DROP CONSTRAINT access_requests_scope_check,
    ADD CONSTRAINT access_requests_scope_check CHECK (
        requested_project_role IN ('project_admin', 'project_user', 'project_viewer')
        AND requested_scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(requested_models) = 'array'
        AND jsonb_typeof(requested_endpoints) = 'array'
        AND requested_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]'::jsonb
        AND (
            (requested_scope_mode = 'project_default'
                AND jsonb_array_length(requested_models) = 0
                AND jsonb_array_length(requested_endpoints) = 0)
            OR
            (requested_scope_mode = 'explicit'
                AND jsonb_array_length(requested_models) > 0
                AND jsonb_array_length(requested_endpoints) > 0)
        )
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND (requested_rpm_limit IS NULL OR requested_rpm_limit BETWEEN 0 AND 2147483647)
        AND (requested_rps_limit IS NULL OR requested_rps_limit BETWEEN 0 AND 2147483647)
        AND (requested_concurrency_limit IS NULL OR requested_concurrency_limit BETWEEN 0 AND 2147483647)
        AND (requested_valid_until IS NULL OR requested_valid_until > submitted_at)
    );

ALTER TABLE access_grants
    DROP CONSTRAINT access_grants_identity_check,
    ADD CONSTRAINT access_grants_identity_check CHECK (
        grant_ref ~ '^grant_[a-z0-9][a-z0-9_-]{7,65}$'
        AND semantic_hash ~ '^[0-9a-f]{64}$'
        AND revision > 0
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND project_role_snapshot IN ('project_admin', 'project_user', 'project_viewer')
        AND scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(allowed_models) = 'array'
        AND jsonb_typeof(allowed_endpoints) = 'array'
        AND allowed_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]'::jsonb
        AND (
            (scope_mode = 'project_default'
                AND jsonb_array_length(allowed_models) = 0
                AND jsonb_array_length(allowed_endpoints) = 0)
            OR
            (scope_mode = 'explicit'
                AND jsonb_array_length(allowed_models) > 0
                AND jsonb_array_length(allowed_endpoints) > 0)
        )
        AND (rpm_limit IS NULL OR rpm_limit BETWEEN 0 AND 2147483647)
        AND (rps_limit IS NULL OR rps_limit BETWEEN 0 AND 2147483647)
        AND (concurrency_limit IS NULL OR concurrency_limit BETWEEN 0 AND 2147483647)
        AND (valid_until IS NULL OR valid_until > valid_from)
    );

ALTER TABLE access_grant_revisions
    DROP CONSTRAINT access_grant_revisions_scope_check,
    ADD CONSTRAINT access_grant_revisions_scope_check CHECK (
        revision > 0
        AND grant_ref ~ '^grant_[a-z0-9][a-z0-9_-]{7,65}$'
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND status IN ('active', 'suspended', 'revoked')
        AND (
            (subject_type = 'user' AND subject_user_id IS NOT NULL
                AND subject_workload_application_id IS NULL
                AND sponsor_user_id IS NULL
                AND environment = 'personal' AND NOT production_use)
            OR
            (subject_type = 'workload_application' AND subject_user_id IS NULL
                AND subject_workload_application_id IS NOT NULL
                AND project_role_snapshot = 'project_user'
                AND sponsor_user_id IS NOT NULL)
        )
        AND project_role_snapshot IN ('project_admin', 'project_user', 'project_viewer')
        AND scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(allowed_models) = 'array'
        AND jsonb_typeof(allowed_endpoints) = 'array'
        AND allowed_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/models","/v1/media/jobs"]'::jsonb
        AND (
            (scope_mode = 'project_default'
                AND jsonb_array_length(allowed_models) = 0
                AND jsonb_array_length(allowed_endpoints) = 0)
            OR
            (scope_mode = 'explicit'
                AND jsonb_array_length(allowed_models) > 0
                AND jsonb_array_length(allowed_endpoints) > 0)
        )
        AND (rpm_limit IS NULL OR rpm_limit BETWEEN 0 AND 2147483647)
        AND (rps_limit IS NULL OR rps_limit BETWEEN 0 AND 2147483647)
        AND (concurrency_limit IS NULL OR concurrency_limit BETWEEN 0 AND 2147483647)
        AND (valid_until IS NULL OR valid_until > valid_from)
        AND semantic_hash ~ '^[0-9a-f]{64}$'
        AND policy_version = 'unirouter.enterprise-access-policy.v1'
        AND (
          (revision_kind='scope_decision' AND access_request_id IS NOT NULL
            AND lifecycle_action IS NULL AND lifecycle_reason_code IS NULL)
          OR
          (revision_kind='lifecycle' AND access_request_id IS NULL
            AND lifecycle_action IN ('suspend','resume','revoke')
            AND lifecycle_reason_code IN (
              'explicit_suspend','explicit_resume','explicit_revoke','membership_removed',
              'membership_noncalling_downgrade','user_offboard','workload_archive'
            ))
        )
    );

CREATE OR REPLACE FUNCTION preserve_provider_attempt_dispatch_fence()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.dispatch_owner_hash IS DISTINCT FROM OLD.dispatch_owner_hash THEN
        RAISE EXCEPTION 'provider attempt dispatch owner is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.state = 'released_undispatched' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'released-undispatched provider attempt is terminal'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.health_outcome IS NULL AND NEW.health_outcome IS NOT NULL THEN
        IF NOT (
            (NEW.health_outcome = 'released_undispatched'
                AND OLD.state = 'dispatch_started'
                AND OLD.failure_kind IS NULL
                AND NEW.state = 'released_undispatched')
            OR (NEW.health_outcome = 'success'
                AND OLD.state = 'response_accepted'
                AND OLD.failure_kind IS NULL
                AND NEW.state = OLD.state)
            OR (NEW.health_outcome = 'provider_failure'
                AND (
                    (OLD.state = 'response_accepted' AND OLD.failure_kind IS NULL)
                    OR (OLD.state IN ('rejected','outcome_unknown')
                        AND OLD.failure_kind IS NOT DISTINCT FROM 'channel_failure')
                )
                AND NEW.state = OLD.state)
            OR (NEW.health_outcome = 'auth_failure'
                AND OLD.state = 'rejected'
                AND OLD.failure_kind IS NOT DISTINCT FROM 'auth'
                AND NEW.state = OLD.state)
            OR (NEW.health_outcome = 'rate_limited'
                AND OLD.state = 'rejected'
                AND OLD.failure_kind IS NOT DISTINCT FROM 'rate_limit'
                AND NEW.state = OLD.state)
        ) THEN
            RAISE EXCEPTION 'provider attempt health outcome is out of sequence'
                USING ERRCODE = '55000';
        END IF;
    END IF;
    IF OLD.health_outcome IS NOT NULL
       AND ROW(NEW.health_outcome,NEW.health_applied_at)
           IS DISTINCT FROM ROW(OLD.health_outcome,OLD.health_applied_at) THEN
        RAISE EXCEPTION 'provider attempt health outcome is terminal'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.health_outcome IS NULL
       AND NEW.health_outcome IS NULL
       AND NEW.health_applied_at IS DISTINCT FROM OLD.health_applied_at THEN
        RAISE EXCEPTION 'provider attempt health timestamp requires an outcome'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS runtime_change_releases_initially_unsealed
    ON runtime_change_releases;
DROP FUNCTION IF EXISTS require_runtime_release_initially_unsealed();

CREATE OR REPLACE FUNCTION enforce_execution_projection_attempt_match()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (
        OLD.route_evidence_source IS NOT NULL OR NEW.route_evidence_source IS NOT NULL
    ) AND ROW(
        NEW.org_id,NEW.request_id,NEW.attempt_id,NEW.source_event_hash,
        NEW.route_evidence_source,NEW.route_evidence_version,NEW.route_authority_mode,
        NEW.route_evidence_hash,NEW.route_public_model_id,NEW.route_deployment_id,
        NEW.route_connection_id,NEW.route_credential_binding_id,NEW.route_model_operation_id,
        NEW.route_runtime_release_id,NEW.route_snapshot_hash,NEW.route_channel_id
    ) IS DISTINCT FROM ROW(
        OLD.org_id,OLD.request_id,OLD.attempt_id,OLD.source_event_hash,
        OLD.route_evidence_source,OLD.route_evidence_version,OLD.route_authority_mode,
        OLD.route_evidence_hash,OLD.route_public_model_id,OLD.route_deployment_id,
        OLD.route_connection_id,OLD.route_credential_binding_id,OLD.route_model_operation_id,
        OLD.route_runtime_release_id,OLD.route_snapshot_hash,OLD.route_channel_id
    ) THEN
        RAISE EXCEPTION 'execution evidence projection is immutable'
            USING ERRCODE='55000';
    END IF;

    IF NEW.route_evidence_source='provider_attempt' THEN
        PERFORM 1
          FROM billing_provider_attempts attempt
         WHERE attempt.org_id=NEW.org_id
           AND attempt.request_id=NEW.request_id
           AND attempt.attempt_id=NEW.attempt_id
           AND ROW(
               NEW.route_evidence_version,NEW.route_authority_mode,NEW.route_evidence_hash,
               NEW.route_public_model_id,NEW.route_deployment_id,NEW.route_connection_id,
               NEW.route_credential_binding_id,NEW.route_model_operation_id,
               NEW.route_runtime_release_id,NEW.route_snapshot_hash,NEW.route_channel_id
           ) IS NOT DISTINCT FROM ROW(
               attempt.route_evidence_version,attempt.route_authority_mode,attempt.route_evidence_hash,
               attempt.route_public_model_id,attempt.deployment_id,attempt.route_connection_id,
               attempt.route_credential_binding_id,attempt.route_model_operation_id,
               attempt.route_runtime_release_id,attempt.route_snapshot_hash,attempt.channel_id
           );
        IF NOT FOUND THEN
            RAISE EXCEPTION 'execution projection does not match exact provider attempt'
                USING ERRCODE='23503';
        END IF;
    ELSIF NEW.route_evidence_source='precontract_provider_attempt' THEN
        PERFORM 1
          FROM billing_provider_attempts attempt
         WHERE attempt.org_id=NEW.org_id
           AND attempt.request_id=NEW.request_id
           AND attempt.attempt_id=NEW.attempt_id
           AND attempt.route_evidence_version IS NULL
           AND attempt.channel_id IS NOT DISTINCT FROM NEW.route_channel_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'pre-contract projection does not match exact provider attempt'
                USING ERRCODE='23503';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE call_logs
    DROP CONSTRAINT call_logs_execution_projection_check,
    DROP COLUMN route_idempotency_contract_version,
    DROP COLUMN route_charge_contract_version,
    DROP COLUMN route_delivery_contract_version,
    DROP COLUMN route_public_execution_binding_id,
    ADD CONSTRAINT call_logs_execution_projection_check CHECK (
        (
            route_evidence_source IS NULL
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
            AND route_model_operation_id IS NULL AND route_runtime_release_id IS NULL
            AND route_snapshot_hash IS NULL
        )
        OR (
            route_evidence_source IN ('response_cache','no_provider_attempt')
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_connection_id IS NULL AND route_credential_binding_id IS NULL
            AND route_model_operation_id IS NULL AND route_runtime_release_id IS NULL
            AND route_snapshot_hash IS NULL
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
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version = 'unirouter.execution-route.v1'
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
        )
    );

ALTER TABLE request_trace_events
    DROP CONSTRAINT request_trace_events_execution_projection_check,
    DROP COLUMN route_idempotency_contract_version,
    DROP COLUMN route_charge_contract_version,
    DROP COLUMN route_delivery_contract_version,
    DROP COLUMN route_public_execution_binding_id,
    ADD CONSTRAINT request_trace_events_execution_projection_check CHECK (
        (
            route_evidence_source IS NULL
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND route_channel_id IS NULL
        )
        OR (
            route_evidence_source IN ('response_cache','no_provider_attempt')
            AND route_evidence_version IS NULL AND route_authority_mode IS NULL
            AND route_evidence_hash IS NULL AND route_public_model_id IS NULL
            AND route_deployment_id IS NULL AND route_connection_id IS NULL
            AND route_credential_binding_id IS NULL AND route_model_operation_id IS NULL
            AND route_runtime_release_id IS NULL AND route_snapshot_hash IS NULL
            AND route_channel_id IS NULL
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
            AND source_event_hash ~ '^[a-f0-9]{64}$'
        )
        OR (
            route_evidence_source = 'provider_attempt'
            AND COALESCE(BTRIM(attempt_id),'') <> ''
            AND route_evidence_version = 'unirouter.execution-route.v1'
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
        )
    );

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_public_binding_fk,
    DROP CONSTRAINT billing_provider_attempts_execution_evidence_check,
    DROP CONSTRAINT billing_provider_attempts_identity_check,
    DROP COLUMN route_idempotency_contract_version,
    DROP COLUMN route_charge_contract_version,
    DROP COLUMN route_delivery_contract_version,
    DROP COLUMN route_public_execution_binding_id,
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
    );

CREATE OR REPLACE FUNCTION preserve_provider_attempt_execution_identity()
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

COMMENT ON COLUMN billing_provider_attempts.route_evidence_hash IS
    'SHA-256 of the normalized executionevidence.EvidenceV1 payload used for idempotent replay matching.';

CREATE OR REPLACE FUNCTION enforce_runtime_release_p3_qualification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.route_items_sealed_at IS NULL AND NEW.route_items_sealed_at IS NOT NULL
       AND EXISTS (
           SELECT 1
             FROM runtime_route_revision_items item
             LEFT JOIN model_operation_execution_qualifications qualification
               ON qualification.deployment_id=item.deployment_id
              AND qualification.operation_id=item.operation_id
            WHERE item.release_id=NEW.id
              AND item.operation_id IN ('openai.responses.v1','anthropic.messages.v1')
              AND (
                  qualification.qualified IS DISTINCT FROM TRUE
                  OR item.public_path IS DISTINCT FROM qualification.public_path
                  OR item.executor_contract_version IS DISTINCT FROM qualification.executor_contract_version
                  OR item.expected_connection_revision IS DISTINCT FROM qualification.current_connection_revision
                  OR item.expected_deployment_revision IS DISTINCT FROM qualification.current_deployment_revision
                  OR item.expected_operation_revision IS DISTINCT FROM qualification.current_operation_revision
              )
       ) THEN
        RAISE EXCEPTION 'Responses/Messages runtime manifest lacks current production qualification'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP VIEW model_operation_public_execution_qualifications;

CREATE OR REPLACE FUNCTION enforce_runtime_route_item_production_qualification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    support RECORD;
    source_sealed_at TIMESTAMPTZ(6);
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN
        SELECT route_items_sealed_at INTO source_sealed_at
          FROM runtime_change_releases WHERE id=OLD.release_id FOR SHARE;
        IF source_sealed_at IS NOT NULL THEN
            RAISE EXCEPTION 'sealed runtime route qualification evidence is immutable'
                USING ERRCODE='55000';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.operation_id IN ('openai.responses.v1','anthropic.messages.v1') THEN
        SELECT * INTO support
          FROM model_operation_current_execution_support_bindings current_support
         WHERE current_support.operation_id=NEW.operation_id
           AND current_support.protocol_profile_id=NEW.protocol_profile_id
           AND current_support.protocol_profile_version=NEW.protocol_profile_version;
        IF NOT FOUND OR NEW.public_path IS DISTINCT FROM support.public_path
           OR NEW.executor_contract_version IS DISTINCT FROM support.executor_contract_version THEN
            RAISE EXCEPTION 'Responses/Messages runtime item requires the current exact execution support binding'
                USING ERRCODE='23514';
        END IF;
    ELSIF NEW.public_path IS NOT NULL OR NEW.executor_contract_version IS NOT NULL THEN
        RAISE EXCEPTION 'non-qualified runtime item cannot claim a production execution binding'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE runtime_route_revision_items
    DROP CONSTRAINT runtime_route_revision_items_public_binding_fk,
    DROP CONSTRAINT runtime_route_revision_items_public_contract_shape_check,
    DROP COLUMN idempotency_contract_version,
    DROP COLUMN charge_contract_version,
    DROP COLUMN delivery_contract_version,
    DROP COLUMN public_execution_binding_id;

DROP TRIGGER IF EXISTS model_operation_public_execution_bindings_item_membership
    ON model_operation_public_execution_bindings;
DROP FUNCTION IF EXISTS require_public_execution_binding_item_membership();
DROP TRIGGER IF EXISTS model_operation_public_execution_bindings_append_only
    ON model_operation_public_execution_bindings;
DROP FUNCTION IF EXISTS reject_model_operation_public_execution_binding_mutation();
DROP TRIGGER IF EXISTS model_operation_public_execution_bindings_validate
    ON model_operation_public_execution_bindings;
DROP FUNCTION IF EXISTS validate_model_operation_public_execution_binding();
DROP TABLE model_operation_public_execution_bindings;

DROP VIEW model_operation_current_public_execution_contracts;
DROP TRIGGER IF EXISTS model_operation_public_execution_contract_extensions_immutable
    ON model_operation_public_execution_contract_extensions;
DROP FUNCTION IF EXISTS reject_model_operation_public_execution_contract_mutation();
DROP TABLE model_operation_public_execution_contract_extensions;

DELETE FROM schema_migrations
WHERE version='000077_public_model_execution_authority';

COMMIT;
