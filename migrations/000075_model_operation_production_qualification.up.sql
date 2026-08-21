BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_deployment_operations, model_deployment_probes,
    model_deployments, provider_connections, provider_credential_bindings,
    credential_binding_deployments, runtime_change_releases,
    runtime_route_revision_items, users, orgs, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- A mutable state label created before this qualification authority cannot be
-- grandfathered into production. It has no exact network proof or approval.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM model_deployment_operations
         WHERE operation_id IN ('openai.responses.v1','anthropic.messages.v1')
           AND evidence_state='production_approved'
    ) THEN
        RAISE EXCEPTION 'Model operation production qualification migration refused: pre-authority production approval exists';
    END IF;
END;
$$;

CREATE FUNCTION model_operation_https_origin(raw_url TEXT)
RETURNS TEXT LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
    parts TEXT[];
    authority_host TEXT;
    authority_port TEXT;
    label TEXT;
    parsed_ip INET;
BEGIN
    parts := regexp_match(
        BTRIM(raw_url, ' '),
        '^https://(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]+)(:([0-9]{1,5}))?/?$',
        'i'
    );
    IF parts IS NULL OR parts[1] IS NULL THEN
        RETURN NULL;
    END IF;
    authority_host := parts[1];
    authority_port := parts[3];
    IF authority_port IS NOT NULL
       AND (authority_port::INTEGER<1 OR authority_port::INTEGER>65535) THEN
        RETURN NULL;
    END IF;
    IF LEFT(authority_host,1)='[' AND RIGHT(authority_host,1)=']' THEN
        IF authority_host LIKE '%.%' THEN
            RETURN NULL;
        END IF;
        BEGIN
            parsed_ip := SUBSTRING(authority_host FROM 2 FOR LENGTH(authority_host)-2)::INET;
        EXCEPTION WHEN OTHERS THEN
            RETURN NULL;
        END;
        IF family(parsed_ip)<>6 OR parsed_ip <<= '::ffff:0:0/96'::INET THEN
            RETURN NULL;
        END IF;
        authority_host := '[' || host(parsed_ip) || ']';
    ELSE
        IF LENGTH(authority_host)>253 OR authority_host LIKE '.%' OR authority_host LIKE '%.'
           OR authority_host LIKE '%..%' THEN
            RETURN NULL;
        END IF;
        IF authority_host ~ '^[0-9.]+$' THEN
            BEGIN
                parsed_ip := authority_host::INET;
            EXCEPTION WHEN OTHERS THEN
                RETURN NULL;
            END;
            IF family(parsed_ip)<>4 OR host(parsed_ip)<>authority_host THEN
                RETURN NULL;
            END IF;
        ELSE
            FOREACH label IN ARRAY string_to_array(authority_host,'.') LOOP
                IF LENGTH(label)<1 OR LENGTH(label)>63
                   OR label !~ '^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$' THEN
                    RETURN NULL;
                END IF;
            END LOOP;
        END IF;
    END IF;
    RETURN 'https://' || LOWER(authority_host)
        || CASE WHEN authority_port IS NULL THEN '' ELSE ':' || authority_port END;
END;
$$;

-- This repository table is compiled support, not customer configuration. A
-- future binary may append a new binding only from a reviewed migration after
-- explicitly replacing the immutable trigger.
CREATE TABLE model_operation_execution_support_bindings (
    operation_id VARCHAR(80) NOT NULL,
    public_path VARCHAR(255) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    executor_contract_version VARCHAR(100) NOT NULL,
    introduced_migration VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        operation_id,public_path,protocol_profile_id,
        protocol_profile_version,executor_contract_version
    ),
    CONSTRAINT model_operation_execution_support_migration_unique
        UNIQUE (operation_id,introduced_migration),
    CONSTRAINT model_operation_execution_support_identity_check CHECK (
        operation_id=BTRIM(operation_id) AND BTRIM(operation_id)<>''
        AND public_path=BTRIM(public_path) AND public_path ~ '^/v1/[a-z][a-z0-9_/-]*$'
        AND protocol_profile_id=BTRIM(protocol_profile_id) AND BTRIM(protocol_profile_id)<>''
        AND protocol_profile_version=BTRIM(protocol_profile_version) AND BTRIM(protocol_profile_version)<>''
        AND executor_contract_version=BTRIM(executor_contract_version) AND BTRIM(executor_contract_version)<>''
        AND introduced_migration ~ '^000[0-9]{3}_[a-z0-9_]+$'
        AND operation_id !~ E'[\\x00\\r\\n]'
        AND public_path !~ E'[\\x00\\r\\n]'
        AND protocol_profile_id !~ E'[\\x00\\r\\n]'
        AND protocol_profile_version !~ E'[\\x00\\r\\n]'
        AND executor_contract_version !~ E'[\\x00\\r\\n]'
    )
);

