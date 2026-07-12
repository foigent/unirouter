BEGIN;

-- Once any new writer has produced an outbox/journal/destination receipt, an
-- old binary can no longer preserve replay and incremental-budget semantics.
-- Refuse destructive rollback and require roll-forward.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM internal_outbox_events)
       OR EXISTS (SELECT 1 FROM usage_finalization_journal)
       OR EXISTS (SELECT 1 FROM project_budget_usage)
       OR EXISTS (SELECT 1 FROM runtime_terminal_hooks)
       OR EXISTS (SELECT 1 FROM call_logs WHERE outbox_event_key IS NOT NULL)
       OR EXISTS (SELECT 1 FROM request_trace_events WHERE outbox_event_key IS NOT NULL)
       OR EXISTS (SELECT 1 FROM audit_logs WHERE outbox_event_key IS NOT NULL)
       OR EXISTS (SELECT 1 FROM notification_deliveries WHERE outbox_event_key IS NOT NULL)
       OR EXISTS (SELECT 1 FROM freeze_idempotency WHERE project_id <> 0)
    THEN
        RAISE EXCEPTION 'P6-F20 down refused: activated internal outbox or journal state exists; retain the schema and roll forward';
    END IF;
END;
$$;

DROP INDEX IF EXISTS idx_runtime_terminal_hooks_pending;
DROP TABLE IF EXISTS runtime_terminal_hooks;

DROP INDEX IF EXISTS idx_project_budget_usage_project_updated;
DROP TABLE IF EXISTS project_budget_usage;

DROP INDEX IF EXISTS idx_usage_finalization_request;
DROP INDEX IF EXISTS idx_usage_finalization_org_created;
DROP TABLE IF EXISTS usage_finalization_journal;

ALTER TABLE freeze_idempotency
    DROP CONSTRAINT IF EXISTS freeze_idempotency_project_nonnegative,
    DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS idx_notification_deliveries_outbox_event_key;
ALTER TABLE notification_deliveries DROP COLUMN IF EXISTS outbox_event_key;

DROP INDEX IF EXISTS idx_audit_logs_outbox_event_key;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS outbox_event_key;

DROP INDEX IF EXISTS idx_request_trace_events_outbox_event_key;
ALTER TABLE request_trace_events DROP COLUMN IF EXISTS outbox_event_key;

DROP INDEX IF EXISTS idx_call_logs_outbox_event_key;
ALTER TABLE call_logs DROP COLUMN IF EXISTS outbox_event_key;

DROP INDEX IF EXISTS idx_internal_outbox_dead_letter;
DROP INDEX IF EXISTS idx_internal_outbox_lease;
DROP INDEX IF EXISTS idx_internal_outbox_claim;
DROP TABLE IF EXISTS internal_outbox_events;

DELETE FROM schema_migrations
WHERE version = '000027_prd6_durable_internal_outbox';

COMMIT;
