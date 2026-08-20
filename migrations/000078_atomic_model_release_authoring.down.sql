BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_change_releases, runtime_change_slots, orgs,
	    model_deployment_release_authoring_receipts,
	    model_deployment_lifecycle_receipts,
	    model_deployment_lifecycle_receipt_releases,
	    model_deployment_lifecycle_receipt_qualifications,
	    model_operation_public_execution_bindings,
    runtime_change_events, schema_migrations
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000078_atomic_model_release_authoring'
    ) THEN
        RAISE EXCEPTION 'Atomic model release authoring rollback refused: later migrations must be reversed first';
    END IF;
	    IF EXISTS (
	        SELECT 1 FROM model_deployment_lifecycle_receipts
	         WHERE action IN ('cutover_legacy','rollback_legacy')
	    ) THEN
	        RAISE EXCEPTION 'Atomic model release authoring rollback refused: durable legacy lifecycle evidence exists';
	    END IF;
	    IF EXISTS (SELECT 1 FROM model_deployment_release_authoring_receipts)
       OR EXISTS (
           SELECT 1 FROM runtime_change_releases
            WHERE authoring_receipt_id IS NOT NULL
       ) THEN
	        RAISE EXCEPTION 'Atomic model release authoring rollback refused: durable authoring evidence exists';
	    END IF;
    -- The old M77 check cannot represent a platform-admin creator. Prove an
    -- entirely empty immutable binding relation before restoring it; absence
    -- of only mismatching rows is not sufficient downgrade evidence.
    IF EXISTS (SELECT 1 FROM model_operation_public_execution_bindings) THEN
        RAISE EXCEPTION 'Atomic model release authoring rollback refused: public execution bindings must be empty before restoring M77 creator authority';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipts_immutable
    ON model_deployment_lifecycle_receipts;
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipts_legacy_immutable
    ON model_deployment_lifecycle_receipts;
DROP TRIGGER IF EXISTS model_deployment_lifecycle_receipts_canonical_immutable
    ON model_deployment_lifecycle_receipts;
DROP FUNCTION IF EXISTS reject_model_deployment_lifecycle_receipt_delete();
DROP FUNCTION IF EXISTS preserve_legacy_model_deployment_lifecycle_receipt();