INSERT INTO model_operation_execution_support_bindings (
    operation_id,public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,introduced_migration)
VALUES
    ('openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v1','1.0.0',
     'unirouter.endpoint-profile-nonstream-executor.v1','000075_model_operation_production_qualification'),
    ('anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v1','1.0.0',
     'unirouter.endpoint-profile-nonstream-executor.v1','000075_model_operation_production_qualification');

CREATE VIEW model_operation_current_execution_support_bindings AS
SELECT DISTINCT ON (operation_id) *
  FROM model_operation_execution_support_bindings
 ORDER BY operation_id,introduced_migration DESC;

CREATE FUNCTION reject_model_operation_execution_support_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model operation execution support bindings are migration-owned and immutable'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_operation_execution_support_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_execution_support_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_execution_support_mutation();

ALTER TABLE model_deployment_probes
    ADD COLUMN operation_revision BIGINT,
    ADD COLUMN public_path VARCHAR(255),
    ADD COLUMN executor_contract_version VARCHAR(100),
    ADD COLUMN https_origin VARCHAR(512),
    ADD CONSTRAINT model_deployment_probes_operation_revision_check
        CHECK (operation_revision IS NULL OR operation_revision > 0),
    ADD CONSTRAINT model_deployment_probes_execution_binding_shape_check CHECK (
        (
            operation_revision IS NULL AND public_path IS NULL
            AND executor_contract_version IS NULL AND https_origin IS NULL
        )
        OR
        (
            operation_revision IS NOT NULL
            AND public_path IS NOT NULL
            AND executor_contract_version IS NOT NULL
            AND https_origin IS NOT NULL
            AND public_path=BTRIM(public_path) AND public_path ~ '^/v1/[a-z][a-z0-9_/-]*$'
            AND executor_contract_version=BTRIM(executor_contract_version)
            AND BTRIM(executor_contract_version)<>''
            AND model_operation_https_origin(https_origin) IS NOT NULL
            AND https_origin=model_operation_https_origin(https_origin)
        )
    ),
    ADD CONSTRAINT model_deployment_probes_execution_support_fk
        FOREIGN KEY (
            operation_id,public_path,probe_profile_id,probe_profile_version,
            executor_contract_version
        ) REFERENCES model_operation_execution_support_bindings (
            operation_id,public_path,protocol_profile_id,protocol_profile_version,
            executor_contract_version
        ) ON DELETE RESTRICT;

