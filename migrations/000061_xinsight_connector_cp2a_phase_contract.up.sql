BEGIN;

-- Remote management identities are exact bytes. Migration 60 constrained
-- them to lowercase; CP2A removes only that case-folding assumption while
-- retaining the existing bounded, printable storage contract.
ALTER TABLE xinsight_connector_installations
    DROP CONSTRAINT xinsight_connector_installation_id_shape,
    DROP CONSTRAINT xinsight_connector_deployment_id_shape;

ALTER TABLE xinsight_connector_installations
    ADD CONSTRAINT xinsight_connector_installation_id_shape
        CHECK (octet_length(installation_id) BETWEEN 1 AND 128
               AND installation_id = BTRIM(installation_id)
               AND installation_id !~ '[[:cntrl:]]'),
    ADD CONSTRAINT xinsight_connector_deployment_id_shape
        CHECK (octet_length(x_deployment_id) BETWEEN 1 AND 128
               AND x_deployment_id = BTRIM(x_deployment_id)
               AND x_deployment_id !~ '[[:cntrl:]]');

-- This is product-local secret storage, not a second copy of the X management
-- schema. Invitation and control authorities are mutually exclusive states;
-- the receiver bearer is X-owned and deliberately has no storage column.
CREATE TABLE xinsight_connector_control_bindings (
    installation_id VARCHAR(128) PRIMARY KEY,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    control_project_id VARCHAR(128),
    invitation_token_enc TEXT,
    control_token_enc TEXT,
    control_token_sha256 VARCHAR(71),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT xinsight_connector_control_binding_installation_scope
        FOREIGN KEY (installation_id, org_id, project_id)
        REFERENCES xinsight_connector_installations (installation_id, org_id, project_id)
        ON DELETE CASCADE,
    CONSTRAINT xinsight_connector_control_binding_secret_lifecycle
        CHECK (
            (invitation_token_enc IS NOT NULL
             AND octet_length(invitation_token_enc) BETWEEN 1 AND 4096
             AND control_project_id IS NULL
             AND control_token_enc IS NULL
             AND control_token_sha256 IS NULL)
            OR
            (invitation_token_enc IS NULL
             AND control_token_enc IS NOT NULL
             AND octet_length(control_token_enc) BETWEEN 1 AND 4096
             AND control_project_id IS NOT NULL
             AND octet_length(control_project_id) BETWEEN 1 AND 128
             AND control_project_id = BTRIM(control_project_id)
             AND control_project_id !~ '[[:cntrl:]]'
             AND control_token_sha256 IS NOT NULL
             AND control_token_sha256 ~ '^sha256:[a-f0-9]{64}$')
        )
);

-- Canonical X request/result bodies are persisted opaquely. The typed consumer
-- owns phase and predecessor validation; the database owns tenant binding,
-- exact idempotency identity, immutable request identity and terminal replay.
CREATE TABLE xinsight_connector_phase_operations (
    operation_id VARCHAR(128) PRIMARY KEY,
    installation_id VARCHAR(128) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    request_sha256 VARCHAR(71) NOT NULL,
    request_canonical BYTEA NOT NULL,
    control_fence BIGINT NOT NULL,
    claim_fence BIGINT NOT NULL,
    claim_token VARCHAR(64),
    claim_expires_at TIMESTAMPTZ,
    result_sha256 VARCHAR(71),
    result_canonical BYTEA,
    acknowledged_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT xinsight_connector_phase_installation_scope
        FOREIGN KEY (installation_id, org_id, project_id)
        REFERENCES xinsight_connector_installations (installation_id, org_id, project_id)
        ON DELETE CASCADE,
    CONSTRAINT xinsight_connector_phase_operation_id
        CHECK (octet_length(operation_id) BETWEEN 1 AND 128
               AND operation_id = BTRIM(operation_id)
               AND operation_id !~ '[[:cntrl:]]'),
    CONSTRAINT xinsight_connector_phase_request
        CHECK (request_sha256 ~ '^sha256:[a-f0-9]{64}$'
               AND octet_length(request_canonical) BETWEEN 1 AND 65536),
    CONSTRAINT xinsight_connector_phase_fence
        CHECK (control_fence >= 1 AND claim_fence >= 1),
    CONSTRAINT xinsight_connector_phase_terminal
        CHECK (
            (result_sha256 IS NULL
             AND result_canonical IS NULL
             AND acknowledged_at IS NULL
             AND claim_token IS NOT NULL
             AND octet_length(claim_token) BETWEEN 1 AND 64
             AND claim_expires_at IS NOT NULL)
            OR
            (result_sha256 IS NOT NULL
             AND result_sha256 ~ '^sha256:[a-f0-9]{64}$'
             AND result_canonical IS NOT NULL
             AND octet_length(result_canonical) BETWEEN 1 AND 65536
             AND claim_token IS NULL
             AND claim_expires_at IS NULL)
        ),
    CONSTRAINT xinsight_connector_phase_installation_operation
        UNIQUE (installation_id, operation_id)
);

COMMIT;
