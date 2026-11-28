BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_generations, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM runtime_generations WHERE state <> 'stopped' OR ingress_state <> 'absent')
       OR EXISTS (SELECT 1 FROM runtime_ingress_changes WHERE status NOT IN ('applied', 'cancelled'))
       OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE c.status='cancelled' AND NOT EXISTS (
               SELECT 1 FROM runtime_ingress_changes n
               WHERE n.predecessor_id=c.change_id AND n.change_id=c.change_id || '-withdrawn'
                 AND n.status='applied' AND n.membership_after='[]'::jsonb
           )
       ) OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE NOT EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)
             AND c.membership_after <> '[]'::jsonb
       ) THEN
        RAISE EXCEPTION 'M135 requires stopped writers and completed empty ingress';
    END IF;
    IF (SELECT max(version) FROM schema_migrations) IS DISTINCT FROM '000135_canonical_media_job_authority' THEN
        RAISE EXCEPTION 'M135 rollback requires exact M135 head';
    END IF;
END $$;

LOCK TABLE async_media_charge_facts, model_operation_execution_support_bindings,
    model_operation_public_execution_contract_extensions,
    model_operation_production_approvals, model_deployment_probes,
    model_deployment_operations, runtime_change_releases,
    model_deployment_release_authoring_receipts, model_deployment_lifecycle_receipts,
    model_deployment_lifecycle_receipt_qualifications, runtime_route_revision_items,
    model_operation_public_execution_bindings, async_media_jobs, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE NOWAIT;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM async_media_jobs WHERE canonical_route IS NOT NULL)
       OR EXISTS (SELECT 1 FROM model_operation_public_execution_bindings WHERE operation_id='unirouter.media_jobs.v1')
       OR EXISTS (SELECT 1 FROM model_operation_production_approvals WHERE operation_id='unirouter.media_jobs.v1')
       OR EXISTS (SELECT 1 FROM model_deployment_operations WHERE operation_id='unirouter.media_jobs.v1') THEN
        RAISE EXCEPTION 'M135 rollback would discard retained canonical media authority';
    END IF;
END $$;

DROP TRIGGER async_media_jobs_canonical_route_immutable ON async_media_jobs;
DROP FUNCTION preserve_media_job_canonical_route();
ALTER TABLE async_media_jobs
    DROP COLUMN canonical_dispatch_route, DROP COLUMN canonical_route,
    DROP CONSTRAINT async_media_jobs_profile_check,
    ADD CONSTRAINT async_media_jobs_profile_check CHECK (profile_id='unirouter.openai.images.gpt-image.v1');

ALTER TABLE async_media_charge_facts
    DROP CONSTRAINT async_media_charge_units_check,
    ADD CONSTRAINT async_media_charge_units_check
        CHECK (
            jsonb_typeof(units) = 'array'
            AND jsonb_array_length(units) = 4
            AND units @> '[{"class":"input_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"input_text_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"input_image_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"output_image_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
        );

CREATE OR REPLACE FUNCTION enforce_model_operation_live_probe_contract()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    operation_row RECORD;
BEGIN
    IF NEW.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1') THEN
        IF NEW.operation_revision IS NOT NULL OR NEW.public_path IS NOT NULL
           OR NEW.executor_contract_version IS NOT NULL OR NEW.https_origin IS NOT NULL THEN
            RAISE EXCEPTION 'non-qualified operation probe cannot claim production execution binding'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.operation_revision IS NULL OR NEW.public_path IS NULL
       OR NEW.executor_contract_version IS NULL OR NEW.https_origin IS NULL
       OR NEW.source_kind<>'admin.endpointprotocol_live_probe.v1' THEN
        RAISE EXCEPTION 'Responses/Messages probe requires exact endpointprotocol network evidence'
            USING ERRCODE='23514';
    END IF;

    SELECT operation.revision AS operation_revision,
           operation.support_state,operation.protocol_profile_id,
           operation.protocol_profile_version,
           deployment.connection_id,deployment.upstream_model,
           deployment.config_revision AS deployment_revision,
           deployment.admin_state AS deployment_state,
           connection.config_revision AS connection_revision,
           connection.admin_state AS connection_state,
           model_operation_https_origin(connection.base_url) AS current_https_origin,
           support.public_path,support.executor_contract_version
      INTO operation_row
      FROM model_deployment_operations operation
      JOIN model_deployments deployment ON deployment.id=operation.deployment_id
      JOIN provider_connections connection ON connection.id=deployment.connection_id
      JOIN model_operation_execution_support_bindings support
        ON support.operation_id=NEW.operation_id
       AND support.public_path=NEW.public_path
       AND support.protocol_profile_id=NEW.probe_profile_id
       AND support.protocol_profile_version=NEW.probe_profile_version
       AND support.executor_contract_version=NEW.executor_contract_version
     WHERE operation.deployment_id=NEW.deployment_id
       AND operation.operation_id=NEW.operation_id
     FOR KEY SHARE OF operation,deployment,connection;

    IF NOT FOUND
       OR NEW.public_path IS DISTINCT FROM operation_row.public_path
       OR NEW.executor_contract_version IS DISTINCT FROM operation_row.executor_contract_version THEN
        RAISE EXCEPTION 'Responses/Messages probe execution binding is stale or mismatched'
            USING ERRCODE='23514';
    END IF;
    IF NEW.success AND (
       operation_row.support_state<>'enabled'
       OR operation_row.deployment_state<>'enabled'
       OR operation_row.connection_state<>'enabled'
       OR operation_row.current_https_origin IS NULL
       OR NEW.probe_profile_id IS DISTINCT FROM operation_row.protocol_profile_id
       OR NEW.probe_profile_version IS DISTINCT FROM operation_row.protocol_profile_version
       OR NEW.connection_id IS DISTINCT FROM operation_row.connection_id
       OR NEW.upstream_model IS DISTINCT FROM operation_row.upstream_model
       OR NEW.connection_revision IS DISTINCT FROM operation_row.connection_revision
       OR NEW.deployment_revision IS DISTINCT FROM operation_row.deployment_revision
       OR NEW.operation_revision IS DISTINCT FROM operation_row.operation_revision
       OR NEW.https_origin IS DISTINCT FROM operation_row.current_https_origin
    ) THEN
        RAISE EXCEPTION 'successful Responses/Messages probe must bind current executable state'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_qualified_model_operation_evidence_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    support RECORD;
    probe RECORD;
    approval RECORD;
