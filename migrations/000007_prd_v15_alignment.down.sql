-- Rollback migration 000007

BEGIN;

DROP INDEX IF EXISTS idx_wallets_freeze_expire;
ALTER TABLE wallets DROP COLUMN IF EXISTS freeze_expire_at;

DROP INDEX IF EXISTS idx_audit_logs_source;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS source;

DROP INDEX IF EXISTS idx_call_logs_requested_model;
ALTER TABLE call_logs DROP COLUMN IF EXISTS ip_address;
ALTER TABLE call_logs DROP COLUMN IF EXISTS requested_model;

COMMIT;
