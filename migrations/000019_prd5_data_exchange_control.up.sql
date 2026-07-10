BEGIN;

CREATE TABLE IF NOT EXISTS integration_exchange_items (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    approval_mode VARCHAR(32) NOT NULL CHECK (approval_mode IN ('exact_request', 'controlled_scenario')),
    request_id VARCHAR(128),
    scenario_label VARCHAR(120) NOT NULL,
    source_instance VARCHAR(160) NOT NULL,
    data_classes TEXT[] NOT NULL DEFAULT ARRAY['metadata_only']::TEXT[],
    safety_profile VARCHAR(64) NOT NULL DEFAULT 'metadata_only',
    approval_status VARCHAR(24) NOT NULL DEFAULT 'pending' CHECK (approval_status IN ('pending', 'approved', 'unavailable')),
    reason VARCHAR(500) NOT NULL DEFAULT '',
    authorized_operator VARCHAR(160),
    retention_until TIMESTAMPTZ,
    approved_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (request_id IS NOT NULL OR approval_mode = 'controlled_scenario'),
    CHECK (cardinality(data_classes) BETWEEN 1 AND 16)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_integration_exchange_items_org_request
    ON integration_exchange_items(org_id, request_id)
    WHERE request_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_integration_exchange_items_org_status
    ON integration_exchange_items(org_id, approval_status, created_at DESC);

CREATE TABLE IF NOT EXISTS integration_exchanges (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    exchange_id VARCHAR(160) NOT NULL UNIQUE,
    downstream_system VARCHAR(64) NOT NULL DEFAULT 'x-insight',
    source_instance VARCHAR(160) NOT NULL,
    schema_version VARCHAR(80) NOT NULL,
    approval_manifest_sha256 VARCHAR(71) NOT NULL,
    artifact_sha256 VARCHAR(71) NOT NULL,
    artifact_ref VARCHAR(255) NOT NULL,
    workflow_status VARCHAR(24) NOT NULL DEFAULT 'generated' CHECK (workflow_status IN ('generated', 'handed_off', 'cancelled', 'artifact_deleted')),
    request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    input_event_count INTEGER NOT NULL DEFAULT 0 CHECK (input_event_count >= 0),
    contains_raw_prompt BOOLEAN NOT NULL DEFAULT FALSE CHECK (contains_raw_prompt = FALSE),
    contains_raw_response BOOLEAN NOT NULL DEFAULT FALSE CHECK (contains_raw_response = FALSE),
    contains_credentials BOOLEAN NOT NULL DEFAULT FALSE CHECK (contains_credentials = FALSE),
    contains_raw_ip BOOLEAN NOT NULL DEFAULT FALSE CHECK (contains_raw_ip = FALSE),
    contains_raw_upstream_error BOOLEAN NOT NULL DEFAULT FALSE CHECK (contains_raw_upstream_error = FALSE),
    retention_until TIMESTAMPTZ,
    generated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    generated_at TIMESTAMPTZ NOT NULL,
    artifact_deleted_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    artifact_deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_exchanges_org_created
    ON integration_exchanges(org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS integration_exchange_members (
    exchange_record_id BIGINT NOT NULL REFERENCES integration_exchanges(id) ON DELETE CASCADE,
    item_id BIGINT NOT NULL REFERENCES integration_exchange_items(id) ON DELETE RESTRICT,
    PRIMARY KEY (exchange_record_id, item_id)
);

CREATE TABLE IF NOT EXISTS integration_exchange_deliveries (
    id BIGSERIAL PRIMARY KEY,
    exchange_record_id BIGINT NOT NULL REFERENCES integration_exchanges(id) ON DELETE CASCADE,
    action VARCHAR(24) NOT NULL CHECK (action IN ('handed_off', 'redelivered')),
    handoff_ref VARCHAR(255) NOT NULL,
    note VARCHAR(500) NOT NULL DEFAULT '',
    delivered_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_exchange_deliveries_exchange
    ON integration_exchange_deliveries(exchange_record_id, delivered_at DESC);

CREATE TABLE IF NOT EXISTS integration_exchange_receipts (
    id BIGSERIAL PRIMARY KEY,
    exchange_record_id BIGINT NOT NULL REFERENCES integration_exchanges(id) ON DELETE CASCADE,
    receipt_schema_version VARCHAR(80) NOT NULL,
    source_system VARCHAR(64) NOT NULL,
    source_instance VARCHAR(160) NOT NULL,
    artifact_schema_version VARCHAR(80) NOT NULL,
    artifact_sha256 VARCHAR(71) NOT NULL,
    approval_manifest_sha256 VARCHAR(71),
    result_status VARCHAR(24) NOT NULL CHECK (result_status IN ('validated', 'persisted', 'rejected')),
    request_ids INTEGER NOT NULL DEFAULT 0 CHECK (request_ids >= 0),
    input_events INTEGER NOT NULL DEFAULT 0 CHECK (input_events >= 0),
    accepted INTEGER NOT NULL DEFAULT 0 CHECK (accepted >= 0),
    duplicate_noop INTEGER NOT NULL DEFAULT 0 CHECK (duplicate_noop >= 0),
    rerun_duplicate_noop INTEGER NOT NULL DEFAULT 0 CHECK (rerun_duplicate_noop >= 0),
    rejected INTEGER NOT NULL DEFAULT 0 CHECK (rejected >= 0),
    quarantined INTEGER NOT NULL DEFAULT 0 CHECK (quarantined >= 0),
    capture_gap_events INTEGER NOT NULL DEFAULT 0 CHECK (capture_gap_events >= 0),
    persisted_inserted INTEGER NOT NULL DEFAULT 0 CHECK (persisted_inserted >= 0),
    persisted_duplicate_noop INTEGER NOT NULL DEFAULT 0 CHECK (persisted_duplicate_noop >= 0),
    persistence_conflicts INTEGER NOT NULL DEFAULT 0 CHECK (persistence_conflicts >= 0),
    readback_events INTEGER NOT NULL DEFAULT 0 CHECK (readback_events >= 0),
    readback_capture_gaps INTEGER NOT NULL DEFAULT 0 CHECK (readback_capture_gaps >= 0),
    warning_codes JSONB NOT NULL DEFAULT '[]'::JSONB,
    import_job_id VARCHAR(160),
    deep_link VARCHAR(500),
    checked_at TIMESTAMPTZ NOT NULL,
    received_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (exchange_record_id, artifact_sha256, checked_at)
);

CREATE INDEX IF NOT EXISTS idx_integration_exchange_receipts_exchange
    ON integration_exchange_receipts(exchange_record_id, received_at DESC);

COMMIT;
