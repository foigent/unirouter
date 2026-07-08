BEGIN;

ALTER TABLE request_trace_events
    ADD COLUMN IF NOT EXISTS trace_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS span_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS parent_span_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS schema_version VARCHAR(40) NOT NULL DEFAULT 'unirouter.evidence.v1',
    ADD COLUMN IF NOT EXISTS source_component VARCHAR(40) NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS capture_mode VARCHAR(40) NOT NULL DEFAULT 'native',
    ADD COLUMN IF NOT EXISTS capture_gap VARCHAR(80),
    ADD COLUMN IF NOT EXISTS evidence_ref VARCHAR(255),
    ADD COLUMN IF NOT EXISTS upstream_request_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS decision_kind VARCHAR(40),
    ADD COLUMN IF NOT EXISTS decision_result VARCHAR(40),
    ADD COLUMN IF NOT EXISTS decision_reason VARCHAR(120),
    ADD COLUMN IF NOT EXISTS actor_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS actor_ref VARCHAR(128),
    ADD COLUMN IF NOT EXISTS on_behalf_of VARCHAR(128),
    ADD COLUMN IF NOT EXISTS session_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS payload_capture_mode VARCHAR(40) NOT NULL DEFAULT 'metadata_only',
    ADD COLUMN IF NOT EXISTS payload_ref VARCHAR(255),
    ADD COLUMN IF NOT EXISTS source_event_hash VARCHAR(64);

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS trace_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS session_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS upstream_request_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS schema_version VARCHAR(40) NOT NULL DEFAULT 'unirouter.evidence.v1',
    ADD COLUMN IF NOT EXISTS capture_mode VARCHAR(40) NOT NULL DEFAULT 'native',
    ADD COLUMN IF NOT EXISTS evidence_ref VARCHAR(255),
    ADD COLUMN IF NOT EXISTS source_event_hash VARCHAR(64),
    ADD COLUMN IF NOT EXISTS actor_type VARCHAR(40),
    ADD COLUMN IF NOT EXISTS actor_ref VARCHAR(128),
    ADD COLUMN IF NOT EXISTS on_behalf_of VARCHAR(128),
    ADD COLUMN IF NOT EXISTS payload_capture_mode VARCHAR(40) NOT NULL DEFAULT 'metadata_only',
    ADD COLUMN IF NOT EXISTS payload_ref VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_request_trace_events_trace_created
    ON request_trace_events(trace_id, created_at, id)
    WHERE trace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_request_trace_events_span
    ON request_trace_events(span_id)
    WHERE span_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_call_logs_trace_created
    ON call_logs(trace_id, created_at, id)
    WHERE trace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_call_logs_upstream_request
    ON call_logs(upstream_request_id)
    WHERE upstream_request_id IS NOT NULL;

COMMIT;
