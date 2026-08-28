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
         WHERE version>'000094_deepseek_v4_pro_chat_core_qualification'
    ) THEN
        RAISE EXCEPTION 'cannot remove M94 while later migrations exist'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_capability_probes
         WHERE execution_profile_id='deepseek.v4-pro.api'
           AND execution_profile_version='1.5.0'
           AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
           AND protocol_profile_version='1.0.0'
           AND operation_id='openai.chat_completions.v1'
    ) OR EXISTS (
        SELECT 1 FROM model_capability_routing_approvals
         WHERE execution_profile_id='deepseek.v4-pro.api'
           AND execution_profile_version='1.5.0'
           AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
           AND protocol_profile_version='1.0.0'
           AND operation_id='openai.chat_completions.v1'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade DeepSeek V4 Pro qualification with retained evidence'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM model_deployment_capabilities capability
          JOIN (VALUES
              ('streaming','capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0'),
              ('reasoning','capability_contract:unirouter.capability-probe.deepseek-chat-reasoning.v1@1.0.0'),
              ('reasoning_effort','capability_contract:unirouter.capability-probe.deepseek-chat-reasoning-effort.v1@1.0.0'),
              ('tools','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('function_tools','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('tool_choice_auto','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('tool_choice_named','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('tool_choice_required','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('json_object','capability_contract:unirouter.capability-probe.deepseek-chat-json-object.v1@1.0.0')
          ) AS owned(capability_id,evidence_ref)
            ON owned.capability_id=capability.capability_id
           AND owned.evidence_ref=capability.evidence_ref
          JOIN model_deployments deployment
            ON deployment.id=capability.deployment_id
           AND deployment.upstream_model='deepseek-v4-pro'
          LEFT JOIN provider_connections connection
            ON connection.id=deployment.connection_id
          LEFT JOIN model_registry model
            ON model.id=deployment.public_model_id
          LEFT JOIN model_deployment_operations operation
            ON operation.deployment_id=capability.deployment_id
           AND operation.operation_id=capability.operation_id
         WHERE capability.operation_id='openai.chat_completions.v1'
           AND (
               capability.support_state='enabled'
               AND capability.evidence_state='declared'
               AND connection.provider='deepseek'
               AND deployment.upstream_model='deepseek-v4-pro'
               AND model.execution_profile_id='deepseek.v4-pro.api'
               AND model.execution_profile_version='1.5.0'
               AND operation.protocol_profile_id='protocol.deepseek-openai-chat.v4'
               AND operation.protocol_profile_version='1.0.0'
           ) IS NOT TRUE
    ) THEN
        RAISE EXCEPTION 'cannot downgrade DeepSeek V4 Pro qualification after projection identity drift'
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
 USING (VALUES
       ('streaming','capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0'),
       ('reasoning','capability_contract:unirouter.capability-probe.deepseek-chat-reasoning.v1@1.0.0'),
       ('reasoning_effort','capability_contract:unirouter.capability-probe.deepseek-chat-reasoning-effort.v1@1.0.0'),
       ('tools','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
       ('function_tools','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
       ('tool_choice_auto','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
       ('tool_choice_named','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
       ('tool_choice_required','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
       ('json_object','capability_contract:unirouter.capability-probe.deepseek-chat-json-object.v1@1.0.0')
   ) AS owned(capability_id,evidence_ref),
   model_deployments deployment, provider_connections connection,
   model_registry model, model_deployment_operations operation
 WHERE capability.deployment_id=deployment.id
   AND connection.id=deployment.connection_id
   AND model.id=deployment.public_model_id
   AND operation.deployment_id=deployment.id
   AND operation.operation_id=capability.operation_id
   AND capability.capability_id=owned.capability_id
   AND capability.evidence_ref=owned.evidence_ref
   AND model.execution_profile_id='deepseek.v4-pro.api'
   AND model.execution_profile_version='1.5.0'
   AND operation.protocol_profile_id='protocol.deepseek-openai-chat.v4'
   AND operation.protocol_profile_version='1.0.0'
   AND capability.operation_id='openai.chat_completions.v1'
   AND deployment.upstream_model='deepseek-v4-pro'
   AND capability.support_state='enabled'
   AND capability.evidence_state='declared';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM model_deployment_capabilities capability
          JOIN (VALUES
              ('streaming','capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0'),
              ('reasoning','capability_contract:unirouter.capability-probe.deepseek-chat-reasoning.v1@1.0.0'),
              ('reasoning_effort','capability_contract:unirouter.capability-probe.deepseek-chat-reasoning-effort.v1@1.0.0'),
              ('tools','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('function_tools','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('tool_choice_auto','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('tool_choice_named','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('tool_choice_required','capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0'),
              ('json_object','capability_contract:unirouter.capability-probe.deepseek-chat-json-object.v1@1.0.0')
          ) AS owned(capability_id,evidence_ref)
            ON owned.capability_id=capability.capability_id
           AND owned.evidence_ref=capability.evidence_ref
          JOIN model_deployments deployment
            ON deployment.id=capability.deployment_id
           AND deployment.upstream_model='deepseek-v4-pro'
         WHERE capability.operation_id='openai.chat_completions.v1'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade DeepSeek V4 Pro qualification with retained M94 capability projection'
            USING ERRCODE='55000';
    END IF;
END;
$$;

DELETE FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1'
   AND execution_profile_id='deepseek.v4-pro.api'
   AND execution_profile_version='1.5.0'
   AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
   AND protocol_profile_version='1.0.0';

DELETE FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000094_deepseek_v4_pro_chat_core_qualification';

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
 WHERE version='000094_deepseek_v4_pro_chat_core_qualification';

COMMIT;
