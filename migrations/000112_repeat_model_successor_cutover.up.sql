BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);
LOCK TABLE model_deployment_lifecycle_receipts,model_successor_cutover_receipts,
    model_successor_cutover_receipt_model_sets,model_aliases,model_registry,
    model_sets,model_set_items,runtime_change_releases,runtime_change_slots,
    audit_logs,schema_migrations IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
 IF (SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1) IS DISTINCT FROM '000111_minimax_m27_stream_function_tools_qualification' THEN
  RAISE EXCEPTION 'repeat successor schema predecessor mismatch';
 END IF;
END; $$;
ALTER TABLE model_successor_cutover_receipts DROP CONSTRAINT model_successor_cutover_receipts_identity_check,
    ADD CONSTRAINT model_successor_cutover_receipts_identity_check CHECK (
        stable_request_model=BTRIM(stable_request_model) AND BTRIM(stable_request_model)<>''
        AND predecessor_public_model_id<>successor_public_model_id
        AND successor_model_name=BTRIM(successor_model_name) AND BTRIM(successor_model_name)<>''
        AND successor_model_name<>stable_request_model
        AND predecessor_after_lifecycle='retired' AND NOT predecessor_after_approved_catalog
        AND successor_before_lifecycle='staging' AND NOT successor_before_approved_catalog
        AND successor_after_lifecycle='ga' AND successor_after_approved_catalog
        AND alias_after_target_model=successor_model_name
        AND alias_after_status='active' AND alias_after_is_primary
        AND model_set_count>=0
        AND jsonb_typeof(model_sets_before)='array'
        AND jsonb_typeof(model_sets_after)='array'
        AND (
            (contract_version='unirouter.model-successor-cutover-receipt.v1'
             AND predecessor_model_name=stable_request_model AND NOT alias_before_exists
             AND predecessor_before_lifecycle IN ('ga','deprecated')
             AND predecessor_before_approved_catalog
             AND predecessor_authority_mode IS NULL
             AND predecessor_authority_snapshot IS NULL)
            OR
            (contract_version='unirouter.model-successor-cutover-receipt.v2'
             AND predecessor_model_name=stable_request_model AND NOT alias_before_exists
             AND predecessor_before_lifecycle='staging'
             AND NOT predecessor_before_approved_catalog
             AND predecessor_authority_mode='never_activated_staging'
             AND predecessor_authority_snapshot='{
                "runtime_slot_count": 0,
                "sealed_or_executable_release_count": 0,
                "active_alias_count": 0,
                "completed_successor_receipt_count": 0
             }'::jsonb)
            OR
            (contract_version='unirouter.model-successor-cutover-receipt.v3'
             AND predecessor_model_name<>stable_request_model
             AND predecessor_model_name<>successor_model_name
             AND predecessor_model_name=BTRIM(predecessor_model_name) AND predecessor_model_name<>''
             AND predecessor_before_lifecycle='ga' AND predecessor_before_approved_catalog
             AND alias_before_exists AND predecessor_authority_mode='primary_alias'
             AND predecessor_authority_snapshot IS NOT NULL
             AND jsonb_typeof(predecessor_authority_snapshot)='object'
             AND predecessor_authority_snapshot ?& ARRAY['primary_alias_id','slot_version','stable_release_id','release_version']
             AND (predecessor_authority_snapshot - ARRAY['primary_alias_id','slot_version','stable_release_id','release_version']::text[])='{}'::jsonb
             AND predecessor_authority_snapshot->>'primary_alias_id' ~ '^[1-9][0-9]*$'
             AND predecessor_authority_snapshot->>'slot_version' ~ '^[1-9][0-9]*$'
             AND predecessor_authority_snapshot->>'stable_release_id' ~ '^[1-9][0-9]*$'
             AND predecessor_authority_snapshot->>'release_version' ~ '^[1-9][0-9]*$')
        )
    );
