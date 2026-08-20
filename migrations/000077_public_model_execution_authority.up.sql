BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE orgs, users, model_registry, provider_connections,
    provider_credential_bindings, credential_binding_deployments,
    model_deployments, model_deployment_operations, model_deployment_probes,
    model_operation_execution_support_bindings,
    model_operation_production_approvals, runtime_change_releases,
    runtime_route_revision_items, billing_provider_attempts,
    call_logs, request_trace_events, access_requests, access_grants,
    access_grant_revisions, access_credentials, api_keys,
    model_call_authorizations, gateway_endpoints, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- M55 Gateway Endpoints are operator-owned desired state, not M77 execution
-- authority. Keep the historical Responses identifier stable while allowing
-- the same strict non-stream Messages surface exposed by this release.
ALTER TABLE gateway_endpoints
    DROP CONSTRAINT gateway_endpoints_profile_check,
    ADD CONSTRAINT gateway_endpoints_profile_check CHECK (
        endpoint_profile IN (
            'endpoint.openai-chat.v1',
            'endpoint.openai-responses.contract.v1',
            'endpoint.anthropic-messages.v1'
        )
    );

-- Responses and Messages are grant-only operations in the shared operation
-- catalog. Extend the three authoritative M59 scope documents atomically with
-- M77; derived credential/key endpoint snapshots remain exact copies of an
-- immutable grant and gain no independent authority here.
ALTER TABLE access_requests
    DROP CONSTRAINT access_requests_scope_check,
    ADD CONSTRAINT access_requests_scope_check CHECK (
        requested_project_role IN ('project_admin', 'project_user', 'project_viewer')
        AND requested_scope_mode IN ('project_default', 'explicit')
        AND jsonb_typeof(requested_models) = 'array'
        AND jsonb_typeof(requested_endpoints) = 'array'
        AND requested_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/responses","/v1/messages","/v1/models","/v1/media/jobs"]'::jsonb
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
        AND allowed_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/responses","/v1/messages","/v1/models","/v1/media/jobs"]'::jsonb
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
        AND allowed_endpoints <@ '["/v1/chat/completions","/v1/embeddings","/v1/responses","/v1/messages","/v1/models","/v1/media/jobs"]'::jsonb
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

-- M73 predates the explicit response_unusable attempt outcome. A public 2xx
-- response that fails the strict provider contract is financially ambiguous
-- but is still an exact provider-health failure when its durable billing kind
-- is channel_failure. Preserve the attempt-first lock/commit order and extend
-- only that one terminal fence state.
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
                    OR (OLD.state IN ('rejected','outcome_unknown','response_unusable')
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

-- M70 validates the seal transition on UPDATE. An INSERT with a non-NULL
-- route_items_sealed_at would otherwise manufacture a sealed release before
-- any normalized item (and, in M77, its public binding) exists.
CREATE FUNCTION require_runtime_release_initially_unsealed()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.route_items_sealed_at IS NOT NULL THEN
        RAISE EXCEPTION 'runtime route manifest must be inserted unsealed'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_change_releases_initially_unsealed
BEFORE INSERT ON runtime_change_releases
FOR EACH ROW EXECUTE FUNCTION require_runtime_release_initially_unsealed();

-- M75's five-column support tuple remains the sole provider-executor truth.
-- M77 only extends that exact tuple with the three independently versioned
-- customer delivery, charging and idempotency contracts.
CREATE TABLE model_operation_public_execution_contract_extensions (
    operation_id VARCHAR(80) NOT NULL,
    public_path VARCHAR(255) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    executor_contract_version VARCHAR(100) NOT NULL,
    delivery_contract_version VARCHAR(100) NOT NULL,
    charge_contract_version VARCHAR(100) NOT NULL,
    idempotency_contract_version VARCHAR(100) NOT NULL,
    introduced_migration VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT model_operation_public_execution_contract_extensions_pk
        PRIMARY KEY (
            operation_id,public_path,protocol_profile_id,
            protocol_profile_version,executor_contract_version,
            delivery_contract_version,charge_contract_version,
            idempotency_contract_version
        ),
    CONSTRAINT model_operation_public_execution_contract_extensions_base_unique
        UNIQUE (
            operation_id,public_path,protocol_profile_id,
            protocol_profile_version,executor_contract_version
        ),
    CONSTRAINT model_operation_public_execution_contract_extensions_base_fk
        FOREIGN KEY (
            operation_id,public_path,protocol_profile_id,
            protocol_profile_version,executor_contract_version
        ) REFERENCES model_operation_execution_support_bindings (
            operation_id,public_path,protocol_profile_id,
            protocol_profile_version,executor_contract_version
        ) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_contract_extensions_identity_check CHECK (
        operation_id IN ('openai.responses.v1','anthropic.messages.v1')
        AND delivery_contract_version='unirouter.public-nonstream-delivery.v1'
        AND charge_contract_version='unirouter.provider-final-token-charge.v1'
        AND idempotency_contract_version='unirouter.public-at-most-once.v1'
        AND introduced_migration='000077_public_model_execution_authority'
    )
);

INSERT INTO model_operation_public_execution_contract_extensions (
    operation_id,public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,delivery_contract_version,
    charge_contract_version,idempotency_contract_version,introduced_migration)
VALUES
    ('openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v1','1.0.0',
     'unirouter.endpoint-profile-nonstream-executor.v1',
     'unirouter.public-nonstream-delivery.v1',
     'unirouter.provider-final-token-charge.v1',
     'unirouter.public-at-most-once.v1','000077_public_model_execution_authority'),
    ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v1','1.0.0',
     'unirouter.endpoint-profile-nonstream-executor.v1',
     'unirouter.public-nonstream-delivery.v1',
     'unirouter.provider-final-token-charge.v1',
     'unirouter.public-at-most-once.v1','000077_public_model_execution_authority');

CREATE FUNCTION reject_model_operation_public_execution_contract_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model operation public execution contracts are migration-owned and immutable'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_operation_public_execution_contract_extensions_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_public_execution_contract_extensions
FOR EACH STATEMENT
EXECUTE FUNCTION reject_model_operation_public_execution_contract_mutation();

CREATE VIEW model_operation_current_public_execution_contracts AS
SELECT support.operation_id,support.public_path,support.protocol_profile_id,
       support.protocol_profile_version,support.executor_contract_version,
       extension.delivery_contract_version,extension.charge_contract_version,
       extension.idempotency_contract_version
  FROM model_operation_current_execution_support_bindings support
  JOIN model_operation_public_execution_contract_extensions extension
    ON extension.operation_id=support.operation_id
   AND extension.public_path=support.public_path
   AND extension.protocol_profile_id=support.protocol_profile_id
   AND extension.protocol_profile_version=support.protocol_profile_version
   AND extension.executor_contract_version=support.executor_contract_version;

-- A binding is one immutable release-item structural authorization. It does
-- not select or freeze a provider credential: rotation, revocation, link state
-- and each credential's own latest probe remain per-attempt Planner and
-- Coordinator authority.
CREATE TABLE model_operation_public_execution_bindings (
    public_execution_binding_id BIGSERIAL PRIMARY KEY,
    customer_org_id BIGINT NOT NULL,
    public_model_id BIGINT NOT NULL,
    release_id BIGINT NOT NULL REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    ordinal INTEGER NOT NULL,
    deployment_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    production_approval_id BIGINT NOT NULL
        REFERENCES model_operation_production_approvals(approval_id) ON DELETE RESTRICT,
    https_origin VARCHAR(512) NOT NULL,
    approved_connection_revision BIGINT NOT NULL,
    approved_deployment_revision BIGINT NOT NULL,
    approved_operation_revision BIGINT NOT NULL,
    public_path VARCHAR(255) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    executor_contract_version VARCHAR(100) NOT NULL,
    delivery_contract_version VARCHAR(100) NOT NULL,
    charge_contract_version VARCHAR(100) NOT NULL,
    idempotency_contract_version VARCHAR(100) NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_by_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    source_kind VARCHAR(80) NOT NULL,
    source_ref VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT model_operation_public_execution_bindings_release_item_unique
        UNIQUE (release_id,ordinal),
    CONSTRAINT model_operation_public_execution_bindings_item_identity_unique
        UNIQUE (public_execution_binding_id,release_id,ordinal,deployment_id,operation_id),
	CONSTRAINT model_operation_public_execution_bindings_attempt_identity_unique
		UNIQUE (
			public_execution_binding_id,customer_org_id,public_model_id,
			release_id,deployment_id,connection_id,operation_id,
			approved_connection_revision,approved_deployment_revision,
			approved_operation_revision,protocol_profile_id,
			protocol_profile_version,delivery_contract_version,
			charge_contract_version,idempotency_contract_version
		),
    CONSTRAINT model_operation_public_execution_bindings_model_tenant_fk
        FOREIGN KEY (public_model_id,customer_org_id)
        REFERENCES model_registry(id,org_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_bindings_customer_deployment_fk
        FOREIGN KEY (customer_org_id,deployment_id)
        REFERENCES model_deployments(customer_org_id,id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_bindings_deployment_connection_fk
        FOREIGN KEY (deployment_id,connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_bindings_operation_fk
        FOREIGN KEY (deployment_id,operation_id)
        REFERENCES model_deployment_operations(deployment_id,operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_bindings_creator_tenant_fk
        FOREIGN KEY (created_by,created_by_org_id)
        REFERENCES users(id,org_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_bindings_contract_fk
        FOREIGN KEY (
            operation_id,public_path,protocol_profile_id,
            protocol_profile_version,executor_contract_version,
            delivery_contract_version,charge_contract_version,
            idempotency_contract_version
        ) REFERENCES model_operation_public_execution_contract_extensions (
            operation_id,public_path,protocol_profile_id,
            protocol_profile_version,executor_contract_version,
            delivery_contract_version,charge_contract_version,
            idempotency_contract_version
        ) ON DELETE RESTRICT,
    CONSTRAINT model_operation_public_execution_bindings_identity_check CHECK (
        customer_org_id>0 AND public_model_id>0 AND release_id>0
        AND ordinal>0 AND deployment_id>0 AND connection_id>0
        AND production_approval_id>0
        AND approved_connection_revision>0
        AND approved_deployment_revision>0
        AND approved_operation_revision>0
        AND model_operation_https_origin(https_origin)=https_origin
        AND created_by>0 AND created_by_org_id=customer_org_id
        AND source_kind='runtime.public_execution_binding.v1'
        AND source_ref='release:' || release_id::TEXT || '/item:' || ordinal::TEXT
    )
);

CREATE FUNCTION validate_model_operation_public_execution_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof RECORD;
BEGIN
    SELECT release_row.candidate_version,release_row.stage,
           release_row.route_items_sealed_at,release_row.public_model_id,
           release_row.created_by,
           deployment.customer_org_id,deployment.public_model_id AS deployment_public_model_id,
           deployment.connection_id,
           connection.config_revision AS current_connection_revision,
           deployment.config_revision AS current_deployment_revision,
           operation.revision AS current_operation_revision,
           model_operation_https_origin(connection.base_url) AS current_https_origin,
           qualification.qualified,qualification.approval_id,
           qualification.https_origin AS qualification_https_origin,
           qualification.approved_connection_revision,
           qualification.approved_deployment_revision,
           qualification.approved_operation_revision,
           qualification.public_path,qualification.protocol_profile_id,
           qualification.protocol_profile_version,
           qualification.executor_contract_version,
           contract.delivery_contract_version,contract.charge_contract_version,
           contract.idempotency_contract_version,
           actor.org_id AS actor_org_id,actor.status AS actor_status
      INTO proof
      FROM runtime_change_releases release_row
      JOIN model_deployments deployment ON deployment.id=NEW.deployment_id
      JOIN provider_connections connection ON connection.id=deployment.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=deployment.id AND operation.operation_id=NEW.operation_id
      JOIN model_operation_execution_qualifications qualification
        ON qualification.deployment_id=operation.deployment_id
       AND qualification.operation_id=operation.operation_id
      JOIN model_operation_current_public_execution_contracts contract
        ON contract.operation_id=qualification.operation_id
       AND contract.public_path=qualification.public_path
       AND contract.protocol_profile_id=qualification.protocol_profile_id
       AND contract.protocol_profile_version=qualification.protocol_profile_version
       AND contract.executor_contract_version=qualification.executor_contract_version
      JOIN users actor ON actor.id=NEW.created_by
     WHERE release_row.id=NEW.release_id
     FOR SHARE OF release_row,deployment,connection,operation,actor;

    IF NOT FOUND
       OR proof.candidate_version<>'unirouter.runtime-change-candidate.v2'
       OR proof.stage<>'draft' OR proof.route_items_sealed_at IS NOT NULL
       OR proof.public_model_id IS DISTINCT FROM NEW.public_model_id
       OR proof.created_by IS DISTINCT FROM NEW.created_by
       OR proof.customer_org_id IS DISTINCT FROM NEW.customer_org_id
       OR proof.deployment_public_model_id IS DISTINCT FROM NEW.public_model_id
       OR proof.connection_id IS DISTINCT FROM NEW.connection_id
       OR proof.actor_org_id IS DISTINCT FROM NEW.created_by_org_id
       OR proof.actor_status<>'active'
       OR proof.qualified IS DISTINCT FROM TRUE
       OR proof.approval_id IS DISTINCT FROM NEW.production_approval_id
       OR proof.qualification_https_origin IS DISTINCT FROM NEW.https_origin
       OR proof.current_https_origin IS DISTINCT FROM NEW.https_origin
       OR proof.current_connection_revision IS DISTINCT FROM NEW.approved_connection_revision
       OR proof.current_deployment_revision IS DISTINCT FROM NEW.approved_deployment_revision
       OR proof.current_operation_revision IS DISTINCT FROM NEW.approved_operation_revision
       OR proof.approved_connection_revision IS DISTINCT FROM NEW.approved_connection_revision
       OR proof.approved_deployment_revision IS DISTINCT FROM NEW.approved_deployment_revision
       OR proof.approved_operation_revision IS DISTINCT FROM NEW.approved_operation_revision
       OR proof.public_path IS DISTINCT FROM NEW.public_path
       OR proof.protocol_profile_id IS DISTINCT FROM NEW.protocol_profile_id
       OR proof.protocol_profile_version IS DISTINCT FROM NEW.protocol_profile_version
       OR proof.executor_contract_version IS DISTINCT FROM NEW.executor_contract_version
       OR proof.delivery_contract_version IS DISTINCT FROM NEW.delivery_contract_version
       OR proof.charge_contract_version IS DISTINCT FROM NEW.charge_contract_version
       OR proof.idempotency_contract_version IS DISTINCT FROM NEW.idempotency_contract_version THEN
        RAISE EXCEPTION 'public execution binding proof is stale, incomplete, or mismatched'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_operation_public_execution_bindings_validate
BEFORE INSERT ON model_operation_public_execution_bindings
FOR EACH ROW EXECUTE FUNCTION validate_model_operation_public_execution_binding();

CREATE FUNCTION reject_model_operation_public_execution_binding_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model operation public execution bindings are append-only'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_operation_public_execution_bindings_append_only
BEFORE UPDATE OR DELETE OR TRUNCATE ON model_operation_public_execution_bindings
FOR EACH STATEMENT
EXECUTE FUNCTION reject_model_operation_public_execution_binding_mutation();

-- EvidenceV2 is a distinct immutable receipt, never an expansion of the V1
-- digest. The common route columns retain their V1 meaning and the four public
-- authority columns are an all-or-none V2-only bundle.
ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_execution_evidence_check,
    DROP CONSTRAINT billing_provider_attempts_identity_check,
    ADD COLUMN route_public_execution_binding_id BIGINT,
    ADD COLUMN route_delivery_contract_version VARCHAR(100),
    ADD COLUMN route_charge_contract_version VARCHAR(100),
    ADD COLUMN route_idempotency_contract_version VARCHAR(100),
    ADD CONSTRAINT billing_provider_attempts_identity_check CHECK (
        length(btrim(attempt_id)) > 0
        AND length(btrim(endpoint)) > 0
        AND length(btrim(provider)) > 0
        AND length(btrim(channel_name)) > 0
        AND api_key_id > 0
        AND (
            (route_evidence_version IS NULL
             AND COALESCE(channel_id,0) > 0 AND COALESCE(channel_key_id,0) > 0)
            OR COALESCE(route_evidence_version,'') IN (
                'unirouter.execution-route.v1','unirouter.execution-route.v2'
            )
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
                AND route_delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND route_charge_contract_version='unirouter.provider-final-token-charge.v1'
                AND route_idempotency_contract_version='unirouter.public-at-most-once.v1'
            )
        )
    ),
    ADD CONSTRAINT billing_provider_attempts_public_binding_fk
        FOREIGN KEY (
            route_public_execution_binding_id,org_id,route_public_model_id,
            route_runtime_release_id,deployment_id,route_connection_id,
            route_model_operation_id,route_connection_revision,
            route_deployment_revision,route_model_operation_revision,
            route_protocol_profile_id,route_protocol_profile_version,
            route_delivery_contract_version,route_charge_contract_version,
            route_idempotency_contract_version
        ) REFERENCES model_operation_public_execution_bindings (
            public_execution_binding_id,customer_org_id,public_model_id,
            release_id,deployment_id,connection_id,operation_id,
            approved_connection_revision,approved_deployment_revision,
            approved_operation_revision,protocol_profile_id,
            protocol_profile_version,delivery_contract_version,
            charge_contract_version,idempotency_contract_version
        ) ON DELETE RESTRICT;

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
        NEW.route_legacy_credential_state_revision,NEW.route_requires_probe,NEW.route_probe_lease_token_hash,
        NEW.route_public_execution_binding_id,NEW.route_delivery_contract_version,
        NEW.route_charge_contract_version,NEW.route_idempotency_contract_version
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
        OLD.route_legacy_credential_state_revision,OLD.route_requires_probe,OLD.route_probe_lease_token_hash,
        OLD.route_public_execution_binding_id,OLD.route_delivery_contract_version,
        OLD.route_charge_contract_version,OLD.route_idempotency_contract_version
    ) THEN
        RAISE EXCEPTION 'provider attempt execution identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE call_logs
    DROP CONSTRAINT call_logs_execution_projection_check,
    ADD COLUMN route_public_execution_binding_id BIGINT,
    ADD COLUMN route_delivery_contract_version VARCHAR(100),
    ADD COLUMN route_charge_contract_version VARCHAR(100),
    ADD COLUMN route_idempotency_contract_version VARCHAR(100),
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
                AND route_delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND route_charge_contract_version='unirouter.provider-final-token-charge.v1'
                AND route_idempotency_contract_version='unirouter.public-at-most-once.v1'
            )
        )
    );

ALTER TABLE request_trace_events
    DROP CONSTRAINT request_trace_events_execution_projection_check,
    ADD COLUMN route_public_execution_binding_id BIGINT,
    ADD COLUMN route_delivery_contract_version VARCHAR(100),
    ADD COLUMN route_charge_contract_version VARCHAR(100),
    ADD COLUMN route_idempotency_contract_version VARCHAR(100),
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
                AND route_delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND route_charge_contract_version='unirouter.provider-final-token-charge.v1'
                AND route_idempotency_contract_version='unirouter.public-at-most-once.v1'
            )
        )
    );

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
        NEW.route_runtime_release_id,NEW.route_snapshot_hash,NEW.route_channel_id,
        NEW.route_public_execution_binding_id,NEW.route_delivery_contract_version,
        NEW.route_charge_contract_version,NEW.route_idempotency_contract_version
    ) IS DISTINCT FROM ROW(
        OLD.org_id,OLD.request_id,OLD.attempt_id,OLD.source_event_hash,
        OLD.route_evidence_source,OLD.route_evidence_version,OLD.route_authority_mode,
        OLD.route_evidence_hash,OLD.route_public_model_id,OLD.route_deployment_id,
        OLD.route_connection_id,OLD.route_credential_binding_id,OLD.route_model_operation_id,
        OLD.route_runtime_release_id,OLD.route_snapshot_hash,OLD.route_channel_id,
        OLD.route_public_execution_binding_id,OLD.route_delivery_contract_version,
        OLD.route_charge_contract_version,OLD.route_idempotency_contract_version
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
               NEW.route_runtime_release_id,NEW.route_snapshot_hash,NEW.route_channel_id,
               NEW.route_public_execution_binding_id,NEW.route_delivery_contract_version,
               NEW.route_charge_contract_version,NEW.route_idempotency_contract_version
           ) IS NOT DISTINCT FROM ROW(
               attempt.route_evidence_version,attempt.route_authority_mode,attempt.route_evidence_hash,
               attempt.route_public_model_id,attempt.deployment_id,attempt.route_connection_id,
               attempt.route_credential_binding_id,attempt.route_model_operation_id,
               attempt.route_runtime_release_id,attempt.route_snapshot_hash,attempt.channel_id,
               attempt.route_public_execution_binding_id,attempt.route_delivery_contract_version,
               attempt.route_charge_contract_version,attempt.route_idempotency_contract_version
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

ALTER TABLE runtime_route_revision_items
    ADD COLUMN public_execution_binding_id BIGINT,
    ADD COLUMN delivery_contract_version VARCHAR(100),
    ADD COLUMN charge_contract_version VARCHAR(100),
    ADD COLUMN idempotency_contract_version VARCHAR(100),
    ADD CONSTRAINT runtime_route_revision_items_public_contract_shape_check CHECK (
        (public_execution_binding_id IS NULL
         AND delivery_contract_version IS NULL
         AND charge_contract_version IS NULL
         AND idempotency_contract_version IS NULL)
        OR
        (public_execution_binding_id IS NOT NULL
         AND public_execution_binding_id>0
         AND delivery_contract_version IS NOT NULL
         AND charge_contract_version IS NOT NULL
         AND idempotency_contract_version IS NOT NULL
         AND delivery_contract_version=BTRIM(delivery_contract_version)
         AND charge_contract_version=BTRIM(charge_contract_version)
         AND idempotency_contract_version=BTRIM(idempotency_contract_version))
    ),
    ADD CONSTRAINT runtime_route_revision_items_public_binding_fk
        FOREIGN KEY (
            public_execution_binding_id,release_id,ordinal,deployment_id,operation_id
        ) REFERENCES model_operation_public_execution_bindings (
            public_execution_binding_id,release_id,ordinal,deployment_id,operation_id
        ) ON DELETE RESTRICT;

CREATE FUNCTION require_public_execution_binding_item_membership()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM runtime_route_revision_items item
         WHERE item.public_execution_binding_id=NEW.public_execution_binding_id
           AND item.release_id=NEW.release_id AND item.ordinal=NEW.ordinal
           AND item.deployment_id=NEW.deployment_id
           AND item.operation_id=NEW.operation_id
           AND item.expected_connection_revision=NEW.approved_connection_revision
           AND item.expected_deployment_revision=NEW.approved_deployment_revision
           AND item.expected_operation_revision=NEW.approved_operation_revision
           AND item.public_path=NEW.public_path
           AND item.protocol_profile_id=NEW.protocol_profile_id
           AND item.protocol_profile_version=NEW.protocol_profile_version
           AND item.executor_contract_version=NEW.executor_contract_version
           AND item.delivery_contract_version=NEW.delivery_contract_version
           AND item.charge_contract_version=NEW.charge_contract_version
           AND item.idempotency_contract_version=NEW.idempotency_contract_version
    ) THEN
        RAISE EXCEPTION 'public execution binding requires its exact release item'
            USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER model_operation_public_execution_bindings_item_membership
