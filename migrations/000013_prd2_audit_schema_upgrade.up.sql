-- Migration 000013: PRD2 audit schema upgrade
-- Adds structured before/after fields and canonical source values for audit evidence.

BEGIN;

ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS before_state JSONB NOT NULL DEFAULT '{}';

ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS after_state JSONB NOT NULL DEFAULT '{}';

UPDATE audit_logs
SET source = 'admin_console'
WHERE source = 'admin';

CREATE INDEX IF NOT EXISTS idx_audit_logs_action_created ON audit_logs(action, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_created ON audit_logs(resource, resource_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_source_created ON audit_logs(source, created_at);

COMMIT;
