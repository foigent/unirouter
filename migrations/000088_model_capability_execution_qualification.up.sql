BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_deployment_capabilities, model_deployment_operations,
    model_deployments, provider_connections, provider_credential_bindings,
    credential_binding_deployments, model_registry, users, orgs,
    billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- Capability support is stable configuration; probe/approval evidence is not.
-- Keeping a separate support revision lets an equivalent newer receipt become
-- the preferred projection without invalidating an already frozen Plan.
ALTER TABLE model_deployment_capabilities
    ADD COLUMN support_revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT model_deployment_capabilities_support_revision_check
        CHECK (support_revision > 0);

CREATE FUNCTION enforce_model_deployment_capability_support_revision()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.support_state IS DISTINCT FROM OLD.support_state THEN
        NEW.support_revision := OLD.support_revision + 1;
    ELSE
        NEW.support_revision := OLD.support_revision;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER a_model_deployment_capabilities_support_revision
BEFORE UPDATE ON model_deployment_capabilities
FOR EACH ROW EXECUTE FUNCTION enforce_model_deployment_capability_support_revision();

CREATE TABLE model_capability_probe_contract_bindings (
    operation_id VARCHAR(80) NOT NULL,
    capability_id VARCHAR(80) NOT NULL,
    execution_profile_id VARCHAR(120) NOT NULL,
    execution_profile_version VARCHAR(40) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    probe_contract_id VARCHAR(120) NOT NULL,
    probe_contract_version VARCHAR(40) NOT NULL,
    expected_provider VARCHAR(80) NOT NULL,
    expected_upstream_model VARCHAR(200) NOT NULL,
    public_path VARCHAR(255) NOT NULL,
    introduced_migration VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        operation_id,capability_id,
        execution_profile_id,execution_profile_version,
        protocol_profile_id,protocol_profile_version,
        probe_contract_id,probe_contract_version
    ),
    CONSTRAINT model_capability_probe_contract_bindings_identity_check CHECK (
        operation_id=BTRIM(operation_id) AND BTRIM(operation_id)<>''
        AND capability_id=BTRIM(capability_id) AND BTRIM(capability_id)<>''
        AND execution_profile_id=BTRIM(execution_profile_id) AND BTRIM(execution_profile_id)<>''
        AND execution_profile_version=BTRIM(execution_profile_version) AND BTRIM(execution_profile_version)<>''
        AND protocol_profile_id=BTRIM(protocol_profile_id) AND BTRIM(protocol_profile_id)<>''
        AND protocol_profile_version=BTRIM(protocol_profile_version) AND BTRIM(protocol_profile_version)<>''
        AND probe_contract_id=BTRIM(probe_contract_id) AND BTRIM(probe_contract_id)<>''
        AND probe_contract_version=BTRIM(probe_contract_version) AND BTRIM(probe_contract_version)<>''
        AND expected_provider=BTRIM(expected_provider) AND BTRIM(expected_provider)<>''
        AND expected_upstream_model=BTRIM(expected_upstream_model) AND BTRIM(expected_upstream_model)<>''
        AND public_path=BTRIM(public_path) AND LEFT(public_path,1)='/'
        AND introduced_migration='000088_model_capability_execution_qualification'
        AND CONCAT_WS('',operation_id,capability_id,execution_profile_id,
            execution_profile_version,protocol_profile_id,protocol_profile_version,
            probe_contract_id,probe_contract_version,expected_provider,
            expected_upstream_model,public_path) !~ E'[\r\n]'
    )
);

CREATE FUNCTION reject_model_capability_probe_contract_binding_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model capability probe contract bindings are immutable'
        USING ERRCODE='55000';
END;
$$;

CREATE TABLE model_capability_current_probe_contracts (
    operation_id VARCHAR(80) NOT NULL,
    capability_id VARCHAR(80) NOT NULL,
    execution_profile_id VARCHAR(120) NOT NULL,
    execution_profile_version VARCHAR(40) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    probe_contract_id VARCHAR(120) NOT NULL,
    probe_contract_version VARCHAR(40) NOT NULL,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        operation_id,capability_id,
        execution_profile_id,execution_profile_version,
        protocol_profile_id,protocol_profile_version
    ),
    FOREIGN KEY (
        operation_id,capability_id,
        execution_profile_id,execution_profile_version,
        protocol_profile_id,protocol_profile_version,
        probe_contract_id,probe_contract_version
    ) REFERENCES model_capability_probe_contract_bindings (
        operation_id,capability_id,
        execution_profile_id,execution_profile_version,
        protocol_profile_id,protocol_profile_version,
        probe_contract_id,probe_contract_version
    ) ON DELETE RESTRICT
);

