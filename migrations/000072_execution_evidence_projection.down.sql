BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE call_logs, request_trace_events, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000072_execution_evidence_projection'
    ) THEN
        RAISE EXCEPTION 'Execution evidence projection rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM call_logs WHERE route_evidence_source IS NOT NULL)
       OR EXISTS (SELECT 1 FROM request_trace_events WHERE route_evidence_source IS NOT NULL) THEN
        RAISE EXCEPTION 'Execution evidence projection rollback refused: versioned projection evidence exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS request_trace_events_execution_projection_guard ON request_trace_events;
DROP TRIGGER IF EXISTS call_logs_execution_projection_guard ON call_logs;
DROP FUNCTION IF EXISTS enforce_execution_projection_attempt_match();

DROP INDEX IF EXISTS request_trace_events_execution_projection_idx;
DROP INDEX IF EXISTS call_logs_execution_projection_idx;

ALTER TABLE request_trace_events
    DROP CONSTRAINT request_trace_events_route_runtime_release_fk,
    DROP CONSTRAINT request_trace_events_route_credential_connection_fk,
    DROP CONSTRAINT request_trace_events_route_deployment_connection_fk,
    DROP CONSTRAINT request_trace_events_route_deployment_org_fk,
    DROP CONSTRAINT request_trace_events_route_public_model_org_fk,
    DROP CONSTRAINT request_trace_events_execution_projection_check,
    DROP COLUMN route_channel_id,
    DROP COLUMN route_snapshot_hash,
    DROP COLUMN route_runtime_release_id,
    DROP COLUMN route_model_operation_id,
    DROP COLUMN route_credential_binding_id,
    DROP COLUMN route_connection_id,
    DROP COLUMN route_deployment_id,
    DROP COLUMN route_public_model_id,
    DROP COLUMN route_evidence_hash,
    DROP COLUMN route_authority_mode,
    DROP COLUMN route_evidence_version,
    DROP COLUMN route_evidence_source;

ALTER TABLE call_logs
    DROP CONSTRAINT call_logs_route_runtime_release_fk,
    DROP CONSTRAINT call_logs_route_credential_connection_fk,
    DROP CONSTRAINT call_logs_route_deployment_connection_fk,
    DROP CONSTRAINT call_logs_route_public_model_org_fk,
    DROP CONSTRAINT call_logs_execution_projection_check,
    DROP COLUMN route_snapshot_hash,
    DROP COLUMN route_runtime_release_id,
    DROP COLUMN route_model_operation_id,
    DROP COLUMN route_credential_binding_id,
    DROP COLUMN route_connection_id,
    DROP COLUMN route_public_model_id,
    DROP COLUMN route_evidence_hash,
    DROP COLUMN route_authority_mode,
    DROP COLUMN route_evidence_version,
    DROP COLUMN route_evidence_source;

DELETE FROM schema_migrations
WHERE version='000072_execution_evidence_projection';

COMMIT;
