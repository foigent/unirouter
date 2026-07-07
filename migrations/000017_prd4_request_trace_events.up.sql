BEGIN;

CREATE TABLE IF NOT EXISTS request_trace_events (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(64) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT,
    api_key_id BIGINT,
    owner_user_id BIGINT,
    key_type VARCHAR(20) NOT NULL DEFAULT 'unknown',
    event_type VARCHAR(80) NOT NULL,
    stage VARCHAR(40) NOT NULL,
    severity VARCHAR(20) NOT NULL DEFAULT 'info',
    model VARCHAR(100) NOT NULL DEFAULT '',
    requested_model VARCHAR(100) NOT NULL DEFAULT '',
    provider VARCHAR(50) NOT NULL DEFAULT '',
    channel VARCHAR(255) NOT NULL DEFAULT '',
    status_code INTEGER,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT request_trace_events_key_type_check CHECK (key_type IN ('unknown', 'shared', 'personal', 'service')),
    CONSTRAINT request_trace_events_severity_check CHECK (severity IN ('info', 'warning', 'error'))
);

CREATE INDEX IF NOT EXISTS idx_request_trace_events_request_created
    ON request_trace_events(request_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_request_trace_events_org_created
    ON request_trace_events(org_id, created_at);

CREATE INDEX IF NOT EXISTS idx_request_trace_events_project_created
    ON request_trace_events(project_id, created_at)
    WHERE project_id IS NOT NULL;

COMMIT;