CREATE FUNCTION reject_model_capability_current_probe_contract_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'current model capability probe contracts are migration-owned'
        USING ERRCODE='55000';
END;
$$;

INSERT INTO model_capability_probe_contract_bindings (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version,expected_provider,
    expected_upstream_model,public_path,introduced_migration
) VALUES (
    'openai.chat_completions.v1','streaming','deepseek.v4-flash.api','1.4.0',
    'protocol.deepseek-openai-chat.v4','1.0.0',
    'unirouter.capability-probe.deepseek-chat-sse.v1','1.0.0',
    'deepseek','deepseek-v4-flash','/v1/chat/completions',
    '000088_model_capability_execution_qualification'
);

INSERT INTO model_capability_current_probe_contracts (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version
) VALUES (
    'openai.chat_completions.v1','streaming','deepseek.v4-flash.api','1.4.0',
    'protocol.deepseek-openai-chat.v4','1.0.0',
    'unirouter.capability-probe.deepseek-chat-sse.v1','1.0.0'
);

-- Both support tables are migration-owned. Future contract versions must be
-- introduced by a migration that deliberately opens and reseals this boundary;
-- runtime SQL cannot create a second support authority beside compiled code.
CREATE TRIGGER model_capability_probe_contract_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_contract_binding_mutation();

CREATE TRIGGER model_capability_current_probe_contracts_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_current_probe_contract_mutation();

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM model_deployment_capabilities capability
          JOIN model_deployments deployment ON deployment.id=capability.deployment_id
          JOIN model_registry model ON model.id=deployment.public_model_id
          JOIN model_deployment_operations operation
            ON operation.deployment_id=capability.deployment_id
           AND operation.operation_id=capability.operation_id
         WHERE capability.operation_id='openai.chat_completions.v1'
           AND capability.capability_id='streaming'
           AND model.execution_profile_id='deepseek.v4-flash.api'
           AND model.execution_profile_version='1.4.0'
           AND operation.protocol_profile_id='protocol.deepseek-openai-chat.v4'
           AND operation.protocol_profile_version='1.0.0'
           AND capability.evidence_state IN ('live_probed','production_approved')
    ) THEN
        RAISE EXCEPTION 'pre-authority DeepSeek streaming live or production evidence exists'
            USING ERRCODE='23514';
    END IF;
END;
$$;

-- `supported` is an exact Profile/typed-contract projection, not live
-- evidence. Materialize only the one compiled M88 tuple for matching existing
-- deployments. Existing rows, including an explicit unsupported decision, are
-- never overwritten; probe and routing approval remain separate later steps.
INSERT INTO model_deployment_capabilities (
    deployment_id,operation_id,capability_id,support_state,evidence_state,
    evidence_ref,revision
)
SELECT deployment.id,operation.operation_id,current_contract.capability_id,
       'enabled','declared',
       'capability_contract:' || current_contract.probe_contract_id || '@' ||
           current_contract.probe_contract_version,
       1
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id
  JOIN model_capability_current_probe_contracts current_contract
    ON current_contract.operation_id=operation.operation_id
   AND current_contract.execution_profile_id=model.execution_profile_id
   AND current_contract.execution_profile_version=model.execution_profile_version
   AND current_contract.protocol_profile_id=operation.protocol_profile_id
   AND current_contract.protocol_profile_version=operation.protocol_profile_version
  JOIN model_capability_probe_contract_bindings contract
    ON contract.operation_id=current_contract.operation_id
   AND contract.capability_id=current_contract.capability_id
   AND contract.execution_profile_id=current_contract.execution_profile_id
   AND contract.execution_profile_version=current_contract.execution_profile_version
   AND contract.protocol_profile_id=current_contract.protocol_profile_id
   AND contract.protocol_profile_version=current_contract.protocol_profile_version
   AND contract.probe_contract_id=current_contract.probe_contract_id
   AND contract.probe_contract_version=current_contract.probe_contract_version
 WHERE connection.provider=contract.expected_provider
   AND deployment.upstream_model=contract.expected_upstream_model
ON CONFLICT (deployment_id,operation_id,capability_id) DO NOTHING;