CREATE FUNCTION enforce_model_operation_live_probe_contract()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    operation_row RECORD;
BEGIN
    IF NEW.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1') THEN
        IF NEW.operation_revision IS NOT NULL OR NEW.public_path IS NOT NULL
           OR NEW.executor_contract_version IS NOT NULL OR NEW.https_origin IS NOT NULL THEN
            RAISE EXCEPTION 'non-qualified operation probe cannot claim production execution binding'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.operation_revision IS NULL OR NEW.public_path IS NULL
       OR NEW.executor_contract_version IS NULL OR NEW.https_origin IS NULL
       OR NEW.source_kind<>'admin.endpointprotocol_live_probe.v1' THEN
        RAISE EXCEPTION 'Responses/Messages probe requires exact endpointprotocol network evidence'
            USING ERRCODE='23514';
    END IF;

    SELECT operation.revision AS operation_revision,
           operation.support_state,operation.protocol_profile_id,
           operation.protocol_profile_version,
           deployment.connection_id,deployment.upstream_model,
           deployment.config_revision AS deployment_revision,
           deployment.admin_state AS deployment_state,
           connection.config_revision AS connection_revision,
           connection.admin_state AS connection_state,
           model_operation_https_origin(connection.base_url) AS current_https_origin,
           support.public_path,support.executor_contract_version
      INTO operation_row
      FROM model_deployment_operations operation
      JOIN model_deployments deployment ON deployment.id=operation.deployment_id
      JOIN provider_connections connection ON connection.id=deployment.connection_id
      JOIN model_operation_execution_support_bindings support
        ON support.operation_id=NEW.operation_id
       AND support.public_path=NEW.public_path
       AND support.protocol_profile_id=NEW.probe_profile_id
       AND support.protocol_profile_version=NEW.probe_profile_version
       AND support.executor_contract_version=NEW.executor_contract_version
     WHERE operation.deployment_id=NEW.deployment_id
       AND operation.operation_id=NEW.operation_id
     FOR KEY SHARE OF operation,deployment,connection;

    IF NOT FOUND
       OR NEW.public_path IS DISTINCT FROM operation_row.public_path
       OR NEW.executor_contract_version IS DISTINCT FROM operation_row.executor_contract_version THEN
        RAISE EXCEPTION 'Responses/Messages probe execution binding is stale or mismatched'
            USING ERRCODE='23514';
    END IF;
    IF NEW.success AND (
       operation_row.support_state<>'enabled'
       OR operation_row.deployment_state<>'enabled'
       OR operation_row.connection_state<>'enabled'
       OR operation_row.current_https_origin IS NULL
       OR NEW.probe_profile_id IS DISTINCT FROM operation_row.protocol_profile_id
       OR NEW.probe_profile_version IS DISTINCT FROM operation_row.protocol_profile_version
       OR NEW.connection_id IS DISTINCT FROM operation_row.connection_id
       OR NEW.upstream_model IS DISTINCT FROM operation_row.upstream_model
       OR NEW.connection_revision IS DISTINCT FROM operation_row.connection_revision
       OR NEW.deployment_revision IS DISTINCT FROM operation_row.deployment_revision
       OR NEW.operation_revision IS DISTINCT FROM operation_row.operation_revision
       OR NEW.https_origin IS DISTINCT FROM operation_row.current_https_origin
    ) THEN
        RAISE EXCEPTION 'successful Responses/Messages probe must bind current executable state'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_probes_p3_contract_guard
BEFORE INSERT ON model_deployment_probes
FOR EACH ROW EXECUTE FUNCTION enforce_model_operation_live_probe_contract();

CREATE FUNCTION reject_model_deployment_probe_truncate()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model deployment probe attempts are append-only'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_deployment_probes_truncate_guard
BEFORE TRUNCATE ON model_deployment_probes
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_deployment_probe_truncate();

