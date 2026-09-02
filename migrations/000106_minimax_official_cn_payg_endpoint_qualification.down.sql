-- unirouter:capability-contract-publication-v1
-- publication: 000106_minimax_official_cn_payg_endpoint_qualification
-- declaration-sha256: fbd93e5b00d50c9466d631e279f5c94360bc378a288703c36f72867b39578d5c
-- artifact: down

BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities,
    model_capability_probes,
    model_capability_routing_approvals,
    freeze_idempotency,
    billing_provider_attempts,
    model_registry,
    schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM schema_migrations WHERE version > '000106_minimax_official_cn_payg_endpoint_qualification') THEN
        RAISE EXCEPTION 'later migration prevents guarded capability publication downgrade';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM schema_migrations WHERE version='000106_minimax_official_cn_payg_endpoint_qualification') THEN
        RAISE EXCEPTION 'capability publication ledger is missing';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_registry
                WHERE execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.1.0') THEN
        RAISE EXCEPTION 'retained Profile attachment prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_registry
                WHERE execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.1.0') THEN
        RAISE EXCEPTION 'retained Profile attachment prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_registry
                WHERE execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.1.0') THEN
        RAISE EXCEPTION 'retained Profile attachment prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM freeze_idempotency WHERE execution_snapshot_hash IS NOT NULL)
       OR EXISTS (SELECT 1 FROM billing_provider_attempts WHERE execution_snapshot_hash IS NOT NULL) THEN
        RAISE EXCEPTION 'retained execution Freeze evidence prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_capability_probes
                WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
                  AND execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.1.0'
                  AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
                  AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0')
       OR EXISTS (SELECT 1 FROM model_capability_routing_approvals
                WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
                  AND execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.1.0'
                  AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
                  AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0')
       OR EXISTS (SELECT 1 FROM billing_provider_attempts
                WHERE route_capability_evidence @> jsonb_build_array(jsonb_build_object(
                    'capability_id','function_tools'::text,'evidence_ref','capability_contract:unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1@1.0.0'::text))) THEN
        RAISE EXCEPTION 'retained capability evidence prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_capability_probes
                WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
                  AND execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.1.0'
                  AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
                  AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0')
       OR EXISTS (SELECT 1 FROM model_capability_routing_approvals
                WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
                  AND execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.1.0'
                  AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
                  AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0')
       OR EXISTS (SELECT 1 FROM billing_provider_attempts
                WHERE route_capability_evidence @> jsonb_build_array(jsonb_build_object(
                    'capability_id','function_tools'::text,'evidence_ref','capability_contract:unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1@1.0.0'::text))) THEN
        RAISE EXCEPTION 'retained capability evidence prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_capability_probes
                WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
                  AND execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.1.0'
                  AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0'
                  AND probe_contract_id='unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1' AND probe_contract_version='1.0.0')
       OR EXISTS (SELECT 1 FROM model_capability_routing_approvals
                WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
                  AND execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.1.0'
                  AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0'
                  AND probe_contract_id='unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1' AND probe_contract_version='1.0.0')
       OR EXISTS (SELECT 1 FROM billing_provider_attempts
                WHERE route_capability_evidence @> jsonb_build_array(jsonb_build_object(
                    'capability_id','function_tools'::text,'evidence_ref','capability_contract:unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1@1.0.0'::text))) THEN
        RAISE EXCEPTION 'retained capability evidence prevents guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployment_capabilities capability
        JOIN model_deployments deployment ON deployment.id=capability.deployment_id
        JOIN provider_connections connection ON connection.id=deployment.connection_id
        JOIN model_registry model ON model.id=deployment.public_model_id
        JOIN model_deployment_operations operation
          ON operation.deployment_id=deployment.id
         AND operation.operation_id=capability.operation_id
       WHERE capability.operation_id='openai.chat_completions.v1' AND capability.capability_id='function_tools'
         AND capability.evidence_ref='capability_contract:unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1@1.0.0'
         AND model.execution_profile_id='minimax.m2.7-highspeed.payg' AND model.execution_profile_version='1.1.0'
         AND operation.protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND operation.protocol_profile_version='1.0.0'
         AND connection.provider='minimax' AND deployment.upstream_model='MiniMax-M2.7-highspeed'
    ) THEN
        RAISE EXCEPTION 'exact projection recovery must precede guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployment_capabilities capability
        JOIN model_deployments deployment ON deployment.id=capability.deployment_id
        JOIN provider_connections connection ON connection.id=deployment.connection_id
        JOIN model_registry model ON model.id=deployment.public_model_id
        JOIN model_deployment_operations operation
          ON operation.deployment_id=deployment.id
         AND operation.operation_id=capability.operation_id
       WHERE capability.operation_id='openai.chat_completions.v1' AND capability.capability_id='function_tools'
         AND capability.evidence_ref='capability_contract:unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1@1.0.0'
         AND model.execution_profile_id='minimax.m2.7.payg' AND model.execution_profile_version='1.1.0'
         AND operation.protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND operation.protocol_profile_version='1.0.0'
         AND connection.provider='minimax' AND deployment.upstream_model='MiniMax-M2.7'
    ) THEN
        RAISE EXCEPTION 'exact projection recovery must precede guarded publication downgrade';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployment_capabilities capability
        JOIN model_deployments deployment ON deployment.id=capability.deployment_id
        JOIN provider_connections connection ON connection.id=deployment.connection_id
        JOIN model_registry model ON model.id=deployment.public_model_id
        JOIN model_deployment_operations operation
          ON operation.deployment_id=deployment.id
         AND operation.operation_id=capability.operation_id
       WHERE capability.operation_id='openai.chat_completions.v1' AND capability.capability_id='function_tools'
         AND capability.evidence_ref='capability_contract:unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1@1.0.0'
         AND model.execution_profile_id='minimax.m3.payg' AND model.execution_profile_version='1.1.0'
         AND operation.protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND operation.protocol_profile_version='1.0.0'
         AND connection.provider='minimax' AND deployment.upstream_model='MiniMax-M3'
    ) THEN
        RAISE EXCEPTION 'exact projection recovery must precede guarded publication downgrade';
    END IF;
