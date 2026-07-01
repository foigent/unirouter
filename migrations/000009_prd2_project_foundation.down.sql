-- Rollback migration 000009: PRD2 project/workspace foundation

BEGIN;

DROP INDEX IF EXISTS idx_audit_logs_project_created;
DROP INDEX IF EXISTS idx_transactions_project_created;
DROP INDEX IF EXISTS idx_call_logs_project_created;
DROP INDEX IF EXISTS idx_api_keys_project_id;
DROP INDEX IF EXISTS idx_project_members_user_id;
DROP INDEX IF EXISTS idx_projects_status;
DROP INDEX IF EXISTS idx_projects_org_id;

ALTER TABLE audit_logs DROP COLUMN IF EXISTS project_id;
ALTER TABLE transactions DROP COLUMN IF EXISTS project_id;
ALTER TABLE call_logs DROP COLUMN IF EXISTS project_id;
ALTER TABLE api_keys DROP COLUMN IF EXISTS project_id;

DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS projects;

COMMIT;
