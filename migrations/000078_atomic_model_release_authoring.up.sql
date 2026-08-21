BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_change_releases, runtime_change_slots, orgs, users,
    model_registry, provider_connections, model_deployments,
	    model_deployment_operations, model_operation_production_approvals,
	    model_deployment_probes, model_operation_public_execution_bindings,
	    model_deployment_lifecycle_receipts,
	    model_deployment_lifecycle_receipt_releases,
	    model_deployment_lifecycle_receipt_qualifications,
	    runtime_route_revision_items, runtime_change_events, audit_logs,
    schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- An M77 draft has no durable authoring command identity and cannot be adopted
-- without inventing provenance. Refuse the upgrade before any schema mutation.
-- While the database is still at M77, the operator must use the M77 artifact's
-- supported SealDeploymentDraft path. If that draft cannot be sealed, use the
-- M77 artifact's exact reset procedure, then upgrade and author a new release.
-- The M78 binary deliberately cannot operate either legacy recovery path.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM runtime_change_releases release
         WHERE release.candidate_version='unirouter.runtime-change-candidate.v2'
           AND (release.route_items_sealed_at IS NULL OR release.stage='draft')
    ) THEN
        RAISE EXCEPTION 'Atomic model release authoring migration refused: while schema remains M77, seal every canonical v2 draft with the M77 artifact SealDeploymentDraft; if sealing is impossible, use the M77 artifact exact reset before upgrading and re-authoring under M78 (the M78 binary cannot recover an M77 draft)';
    END IF;
END;
$$;

-- M78 keeps the canonical M76 state machine intact and extends its durable
-- ledger only for the explicit v1 compatibility bridge. Raw slot identity is
-- frozen separately from the receipt's logical public model because bridge
-- actions intentionally change runtime_change_slots.public_model_id NULL<->ID.
DROP TRIGGER model_deployment_lifecycle_receipts_immutable
    ON model_deployment_lifecycle_receipts;

ALTER TABLE model_deployment_lifecycle_receipts
    DROP CONSTRAINT model_deployment_lifecycle_receipts_identity_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_state_check,
    DROP CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check,
    ADD COLUMN before_slot_public_model_id BIGINT REFERENCES model_registry(id) ON DELETE RESTRICT,
    ADD COLUMN after_slot_public_model_id BIGINT REFERENCES model_registry(id) ON DELETE RESTRICT,
    ADD COLUMN legacy_plan_hash VARCHAR(71),
    ADD COLUMN deployment_plan_hash VARCHAR(71);

ALTER TABLE model_deployment_lifecycle_receipts
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