BEGIN
    IF NEW.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1') THEN
        RETURN NEW;
    END IF;

    SELECT * INTO support
      FROM model_operation_current_execution_support_bindings current_support
     WHERE current_support.operation_id=NEW.operation_id
       AND current_support.protocol_profile_id=NEW.protocol_profile_id
       AND current_support.protocol_profile_version=NEW.protocol_profile_version;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Responses/Messages operation lacks the current immutable support binding'
            USING ERRCODE='23514';
    END IF;

    IF TG_OP='INSERT' THEN
        IF NEW.evidence_state IN ('live_probed','production_approved') THEN
            RAISE EXCEPTION 'Responses/Messages operation cannot be created with production evidence'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.evidence_state='live_probed'
       AND (OLD.evidence_state IS DISTINCT FROM NEW.evidence_state
            OR OLD.evidence_ref IS DISTINCT FROM NEW.evidence_ref) THEN
        IF NEW.evidence_ref !~ '^probe:[1-9][0-9]*$' THEN
            RAISE EXCEPTION 'live_probed transition requires an exact probe receipt'
                USING ERRCODE='23514';
        END IF;
        SELECT p.*,deployment.config_revision AS current_deployment_revision,
               connection.config_revision AS current_connection_revision,
               model_operation_https_origin(connection.base_url) AS current_https_origin
          INTO probe
          FROM model_deployment_probes p
          JOIN model_deployments deployment ON deployment.id=p.deployment_id
          JOIN provider_connections connection ON connection.id=deployment.connection_id
         WHERE p.probe_id=SUBSTRING(NEW.evidence_ref FROM 7)::BIGINT
           AND p.deployment_id=NEW.deployment_id
           AND p.operation_id=NEW.operation_id;
        IF NOT FOUND OR NOT probe.success
           OR probe.source_kind<>'admin.endpointprotocol_live_probe.v1'
           OR probe.operation_revision IS DISTINCT FROM OLD.revision
           OR probe.connection_revision IS DISTINCT FROM probe.current_connection_revision
           OR probe.deployment_revision IS DISTINCT FROM probe.current_deployment_revision
           OR probe.probe_profile_id IS DISTINCT FROM NEW.protocol_profile_id
           OR probe.probe_profile_version IS DISTINCT FROM NEW.protocol_profile_version
           OR probe.public_path IS DISTINCT FROM support.public_path
           OR probe.executor_contract_version IS DISTINCT FROM support.executor_contract_version
           OR probe.https_origin IS DISTINCT FROM probe.current_https_origin
           OR NEW.revision IS DISTINCT FROM OLD.revision+1 THEN
            RAISE EXCEPTION 'live_probed transition proof is stale or mismatched'
                USING ERRCODE='23514';
        END IF;
    END IF;

    IF NEW.evidence_state='production_approved'
       AND (OLD.evidence_state IS DISTINCT FROM NEW.evidence_state
            OR OLD.evidence_ref IS DISTINCT FROM NEW.evidence_ref) THEN
        IF NEW.evidence_ref !~ '^approval:[1-9][0-9]*$' THEN
            RAISE EXCEPTION 'production_approved transition requires an approval receipt'
                USING ERRCODE='23514';
        END IF;
        SELECT * INTO approval
          FROM model_operation_production_approvals a
         WHERE a.approval_id=SUBSTRING(NEW.evidence_ref FROM 10)::BIGINT
           AND a.deployment_id=NEW.deployment_id
           AND a.operation_id=NEW.operation_id;
        IF NOT FOUND
           OR approval.source_operation_revision IS DISTINCT FROM OLD.revision
           OR approval.approved_operation_revision IS DISTINCT FROM NEW.revision
           OR approval.public_path IS DISTINCT FROM support.public_path
           OR approval.protocol_profile_id IS DISTINCT FROM NEW.protocol_profile_id
           OR approval.protocol_profile_version IS DISTINCT FROM NEW.protocol_profile_version
           OR approval.executor_contract_version IS DISTINCT FROM support.executor_contract_version
           OR NEW.revision IS DISTINCT FROM OLD.revision+1 THEN
            RAISE EXCEPTION 'production_approved transition proof is stale or mismatched'
                USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE VIEW model_operation_execution_qualifications AS
SELECT operation.deployment_id,deployment.connection_id,operation.operation_id,
       support.public_path,operation.protocol_profile_id,
       operation.protocol_profile_version,support.executor_contract_version,
       approval.https_origin,approval.approval_id,approval.probe_id,
       approval.credential_binding_id,approval.credential_state_revision,
       approval.credential_link_created_at,
       approval.connection_revision AS approved_connection_revision,
       approval.deployment_revision AS approved_deployment_revision,
       approval.approved_operation_revision,
       connection.config_revision AS current_connection_revision,
       deployment.config_revision AS current_deployment_revision,
       operation.revision AS current_operation_revision,
       CASE
         WHEN connection.admin_state<>'enabled' THEN 'connection_disabled'
         WHEN deployment.admin_state<>'enabled' THEN 'deployment_disabled'
         WHEN operation.support_state<>'enabled' THEN 'operation_unsupported'
         WHEN support.operation_id IS NULL THEN 'compiled_support_binding_missing'
         WHEN operation.evidence_state<>'production_approved' THEN 'production_approval_missing'
         WHEN approval.approval_id IS NULL THEN 'production_approval_receipt_missing'
         WHEN approved_probe.probe_id IS NULL THEN 'approved_probe_missing'
         WHEN NOT approved_probe.success
           OR approved_probe.source_kind<>'admin.endpointprotocol_live_probe.v1'
           OR approved_probe.credential_binding_id IS DISTINCT FROM approval.credential_binding_id
           OR approved_probe.credential_state_revision IS DISTINCT FROM approval.credential_state_revision
           OR approved_probe.connection_revision IS DISTINCT FROM approval.connection_revision
           OR approved_probe.deployment_revision IS DISTINCT FROM approval.deployment_revision
           OR approved_probe.operation_revision IS DISTINCT FROM approval.probe_operation_revision
           OR approved_probe.public_path IS DISTINCT FROM approval.public_path
           OR approved_probe.probe_profile_id IS DISTINCT FROM approval.protocol_profile_id
           OR approved_probe.probe_profile_version IS DISTINCT FROM approval.protocol_profile_version
           OR approved_probe.executor_contract_version IS DISTINCT FROM approval.executor_contract_version
           OR approved_probe.https_origin IS DISTINCT FROM approval.https_origin
           OR approval.credential_link_created_at>approved_probe.tested_at
           THEN 'approved_probe_identity_stale'
         WHEN approval.connection_revision<>connection.config_revision THEN 'connection_revision_stale'
         WHEN approval.deployment_revision<>deployment.config_revision THEN 'deployment_revision_stale'
         WHEN approval.approved_operation_revision<>operation.revision THEN 'operation_revision_stale'
         WHEN approval.protocol_profile_id<>operation.protocol_profile_id
           OR approval.protocol_profile_version<>operation.protocol_profile_version THEN 'protocol_profile_stale'
         WHEN approval.public_path<>support.public_path
           OR approval.executor_contract_version<>support.executor_contract_version THEN 'execution_support_binding_stale'
         WHEN approval.https_origin IS DISTINCT FROM model_operation_https_origin(connection.base_url)
           THEN 'https_origin_stale'
         ELSE 'production_qualified'
       END AS qualification_state,
       (
         connection.admin_state='enabled' AND deployment.admin_state='enabled'
         AND operation.support_state='enabled' AND support.operation_id IS NOT NULL
         AND operation.evidence_state='production_approved'
         AND approval.approval_id IS NOT NULL
         AND approved_probe.probe_id IS NOT NULL AND approved_probe.success
         AND approved_probe.source_kind='admin.endpointprotocol_live_probe.v1'
         AND approved_probe.credential_binding_id=approval.credential_binding_id
         AND approved_probe.credential_state_revision=approval.credential_state_revision
         AND approved_probe.connection_revision=approval.connection_revision
         AND approved_probe.deployment_revision=approval.deployment_revision
         AND approved_probe.operation_revision=approval.probe_operation_revision
         AND approved_probe.public_path=approval.public_path
         AND approved_probe.probe_profile_id=approval.protocol_profile_id
         AND approved_probe.probe_profile_version=approval.protocol_profile_version
         AND approved_probe.executor_contract_version=approval.executor_contract_version
         AND approved_probe.https_origin=approval.https_origin
         AND approval.credential_link_created_at<=approved_probe.tested_at
         AND approval.connection_revision=connection.config_revision
         AND approval.deployment_revision=deployment.config_revision
         AND approval.approved_operation_revision=operation.revision
         AND approval.protocol_profile_id=operation.protocol_profile_id
         AND approval.protocol_profile_version=operation.protocol_profile_version
         AND approval.public_path=support.public_path
         AND approval.executor_contract_version=support.executor_contract_version
         AND approval.https_origin IS NOT DISTINCT FROM model_operation_https_origin(connection.base_url)
       ) AS qualified
  FROM model_deployment_operations operation
  JOIN model_deployments deployment ON deployment.id=operation.deployment_id
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  LEFT JOIN model_operation_current_execution_support_bindings support
    ON support.operation_id=operation.operation_id
   AND support.protocol_profile_id=operation.protocol_profile_id
   AND support.protocol_profile_version=operation.protocol_profile_version
  LEFT JOIN model_operation_production_approvals approval
    ON operation.evidence_ref='approval:' || approval.approval_id::TEXT
   AND approval.deployment_id=operation.deployment_id
   AND approval.operation_id=operation.operation_id
  LEFT JOIN model_deployment_probes approved_probe
    ON approved_probe.probe_id=approval.probe_id
   AND approved_probe.deployment_id=approval.deployment_id
   AND approved_probe.connection_id=approval.connection_id
   AND approved_probe.operation_id=approval.operation_id
 WHERE operation.operation_id IN ('openai.responses.v1','anthropic.messages.v1');