CREATE TABLE model_operation_production_approvals (
    approval_id BIGSERIAL PRIMARY KEY,
    customer_org_id BIGINT NOT NULL,
    deployment_id BIGINT NOT NULL,
    connection_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    probe_id BIGINT NOT NULL UNIQUE REFERENCES model_deployment_probes(probe_id) ON DELETE RESTRICT,
    credential_binding_id BIGINT NOT NULL,
    credential_state_revision BIGINT NOT NULL,
    credential_link_created_at TIMESTAMPTZ(6) NOT NULL,
    public_path VARCHAR(255) NOT NULL,
    protocol_profile_id VARCHAR(100) NOT NULL,
    protocol_profile_version VARCHAR(40) NOT NULL,
    executor_contract_version VARCHAR(100) NOT NULL,
    https_origin VARCHAR(512) NOT NULL,
    connection_revision BIGINT NOT NULL,
    deployment_revision BIGINT NOT NULL,
    probe_operation_revision BIGINT NOT NULL,
    source_operation_revision BIGINT NOT NULL,
    approved_operation_revision BIGINT NOT NULL,
    approved_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    approved_by_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    reason VARCHAR(512) NOT NULL,
    source_kind VARCHAR(80) NOT NULL,
    source_ref VARCHAR(255) NOT NULL,
    approved_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT model_operation_production_approvals_deployment_fk
        FOREIGN KEY (deployment_id,connection_id)
        REFERENCES model_deployments(id,connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_production_approvals_customer_deployment_fk
        FOREIGN KEY (customer_org_id,deployment_id)
        REFERENCES model_deployments(customer_org_id,id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_production_approvals_operation_fk
        FOREIGN KEY (deployment_id,operation_id)
        REFERENCES model_deployment_operations(deployment_id,operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_production_approvals_credential_fk
        FOREIGN KEY (credential_binding_id,connection_id)
        REFERENCES provider_credential_bindings(id,connection_id) ON DELETE RESTRICT,
    CONSTRAINT model_operation_production_approvals_support_fk
        FOREIGN KEY (
            operation_id,public_path,protocol_profile_id,protocol_profile_version,
            executor_contract_version
        ) REFERENCES model_operation_execution_support_bindings (
            operation_id,public_path,protocol_profile_id,protocol_profile_version,
            executor_contract_version
        ) ON DELETE RESTRICT,
    CONSTRAINT model_operation_production_approvals_identity_check CHECK (
        operation_id IN ('openai.responses.v1','anthropic.messages.v1')
        AND public_path=BTRIM(public_path)
        AND protocol_profile_id=BTRIM(protocol_profile_id)
        AND protocol_profile_version=BTRIM(protocol_profile_version)
        AND executor_contract_version=BTRIM(executor_contract_version)
        AND model_operation_https_origin(https_origin) IS NOT NULL
        AND https_origin=model_operation_https_origin(https_origin)
        AND connection_revision>0 AND deployment_revision>0
        AND probe_operation_revision>0 AND source_operation_revision>0
        AND approved_operation_revision=source_operation_revision+1
        AND probe_operation_revision+1=source_operation_revision
        AND credential_binding_id>0 AND credential_state_revision>0
        AND credential_link_created_at<=approved_at
        AND customer_org_id>0 AND approved_by>0 AND approved_by_org_id>0
        AND reason=BTRIM(reason) AND BTRIM(reason)<>''
        AND reason !~ E'[\\x00\\r\\n]'
        AND source_kind='admin.model_operation_production_approval.v1'
        AND source_ref='probe:' || probe_id::TEXT
    )
);

CREATE INDEX model_operation_production_approvals_operation_idx
    ON model_operation_production_approvals(
        deployment_id,operation_id,approved_at DESC,approval_id DESC
    );

CREATE FUNCTION validate_model_operation_production_approval()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof RECORD;
    actor RECORD;
    root_customer_count BIGINT;
    root_customer_id BIGINT;
BEGIN
    -- Prevent both request-path and direct-SQL approvals from consuming a
    -- customer context that ceased to be the unique active root authority.
    -- The table lock also closes insert/update races until this transaction
    -- either appends the receipt or rolls back.
    LOCK TABLE orgs IN SHARE MODE;
    SELECT COUNT(*),MIN(id)
      INTO root_customer_count,root_customer_id
      FROM orgs
     WHERE status='active' AND org_type='customer' AND parent_id IS NULL;
    IF root_customer_count<>1 OR root_customer_id IS DISTINCT FROM NEW.customer_org_id THEN
        RAISE EXCEPTION 'production approval customer root authority is stale or ambiguous'
            USING ERRCODE='23514';
    END IF;

    SELECT deployment.customer_org_id,probe.deployment_id,probe.connection_id,probe.operation_id,
           probe.success,probe.source_kind AS probe_source_kind,
           probe.connection_revision,probe.deployment_revision,
           probe.operation_revision AS probe_operation_revision,
           probe.credential_binding_id,probe.credential_state_revision,probe.tested_at,
           probe.probe_profile_id,probe.probe_profile_version,
           probe.public_path,probe.executor_contract_version,probe.https_origin,
           operation.support_state,operation.evidence_state,operation.evidence_ref,
           operation.revision AS operation_revision,
           operation.protocol_profile_id,operation.protocol_profile_version,
           deployment.admin_state AS deployment_state,
           deployment.config_revision AS current_deployment_revision,
           connection.admin_state AS connection_state,
           connection.config_revision AS current_connection_revision,
           model_operation_https_origin(connection.base_url) AS current_https_origin,
           binding.status AS credential_status,binding.key_enc AS credential_key_enc,
           binding.state_revision AS current_credential_state_revision,
           binding.cooldown_until AS credential_cooldown_until,
           credential_link.created_at AS credential_link_created_at,
           support.public_path AS support_public_path,
           support.executor_contract_version AS support_executor_contract_version
      INTO proof
      FROM model_deployment_probes probe
      JOIN model_deployment_operations operation
        ON operation.deployment_id=probe.deployment_id
       AND operation.operation_id=probe.operation_id
      JOIN model_deployments deployment ON deployment.id=probe.deployment_id
      JOIN provider_connections connection ON connection.id=deployment.connection_id
      JOIN provider_credential_bindings binding
        ON binding.id=probe.credential_binding_id
       AND binding.connection_id=probe.connection_id
      JOIN credential_binding_deployments credential_link
        ON credential_link.binding_id=probe.credential_binding_id
       AND credential_link.deployment_id=probe.deployment_id
       AND credential_link.connection_id=probe.connection_id
      JOIN model_operation_current_execution_support_bindings support
        ON support.operation_id=operation.operation_id
       AND support.protocol_profile_id=operation.protocol_profile_id
       AND support.protocol_profile_version=operation.protocol_profile_version
     WHERE probe.probe_id=NEW.probe_id
     FOR UPDATE OF probe,operation,deployment,connection,binding,credential_link;

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
        RAISE EXCEPTION 'production approval requires a current platform administrator'
            USING ERRCODE='42501';
    END IF;

    IF proof.deployment_id IS NULL
       OR NOT proof.success
       OR proof.probe_source_kind<>'admin.endpointprotocol_live_probe.v1'
       OR proof.support_state<>'enabled'
       OR proof.evidence_state<>'live_probed'
       OR proof.evidence_ref IS DISTINCT FROM 'probe:' || NEW.probe_id::TEXT
       OR proof.deployment_state<>'enabled' OR proof.connection_state<>'enabled'
       OR proof.current_https_origin IS NULL
       OR proof.credential_status<>'active' OR proof.credential_key_enc=''
       OR proof.current_credential_state_revision IS DISTINCT FROM proof.credential_state_revision
       OR (proof.credential_cooldown_until IS NOT NULL
           AND proof.credential_cooldown_until>transaction_timestamp())
       OR proof.credential_link_created_at>proof.tested_at
       OR proof.operation_revision IS DISTINCT FROM proof.probe_operation_revision+1
       OR NEW.customer_org_id IS DISTINCT FROM proof.customer_org_id
       OR NEW.deployment_id IS DISTINCT FROM proof.deployment_id
       OR NEW.connection_id IS DISTINCT FROM proof.connection_id
       OR NEW.operation_id IS DISTINCT FROM proof.operation_id
       OR NEW.credential_binding_id IS DISTINCT FROM proof.credential_binding_id
       OR NEW.credential_state_revision IS DISTINCT FROM proof.credential_state_revision
       OR NEW.credential_link_created_at IS DISTINCT FROM proof.credential_link_created_at
       OR NEW.public_path IS DISTINCT FROM proof.public_path
       OR NEW.protocol_profile_id IS DISTINCT FROM proof.probe_profile_id
       OR NEW.protocol_profile_version IS DISTINCT FROM proof.probe_profile_version
       OR NEW.executor_contract_version IS DISTINCT FROM proof.executor_contract_version
       OR NEW.https_origin IS DISTINCT FROM proof.https_origin
       OR NEW.connection_revision IS DISTINCT FROM proof.connection_revision
       OR NEW.deployment_revision IS DISTINCT FROM proof.deployment_revision
       OR NEW.probe_operation_revision IS DISTINCT FROM proof.probe_operation_revision
       OR NEW.source_operation_revision IS DISTINCT FROM proof.operation_revision
       OR NEW.approved_operation_revision IS DISTINCT FROM proof.operation_revision+1
       OR proof.connection_revision IS DISTINCT FROM proof.current_connection_revision
       OR proof.deployment_revision IS DISTINCT FROM proof.current_deployment_revision
       OR proof.https_origin IS DISTINCT FROM proof.current_https_origin
       OR proof.public_path IS DISTINCT FROM proof.support_public_path
       OR proof.executor_contract_version IS DISTINCT FROM proof.support_executor_contract_version
       OR proof.probe_profile_id IS DISTINCT FROM proof.protocol_profile_id
       OR proof.probe_profile_version IS DISTINCT FROM proof.protocol_profile_version
       OR NEW.source_kind<>'admin.model_operation_production_approval.v1'
       OR NEW.source_ref IS DISTINCT FROM 'probe:' || NEW.probe_id::TEXT THEN
        RAISE EXCEPTION 'production approval proof is stale, incomplete, or mismatched'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_operation_production_approvals_validate
BEFORE INSERT ON model_operation_production_approvals
FOR EACH ROW EXECUTE FUNCTION validate_model_operation_production_approval();

CREATE FUNCTION apply_model_operation_production_approval()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    updated_rows INTEGER;
BEGIN
    UPDATE model_deployment_operations
       SET evidence_state='production_approved',
           evidence_ref='approval:' || NEW.approval_id::TEXT,
           verified_at=NEW.approved_at,
           revision=NEW.approved_operation_revision,
           updated_at=NOW()
     WHERE deployment_id=NEW.deployment_id
       AND operation_id=NEW.operation_id
       AND revision=NEW.source_operation_revision
       AND support_state='enabled'
       AND evidence_state='live_probed'
       AND evidence_ref='probe:' || NEW.probe_id::TEXT;
    GET DIAGNOSTICS updated_rows=ROW_COUNT;
    IF updated_rows<>1 THEN
        RAISE EXCEPTION 'production approval lost its operation revision fence'
            USING ERRCODE='40001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_operation_production_approvals_apply
AFTER INSERT ON model_operation_production_approvals
FOR EACH ROW EXECUTE FUNCTION apply_model_operation_production_approval();

CREATE FUNCTION reject_model_operation_production_approval_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model operation production approvals are append-only'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_operation_production_approvals_append_only
BEFORE UPDATE OR DELETE OR TRUNCATE ON model_operation_production_approvals
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_production_approval_mutation();

CREATE FUNCTION enforce_qualified_model_operation_evidence_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    support RECORD;
    probe RECORD;
    approval RECORD;
BEGIN
    IF NEW.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1') THEN
        RETURN NEW;
    END IF;

    SELECT * INTO support
      FROM model_operation_current_execution_support_bindings current_support
     WHERE current_support.operation_id=NEW.operation_id
       AND current_support.protocol_profile_id=NEW.protocol_profile_id
       AND current_support.protocol_profile_version=NEW.protocol_profile_version;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Responses/Messages operation lacks the current immutable support binding'
            USING ERRCODE='23514';
    END IF;

    IF TG_OP='INSERT' THEN
        IF NEW.evidence_state IN ('live_probed','production_approved') THEN
            RAISE EXCEPTION 'Responses/Messages operation cannot be created with production evidence'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.evidence_state='live_probed'
       AND (OLD.evidence_state IS DISTINCT FROM NEW.evidence_state
            OR OLD.evidence_ref IS DISTINCT FROM NEW.evidence_ref) THEN
        IF NEW.evidence_ref !~ '^probe:[1-9][0-9]*$' THEN
            RAISE EXCEPTION 'live_probed transition requires an exact probe receipt'
                USING ERRCODE='23514';
        END IF;
        SELECT p.*,deployment.config_revision AS current_deployment_revision,
               connection.config_revision AS current_connection_revision,
               model_operation_https_origin(connection.base_url) AS current_https_origin
          INTO probe
          FROM model_deployment_probes p
          JOIN model_deployments deployment ON deployment.id=p.deployment_id
          JOIN provider_connections connection ON connection.id=deployment.connection_id
         WHERE p.probe_id=SUBSTRING(NEW.evidence_ref FROM 7)::BIGINT
           AND p.deployment_id=NEW.deployment_id
           AND p.operation_id=NEW.operation_id;
        IF NOT FOUND OR NOT probe.success
           OR probe.source_kind<>'admin.endpointprotocol_live_probe.v1'
           OR probe.operation_revision IS DISTINCT FROM OLD.revision
           OR probe.connection_revision IS DISTINCT FROM probe.current_connection_revision
           OR probe.deployment_revision IS DISTINCT FROM probe.current_deployment_revision
           OR probe.probe_profile_id IS DISTINCT FROM NEW.protocol_profile_id
           OR probe.probe_profile_version IS DISTINCT FROM NEW.protocol_profile_version
           OR probe.public_path IS DISTINCT FROM support.public_path
           OR probe.executor_contract_version IS DISTINCT FROM support.executor_contract_version
           OR probe.https_origin IS DISTINCT FROM probe.current_https_origin
           OR NEW.revision IS DISTINCT FROM OLD.revision+1 THEN
            RAISE EXCEPTION 'live_probed transition proof is stale or mismatched'
                USING ERRCODE='23514';
        END IF;
    END IF;

    IF NEW.evidence_state='production_approved'
       AND (OLD.evidence_state IS DISTINCT FROM NEW.evidence_state
            OR OLD.evidence_ref IS DISTINCT FROM NEW.evidence_ref) THEN
        IF NEW.evidence_ref !~ '^approval:[1-9][0-9]*$' THEN
            RAISE EXCEPTION 'production_approved transition requires an approval receipt'
                USING ERRCODE='23514';
        END IF;
        SELECT * INTO approval
          FROM model_operation_production_approvals a
         WHERE a.approval_id=SUBSTRING(NEW.evidence_ref FROM 10)::BIGINT
           AND a.deployment_id=NEW.deployment_id
           AND a.operation_id=NEW.operation_id;
        IF NOT FOUND
           OR approval.source_operation_revision IS DISTINCT FROM OLD.revision
           OR approval.approved_operation_revision IS DISTINCT FROM NEW.revision
           OR approval.public_path IS DISTINCT FROM support.public_path
           OR approval.protocol_profile_id IS DISTINCT FROM NEW.protocol_profile_id
           OR approval.protocol_profile_version IS DISTINCT FROM NEW.protocol_profile_version
           OR approval.executor_contract_version IS DISTINCT FROM support.executor_contract_version
           OR NEW.revision IS DISTINCT FROM OLD.revision+1 THEN
            RAISE EXCEPTION 'production_approved transition proof is stale or mismatched'
                USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_operations_p3_evidence_guard
BEFORE INSERT OR UPDATE ON model_deployment_operations
FOR EACH ROW EXECUTE FUNCTION enforce_qualified_model_operation_evidence_transition();

CREATE VIEW model_operation_execution_qualifications AS
SELECT operation.deployment_id,deployment.connection_id,operation.operation_id,
       support.public_path,operation.protocol_profile_id,
       operation.protocol_profile_version,support.executor_contract_version,
       approval.https_origin,approval.approval_id,approval.probe_id,
       approval.credential_binding_id,approval.credential_state_revision,
       approval.credential_link_created_at,
       approval.connection_revision AS approved_connection_revision,
       approval.deployment_revision AS approved_deployment_revision,
       approval.approved_operation_revision,
       connection.config_revision AS current_connection_revision,
       deployment.config_revision AS current_deployment_revision,
       operation.revision AS current_operation_revision,
       CASE
         WHEN connection.admin_state<>'enabled' THEN 'connection_disabled'
         WHEN deployment.admin_state<>'enabled' THEN 'deployment_disabled'
         WHEN operation.support_state<>'enabled' THEN 'operation_unsupported'
         WHEN support.operation_id IS NULL THEN 'compiled_support_binding_missing'
         WHEN operation.evidence_state<>'production_approved' THEN 'production_approval_missing'
         WHEN approval.approval_id IS NULL THEN 'production_approval_receipt_missing'
         WHEN approved_probe.probe_id IS NULL THEN 'approved_probe_missing'
         WHEN NOT approved_probe.success
           OR approved_probe.source_kind<>'admin.endpointprotocol_live_probe.v1'
           OR approved_probe.credential_binding_id IS DISTINCT FROM approval.credential_binding_id
           OR approved_probe.credential_state_revision IS DISTINCT FROM approval.credential_state_revision
           OR approved_probe.connection_revision IS DISTINCT FROM approval.connection_revision
           OR approved_probe.deployment_revision IS DISTINCT FROM approval.deployment_revision
           OR approved_probe.operation_revision IS DISTINCT FROM approval.probe_operation_revision
           OR approved_probe.public_path IS DISTINCT FROM approval.public_path
           OR approved_probe.probe_profile_id IS DISTINCT FROM approval.protocol_profile_id
           OR approved_probe.probe_profile_version IS DISTINCT FROM approval.protocol_profile_version
           OR approved_probe.executor_contract_version IS DISTINCT FROM approval.executor_contract_version
           OR approved_probe.https_origin IS DISTINCT FROM approval.https_origin
           OR approval.credential_link_created_at>approved_probe.tested_at
           THEN 'approved_probe_identity_stale'
         WHEN approval.connection_revision<>connection.config_revision THEN 'connection_revision_stale'
         WHEN approval.deployment_revision<>deployment.config_revision THEN 'deployment_revision_stale'
         WHEN approval.approved_operation_revision<>operation.revision THEN 'operation_revision_stale'
         WHEN approval.protocol_profile_id<>operation.protocol_profile_id
           OR approval.protocol_profile_version<>operation.protocol_profile_version THEN 'protocol_profile_stale'
         WHEN approval.public_path<>support.public_path
           OR approval.executor_contract_version<>support.executor_contract_version THEN 'execution_support_binding_stale'
         WHEN approval.https_origin IS DISTINCT FROM model_operation_https_origin(connection.base_url)
           THEN 'https_origin_stale'
         ELSE 'production_qualified'
       END AS qualification_state,
       (
         connection.admin_state='enabled' AND deployment.admin_state='enabled'
         AND operation.support_state='enabled' AND support.operation_id IS NOT NULL
         AND operation.evidence_state='production_approved'
         AND approval.approval_id IS NOT NULL
         AND approved_probe.probe_id IS NOT NULL AND approved_probe.success
         AND approved_probe.source_kind='admin.endpointprotocol_live_probe.v1'
         AND approved_probe.credential_binding_id=approval.credential_binding_id
         AND approved_probe.credential_state_revision=approval.credential_state_revision
         AND approved_probe.connection_revision=approval.connection_revision
         AND approved_probe.deployment_revision=approval.deployment_revision
         AND approved_probe.operation_revision=approval.probe_operation_revision
         AND approved_probe.public_path=approval.public_path
         AND approved_probe.probe_profile_id=approval.protocol_profile_id
         AND approved_probe.probe_profile_version=approval.protocol_profile_version
         AND approved_probe.executor_contract_version=approval.executor_contract_version
         AND approved_probe.https_origin=approval.https_origin
         AND approval.credential_link_created_at<=approved_probe.tested_at
         AND approval.connection_revision=connection.config_revision
         AND approval.deployment_revision=deployment.config_revision
         AND approval.approved_operation_revision=operation.revision
         AND approval.protocol_profile_id=operation.protocol_profile_id
         AND approval.protocol_profile_version=operation.protocol_profile_version
         AND approval.public_path=support.public_path
         AND approval.executor_contract_version=support.executor_contract_version
         AND approval.https_origin IS NOT DISTINCT FROM model_operation_https_origin(connection.base_url)
       ) AS qualified
  FROM model_deployment_operations operation
  JOIN model_deployments deployment ON deployment.id=operation.deployment_id
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  LEFT JOIN model_operation_current_execution_support_bindings support
    ON support.operation_id=operation.operation_id
   AND support.protocol_profile_id=operation.protocol_profile_id
   AND support.protocol_profile_version=operation.protocol_profile_version
  LEFT JOIN model_operation_production_approvals approval
    ON operation.evidence_ref='approval:' || approval.approval_id::TEXT
   AND approval.deployment_id=operation.deployment_id
   AND approval.operation_id=operation.operation_id
  LEFT JOIN model_deployment_probes approved_probe
    ON approved_probe.probe_id=approval.probe_id
   AND approved_probe.deployment_id=approval.deployment_id
   AND approved_probe.connection_id=approval.connection_id
   AND approved_probe.operation_id=approval.operation_id
 WHERE operation.operation_id IN ('openai.responses.v1','anthropic.messages.v1');

ALTER TABLE runtime_route_revision_items
    ADD COLUMN public_path VARCHAR(255),
    ADD COLUMN executor_contract_version VARCHAR(100),
    ADD CONSTRAINT runtime_route_revision_items_execution_binding_shape_check CHECK (
        (public_path IS NULL AND executor_contract_version IS NULL)
        OR
        (public_path IS NOT NULL AND executor_contract_version IS NOT NULL
         AND public_path=BTRIM(public_path) AND public_path ~ '^/v1/[a-z][a-z0-9_/-]*$'
         AND executor_contract_version=BTRIM(executor_contract_version)
         AND BTRIM(executor_contract_version)<>'')
    );

CREATE FUNCTION enforce_runtime_route_item_production_qualification()
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

CREATE TRIGGER runtime_route_revision_items_p3_qualification_guard
BEFORE INSERT OR UPDATE OR DELETE ON runtime_route_revision_items
FOR EACH ROW EXECUTE FUNCTION enforce_runtime_route_item_production_qualification();

CREATE FUNCTION enforce_runtime_release_p3_qualification()
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

CREATE TRIGGER runtime_change_releases_p3_qualification_guard
BEFORE UPDATE ON runtime_change_releases
FOR EACH ROW EXECUTE FUNCTION enforce_runtime_release_p3_qualification();

COMMENT ON TABLE model_operation_execution_support_bindings IS
    'Migration-owned immutable support bindings. Rows describe compiled capability only and are not production approval.';
COMMENT ON TABLE model_operation_production_approvals IS
    'Append-only platform-admin approval receipts bound to one exact successful endpointprotocol network probe and exact revisions.';
COMMENT ON VIEW model_operation_execution_qualifications IS
    'Sole current SQL truth for Responses/Messages production execution qualification. Local contract fixtures never satisfy this view.';

COMMIT;
