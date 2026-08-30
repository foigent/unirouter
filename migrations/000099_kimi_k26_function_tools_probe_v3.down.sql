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
         WHERE version>'000099_kimi_k26_function_tools_probe_v3'
    ) THEN
        RAISE EXCEPTION 'cannot remove M99 while later migrations exist'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM model_deployment_capabilities capability
          LEFT JOIN model_deployments deployment
            ON deployment.id=capability.deployment_id
          LEFT JOIN provider_connections connection
            ON connection.id=deployment.connection_id
          LEFT JOIN model_registry model
            ON model.id=deployment.public_model_id
          LEFT JOIN model_deployment_operations operation
            ON operation.deployment_id=capability.deployment_id
           AND operation.operation_id=capability.operation_id
         WHERE capability.operation_id='openai.chat_completions.v1'
           AND capability.capability_id IN ('tools','function_tools','tool_choice_auto')
           AND capability.evidence_ref='capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@3.0.0'
           AND (
               capability.support_state='enabled'
               AND capability.evidence_state='declared'
               AND connection.provider='kimi'
               AND deployment.upstream_model='kimi-k2.6'
               AND model.execution_profile_id='kimi.k2.6.api'
               AND model.execution_profile_version='1.7.0'
               AND operation.protocol_profile_id='protocol.kimi-openai-chat.v5'
               AND operation.protocol_profile_version='1.0.0'
           ) IS NOT TRUE
    ) THEN
        RAISE EXCEPTION 'cannot downgrade Kimi K2.6 Function Tools probe v3 after projection identity drift'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_capability_probes
         WHERE execution_profile_id='kimi.k2.6.api'
           AND execution_profile_version='1.7.0'
           AND protocol_profile_id='protocol.kimi-openai-chat.v5'
           AND protocol_profile_version='1.0.0'
           AND operation_id='openai.chat_completions.v1'
           AND capability_id IN ('tools','function_tools','tool_choice_auto')
           AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1'
           AND probe_contract_version='3.0.0'
    ) OR EXISTS (
        SELECT 1 FROM model_capability_routing_approvals
         WHERE execution_profile_id='kimi.k2.6.api'
           AND execution_profile_version='1.7.0'
           AND protocol_profile_id='protocol.kimi-openai-chat.v5'
           AND protocol_profile_version='1.0.0'
           AND operation_id='openai.chat_completions.v1'
           AND capability_id IN ('tools','function_tools','tool_choice_auto')
           AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1'
           AND probe_contract_version='3.0.0'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade Kimi K2.6 Function Tools probe v3 with retained v3 qualification evidence'
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

UPDATE model_capability_current_probe_contracts current_contract
   SET probe_contract_version='2.0.0',updated_at=NOW()
 WHERE current_contract.operation_id='openai.chat_completions.v1'
   AND current_contract.capability_id IN ('tools','function_tools','tool_choice_auto')
   AND current_contract.execution_profile_id='kimi.k2.6.api'
   AND current_contract.execution_profile_version='1.7.0'
   AND current_contract.protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND current_contract.protocol_profile_version='1.0.0'
   AND current_contract.probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1'
   AND current_contract.probe_contract_version='3.0.0';

ALTER TABLE model_deployment_capabilities
    DISABLE TRIGGER model_deployment_capabilities_revision_guard;

UPDATE model_deployment_capabilities capability
   SET evidence_ref='capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@2.0.0',
       revision=CASE WHEN capability.revision>1 THEN capability.revision-1 ELSE 1 END
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_registry model ON model.id=deployment.public_model_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id
 WHERE capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.operation_id='openai.chat_completions.v1'
   AND capability.capability_id IN ('tools','function_tools','tool_choice_auto')
   AND capability.support_state='enabled'
   AND capability.evidence_state='declared'
   AND capability.evidence_ref='capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@3.0.0'
   AND model.execution_profile_id='kimi.k2.6.api'
   AND model.execution_profile_version='1.7.0'
   AND operation.protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND operation.protocol_profile_version='1.0.0'
   AND connection.provider='kimi'
   AND deployment.upstream_model='kimi-k2.6';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployment_capabilities
         WHERE operation_id='openai.chat_completions.v1'
           AND capability_id IN ('tools','function_tools','tool_choice_auto')
           AND evidence_ref='capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@3.0.0'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade Kimi K2.6 Function Tools probe v3 with retained v3 capability projection';
    END IF;
END;
$$;

ALTER TABLE model_deployment_capabilities
    ENABLE TRIGGER model_deployment_capabilities_revision_guard;

DELETE FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000099_kimi_k26_function_tools_probe_v3';

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
 WHERE version='000099_kimi_k26_function_tools_probe_v3';

COMMIT;