AFTER INSERT ON model_operation_public_execution_bindings
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION require_public_execution_binding_item_membership();

CREATE OR REPLACE FUNCTION enforce_runtime_route_item_production_qualification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    support RECORD;
    public_binding RECORD;
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
          FROM model_operation_current_public_execution_contracts current_support
         WHERE current_support.operation_id=NEW.operation_id
           AND current_support.public_path=NEW.public_path
           AND current_support.protocol_profile_id=NEW.protocol_profile_id
           AND current_support.protocol_profile_version=NEW.protocol_profile_version
           AND current_support.executor_contract_version=NEW.executor_contract_version
           AND current_support.delivery_contract_version=NEW.delivery_contract_version
           AND current_support.charge_contract_version=NEW.charge_contract_version
           AND current_support.idempotency_contract_version=NEW.idempotency_contract_version;
        SELECT * INTO public_binding
          FROM model_operation_public_execution_bindings binding
         WHERE binding.public_execution_binding_id=NEW.public_execution_binding_id
           AND binding.release_id=NEW.release_id AND binding.ordinal=NEW.ordinal
           AND binding.deployment_id=NEW.deployment_id
           AND binding.operation_id=NEW.operation_id;
        IF support.operation_id IS NULL OR public_binding.public_execution_binding_id IS NULL
           OR NEW.source_candidate_version<>'unirouter.runtime-change-candidate.v2'
           OR NEW.expected_connection_revision IS DISTINCT FROM public_binding.approved_connection_revision
           OR NEW.expected_deployment_revision IS DISTINCT FROM public_binding.approved_deployment_revision
           OR NEW.expected_operation_revision IS DISTINCT FROM public_binding.approved_operation_revision
           OR NEW.public_path IS DISTINCT FROM public_binding.public_path
           OR NEW.protocol_profile_id IS DISTINCT FROM public_binding.protocol_profile_id
           OR NEW.protocol_profile_version IS DISTINCT FROM public_binding.protocol_profile_version
           OR NEW.executor_contract_version IS DISTINCT FROM public_binding.executor_contract_version
           OR NEW.delivery_contract_version IS DISTINCT FROM public_binding.delivery_contract_version
           OR NEW.charge_contract_version IS DISTINCT FROM public_binding.charge_contract_version
           OR NEW.idempotency_contract_version IS DISTINCT FROM public_binding.idempotency_contract_version THEN
            RAISE EXCEPTION 'Responses/Messages runtime item requires the exact M77 public execution binding'
                USING ERRCODE='23514';
        END IF;
    ELSIF NEW.public_path IS NOT NULL OR NEW.executor_contract_version IS NOT NULL
       OR NEW.public_execution_binding_id IS NOT NULL
       OR NEW.delivery_contract_version IS NOT NULL
       OR NEW.charge_contract_version IS NOT NULL
       OR NEW.idempotency_contract_version IS NOT NULL THEN
        RAISE EXCEPTION 'non-public runtime item cannot claim a public execution binding'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE VIEW model_operation_public_execution_qualifications AS
