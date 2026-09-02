BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_deployment_lifecycle_receipts,model_aliases,model_registry,
    model_sets,model_set_items,runtime_change_releases,runtime_change_slots,
    audit_logs,schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE model_aliases
    ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT model_aliases_primary_active_check CHECK (NOT is_primary OR status='active');

CREATE UNIQUE INDEX model_aliases_one_active_primary_target
    ON model_aliases(org_id,target_model)
    WHERE is_primary AND status='active';

ALTER TABLE model_deployment_lifecycle_receipts
    DROP CONSTRAINT model_deployment_lifecycle_receipts_identity_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_state_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check,
    ADD COLUMN successor_predecessor_public_model_id BIGINT REFERENCES model_registry(id) ON DELETE RESTRICT,
    ADD COLUMN successor_public_model_id BIGINT REFERENCES model_registry(id) ON DELETE RESTRICT,
    ADD COLUMN successor_stable_request_model VARCHAR(100);

ALTER TABLE model_deployment_lifecycle_receipts
    ADD CONSTRAINT model_deployment_lifecycle_receipts_identity_check CHECK (
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
    ),
    ADD CONSTRAINT model_deployment_lifecycle_receipts_state_check CHECK (
        (status='in_progress'
            AND target_release_id IS NULL AND public_model_id IS NULL
            AND original_http_status IS NULL
            AND before_slot_public_model_id IS NULL
            AND before_stable_release_id IS NULL AND before_canary_release_id IS NULL
            AND before_canary_basis_points IS NULL AND before_slot_version IS NULL
            AND after_slot_public_model_id IS NULL
            AND after_stable_release_id IS NULL AND after_canary_release_id IS NULL
            AND after_canary_basis_points IS NULL AND after_slot_version IS NULL
            AND legacy_plan_hash IS NULL AND deployment_plan_hash IS NULL
            AND audit_log_id IS NULL AND completed_at IS NULL)
        OR
        (status='completed'
            AND target_release_id IS NOT NULL AND public_model_id IS NOT NULL
            AND original_http_status=200
            AND before_canary_basis_points IS NOT NULL AND before_slot_version IS NOT NULL
            AND after_canary_basis_points IS NOT NULL AND after_slot_version IS NOT NULL
            AND before_canary_basis_points BETWEEN 0 AND 5000
            AND after_canary_basis_points BETWEEN 0 AND 5000
            AND before_slot_version>=0 AND after_slot_version=before_slot_version+1
            AND audit_log_id IS NOT NULL AND audit_log_id>0 AND completed_at IS NOT NULL
            AND (
                (action NOT IN ('cutover_legacy','rollback_legacy')
                    AND before_slot_public_model_id IS NULL
                    AND after_slot_public_model_id IS NULL
                    AND legacy_plan_hash IS NULL AND deployment_plan_hash IS NULL)
                OR (action='cutover_legacy'
                    AND legacy_plan_hash ~ '^sha256:[a-f0-9]{64}$'
                    AND deployment_plan_hash=legacy_plan_hash)
                OR (action='rollback_legacy'
                    AND legacy_plan_hash ~ '^sha256:[a-f0-9]{64}$'
                    AND deployment_plan_hash IS NULL)
            ))
    ),
    ADD CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check CHECK (
        status<>'completed' OR (
            ((before_canary_release_id IS NULL AND before_canary_basis_points=0)
              OR (before_canary_release_id IS NOT NULL AND before_canary_basis_points BETWEEN 1 AND 5000))
            AND ((after_canary_release_id IS NULL AND after_canary_basis_points=0)
              OR (after_canary_release_id IS NOT NULL AND after_canary_basis_points BETWEEN 1 AND 5000))
            AND (action<>'activate_initial' OR (
                before_slot_version=0
                AND before_stable_release_id IS NULL AND before_canary_release_id IS NULL
                AND after_slot_version=1
                AND after_stable_release_id=target_release_id AND after_canary_release_id IS NULL))
            AND (action<>'start_canary' OR (
                before_stable_release_id IS NOT NULL AND before_canary_release_id IS NULL
                AND after_stable_release_id=before_stable_release_id
                AND after_canary_release_id=target_release_id
                AND after_canary_basis_points=requested_canary_basis_points))
            AND (action<>'promote' OR (
                before_stable_release_id IS NOT NULL AND before_canary_release_id=target_release_id
                AND after_stable_release_id=target_release_id AND after_canary_release_id IS NULL))
            AND (action<>'cancel_canary' OR (
                before_stable_release_id IS NOT NULL AND before_canary_release_id=target_release_id
                AND after_stable_release_id=before_stable_release_id AND after_canary_release_id IS NULL))
            AND (action<>'rollback_stable' OR (
                before_stable_release_id=target_release_id AND before_canary_release_id IS NULL
                AND after_stable_release_id IS NOT NULL
                AND after_stable_release_id<>target_release_id AND after_canary_release_id IS NULL))
            AND (action<>'cutover_legacy' OR (
                before_slot_public_model_id IS NULL AND after_slot_public_model_id=public_model_id
                AND before_stable_release_id IS NOT NULL
                AND before_stable_release_id<>target_release_id AND before_canary_release_id IS NULL
                AND after_stable_release_id=target_release_id AND after_canary_release_id IS NULL))
            AND (action<>'rollback_legacy' OR (
                before_slot_public_model_id=public_model_id AND after_slot_public_model_id IS NULL
                AND before_stable_release_id=target_release_id AND before_canary_release_id IS NULL
                AND after_stable_release_id IS NOT NULL
                AND after_stable_release_id<>target_release_id AND after_canary_release_id IS NULL))
        )
    );