ALTER TABLE model_deployment_lifecycle_receipts
    DROP CONSTRAINT model_deployment_lifecycle_receipts_identity_check,
    ADD CONSTRAINT model_deployment_lifecycle_receipts_identity_check CHECK (
        (
        (
            contract_version='unirouter.model-deployment-lifecycle-receipt.v1'
            AND action IN (
                'activate_initial','start_canary','promote','cancel_canary',
                'rollback_stable','cutover_legacy','rollback_legacy'
            )
            AND operation_scope='model_deployment.lifecycle.' || action || '.v1'
            AND successor_predecessor_public_model_id IS NULL
            AND successor_public_model_id IS NULL
            AND successor_stable_request_model IS NULL
        ) OR (
            contract_version='unirouter.model-deployment-lifecycle-receipt.v2'
            AND action='activate_initial'
            AND operation_scope='model_deployment.lifecycle.activate_initial.successor.v1'
            AND successor_predecessor_public_model_id IS NOT NULL
            AND successor_public_model_id IS NOT NULL
            AND successor_predecessor_public_model_id<>successor_public_model_id
            AND successor_stable_request_model=BTRIM(successor_stable_request_model)
            AND BTRIM(successor_stable_request_model)<>''
            AND successor_stable_request_model !~ E'[\\x00\\r\\n]'
        ) OR (
            contract_version='unirouter.model-deployment-lifecycle-receipt.v3'
            AND action='activate_initial'
            AND operation_scope='model_deployment.lifecycle.activate_initial.staging-successor.v2'
            AND successor_predecessor_public_model_id IS NOT NULL
            AND successor_public_model_id IS NOT NULL
            AND successor_predecessor_public_model_id<>successor_public_model_id
            AND successor_stable_request_model=BTRIM(successor_stable_request_model)
            AND BTRIM(successor_stable_request_model)<>''
            AND successor_stable_request_model !~ E'[\\x00\\r\\n]'
        ) OR (
            contract_version='unirouter.model-deployment-lifecycle-receipt.v4'
            AND action='activate_initial'
            AND operation_scope='model_deployment.lifecycle.activate_initial.primary-successor.v3'
            AND successor_predecessor_public_model_id IS NOT NULL
            AND successor_public_model_id IS NOT NULL
            AND successor_predecessor_public_model_id<>successor_public_model_id
            AND successor_stable_request_model=BTRIM(successor_stable_request_model)
            AND BTRIM(successor_stable_request_model)<>''
            AND successor_stable_request_model !~ E'[\\x00\\r\\n]'
        )
        )
        AND idempotency_key_hash ~ '^[a-f0-9]{64}$'
        AND request_fingerprint ~ '^hmac-sha256:[a-f0-9]{64}$'
        AND server_operation_id=BTRIM(server_operation_id)
        AND BTRIM(server_operation_id)<>''
        AND server_operation_id !~ E'[\\x00\\r\\n]'
        AND target_release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND expected_release_version>0 AND expected_slot_version>=0
        AND reason=BTRIM(reason) AND reason !~ E'[\\x00\\r\\n]'
        AND OCTET_LENGTH(reason)<=4000
        AND (
            (action='activate_initial' AND expected_slot_version=0
                AND requested_canary_basis_points=0)
            OR (action='start_canary' AND expected_slot_version>0
                AND requested_canary_basis_points BETWEEN 1 AND 5000)
            OR (action IN (
                    'promote','cancel_canary','rollback_stable',
                    'cutover_legacy','rollback_legacy'
                ) AND expected_slot_version>0 AND requested_canary_basis_points=0)
        )
    );
