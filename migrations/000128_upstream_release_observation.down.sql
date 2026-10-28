BEGIN;
LOCK TABLE runtime_generations,runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000128_upstream_release_observation'
 OR EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped' OR ingress_state<>'absent')
 OR EXISTS(SELECT 1 FROM runtime_ingress_changes WHERE status IN ('pending','ambiguous')) THEN
  RAISE EXCEPTION 'M128 down requires exact head and stopped retired writers'; END IF;
END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM model_deployments WHERE upstream_release_observation IS NOT NULL)
 OR EXISTS(SELECT 1 FROM model_capability_probes WHERE request_summary->'upstream_release_observation' IS NOT NULL
   AND request_summary->'upstream_release_observation'<>'null'::jsonb)
 OR EXISTS(SELECT 1 FROM model_deployment_probes WHERE request_summary->'upstream_release_observation' IS NOT NULL
   AND request_summary->'upstream_release_observation'<>'null'::jsonb) THEN
  RAISE EXCEPTION 'retained upstream declarations/evidence forbid a destructive down migration' USING ERRCODE='55000';
 END IF;
END $$;
DROP TRIGGER model_capability_upstream_scope_guard ON model_capability_routing_approvals;
DROP TRIGGER model_operation_upstream_scope_guard ON model_operation_production_approvals;
DROP FUNCTION enforce_new_approval_upstream_scope();
DROP TRIGGER model_deployment_upstream_observation_guard ON model_deployments;
DROP FUNCTION preserve_upstream_release_observation();
ALTER TABLE model_deployments DROP CONSTRAINT model_deployment_upstream_observation_shape,
 DROP COLUMN upstream_release_observation,DROP COLUMN upstream_observation_revision;
DROP FUNCTION valid_upstream_release_observation(JSONB);
DROP FUNCTION valid_upstream_release_text(TEXT,INTEGER,BOOLEAN);
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
                        THEN (SELECT COUNT(*) FROM jsonb_object_keys(selection.value))<>2
                        ELSE FALSE END
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
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
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