CREATE OR REPLACE FUNCTION enforce_runtime_route_item_production_qualification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    support RECORD;
    public_binding RECORD;
    source_sealed_at TIMESTAMPTZ(6);
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN
        SELECT route_items_sealed_at INTO source_sealed_at
          FROM runtime_change_releases WHERE id=OLD.release_id FOR SHARE;
        IF source_sealed_at IS NOT NULL THEN
            RAISE EXCEPTION 'sealed runtime route qualification evidence is immutable'
                USING ERRCODE='55000';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.operation_id IN ('openai.responses.v1','anthropic.messages.v1') THEN
        SELECT * INTO support
          FROM model_operation_current_public_execution_contracts current_support
         WHERE current_support.operation_id=NEW.operation_id
           AND current_support.public_path=NEW.public_path
           AND current_support.protocol_profile_id=NEW.protocol_profile_id
           AND current_support.protocol_profile_version=NEW.protocol_profile_version
           AND current_support.executor_contract_version=NEW.executor_contract_version
           AND current_support.delivery_contract_version=NEW.delivery_contract_version
           AND current_support.charge_contract_version=NEW.charge_contract_version
           AND current_support.idempotency_contract_version=NEW.idempotency_contract_version;
        SELECT * INTO public_binding
          FROM model_operation_public_execution_bindings binding
         WHERE binding.public_execution_binding_id=NEW.public_execution_binding_id
           AND binding.release_id=NEW.release_id AND binding.ordinal=NEW.ordinal
           AND binding.deployment_id=NEW.deployment_id
           AND binding.operation_id=NEW.operation_id;
        IF support.operation_id IS NULL OR public_binding.public_execution_binding_id IS NULL
           OR NEW.source_candidate_version<>'unirouter.runtime-change-candidate.v2'
           OR NEW.expected_connection_revision IS DISTINCT FROM public_binding.approved_connection_revision
           OR NEW.expected_deployment_revision IS DISTINCT FROM public_binding.approved_deployment_revision
           OR NEW.expected_operation_revision IS DISTINCT FROM public_binding.approved_operation_revision
           OR NEW.public_path IS DISTINCT FROM public_binding.public_path
           OR NEW.protocol_profile_id IS DISTINCT FROM public_binding.protocol_profile_id
           OR NEW.protocol_profile_version IS DISTINCT FROM public_binding.protocol_profile_version
           OR NEW.executor_contract_version IS DISTINCT FROM public_binding.executor_contract_version
           OR NEW.delivery_contract_version IS DISTINCT FROM public_binding.delivery_contract_version
           OR NEW.charge_contract_version IS DISTINCT FROM public_binding.charge_contract_version
           OR NEW.idempotency_contract_version IS DISTINCT FROM public_binding.idempotency_contract_version THEN
            RAISE EXCEPTION 'Responses/Messages runtime item requires the exact M77 public execution binding'
                USING ERRCODE='23514';
        END IF;
    ELSIF NEW.public_path IS NOT NULL OR NEW.executor_contract_version IS NOT NULL
       OR NEW.public_execution_binding_id IS NOT NULL
       OR NEW.delivery_contract_version IS NOT NULL
       OR NEW.charge_contract_version IS NOT NULL
       OR NEW.idempotency_contract_version IS NOT NULL THEN
        RAISE EXCEPTION 'non-public runtime item cannot claim a public execution binding'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_runtime_release_p3_qualification()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.route_items_sealed_at IS NULL AND NEW.route_items_sealed_at IS NOT NULL
       AND EXISTS (
           SELECT 1
             FROM runtime_route_revision_items item
             LEFT JOIN model_operation_public_execution_qualifications qualification
               ON qualification.public_execution_binding_id=item.public_execution_binding_id
              AND qualification.release_id=item.release_id
              AND qualification.ordinal=item.ordinal
            WHERE item.release_id=NEW.id
              AND item.operation_id IN ('openai.responses.v1','anthropic.messages.v1')
              AND qualification.public_executable IS DISTINCT FROM TRUE
       ) THEN
        RAISE EXCEPTION 'Responses/Messages runtime manifest lacks current M77 public execution qualification'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION preserve_model_deployment_release_authoring_receipt()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    audit_count BIGINT;
    release_count BIGINT;
    event_count BIGINT;
    selection_count BIGINT;
    item_count BIGINT;
    binding_count BIGINT;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'model deployment release authoring receipts are durable evidence'
            USING ERRCODE='55000';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'in_progress' THEN
            RAISE EXCEPTION 'model deployment release authoring receipt must be claimed in progress'
                USING ERRCODE='23514';
        END IF;
        IF EXISTS (
            SELECT 1
             FROM jsonb_array_elements(NEW.requested_selections) WITH ORDINALITY selection(value,ordinal)
             WHERE jsonb_typeof(selection.value)<>'object'
                OR CASE WHEN jsonb_typeof(selection.value)='object'
                        THEN (SELECT COUNT(*) FROM jsonb_object_keys(selection.value)) NOT IN (2,3)
                        ELSE FALSE END
                OR selection.value-ARRAY['deployment_id','operation_id','expected_upstream_observation_identity']<>'{}'::jsonb
                OR (selection.value ? 'expected_upstream_observation_identity' AND
                  (jsonb_typeof(selection.value->'expected_upstream_observation_identity') IS DISTINCT FROM 'string'
                   OR selection.value->>'expected_upstream_observation_identity' !~ '^uor_[0-9a-f]{32}$'))
                OR NOT (selection.value ? 'deployment_id')
                OR NOT (selection.value ? 'operation_id')
                OR jsonb_typeof(selection.value->'deployment_id')<>'number'
                OR jsonb_typeof(selection.value->'operation_id')<>'string'
                OR (selection.value->>'deployment_id') !~ '^[1-9][0-9]*$'
                OR selection.value->>'operation_id' NOT IN (
                    'openai.chat_completions.v1','openai.embeddings.v1',
                    'openai.responses.v1','anthropic.messages.v1'
                )
        ) OR EXISTS (
            SELECT 1
              FROM jsonb_array_elements(NEW.requested_selections) selection(value)
             GROUP BY selection.value->>'deployment_id',selection.value->>'operation_id'
            HAVING COUNT(*)<>1
        ) THEN
            RAISE EXCEPTION 'model deployment release authoring selections must be exact, unique, and supported'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.status='completed' OR NEW.status<>'completed'
       OR NEW.receipt_id IS DISTINCT FROM OLD.receipt_id
       OR NEW.contract_version IS DISTINCT FROM OLD.contract_version
       OR NEW.customer_org_id IS DISTINCT FROM OLD.customer_org_id
       OR NEW.actor_user_id IS DISTINCT FROM OLD.actor_user_id
       OR NEW.actor_org_id IS DISTINCT FROM OLD.actor_org_id
       OR NEW.operation_scope IS DISTINCT FROM OLD.operation_scope
       OR NEW.idempotency_key_hash IS DISTINCT FROM OLD.idempotency_key_hash
       OR NEW.request_fingerprint IS DISTINCT FROM OLD.request_fingerprint
       OR NEW.server_operation_id IS DISTINCT FROM OLD.server_operation_id
       OR NEW.public_model_id IS DISTINCT FROM OLD.public_model_id
       OR NEW.baseline_kind IS DISTINCT FROM OLD.baseline_kind
       OR NEW.expected_slot_version IS DISTINCT FROM OLD.expected_slot_version
       OR NEW.expected_stable_release_ref IS DISTINCT FROM OLD.expected_stable_release_ref
       OR NEW.expected_stable_release_version IS DISTINCT FROM OLD.expected_stable_release_version
       OR NEW.requested_selections IS DISTINCT FROM OLD.requested_selections
       OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.target_release_ref IS DISTINCT FROM OLD.target_release_ref
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'model deployment release authoring receipt identity is immutable'
            USING ERRCODE='55000';
    END IF;

    PERFORM actor.id
      FROM users actor
      JOIN orgs actor_org ON actor_org.id=actor.org_id
     WHERE actor.id=NEW.actor_user_id
       AND actor.org_id=NEW.actor_org_id
       AND actor.status='active' AND actor_org.status='active'
       AND (
         COALESCE(NULLIF(actor.organization_role,''),actor.role)='platform_admin'
         OR (
           COALESCE(NULLIF(actor.organization_role,''),actor.role)='org_admin'
           AND actor.org_id=NEW.customer_org_id
         )
       )
     FOR SHARE OF actor,actor_org;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires an active authorized actor'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO audit_count
      FROM audit_logs audit
     WHERE audit.id=NEW.audit_log_id
       AND audit.org_id=NEW.customer_org_id
       AND audit.user_id=NEW.actor_user_id
       AND audit.action='model_deployment.release_authoring.create'
       AND audit.resource='model_deployment_release'
       AND audit.resource_id=NEW.release_id::TEXT
       AND audit.detail->>'receipt_id'=NEW.receipt_id::TEXT
       AND audit.detail->>'contract_version'=NEW.contract_version
       AND audit.detail->>'server_operation_id'=NEW.server_operation_id
       AND audit.detail->>'runtime_mutation'='none';
    IF audit_count<>1 THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires the exact audit row'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO release_count
      FROM runtime_change_releases release
     WHERE release.id=NEW.release_id
       AND release.release_ref=NEW.target_release_ref
       AND release.authoring_receipt_id=NEW.receipt_id
       AND release.public_model_id=NEW.public_model_id
       AND release.model_name=NEW.model_name
       AND release.candidate_version='unirouter.runtime-change-candidate.v2'
       AND release.candidate_hash=NEW.candidate_hash
       AND release.route_items_hash=NEW.route_items_hash
       AND release.route_items_count=NEW.route_items_count
       AND release.stage='validated' AND release.version=2
       AND release.route_items_sealed_at IS NOT NULL
       AND release.created_by=NEW.actor_user_id
       AND release.validated_by=NEW.actor_user_id
       AND release.reason=NEW.reason
       AND release.candidate->>'contract_version'=release.candidate_version
       AND CASE WHEN jsonb_typeof(release.candidate)='object'
                THEN (SELECT COUNT(*) FROM jsonb_object_keys(release.candidate))=4
                ELSE FALSE END
       AND (release.candidate->>'public_model_id')::BIGINT=NEW.public_model_id
       AND release.candidate->>'model_name'=NEW.model_name
       AND jsonb_array_length(release.candidate->'deployment_routes')=NEW.route_items_count
       AND (
         (NEW.baseline_kind='initial' AND release.parent_stable_release_id IS NULL
          AND NOT EXISTS (
              SELECT 1 FROM runtime_change_slots slot
               WHERE slot.model_name=NEW.model_name OR slot.public_model_id=NEW.public_model_id
          ))
         OR
	         (NEW.baseline_kind='stable' AND EXISTS (
             SELECT 1
               FROM runtime_change_slots slot
               JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
              WHERE slot.model_name=NEW.model_name
                AND slot.public_model_id=NEW.public_model_id
                AND slot.slot_version=NEW.expected_slot_version
                AND slot.canary_release_id IS NULL
                AND slot.canary_basis_points=0
                AND stable.id=release.parent_stable_release_id
                AND stable.release_ref=NEW.expected_stable_release_ref
                AND stable.version=NEW.expected_stable_release_version
                AND stable.stage='active'
                AND stable.candidate_version='unirouter.runtime-change-candidate.v2'
                AND stable.public_model_id=NEW.public_model_id
                AND stable.model_name=NEW.model_name
                AND stable.route_items_sealed_at IS NOT NULL
                AND stable.route_items_count>0
                AND stable.candidate_hash=release.baseline_hash
         ) AND 1=(
             SELECT COUNT(*) FROM runtime_change_slots identity_slot
              WHERE identity_slot.model_name=NEW.model_name
                 OR identity_slot.public_model_id=NEW.public_model_id
	         ))
	         OR
	         (NEW.baseline_kind='legacy_cutover' AND EXISTS (
	             SELECT 1
	               FROM runtime_change_slots slot
	               JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
	              WHERE slot.model_name=NEW.model_name
	                AND slot.public_model_id IS NULL
	                AND slot.slot_version=NEW.expected_slot_version
	                AND slot.canary_release_id IS NULL
	                AND slot.canary_basis_points=0
	                AND stable.id=release.parent_stable_release_id
	                AND stable.release_ref=NEW.expected_stable_release_ref
	                AND stable.version=NEW.expected_stable_release_version
	                AND stable.stage='active'
	                AND stable.candidate_version='unirouter.runtime-change-candidate.v1'
	                AND stable.public_model_id=NEW.public_model_id
	                AND stable.model_name=NEW.model_name
	                AND stable.route_items_sealed_at IS NOT NULL
	                AND stable.route_items_hash ~ '^sha256:[a-f0-9]{64}$'
	                AND stable.route_items_count>0
	                AND stable.candidate_hash=release.baseline_hash
	         ) AND 1=(
	             SELECT COUNT(*) FROM runtime_change_slots identity_slot
	              WHERE identity_slot.model_name=NEW.model_name
	                 OR identity_slot.public_model_id=NEW.public_model_id
	         ))
	       );
    IF release_count<>1 THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires the exact sealed release and baseline'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO event_count
      FROM runtime_change_events event
     WHERE event.release_id=NEW.release_id;
    IF event_count<>2 OR NOT EXISTS (
        SELECT 1 FROM runtime_change_events event
         WHERE event.release_id=NEW.release_id AND event.sequence=1
           AND event.action='create' AND event.from_stage='none' AND event.to_stage='draft'
           AND event.actor_user_id=NEW.actor_user_id
           AND event.expected_version=0 AND event.resulting_version=1
           AND event.detail->>'authoring_receipt_id'=NEW.receipt_id::TEXT
           AND event.detail->>'canonical_authoring'='true'
    ) OR NOT EXISTS (
        SELECT 1 FROM runtime_change_events event
         WHERE event.release_id=NEW.release_id AND event.sequence=2
           AND event.action='validate' AND event.from_stage='draft' AND event.to_stage='validated'
           AND event.actor_user_id=NEW.actor_user_id
           AND event.expected_version=1 AND event.resulting_version=2
           AND event.detail->>'authoring_receipt_id'=NEW.receipt_id::TEXT
           AND event.detail->>'canonical_authoring'='true'
    ) THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires exactly two authoring events'
            USING ERRCODE='23514';
    END IF;

    WITH candidate_items AS (
        SELECT route,ordinality::INTEGER AS ordinal
          FROM runtime_change_releases release
          CROSS JOIN LATERAL jsonb_array_elements(release.candidate->'deployment_routes')
               WITH ORDINALITY candidate(route,ordinality)
         WHERE release.id=NEW.release_id
    ), requested AS (
        SELECT selection,ordinality::INTEGER AS ordinal
          FROM jsonb_array_elements(NEW.requested_selections)
               WITH ORDINALITY requested(selection,ordinality)
    )
    SELECT COUNT(*) INTO selection_count
      FROM requested
      FULL JOIN candidate_items USING (ordinal)
     WHERE requested.selection IS NULL OR candidate_items.route IS NULL
        OR requested.selection->>'deployment_id' IS DISTINCT FROM candidate_items.route->>'deployment_id'
        OR requested.selection->>'operation_id' IS DISTINCT FROM candidate_items.route->>'model_operation_id';
    IF selection_count<>0 THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires exact ordered request selections'
            USING ERRCODE='23514';
    END IF;

    WITH candidate_items AS (
        SELECT route,ordinality::INTEGER AS ordinal
          FROM runtime_change_releases release
          CROSS JOIN LATERAL jsonb_array_elements(release.candidate->'deployment_routes')
               WITH ORDINALITY candidate(route,ordinality)
         WHERE release.id=NEW.release_id
    )
    SELECT COUNT(*) INTO item_count
      FROM candidate_items candidate
      FULL JOIN (
          SELECT * FROM runtime_route_revision_items source_item
           WHERE source_item.release_id=NEW.release_id
      ) item ON item.ordinal=candidate.ordinal
     WHERE candidate.route IS NULL OR item.release_id IS NULL
        OR CASE WHEN jsonb_typeof(candidate.route)='object'
                THEN (SELECT COUNT(*) FROM jsonb_object_keys(candidate.route))<>CASE
                     WHEN item.operation_id IN ('openai.responses.v1','anthropic.messages.v1') THEN 14 ELSE 8 END
                ELSE TRUE END
        OR candidate.route->'deployment_id' IS DISTINCT FROM to_jsonb(item.deployment_id)
        OR candidate.route->'model_operation_id' IS DISTINCT FROM to_jsonb(item.operation_id)
        OR candidate.route->'expected_connection_revision' IS DISTINCT FROM to_jsonb(item.expected_connection_revision)
        OR candidate.route->'expected_deployment_revision' IS DISTINCT FROM to_jsonb(item.expected_deployment_revision)
        OR candidate.route->'expected_operation_revision' IS DISTINCT FROM to_jsonb(item.expected_operation_revision)
        OR candidate.route->'protocol_profile_id' IS DISTINCT FROM to_jsonb(item.protocol_profile_id)
        OR candidate.route->'protocol_profile_version' IS DISTINCT FROM to_jsonb(item.protocol_profile_version)
        OR candidate.route->'public_path' IS DISTINCT FROM to_jsonb(item.public_path)
        OR candidate.route->'executor_contract_version' IS DISTINCT FROM to_jsonb(item.executor_contract_version)
        OR candidate.route->'public_execution_binding_id' IS DISTINCT FROM to_jsonb(item.public_execution_binding_id)
        OR candidate.route->'delivery_contract_version' IS DISTINCT FROM to_jsonb(item.delivery_contract_version)
        OR candidate.route->'charge_contract_version' IS DISTINCT FROM to_jsonb(item.charge_contract_version)
        OR candidate.route->'idempotency_contract_version' IS DISTINCT FROM to_jsonb(item.idempotency_contract_version)
        OR candidate.route->'admission' IS DISTINCT FROM to_jsonb(item.admission)
        OR candidate.route->'priority' IS DISTINCT FROM to_jsonb(item.priority)
        OR candidate.route->'weight' IS DISTINCT FROM to_jsonb(item.weight)
        OR item.source_candidate_version<>'unirouter.runtime-change-candidate.v2';
    IF item_count<>0 THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires bidirectional field-exact candidate items'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO binding_count
      FROM (
          SELECT * FROM runtime_route_revision_items source_item
           WHERE source_item.release_id=NEW.release_id
      ) item
      FULL JOIN (
          SELECT * FROM model_operation_public_execution_bindings source_binding
           WHERE source_binding.release_id=NEW.release_id
      ) binding ON binding.ordinal=item.ordinal
     LEFT JOIN users actor ON actor.id=binding.created_by AND actor.org_id=binding.created_by_org_id
     LEFT JOIN orgs actor_org ON actor_org.id=actor.org_id
     LEFT JOIN model_deployments binding_deployment
       ON binding_deployment.id=binding.deployment_id
     LEFT JOIN model_operation_public_execution_qualifications public_qualification
       ON public_qualification.public_execution_binding_id=binding.public_execution_binding_id
     WHERE COALESCE(item.release_id,binding.release_id)=NEW.release_id
       AND (
         (item.operation_id IN ('openai.responses.v1','anthropic.messages.v1') AND (
            binding.public_execution_binding_id IS NULL
            OR binding.public_execution_binding_id IS DISTINCT FROM item.public_execution_binding_id
            OR binding.customer_org_id IS DISTINCT FROM NEW.customer_org_id
            OR binding.public_model_id IS DISTINCT FROM NEW.public_model_id
            OR binding.deployment_id IS DISTINCT FROM item.deployment_id
            OR binding.connection_id IS DISTINCT FROM binding_deployment.connection_id
            OR binding.operation_id IS DISTINCT FROM item.operation_id
            OR binding.approved_connection_revision IS DISTINCT FROM item.expected_connection_revision
            OR binding.approved_deployment_revision IS DISTINCT FROM item.expected_deployment_revision
            OR binding.approved_operation_revision IS DISTINCT FROM item.expected_operation_revision
            OR binding.public_path IS DISTINCT FROM item.public_path
            OR binding.protocol_profile_id IS DISTINCT FROM item.protocol_profile_id
            OR binding.protocol_profile_version IS DISTINCT FROM item.protocol_profile_version
            OR binding.executor_contract_version IS DISTINCT FROM item.executor_contract_version
            OR binding.delivery_contract_version IS DISTINCT FROM item.delivery_contract_version
            OR binding.charge_contract_version IS DISTINCT FROM item.charge_contract_version
            OR binding.idempotency_contract_version IS DISTINCT FROM item.idempotency_contract_version
            OR binding.source_kind IS DISTINCT FROM 'runtime.public_execution_binding.v1'
            OR binding.source_ref IS DISTINCT FROM
               'release:' || NEW.release_id::TEXT || '/item:' || item.ordinal::TEXT
            OR public_qualification.qualification_state IS DISTINCT FROM 'public_executable'
            OR public_qualification.public_executable IS DISTINCT FROM TRUE
            OR binding.created_by IS DISTINCT FROM NEW.actor_user_id
            OR binding.created_by_org_id IS DISTINCT FROM NEW.actor_org_id
            OR actor.status IS DISTINCT FROM 'active'
            OR actor_org.status IS DISTINCT FROM 'active'
            OR (COALESCE(NULLIF(actor.organization_role,''),actor.role)<>'platform_admin'
                AND (COALESCE(NULLIF(actor.organization_role,''),actor.role)<>'org_admin'
                     OR actor.org_id IS DISTINCT FROM NEW.customer_org_id))
         ))
         OR (COALESCE(item.operation_id,binding.operation_id) NOT IN
                ('openai.responses.v1','anthropic.messages.v1')
             AND binding.public_execution_binding_id IS NOT NULL)
         OR item.release_id IS NULL
       );
    IF binding_count<>0 THEN
        RAISE EXCEPTION 'model deployment release authoring completion requires bidirectional exact and authorized public bindings'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION preserve_model_deployment_lifecycle_receipt()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    audit_count BIGINT;
    release_count BIGINT;
    matched_release_count BIGINT;
    expected_release_count BIGINT;
    slot_count BIGINT;
    changed_release_count BIGINT;
    exact_event_count BIGINT;
    receipt_event_count BIGINT;
    qualification_count BIGINT;
    expected_qualification_count BIGINT;
    target_model_name TEXT;
    target_parent_release_id BIGINT;
    target_baseline_hash TEXT;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'model deployment lifecycle receipts are durable evidence'
            USING ERRCODE='55000';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'in_progress' THEN
            RAISE EXCEPTION 'model deployment lifecycle receipt must be claimed in progress'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.status='completed' OR NEW.status<>'completed'
       OR NEW.receipt_id IS DISTINCT FROM OLD.receipt_id
       OR NEW.contract_version IS DISTINCT FROM OLD.contract_version
       OR NEW.customer_org_id IS DISTINCT FROM OLD.customer_org_id
       OR NEW.actor_user_id IS DISTINCT FROM OLD.actor_user_id
       OR NEW.actor_org_id IS DISTINCT FROM OLD.actor_org_id
       OR NEW.operation_scope IS DISTINCT FROM OLD.operation_scope
       OR NEW.action IS DISTINCT FROM OLD.action
       OR NEW.idempotency_key_hash IS DISTINCT FROM OLD.idempotency_key_hash
       OR NEW.request_fingerprint IS DISTINCT FROM OLD.request_fingerprint
       OR NEW.server_operation_id IS DISTINCT FROM OLD.server_operation_id
       OR NEW.target_release_ref IS DISTINCT FROM OLD.target_release_ref
       OR NEW.expected_release_version IS DISTINCT FROM OLD.expected_release_version
       OR NEW.expected_slot_version IS DISTINCT FROM OLD.expected_slot_version
       OR NEW.requested_canary_basis_points IS DISTINCT FROM OLD.requested_canary_basis_points
       OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'model deployment lifecycle receipt identity is immutable'
            USING ERRCODE='55000';
    END IF;
    -- The completion update is the atomic publication point. Lock and verify
    -- the exact audit identity and all child evidence now; no permanent audit
    -- FK is retained because audit_logs has an independent retention policy.
    PERFORM audit.id
      FROM audit_logs audit
     WHERE audit.id=NEW.audit_log_id
     FOR SHARE;
    SELECT COUNT(*) INTO audit_count
      FROM audit_logs audit
     WHERE audit.id=NEW.audit_log_id
       AND audit.org_id=NEW.customer_org_id
       AND audit.user_id=NEW.actor_user_id
       AND audit.action='model_deployment.lifecycle.' || NEW.action
       AND audit.resource='model_deployment_release'
       AND audit.resource_id=NEW.target_release_id::TEXT
       AND audit.detail->>'receipt_id'=NEW.receipt_id::TEXT
       AND audit.detail->>'contract_version'=NEW.contract_version
       AND audit.detail->>'action'=NEW.action
       AND audit.detail->>'server_operation_id'=NEW.server_operation_id
       AND audit.detail->>'runtime_mutation'='bounded_route_assignment';
    IF audit_count<>1 THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires the exact audit row'
            USING ERRCODE='23514';
    END IF;

    expected_release_count := CASE WHEN NEW.action='activate_initial' THEN 1 ELSE 2 END;
    SELECT COUNT(*) INTO release_count
      FROM model_deployment_lifecycle_receipt_releases evidence
     WHERE evidence.receipt_id=NEW.receipt_id;
    IF release_count<>expected_release_count THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires exact release cardinality'
            USING ERRCODE='23514';
    END IF;

    -- Lock all source releases in a deterministic order before inspecting
    -- their current state. The receipt parent lock already prevents any new
    -- child append from crossing this completion publication point.
    PERFORM release.id
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_releases release ON release.id=evidence.release_id
     WHERE evidence.receipt_id=NEW.receipt_id
     ORDER BY release.id
     FOR SHARE OF release;

    SELECT release.model_name,release.parent_stable_release_id,release.baseline_hash
      INTO target_model_name,target_parent_release_id,target_baseline_hash
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_releases release ON release.id=evidence.release_id
     WHERE evidence.receipt_id=NEW.receipt_id
       AND evidence.release_role='target'
       AND evidence.release_id=NEW.target_release_id;
    IF NOT FOUND OR target_model_name IS NULL
       OR (NEW.action='activate_initial' AND target_parent_release_id IS NOT NULL)
       OR (NEW.action<>'activate_initial' AND target_parent_release_id IS NULL) THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires exact target identity'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO matched_release_count
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_releases release ON release.id=evidence.release_id
     WHERE evidence.receipt_id=NEW.receipt_id
       AND evidence.release_ref=release.release_ref
       AND evidence.candidate_hash=release.candidate_hash
       AND evidence.route_items_hash=release.route_items_hash
       AND evidence.after_stage=release.stage
       AND evidence.after_version=release.version
       AND release.candidate_version='unirouter.runtime-change-candidate.v2'
       AND release.public_model_id=NEW.public_model_id
       AND release.model_name=target_model_name
       AND release.route_items_sealed_at IS NOT NULL
       AND release.route_items_count>0
       AND (
         (
           evidence.release_role='target'
           AND evidence.release_id=NEW.target_release_id
           AND evidence.release_ref=NEW.target_release_ref
           AND evidence.before_version=NEW.expected_release_version
           AND evidence.after_version=NEW.expected_release_version+1
           AND (
             (NEW.action='activate_initial'
               AND evidence.before_stage='validated' AND evidence.after_stage='active')
             OR (NEW.action='start_canary'
               AND evidence.before_stage='validated' AND evidence.after_stage='canary')
             OR (NEW.action='promote'
               AND evidence.before_stage='canary' AND evidence.after_stage='active')
             OR (NEW.action='cancel_canary'
               AND evidence.before_stage='canary' AND evidence.after_stage='rolled_back')
             OR (NEW.action='rollback_stable'
               AND evidence.before_stage='active' AND evidence.after_stage='rolled_back')
           )
         )
         OR (
           evidence.release_role='stable_parent'
           AND NEW.action<>'activate_initial'
           AND evidence.release_id=target_parent_release_id
           AND target_baseline_hash=release.candidate_hash
           AND (
             (NEW.action IN ('start_canary','cancel_canary')
               AND evidence.release_id=NEW.before_stable_release_id
               AND evidence.release_id=NEW.after_stable_release_id
               AND evidence.before_stage='active' AND evidence.after_stage='active'
               AND evidence.after_version=evidence.before_version)
             OR (NEW.action='promote'
               AND evidence.release_id=NEW.before_stable_release_id
               AND evidence.before_stage='active' AND evidence.after_stage='rolled_back'
               AND evidence.after_version=evidence.before_version+1)
             OR (NEW.action='rollback_stable'
               AND evidence.release_id=NEW.after_stable_release_id
               AND evidence.before_stage='rolled_back' AND evidence.after_stage='active'
               AND evidence.after_version=evidence.before_version+1)
           )
         )
       );
    IF matched_release_count<>expected_release_count THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires exact release evidence'
            USING ERRCODE='23514';
    END IF;

    IF NEW.before_slot_version<>NEW.expected_slot_version
       OR NEW.after_slot_version<>NEW.expected_slot_version+1 THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires exact slot versions'
            USING ERRCODE='23514';
    END IF;

    -- A public model and model name must resolve to exactly one current slot;
    -- lock the complete identity boundary before checking its after image.
    PERFORM slot.model_name
      FROM runtime_change_slots slot
     WHERE slot.model_name=target_model_name OR slot.public_model_id=NEW.public_model_id
     ORDER BY slot.model_name
     FOR SHARE OF slot;
    SELECT COUNT(*) INTO slot_count
      FROM runtime_change_slots slot
     WHERE slot.model_name=target_model_name OR slot.public_model_id=NEW.public_model_id;
    IF slot_count<>1 OR NOT EXISTS (
        SELECT 1
          FROM runtime_change_slots slot
         WHERE slot.model_name=target_model_name
           AND slot.public_model_id=NEW.public_model_id
           AND slot.stable_release_id IS NOT DISTINCT FROM NEW.after_stable_release_id
           AND slot.canary_release_id IS NOT DISTINCT FROM NEW.after_canary_release_id
           AND slot.canary_basis_points=NEW.after_canary_basis_points
           AND slot.slot_version=NEW.after_slot_version
    ) THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires the exact live after slot'
            USING ERRCODE='23514';
    END IF;

    -- Every changed release has one exact lifecycle event tied to this
    -- receipt. Unchanged stable-parent evidence intentionally reuses its
    -- historical version and therefore has no new event.
    PERFORM event.id
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_events event
        ON event.release_id=evidence.release_id
       AND event.sequence=evidence.after_version
     WHERE evidence.receipt_id=NEW.receipt_id
       AND evidence.after_version=evidence.before_version+1
     ORDER BY event.release_id,event.sequence
     FOR SHARE OF event;
    SELECT COUNT(*) INTO changed_release_count
      FROM model_deployment_lifecycle_receipt_releases evidence
     WHERE evidence.receipt_id=NEW.receipt_id
       AND evidence.after_version=evidence.before_version+1;
    SELECT COUNT(*) INTO exact_event_count
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_events event
        ON event.release_id=evidence.release_id
       AND event.sequence=evidence.after_version
     WHERE evidence.receipt_id=NEW.receipt_id
       AND evidence.after_version=evidence.before_version+1
       AND event.actor_user_id=NEW.actor_user_id
       AND event.expected_version=evidence.before_version
       AND event.resulting_version=evidence.after_version
       AND event.from_stage=evidence.before_stage
       AND event.to_stage=evidence.after_stage
       AND event.action=CASE
           WHEN evidence.after_stage='rolled_back' THEN 'rollback'
           WHEN evidence.before_stage='validated' AND evidence.after_stage='canary' THEN 'advance'
           ELSE 'promote'
       END
       AND event.detail->>'lifecycle_receipt_id'=NEW.receipt_id::TEXT
       AND event.detail->>'canonical_lifecycle'='true'
       AND event.detail->>'public_model_id'=NEW.public_model_id::TEXT
       AND event.detail->>'route_items_hash'=evidence.route_items_hash
       AND event.detail->>'resulting_slot_version'=NEW.after_slot_version::TEXT
       AND (
         (NEW.action='activate_initial'
           AND event.detail->>'initial_activation'='true'
           AND event.detail->>'slot_precondition'='absent')
         OR
         (NEW.action<>'activate_initial'
           AND event.detail->>'expected_slot_version'=NEW.before_slot_version::TEXT)
       );
    SELECT COUNT(*) INTO receipt_event_count
      FROM runtime_change_events event
     WHERE event.detail->>'lifecycle_receipt_id'=NEW.receipt_id::TEXT;
    IF changed_release_count<>(CASE
           WHEN NEW.action IN ('promote','rollback_stable') THEN 2 ELSE 1 END)
       OR exact_event_count<>changed_release_count
       OR receipt_event_count<>changed_release_count THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires exact lifecycle events'
            USING ERRCODE='23514';
    END IF;

    -- Route items and every mutable row that feeds the M75 qualification view
    -- are locked in a stable order before the evidence equality check.
    PERFORM item.release_id
      FROM model_deployment_lifecycle_receipt_releases release_evidence
      JOIN runtime_route_revision_items item ON item.release_id=release_evidence.release_id
     WHERE release_evidence.receipt_id=NEW.receipt_id
     ORDER BY item.release_id,item.ordinal
     FOR SHARE OF item;
    PERFORM deployment.id
      FROM model_deployment_lifecycle_receipt_releases release_evidence
      JOIN runtime_route_revision_items item ON item.release_id=release_evidence.release_id
      JOIN model_deployments deployment ON deployment.id=item.deployment_id
      JOIN provider_connections connection ON connection.id=deployment.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=item.deployment_id
       AND operation.operation_id=item.operation_id
     WHERE release_evidence.receipt_id=NEW.receipt_id
     ORDER BY deployment.id,operation.operation_id,item.release_id,item.ordinal
     FOR SHARE OF deployment,connection,operation;

    SELECT COUNT(*) INTO expected_qualification_count
      FROM model_deployment_lifecycle_receipt_releases release_evidence
      JOIN runtime_route_revision_items item ON item.release_id=release_evidence.release_id
     WHERE release_evidence.receipt_id=NEW.receipt_id;
    SELECT COUNT(*) INTO qualification_count
      FROM model_deployment_lifecycle_receipt_qualifications
     WHERE receipt_id=NEW.receipt_id;
    IF qualification_count<>expected_qualification_count OR EXISTS (
        SELECT 1
          FROM model_deployment_lifecycle_receipt_releases release_evidence
          JOIN runtime_route_revision_items item ON item.release_id=release_evidence.release_id
          JOIN model_deployments deployment ON deployment.id=item.deployment_id
          JOIN provider_connections connection ON connection.id=deployment.connection_id
          JOIN model_deployment_operations operation
            ON operation.deployment_id=item.deployment_id
           AND operation.operation_id=item.operation_id
          LEFT JOIN model_operation_execution_qualifications qualification
            ON qualification.deployment_id=item.deployment_id
           AND qualification.operation_id=item.operation_id
          LEFT JOIN model_deployment_lifecycle_receipt_qualifications evidence
            ON evidence.receipt_id=release_evidence.receipt_id
           AND evidence.release_id=item.release_id
           AND evidence.release_role=release_evidence.release_role
           AND evidence.ordinal=item.ordinal
         WHERE release_evidence.receipt_id=NEW.receipt_id
           AND (
             evidence.receipt_id IS NULL
             OR evidence.deployment_id IS DISTINCT FROM item.deployment_id
             OR evidence.operation_id IS DISTINCT FROM item.operation_id
             OR evidence.expected_connection_revision IS DISTINCT FROM item.expected_connection_revision
             OR evidence.expected_deployment_revision IS DISTINCT FROM item.expected_deployment_revision
             OR evidence.expected_operation_revision IS DISTINCT FROM item.expected_operation_revision
             OR evidence.public_path IS DISTINCT FROM item.public_path
             OR evidence.executor_contract_version IS DISTINCT FROM item.executor_contract_version
             OR evidence.connection_id IS DISTINCT FROM deployment.connection_id
             OR evidence.current_connection_revision IS DISTINCT FROM connection.config_revision
             OR evidence.current_deployment_revision IS DISTINCT FROM deployment.config_revision
             OR evidence.current_operation_revision IS DISTINCT FROM operation.revision
             OR (
               item.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
               AND (evidence.qualification_required OR NOT evidence.qualified
                    OR evidence.qualification_state<>'not_required'
                    OR evidence.approval_id IS NOT NULL OR evidence.probe_id IS NOT NULL)
             )
             OR (
               item.operation_id IN ('openai.responses.v1','anthropic.messages.v1')
               AND (NOT evidence.qualification_required
                    OR evidence.qualified IS DISTINCT FROM COALESCE(qualification.qualified,FALSE)
                    OR evidence.qualification_state IS DISTINCT FROM COALESCE(qualification.qualification_state,'qualification_missing')
                    OR evidence.approval_id IS DISTINCT FROM qualification.approval_id
                    OR evidence.probe_id IS DISTINCT FROM qualification.probe_id
                    OR evidence.https_origin IS DISTINCT FROM qualification.https_origin
                    OR (COALESCE(qualification.qualified,FALSE) AND (
                         evidence.public_path IS DISTINCT FROM qualification.public_path
                         OR evidence.executor_contract_version IS DISTINCT FROM qualification.executor_contract_version))
                    OR evidence.approved_connection_revision IS DISTINCT FROM qualification.approved_connection_revision
                    OR evidence.approved_deployment_revision IS DISTINCT FROM qualification.approved_deployment_revision
                    OR evidence.approved_operation_revision IS DISTINCT FROM qualification.approved_operation_revision)
             )
           )
    ) THEN
        RAISE EXCEPTION 'model deployment lifecycle completion requires exact qualification evidence'
            USING ERRCODE='23514';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM model_deployment_lifecycle_receipt_qualifications evidence
         WHERE evidence.receipt_id=NEW.receipt_id
           AND evidence.qualification_required
           AND evidence.qualified IS DISTINCT FROM TRUE
           AND (
             (NEW.action IN ('activate_initial','start_canary','promote')
               AND evidence.release_role='target')
             OR (NEW.action='rollback_stable'
               AND evidence.release_role='stable_parent')
           )
    ) THEN
        RAISE EXCEPTION 'model deployment lifecycle authority expansion requires current production qualification'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE model_deployment_lifecycle_receipt_qualifications
    DROP CONSTRAINT model_deployment_lifecycle_receipt_qualifications_shape_check,
    ADD CONSTRAINT model_deployment_lifecycle_receipt_qualifications_shape_check CHECK (
        ordinal>=0
        AND operation_id=BTRIM(operation_id) AND BTRIM(operation_id)<>''
        AND qualification_state=BTRIM(qualification_state) AND BTRIM(qualification_state)<>''
        AND expected_connection_revision>0
        AND expected_deployment_revision>0
        AND expected_operation_revision>0
        AND (
            (NOT qualification_required AND qualified
                AND qualification_state='not_required'
                AND approval_id IS NULL AND probe_id IS NULL)
            OR
            (qualification_required
                AND operation_id IN ('openai.responses.v1','anthropic.messages.v1')
                AND connection_id IS NOT NULL
                AND current_connection_revision IS NOT NULL
                AND current_deployment_revision IS NOT NULL
                AND current_operation_revision IS NOT NULL
                AND (qualified=(qualification_state='production_qualified'))
                AND (
                    NOT qualified
                    OR (
                        approval_id IS NOT NULL AND probe_id IS NOT NULL
                        AND https_origin IS NOT NULL AND public_path IS NOT NULL
                        AND executor_contract_version IS NOT NULL
                        AND approved_connection_revision IS NOT NULL
                        AND approved_deployment_revision IS NOT NULL
                        AND approved_operation_revision IS NOT NULL
                    )
                ))
        )
    );