CREATE TABLE model_capability_probes (
    capability_probe_id BIGSERIAL PRIMARY KEY,
    customer_org_id BIGINT NOT NULL,
    deployment_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    capability_id VARCHAR(80) NOT NULL,
    execution_profile_id VARCHAR(120) NOT NULL,
    execution_profile_version VARCHAR(40) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    probe_contract_id VARCHAR(120) NOT NULL,
    probe_contract_version VARCHAR(40) NOT NULL,
    provider VARCHAR(80) NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    adapter_type VARCHAR(80) NOT NULL,
    adapter_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    upstream_model VARCHAR(200) NOT NULL,
    connection_revision BIGINT NOT NULL,
    deployment_revision BIGINT NOT NULL,
    operation_revision BIGINT NOT NULL,
    capability_support_revision BIGINT NOT NULL,
    credential_binding_id BIGINT NOT NULL,
    credential_state_revision BIGINT NOT NULL,
    credential_link_created_at TIMESTAMPTZ(6) NOT NULL,
    provider_success BOOLEAN NOT NULL,
    success BOOLEAN NOT NULL,
    status_code INTEGER NOT NULL,
    latency_ms BIGINT NOT NULL,
    reported_model VARCHAR(200) NOT NULL DEFAULT '',
    error_summary VARCHAR(255) NOT NULL DEFAULT '',
    request_summary JSONB NOT NULL,
    response_summary JSONB NOT NULL,
    source_kind VARCHAR(100) NOT NULL,
    source_ref VARCHAR(100) NOT NULL,
    source_fingerprint VARCHAR(71) NOT NULL,
    tested_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    tested_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_capability_probes_deployment_fk
        FOREIGN KEY (deployment_id,connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_probes_customer_deployment_fk
        FOREIGN KEY (customer_org_id,deployment_id)
        REFERENCES model_deployments(customer_org_id,id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_probes_operation_fk
        FOREIGN KEY (deployment_id,operation_id)
        REFERENCES model_deployment_operations(deployment_id,operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_probes_capability_fk
        FOREIGN KEY (deployment_id,operation_id,capability_id)
        REFERENCES model_deployment_capabilities(deployment_id,operation_id,capability_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_probes_credential_fk
        FOREIGN KEY (credential_binding_id,connection_id)
        REFERENCES provider_credential_bindings(id,connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_probes_contract_fk
        FOREIGN KEY (
            operation_id,capability_id,
            execution_profile_id,execution_profile_version,
            protocol_profile_id,protocol_profile_version,
            probe_contract_id,probe_contract_version
        ) REFERENCES model_capability_probe_contract_bindings (
            operation_id,capability_id,
            execution_profile_id,execution_profile_version,
            protocol_profile_id,protocol_profile_version,
            probe_contract_id,probe_contract_version
        ) ON DELETE RESTRICT,
    CONSTRAINT model_capability_probes_identity_check CHECK (
        customer_org_id>0 AND deployment_id>0 AND connection_id>0
        AND credential_binding_id>0 AND connection_revision>0
        AND deployment_revision>0 AND operation_revision>0
        AND capability_support_revision>0 AND credential_state_revision>0
        AND status_code BETWEEN 0 AND 599 AND latency_ms>=0
        AND provider=BTRIM(provider) AND BTRIM(provider)<>''
        AND base_url=BTRIM(base_url) AND BTRIM(base_url)<>''
        AND adapter_type=BTRIM(adapter_type) AND BTRIM(adapter_type)<>''
        AND upstream_model=BTRIM(upstream_model) AND BTRIM(upstream_model)<>''
        AND reported_model=BTRIM(reported_model)
        AND error_summary=BTRIM(error_summary)
        AND source_kind='admin.model_capability_probe.v1'
        AND source_ref=BTRIM(source_ref) AND BTRIM(source_ref)<>''
        AND source_fingerprint ~ '^sha256:[a-f0-9]{64}$'
        AND pg_column_size(request_summary)<=16384
        AND pg_column_size(response_summary)<=32768
        AND CONCAT_WS('',provider,base_url,adapter_type,upstream_model,
            reported_model,error_summary,source_kind,source_ref) !~ E'[\r\n]'
    )
);

CREATE INDEX model_capability_probes_target_idx
    ON model_capability_probes (
        deployment_id,operation_id,capability_id,
        execution_profile_id,execution_profile_version,
        tested_at DESC,capability_probe_id DESC
    );

CREATE FUNCTION reject_model_capability_probe_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model capability probes are append-only'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_capability_probes_append_only
BEFORE UPDATE OR DELETE OR TRUNCATE ON model_capability_probes
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_mutation();

CREATE TABLE model_capability_routing_approvals (
    approval_id BIGSERIAL PRIMARY KEY,
    capability_probe_id BIGINT NOT NULL UNIQUE
        REFERENCES model_capability_probes(capability_probe_id) ON DELETE RESTRICT,
    customer_org_id BIGINT NOT NULL,
    deployment_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    capability_id VARCHAR(80) NOT NULL,
    execution_profile_id VARCHAR(120) NOT NULL,
    execution_profile_version VARCHAR(40) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    probe_contract_id VARCHAR(120) NOT NULL,
    probe_contract_version VARCHAR(40) NOT NULL,
    connection_revision BIGINT NOT NULL,
    deployment_revision BIGINT NOT NULL,
    operation_revision BIGINT NOT NULL,
    capability_support_revision BIGINT NOT NULL,
    credential_binding_id BIGINT NOT NULL,
    credential_state_revision BIGINT NOT NULL,
    credential_link_created_at TIMESTAMPTZ(6) NOT NULL,
    approved_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    approved_by_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    reason VARCHAR(512) NOT NULL,
    source_kind VARCHAR(100) NOT NULL,
    source_ref VARCHAR(100) NOT NULL,
    approved_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_capability_routing_approvals_deployment_fk
        FOREIGN KEY (deployment_id,connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_routing_approvals_customer_deployment_fk
        FOREIGN KEY (customer_org_id,deployment_id)
        REFERENCES model_deployments(customer_org_id,id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_routing_approvals_operation_fk
        FOREIGN KEY (deployment_id,operation_id)
        REFERENCES model_deployment_operations(deployment_id,operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_routing_approvals_capability_fk
        FOREIGN KEY (deployment_id,operation_id,capability_id)
        REFERENCES model_deployment_capabilities(deployment_id,operation_id,capability_id) ON DELETE RESTRICT,
    CONSTRAINT model_capability_routing_approvals_identity_check CHECK (
        customer_org_id>0 AND deployment_id>0 AND connection_id>0
        AND credential_binding_id>0 AND connection_revision>0
        AND deployment_revision>0 AND operation_revision>0
        AND capability_support_revision>0 AND credential_state_revision>0
        AND approved_by>0 AND approved_by_org_id>0
        AND reason=BTRIM(reason) AND BTRIM(reason)<>''
        AND reason !~ E'[\r\n]'
        AND source_kind='admin.model_capability_routing_approval.v1'
        AND source_ref='capability_probe:' || capability_probe_id::TEXT
    )
);

CREATE INDEX model_capability_routing_approvals_target_idx
    ON model_capability_routing_approvals (
        deployment_id,operation_id,capability_id,
        execution_profile_id,execution_profile_version,
        approved_at DESC,approval_id DESC
    );

CREATE FUNCTION validate_model_capability_routing_approval()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof RECORD;
    actor RECORD;
    root_customer_count BIGINT;
    root_customer_id BIGINT;
BEGIN
    LOCK TABLE orgs IN SHARE MODE;
    SELECT COUNT(*),MIN(id)
      INTO root_customer_count,root_customer_id
      FROM orgs
     WHERE status='active' AND org_type='customer' AND parent_id IS NULL;
    IF root_customer_count<>1 OR root_customer_id IS DISTINCT FROM NEW.customer_org_id THEN
        RAISE EXCEPTION 'capability approval customer root authority is stale or ambiguous'
            USING ERRCODE='23514';
    END IF;

    SELECT probe.*,connection.config_revision AS current_connection_revision,
           connection.provider AS current_provider,connection.base_url AS current_base_url,
           connection.adapter_type AS current_adapter_type,
           COALESCE(connection.adapter_config,'{}'::jsonb) AS current_adapter_config,
           deployment.config_revision AS current_deployment_revision,
           deployment.upstream_model AS current_upstream_model,
           operation.revision AS current_operation_revision,
           operation.protocol_profile_id AS current_protocol_profile_id,
           operation.protocol_profile_version AS current_protocol_profile_version,
           capability.support_state AS current_capability_support_state,
           capability.support_revision AS current_capability_support_revision,
           model.execution_profile_id AS current_execution_profile_id,
           model.execution_profile_version AS current_execution_profile_version,
           binding.status AS current_credential_status,
           binding.key_enc AS current_credential_key,
           binding.state_revision AS current_credential_state_revision,
           binding.cooldown_until AS current_credential_cooldown,
           link.created_at AS current_credential_link_created_at,
           current_contract.probe_contract_id AS current_probe_contract_id,
           current_contract.probe_contract_version AS current_probe_contract_version,
           contract.expected_provider AS expected_contract_provider,
           contract.expected_upstream_model AS expected_contract_upstream_model
      INTO proof
      FROM model_capability_probes probe
      JOIN model_deployments deployment ON deployment.id=probe.deployment_id
      JOIN provider_connections connection ON connection.id=probe.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=probe.deployment_id
       AND operation.operation_id=probe.operation_id
      JOIN model_deployment_capabilities capability
        ON capability.deployment_id=probe.deployment_id
       AND capability.operation_id=probe.operation_id
       AND capability.capability_id=probe.capability_id
      JOIN model_registry model ON model.id=deployment.public_model_id
      JOIN provider_credential_bindings binding
        ON binding.id=probe.credential_binding_id
       AND binding.connection_id=probe.connection_id
      JOIN credential_binding_deployments link
        ON link.binding_id=probe.credential_binding_id
       AND link.deployment_id=probe.deployment_id
       AND link.connection_id=probe.connection_id
      JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=probe.operation_id
       AND current_contract.capability_id=probe.capability_id
       AND current_contract.execution_profile_id=probe.execution_profile_id
       AND current_contract.execution_profile_version=probe.execution_profile_version
       AND current_contract.protocol_profile_id=probe.protocol_profile_id
       AND current_contract.protocol_profile_version=probe.protocol_profile_version
      JOIN model_capability_probe_contract_bindings contract
        ON contract.operation_id=probe.operation_id
       AND contract.capability_id=probe.capability_id
       AND contract.execution_profile_id=probe.execution_profile_id
       AND contract.execution_profile_version=probe.execution_profile_version
       AND contract.protocol_profile_id=probe.protocol_profile_id
       AND contract.protocol_profile_version=probe.protocol_profile_version
       AND contract.probe_contract_id=probe.probe_contract_id
       AND contract.probe_contract_version=probe.probe_contract_version
     WHERE probe.capability_probe_id=NEW.capability_probe_id
     FOR UPDATE OF probe,deployment,connection,operation,capability,binding,link;

    SELECT user_row.id,user_row.org_id,
           COALESCE(NULLIF(user_row.organization_role,''),user_row.role) AS organization_role,
           user_row.status AS user_status,org_row.status AS org_status
      INTO actor
      FROM users user_row
      JOIN orgs org_row ON org_row.id=user_row.org_id
     WHERE user_row.id=NEW.approved_by
     FOR KEY SHARE OF user_row,org_row;

    IF NOT FOUND OR actor.org_id IS DISTINCT FROM NEW.approved_by_org_id
       OR actor.organization_role<>'platform_admin'
       OR actor.user_status<>'active' OR actor.org_status<>'active' THEN
        RAISE EXCEPTION 'capability routing approval requires a current platform administrator'
            USING ERRCODE='42501';
    END IF;

    IF proof.capability_probe_id IS NULL OR NOT proof.success
       OR proof.source_kind<>'admin.model_capability_probe.v1'
       OR proof.current_capability_support_state<>'enabled'
       OR proof.current_connection_revision IS DISTINCT FROM proof.connection_revision
       OR proof.current_deployment_revision IS DISTINCT FROM proof.deployment_revision
       OR proof.current_operation_revision IS DISTINCT FROM proof.operation_revision
       OR proof.current_capability_support_revision IS DISTINCT FROM proof.capability_support_revision
       OR proof.current_execution_profile_id IS DISTINCT FROM proof.execution_profile_id
       OR proof.current_execution_profile_version IS DISTINCT FROM proof.execution_profile_version
       OR proof.current_protocol_profile_id IS DISTINCT FROM proof.protocol_profile_id
       OR proof.current_protocol_profile_version IS DISTINCT FROM proof.protocol_profile_version
       OR proof.current_probe_contract_id IS DISTINCT FROM proof.probe_contract_id
       OR proof.current_probe_contract_version IS DISTINCT FROM proof.probe_contract_version
       OR proof.expected_contract_provider IS DISTINCT FROM proof.provider
       OR proof.expected_contract_upstream_model IS DISTINCT FROM proof.upstream_model
       OR proof.current_provider IS DISTINCT FROM proof.provider
       OR proof.current_base_url IS DISTINCT FROM proof.base_url
       OR proof.current_adapter_type IS DISTINCT FROM proof.adapter_type
       OR proof.current_adapter_config IS DISTINCT FROM proof.adapter_config
       OR proof.current_upstream_model IS DISTINCT FROM proof.upstream_model
       OR proof.current_credential_status<>'active' OR proof.current_credential_key=''
       OR proof.current_credential_state_revision IS DISTINCT FROM proof.credential_state_revision
       OR proof.current_credential_link_created_at IS DISTINCT FROM proof.credential_link_created_at
       OR (proof.current_credential_cooldown IS NOT NULL
           AND proof.current_credential_cooldown>transaction_timestamp())
       OR NEW.customer_org_id IS DISTINCT FROM proof.customer_org_id
       OR NEW.deployment_id IS DISTINCT FROM proof.deployment_id
       OR NEW.connection_id IS DISTINCT FROM proof.connection_id
       OR NEW.operation_id IS DISTINCT FROM proof.operation_id
       OR NEW.capability_id IS DISTINCT FROM proof.capability_id
       OR NEW.execution_profile_id IS DISTINCT FROM proof.execution_profile_id
       OR NEW.execution_profile_version IS DISTINCT FROM proof.execution_profile_version
       OR NEW.protocol_profile_id IS DISTINCT FROM proof.protocol_profile_id
       OR NEW.protocol_profile_version IS DISTINCT FROM proof.protocol_profile_version
       OR NEW.probe_contract_id IS DISTINCT FROM proof.probe_contract_id
       OR NEW.probe_contract_version IS DISTINCT FROM proof.probe_contract_version
       OR NEW.connection_revision IS DISTINCT FROM proof.connection_revision
       OR NEW.deployment_revision IS DISTINCT FROM proof.deployment_revision
       OR NEW.operation_revision IS DISTINCT FROM proof.operation_revision
       OR NEW.capability_support_revision IS DISTINCT FROM proof.capability_support_revision
       OR NEW.credential_binding_id IS DISTINCT FROM proof.credential_binding_id
       OR NEW.credential_state_revision IS DISTINCT FROM proof.credential_state_revision
       OR NEW.credential_link_created_at IS DISTINCT FROM proof.credential_link_created_at
       OR NEW.source_kind<>'admin.model_capability_routing_approval.v1'
       OR NEW.source_ref IS DISTINCT FROM 'capability_probe:' || NEW.capability_probe_id::TEXT THEN
        RAISE EXCEPTION 'capability routing approval proof is stale, incomplete, or mismatched'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_capability_routing_approvals_validate
BEFORE INSERT ON model_capability_routing_approvals
FOR EACH ROW EXECUTE FUNCTION validate_model_capability_routing_approval();

CREATE FUNCTION reject_model_capability_routing_approval_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model capability routing approvals are append-only'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_capability_routing_approvals_append_only
BEFORE UPDATE OR DELETE OR TRUNCATE ON model_capability_routing_approvals
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_routing_approval_mutation();

CREATE VIEW model_capability_approval_qualifications AS
WITH root_authority AS (
    SELECT COUNT(*) AS root_count,MIN(id) AS root_customer_id
      FROM orgs
     WHERE status='active' AND org_type='customer' AND parent_id IS NULL
), facts AS (
    SELECT approval.*,probe.provider,probe.base_url,probe.adapter_type,probe.adapter_config,
           probe.upstream_model,probe.success AS probe_success,
           probe.source_kind AS probe_source_kind,
           connection.config_revision AS current_connection_revision,
           connection.provider AS current_provider,connection.base_url AS current_base_url,
           connection.adapter_type AS current_adapter_type,
           COALESCE(connection.adapter_config,'{}'::jsonb) AS current_adapter_config,
           deployment.config_revision AS current_deployment_revision,
           deployment.upstream_model AS current_upstream_model,
           operation.revision AS current_operation_revision,
           operation.protocol_profile_id AS current_protocol_profile_id,
           operation.protocol_profile_version AS current_protocol_profile_version,
           capability.support_state AS current_capability_support_state,
           capability.support_revision AS current_capability_support_revision,
           model.execution_profile_id AS current_execution_profile_id,
           model.execution_profile_version AS current_execution_profile_version,
           current_contract.probe_contract_id AS current_probe_contract_id,
           current_contract.probe_contract_version AS current_probe_contract_version,
           contract.expected_provider,contract.expected_upstream_model,
           root_authority.root_count,root_authority.root_customer_id
      FROM model_capability_routing_approvals approval
      JOIN model_capability_probes probe
        ON probe.capability_probe_id=approval.capability_probe_id
      JOIN model_deployments deployment ON deployment.id=approval.deployment_id
      JOIN provider_connections connection ON connection.id=approval.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=approval.deployment_id
       AND operation.operation_id=approval.operation_id
      JOIN model_deployment_capabilities capability
        ON capability.deployment_id=approval.deployment_id
       AND capability.operation_id=approval.operation_id
       AND capability.capability_id=approval.capability_id
      JOIN model_registry model ON model.id=deployment.public_model_id
      LEFT JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=approval.operation_id
       AND current_contract.capability_id=approval.capability_id
       AND current_contract.execution_profile_id=approval.execution_profile_id
       AND current_contract.execution_profile_version=approval.execution_profile_version
       AND current_contract.protocol_profile_id=approval.protocol_profile_id
       AND current_contract.protocol_profile_version=approval.protocol_profile_version
      LEFT JOIN model_capability_probe_contract_bindings contract
        ON contract.operation_id=approval.operation_id
       AND contract.capability_id=approval.capability_id
       AND contract.execution_profile_id=approval.execution_profile_id
       AND contract.execution_profile_version=approval.execution_profile_version
       AND contract.protocol_profile_id=approval.protocol_profile_id
       AND contract.protocol_profile_version=approval.protocol_profile_version
       AND contract.probe_contract_id=approval.probe_contract_id
       AND contract.probe_contract_version=approval.probe_contract_version
      CROSS JOIN root_authority
), evaluated AS (
    SELECT facts.*,
           CASE
             WHEN root_count<>1 OR root_customer_id IS DISTINCT FROM customer_org_id
               THEN 'customer_authority_stale'
             WHEN NOT probe_success OR probe_source_kind<>'admin.model_capability_probe.v1'
               THEN 'probe_invalid'
             WHEN current_capability_support_state<>'enabled'
               THEN 'capability_unsupported'
             WHEN current_connection_revision IS DISTINCT FROM connection_revision
               OR current_provider IS DISTINCT FROM provider
               OR current_base_url IS DISTINCT FROM base_url
               OR current_adapter_type IS DISTINCT FROM adapter_type
               OR current_adapter_config IS DISTINCT FROM adapter_config
               THEN 'connection_stale'
             WHEN current_deployment_revision IS DISTINCT FROM deployment_revision
               OR current_upstream_model IS DISTINCT FROM upstream_model
               THEN 'deployment_stale'
             WHEN current_operation_revision IS DISTINCT FROM operation_revision
               OR current_protocol_profile_id IS DISTINCT FROM protocol_profile_id
               OR current_protocol_profile_version IS DISTINCT FROM protocol_profile_version
               THEN 'operation_stale'
             WHEN current_capability_support_revision IS DISTINCT FROM capability_support_revision
               THEN 'capability_support_stale'
             WHEN current_execution_profile_id IS DISTINCT FROM execution_profile_id
               OR current_execution_profile_version IS DISTINCT FROM execution_profile_version
               THEN 'execution_profile_stale'
             WHEN current_probe_contract_id IS DISTINCT FROM probe_contract_id
               OR current_probe_contract_version IS DISTINCT FROM probe_contract_version
               OR expected_provider IS DISTINCT FROM provider
               OR expected_upstream_model IS DISTINCT FROM upstream_model
               THEN 'probe_contract_stale'
             ELSE 'qualified'
           END AS qualification_state
      FROM facts
)
SELECT evaluated.*,qualification_state='qualified' AS qualified
  FROM evaluated;

CREATE VIEW model_capability_execution_qualifications AS
SELECT DISTINCT ON (
       deployment_id,operation_id,capability_id,
       execution_profile_id,execution_profile_version,
       protocol_profile_id,protocol_profile_version,
       probe_contract_id,probe_contract_version
   )
       approval_id,capability_probe_id,customer_org_id,deployment_id,connection_id,
       operation_id,capability_id,execution_profile_id,execution_profile_version,
       protocol_profile_id,protocol_profile_version,probe_contract_id,
       probe_contract_version,capability_support_revision,approved_at,
       qualification_state,qualified
  FROM model_capability_approval_qualifications
 WHERE qualified
 ORDER BY deployment_id,operation_id,capability_id,
          execution_profile_id,execution_profile_version,
          protocol_profile_id,protocol_profile_version,
          probe_contract_id,probe_contract_version,
          approved_at DESC,approval_id DESC;

CREATE FUNCTION enforce_model_capability_evidence_projection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    current_model RECORD;
    numeric_id BIGINT;
    valid_proof BOOLEAN;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.evidence_state NOT IN ('live_probed','production_approved') THEN
            RETURN NEW;
        END IF;
    ELSIF NEW.evidence_state IS NOT DISTINCT FROM OLD.evidence_state
       AND NEW.evidence_ref IS NOT DISTINCT FROM OLD.evidence_ref THEN
        RETURN NEW;
    END IF;

    SELECT model.execution_profile_id,model.execution_profile_version,
           operation.protocol_profile_id,operation.protocol_profile_version,
           current_contract.probe_contract_id,current_contract.probe_contract_version
      INTO current_model
      FROM model_deployments deployment
      JOIN model_registry model ON model.id=deployment.public_model_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=NEW.deployment_id
       AND operation.operation_id=NEW.operation_id
      LEFT JOIN model_capability_current_probe_contracts current_contract
        ON current_contract.operation_id=NEW.operation_id
       AND current_contract.capability_id=NEW.capability_id
       AND current_contract.execution_profile_id=model.execution_profile_id
       AND current_contract.execution_profile_version=model.execution_profile_version
       AND current_contract.protocol_profile_id=operation.protocol_profile_id
       AND current_contract.protocol_profile_version=operation.protocol_profile_version
     WHERE deployment.id=NEW.deployment_id;

    IF current_model.probe_contract_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.evidence_state='live_probed'
       AND NEW.evidence_ref ~ '^capability_probe:[1-9][0-9]*$' THEN
        numeric_id := SUBSTRING(NEW.evidence_ref FROM 18)::BIGINT;
        SELECT EXISTS (
            SELECT 1 FROM model_capability_probes probe
             WHERE probe.capability_probe_id=numeric_id
               AND probe.deployment_id=NEW.deployment_id
               AND probe.operation_id=NEW.operation_id
               AND probe.capability_id=NEW.capability_id
               AND probe.execution_profile_id=current_model.execution_profile_id
               AND probe.execution_profile_version=current_model.execution_profile_version
               AND probe.protocol_profile_id=current_model.protocol_profile_id
               AND probe.protocol_profile_version=current_model.protocol_profile_version
               AND probe.probe_contract_id=current_model.probe_contract_id
               AND probe.probe_contract_version=current_model.probe_contract_version
               AND probe.capability_support_revision=NEW.support_revision
               AND probe.success
        ) INTO valid_proof;
    ELSIF NEW.evidence_state='production_approved'
       AND NEW.evidence_ref ~ '^capability_approval:[1-9][0-9]*$' THEN
        numeric_id := SUBSTRING(NEW.evidence_ref FROM 21)::BIGINT;
        SELECT EXISTS (
            SELECT 1 FROM model_capability_approval_qualifications qualification
             WHERE qualification.approval_id=numeric_id
               AND qualification.deployment_id=NEW.deployment_id
               AND qualification.operation_id=NEW.operation_id
               AND qualification.capability_id=NEW.capability_id
               AND qualification.qualified
        ) INTO valid_proof;
    ELSE
        valid_proof := FALSE;
    END IF;

    IF NOT COALESCE(valid_proof,FALSE) THEN
        RAISE EXCEPTION 'capability evidence projection lacks a current typed receipt'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_capabilities_receipt_projection_guard
BEFORE INSERT OR UPDATE ON model_deployment_capabilities
FOR EACH ROW EXECUTE FUNCTION enforce_model_capability_evidence_projection();

COMMENT ON VIEW model_capability_approval_qualifications IS
    'Per-receipt capability qualification. Pre-dispatch validates the receipt already bound to the frozen Plan and never requires it to remain preferred.';
COMMENT ON VIEW model_capability_execution_qualifications IS
    'Deterministic preferred current capability approval for new Plans and cutover proofs; raw capability projection rows are not routing authority.';

COMMIT;
