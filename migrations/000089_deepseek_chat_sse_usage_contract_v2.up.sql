BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- M88 deliberately sealed both authorities against runtime mutation. A newer
-- immutable probe contract may only be added and selected by a migration.
DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;
DROP TRIGGER model_deployment_capabilities_receipt_projection_guard
    ON model_deployment_capabilities;

ALTER TABLE model_capability_probe_contract_bindings
    DROP CONSTRAINT model_capability_probe_contract_bindings_identity_check;
ALTER TABLE model_capability_probe_contract_bindings
    ADD CONSTRAINT model_capability_probe_contract_bindings_identity_check CHECK (
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
        AND introduced_migration ~ '^000[0-9]{3}_[a-z0-9_]+$'
        AND CONCAT_WS('',operation_id,capability_id,execution_profile_id,
            execution_profile_version,protocol_profile_id,protocol_profile_version,
            probe_contract_id,probe_contract_version,expected_provider,
            expected_upstream_model,public_path) !~ E'[\r\n]'
    );

INSERT INTO model_capability_probe_contract_bindings (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version,expected_provider,
    expected_upstream_model,public_path,introduced_migration
) VALUES (
    'openai.chat_completions.v1','streaming','deepseek.v4-flash.api','1.4.0',
    'protocol.deepseek-openai-chat.v4','1.0.0',
    'unirouter.capability-probe.deepseek-chat-sse.v1','2.0.0',
    'deepseek','deepseek-v4-flash','/v1/chat/completions',
    '000089_deepseek_chat_sse_usage_contract_v2'
), (
    'openai.chat_completions.v1','streaming','deepseek.v4-flash.api','1.4.0',
    'openai.chat_completions','1.0.0',
    'unirouter.capability-probe.deepseek-chat-sse.v1','2.0.0',
    'deepseek','deepseek-v4-flash','/v1/chat/completions',
    '000089_deepseek_chat_sse_usage_contract_v2'
);

UPDATE model_capability_current_probe_contracts
   SET probe_contract_version='2.0.0',updated_at=NOW()
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'
   AND execution_profile_id='deepseek.v4-flash.api'
   AND execution_profile_version='1.4.0'
   AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'
   AND probe_contract_version='1.0.0';

DO $$
BEGIN
    IF (SELECT COUNT(*) FROM model_capability_current_probe_contracts
         WHERE operation_id='openai.chat_completions.v1'
           AND capability_id='streaming'
           AND execution_profile_id='deepseek.v4-flash.api'
           AND execution_profile_version='1.4.0'
           AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
           AND protocol_profile_version='1.0.0'
           AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'
           AND probe_contract_version='2.0.0') <> 2 THEN
        RAISE EXCEPTION 'DeepSeek Chat SSE v2 current-contract promotion is incomplete';
    END IF;
END;
$$;

UPDATE model_deployment_capabilities capability
   SET evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',
       revision=capability.revision+1
  FROM model_deployments deployment
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id
 WHERE capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.operation_id='openai.chat_completions.v1'
   AND capability.capability_id='streaming'
   AND capability.support_state='enabled'
   AND capability.evidence_state='declared'
   AND capability.evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@1.0.0'
   AND model.execution_profile_id='deepseek.v4-flash.api'
   AND model.execution_profile_version='1.4.0'
   AND operation.protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND operation.protocol_profile_version='1.0.0'
   AND deployment.upstream_model='deepseek-v4-flash';

CREATE TRIGGER model_capability_probe_contract_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_contract_binding_mutation();

CREATE TRIGGER model_capability_current_probe_contracts_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_current_probe_contract_mutation();

CREATE TRIGGER model_deployment_capabilities_receipt_projection_guard
BEFORE INSERT OR UPDATE ON model_deployment_capabilities
FOR EACH ROW EXECUTE FUNCTION enforce_model_capability_evidence_projection();

COMMIT;
