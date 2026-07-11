BEGIN;

-- A security rollback makes the legacy table read-only. Remove that guard only
-- while this migration atomically reinstalls the tenant-safe writer contract.
DROP TRIGGER IF EXISTS notification_webhooks_rollback_readonly ON notification_webhooks;
DROP FUNCTION IF EXISTS reject_notification_webhook_rollback_writes();

-- A security rollback leaves this fail-closed constraint behind so an older
-- vulnerable binary cannot silently re-enable delivery. Remove it only while
-- reinstalling the verification controls in this migration.
ALTER TABLE notification_webhooks
    DROP CONSTRAINT IF EXISTS notification_webhooks_rollback_lockdown;

-- Existing Webhook rows may have been written before tenant ownership was
-- enforced. Keep every row for review and rollback, but quarantine it from
-- delivery until an authorized tenant/platform administrator explicitly
-- confirms the stored organization, project and destination.
ALTER TABLE notification_webhooks
    ADD COLUMN IF NOT EXISTS scope_verified BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE notification_webhooks
    ADD COLUMN IF NOT EXISTS scope_quarantine_previous_enabled BOOLEAN;

ALTER TABLE notification_webhooks
    ADD COLUMN IF NOT EXISTS scope_write_generation BIGINT NOT NULL DEFAULT 0
    CHECK (scope_write_generation >= 0);

ALTER TABLE notification_deliveries
    ADD COLUMN IF NOT EXISTS retry_scope_quarantined BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE notification_deliveries
    ADD COLUMN IF NOT EXISTS scope_quarantine_previous_next_retry_at TIMESTAMPTZ;

-- Disable first so an older binary sharing the database also respects the
-- quarantine during a rolling/canary upgrade. Preserve the operator's prior
-- enabled intent for the explicit verification action.
UPDATE notification_webhooks
SET scope_quarantine_previous_enabled = enabled,
    enabled = FALSE
WHERE scope_verified = FALSE
  AND scope_quarantine_previous_enabled IS NULL;

-- A later route re-verification must not silently replay payloads queued under
-- the old, untrusted destination. Nulling the due time protects mixed-version
-- workers; the original schedule is retained as evidence only.
UPDATE notification_deliveries
SET retry_scope_quarantined = TRUE,
    scope_quarantine_previous_next_retry_at = next_retry_at,
    next_retry_at = NULL
WHERE webhook_id IS NOT NULL
  AND status = 'failed'
  AND next_retry_at IS NOT NULL
  AND retry_scope_quarantined = FALSE;

ALTER TABLE notification_webhooks
    ADD CONSTRAINT notification_webhooks_verified_before_enabled
    CHECK (scope_verified OR enabled = FALSE);

CREATE OR REPLACE FUNCTION enforce_notification_webhook_safe_writer()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    -- New code explicitly stamps creates and increments every update. Old
    -- binaries cannot do either, so their cross-tenant writes fail closed
    -- during a mixed-version window instead of polluting the review queue.
    IF TG_OP = 'INSERT' AND NEW.scope_write_generation < 1 THEN
        RAISE EXCEPTION 'notification webhook writer is not P6-F33 compatible';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.scope_write_generation <= OLD.scope_write_generation THEN
        RAISE EXCEPTION 'notification webhook update writer is not P6-F33 compatible';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER notification_webhooks_safe_writer
BEFORE INSERT OR UPDATE ON notification_webhooks
FOR EACH ROW
EXECUTE FUNCTION enforce_notification_webhook_safe_writer();

COMMENT ON COLUMN notification_webhooks.scope_verified IS
    'Explicit post-P6-F33 authorization of the stored organization, project and destination; false rows are retained but excluded from delivery and retry.';

COMMENT ON COLUMN notification_webhooks.scope_quarantine_previous_enabled IS
    'Enabled state captured while an unverified pre-P6-F33 route is quarantined; restored only by explicit authorized scope verification.';

COMMENT ON COLUMN notification_webhooks.scope_write_generation IS
    'Monotonic write marker required by the database trigger so pre-P6-F33 binaries cannot insert or mutate Webhook routes.';

COMMENT ON COLUMN notification_deliveries.retry_scope_quarantined IS
    'Historical failed delivery retained but excluded from automatic retry because its destination predates P6-F33 scope verification.';

COMMENT ON COLUMN notification_deliveries.scope_quarantine_previous_next_retry_at IS
    'Original retry schedule retained as evidence when a pre-P6-F33 failed delivery is quarantined.';

COMMIT;
