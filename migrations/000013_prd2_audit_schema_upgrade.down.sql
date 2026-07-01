-- Rollback migration 000013: PRD2 audit schema upgrade

BEGIN;

DROP INDEX IF EXISTS idx_audit_logs_source_created;
DROP INDEX IF EXISTS idx_audit_logs_resource_created;
DROP INDEX IF EXISTS idx_audit_logs_action_created;

UPDATE audit_logs
SET source = 'admin'
WHERE source = 'admin_console';

ALTER TABLE audit_logs DROP COLUMN IF EXISTS after_state;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS before_state;

COMMIT;