CREATE FUNCTION preserve_legacy_model_deployment_lifecycle_receipt()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    audit_count BIGINT;
    release_count BIGINT;
    matched_release_count BIGINT;
    slot_count BIGINT;
    event_count BIGINT;
    receipt_event_count BIGINT;
    qualification_count BIGINT;
    expected_qualification_count BIGINT;
    target_model_name TEXT;
    target_parent_release_id BIGINT;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'in_progress' THEN
            RAISE EXCEPTION 'legacy deployment lifecycle receipt must be claimed in progress'
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
        RAISE EXCEPTION 'legacy deployment lifecycle receipt identity is immutable'
            USING ERRCODE='55000';
    END IF;

    PERFORM actor.id
      FROM users actor
      JOIN orgs actor_org ON actor_org.id=actor.org_id
     WHERE actor.id=NEW.actor_user_id AND actor.org_id=NEW.actor_org_id
       AND actor.status='active' AND actor_org.status='active'
       AND (COALESCE(NULLIF(actor.organization_role,''),actor.role)='platform_admin'
         OR (COALESCE(NULLIF(actor.organization_role,''),actor.role)='org_admin'
             AND actor.org_id=NEW.customer_org_id))
     FOR SHARE OF actor,actor_org;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires an active authorized actor'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO audit_count
      FROM audit_logs audit
     WHERE audit.id=NEW.audit_log_id
       AND audit.org_id=NEW.customer_org_id AND audit.user_id=NEW.actor_user_id
       AND audit.action='model_deployment.lifecycle.' || NEW.action
       AND audit.resource='model_deployment_release'
       AND audit.resource_id=NEW.target_release_id::TEXT
       AND audit.detail->>'receipt_id'=NEW.receipt_id::TEXT
       AND audit.detail->>'contract_version'=NEW.contract_version
       AND audit.detail->>'action'=NEW.action
       AND audit.detail->>'server_operation_id'=NEW.server_operation_id
       AND audit.detail->>'runtime_mutation'='bounded_route_assignment'
       AND audit.detail->>'legacy_plan_hash'=NEW.legacy_plan_hash
       AND audit.detail->>'deployment_plan_hash' IS NOT DISTINCT FROM NEW.deployment_plan_hash
       AND audit.detail#>>'{before,slot,public_model_id}' IS NOT DISTINCT FROM
           CASE WHEN NEW.before_slot_public_model_id IS NULL
                THEN NULL ELSE NEW.before_slot_public_model_id::TEXT END
       AND audit.detail#>>'{after,slot,public_model_id}' IS NOT DISTINCT FROM
           CASE WHEN NEW.after_slot_public_model_id IS NULL
                THEN NULL ELSE NEW.after_slot_public_model_id::TEXT END;
    IF audit_count<>1 THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires the exact audit row'
            USING ERRCODE='23514';
    END IF;

    SELECT release.model_name,release.parent_stable_release_id
      INTO target_model_name,target_parent_release_id
      FROM runtime_change_releases release
     WHERE release.id=NEW.target_release_id
       AND release.release_ref=NEW.target_release_ref
       AND release.candidate_version='unirouter.runtime-change-candidate.v2'
       AND release.public_model_id=NEW.public_model_id
     FOR SHARE;
    IF NOT FOUND OR target_parent_release_id IS NULL THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires exact target lineage'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO release_count
      FROM model_deployment_lifecycle_receipt_releases evidence
     WHERE evidence.receipt_id=NEW.receipt_id;
    IF release_count<>2 THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires two release images'
            USING ERRCODE='23514';
    END IF;
    PERFORM release.id
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_releases release ON release.id=evidence.release_id
     WHERE evidence.receipt_id=NEW.receipt_id
     ORDER BY release.id
     FOR SHARE OF release;

    SELECT COUNT(*) INTO matched_release_count
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_releases release ON release.id=evidence.release_id
      JOIN runtime_change_releases target ON target.id=NEW.target_release_id
     WHERE evidence.receipt_id=NEW.receipt_id
       AND release.release_ref=evidence.release_ref
       AND release.candidate_hash=evidence.candidate_hash
       AND release.route_items_hash=evidence.route_items_hash
       AND release.route_items_sealed_at IS NOT NULL
       AND release.route_items_count>0
       AND release.stage=evidence.after_stage
       AND release.version=evidence.after_version
       AND evidence.after_version=evidence.before_version+1
       AND (
         (evidence.release_role='target'
           AND release.id=NEW.target_release_id
           AND release.candidate_version='unirouter.runtime-change-candidate.v2'
           AND release.public_model_id=NEW.public_model_id
           AND release.model_name=target_model_name
           AND release.parent_stable_release_id=target_parent_release_id
           AND release.baseline_hash=(
               SELECT parent.candidate_hash FROM runtime_change_releases parent
                WHERE parent.id=target_parent_release_id)
           AND evidence.before_version=NEW.expected_release_version
           AND ((NEW.action='cutover_legacy'
                  AND evidence.before_stage='validated' AND evidence.after_stage='active')
             OR (NEW.action='rollback_legacy'
                  AND evidence.before_stage='active' AND evidence.after_stage='validated')))
         OR
         (evidence.release_role='stable_parent'
           AND release.id=target_parent_release_id
           AND release.candidate_version='unirouter.runtime-change-candidate.v1'
           AND release.public_model_id=NEW.public_model_id
           AND release.model_name=target_model_name
           AND release.candidate_hash=target.baseline_hash
           AND ((NEW.action='cutover_legacy'
                  AND evidence.before_stage='active' AND evidence.after_stage='rolled_back')
             OR (NEW.action='rollback_legacy'
                  AND evidence.before_stage='rolled_back' AND evidence.after_stage='active')))
       );
    IF matched_release_count<>2 THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires exact dual-release evidence'
            USING ERRCODE='23514';
    END IF;

    IF NEW.before_slot_version<>NEW.expected_slot_version
       OR NEW.after_slot_version<>NEW.expected_slot_version+1 THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires exact slot versions'
            USING ERRCODE='23514';
    END IF;
    PERFORM slot.model_name
      FROM runtime_change_slots slot
     WHERE slot.model_name=target_model_name OR slot.public_model_id=NEW.public_model_id
     ORDER BY slot.model_name
     FOR SHARE OF slot;
    SELECT COUNT(*) INTO slot_count
      FROM runtime_change_slots slot
     WHERE slot.model_name=target_model_name OR slot.public_model_id=NEW.public_model_id;
    IF slot_count<>1 OR NOT EXISTS (
        SELECT 1 FROM runtime_change_slots slot
         WHERE slot.model_name=target_model_name
           AND slot.public_model_id IS NOT DISTINCT FROM NEW.after_slot_public_model_id
           AND slot.stable_release_id IS NOT DISTINCT FROM NEW.after_stable_release_id
           AND slot.canary_release_id IS NOT DISTINCT FROM NEW.after_canary_release_id
           AND slot.canary_basis_points=NEW.after_canary_basis_points
           AND slot.slot_version=NEW.after_slot_version
    ) THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires exact raw after slot'
            USING ERRCODE='23514';
    END IF;

    SELECT COUNT(*) INTO event_count
      FROM model_deployment_lifecycle_receipt_releases evidence
      JOIN runtime_change_events event
        ON event.release_id=evidence.release_id
       AND event.sequence=evidence.after_version
     WHERE evidence.receipt_id=NEW.receipt_id
       AND event.actor_user_id=NEW.actor_user_id
       AND event.expected_version=evidence.before_version
       AND event.resulting_version=evidence.after_version
       AND event.from_stage=evidence.before_stage
       AND event.to_stage=evidence.after_stage
       AND event.action=CASE
           WHEN evidence.after_stage='rolled_back' THEN 'rollback'
           WHEN evidence.before_stage='active' AND evidence.after_stage='validated' THEN 'rollback'
           ELSE 'promote' END
       AND event.detail->>'lifecycle_receipt_id'=NEW.receipt_id::TEXT
       AND event.detail->>'canonical_lifecycle'='true'
       AND event.detail->>'legacy_bridge'='true'
       AND event.detail->>'public_model_id'=NEW.public_model_id::TEXT
       AND event.detail->>'route_items_hash'=evidence.route_items_hash
       AND event.detail->>'expected_slot_version'=NEW.before_slot_version::TEXT
       AND event.detail->>'resulting_slot_version'=NEW.after_slot_version::TEXT
       AND event.detail->>'legacy_plan_hash'=NEW.legacy_plan_hash
       AND event.detail->>'deployment_plan_hash' IS NOT DISTINCT FROM NEW.deployment_plan_hash;
    SELECT COUNT(*) INTO receipt_event_count
      FROM runtime_change_events event
     WHERE event.detail->>'lifecycle_receipt_id'=NEW.receipt_id::TEXT;
    IF event_count<>2 OR receipt_event_count<>2 THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires exactly two receipt events'
            USING ERRCODE='23514';
    END IF;

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
        ON operation.deployment_id=item.deployment_id AND operation.operation_id=item.operation_id
     WHERE release_evidence.receipt_id=NEW.receipt_id
     ORDER BY deployment.id,operation.operation_id,item.release_id,item.ordinal
     FOR SHARE OF deployment,connection,operation;

    SELECT COUNT(*) INTO expected_qualification_count
      FROM model_deployment_lifecycle_receipt_releases release_evidence
      JOIN runtime_route_revision_items item ON item.release_id=release_evidence.release_id
     WHERE release_evidence.receipt_id=NEW.receipt_id;
    SELECT COUNT(*) INTO qualification_count
      FROM model_deployment_lifecycle_receipt_qualifications evidence
     WHERE evidence.receipt_id=NEW.receipt_id;
    IF qualification_count<>expected_qualification_count OR EXISTS (
        SELECT 1
          FROM model_deployment_lifecycle_receipt_releases release_evidence
          JOIN runtime_route_revision_items item ON item.release_id=release_evidence.release_id
          JOIN model_deployments deployment ON deployment.id=item.deployment_id
          JOIN provider_connections connection ON connection.id=deployment.connection_id
          JOIN model_deployment_operations operation
            ON operation.deployment_id=item.deployment_id AND operation.operation_id=item.operation_id
          LEFT JOIN model_operation_execution_qualifications qualification
            ON qualification.deployment_id=item.deployment_id AND qualification.operation_id=item.operation_id
          LEFT JOIN model_deployment_lifecycle_receipt_qualifications evidence
            ON evidence.receipt_id=release_evidence.receipt_id
           AND evidence.release_id=item.release_id
           AND evidence.release_role=release_evidence.release_role
           AND evidence.ordinal=item.ordinal
         WHERE release_evidence.receipt_id=NEW.receipt_id
           AND (evidence.receipt_id IS NULL
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
             OR (item.operation_id NOT IN ('openai.responses.v1','anthropic.messages.v1')
               AND (evidence.qualification_required OR NOT evidence.qualified
                    OR evidence.qualification_state<>'not_required'
                    OR evidence.approval_id IS NOT NULL OR evidence.probe_id IS NOT NULL))
             OR (item.operation_id IN ('openai.responses.v1','anthropic.messages.v1')
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
                    OR evidence.approved_operation_revision IS DISTINCT FROM qualification.approved_operation_revision)))
    ) THEN
        RAISE EXCEPTION 'legacy deployment lifecycle completion requires exact qualification evidence'
            USING ERRCODE='23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM model_deployment_lifecycle_receipt_qualifications evidence
         WHERE evidence.receipt_id=NEW.receipt_id
           AND evidence.qualification_required AND evidence.qualified IS DISTINCT FROM TRUE
           AND ((NEW.action='cutover_legacy' AND evidence.release_role='target')
             OR (NEW.action='rollback_legacy' AND evidence.release_role='stable_parent'))
    ) THEN
        RAISE EXCEPTION 'legacy deployment lifecycle authority expansion requires current production qualification'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION reject_model_deployment_lifecycle_receipt_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model deployment lifecycle receipts are durable evidence'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_deployment_lifecycle_receipts_canonical_immutable
