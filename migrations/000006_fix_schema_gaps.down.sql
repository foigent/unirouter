-- Rollback migration 000004

BEGIN;

-- Revert grants
DROP INDEX IF EXISTS idx_grants_client_request_id;
ALTER TABLE grants ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE grants ALTER COLUMN created_by DROP DEFAULT;
ALTER TABLE grants DROP COLUMN IF EXISTS client_request_id;

-- Revert freeze_idempotency
DROP INDEX IF EXISTS idx_freeze_idempotency_org_id;
ALTER TABLE freeze_idempotency DROP COLUMN IF EXISTS frozen_amount;
ALTER TABLE freeze_idempotency DROP COLUMN IF EXISTS org_id;

COMMIT;
