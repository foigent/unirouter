BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE orgs, users, model_registry, model_deployments,
    model_deployment_operations, model_operation_production_approvals,
    model_deployment_probes, runtime_change_releases, runtime_change_slots,
    runtime_change_events, audit_logs, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

-- Canonical v2 lifecycle mutations have a durable, actor-scoped idempotency
-- receipt. The receipt is product evidence, not a short-lived response cache:
-- it never stores the raw Idempotency-Key or request body and never expires.
CREATE TABLE model_deployment_lifecycle_receipts (
    receipt_id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(80) NOT NULL,
    customer_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    actor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    actor_org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    operation_scope VARCHAR(80) NOT NULL,
    action VARCHAR(32) NOT NULL,
    idempotency_key_hash CHAR(64) NOT NULL,
    request_fingerprint VARCHAR(76) NOT NULL,
    server_operation_id VARCHAR(80) NOT NULL,
    target_release_ref VARCHAR(80) NOT NULL,
    target_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    public_model_id BIGINT,
    expected_release_version BIGINT NOT NULL,
    expected_slot_version BIGINT NOT NULL,
    requested_canary_basis_points INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    original_http_status SMALLINT,
    before_stable_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    before_canary_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    before_canary_basis_points INTEGER,
    before_slot_version BIGINT,
    after_stable_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    after_canary_release_id BIGINT REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    after_canary_basis_points INTEGER,
    after_slot_version BIGINT,
    audit_log_id BIGINT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT transaction_timestamp(),
    completed_at TIMESTAMPTZ(6),
    CONSTRAINT model_deployment_lifecycle_receipts_actor_tenant_fk
        FOREIGN KEY (actor_user_id,actor_org_id)
        REFERENCES users(id,org_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_lifecycle_receipts_model_tenant_fk
        FOREIGN KEY (public_model_id,customer_org_id)
        REFERENCES model_registry(id,org_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_lifecycle_receipts_key_unique
        UNIQUE (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash),
    CONSTRAINT model_deployment_lifecycle_receipts_identity_check CHECK (
        contract_version='unirouter.model-deployment-lifecycle-receipt.v1'
        AND action IN ('activate_initial','start_canary','promote','cancel_canary','rollback_stable')
        AND operation_scope='model_deployment.lifecycle.' || action || '.v1'
        AND idempotency_key_hash ~ '^[a-f0-9]{64}$'
        AND request_fingerprint ~ '^hmac-sha256:[a-f0-9]{64}$'
        AND server_operation_id=BTRIM(server_operation_id)
        AND BTRIM(server_operation_id)<>''
        AND server_operation_id !~ E'[\\x00\\r\\n]'
        AND target_release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND expected_release_version>0
        AND expected_slot_version>=0
        AND reason=BTRIM(reason)
        AND reason !~ E'[\\x00\\r\\n]'
        AND OCTET_LENGTH(reason)<=4000
        AND (
            (action='activate_initial' AND expected_slot_version=0
                AND requested_canary_basis_points=0)
            OR
            (action='start_canary' AND expected_slot_version>0
                AND requested_canary_basis_points BETWEEN 1 AND 5000)
            OR
            (action IN ('promote','cancel_canary','rollback_stable')
                AND expected_slot_version>0 AND requested_canary_basis_points=0)
        )
    ),
    CONSTRAINT model_deployment_lifecycle_receipts_state_check CHECK (
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
    CONSTRAINT model_deployment_lifecycle_receipts_slot_shape_check CHECK (
        status<>'completed' OR (
            (before_canary_release_id IS NULL AND before_canary_basis_points=0)
            OR (before_canary_release_id IS NOT NULL AND before_canary_basis_points BETWEEN 1 AND 5000)
        ) AND (
            (after_canary_release_id IS NULL AND after_canary_basis_points=0)
            OR (after_canary_release_id IS NOT NULL AND after_canary_basis_points BETWEEN 1 AND 5000)
        ) AND (
            action<>'activate_initial'
            OR (before_slot_version=0 AND before_stable_release_id IS NULL
                AND before_canary_release_id IS NULL AND after_slot_version=1
                AND after_stable_release_id=target_release_id
                AND after_canary_release_id IS NULL)
        ) AND (
            action<>'start_canary'
            OR (before_stable_release_id IS NOT NULL AND before_canary_release_id IS NULL
                AND after_stable_release_id=before_stable_release_id
                AND after_canary_release_id=target_release_id
                AND after_canary_basis_points=requested_canary_basis_points)
        ) AND (
            action<>'promote'
            OR (before_stable_release_id IS NOT NULL
                AND before_canary_release_id=target_release_id
                AND after_stable_release_id=target_release_id
                AND after_canary_release_id IS NULL)
        ) AND (
            action<>'cancel_canary'
            OR (before_stable_release_id IS NOT NULL
                AND before_canary_release_id=target_release_id
                AND after_stable_release_id=before_stable_release_id
                AND after_canary_release_id IS NULL)
        ) AND (
            action<>'rollback_stable'
            OR (before_stable_release_id=target_release_id
                AND before_canary_release_id IS NULL
                AND after_stable_release_id IS NOT NULL
                AND after_stable_release_id<>target_release_id
                AND after_canary_release_id IS NULL)
        )
    )
);

CREATE INDEX model_deployment_lifecycle_receipts_target_idx
    ON model_deployment_lifecycle_receipts(customer_org_id,target_release_ref,created_at DESC,receipt_id DESC);
CREATE INDEX model_deployment_lifecycle_receipts_operation_idx
    ON model_deployment_lifecycle_receipts(server_operation_id);

CREATE TABLE model_deployment_lifecycle_receipt_releases (
    receipt_id BIGINT NOT NULL REFERENCES model_deployment_lifecycle_receipts(receipt_id) ON DELETE RESTRICT,
    release_role VARCHAR(24) NOT NULL,
    release_id BIGINT NOT NULL REFERENCES runtime_change_releases(id) ON DELETE RESTRICT,
    release_ref VARCHAR(80) NOT NULL,
    candidate_hash VARCHAR(71) NOT NULL,
    route_items_hash VARCHAR(71) NOT NULL,
    before_stage VARCHAR(24) NOT NULL,
    before_version BIGINT NOT NULL,
    after_stage VARCHAR(24) NOT NULL,
    after_version BIGINT NOT NULL,
    PRIMARY KEY (receipt_id,release_role),
    CONSTRAINT model_deployment_lifecycle_receipt_releases_identity_unique
        UNIQUE (receipt_id,release_id,release_role),
    CONSTRAINT model_deployment_lifecycle_receipt_releases_shape_check CHECK (
        release_role IN ('target','stable_parent')
        AND release_ref ~ '^rcr_[a-z0-9][a-z0-9._-]{2,63}$'
        AND candidate_hash ~ '^sha256:[a-f0-9]{64}$'
        AND route_items_hash ~ '^sha256:[a-f0-9]{64}$'
        AND before_stage IN ('validated','canary','active','rolled_back')
        AND after_stage IN ('validated','canary','active','rolled_back')
        AND before_version>0 AND after_version>=before_version
        AND after_version<=before_version+1
    )
);

CREATE TABLE model_deployment_lifecycle_receipt_qualifications (
    receipt_id BIGINT NOT NULL,
    release_id BIGINT NOT NULL,
    release_role VARCHAR(24) NOT NULL,
    ordinal INTEGER NOT NULL,
    deployment_id BIGINT NOT NULL,
    operation_id VARCHAR(80) NOT NULL,
    qualification_required BOOLEAN NOT NULL,
    qualified BOOLEAN NOT NULL,
    qualification_state VARCHAR(80) NOT NULL,
    approval_id BIGINT REFERENCES model_operation_production_approvals(approval_id) ON DELETE RESTRICT,
    probe_id BIGINT REFERENCES model_deployment_probes(probe_id) ON DELETE RESTRICT,
    connection_id BIGINT REFERENCES provider_connections(id) ON DELETE RESTRICT,
    https_origin TEXT,
    public_path VARCHAR(255),
    executor_contract_version VARCHAR(100),
    expected_connection_revision BIGINT NOT NULL,
    expected_deployment_revision BIGINT NOT NULL,
    expected_operation_revision BIGINT NOT NULL,
    current_connection_revision BIGINT,
    current_deployment_revision BIGINT,
    current_operation_revision BIGINT,
    approved_connection_revision BIGINT,
    approved_deployment_revision BIGINT,
    approved_operation_revision BIGINT,
    PRIMARY KEY (receipt_id,release_id,ordinal),
    CONSTRAINT model_deployment_lifecycle_receipt_qualifications_release_fk
        FOREIGN KEY (receipt_id,release_id,release_role)
        REFERENCES model_deployment_lifecycle_receipt_releases(receipt_id,release_id,release_role)
        ON DELETE RESTRICT,
    CONSTRAINT model_deployment_lifecycle_receipt_qualifications_operation_fk
        FOREIGN KEY (deployment_id,operation_id)
        REFERENCES model_deployment_operations(deployment_id,operation_id) ON DELETE RESTRICT,
    CONSTRAINT model_deployment_lifecycle_receipt_qualifications_shape_check CHECK (
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
    )
);

CREATE FUNCTION preserve_model_deployment_lifecycle_receipt()
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

CREATE TRIGGER model_deployment_lifecycle_receipts_immutable
BEFORE INSERT OR UPDATE OR DELETE ON model_deployment_lifecycle_receipts
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_lifecycle_receipt();

CREATE FUNCTION require_terminal_model_deployment_lifecycle_receipt()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    current_status TEXT;
BEGIN
    SELECT status INTO current_status
      FROM model_deployment_lifecycle_receipts
     WHERE receipt_id=NEW.receipt_id;
    IF current_status IS DISTINCT FROM 'completed' THEN
        RAISE EXCEPTION 'model deployment lifecycle receipt cannot commit in progress'
            USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER model_deployment_lifecycle_receipts_terminal
AFTER INSERT OR UPDATE ON model_deployment_lifecycle_receipts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION require_terminal_model_deployment_lifecycle_receipt();

CREATE FUNCTION preserve_model_deployment_lifecycle_receipt_child()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    parent_status TEXT;
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'model deployment lifecycle receipt evidence is append-only'
            USING ERRCODE='55000';
    END IF;
    SELECT status INTO parent_status
      FROM model_deployment_lifecycle_receipts
     WHERE receipt_id=NEW.receipt_id
     FOR SHARE;
    IF parent_status IS DISTINCT FROM 'in_progress' THEN
        RAISE EXCEPTION 'model deployment lifecycle receipt evidence is terminal'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER model_deployment_lifecycle_receipt_releases_append_only
BEFORE INSERT OR UPDATE OR DELETE ON model_deployment_lifecycle_receipt_releases
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_lifecycle_receipt_child();
CREATE TRIGGER model_deployment_lifecycle_receipt_qualifications_append_only
BEFORE INSERT OR UPDATE OR DELETE ON model_deployment_lifecycle_receipt_qualifications
FOR EACH ROW EXECUTE FUNCTION preserve_model_deployment_lifecycle_receipt_child();

-- Lifecycle events are append-only history. M76 receipts bind exact event
-- rows at completion, so a later UPDATE, DELETE or TRUNCATE must not be able
-- to sever or rewrite that durable decision chain.
CREATE FUNCTION reject_runtime_change_event_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'runtime change events are append-only evidence'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER runtime_change_events_append_only
BEFORE UPDATE OR DELETE OR TRUNCATE ON runtime_change_events
FOR EACH STATEMENT EXECUTE FUNCTION reject_runtime_change_event_mutation();

-- Legacy runtime events remain insertable without a receipt link. Once an
-- event claims M76 evidence identity, however, its parent must exist and still
-- be in progress. FOR SHARE conflicts with the parent's completion UPDATE, so
-- an insert cannot cross the terminal publication point in either direction.
CREATE FUNCTION preserve_runtime_change_event_receipt_link()
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

CREATE TRIGGER runtime_change_events_receipt_link_guard
BEFORE INSERT ON runtime_change_events
FOR EACH ROW EXECUTE FUNCTION preserve_runtime_change_event_receipt_link();

CREATE FUNCTION reject_model_deployment_lifecycle_receipt_truncate()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model deployment lifecycle receipt evidence cannot be truncated'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER model_deployment_lifecycle_receipts_truncate_guard
BEFORE TRUNCATE ON model_deployment_lifecycle_receipts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_deployment_lifecycle_receipt_truncate();
CREATE TRIGGER model_deployment_lifecycle_receipt_releases_truncate_guard
BEFORE TRUNCATE ON model_deployment_lifecycle_receipt_releases
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_deployment_lifecycle_receipt_truncate();
CREATE TRIGGER model_deployment_lifecycle_receipt_qualifications_truncate_guard
BEFORE TRUNCATE ON model_deployment_lifecycle_receipt_qualifications
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_deployment_lifecycle_receipt_truncate();

COMMENT ON TABLE model_deployment_lifecycle_receipts IS
    'Durable actor-scoped idempotency and decision receipts for canonical v2 release lifecycle writes. Exact replay returns frozen result facts, never inferred current state.';
COMMENT ON TABLE model_deployment_lifecycle_receipt_qualifications IS
    'Transaction-time M75 qualification snapshot for every sealed route item used by a lifecycle decision; non-qualified operations are explicit not_required rows.';

COMMIT;
