-- Migration 000007: Align schema with PRD v1.5 requirements
-- Adds missing columns identified in gap analysis

BEGIN;

-- 1. call_logs: add requested_model (client's model before failover) and ip_address
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS requested_model VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS ip_address VARCHAR(45) NOT NULL DEFAULT '';

-- Index for requested_model queries (failover analysis)
CREATE INDEX IF NOT EXISTS idx_call_logs_requested_model ON call_logs(requested_model) WHERE requested_model != '';

-- 2. audit_logs: add source field to distinguish admin vs client operations
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'admin';
CREATE INDEX IF NOT EXISTS idx_audit_logs_source ON audit_logs(source);

-- 3. wallets: add freeze_expire_at for abandoned freeze cleanup
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS freeze_expire_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_wallets_freeze_expire ON wallets(freeze_expire_at) WHERE freeze_expire_at IS NOT NULL AND frozen > 0;

COMMIT;
