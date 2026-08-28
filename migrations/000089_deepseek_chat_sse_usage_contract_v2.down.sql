BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities, model_capability_probes, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000089_deepseek_chat_sse_usage_contract_v2'
    ) THEN
        RAISE EXCEPTION 'cannot remove M89 while later migrations exist'
            USING ERRCODE='55000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_capability_probes
         WHERE probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'
           AND probe_contract_version='2.0.0'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade DeepSeek Chat SSE contract v2 with retained v2 probe evidence'
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

UPDATE model_capability_current_probe_contracts
   SET probe_contract_version='1.0.0',updated_at=NOW()
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'
   AND execution_profile_id='deepseek.v4-flash.api'
   AND execution_profile_version='1.4.0'
   AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'
   AND probe_contract_version='2.0.0';

-- Runtime revisions are monotonic, but this migration rollback must restore
-- the exact predecessor revision contributed by M89. The table lock above
-- excludes concurrent writers, and any error before re-enabling rolls back the
-- whole transaction including this trigger state.
ALTER TABLE model_deployment_capabilities
    DISABLE TRIGGER model_deployment_capabilities_revision_guard;

UPDATE model_deployment_capabilities capability
   SET evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@1.0.0',
       revision=CASE WHEN capability.revision>1 THEN capability.revision-1 ELSE 1 END
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
   AND capability.evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0'
   AND model.execution_profile_id='deepseek.v4-flash.api'
   AND model.execution_profile_version='1.4.0'
   AND operation.protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND operation.protocol_profile_version='1.0.0'
   AND deployment.upstream_model='deepseek-v4-flash';

ALTER TABLE model_deployment_capabilities
    ENABLE TRIGGER model_deployment_capabilities_revision_guard;

DELETE FROM model_capability_probe_contract_bindings
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'
   AND execution_profile_id='deepseek.v4-flash.api'
   AND execution_profile_version='1.4.0'
   AND protocol_profile_id IN ('protocol.deepseek-openai-chat.v4','openai.chat_completions')
   AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1'
   AND probe_contract_version='2.0.0'
   AND introduced_migration='000089_deepseek_chat_sse_usage_contract_v2';

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
        AND introduced_migration='000088_model_capability_execution_qualification'
        AND CONCAT_WS('',operation_id,capability_id,execution_profile_id,
            execution_profile_version,protocol_profile_id,protocol_profile_version,
            probe_contract_id,probe_contract_version,expected_provider,
            expected_upstream_model,public_path) !~ E'[\r\n]'
    );

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
 WHERE version='000089_deepseek_chat_sse_usage_contract_v2';

COMMIT;
