BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_deployment_lifecycle_receipts,model_aliases,model_registry,
    model_sets,model_set_items,runtime_change_releases,runtime_change_slots,
    audit_logs,schema_migrations
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version>'000107_stable_model_successor_cutover'
    ) THEN
        RAISE EXCEPTION 'Stable model successor rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM model_successor_cutover_receipts)
       OR EXISTS (SELECT 1 FROM model_successor_cutover_receipt_model_sets)
       OR EXISTS (
          SELECT 1 FROM model_deployment_lifecycle_receipts
           WHERE contract_version='unirouter.model-deployment-lifecycle-receipt.v2'
       )
       OR EXISTS (SELECT 1 FROM model_aliases WHERE is_primary) THEN
        RAISE EXCEPTION 'Stable model successor rollback refused: durable successor authority or evidence exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS model_deployment_successor_completion_guard
    ON model_deployment_lifecycle_receipts;
DROP FUNCTION IF EXISTS validate_model_successor_parent_completion();
DROP TRIGGER IF EXISTS model_registry_completed_successor_guard ON model_registry;
DROP FUNCTION IF EXISTS protect_completed_model_successor_registry();
DROP TRIGGER IF EXISTS model_aliases_primary_successor_guard ON model_aliases;
DROP FUNCTION IF EXISTS protect_primary_model_alias();

DROP TRIGGER IF EXISTS model_successor_cutover_receipt_model_sets_truncate_guard
    ON model_successor_cutover_receipt_model_sets;
DROP TRIGGER IF EXISTS model_successor_cutover_receipts_truncate_guard
    ON model_successor_cutover_receipts;
DROP FUNCTION IF EXISTS reject_model_successor_evidence_truncate();
DROP TRIGGER IF EXISTS model_successor_cutover_receipt_model_sets_immutable
    ON model_successor_cutover_receipt_model_sets;
DROP FUNCTION IF EXISTS preserve_model_successor_cutover_model_set_child();
DROP TRIGGER IF EXISTS model_successor_cutover_receipts_immutable
    ON model_successor_cutover_receipts;
DROP FUNCTION IF EXISTS preserve_model_successor_cutover_child();

DROP TABLE model_successor_cutover_receipt_model_sets;
DROP TABLE model_successor_cutover_receipts;

ALTER TABLE model_deployment_lifecycle_receipts
    DROP CONSTRAINT model_deployment_lifecycle_receipts_identity_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_state_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check,
    DROP COLUMN successor_predecessor_public_model_id,
    DROP COLUMN successor_public_model_id,
    DROP COLUMN successor_stable_request_model,
    ADD CONSTRAINT model_deployment_lifecycle_receipts_identity_check CHECK (
        contract_version='unirouter.model-deployment-lifecycle-receipt.v1'
        AND action IN (
            'activate_initial','start_canary','promote','cancel_canary',
            'rollback_stable','cutover_legacy','rollback_legacy'
        )
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

DROP INDEX model_aliases_one_active_primary_target;
ALTER TABLE model_aliases
    DROP CONSTRAINT model_aliases_primary_active_check,
    DROP COLUMN is_primary;

DO $$
DECLARE affected BIGINT;
BEGIN
    DELETE FROM schema_migrations
     WHERE version='000107_stable_model_successor_cutover';
    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected<>1 THEN
        RAISE EXCEPTION 'Stable model successor rollback ledger delete mismatch';
    END IF;
END;
$$;

COMMIT;
