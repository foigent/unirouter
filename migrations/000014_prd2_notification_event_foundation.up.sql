-- Migration 000014: PRD2 notification/event foundation
-- Adds persistent notification events, webhook routes, and delivery state.

BEGIN;

CREATE TABLE IF NOT EXISTS notification_events (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL,
    event_type VARCHAR(80) NOT NULL,
    severity VARCHAR(20) NOT NULL DEFAULT 'info'
        CHECK (severity IN ('info', 'warning', 'critical')),
    title VARCHAR(200) NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    resource VARCHAR(80) NOT NULL DEFAULT '',
    resource_id VARCHAR(80) NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'acknowledged', 'resolved')),
    dedupe_key VARCHAR(200) NOT NULL DEFAULT '',
    delivery_state VARCHAR(20) NOT NULL DEFAULT 'not_configured'
        CHECK (delivery_state IN ('not_configured', 'pending', 'delivered', 'failed')),
    last_delivery_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_events_org_dedupe
    ON notification_events(org_id, dedupe_key)
    WHERE dedupe_key <> '';
CREATE INDEX IF NOT EXISTS idx_notification_events_org_created
    ON notification_events(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_events_project_created
    ON notification_events(project_id, created_at DESC)
    WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_notification_events_type_created
    ON notification_events(event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_events_delivery_state
    ON notification_events(delivery_state, created_at DESC);

CREATE TABLE IF NOT EXISTS notification_webhooks (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    url TEXT NOT NULL,
    event_types JSONB NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_webhooks_org_enabled
    ON notification_webhooks(org_id, enabled);
CREATE INDEX IF NOT EXISTS idx_notification_webhooks_project_enabled
    ON notification_webhooks(project_id, enabled)
    WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES notification_events(id) ON DELETE CASCADE,
    webhook_id BIGINT REFERENCES notification_webhooks(id) ON DELETE SET NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    response_status INTEGER,
    error TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_event
    ON notification_deliveries(event_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_status
    ON notification_deliveries(status, updated_at DESC);

COMMIT;