SELECT binding.public_execution_binding_id,binding.customer_org_id,
       binding.public_model_id,binding.release_id,binding.ordinal,
       binding.deployment_id,binding.connection_id,binding.operation_id,
       binding.production_approval_id,binding.https_origin,
       binding.approved_connection_revision,
       binding.approved_deployment_revision,
       binding.approved_operation_revision,binding.public_path,
       binding.protocol_profile_id,binding.protocol_profile_version,
       binding.executor_contract_version,binding.delivery_contract_version,
       binding.charge_contract_version,binding.idempotency_contract_version,
       CASE
         WHEN release_row.public_model_id IS DISTINCT FROM binding.public_model_id
           THEN 'release_model_stale'
         WHEN item.public_execution_binding_id IS NULL THEN 'release_item_missing'
         WHEN qualification.approval_id IS DISTINCT FROM binding.production_approval_id
           OR qualification.qualified IS DISTINCT FROM TRUE
           THEN 'structural_qualification_stale'
         WHEN qualification.current_connection_revision IS DISTINCT FROM binding.approved_connection_revision
           THEN 'connection_revision_stale'
         WHEN qualification.current_deployment_revision IS DISTINCT FROM binding.approved_deployment_revision
           THEN 'deployment_revision_stale'
         WHEN qualification.current_operation_revision IS DISTINCT FROM binding.approved_operation_revision
           THEN 'operation_revision_stale'
         WHEN qualification.https_origin IS DISTINCT FROM binding.https_origin
           THEN 'https_origin_stale'
         WHEN contract.operation_id IS NULL THEN 'public_contract_stale'
         ELSE 'public_executable'
       END AS qualification_state,
       (
         release_row.public_model_id=binding.public_model_id
         AND release_row.candidate_version='unirouter.runtime-change-candidate.v2'
         AND item.public_execution_binding_id=binding.public_execution_binding_id
         AND item.release_id=binding.release_id AND item.ordinal=binding.ordinal
         AND item.deployment_id=binding.deployment_id
         AND item.operation_id=binding.operation_id
         AND item.expected_connection_revision=binding.approved_connection_revision
         AND item.expected_deployment_revision=binding.approved_deployment_revision
         AND item.expected_operation_revision=binding.approved_operation_revision
         AND item.public_path=binding.public_path
         AND item.protocol_profile_id=binding.protocol_profile_id
         AND item.protocol_profile_version=binding.protocol_profile_version
         AND item.executor_contract_version=binding.executor_contract_version
         AND item.delivery_contract_version=binding.delivery_contract_version
         AND item.charge_contract_version=binding.charge_contract_version
         AND item.idempotency_contract_version=binding.idempotency_contract_version
         AND qualification.qualified
         AND qualification.approval_id=binding.production_approval_id
         AND qualification.current_connection_revision=binding.approved_connection_revision
         AND qualification.current_deployment_revision=binding.approved_deployment_revision
         AND qualification.current_operation_revision=binding.approved_operation_revision
         AND qualification.https_origin=binding.https_origin
         AND contract.operation_id IS NOT NULL
       ) AS public_executable
  FROM model_operation_public_execution_bindings binding
  JOIN runtime_change_releases release_row ON release_row.id=binding.release_id
  LEFT JOIN runtime_route_revision_items item
    ON item.public_execution_binding_id=binding.public_execution_binding_id
   AND item.release_id=binding.release_id AND item.ordinal=binding.ordinal
   AND item.deployment_id=binding.deployment_id
   AND item.operation_id=binding.operation_id
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=binding.deployment_id
   AND qualification.operation_id=binding.operation_id
  LEFT JOIN model_operation_current_public_execution_contracts contract
    ON contract.operation_id=binding.operation_id
   AND contract.public_path=binding.public_path
   AND contract.protocol_profile_id=binding.protocol_profile_id
   AND contract.protocol_profile_version=binding.protocol_profile_version
   AND contract.executor_contract_version=binding.executor_contract_version
   AND contract.delivery_contract_version=binding.delivery_contract_version
   AND contract.charge_contract_version=binding.charge_contract_version
   AND contract.idempotency_contract_version=binding.idempotency_contract_version;

