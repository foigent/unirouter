BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities, model_deployment_operations,
    model_deployments, provider_connections, model_registry,
    model_capability_probes, model_capability_routing_approvals,
    schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000092_deepseek_reasoning_tool_choice_contract'
    ) THEN
        RAISE EXCEPTION 'cannot remove M92 while later migrations exist'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_capability_probes
         WHERE execution_profile_id='deepseek.v4-flash.api'
           AND execution_profile_version='1.5.0'
    ) OR EXISTS (
        SELECT 1 FROM model_capability_routing_approvals
         WHERE execution_profile_id='deepseek.v4-flash.api'
           AND execution_profile_version='1.5.0'
    ) THEN
        RAISE EXCEPTION 'cannot remove M92 with retained DeepSeek 1.5 qualification evidence'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM model_registry
         WHERE (execution_profile_id, execution_profile_version) IN (
             ('deepseek.v4-flash.api', '1.5.0'),
             ('deepseek.v4-pro.api', '1.5.0')
         )
    ) THEN
        RAISE EXCEPTION 'cannot remove M92 with retained DeepSeek 1.5 model attachment'
            USING ERRCODE='55000';
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
           AND model.execution_profile_id='deepseek.v4-flash.api'
           AND model.execution_profile_version='1.5.0'
           AND operation.protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
           AND operation.protocol_profile_version='1.0.0'
           AND capability.support_state='enabled'
           AND NOT (
               capability.evidence_state='declared'
               AND capability.evidence_ref IN (
                   'capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',
                   'capability_contract:unirouter.capability-probe.deepseek-chat-reasoning.v1@1.0.0',
                   'capability_contract:unirouter.capability-probe.deepseek-chat-reasoning-effort.v1@1.0.0',
                   'capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@1.0.0',
                   'capability_contract:unirouter.capability-probe.deepseek-chat-json-object.v1@1.0.0'
               )
           )
    ) THEN
        RAISE EXCEPTION 'cannot remove M92 after DeepSeek 1.5 capability projection changed'
            USING ERRCODE='55000';
    END IF;
END;
$$;

DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;
DROP TRIGGER model_deployment_capabilities_receipt_projection_guard
    ON model_deployment_capabilities;

DELETE FROM model_deployment_capabilities capability
 USING model_deployments deployment,model_registry model,model_deployment_operations operation
 WHERE capability.deployment_id=deployment.id
   AND model.id=deployment.public_model_id
   AND operation.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.operation_id='openai.chat_completions.v1'
   AND capability.support_state='enabled'
   AND capability.evidence_state='declared'
   AND capability.evidence_ref IN (
       'capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',
       'capability_contract:unirouter.capability-probe.deepseek-chat-reasoning.v1@1.0.0',
       'capability_contract:unirouter.capability-probe.deepseek-chat-reasoning-effort.v1@1.0.0',
       'capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@1.0.0',
       'capability_contract:unirouter.capability-probe.deepseek-chat-json-object.v1@1.0.0'
   )
   AND model.execution_profile_id='deepseek.v4-flash.api'
   AND model.execution_profile_version='1.5.0'
   AND operation.protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND operation.protocol_profile_version='1.0.0';

DELETE FROM model_capability_current_probe_contracts
 WHERE execution_profile_id='deepseek.v4-flash.api'
   AND execution_profile_version='1.5.0';

DELETE FROM model_capability_probe_contract_bindings
 WHERE execution_profile_id='deepseek.v4-flash.api'
   AND execution_profile_version='1.5.0'
   AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract';

CREATE TRIGGER model_capability_probe_contract_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_contract_binding_mutation();

CREATE TRIGGER model_capability_current_probe_contracts_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_current_probe_contract_mutation();

CREATE TRIGGER model_deployment_capabilities_receipt_projection_guard
BEFORE INSERT OR UPDATE ON model_deployment_capabilities
FOR EACH ROW EXECUTE FUNCTION enforce_model_capability_evidence_projection();

DELETE FROM schema_migrations
 WHERE version='000092_deepseek_reasoning_tool_choice_contract';

COMMIT;
