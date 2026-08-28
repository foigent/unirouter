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

-- M93 changes only the interpretation contract for the five function-tool
-- capabilities already published by Profile 1.5. The model Profile, operation
-- protocols, Releases and aliases remain unchanged and historical v1 receipts
-- remain immutable.
INSERT INTO model_capability_probe_contract_bindings (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version,expected_provider,
    expected_upstream_model,public_path,introduced_migration
)
SELECT 'openai.chat_completions.v1',capability.capability_id,
       'deepseek.v4-flash.api','1.5.0',protocol.protocol_profile_id,'1.0.0',
       'unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0',
       'deepseek','deepseek-v4-flash','/v1/chat/completions',
       '000093_deepseek_function_tool_probe_contract_v2'
  FROM (VALUES
      ('tools'),
      ('function_tools'),
      ('tool_choice_auto'),
      ('tool_choice_named'),
      ('tool_choice_required')
  ) AS capability(capability_id)
 CROSS JOIN (VALUES
      ('protocol.deepseek-openai-chat.v4'),
      ('openai.chat_completions')
  ) AS protocol(protocol_profile_id);

UPDATE model_capability_current_probe_contracts
   SET probe_contract_version='2.0.0',updated_at=NOW()
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id IN (
       'tools','function_tools','tool_choice_auto','tool_choice_named','tool_choice_required'
   )
   AND execution_profile_id='deepseek.v4-flash.api'
   AND execution_profile_version='1.5.0'
   AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1'
   AND probe_contract_version='1.0.0';

DO $$
BEGIN
    IF (SELECT COUNT(*) FROM model_capability_probe_contract_bindings
         WHERE introduced_migration='000093_deepseek_function_tool_probe_contract_v2') <> 10 THEN
        RAISE EXCEPTION 'DeepSeek function-tool probe v2 binding installation is incomplete';
    END IF;
    IF (SELECT COUNT(*) FROM model_capability_current_probe_contracts
         WHERE operation_id='openai.chat_completions.v1'
           AND capability_id IN (
               'tools','function_tools','tool_choice_auto','tool_choice_named','tool_choice_required'
           )
           AND execution_profile_id='deepseek.v4-flash.api'
           AND execution_profile_version='1.5.0'
           AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
           AND protocol_profile_version='1.0.0'
           AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1'
           AND probe_contract_version='2.0.0') <> 10 THEN
        RAISE EXCEPTION 'DeepSeek function-tool probe v2 current-contract promotion is incomplete';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_capability_current_probe_contracts
         WHERE operation_id='openai.chat_completions.v1'
           AND capability_id IN (
               'tools','function_tools','tool_choice_auto','tool_choice_named','tool_choice_required'
           )
           AND execution_profile_id='deepseek.v4-flash.api'
           AND execution_profile_version='1.5.0'
           AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
           AND protocol_profile_version='1.0.0'
           AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1'
           AND probe_contract_version='1.0.0'
    ) THEN
        RAISE EXCEPTION 'DeepSeek function-tool probe v1 remains current after M93';
    END IF;
END;
$$;

-- Only declared contract projections move to v2. Existing live or approved
-- evidence is never rewritten; the qualification resolver observes its v1
-- contract as stale until an administrator runs and approves a v2 probe.
UPDATE model_deployment_capabilities capability
   SET evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0',
       revision=capability.revision+1
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id
 WHERE capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.operation_id='openai.chat_completions.v1'
   AND capability.capability_id IN (
       'tools','function_tools','tool_choice_auto','tool_choice_named','tool_choice_required'
   )
   AND capability.support_state='enabled'
   AND capability.evidence_state='declared'
   AND capability.evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@1.0.0'
   AND model.execution_profile_id='deepseek.v4-flash.api'
   AND model.execution_profile_version='1.5.0'
   AND operation.protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND operation.protocol_profile_version='1.0.0'
   AND connection.provider='deepseek'
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