ALTER TABLE model_operation_production_approvals
    DROP CONSTRAINT model_operation_production_approvals_identity_check,
    ADD CONSTRAINT model_operation_production_approvals_identity_check CHECK (
        operation_id IN ('openai.responses.v1','anthropic.messages.v1')
        AND public_path=BTRIM(public_path)
        AND protocol_profile_id=BTRIM(protocol_profile_id)
        AND protocol_profile_version=BTRIM(protocol_profile_version)
        AND executor_contract_version=BTRIM(executor_contract_version)
        AND model_operation_https_origin(https_origin) IS NOT NULL
        AND https_origin=model_operation_https_origin(https_origin)
        AND connection_revision>0 AND deployment_revision>0
        AND probe_operation_revision>0 AND source_operation_revision>0
        AND approved_operation_revision=source_operation_revision+1
        AND probe_operation_revision+1=source_operation_revision
        AND credential_binding_id>0 AND credential_state_revision>0
        AND credential_link_created_at<=approved_at
        AND customer_org_id>0 AND approved_by>0 AND approved_by_org_id>0
        AND reason=BTRIM(reason) AND BTRIM(reason)<>''
        AND reason !~ E'[\\x00\\r\\n]'
        AND source_kind='admin.model_operation_production_approval.v1'
        AND source_ref='probe:' || probe_id::TEXT
    );

