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
         WHERE version>'000095_kimi_k26_chat_core_qualification'
    ) THEN
        RAISE EXCEPTION 'cannot remove M95 while later migrations exist'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_capability_probes
         WHERE execution_profile_id='kimi.k2.6.api'
           AND execution_profile_version='1.7.0'
           AND protocol_profile_id='protocol.kimi-openai-chat.v5'
           AND protocol_profile_version='1.0.0'
           AND operation_id='openai.chat_completions.v1'
    ) OR EXISTS (
        SELECT 1 FROM model_capability_routing_approvals
         WHERE execution_profile_id='kimi.k2.6.api'
           AND execution_profile_version='1.7.0'
           AND protocol_profile_id='protocol.kimi-openai-chat.v5'
           AND protocol_profile_version='1.0.0'
           AND operation_id='openai.chat_completions.v1'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade Kimi K2.6 qualification with retained evidence'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM model_deployment_operations operation
          JOIN model_deployments deployment ON deployment.id=operation.deployment_id
          JOIN model_registry model ON model.id=deployment.public_model_id
         WHERE operation.operation_id='openai.chat_completions.v1'
           AND operation.protocol_profile_id='protocol.kimi-openai-chat.v5'
           AND operation.protocol_profile_version='1.0.0'
           AND model.execution_profile_id='kimi.k2.6.api'
           AND model.execution_profile_version='1.7.0'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade Kimi K2.6 qualification with retained exact Chat attachment'
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

-- Exact attachments are rejected above, so only migration-owned orphan-free
-- declarations can remain here. Preserve every foreign or generic row.
DELETE FROM model_deployment_capabilities capability
 USING model_deployments deployment, model_registry model,
       model_deployment_operations operation
 WHERE capability.deployment_id=deployment.id
   AND model.id=deployment.public_model_id
   AND operation.deployment_id=deployment.id
   AND operation.operation_id=capability.operation_id
   AND capability.operation_id='openai.chat_completions.v1'
   AND model.execution_profile_id='kimi.k2.6.api'
   AND model.execution_profile_version='1.7.0'
   AND operation.protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND operation.protocol_profile_version='1.0.0'
   AND capability.support_state='enabled'
   AND capability.evidence_state='declared'
   AND capability.evidence_ref IN (
       'capability_contract:unirouter.capability-probe.kimi-chat-sse.v1@1.0.0',
       'capability_contract:unirouter.capability-probe.kimi-chat-reasoning.v1@1.0.0',
       'capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@1.0.0',
       'capability_contract:unirouter.capability-probe.kimi-chat-json-object.v1@1.0.0'
   );

DELETE FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1'
   AND execution_profile_id='kimi.k2.6.api'
   AND execution_profile_version='1.7.0'
   AND protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND protocol_profile_version='1.0.0';

DELETE FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000095_kimi_k26_chat_core_qualification';

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
 WHERE version='000095_kimi_k26_chat_core_qualification';

COMMIT;