END;
$$;

DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;

DELETE FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
   AND execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.1.0'
   AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1' AND probe_contract_version='1.0.0';
DELETE FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
   AND execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.1.0'
   AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0';
DELETE FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
   AND execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.1.0'
   AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0';
DELETE FROM model_capability_probe_contract_bindings
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
   AND execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.1.0'
   AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1' AND probe_contract_version='1.0.0'
   AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M3'
   AND public_path='/v1/chat/completions' AND introduced_migration='000106_minimax_official_cn_payg_endpoint_qualification';
DELETE FROM model_capability_probe_contract_bindings
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
   AND execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.1.0'
   AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0'
   AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M2.7'
   AND public_path='/v1/chat/completions' AND introduced_migration='000106_minimax_official_cn_payg_endpoint_qualification';
DELETE FROM model_capability_probe_contract_bindings
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools'
   AND execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.1.0'
   AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0'
   AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M2.7-highspeed'
   AND public_path='/v1/chat/completions' AND introduced_migration='000106_minimax_official_cn_payg_endpoint_qualification';
DO $$
BEGIN
    IF (SELECT COUNT(*) FROM model_capability_probe_contract_bindings) <> 100 THEN
        RAISE EXCEPTION 'guarded downgrade final binding count mismatch';
    END IF;
    IF (SELECT COUNT(*) FROM model_capability_current_probe_contracts) <> 75 THEN
        RAISE EXCEPTION 'guarded downgrade final selector count mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5-turbo.api' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='glm' AND expected_upstream_model='glm-5-turbo' AND public_path='/v1/chat/completions' AND introduced_migration='000105_glm_toggle_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.1.api' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='glm' AND expected_upstream_model='glm-5.1' AND public_path='/v1/chat/completions' AND introduced_migration='000105_glm_toggle_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.2.api' AND execution_profile_version='1.8.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='glm' AND expected_upstream_model='glm-5.2' AND public_path='/v1/chat/completions' AND introduced_migration='000105_glm_toggle_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.3.api' AND execution_profile_version='1.8.0' AND protocol_profile_id='protocol.glm-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='glm' AND expected_upstream_model='glm-5.3' AND public_path='/v1/chat/completions' AND introduced_migration='000103_minimax_m3_glm53_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.api' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='glm' AND expected_upstream_model='glm-5' AND public_path='/v1/chat/completions' AND introduced_migration='000105_glm_toggle_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5v-turbo.api' AND execution_profile_version='1.11.0' AND protocol_profile_id='protocol.glm-openai-chat.glm5v.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='glm' AND expected_upstream_model='glm-5v-turbo' AND public_path='/v1/chat/completions' AND introduced_migration='000105_glm_toggle_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000095_kimi_k26_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000098_kimi_k26_nonstream_usage_probe_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='3.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000099_kimi_k26_function_tools_probe_v3') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000100_kimi_k26_function_tools_tool_index_normalizer') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.7-code' AND public_path='/v1/chat/completions' AND introduced_migration='000101_kimi_k27_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M2.7-highspeed' AND public_path='/v1/chat/completions' AND introduced_migration='000104_minimax_payg_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M2.7' AND public_path='/v1/chat/completions' AND introduced_migration='000104_minimax_payg_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M3' AND public_path='/v1/chat/completions' AND introduced_migration='000104_minimax_payg_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m3.token-plan' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='minimax' AND expected_upstream_model='MiniMax-M3' AND public_path='/v1/chat/completions' AND introduced_migration='000103_minimax_m3_glm53_function_tools_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000095_kimi_k26_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000098_kimi_k26_nonstream_usage_probe_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.7-code' AND public_path='/v1/chat/completions' AND introduced_migration='000101_kimi_k27_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000090_deepseek_chat_reasoning_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000090_deepseek_chat_reasoning_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000095_kimi_k26_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000098_kimi_k26_nonstream_usage_probe_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.7-code' AND public_path='/v1/chat/completions' AND introduced_migration='000101_kimi_k27_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000090_deepseek_chat_reasoning_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000090_deepseek_chat_reasoning_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000088_model_capability_execution_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000089_deepseek_chat_sse_usage_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000088_model_capability_execution_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000089_deepseek_chat_sse_usage_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000095_kimi_k26_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000096_kimi_k26_stream_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='3.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000097_kimi_k26_stream_choice_usage_probe_v3') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.7-code' AND public_path='/v1/chat/completions' AND introduced_migration='000101_kimi_k27_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000095_kimi_k26_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000098_kimi_k26_nonstream_usage_probe_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='3.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000099_kimi_k26_function_tools_probe_v3') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000100_kimi_k26_function_tools_tool_index_normalizer') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.7-code' AND public_path='/v1/chat/completions' AND introduced_migration='000101_kimi_k27_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000091_deepseek_chat_tools_json_capabilities') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000092_deepseek_reasoning_tool_choice_contract') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-flash' AND public_path='/v1/chat/completions' AND introduced_migration='000093_deepseek_function_tool_probe_contract_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='deepseek' AND expected_upstream_model='deepseek-v4-pro' AND public_path='/v1/chat/completions' AND introduced_migration='000094_deepseek_v4_pro_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='1.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000095_kimi_k26_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='2.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000098_kimi_k26_nonstream_usage_probe_v2') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='3.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000099_kimi_k26_function_tools_probe_v3') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.6' AND public_path='/v1/chat/completions' AND introduced_migration='000100_kimi_k26_function_tools_tool_index_normalizer') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k2.7-code' AND public_path='/v1/chat/completions' AND introduced_migration='000101_kimi_k27_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0' AND expected_provider='kimi' AND expected_upstream_model='kimi-k3' AND public_path='/v1/chat/completions' AND introduced_migration='000102_kimi_k3_chat_core_qualification') THEN
        RAISE EXCEPTION 'guarded downgrade final binding content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5-turbo.api' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.1.api' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.2.api' AND execution_profile_version='1.8.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.3.api' AND execution_profile_version='1.8.0' AND protocol_profile_id='protocol.glm-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5.api' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.glm-openai-chat.v6' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='glm.5v-turbo.api' AND execution_profile_version='1.11.0' AND protocol_profile_id='protocol.glm-openai-chat.glm5v.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m2.7-highspeed.payg' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m2.7.payg' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.minimax-m2-openai-chat.v7' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m3.payg' AND execution_profile_version='1.0.0' AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='function_tools' AND execution_profile_id='minimax.m3.token-plan' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.minimax-m3-openai-chat.v9' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.minimax-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-json-object.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='json_object' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-json-object.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='reasoning_effort' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-reasoning-effort.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-sse.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='3.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-sse.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_auto' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_named' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tool_choice_required' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='1.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='openai.chat_completions' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='deepseek.v4-pro.api' AND execution_profile_version='1.5.0' AND protocol_profile_id='protocol.deepseek-openai-chat.v4' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.deepseek-chat-function-tools.v1' AND probe_contract_version='2.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.6.api' AND execution_profile_version='1.7.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0' AND protocol_profile_id='protocol.kimi-openai-chat.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id='openai.chat_completions.v1' AND capability_id='tools' AND execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0' AND protocol_profile_id='protocol.kimi-openai-chat.k3.v5' AND protocol_profile_version='1.0.0' AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1' AND probe_contract_version='4.0.0') THEN
        RAISE EXCEPTION 'guarded downgrade final selector content mismatch';
    END IF;
END;
$$;

DO $$
DECLARE affected BIGINT;
BEGIN
    DELETE FROM schema_migrations WHERE version='000106_minimax_official_cn_payg_endpoint_qualification';
    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected <> 1 THEN
        RAISE EXCEPTION 'capability publication ledger delete mismatch';
    END IF;
END;
$$;

CREATE TRIGGER model_capability_probe_contract_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_contract_binding_mutation();

CREATE TRIGGER model_capability_current_probe_contracts_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_current_probe_contract_mutation();

COMMIT;