CREATE TABLE model_successor_cutover_receipts (
    lifecycle_receipt_id BIGINT PRIMARY KEY
        REFERENCES model_deployment_lifecycle_receipts(receipt_id) ON DELETE RESTRICT,
    contract_version VARCHAR(80) NOT NULL,
    customer_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    stable_request_model VARCHAR(100) NOT NULL,
    predecessor_public_model_id BIGINT NOT NULL REFERENCES model_registry(id) ON DELETE RESTRICT,
    predecessor_model_name VARCHAR(100) NOT NULL,
    predecessor_before_lifecycle VARCHAR(20) NOT NULL,
    predecessor_before_approved_catalog BOOLEAN NOT NULL,
    predecessor_after_lifecycle VARCHAR(20) NOT NULL,
    predecessor_after_approved_catalog BOOLEAN NOT NULL,
    successor_public_model_id BIGINT NOT NULL REFERENCES model_registry(id) ON DELETE RESTRICT,
    successor_model_name VARCHAR(100) NOT NULL,
    successor_before_lifecycle VARCHAR(20) NOT NULL,
    successor_before_approved_catalog BOOLEAN NOT NULL,
    successor_after_lifecycle VARCHAR(20) NOT NULL,
    successor_after_approved_catalog BOOLEAN NOT NULL,
    alias_before_exists BOOLEAN NOT NULL,
    alias_after_target_model VARCHAR(100) NOT NULL,
    alias_after_status VARCHAR(20) NOT NULL,
    alias_after_is_primary BOOLEAN NOT NULL,
    model_set_count INTEGER NOT NULL,
    model_sets_before JSONB NOT NULL,
    model_sets_after JSONB NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT model_successor_cutover_receipts_identity_check CHECK (
        contract_version='unirouter.model-successor-cutover-receipt.v1'
        AND stable_request_model=BTRIM(stable_request_model) AND BTRIM(stable_request_model)<>''
        AND predecessor_model_name=stable_request_model
        AND predecessor_public_model_id<>successor_public_model_id
        AND successor_model_name=BTRIM(successor_model_name) AND BTRIM(successor_model_name)<>''
        AND successor_model_name<>stable_request_model
        AND predecessor_before_lifecycle IN ('ga','deprecated')
        AND predecessor_before_approved_catalog
        AND predecessor_after_lifecycle='retired' AND NOT predecessor_after_approved_catalog
        AND successor_before_lifecycle='staging' AND NOT successor_before_approved_catalog
        AND successor_after_lifecycle='ga' AND successor_after_approved_catalog
        AND NOT alias_before_exists
        AND alias_after_target_model=successor_model_name
        AND alias_after_status='active' AND alias_after_is_primary
        AND model_set_count>=0
        AND jsonb_typeof(model_sets_before)='array'
        AND jsonb_typeof(model_sets_after)='array'
    )
);

CREATE TABLE model_successor_cutover_receipt_model_sets (
    lifecycle_receipt_id BIGINT NOT NULL
        REFERENCES model_successor_cutover_receipts(lifecycle_receipt_id) ON DELETE RESTRICT,
    model_set_id BIGINT NOT NULL REFERENCES model_sets(id) ON DELETE RESTRICT,
    before_model_name VARCHAR(100) NOT NULL,
    before_alias_exists BOOLEAN NOT NULL,
    after_alias_name VARCHAR(100) NOT NULL,
    PRIMARY KEY (lifecycle_receipt_id,model_set_id),
    CONSTRAINT model_successor_cutover_receipt_model_sets_shape_check CHECK (
        before_model_name=after_alias_name AND BTRIM(before_model_name)<>''
    )
);

CREATE FUNCTION preserve_model_successor_cutover_child()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    parent_status TEXT;
    parent_contract TEXT;
    parent_org BIGINT;
    parent_predecessor BIGINT;
    parent_successor BIGINT;
    parent_stable TEXT;
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'model successor cutover evidence is immutable' USING ERRCODE='55000';
    END IF;
    SELECT status,contract_version,customer_org_id,successor_predecessor_public_model_id,
           successor_public_model_id,successor_stable_request_model
      INTO parent_status,parent_contract,parent_org,parent_predecessor,parent_successor,parent_stable
      FROM model_deployment_lifecycle_receipts
     WHERE receipt_id=NEW.lifecycle_receipt_id FOR SHARE;
    IF parent_status IS DISTINCT FROM 'in_progress'
       OR parent_contract IS DISTINCT FROM 'unirouter.model-deployment-lifecycle-receipt.v2'
       OR parent_org IS DISTINCT FROM NEW.customer_org_id
       OR parent_predecessor IS DISTINCT FROM NEW.predecessor_public_model_id
       OR parent_successor IS DISTINCT FROM NEW.successor_public_model_id
       OR parent_stable IS DISTINCT FROM NEW.stable_request_model THEN
        RAISE EXCEPTION 'model successor evidence requires its exact in-progress lifecycle parent'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_successor_cutover_receipts_immutable