ALTER TABLE model_successor_cutover_receipt_model_sets
DROP CONSTRAINT model_successor_cutover_receipt_model_sets_shape_check,
ADD CONSTRAINT model_successor_cutover_receipt_model_sets_shape_check CHECK (
  before_model_name=BTRIM(before_model_name) AND before_model_name<>''
  AND after_alias_name=BTRIM(after_alias_name) AND after_alias_name<>''
);
CREATE OR REPLACE FUNCTION preserve_model_successor_cutover_child()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    parent_status TEXT;
    parent_contract TEXT;
    parent_scope TEXT;
    parent_org BIGINT;
    parent_predecessor BIGINT;
    parent_successor BIGINT;
    parent_stable TEXT;
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'model successor cutover evidence is immutable' USING ERRCODE='55000';
    END IF;
    SELECT status,contract_version,operation_scope,customer_org_id,
           successor_predecessor_public_model_id,successor_public_model_id,
           successor_stable_request_model
      INTO parent_status,parent_contract,parent_scope,parent_org,
           parent_predecessor,parent_successor,parent_stable
      FROM model_deployment_lifecycle_receipts
     WHERE receipt_id=NEW.lifecycle_receipt_id FOR SHARE;
    IF parent_status IS DISTINCT FROM 'in_progress'
       OR parent_org IS DISTINCT FROM NEW.customer_org_id
       OR parent_predecessor IS DISTINCT FROM NEW.predecessor_public_model_id
       OR parent_successor IS DISTINCT FROM NEW.successor_public_model_id
       OR parent_stable IS DISTINCT FROM NEW.stable_request_model
       OR NOT (
          (parent_contract='unirouter.model-deployment-lifecycle-receipt.v2'
           AND parent_scope='model_deployment.lifecycle.activate_initial.successor.v1'
           AND NEW.contract_version='unirouter.model-successor-cutover-receipt.v1'
           AND NEW.predecessor_authority_mode IS NULL
           AND NEW.predecessor_authority_snapshot IS NULL)
          OR
          (parent_contract='unirouter.model-deployment-lifecycle-receipt.v3'
           AND parent_scope='model_deployment.lifecycle.activate_initial.staging-successor.v2'
           AND NEW.contract_version='unirouter.model-successor-cutover-receipt.v2'
           AND NEW.predecessor_authority_mode='never_activated_staging'
           AND NEW.predecessor_authority_snapshot='{
              "runtime_slot_count": 0,
              "sealed_or_executable_release_count": 0,
              "active_alias_count": 0,
              "completed_successor_receipt_count": 0
           }'::jsonb)
          OR
          (parent_contract='unirouter.model-deployment-lifecycle-receipt.v4'
           AND parent_scope='model_deployment.lifecycle.activate_initial.primary-successor.v3'
           AND NEW.contract_version='unirouter.model-successor-cutover-receipt.v3'
           AND NEW.predecessor_authority_mode='primary_alias' AND NEW.alias_before_exists)
       ) THEN
        RAISE EXCEPTION 'model successor evidence requires its exact in-progress lifecycle parent'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION preserve_model_successor_cutover_model_set_child()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    parent_status TEXT;
    child_stable TEXT;
    child_model TEXT;
    child_org BIGINT;
    set_org BIGINT;
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'model successor model-set evidence is immutable' USING ERRCODE='55000';
    END IF;
    SELECT parent.status,child.stable_request_model,child.customer_org_id,model_set.org_id,child.predecessor_model_name
      INTO parent_status,child_stable,child_org,set_org,child_model
      FROM model_successor_cutover_receipts child
      JOIN model_deployment_lifecycle_receipts parent
        ON parent.receipt_id=child.lifecycle_receipt_id
      JOIN model_sets model_set ON model_set.id=NEW.model_set_id
     WHERE child.lifecycle_receipt_id=NEW.lifecycle_receipt_id
     FOR SHARE OF parent,child,model_set;
    IF parent_status IS DISTINCT FROM 'in_progress'
       OR child_org IS DISTINCT FROM set_org
       OR child_model IS DISTINCT FROM NEW.before_model_name
       OR child_stable IS DISTINCT FROM NEW.after_alias_name THEN
        RAISE EXCEPTION 'model successor model-set evidence requires its exact in-progress parent'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION protect_primary_model_alias()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    matching_parent BIGINT;
