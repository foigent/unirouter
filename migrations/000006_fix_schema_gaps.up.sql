-- Migration 000006: Fix schema-code mismatches (consolidated)
-- Fixes 3 issues:
--   1. freeze_idempotency missing org_id and frozen_amount (billing.go needs them)
--   2. grants missing client_request_id (handlers.go CreateGrant needs it)
--   3. grants.created_by is NOT NULL but admin handler has no user context

BEGIN;

-- BUG-1: freeze_idempotency missing org_id and frozen_amount
ALTER TABLE freeze_idempotency ADD COLUMN IF NOT EXISTS org_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE freeze_idempotency ADD COLUMN IF NOT EXISTS frozen_amount INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_freeze_idempotency_org_id ON freeze_idempotency(org_id);

-- BUG-2: grants missing client_request_id for idempotency
ALTER TABLE grants ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(64);
CREATE UNIQUE INDEX IF NOT EXISTS idx_grants_client_request_id ON grants(client_request_id) WHERE client_request_id IS NOT NULL;

-- BUG-3: grants.created_by is NOT NULL but code never provides it
ALTER TABLE grants ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE grants ALTER COLUMN created_by SET DEFAULT 0;

COMMIT;
