BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities, model_deployment_operations,
    model_deployments, provider_connections, model_registry,
    schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;
DROP TRIGGER model_deployment_capabilities_receipt_projection_guard
    ON model_deployment_capabilities;

-- Pro reuses the reviewed DeepSeek v4 Chat probe grammars, but has no retained
-- generic Chat attachment. Publish only the exact v4 operation identity.
INSERT INTO model_capability_probe_contract_bindings (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version,expected_provider,
    expected_upstream_model,public_path,introduced_migration
)
SELECT 'openai.chat_completions.v1',capability.capability_id,
       'deepseek.v4-pro.api','1.5.0','protocol.deepseek-openai-chat.v4','1.0.0',
       capability.probe_contract_id,capability.probe_contract_version,
       'deepseek','deepseek-v4-pro','/v1/chat/completions',
       '000094_deepseek_v4_pro_chat_core_qualification'
  FROM (VALUES
      ('streaming','unirouter.capability-probe.deepseek-chat-sse.v1','2.0.0'),
      ('reasoning','unirouter.capability-probe.deepseek-chat-reasoning.v1','1.0.0'),
      ('reasoning_effort','unirouter.capability-probe.deepseek-chat-reasoning-effort.v1','1.0.0'),
      ('tools','unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0'),
      ('function_tools','unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0'),
      ('tool_choice_auto','unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0'),
      ('tool_choice_named','unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0'),
      ('tool_choice_required','unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0'),
      ('json_object','unirouter.capability-probe.deepseek-chat-json-object.v1','1.0.0')
  ) AS capability(capability_id,probe_contract_id,probe_contract_version);

INSERT INTO model_capability_current_probe_contracts (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version
)
SELECT operation_id,capability_id,execution_profile_id,execution_profile_version,
       protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version
  FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000094_deepseek_v4_pro_chat_core_qualification';

DO $$
BEGIN
    IF (SELECT COUNT(*) FROM model_capability_probe_contract_bindings
         WHERE introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') <> 9 THEN
        RAISE EXCEPTION 'DeepSeek V4 Pro capability binding installation is incomplete';
    END IF;
    IF (SELECT COUNT(*) FROM model_capability_current_probe_contracts
         WHERE operation_id='openai.chat_completions.v1'
           AND execution_profile_id='deepseek.v4-pro.api'
           AND execution_profile_version='1.5.0'
           AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
           AND protocol_profile_version='1.0.0') <> 9 THEN
        RAISE EXCEPTION 'DeepSeek V4 Pro current capability contracts are incomplete';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM model_deployment_capabilities capability
          JOIN model_deployments deployment ON deployment.id=capability.deployment_id
          JOIN model_registry model ON model.id=deployment.public_model_id
          JOIN model_deployment_operations operation
            ON operation.deployment_id=capability.deployment_id
           AND operation.operation_id=capability.operation_id
         WHERE capability.operation_id='openai.chat_completions.v1'
           AND model.execution_profile_id='deepseek.v4-pro.api'
           AND model.execution_profile_version='1.5.0'
           AND operation.protocol_profile_id='protocol.deepseek-openai-chat.v4'
           AND operation.protocol_profile_version='1.0.0'
           AND capability.evidence_state IN ('live_probed','production_approved')
    ) THEN
        RAISE EXCEPTION 'pre-authority DeepSeek V4 Pro capability evidence exists'
            USING ERRCODE='23514';
    END IF;
END;
$$;

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
 WHERE current_contract.execution_profile_id='deepseek.v4-pro.api'
   AND current_contract.execution_profile_version='1.5.0'
   AND current_contract.protocol_profile_id='protocol.deepseek-openai-chat.v4'
   AND current_contract.protocol_profile_version='1.0.0'
   AND connection.provider='deepseek'
   AND deployment.upstream_model='deepseek-v4-pro'
ON CONFLICT (deployment_id,operation_id,capability_id) DO NOTHING;

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