BEGIN
    IF TG_OP IN ('INSERT','UPDATE') AND NEW.status='active' AND NOT NEW.is_primary
       AND EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts child
        JOIN model_deployment_lifecycle_receipts parent
          ON parent.receipt_id=child.lifecycle_receipt_id AND parent.status='completed'
        WHERE child.customer_org_id=NEW.org_id
          AND child.successor_model_name=NEW.target_model
       ) THEN
        RAISE EXCEPTION 'completed model successor accepts only its primary stable alias'
            USING ERRCODE='55000';
    END IF;
    IF TG_OP='INSERT' AND NEW.is_primary THEN
        SELECT receipt.receipt_id INTO matching_parent
          FROM model_deployment_lifecycle_receipts receipt
         WHERE receipt.status='in_progress'
           AND receipt.contract_version IN (
             'unirouter.model-deployment-lifecycle-receipt.v2',
             'unirouter.model-deployment-lifecycle-receipt.v3')
           AND receipt.customer_org_id=NEW.org_id
           AND receipt.successor_stable_request_model=NEW.alias
           AND EXISTS (
             SELECT 1 FROM model_registry target
              WHERE target.id=receipt.successor_public_model_id
                AND target.org_id=NEW.org_id AND target.model_name=NEW.target_model)
         FOR SHARE;
        IF matching_parent IS NULL THEN
            RAISE EXCEPTION 'primary model alias requires an in-progress successor cutover'
                USING ERRCODE='55000';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' AND OLD.is_primary AND NEW.is_primary
       AND OLD.status='active' AND NEW.status='active'
       AND (to_jsonb(NEW)-'target_model'-'updated_at')=(to_jsonb(OLD)-'target_model'-'updated_at')
       AND EXISTS (
         SELECT 1 FROM model_successor_cutover_receipts child
         JOIN model_deployment_lifecycle_receipts parent ON parent.receipt_id=child.lifecycle_receipt_id
         WHERE parent.status='in_progress'
           AND parent.contract_version='unirouter.model-deployment-lifecycle-receipt.v4'
           AND child.contract_version='unirouter.model-successor-cutover-receipt.v3'
           AND child.customer_org_id=OLD.org_id AND child.stable_request_model=OLD.alias
           AND child.predecessor_model_name=OLD.target_model
           AND child.successor_model_name=NEW.target_model
           AND (child.predecessor_authority_snapshot->>'primary_alias_id')::bigint=OLD.id
       ) THEN
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' AND (OLD.is_primary OR NEW.is_primary) THEN
        RAISE EXCEPTION 'primary model alias is immutable; use a reviewed successor lifecycle'
            USING ERRCODE='55000';
    END IF;
    IF TG_OP='DELETE' AND OLD.is_primary THEN
        RAISE EXCEPTION 'primary model alias is durable successor authority'
            USING ERRCODE='55000';
    END IF;
    RETURN COALESCE(NEW,OLD);
END;
$$;
CREATE OR REPLACE FUNCTION protect_completed_model_successor_registry()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
	IF TG_OP='DELETE' AND EXISTS (
		SELECT 1 FROM model_successor_cutover_receipts receipt
		JOIN model_deployment_lifecycle_receipts parent
		  ON parent.receipt_id=receipt.lifecycle_receipt_id AND parent.status='completed'
		WHERE receipt.predecessor_public_model_id=OLD.id
		   OR receipt.successor_public_model_id=OLD.id
	) THEN
		RAISE EXCEPTION 'completed model successor registry identities cannot be deleted'
			USING ERRCODE='55000';
	END IF;
    IF TG_OP='UPDATE' AND EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts receipt
        JOIN model_deployment_lifecycle_receipts parent
          ON parent.receipt_id=receipt.lifecycle_receipt_id AND parent.status='completed'
        WHERE receipt.predecessor_public_model_id=OLD.id
    ) AND (NEW.lifecycle<>'retired' OR NEW.approved_catalog) THEN
        RAISE EXCEPTION 'completed model successor predecessor cannot be revived or re-listed'
            USING ERRCODE='55000';
    END IF;
    IF TG_OP='UPDATE' AND EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts receipt
        JOIN model_deployment_lifecycle_receipts parent
          ON parent.receipt_id=receipt.lifecycle_receipt_id AND parent.status='completed'
        WHERE receipt.successor_public_model_id=OLD.id
    ) AND (NEW.lifecycle<>'ga' OR NOT NEW.approved_catalog)
      AND NOT (
        NEW.lifecycle='retired' AND NOT NEW.approved_catalog AND EXISTS (
          SELECT 1 FROM model_successor_cutover_receipts child
          JOIN model_deployment_lifecycle_receipts parent ON parent.receipt_id=child.lifecycle_receipt_id
          WHERE child.predecessor_public_model_id=OLD.id
            AND child.customer_org_id=OLD.org_id AND child.predecessor_model_name=OLD.model_name
            AND child.contract_version='unirouter.model-successor-cutover-receipt.v3'
            AND parent.contract_version='unirouter.model-deployment-lifecycle-receipt.v4'
            AND parent.status IN ('in_progress','completed')
        )
      ) THEN
        RAISE EXCEPTION 'completed primary model successor cannot be staged, retired or de-listed'
            USING ERRCODE='55000';
    END IF;
    RETURN COALESCE(NEW,OLD);