ALTER TABLE model_deployment_lifecycle_receipts
    DROP CONSTRAINT model_deployment_lifecycle_receipts_identity_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_state_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check,
    DROP COLUMN before_slot_public_model_id,
    DROP COLUMN after_slot_public_model_id,
    DROP COLUMN legacy_plan_hash,
    DROP COLUMN deployment_plan_hash,
    ADD CONSTRAINT model_deployment_lifecycle_receipts_identity_check CHECK (
        contract_version='unirouter.model-deployment-lifecycle-receipt.v1'
        AND action IN ('activate_initial','start_canary','promote','cancel_canary','rollback_stable')
        AND operation_scope='model_deployment.lifecycle.' || action || '.v1'
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
            OR (action IN ('promote','cancel_canary','rollback_stable')
                AND expected_slot_version>0 AND requested_canary_basis_points=0)
        )
    ),
    ADD CONSTRAINT model_deployment_lifecycle_receipts_state_check CHECK (
        (status='in_progress'
            AND target_release_id IS NULL AND public_model_id IS NULL
            AND original_http_status IS NULL
            AND before_stable_release_id IS NULL AND before_canary_release_id IS NULL
            AND before_canary_basis_points IS NULL AND before_slot_version IS NULL
            AND after_stable_release_id IS NULL AND after_canary_release_id IS NULL
            AND after_canary_basis_points IS NULL AND after_slot_version IS NULL
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
            AND audit_log_id IS NOT NULL AND audit_log_id>0 AND completed_at IS NOT NULL)
    ),
    ADD CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check CHECK (
        status<>'completed' OR (
            ((before_canary_release_id IS NULL AND before_canary_basis_points=0)
              OR (before_canary_release_id IS NOT NULL AND before_canary_basis_points BETWEEN 1 AND 5000))
            AND ((after_canary_release_id IS NULL AND after_canary_basis_points=0)
              OR (after_canary_release_id IS NOT NULL AND after_canary_basis_points BETWEEN 1 AND 5000))
            AND (action<>'activate_initial' OR (
                before_slot_version=0 AND before_stable_release_id IS NULL
                AND before_canary_release_id IS NULL AND after_slot_version=1
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
        )
    );

CREATE TRIGGER model_deployment_lifecycle_receipts_immutable
BEFORE INSERT OR UPDATE OR DELETE ON model_deployment_lifecycle_receipts
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_lifecycle_receipt();

DROP TRIGGER IF EXISTS runtime_change_releases_authoring_link_guard
    ON runtime_change_releases;
DROP FUNCTION IF EXISTS preserve_runtime_release_authoring_link();

DROP TRIGGER IF EXISTS model_deployment_release_authoring_receipts_truncate_guard
    ON model_deployment_release_authoring_receipts;
DROP FUNCTION IF EXISTS reject_model_deployment_release_authoring_receipt_truncate();
DROP TRIGGER IF EXISTS model_deployment_release_authoring_receipts_terminal
    ON model_deployment_release_authoring_receipts;
DROP FUNCTION IF EXISTS require_terminal_model_deployment_release_authoring_receipt();
DROP TRIGGER IF EXISTS model_deployment_release_authoring_receipts_immutable
    ON model_deployment_release_authoring_receipts;
DROP FUNCTION IF EXISTS preserve_model_deployment_release_authoring_receipt();

ALTER TABLE runtime_change_releases
    DROP CONSTRAINT runtime_change_releases_authoring_receipt_unique,
    DROP COLUMN authoring_receipt_id;

DROP TABLE model_deployment_release_authoring_receipts;

-- Restore the exact M76 event-link contract. Historical/unbound events remain
-- legal again after the M78 provenance column and receipts are gone.
CREATE OR REPLACE FUNCTION preserve_runtime_change_event_receipt_link()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    linked_receipt_id BIGINT;
    parent_status TEXT;
BEGIN
    IF NOT (NEW.detail ? 'lifecycle_receipt_id') THEN
        RETURN NEW;
    END IF;
    IF NEW.detail->>'lifecycle_receipt_id' IS NULL
       OR NEW.detail->>'lifecycle_receipt_id' !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'runtime change event requires a positive lifecycle receipt identity'
            USING ERRCODE='23514';
    END IF;
    BEGIN
        linked_receipt_id := (NEW.detail->>'lifecycle_receipt_id')::BIGINT;
    EXCEPTION WHEN numeric_value_out_of_range OR invalid_text_representation THEN
        RAISE EXCEPTION 'runtime change event requires a positive lifecycle receipt identity'
            USING ERRCODE='23514';
    END;
    SELECT status INTO parent_status
      FROM model_deployment_lifecycle_receipts
     WHERE receipt_id=linked_receipt_id
     FOR SHARE;
    IF NOT FOUND OR parent_status<>'in_progress' THEN
        RAISE EXCEPTION 'runtime change event requires an in-progress lifecycle receipt'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE model_operation_public_execution_bindings
    DROP CONSTRAINT model_operation_public_execution_bindings_identity_check,
    ADD CONSTRAINT model_operation_public_execution_bindings_identity_check CHECK (
        customer_org_id>0 AND public_model_id>0 AND release_id>0
        AND ordinal>0 AND deployment_id>0 AND connection_id>0
        AND production_approval_id>0
        AND approved_connection_revision>0
        AND approved_deployment_revision>0
        AND approved_operation_revision>0
        AND model_operation_https_origin(https_origin)=https_origin
        AND created_by>0 AND created_by_org_id=customer_org_id
        AND source_kind='runtime.public_execution_binding.v1'
        AND source_ref='release:' || release_id::TEXT || '/item:' || ordinal::TEXT
    );

CREATE OR REPLACE FUNCTION validate_model_operation_public_execution_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof RECORD;
BEGIN
    SELECT release_row.candidate_version,release_row.stage,
           release_row.route_items_sealed_at,release_row.public_model_id,
           release_row.created_by,
           deployment.customer_org_id,deployment.public_model_id AS deployment_public_model_id,
           deployment.connection_id,
           connection.config_revision AS current_connection_revision,
           deployment.config_revision AS current_deployment_revision,
           operation.revision AS current_operation_revision,
           model_operation_https_origin(connection.base_url) AS current_https_origin,
           qualification.qualified,qualification.approval_id,
           qualification.https_origin AS qualification_https_origin,
           qualification.approved_connection_revision,
           qualification.approved_deployment_revision,
           qualification.approved_operation_revision,
           qualification.public_path,qualification.protocol_profile_id,
           qualification.protocol_profile_version,
           qualification.executor_contract_version,
           contract.delivery_contract_version,contract.charge_contract_version,
           contract.idempotency_contract_version,
           actor.org_id AS actor_org_id,actor.status AS actor_status
      INTO proof
      FROM runtime_change_releases release_row
      JOIN model_deployments deployment ON deployment.id=NEW.deployment_id
      JOIN provider_connections connection ON connection.id=deployment.connection_id
      JOIN model_deployment_operations operation
        ON operation.deployment_id=deployment.id AND operation.operation_id=NEW.operation_id
      JOIN model_operation_execution_qualifications qualification
        ON qualification.deployment_id=operation.deployment_id
       AND qualification.operation_id=operation.operation_id
      JOIN model_operation_current_public_execution_contracts contract
        ON contract.operation_id=qualification.operation_id
       AND contract.public_path=qualification.public_path
       AND contract.protocol_profile_id=qualification.protocol_profile_id
       AND contract.protocol_profile_version=qualification.protocol_profile_version
       AND contract.executor_contract_version=qualification.executor_contract_version
      JOIN users actor ON actor.id=NEW.created_by
     WHERE release_row.id=NEW.release_id
     FOR SHARE OF release_row,deployment,connection,operation,actor;

    IF NOT FOUND
       OR proof.candidate_version<>'unirouter.runtime-change-candidate.v2'
       OR proof.stage<>'draft' OR proof.route_items_sealed_at IS NOT NULL
       OR proof.public_model_id IS DISTINCT FROM NEW.public_model_id
       OR proof.created_by IS DISTINCT FROM NEW.created_by
       OR proof.customer_org_id IS DISTINCT FROM NEW.customer_org_id
       OR proof.deployment_public_model_id IS DISTINCT FROM NEW.public_model_id
       OR proof.connection_id IS DISTINCT FROM NEW.connection_id
       OR proof.actor_org_id IS DISTINCT FROM NEW.created_by_org_id
       OR proof.actor_status<>'active'
       OR proof.qualified IS DISTINCT FROM TRUE
       OR proof.approval_id IS DISTINCT FROM NEW.production_approval_id
       OR proof.qualification_https_origin IS DISTINCT FROM NEW.https_origin
       OR proof.current_https_origin IS DISTINCT FROM NEW.https_origin
       OR proof.current_connection_revision IS DISTINCT FROM NEW.approved_connection_revision
       OR proof.current_deployment_revision IS DISTINCT FROM NEW.approved_deployment_revision
       OR proof.current_operation_revision IS DISTINCT FROM NEW.approved_operation_revision
       OR proof.approved_connection_revision IS DISTINCT FROM NEW.approved_connection_revision
       OR proof.approved_deployment_revision IS DISTINCT FROM NEW.approved_deployment_revision
       OR proof.approved_operation_revision IS DISTINCT FROM NEW.approved_operation_revision
       OR proof.public_path IS DISTINCT FROM NEW.public_path
       OR proof.protocol_profile_id IS DISTINCT FROM NEW.protocol_profile_id
       OR proof.protocol_profile_version IS DISTINCT FROM NEW.protocol_profile_version
       OR proof.executor_contract_version IS DISTINCT FROM NEW.executor_contract_version
       OR proof.delivery_contract_version IS DISTINCT FROM NEW.delivery_contract_version
       OR proof.charge_contract_version IS DISTINCT FROM NEW.charge_contract_version
       OR proof.idempotency_contract_version IS DISTINCT FROM NEW.idempotency_contract_version THEN
        RAISE EXCEPTION 'public execution binding proof is stale, incomplete, or mismatched'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

DELETE FROM schema_migrations
WHERE version='000078_atomic_model_release_authoring';

COMMIT;
