BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE provider_connections, channels, provider_credential_bindings,
    channel_keys, model_deployments, model_registry,
    model_deployment_operations, model_deployment_capabilities,
    model_deployment_probes, channel_model_probes,
    credential_binding_deployments, runtime_route_revision_items,
    call_logs, billing_provider_attempts, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version > '000069_model_deployment_expand'
    ) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: later migrations must be reversed first';
    END IF;
END;
$$;

-- Rollback is permitted only while every canonical row remains a deterministic
-- projection of migration-68 legacy state. Canonical-origin configuration or
-- evidence cannot be recreated after these tables/columns are removed.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM provider_connections pc
          LEFT JOIN channels c ON c.id=pc.legacy_channel_id
         WHERE pc.legacy_channel_id IS NULL OR c.id IS NULL
            OR pc.name IS DISTINCT FROM c.name
            OR pc.provider IS DISTINCT FROM c.provider
            OR pc.base_url IS DISTINCT FROM c.base_url
            OR pc.adapter_type IS DISTINCT FROM COALESCE(NULLIF(BTRIM(c.adapter_type),''),'openai_compatible')
            OR pc.adapter_config IS DISTINCT FROM COALESCE(c.adapter_config,'{}'::jsonb)
            OR pc.admin_state IS DISTINCT FROM CASE WHEN c.status='disabled' THEN 'disabled' ELSE 'enabled' END
            OR pc.retired_at IS NOT NULL
            OR pc.config_revision IS DISTINCT FROM c.routing_epoch
            OR pc.created_at IS DISTINCT FROM c.created_at
            OR pc.updated_at IS DISTINCT FROM c.updated_at
    ) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: non-reconstructable provider connection exists';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM provider_credential_bindings binding
          LEFT JOIN channel_keys legacy_key ON legacy_key.id=binding.legacy_channel_key_id
          LEFT JOIN provider_connections pc ON pc.id=binding.connection_id
         WHERE binding.legacy_channel_key_id IS NULL OR legacy_key.id IS NULL
            OR pc.legacy_channel_id IS DISTINCT FROM legacy_key.channel_id
            OR binding.key_enc IS DISTINCT FROM CASE WHEN legacy_key.status='retired' THEN '' ELSE legacy_key.key_enc END
            OR binding.status IS DISTINCT FROM CASE
                   WHEN legacy_key.status IN ('active','disabled','invalid','retired') THEN legacy_key.status
                   ELSE 'invalid'
               END
            OR binding.retired_at IS DISTINCT FROM CASE
                   WHEN legacy_key.status='retired' THEN legacy_key.updated_at ELSE NULL
               END
            OR binding.state_revision IS DISTINCT FROM legacy_key.state_epoch
            OR binding.fail_count IS DISTINCT FROM GREATEST(legacy_key.fail_count,0)
            OR binding.last_used_at IS DISTINCT FROM legacy_key.last_used_at
            OR binding.cooldown_until IS DISTINCT FROM legacy_key.cooldown_until
            OR binding.created_at IS DISTINCT FROM legacy_key.created_at
            OR binding.updated_at IS DISTINCT FROM legacy_key.updated_at
    ) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: non-reconstructable provider credential binding exists';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM model_deployments deployment
          LEFT JOIN provider_connections pc ON pc.id=deployment.connection_id
          LEFT JOIN channels c ON c.id=deployment.legacy_channel_id
          LEFT JOIN model_registry registry
            ON registry.id=deployment.public_model_id
           AND registry.org_id=deployment.customer_org_id
         WHERE deployment.legacy_channel_id IS NULL OR c.id IS NULL
            OR deployment.legacy_public_model IS NULL
            OR pc.legacy_channel_id IS DISTINCT FROM c.id
            OR registry.model_name IS DISTINCT FROM deployment.legacy_public_model
            OR NOT EXISTS (
                SELECT 1
                  FROM regexp_split_to_table(COALESCE(c.models,''), E'[,，;；\\r\\n]+') AS model(value)
                 WHERE BTRIM(model.value)=deployment.legacy_public_model
            )
            OR jsonb_typeof(COALESCE(c.model_mapping,'{}'::jsonb)) <> 'object'
            OR deployment.upstream_model IS DISTINCT FROM COALESCE(
                   NULLIF(BTRIM(c.model_mapping->>deployment.legacy_public_model),''),
                   deployment.legacy_public_model
               )
            OR deployment.admin_state IS DISTINCT FROM CASE WHEN c.status='disabled' THEN 'disabled' ELSE 'enabled' END
            OR deployment.retired_at IS NOT NULL
            OR deployment.config_revision IS DISTINCT FROM c.routing_epoch
            OR deployment.priority IS DISTINCT FROM c.priority
            OR deployment.routing_weight IS DISTINCT FROM c.routing_weight
            OR deployment.max_context IS DISTINCT FROM c.max_context
            OR deployment.default_max_output IS DISTINCT FROM c.default_max_output
            OR deployment.degradation_threshold IS DISTINCT FROM c.degradation_threshold
            OR deployment.circuit_state IS DISTINCT FROM CASE
                   WHEN BTRIM(c.circuit_state) IN ('healthy','half_open','open') THEN BTRIM(c.circuit_state)
                   ELSE 'open'
               END
            OR deployment.circuit_revision IS DISTINCT FROM c.circuit_epoch
            OR deployment.fail_count IS DISTINCT FROM GREATEST(c.fail_count,0)
            OR deployment.half_open_until IS DISTINCT FROM c.half_open_until
            OR deployment.half_open_probe_until IS DISTINCT FROM c.half_open_probe_until
            OR deployment.half_open_probe_token IS DISTINCT FROM NULLIF(c.half_open_probe_token,'')
            OR deployment.created_at IS DISTINCT FROM c.created_at
            OR deployment.updated_at IS DISTINCT FROM c.updated_at
    ) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: non-reconstructable model deployment exists';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM model_deployment_operations operation
          JOIN model_deployments deployment ON deployment.id=operation.deployment_id
         WHERE operation.operation_id <> 'legacy.any.v1'
            OR operation.support_state <> 'enabled'
            OR operation.evidence_state <> 'declared'
            OR operation.evidence_ref <> 'legacy:channel:' || deployment.legacy_channel_id::text
            OR operation.verified_at IS NOT NULL
            OR operation.revision <> 1
    ) OR EXISTS (SELECT 1 FROM model_deployment_capabilities) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: canonical operation or capability evidence exists';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM model_deployment_probes probe
          LEFT JOIN model_deployments deployment ON deployment.id=probe.deployment_id
          LEFT JOIN channel_model_probes legacy_probe
            ON legacy_probe.channel_id=deployment.legacy_channel_id
           AND legacy_probe.public_model=deployment.legacy_public_model
          LEFT JOIN provider_credential_bindings binding
            ON binding.id=probe.credential_binding_id
         WHERE probe.source_kind <> 'legacy.channel_model_probe'
            OR legacy_probe.channel_id IS NULL
            OR probe.operation_id <> 'legacy.any.v1'
            OR probe.probe_profile_id <> 'legacy.channel_model_probe'
            OR probe.probe_profile_version <> 'v1'
            OR probe.source_ref <> deployment.legacy_channel_id::text || ':' || deployment.legacy_public_model
            OR probe.connection_id IS DISTINCT FROM deployment.connection_id
            OR probe.upstream_model IS DISTINCT FROM BTRIM(legacy_probe.upstream_model)
            OR probe.reported_model IS DISTINCT FROM BTRIM(legacy_probe.reported_model)
            OR probe.connection_revision IS DISTINCT FROM legacy_probe.routing_epoch
            OR probe.deployment_revision IS DISTINCT FROM legacy_probe.routing_epoch
            OR (
                legacy_probe.credential_id IS NULL
                AND (probe.credential_binding_id IS NOT NULL OR probe.credential_state_revision <> 0)
            )
            OR (
                legacy_probe.credential_id IS NOT NULL
                AND (
                    binding.legacy_channel_key_id IS DISTINCT FROM legacy_probe.credential_id
                    OR probe.credential_state_revision IS DISTINCT FROM legacy_probe.credential_state_epoch
                )
            )
            OR probe.success IS DISTINCT FROM legacy_probe.success
            OR probe.status_code IS DISTINCT FROM legacy_probe.status_code
            OR probe.latency_ms IS DISTINCT FROM legacy_probe.latency_ms
            OR probe.error_summary IS DISTINCT FROM legacy_probe.error_summary
            OR probe.capabilities_verified <> '[]'::jsonb
            OR probe.request_summary <> jsonb_build_object(
                   'source','legacy_channel_model_probe',
                   'upstream_model',BTRIM(legacy_probe.upstream_model)
               )
            OR probe.response_summary <> jsonb_build_object(
                   'reported_model',BTRIM(legacy_probe.reported_model)
               )
            OR probe.tested_by IS DISTINCT FROM legacy_probe.tested_by
            OR probe.tested_at IS DISTINCT FROM legacy_probe.tested_at
    ) OR EXISTS (
        SELECT 1
          FROM model_deployment_probes
         GROUP BY source_ref
        HAVING COUNT(*) <> 1
    ) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: non-reconstructable canonical probe evidence exists';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM call_logs call_log
          LEFT JOIN model_deployments deployment ON deployment.id=call_log.route_deployment_id
         WHERE call_log.route_deployment_id IS NOT NULL
           AND (
               deployment.id IS NULL
               OR call_log.org_id IS DISTINCT FROM deployment.customer_org_id
               OR call_log.route_channel_id IS DISTINCT FROM deployment.legacy_channel_id
               OR call_log.model IS DISTINCT FROM deployment.legacy_public_model
           )
    ) OR EXISTS (
        SELECT 1
          FROM billing_provider_attempts attempt
         WHERE attempt.deployment_id IS NOT NULL
           AND NOT EXISTS (
               SELECT 1
                 FROM call_logs call_log
                WHERE call_log.org_id=attempt.org_id
                  AND call_log.request_id=attempt.request_id
                  AND call_log.attempt_id=attempt.attempt_id
                  AND call_log.route_deployment_id=attempt.deployment_id
           )
    ) THEN
        RAISE EXCEPTION 'Model deployment expand rollback refused: non-reconstructable deployment attribution exists';
    END IF;
