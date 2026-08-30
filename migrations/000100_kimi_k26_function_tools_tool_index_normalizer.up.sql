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

-- A live Kimi K2.6 Function Tools probe returned HTTP 200 with the optional
-- non-stream choices[].message.tool_calls[].index member. Runtime and Probe v4
-- now consume the same exact-scoped normalizer, which accepts only a lexical
-- non-negative JSON integer and removes it before enforcing the unchanged
-- canonical Chat v5 delivery contract. Streaming indexes remain untouched;
-- unknown siblings stay fail closed. Profile, Chat protocol, Release, alias,
-- pricing, Planner, UI and canonical traffic are unchanged.
INSERT INTO model_capability_probe_contract_bindings (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,
    probe_contract_id,probe_contract_version,expected_provider,
    expected_upstream_model,public_path,introduced_migration
)
SELECT 'openai.chat_completions.v1',capability.capability_id,
       'kimi.k2.6.api','1.7.0','protocol.kimi-openai-chat.v5','1.0.0',
       'unirouter.capability-probe.kimi-chat-function-tools.v1','4.0.0',
       'kimi','kimi-k2.6','/v1/chat/completions',
       '000100_kimi_k26_function_tools_tool_index_normalizer'
  FROM (VALUES
      ('tools'),
      ('function_tools'),
      ('tool_choice_auto')
  ) AS capability(capability_id);

UPDATE model_capability_current_probe_contracts current_contract
   SET probe_contract_version='4.0.0',updated_at=NOW()
 WHERE current_contract.operation_id='openai.chat_completions.v1'
   AND current_contract.capability_id IN ('tools','function_tools','tool_choice_auto')
   AND current_contract.execution_profile_id='kimi.k2.6.api'
   AND current_contract.execution_profile_version='1.7.0'
   AND current_contract.protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND current_contract.protocol_profile_version='1.0.0'
   AND current_contract.probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1'
   AND current_contract.probe_contract_version='3.0.0';

DO $$
BEGIN
    IF (SELECT COUNT(*) FROM model_capability_probe_contract_bindings
         WHERE introduced_migration='000100_kimi_k26_function_tools_tool_index_normalizer') <> 3 THEN
        RAISE EXCEPTION 'Kimi K2.6 Function Tools probe v4 binding installation is incomplete';
    END IF;
    IF (SELECT COUNT(*) FROM model_capability_current_probe_contracts
         WHERE operation_id='openai.chat_completions.v1'
           AND capability_id IN ('tools','function_tools','tool_choice_auto')
           AND execution_profile_id='kimi.k2.6.api'
           AND execution_profile_version='1.7.0'
           AND protocol_profile_id='protocol.kimi-openai-chat.v5'
           AND protocol_profile_version='1.0.0'
           AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1'
           AND probe_contract_version='4.0.0') <> 3 THEN
        RAISE EXCEPTION 'Kimi K2.6 Function Tools probe v4 current-contract promotion is incomplete';
    END IF;
END;
$$;

UPDATE model_deployment_capabilities capability
   SET evidence_ref='capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0',
       revision=capability.revision+1
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
