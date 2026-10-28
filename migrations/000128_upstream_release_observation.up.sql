-- Maintenance boundary: stop and retire old writers before applying M128.
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations, runtime_change_slots, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000127_xinsight_communication_policy' THEN
  RAISE EXCEPTION 'M128 requires exact M127 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped' OR ingress_state<>'absent')
 OR EXISTS(SELECT 1 FROM runtime_ingress_changes WHERE status IN ('pending','ambiguous')) THEN
  RAISE EXCEPTION 'M128 requires stopped retired writers'; END IF;
END $$;

-- Keep the same bounded UTF-8 text semantics as the typed observation owner.
CREATE FUNCTION valid_upstream_release_text(v TEXT,n INTEGER,required BOOLEAN) RETURNS BOOLEAN LANGUAGE SQL IMMUTABLE AS $$
 SELECT v IS NOT NULL AND (NOT required OR v<>'') AND octet_length(v)<=n
 AND v=btrim(v,U&' \0009\000A\000B\000C\000D\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
 AND strpos(v,chr(10))=0 AND strpos(v,chr(13))=0
$$;

CREATE FUNCTION valid_upstream_release_observation(v JSONB) RETURNS BOOLEAN LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE sc JSONB; cl JSONB; pr JSONB; k TEXT; stamp TIMESTAMPTZ; host TEXT;
BEGIN
 IF jsonb_typeof(v) IS DISTINCT FROM 'object' OR NOT(v ?& ARRAY['schema_version','identity','scope','claim','provenance','checked_at','recorded_by','declaration','remote_weights'])
 OR v-ARRAY['schema_version','identity','scope','claim','provenance','checked_at','recorded_by','declaration','remote_weights']<>'{}'::jsonb THEN RETURN FALSE; END IF;
 FOREACH k IN ARRAY ARRAY['schema_version','identity','checked_at','declaration','remote_weights'] LOOP
  IF jsonb_typeof(v->k) IS DISTINCT FROM 'string' THEN RETURN FALSE; END IF;
 END LOOP;
 IF v->>'schema_version'<>'unirouter.upstream-release-observation.v1' OR v->>'identity' !~ '^uor_[0-9a-f]{32}$'
 OR v->>'declaration'<>'declared' OR v->>'remote_weights'<>'unpinned'
 OR jsonb_typeof(v->'recorded_by') IS DISTINCT FROM 'number' OR v->>'recorded_by' !~ '^[1-9][0-9]*$'
 THEN RETURN FALSE; END IF;
 IF v->>'checked_at' !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$' THEN RETURN FALSE; END IF;
 stamp=(v->>'checked_at')::TIMESTAMPTZ; IF NOT isfinite(stamp) THEN RETURN FALSE; END IF;
 sc=v->'scope';cl=v->'claim';pr=v->'provenance';
 IF jsonb_typeof(sc) IS DISTINCT FROM 'object' OR jsonb_typeof(cl) IS DISTINCT FROM 'object' OR jsonb_typeof(pr) IS DISTINCT FROM 'object'
 OR NOT(sc ?& ARRAY['customer_org_id','deployment_id','connection_id','provider','base_url','api_model'])
 OR sc-ARRAY['customer_org_id','deployment_id','connection_id','provider','base_url','api_model']<>'{}'::jsonb
 OR NOT(cl ? 'release_label') OR cl-ARRAY['release_label','announced_on']<>'{}'::jsonb
 OR NOT(pr ?& ARRAY['source_kind','source_url']) OR pr-ARRAY['source_kind','source_url','source_revision','source_sha256']<>'{}'::jsonb THEN RETURN FALSE; END IF;
 FOREACH k IN ARRAY ARRAY['customer_org_id','deployment_id','connection_id'] LOOP
  IF jsonb_typeof(sc->k) IS DISTINCT FROM 'number' OR sc->>k !~ '^[1-9][0-9]*$' THEN RETURN FALSE; END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['provider','base_url','api_model'] LOOP
  IF jsonb_typeof(sc->k) IS DISTINCT FROM 'string' OR NOT valid_upstream_release_text(sc->>k,CASE k WHEN 'provider' THEN 100 WHEN 'api_model' THEN 200 ELSE 2048 END,TRUE) THEN RETURN FALSE; END IF;
 END LOOP;
 IF jsonb_typeof(cl->'release_label') IS DISTINCT FROM 'string' OR NOT valid_upstream_release_text(cl->>'release_label',160,TRUE) THEN RETURN FALSE; END IF;
 IF cl ? 'announced_on' THEN
  IF jsonb_typeof(cl->'announced_on') IS DISTINCT FROM 'string' OR cl->>'announced_on' !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
   OR to_char((cl->>'announced_on')::DATE,'YYYY-MM-DD')<>cl->>'announced_on' THEN RETURN FALSE; END IF;
 END IF;
 IF jsonb_typeof(pr->'source_kind') IS DISTINCT FROM 'string' OR pr->>'source_kind'<>'official_announcement'
 OR jsonb_typeof(pr->'source_url') IS DISTINCT FROM 'string' OR NOT valid_upstream_release_text(pr->>'source_url',2048,TRUE)
 OR pr->>'source_url' !~ '^https://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$' THEN RETURN FALSE; END IF;
 host=split_part(substr(pr->>'source_url',9),'/',1);
 IF pr->>'source_url' ~ '[[:cntrl:]]' OR host ~ E'["<>^`{|}\\\\]'
 OR strpos(host,'%')>0 OR host='[]'
 OR (left(host,1)='[' AND host !~ '^\[[^]]+\](:[0-9]*)?$')
 OR (left(host,1)<>'[' AND host !~ '^[^:]+(:[0-9]*)?$')
 OR strpos(regexp_replace(pr->>'source_url','%[0-9a-fA-F]{2}','','g'),'%')>0 THEN RETURN FALSE; END IF;
 IF pr ? 'source_revision'  AND (jsonb_typeof(pr->'source_revision') IS DISTINCT FROM 'string' OR NOT valid_upstream_release_text(pr->>'source_revision',255,FALSE)) THEN RETURN FALSE; END IF;
 IF pr ? 'source_sha256' AND (jsonb_typeof(pr->'source_sha256') IS DISTINCT FROM 'string' OR pr->>'source_sha256' !~ '^[0-9a-f]{64}$') THEN RETURN FALSE; END IF;
 RETURN TRUE;
EXCEPTION WHEN OTHERS THEN RETURN FALSE;
END $$;

ALTER TABLE model_deployments
 ADD COLUMN upstream_release_observation JSONB,
 ADD COLUMN upstream_observation_revision BIGINT NOT NULL DEFAULT 0,
 ADD CONSTRAINT model_deployment_upstream_observation_shape CHECK (
  (upstream_release_observation IS NULL AND upstream_observation_revision=0)
  OR (upstream_release_observation IS NOT NULL AND upstream_observation_revision>0
   AND valid_upstream_release_observation(upstream_release_observation)) IS TRUE
 );

CREATE FUNCTION preserve_upstream_release_observation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c RECORD; actor RECORD; old_semantic JSONB; new_semantic JSONB;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.upstream_release_observation IS NOT NULL OR NEW.upstream_observation_revision<>0 THEN
   RAISE EXCEPTION 'record a declaration after creating its exact deployment' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.upstream_release_observation IS NOT DISTINCT FROM OLD.upstream_release_observation
    AND NEW.upstream_observation_revision=OLD.upstream_observation_revision THEN RETURN NEW; END IF;
 IF NEW.upstream_release_observation IS NULL OR NEW.upstream_observation_revision<>OLD.upstream_observation_revision+1
    OR NEW.config_revision<>OLD.config_revision OR NEW.circuit_revision<>OLD.circuit_revision THEN
  RAISE EXCEPTION 'declaration requires a separate monotonic revision; known scope cannot be cleared' USING ERRCODE='55000';
 END IF;
 SELECT provider,base_url INTO c FROM provider_connections WHERE id=NEW.connection_id FOR SHARE;
 IF NOT FOUND OR NEW.upstream_release_observation->'scope' IS DISTINCT FROM jsonb_build_object(
  'customer_org_id',NEW.customer_org_id,'deployment_id',NEW.id,'connection_id',NEW.connection_id,
  'provider',c.provider,'base_url',c.base_url,'api_model',NEW.upstream_model) THEN
  RAISE EXCEPTION 'declaration scope is not this deployment endpoint' USING ERRCODE='55000';
 END IF;
 SELECT u.org_id,u.organization_role,u.status,o.status AS org_status INTO actor
 FROM users u JOIN orgs o ON o.id=u.org_id
 WHERE u.id=(NEW.upstream_release_observation->>'recorded_by')::BIGINT FOR SHARE OF u,o;
 IF NOT FOUND OR actor.status<>'active' OR actor.org_status<>'active'
    OR NOT (actor.organization_role='platform_admin' OR
      (actor.organization_role='org_admin' AND actor.org_id=NEW.customer_org_id)) THEN
  RAISE EXCEPTION 'declaration requires a current scoped administrator' USING ERRCODE='42501';
 END IF;
 old_semantic=jsonb_build_array(OLD.upstream_release_observation->'scope',OLD.upstream_release_observation->'claim');
 new_semantic=jsonb_build_array(NEW.upstream_release_observation->'scope',NEW.upstream_release_observation->'claim');
 IF OLD.upstream_release_observation IS NOT NULL AND
    ((old_semantic=new_semantic) IS DISTINCT FROM
     ((OLD.upstream_release_observation->>'identity')=(NEW.upstream_release_observation->>'identity'))) THEN
  RAISE EXCEPTION 'recheck retains identity; changed declaration creates a new epoch' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_deployment_upstream_observation_guard
 BEFORE INSERT OR UPDATE OF upstream_release_observation,upstream_observation_revision
 ON model_deployments FOR EACH ROW EXECUTE FUNCTION preserve_upstream_release_observation();

-- Prospective append guard only. Existing qualification views and immutable
-- execution decisions deliberately retain their pre-M128 safety semantics.
CREATE FUNCTION enforce_new_approval_upstream_scope() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE current_observation JSONB; captured JSONB; actual_scope JSONB;
BEGIN
 SELECT d.upstream_release_observation,jsonb_build_object('customer_org_id',d.customer_org_id,'deployment_id',d.id,'connection_id',d.connection_id,'provider',c.provider,'base_url',c.base_url,'api_model',d.upstream_model)
 INTO current_observation,actual_scope FROM model_deployments d JOIN provider_connections c ON c.id=d.connection_id
 WHERE d.id=NEW.deployment_id AND d.customer_org_id=NEW.customer_org_id FOR UPDATE OF d,c;
 IF TG_TABLE_NAME='model_capability_routing_approvals' THEN
  SELECT request_summary->'upstream_release_observation' INTO captured FROM model_capability_probes
   WHERE capability_probe_id=NEW.capability_probe_id AND deployment_id=NEW.deployment_id
    AND customer_org_id=NEW.customer_org_id;
 ELSE
  SELECT request_summary->'upstream_release_observation' INTO captured FROM model_deployment_probes
   WHERE probe_id=NEW.probe_id AND deployment_id=NEW.deployment_id;
 END IF;
 IF current_observation IS NULL AND captured IS NOT NULL AND captured<>'null'::jsonb THEN
  RAISE EXCEPTION 'new approval cannot adopt an unrecorded observation' USING ERRCODE='55000';
 END IF;
 IF current_observation IS NOT NULL AND (
   current_observation->'scope' IS DISTINCT FROM actual_scope OR captured IS NULL OR captured='null'::jsonb OR
   captured->>'identity' IS DISTINCT FROM current_observation->>'identity' OR
   captured->'scope' IS DISTINCT FROM current_observation->'scope' OR
   captured->'claim' IS DISTINCT FROM current_observation->'claim') THEN
  RAISE EXCEPTION 'new approval evidence does not cover the recorded upstream declaration' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_capability_upstream_scope_guard BEFORE INSERT ON model_capability_routing_approvals
 FOR EACH ROW EXECUTE FUNCTION enforce_new_approval_upstream_scope();
CREATE TRIGGER model_operation_upstream_scope_guard BEFORE INSERT ON model_operation_production_approvals
 FOR EACH ROW EXECUTE FUNCTION enforce_new_approval_upstream_scope();

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

CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
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