CREATE OR REPLACE FUNCTION enforce_runtime_release_p3_qualification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.route_items_sealed_at IS NULL AND NEW.route_items_sealed_at IS NOT NULL
       AND EXISTS (
           SELECT 1
             FROM runtime_route_revision_items item
             LEFT JOIN model_operation_public_execution_qualifications qualification
               ON qualification.public_execution_binding_id=item.public_execution_binding_id
              AND qualification.release_id=item.release_id
              AND qualification.ordinal=item.ordinal
            WHERE item.release_id=NEW.id
              AND item.operation_id IN ('openai.responses.v1','anthropic.messages.v1')
              AND qualification.public_executable IS DISTINCT FROM TRUE
       ) THEN
        RAISE EXCEPTION 'Responses/Messages runtime manifest lacks current M77 public execution qualification'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

COMMENT ON TABLE model_operation_public_execution_contract_extensions IS
    'Migration-owned delivery/charge/idempotency extension of the exact M75 provider execution support tuple.';
COMMENT ON TABLE model_operation_public_execution_bindings IS
    'Append-only per-release-item structural authority for public non-stream execution. Credential authority is deliberately selected and revalidated per attempt.';
COMMENT ON VIEW model_operation_public_execution_qualifications IS
    'Current structural public-execution authority; it does not assert current credential eligibility.';
COMMENT ON COLUMN billing_provider_attempts.route_evidence_hash IS
    'SHA-256 of the normalized EvidenceV1 or explicit EvidenceV2 payload selected by route_evidence_version.';
COMMENT ON COLUMN billing_provider_attempts.route_public_execution_binding_id IS
    'EvidenceV2-only immutable per-release-item public execution authority; NULL for historical and V1 attempts.';
COMMENT ON COLUMN call_logs.route_public_execution_binding_id IS
    'Exact compact EvidenceV2 authority materialized from the immutable provider attempt; NULL for V1.';
COMMENT ON COLUMN request_trace_events.route_public_execution_binding_id IS
    'Exact compact EvidenceV2 authority materialized from the immutable provider attempt; NULL for V1.';

COMMIT;
