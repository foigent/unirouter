BEGIN;

DROP INDEX IF EXISTS idx_call_logs_upstream_request;
DROP INDEX IF EXISTS idx_call_logs_trace_created;
DROP INDEX IF EXISTS idx_request_trace_events_span;
DROP INDEX IF EXISTS idx_request_trace_events_trace_created;

ALTER TABLE call_logs
    DROP COLUMN IF EXISTS payload_ref,
    DROP COLUMN IF EXISTS payload_capture_mode,
    DROP COLUMN IF EXISTS on_behalf_of,
    DROP COLUMN IF EXISTS actor_ref,
    DROP COLUMN IF EXISTS actor_type,
    DROP COLUMN IF EXISTS source_event_hash,
    DROP COLUMN IF EXISTS evidence_ref,
    DROP COLUMN IF EXISTS capture_mode,
    DROP COLUMN IF EXISTS schema_version,
    DROP COLUMN IF EXISTS upstream_request_id,
    DROP COLUMN IF EXISTS session_id,
    DROP COLUMN IF EXISTS trace_id;

ALTER TABLE request_trace_events
    DROP COLUMN IF EXISTS source_event_hash,
    DROP COLUMN IF EXISTS payload_ref,
    DROP COLUMN IF EXISTS payload_capture_mode,
    DROP COLUMN IF EXISTS session_id,
    DROP COLUMN IF EXISTS on_behalf_of,
    DROP COLUMN IF EXISTS actor_ref,
    DROP COLUMN IF EXISTS actor_type,
    DROP COLUMN IF EXISTS decision_reason,
    DROP COLUMN IF EXISTS decision_result,
    DROP COLUMN IF EXISTS decision_kind,
    DROP COLUMN IF EXISTS upstream_request_id,
    DROP COLUMN IF EXISTS evidence_ref,
    DROP COLUMN IF EXISTS capture_gap,
    DROP COLUMN IF EXISTS capture_mode,
    DROP COLUMN IF EXISTS source_component,
    DROP COLUMN IF EXISTS schema_version,
    DROP COLUMN IF EXISTS parent_span_id,
    DROP COLUMN IF EXISTS span_id,
    DROP COLUMN IF EXISTS trace_id;

COMMIT;