BEFORE INSERT OR UPDATE OR DELETE ON model_successor_cutover_receipts
FOR EACH ROW EXECUTE FUNCTION preserve_model_successor_cutover_child();

CREATE FUNCTION preserve_model_successor_cutover_model_set_child()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    parent_status TEXT;
    child_stable TEXT;
    child_org BIGINT;
    set_org BIGINT;
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'model successor model-set evidence is immutable' USING ERRCODE='55000';
    END IF;
    SELECT parent.status,child.stable_request_model,child.customer_org_id,model_set.org_id
      INTO parent_status,child_stable,child_org,set_org
      FROM model_successor_cutover_receipts child
      JOIN model_deployment_lifecycle_receipts parent
        ON parent.receipt_id=child.lifecycle_receipt_id
      JOIN model_sets model_set ON model_set.id=NEW.model_set_id
     WHERE child.lifecycle_receipt_id=NEW.lifecycle_receipt_id
     FOR SHARE OF parent,child,model_set;
    IF parent_status IS DISTINCT FROM 'in_progress'
       OR child_org IS DISTINCT FROM set_org
       OR child_stable IS DISTINCT FROM NEW.before_model_name
       OR child_stable IS DISTINCT FROM NEW.after_alias_name THEN
        RAISE EXCEPTION 'model successor model-set evidence requires its exact in-progress parent'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_successor_cutover_receipt_model_sets_immutable
BEFORE INSERT OR UPDATE OR DELETE ON model_successor_cutover_receipt_model_sets
FOR EACH ROW EXECUTE FUNCTION preserve_model_successor_cutover_model_set_child();

CREATE FUNCTION protect_primary_model_alias()
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
           AND receipt.contract_version='unirouter.model-deployment-lifecycle-receipt.v2'
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

CREATE TRIGGER model_aliases_primary_successor_guard
BEFORE INSERT OR UPDATE OR DELETE ON model_aliases
FOR EACH ROW EXECUTE FUNCTION protect_primary_model_alias();

CREATE FUNCTION protect_completed_model_successor_registry()
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
    ) AND (NEW.lifecycle<>'ga' OR NOT NEW.approved_catalog) THEN
        RAISE EXCEPTION 'completed primary model successor cannot be staged, retired or de-listed'
            USING ERRCODE='55000';
    END IF;
    RETURN COALESCE(NEW,OLD);
END;
$$;

CREATE TRIGGER model_registry_completed_successor_guard
BEFORE UPDATE OR DELETE ON model_registry
FOR EACH ROW EXECUTE FUNCTION protect_completed_model_successor_registry();

CREATE FUNCTION validate_model_successor_parent_completion()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    child_count BIGINT;
    model_set_child_count BIGINT;
    audit_count BIGINT;
	derived_before JSONB;
	derived_after JSONB;
BEGIN
    IF NEW.contract_version<>'unirouter.model-deployment-lifecycle-receipt.v2'
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
    SELECT COUNT(*) INTO child_count
      FROM model_successor_cutover_receipts child
     WHERE child.lifecycle_receipt_id=NEW.receipt_id
       AND child.customer_org_id=NEW.customer_org_id
       AND child.predecessor_public_model_id=NEW.successor_predecessor_public_model_id
       AND child.successor_public_model_id=NEW.successor_public_model_id
       AND child.successor_public_model_id=NEW.public_model_id
       AND child.stable_request_model=NEW.successor_stable_request_model;
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
       AND audit.detail->'successor_cutover'->'model_sets_after'=child.model_sets_after;
    IF audit_count<>1 THEN
        RAISE EXCEPTION 'successor lifecycle completion requires exact successor audit evidence'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_successor_completion_guard
BEFORE UPDATE ON model_deployment_lifecycle_receipts
FOR EACH ROW
WHEN (NEW.contract_version='unirouter.model-deployment-lifecycle-receipt.v2')
EXECUTE FUNCTION validate_model_successor_parent_completion();

CREATE FUNCTION reject_model_successor_evidence_truncate()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model successor cutover evidence cannot be truncated' USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_successor_cutover_receipts_truncate_guard
BEFORE TRUNCATE ON model_successor_cutover_receipts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_successor_evidence_truncate();

CREATE TRIGGER model_successor_cutover_receipt_model_sets_truncate_guard
BEFORE TRUNCATE ON model_successor_cutover_receipt_model_sets
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_successor_evidence_truncate();

COMMENT ON COLUMN model_aliases.is_primary IS
    'Explicit stable request identity selected by the reviewed immutable-successor cutover; generic Alias CRUD cannot create or mutate it.';
COMMENT ON TABLE model_successor_cutover_receipts IS
    'Append-only exact evidence for one first stable-request-identity to immutable-successor atomic cutover.';

COMMIT;