DROP TRIGGER model_operation_public_execution_contract_extensions_immutable ON model_operation_public_execution_contract_extensions;

DELETE FROM model_operation_public_execution_contract_extensions WHERE introduced_migration='000135_canonical_media_job_authority';

ALTER TABLE model_operation_public_execution_contract_extensions
    DROP CONSTRAINT model_operation_public_execution_contract_extensions_identity_check,
    ADD CONSTRAINT model_operation_public_execution_contract_extensions_identity_check CHECK (
        charge_contract_version='unirouter.provider-final-token-charge.v1'
        AND idempotency_contract_version='unirouter.public-at-most-once.v1'
        AND (
            (
                introduced_migration='000077_public_model_execution_authority'
                AND delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND executor_contract_version='unirouter.endpoint-profile-nonstream-executor.v1'
                AND (
                    (operation_id='openai.responses.v1'
                     AND protocol_profile_id='unirouter.openai.responses.text.v1'
                     AND protocol_profile_version='1.0.0')
                    OR
                    (operation_id='anthropic.messages.v1'
                     AND protocol_profile_id='unirouter.anthropic.messages.text.v1'
                     AND protocol_profile_version='1.0.0')
                )
            )
            OR
            (
                introduced_migration='000086_exact_public_execution_bindings'
                AND protocol_profile_version='1.0.0'
                AND (
                    (
                        operation_id='openai.responses.v1'
                        AND protocol_profile_id='unirouter.openai.responses.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-responses-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-responses-delivery.v2'
                    )
                    OR
                    (
                        operation_id='anthropic.messages.v1'
                        AND protocol_profile_id='unirouter.anthropic.messages.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-messages-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-anthropic-messages-delivery.v2'
                    )
                )
            )
            OR
            (
                introduced_migration='000087_anthropic_messages_tools_public_binding'
                AND operation_id='anthropic.messages.v1'
                AND protocol_profile_id='unirouter.anthropic.messages.text-tools.v3'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-messages-tools-executor.v1'
                AND delivery_contract_version='unirouter.public-anthropic-messages-tools-delivery.v1'
            )
            OR
            (
                introduced_migration='000132_openai_responses_client_tools'
                AND operation_id='openai.responses.v1'
                AND protocol_profile_id='unirouter.openai.responses.text-tools.v3'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-responses-tools-executor.v1'
                AND delivery_contract_version='unirouter.public-responses-tools-delivery.v1'
            )
            OR
            (
                introduced_migration='000134_native_inline_jpeg_public_bindings'
                AND operation_id='openai.responses.v1'
                AND public_path='/v1/responses'
                AND protocol_profile_id='unirouter.openai.responses.inline-jpeg.v1'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-responses-inline-jpeg-executor.v1'
                AND delivery_contract_version='unirouter.public-responses-tools-delivery.v1'
            )
            OR
            (
                introduced_migration='000134_native_inline_jpeg_public_bindings'
                AND operation_id='anthropic.messages.v1'
                AND public_path='/v1/messages'
                AND protocol_profile_id='unirouter.anthropic.messages.inline-jpeg.v1'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-messages-inline-jpeg-executor.v1'
                AND delivery_contract_version='unirouter.public-anthropic-messages-tools-delivery.v1'
            )
        )
    );

