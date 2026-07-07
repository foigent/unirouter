BEGIN;

DROP INDEX IF EXISTS idx_request_trace_events_project_created;
DROP INDEX IF EXISTS idx_request_trace_events_org_created;
DROP INDEX IF EXISTS idx_request_trace_events_request_created;

DROP TABLE IF EXISTS request_trace_events;

COMMIT;