END;
$$;
CREATE OR REPLACE FUNCTION validate_model_successor_parent_completion()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    child_count BIGINT;
    model_set_child_count BIGINT;
    audit_count BIGINT;
    derived_before JSONB;
    derived_after JSONB;
    expected_child_contract TEXT;
BEGIN
    IF NEW.contract_version NOT IN (
         'unirouter.model-deployment-lifecycle-receipt.v2',
         'unirouter.model-deployment-lifecycle-receipt.v3',
         'unirouter.model-deployment-lifecycle-receipt.v4')
       OR NEW.status<>'completed' THEN
        RETURN NEW;
    END IF;
    IF OLD.contract_version IS DISTINCT FROM NEW.contract_version
       OR OLD.successor_predecessor_public_model_id IS DISTINCT FROM NEW.successor_predecessor_public_model_id
       OR OLD.successor_public_model_id IS DISTINCT FROM NEW.successor_public_model_id
       OR OLD.successor_stable_request_model IS DISTINCT FROM NEW.successor_stable_request_model THEN
        RAISE EXCEPTION 'model successor lifecycle identity is immutable'
            USING ERRCODE='55000';
    END IF;
    expected_child_contract := CASE NEW.contract_version
      WHEN 'unirouter.model-deployment-lifecycle-receipt.v2'
        THEN 'unirouter.model-successor-cutover-receipt.v1'
      WHEN 'unirouter.model-deployment-lifecycle-receipt.v3' THEN 'unirouter.model-successor-cutover-receipt.v2'
      ELSE 'unirouter.model-successor-cutover-receipt.v3'
    END;
    SELECT COUNT(*) INTO child_count
      FROM model_successor_cutover_receipts child
     WHERE child.lifecycle_receipt_id=NEW.receipt_id
       AND child.contract_version=expected_child_contract
       AND child.customer_org_id=NEW.customer_org_id
       AND child.predecessor_public_model_id=NEW.successor_predecessor_public_model_id
       AND child.successor_public_model_id=NEW.successor_public_model_id
       AND child.successor_public_model_id=NEW.public_model_id
       AND child.stable_request_model=NEW.successor_stable_request_model
       AND (
          (NEW.contract_version='unirouter.model-deployment-lifecycle-receipt.v2'
           AND child.predecessor_authority_mode IS NULL
           AND child.predecessor_authority_snapshot IS NULL)
          OR
          (NEW.contract_version='unirouter.model-deployment-lifecycle-receipt.v3'
           AND child.predecessor_authority_mode='never_activated_staging'
           AND child.predecessor_authority_snapshot='{
              "runtime_slot_count": 0,
              "sealed_or_executable_release_count": 0,
              "active_alias_count": 0,
              "completed_successor_receipt_count": 0
           }'::jsonb)
          OR (NEW.contract_version='unirouter.model-deployment-lifecycle-receipt.v4'
              AND child.predecessor_authority_mode='primary_alias' AND child.alias_before_exists)
       );
    IF child_count<>1 THEN
        RAISE EXCEPTION 'successor lifecycle completion requires one exact child receipt'
            USING ERRCODE='23514';
    END IF;
    SELECT COUNT(*) INTO model_set_child_count
      FROM model_successor_cutover_receipt_model_sets evidence
     WHERE evidence.lifecycle_receipt_id=NEW.receipt_id;
    IF model_set_child_count<>(
        SELECT model_set_count FROM model_successor_cutover_receipts
         WHERE lifecycle_receipt_id=NEW.receipt_id
    ) THEN
        RAISE EXCEPTION 'successor lifecycle completion requires complete model-set evidence'
            USING ERRCODE='23514';
    END IF;
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
            'model_set_id',evidence.model_set_id,
            'model',evidence.before_model_name,
            'alias_exists',evidence.before_alias_exists
        ) ORDER BY evidence.model_set_id),'[]'::jsonb),
        COALESCE(jsonb_agg(jsonb_build_object(
            'model_set_id',evidence.model_set_id,
            'alias',evidence.after_alias_name
        ) ORDER BY evidence.model_set_id),'[]'::jsonb)
      INTO derived_before,derived_after
      FROM model_successor_cutover_receipt_model_sets evidence
     WHERE evidence.lifecycle_receipt_id=NEW.receipt_id;
    IF NOT EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts child
         WHERE child.lifecycle_receipt_id=NEW.receipt_id
           AND child.model_sets_before=derived_before
           AND child.model_sets_after=derived_after
    ) THEN
        RAISE EXCEPTION 'successor lifecycle completion requires canonical model-set manifests'
            USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts child
        JOIN model_registry predecessor ON predecessor.id=child.predecessor_public_model_id
        JOIN model_registry successor ON successor.id=child.successor_public_model_id
        JOIN model_aliases alias ON alias.org_id=child.customer_org_id
          AND alias.alias=child.stable_request_model
        WHERE child.lifecycle_receipt_id=NEW.receipt_id
          AND predecessor.org_id=child.customer_org_id
          AND predecessor.model_name=child.predecessor_model_name
          AND predecessor.lifecycle='retired' AND NOT predecessor.approved_catalog
          AND successor.org_id=child.customer_org_id
          AND successor.model_name=child.successor_model_name
          AND successor.lifecycle='ga' AND successor.approved_catalog
          AND alias.target_model=child.successor_model_name
          AND alias.status='active' AND alias.is_primary
    ) OR EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts child
        JOIN model_sets model_set ON model_set.org_id=child.customer_org_id
        JOIN model_set_items item ON item.model_set_id=model_set.id
        WHERE child.lifecycle_receipt_id=NEW.receipt_id
          AND item.item_type='model' AND item.item_name=child.predecessor_model_name
    ) OR EXISTS (
        SELECT 1 FROM model_successor_cutover_receipt_model_sets evidence
        LEFT JOIN model_set_items item ON item.model_set_id=evidence.model_set_id
          AND item.item_type='alias' AND item.item_name=evidence.after_alias_name
        WHERE evidence.lifecycle_receipt_id=NEW.receipt_id AND item.model_set_id IS NULL
    ) THEN
        RAISE EXCEPTION 'successor lifecycle completion requires exact live registry, alias and model-set state'
            USING ERRCODE='23514';
    END IF;
    SELECT COUNT(*) INTO audit_count
      FROM audit_logs audit
      JOIN model_successor_cutover_receipts child ON child.lifecycle_receipt_id=NEW.receipt_id
     WHERE audit.id=NEW.audit_log_id
       AND audit.detail->'successor_cutover'->>'contract_version'=child.contract_version
       AND audit.detail->'successor_cutover'->>'stable_request_model'=child.stable_request_model
       AND audit.detail->'successor_cutover'->>'model_set_count'=child.model_set_count::TEXT
       AND audit.detail->'successor_cutover'->'model_sets_before'=child.model_sets_before
       AND audit.detail->'successor_cutover'->'model_sets_after'=child.model_sets_after
       AND (
          (child.contract_version='unirouter.model-successor-cutover-receipt.v1'
           AND NOT (audit.detail->'successor_cutover' ? 'predecessor_authority_mode')
           AND NOT (audit.detail->'successor_cutover' ? 'predecessor_authority_snapshot'))
          OR
          (child.contract_version IN ('unirouter.model-successor-cutover-receipt.v2','unirouter.model-successor-cutover-receipt.v3')
           AND audit.detail->'successor_cutover'->>'predecessor_authority_mode'=child.predecessor_authority_mode
           AND audit.detail->'successor_cutover'->'predecessor_authority_snapshot'=child.predecessor_authority_snapshot)
       );
    IF audit_count<>1 THEN
        RAISE EXCEPTION 'successor lifecycle completion requires exact successor audit evidence'
            USING ERRCODE='23514';
    END IF;
    IF NEW.contract_version='unirouter.model-deployment-lifecycle-receipt.v4' AND NOT EXISTS (
        SELECT 1 FROM model_successor_cutover_receipts child
        JOIN model_aliases alias ON alias.id=(child.predecessor_authority_snapshot->>'primary_alias_id')::bigint
        JOIN runtime_change_slots old_slot ON old_slot.public_model_id=child.predecessor_public_model_id
        JOIN runtime_change_releases old_release ON old_release.id=old_slot.stable_release_id
        JOIN runtime_change_slots new_slot ON new_slot.public_model_id=child.successor_public_model_id
        JOIN runtime_change_releases new_release ON new_release.id=new_slot.stable_release_id
        WHERE child.lifecycle_receipt_id=NEW.receipt_id
          AND alias.org_id=child.customer_org_id AND alias.alias=child.stable_request_model
          AND alias.target_model=child.successor_model_name AND alias.is_primary AND alias.status='active'
          AND old_slot.model_name=child.predecessor_model_name
          AND old_slot.slot_version=(child.predecessor_authority_snapshot->>'slot_version')::bigint
          AND old_slot.stable_release_id=(child.predecessor_authority_snapshot->>'stable_release_id')::bigint
          AND old_slot.canary_release_id IS NULL AND old_slot.canary_basis_points=0
          AND old_release.version=(child.predecessor_authority_snapshot->>'release_version')::bigint
          AND old_release.public_model_id=child.predecessor_public_model_id AND old_release.stage='active'
          AND new_slot.model_name=child.successor_model_name AND new_release.stage='active'
          AND new_release.public_model_id=child.successor_public_model_id
          AND new_release.release_ref=NEW.target_release_ref AND new_release.route_items_sealed_at IS NOT NULL
          AND old_release.route_items_sealed_at IS NOT NULL
          AND old_release.execution_profile_id=new_release.execution_profile_id
          AND old_release.execution_profile_id NOT IN ('','unverified.chat')
    ) THEN
        RAISE EXCEPTION 'primary successor completion requires exact before authority and active target'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER model_deployment_successor_completion_guard ON model_deployment_lifecycle_receipts;
CREATE TRIGGER model_deployment_successor_completion_guard BEFORE UPDATE ON model_deployment_lifecycle_receipts
FOR EACH ROW WHEN (NEW.contract_version IN ('unirouter.model-deployment-lifecycle-receipt.v2','unirouter.model-deployment-lifecycle-receipt.v3','unirouter.model-deployment-lifecycle-receipt.v4'))
EXECUTE FUNCTION validate_model_successor_parent_completion();
COMMENT ON COLUMN model_successor_cutover_receipts.predecessor_authority_mode IS
'Null for published first v1 receipts; never_activated_staging for v2; primary_alias for repeat v3.';
COMMENT ON COLUMN model_successor_cutover_receipts.predecessor_authority_snapshot IS
'Versioned immutable predecessor evidence: zero counters for staging v2; exact primary Alias and stable Slot/Release snapshot for repeat v3.';
COMMIT;