CREATE TRIGGER model_operation_public_execution_contract_extensions_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_public_execution_contract_extensions
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_public_execution_contract_mutation();

DROP TRIGGER model_operation_execution_support_bindings_immutable ON model_operation_execution_support_bindings;

DELETE FROM model_operation_execution_support_bindings WHERE introduced_migration='000135_canonical_media_job_authority';
DROP INDEX model_operation_execution_support_migration_unique;
ALTER TABLE model_operation_execution_support_bindings
    ADD CONSTRAINT model_operation_execution_support_migration_unique UNIQUE(operation_id,introduced_migration);

CREATE TRIGGER model_operation_execution_support_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_execution_support_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_execution_support_mutation();

CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    IF ledger[cardinality(ledger)]='000134_native_inline_jpeg_public_bindings' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000133_media_charge_source_facts' THEN
        RAISE EXCEPTION 'budget coverage requires exact M134 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000133_media_charge_source_facts' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000132_openai_responses_client_tools' THEN
        RAISE EXCEPTION 'budget coverage requires exact M133 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000132_openai_responses_client_tools' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000131_customer_standard_pricing' THEN
        RAISE EXCEPTION 'budget coverage requires exact M132 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000131_customer_standard_pricing' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000130_upgrade_admission_recovery' THEN
        RAISE EXCEPTION 'budget coverage requires exact M131 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000130_upgrade_admission_recovery' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000129_upstream_observation_restore_namespace' THEN
        RAISE EXCEPTION 'budget coverage requires exact M130 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000129_upstream_observation_restore_namespace' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000128_upstream_release_observation' THEN
        RAISE EXCEPTION 'budget coverage requires exact M129 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000128_upstream_release_observation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000127_xinsight_communication_policy' THEN
        RAISE EXCEPTION 'budget coverage requires exact M128 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000127_xinsight_communication_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000126_commercial_license_retirement' THEN
        RAISE EXCEPTION 'budget coverage requires exact M127 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000126_commercial_license_retirement' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000125_xinsight_control_credential_lifecycle' THEN
        RAISE EXCEPTION 'budget coverage requires exact M126 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000125_xinsight_control_credential_lifecycle' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000124_xinsight_receiver_adoption' THEN
        RAISE EXCEPTION 'budget coverage requires exact M125 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000124_xinsight_receiver_adoption' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000123_xinsight_policy_delegation' THEN
        RAISE EXCEPTION 'budget coverage requires exact M124 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000123_xinsight_policy_delegation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000122_scoped_content_policy'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-4] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M123 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-5];
    ELSIF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M122 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-4];
    ELSIF ledger[cardinality(ledger)]='000121_gateway_pre_dispatch_terminal' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M121 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-3];
    ELSIF ledger[cardinality(ledger)]='000120_unpublished_content_reclamation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M120 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-2];
    ELSIF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
      certified_ledger := ledger[:cardinality(ledger)-1];
    END IF;
    IF c.state IS DISTINCT FROM 'certified' OR c.support_manifest_sha256 IS DISTINCT FROM encode(sha256(c.support_manifest_bytes),'hex')
       OR certified_ledger[cardinality(certified_ledger)] IS DISTINCT FROM '000118_source_budget_snapshots'
       OR c.schema_ledger_sha256 IS DISTINCT FROM encode(sha256(convert_to(array_to_string(certified_ledger,E'\n'),'UTF8')),'hex') THEN
      RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction'; END IF;
    m := convert_from(c.support_manifest_bytes,'UTF8')::jsonb;
    IF COALESCE((m->'schema_heads' ? '000118_source_budget_snapshots'),false)=false OR COALESCE((m->'implementations' @>
       '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb),false)=false THEN
      RAISE EXCEPTION 'budget coverage support proof lacks the writer or snapshot contract'; END IF;
    RETURN NEW;
END $$;

COMMIT;