BEFORE INSERT OR UPDATE ON model_deployment_lifecycle_receipts
FOR EACH ROW WHEN (NEW.action NOT IN ('cutover_legacy','rollback_legacy'))
EXECUTE FUNCTION preserve_model_deployment_lifecycle_receipt();

CREATE TRIGGER model_deployment_lifecycle_receipts_legacy_immutable
BEFORE INSERT OR UPDATE ON model_deployment_lifecycle_receipts
FOR EACH ROW WHEN (NEW.action IN ('cutover_legacy','rollback_legacy'))
EXECUTE FUNCTION preserve_legacy_model_deployment_lifecycle_receipt();

CREATE TRIGGER model_deployment_lifecycle_receipts_immutable
BEFORE DELETE ON model_deployment_lifecycle_receipts
FOR EACH ROW EXECUTE FUNCTION reject_model_deployment_lifecycle_receipt_delete();

-- M77 accidentally encoded a same-tenant creator instead of the authenticated
-- actor organization. Platform administrators can legitimately author a
-- customer release from the platform organization; tenant org_admin remains
-- limited to its own customer organization.
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
        AND created_by>0 AND created_by_org_id>0
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
           actor.org_id AS actor_org_id,
           COALESCE(NULLIF(actor.organization_role,''),actor.role) AS actor_role,
           actor.status AS actor_status,actor_org.status AS actor_org_status
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
      JOIN users actor ON actor.id=NEW.created_by AND actor.org_id=NEW.created_by_org_id
      JOIN orgs actor_org ON actor_org.id=actor.org_id
     WHERE release_row.id=NEW.release_id
     FOR SHARE OF release_row,deployment,connection,operation,actor,actor_org;

    IF NOT FOUND
       OR proof.candidate_version<>'unirouter.runtime-change-candidate.v2'
       OR proof.stage<>'draft' OR proof.route_items_sealed_at IS NOT NULL
       OR proof.public_model_id IS DISTINCT FROM NEW.public_model_id
       OR proof.created_by IS DISTINCT FROM NEW.created_by
       OR proof.customer_org_id IS DISTINCT FROM NEW.customer_org_id
       OR proof.deployment_public_model_id IS DISTINCT FROM NEW.public_model_id
       OR proof.connection_id IS DISTINCT FROM NEW.connection_id
       OR proof.actor_org_id IS DISTINCT FROM NEW.created_by_org_id
       OR proof.actor_status<>'active' OR proof.actor_org_status<>'active'
       OR (proof.actor_role<>'platform_admin'
           AND (proof.actor_role<>'org_admin'
                OR proof.actor_org_id IS DISTINCT FROM NEW.customer_org_id))
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
        RAISE EXCEPTION 'public execution binding proof is stale, incomplete, mismatched, or unauthorized'
            USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE model_deployment_release_authoring_receipts (
    receipt_id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(100) NOT NULL,
    customer_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    actor_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    operation_scope VARCHAR(100) NOT NULL,
    idempotency_key_hash CHAR(64) NOT NULL,
    request_fingerprint VARCHAR(76) NOT NULL,
    server_operation_id VARCHAR(80) NOT NULL,
    public_model_id BIGINT NOT NULL,
    baseline_kind VARCHAR(16) NOT NULL,
    expected_slot_version BIGINT NOT NULL,
    expected_stable_release_ref VARCHAR(80),
    expected_stable_release_version BIGINT,
    requested_selections JSONB NOT NULL,
    reason TEXT NOT NULL,
    target_release_ref VARCHAR(80) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    release_id BIGINT,
    model_name VARCHAR(160),
    candidate_hash VARCHAR(71),
    route_items_hash VARCHAR(71),
    route_items_count INTEGER,
    original_http_status SMALLINT,
    original_location VARCHAR(255),
    audit_log_id BIGINT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT transaction_timestamp(),
    completed_at TIMESTAMPTZ(6),
    CONSTRAINT model_deployment_release_authoring_receipts_actor_fk
        FOREIGN KEY (actor_user_id,actor_org_id)
        REFERENCES users(id,org_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_release_authoring_receipts_model_fk
        FOREIGN KEY (public_model_id,customer_org_id)
        REFERENCES model_registry(id,org_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_release_authoring_receipts_key_unique
        UNIQUE (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash),
    CONSTRAINT model_deployment_release_authoring_receipts_release_unique
        UNIQUE (release_id),
    CONSTRAINT model_deployment_release_authoring_receipts_identity_check CHECK (
        contract_version='unirouter.model-deployment-release-authoring-receipt.v1'
        AND operation_scope='model_deployment.release_authoring.create.v1'
        AND idempotency_key_hash ~ '^[a-f0-9]{64}$'
        AND request_fingerprint ~ '^hmac-sha256:[a-f0-9]{64}$'
        AND server_operation_id=BTRIM(server_operation_id)
        AND BTRIM(server_operation_id)<>''
        AND server_operation_id !~ E'[\\x00\\r\\n]'
        AND public_model_id>0
        AND target_release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND jsonb_typeof(requested_selections)='array'
        AND jsonb_array_length(requested_selections) BETWEEN 1 AND 256
        AND reason=BTRIM(reason) AND BTRIM(reason)<>''
        AND reason !~ E'[\\x00\\r\\n]' AND OCTET_LENGTH(reason)<=4000
        AND (
          (baseline_kind='initial' AND expected_slot_version=0
           AND expected_stable_release_ref IS NULL
           AND expected_stable_release_version IS NULL)
          OR
	          (baseline_kind IN ('stable','legacy_cutover') AND expected_slot_version>0
           AND expected_stable_release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
           AND expected_stable_release_version>0)
        )
    ),
    CONSTRAINT model_deployment_release_authoring_receipts_state_check CHECK (
        (status='in_progress' AND release_id IS NULL AND model_name IS NULL
         AND candidate_hash IS NULL AND route_items_hash IS NULL
         AND route_items_count IS NULL AND original_http_status IS NULL
         AND original_location IS NULL AND audit_log_id IS NULL
         AND completed_at IS NULL)
        OR
        (status='completed' AND release_id IS NOT NULL AND release_id>0
         AND model_name IS NOT NULL AND BTRIM(model_name)<>''
         AND candidate_hash ~ '^sha256:[a-f0-9]{64}$'
         AND route_items_hash ~ '^sha256:[a-f0-9]{64}$'
         AND route_items_count BETWEEN 1 AND 256
         AND original_http_status=201
         AND original_location='/api/v1/model-deployment-releases/' || target_release_ref
         AND audit_log_id IS NOT NULL AND audit_log_id>0
         AND completed_at IS NOT NULL)
    )
);

CREATE INDEX model_deployment_release_authoring_receipts_model_idx
    ON model_deployment_release_authoring_receipts(
        customer_org_id,public_model_id,created_at DESC,receipt_id DESC
    );
CREATE INDEX model_deployment_release_authoring_receipts_operation_idx
    ON model_deployment_release_authoring_receipts(server_operation_id);

ALTER TABLE runtime_change_releases
    ADD COLUMN authoring_receipt_id BIGINT
        REFERENCES model_deployment_release_authoring_receipts(receipt_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT runtime_change_releases_authoring_receipt_unique
        UNIQUE (authoring_receipt_id);

CREATE FUNCTION preserve_model_deployment_release_authoring_receipt()
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

CREATE TRIGGER model_deployment_release_authoring_receipts_immutable
BEFORE INSERT OR UPDATE OR DELETE ON model_deployment_release_authoring_receipts
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_release_authoring_receipt();

CREATE FUNCTION require_terminal_model_deployment_release_authoring_receipt()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE current_status TEXT;
BEGIN
    SELECT status INTO current_status
      FROM model_deployment_release_authoring_receipts
     WHERE receipt_id=NEW.receipt_id;
    IF current_status IS DISTINCT FROM 'completed' THEN
        RAISE EXCEPTION 'model deployment release authoring receipt cannot commit in progress'
            USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER model_deployment_release_authoring_receipts_terminal
AFTER INSERT OR UPDATE ON model_deployment_release_authoring_receipts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION require_terminal_model_deployment_release_authoring_receipt();

CREATE FUNCTION preserve_runtime_release_authoring_link()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE proof RECORD;
BEGIN
    IF TG_OP='UPDATE' AND NEW.authoring_receipt_id IS DISTINCT FROM OLD.authoring_receipt_id THEN
        RAISE EXCEPTION 'runtime release authoring receipt identity is immutable'
            USING ERRCODE='55000';
    END IF;
    IF NEW.candidate_version<>'unirouter.runtime-change-candidate.v2' THEN
        IF NEW.authoring_receipt_id IS NOT NULL THEN
            RAISE EXCEPTION 'only canonical v2 releases may bind an authoring receipt'
                USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='INSERT' OR (OLD.route_items_sealed_at IS NULL AND NEW.route_items_sealed_at IS NOT NULL) THEN
        IF NEW.authoring_receipt_id IS NULL THEN
            RAISE EXCEPTION 'new canonical v2 create and seal require an in-progress authoring receipt'
                USING ERRCODE='55000';
        END IF;
        SELECT receipt.status,receipt.customer_org_id,receipt.actor_user_id,
               receipt.public_model_id,receipt.target_release_ref,receipt.reason,
               model.org_id AS model_org_id
          INTO proof
          FROM model_deployment_release_authoring_receipts receipt
          JOIN model_registry model ON model.id=NEW.public_model_id
         WHERE receipt.receipt_id=NEW.authoring_receipt_id
         FOR SHARE OF receipt,model;
        IF NOT FOUND OR proof.status<>'in_progress'
           OR proof.customer_org_id IS DISTINCT FROM proof.model_org_id
           OR proof.actor_user_id IS DISTINCT FROM NEW.created_by
           OR proof.public_model_id IS DISTINCT FROM NEW.public_model_id
           OR proof.target_release_ref IS DISTINCT FROM NEW.release_ref
           OR proof.reason IS DISTINCT FROM NEW.reason THEN
            RAISE EXCEPTION 'canonical v2 release requires its exact in-progress authoring receipt'
                USING ERRCODE='55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER runtime_change_releases_authoring_link_guard
BEFORE INSERT OR UPDATE ON runtime_change_releases
FOR EACH ROW EXECUTE FUNCTION preserve_runtime_release_authoring_link();

-- Post-M78 v2 events are never unproven additions: authoring owns exactly the
-- create/validate pair, while later M76 transitions own lifecycle events.
CREATE OR REPLACE FUNCTION preserve_runtime_change_event_receipt_link()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    authoring_key BOOLEAN := NEW.detail ? 'authoring_receipt_id';
    lifecycle_key BOOLEAN := NEW.detail ? 'lifecycle_receipt_id';
    linked_receipt_id BIGINT;
    parent_status TEXT;
    release_version TEXT;
    release_authoring_receipt_id BIGINT;
BEGIN
    SELECT candidate_version,authoring_receipt_id
      INTO release_version,release_authoring_receipt_id
      FROM runtime_change_releases
     WHERE id=NEW.release_id
     FOR SHARE;
    IF release_version='unirouter.runtime-change-candidate.v2'
       AND authoring_key=lifecycle_key THEN
        RAISE EXCEPTION 'canonical v2 runtime event requires exactly one receipt provenance'
            USING ERRCODE='23514';
    END IF;
    IF authoring_key AND lifecycle_key THEN
        RAISE EXCEPTION 'runtime event receipt provenance is mutually exclusive'
            USING ERRCODE='23514';
    END IF;
    IF NOT authoring_key AND NOT lifecycle_key THEN
        RETURN NEW;
    END IF;
    IF authoring_key THEN
        IF NEW.detail->>'authoring_receipt_id' !~ '^[1-9][0-9]*$' THEN
            RAISE EXCEPTION 'runtime change event requires a positive authoring receipt identity'
                USING ERRCODE='23514';
        END IF;
        linked_receipt_id := (NEW.detail->>'authoring_receipt_id')::BIGINT;
        SELECT status INTO parent_status
          FROM model_deployment_release_authoring_receipts
         WHERE receipt_id=linked_receipt_id
         FOR SHARE;
        IF parent_status IS DISTINCT FROM 'in_progress'
           OR release_authoring_receipt_id IS DISTINCT FROM linked_receipt_id
           OR NEW.action NOT IN ('create','validate') THEN
            RAISE EXCEPTION 'runtime change event requires its in-progress authoring receipt'
                USING ERRCODE='55000';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.detail->>'lifecycle_receipt_id' !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION 'runtime change event requires a positive lifecycle receipt identity'
            USING ERRCODE='23514';
    END IF;
    linked_receipt_id := (NEW.detail->>'lifecycle_receipt_id')::BIGINT;
    SELECT status INTO parent_status
      FROM model_deployment_lifecycle_receipts
     WHERE receipt_id=linked_receipt_id
     FOR SHARE;
    IF parent_status IS DISTINCT FROM 'in_progress' THEN
        RAISE EXCEPTION 'runtime change event requires an in-progress lifecycle receipt'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
EXCEPTION WHEN numeric_value_out_of_range OR invalid_text_representation THEN
    RAISE EXCEPTION 'runtime change event requires a positive receipt identity'
        USING ERRCODE='23514';
END;
$$;

CREATE FUNCTION reject_model_deployment_release_authoring_receipt_truncate()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model deployment release authoring receipt evidence cannot be truncated'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_deployment_release_authoring_receipts_truncate_guard
BEFORE TRUNCATE ON model_deployment_release_authoring_receipts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_deployment_release_authoring_receipt_truncate();

COMMENT ON TABLE model_deployment_release_authoring_receipts IS
    'Durable non-expiring actor-scoped receipt for one atomic canonical v2 draft-to-validated authoring command.';
COMMENT ON COLUMN runtime_change_releases.authoring_receipt_id IS
    'M78 command provenance for canonical v2 releases created after this column existed; historical sealed v2 releases may be NULL.';

COMMIT;
