BEGIN;

-- The one-click Connector installation is a product control-plane identity.
-- It binds the UniRouter project and exact supported event scope only after
-- local consent. X-Insight Cursor/Checkpoint/worker state is intentionally not
-- stored here.
CREATE TABLE xinsight_connector_installations (
    installation_id VARCHAR(128) PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL,
    x_deployment_id VARCHAR(128) NOT NULL,
    invitation_sha256 VARCHAR(71) NOT NULL,
    event_scope VARCHAR(80) NOT NULL,
    source_instance VARCHAR(128) NOT NULL,
    source_binding_sha256 VARCHAR(71) NOT NULL,
    desired_state VARCHAR(16) NOT NULL DEFAULT 'active',
    observed_state VARCHAR(24) NOT NULL DEFAULT 'provisioning',
    state_revision BIGINT NOT NULL DEFAULT 1,
    control_fence BIGINT NOT NULL DEFAULT 1,
    bootstrap_receipt_sha256 VARCHAR(71),
    last_successful_sync_at TIMESTAMPTZ,
    last_error_code VARCHAR(64),
    last_error_at TIMESTAMPTZ,
    consented_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    consented_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT xinsight_connector_installation_project_scope
        FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
    CONSTRAINT xinsight_connector_installation_id_shape
        CHECK (installation_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
    CONSTRAINT xinsight_connector_deployment_id_shape
        CHECK (x_deployment_id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
    CONSTRAINT xinsight_connector_invitation_hash
        CHECK (invitation_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT xinsight_connector_event_scope
        CHECK (event_scope = 'model.request.completed'),
    CONSTRAINT xinsight_connector_source_instance
        CHECK (octet_length(source_instance) BETWEEN 1 AND 128
               AND source_instance !~ '[[:cntrl:]]'),
    CONSTRAINT xinsight_connector_source_binding_hash
        CHECK (source_binding_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT xinsight_connector_desired_state
        CHECK (desired_state IN ('active', 'paused', 'disconnected')),
    CONSTRAINT xinsight_connector_observed_state
        CHECK (observed_state IN ('provisioning', 'connected', 'paused', 'abnormal', 'disconnected')),
    CONSTRAINT xinsight_connector_state_revision
        CHECK (state_revision >= 1),
    CONSTRAINT xinsight_connector_control_fence
        CHECK (control_fence >= 1),
    CONSTRAINT xinsight_connector_bootstrap_receipt
        CHECK (bootstrap_receipt_sha256 IS NULL OR bootstrap_receipt_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT xinsight_connector_error_code
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    CONSTRAINT xinsight_connector_error_time
        CHECK ((last_error_code IS NULL) = (last_error_at IS NULL)),
    CONSTRAINT xinsight_connector_installation_tenant_key
        UNIQUE (installation_id, org_id, project_id),
    CONSTRAINT xinsight_connector_invitation_single_use
        UNIQUE (invitation_sha256)
);

-- The current feed credential contract permits one live receiver per project.
-- This is a V1 product constraint rather than a permanent singleton identity:
-- every row and operation remains keyed by installation_id.
CREATE UNIQUE INDEX xinsight_connector_one_live_project_scope
    ON xinsight_connector_installations (org_id, project_id, event_scope)
    WHERE observed_state <> 'disconnected';

CREATE UNIQUE INDEX xinsight_connector_one_live_x_deployment
    ON xinsight_connector_installations (x_deployment_id)
    WHERE observed_state <> 'disconnected';

CREATE TABLE xinsight_connector_operations (
    operation_ref VARCHAR(37) PRIMARY KEY,
    installation_id VARCHAR(128) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    action VARCHAR(16) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    desired_state VARCHAR(16) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'queued',
    requested_revision BIGINT NOT NULL,
    control_fence BIGINT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner VARCHAR(128),
    lease_token VARCHAR(64),
    lease_expires_at TIMESTAMPTZ,
    result_code VARCHAR(64),
    result_sha256 VARCHAR(71),
    error_code VARCHAR(64),
    completed_at TIMESTAMPTZ,
    requested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT xinsight_connector_operation_installation_scope
        FOREIGN KEY (installation_id, org_id, project_id)
        REFERENCES xinsight_connector_installations (installation_id, org_id, project_id)
        ON DELETE CASCADE,
    CONSTRAINT xinsight_connector_operation_ref_shape
        CHECK (operation_ref ~ '^xcop_[a-f0-9]{32}$'),
    CONSTRAINT xinsight_connector_operation_action
        CHECK (action IN ('connect', 'pause', 'resume', 'rotate', 'replay')),
    CONSTRAINT xinsight_connector_operation_idempotency
        CHECK (idempotency_key ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
    CONSTRAINT xinsight_connector_operation_desired_state
        CHECK (desired_state IN ('active', 'paused')),
    CONSTRAINT xinsight_connector_operation_state
        CHECK (state IN ('queued', 'claimed', 'retry_wait', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT xinsight_connector_operation_revision
        CHECK (requested_revision >= 1),
    CONSTRAINT xinsight_connector_operation_fence
        CHECK (control_fence >= 1),
    CONSTRAINT xinsight_connector_operation_attempts
        CHECK (attempt_count >= 0),
    CONSTRAINT xinsight_connector_operation_lease
        CHECK (
            (state = 'claimed' AND lease_owner IS NOT NULL AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
            OR
            (state <> 'claimed' AND lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
        ),
    CONSTRAINT xinsight_connector_operation_result_code
        CHECK (result_code IS NULL OR result_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    CONSTRAINT xinsight_connector_operation_result_hash
        CHECK (result_sha256 IS NULL OR result_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT xinsight_connector_operation_error_code
        CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    CONSTRAINT xinsight_connector_operation_terminal_time
        CHECK ((state IN ('succeeded', 'failed', 'cancelled')) = (completed_at IS NOT NULL)),
    CONSTRAINT xinsight_connector_operation_idempotent
        UNIQUE (installation_id, idempotency_key)
);

CREATE INDEX xinsight_connector_operation_claim
    ON xinsight_connector_operations (state, next_attempt_at, created_at, operation_ref);

CREATE INDEX xinsight_connector_operation_installation_history
    ON xinsight_connector_operations (installation_id, created_at DESC, operation_ref DESC);

COMMIT;