END;
$$;

DROP INDEX IF EXISTS billing_provider_attempts_deployment_idx;
ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT IF EXISTS billing_provider_attempts_deployment_fk,
    DROP COLUMN IF EXISTS deployment_id;

DROP INDEX IF EXISTS idx_call_logs_org_route_deployment_created;
ALTER TABLE call_logs
    DROP CONSTRAINT IF EXISTS call_logs_route_deployment_fk,
    DROP COLUMN IF EXISTS route_deployment_id;

DROP TABLE IF EXISTS runtime_route_revision_items;
DROP VIEW IF EXISTS model_deployment_latest_probes;
DROP TABLE IF EXISTS model_deployment_probes;
DROP FUNCTION IF EXISTS reject_model_deployment_probe_mutation();
DROP TABLE IF EXISTS model_deployment_capabilities;
DROP TABLE IF EXISTS model_deployment_operations;
DROP TABLE IF EXISTS credential_binding_deployments;
DROP TABLE IF EXISTS provider_credential_bindings;
DROP TABLE IF EXISTS model_deployments;

ALTER TABLE model_registry
    DROP CONSTRAINT IF EXISTS model_registry_id_org_unique;

DROP TABLE IF EXISTS provider_connections;

DROP FUNCTION IF EXISTS preserve_provider_credential_binding_identity_and_retirement();
DROP FUNCTION IF EXISTS preserve_model_deployment_identity_and_retirement();
DROP FUNCTION IF EXISTS preserve_provider_connection_identity_and_retirement();

DELETE FROM schema_migrations
WHERE version = '000069_model_deployment_expand';

COMMIT;
