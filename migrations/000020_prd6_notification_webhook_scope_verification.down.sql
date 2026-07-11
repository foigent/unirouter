BEGIN;

DROP TRIGGER IF EXISTS notification_webhooks_safe_writer ON notification_webhooks;
DROP FUNCTION IF EXISTS enforce_notification_webhook_safe_writer();

-- A rollback to a binary without tenant-safe Webhook writes cannot safely
-- preserve live delivery. Disable every route before removing the new
-- metadata; rows and their configuration remain retained for roll-forward.
UPDATE notification_webhooks
SET enabled = FALSE;

-- Keep quarantined historical retries unscheduled on rollback. Do not restore
-- a pre-fix destination automatically.
ALTER TABLE notification_deliveries
    DROP COLUMN IF EXISTS scope_quarantine_previous_next_retry_at;

ALTER TABLE notification_deliveries
    DROP COLUMN IF EXISTS retry_scope_quarantined;

ALTER TABLE notification_webhooks
    DROP CONSTRAINT IF EXISTS notification_webhooks_verified_before_enabled;

ALTER TABLE notification_webhooks
    DROP COLUMN IF EXISTS scope_quarantine_previous_enabled;

ALTER TABLE notification_webhooks
    DROP COLUMN IF EXISTS scope_write_generation;

-- Intentionally do not re-enable unverified historical routes when rolling
-- back the metadata column; that would recreate the cross-tenant delivery
-- exposure. Rows remain present and disabled for operator review.
ALTER TABLE notification_webhooks
    DROP COLUMN IF EXISTS scope_verified;

ALTER TABLE notification_webhooks
    ADD CONSTRAINT notification_webhooks_rollback_lockdown
    CHECK (enabled = FALSE);

-- The old API can still create disabled rows or mutate destinations while the
-- delivery lockdown is active. Make the entire table read-only until the safe
-- migration is rolled forward; retained rows remain available for inspection.
CREATE OR REPLACE FUNCTION reject_notification_webhook_rollback_writes()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'notification webhook writes are disabled during P6-F33 rollback lockdown';
END;
$$;

CREATE TRIGGER notification_webhooks_rollback_readonly
BEFORE INSERT OR UPDATE OR DELETE ON notification_webhooks
FOR EACH ROW
EXECUTE FUNCTION reject_notification_webhook_rollback_writes();

-- The current runner only applies .up.sql files and trusts this ledger. Keep
-- the manual down and its version record atomic so a later roll-forward can
-- rebuild the P6-F33 columns, trigger and constraints.
DELETE FROM schema_migrations
WHERE version = '000020_prd6_notification_webhook_scope_verification';

COMMIT;
