-- Rollback migration 000014: PRD2 notification/event foundation

BEGIN;

DROP INDEX IF EXISTS idx_notification_deliveries_status;
DROP INDEX IF EXISTS idx_notification_deliveries_event;
DROP TABLE IF EXISTS notification_deliveries;

DROP INDEX IF EXISTS idx_notification_webhooks_project_enabled;
DROP INDEX IF EXISTS idx_notification_webhooks_org_enabled;
DROP TABLE IF EXISTS notification_webhooks;

DROP INDEX IF EXISTS idx_notification_events_delivery_state;
DROP INDEX IF EXISTS idx_notification_events_type_created;
DROP INDEX IF EXISTS idx_notification_events_project_created;
DROP INDEX IF EXISTS idx_notification_events_org_created;
DROP INDEX IF EXISTS idx_notification_events_org_dedupe;
DROP TABLE IF EXISTS notification_events;

COMMIT;
