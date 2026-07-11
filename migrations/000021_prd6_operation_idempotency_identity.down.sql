BEGIN;

DROP INDEX IF EXISTS idx_request_trace_events_attempt;
DROP INDEX IF EXISTS idx_request_trace_events_agent;
DROP INDEX IF EXISTS idx_request_trace_events_workflow;
DROP INDEX IF EXISTS idx_call_logs_workflow;
DROP INDEX IF EXISTS idx_call_logs_idempotency;
DROP INDEX IF EXISTS idx_call_logs_client_request;
DROP INDEX IF EXISTS idx_audit_logs_client_request;
DROP INDEX IF EXISTS idx_transactions_org_type_client_request;

ALTER TABLE request_trace_events
    DROP COLUMN IF EXISTS attempt_id,
    DROP COLUMN IF EXISTS agent_id,
    DROP COLUMN IF EXISTS workflow_id,
    DROP COLUMN IF EXISTS idempotency_key_hash,
    DROP COLUMN IF EXISTS client_request_id;

ALTER TABLE call_logs
    DROP COLUMN IF EXISTS attempt_id,
    DROP COLUMN IF EXISTS agent_id,
    DROP COLUMN IF EXISTS workflow_id,
    DROP COLUMN IF EXISTS idempotency_key_hash,
    DROP COLUMN IF EXISTS client_request_id;

DROP TABLE IF EXISTS request_idempotency;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS client_request_id;

ALTER TABLE audit_logs
    DROP COLUMN IF EXISTS client_request_id;

-- Keep idx_grants_org_client_request on rollback. Recreating the historical
-- global index could fail after legitimate cross-tenant values and would
-- restore the collision defect this migration removes.
-- Keep the widened VARCHAR(128) client_request_id column as well; shrinking it
-- could truncate legitimate correlation values written after the forward
-- migration.
-- Keep freeze_idempotency_org_request_pkey for the same reason: an old global
-- request-id primary key cannot be restored after valid cross-tenant values
-- and must not reintroduce cross-tenant financial interference.

-- The repository migration runner only applies .up.sql files. Keep a manual
-- down and its ledger change atomic so a later roll-forward rebuilds the table
-- and structural identity columns.
DELETE FROM schema_migrations
WHERE version = '000021_prd6_operation_idempotency_identity';

COMMIT;
