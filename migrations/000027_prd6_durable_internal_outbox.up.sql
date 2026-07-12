BEGIN;

-- P6-F20 keeps the financial state machine authoritative while moving
-- detailed evidence and webhook delivery behind a bounded, replayable,
-- PostgreSQL-backed internal outbox. This table is internal only: it is not an
-- exporter, public adapter, or cross-project delivery contract.
CREATE TABLE IF NOT EXISTS internal_outbox_events (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(200) NOT NULL UNIQUE,
    event_type VARCHAR(80) NOT NULL,
    priority SMALLINT NOT NULL,
    org_id BIGINT NOT NULL DEFAULT 0,
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0,
    claimed_by VARCHAR(160) NOT NULL DEFAULT '',
    claimed_until TIMESTAMPTZ,
    last_error VARCHAR(512) NOT NULL DEFAULT '',
    dead_lettered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT internal_outbox_priority_bounds CHECK (priority BETWEEN 0 AND 1000),
    CONSTRAINT internal_outbox_org_nonnegative CHECK (org_id >= 0),
    CONSTRAINT internal_outbox_attempts_nonnegative CHECK (attempts >= 0),
    CONSTRAINT internal_outbox_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT internal_outbox_payload_size CHECK (octet_length(payload::text) <= 262144)
);

CREATE INDEX IF NOT EXISTS idx_internal_outbox_claim
    ON internal_outbox_events (priority DESC, available_at, occurred_at, id)
    WHERE dead_lettered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_internal_outbox_lease
    ON internal_outbox_events (claimed_until)
    WHERE claimed_until IS NOT NULL AND dead_lettered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_internal_outbox_dead_letter
    ON internal_outbox_events (dead_lettered_at, event_type)
    WHERE dead_lettered_at IS NOT NULL;

-- Successful outbox rows are deleted after idempotent materialization. The
-- destination key makes a handler retry safe if a process dies after the
-- destination insert but before the outbox delete.
ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS outbox_event_key VARCHAR(200);
CREATE UNIQUE INDEX IF NOT EXISTS idx_call_logs_outbox_event_key
    ON call_logs (outbox_event_key);

ALTER TABLE request_trace_events
    ADD COLUMN IF NOT EXISTS outbox_event_key VARCHAR(200);
CREATE UNIQUE INDEX IF NOT EXISTS idx_request_trace_events_outbox_event_key
    ON request_trace_events (outbox_event_key);

ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS outbox_event_key VARCHAR(200);
CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_logs_outbox_event_key
    ON audit_logs (outbox_event_key);

ALTER TABLE notification_deliveries
    ADD COLUMN IF NOT EXISTS outbox_event_key VARCHAR(200);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_deliveries_outbox_event_key
    ON notification_deliveries (outbox_event_key);

-- Preserve eligible failed/pending deliveries created by the synchronous
-- worker before activation. Quarantined or disabled destinations remain
-- fail-closed and are not silently replayed.
UPDATE notification_deliveries d
SET outbox_event_key = 'notification-delivery:migrated:' || d.id::text
FROM notification_events e, notification_webhooks w
WHERE d.event_id = e.id
  AND d.webhook_id = w.id
  AND w.org_id = e.org_id
  AND d.status IN ('pending', 'failed')
  AND d.outbox_event_key IS NULL
  AND d.retry_scope_quarantined = FALSE
  AND w.enabled = TRUE
  AND w.scope_verified = TRUE;

INSERT INTO internal_outbox_events (
    event_key, event_type, priority, org_id, payload,
    occurred_at, available_at, created_at, updated_at
)
SELECT d.outbox_event_key,
       'notification.delivery.v1',
       250,
       e.org_id,
       jsonb_build_object('delivery_id', d.id),
       d.created_at,
       NOW(),
       NOW(),
       NOW()
FROM notification_deliveries d
JOIN notification_events e ON e.id = d.event_id
WHERE d.outbox_event_key LIKE 'notification-delivery:migrated:%'
ON CONFLICT (event_key) DO NOTHING;

-- Project identity is captured at reservation time so a terminal financial
-- event can update the incremental budget ledger even if the gateway process
-- dies before it can enqueue the detailed call log.
ALTER TABLE freeze_idempotency
    ADD COLUMN IF NOT EXISTS project_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_project_nonnegative CHECK (project_id >= 0);

-- This is the retained minimal financial journal. It contains usage and cost
-- facts only; request payloads, provider credentials, and model responses are
-- never copied into the outbox or journal.
CREATE TABLE IF NOT EXISTS usage_finalization_journal (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(200) NOT NULL UNIQUE,
    event_type VARCHAR(40) NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL DEFAULT 0,
    model VARCHAR(100) NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    uncached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    usage_source VARCHAR(32) NOT NULL DEFAULT '',
    cache_usage_schema VARCHAR(80) NOT NULL DEFAULT '',
    cache_usage_status VARCHAR(40) NOT NULL DEFAULT 'not_reported',
    calculated_cost INTEGER NOT NULL DEFAULT 0,
    charged_cost INTEGER NOT NULL DEFAULT 0,
    pricing_version VARCHAR(80) NOT NULL DEFAULT '',
    terminal_transaction_id BIGINT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_finalization_event_type CHECK (
        event_type IN ('usage_prepared', 'settled', 'cancelled')
    ),
    CONSTRAINT usage_finalization_identity CHECK (org_id > 0 AND project_id >= 0),
    CONSTRAINT usage_finalization_tokens_nonnegative CHECK (
        prompt_tokens >= 0
        AND uncached_prompt_tokens >= 0
        AND cache_read_tokens >= 0
        AND cache_creation_tokens >= 0
        AND completion_tokens >= 0
    ),
    CONSTRAINT usage_finalization_prompt_partition CHECK (
        uncached_prompt_tokens + cache_read_tokens + cache_creation_tokens = prompt_tokens
    ),
    CONSTRAINT usage_finalization_costs_nonnegative CHECK (
        calculated_cost >= 0 AND charged_cost >= 0
    )
);

CREATE INDEX IF NOT EXISTS idx_usage_finalization_org_created
    ON usage_finalization_journal (org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_finalization_request
    ON usage_finalization_journal (org_id, request_id, occurred_at);

-- One incremental row replaces the previous per-request SUM(call_logs) scan.
-- Updates are driven only after an idempotent settled-journal insert succeeds.
CREATE TABLE IF NOT EXISTS project_budget_usage (
    org_id BIGINT NOT NULL,
    project_id BIGINT NOT NULL,
    budget_period VARCHAR(20) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    spent_cents BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, project_id, budget_period, period_start),
    CONSTRAINT project_budget_usage_identity CHECK (org_id > 0 AND project_id > 0),
    CONSTRAINT project_budget_usage_period CHECK (budget_period IN ('daily', 'monthly')),
    CONSTRAINT project_budget_usage_spend_nonnegative CHECK (spent_cents >= 0)
);

CREATE INDEX IF NOT EXISTS idx_project_budget_usage_project_updated
    ON project_budget_usage (org_id, project_id, updated_at DESC);

-- Establish the one-time pre-outbox baseline while old writers are drained.
-- Runtime workers never SUM call_logs: doing so could count another request's
-- already-materialized log before that request's unique settlement journal and
-- then count the settlement again.
INSERT INTO project_budget_usage (
    org_id, project_id, budget_period, period_start, spent_cents, updated_at
)
SELECT cl.org_id,
       cl.project_id,
       p.budget_period,
       CASE p.budget_period
           WHEN 'daily' THEN date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
           WHEN 'monthly' THEN date_trunc('month', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
       END,
       COALESCE(SUM(cl.cost_cents), 0),
       NOW()
FROM call_logs cl
JOIN projects p
  ON p.id = cl.project_id
 AND p.org_id = cl.org_id
 AND p.status = 'active'
 AND p.budget_cents > 0
 AND p.budget_period IN ('daily', 'monthly')
WHERE cl.project_id IS NOT NULL
  AND cl.created_at >= CASE p.budget_period
      WHEN 'daily' THEN date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
      WHEN 'monthly' THEN date_trunc('month', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
  END
GROUP BY cl.org_id, cl.project_id, p.budget_period
ON CONFLICT (org_id, project_id, budget_period, period_start)
DO NOTHING;

-- Runtime terminal hooks bridge the durable financial terminal state to the
-- Redis-backed Agent ancestor, distributed-policy, and workload reservations.
-- The reservation is a server-owned handle, never a bearer credential.
CREATE TABLE IF NOT EXISTS runtime_terminal_hooks (
    operation_id VARCHAR(64) NOT NULL,
    org_id BIGINT NOT NULL,
    hook_type VARCHAR(40) NOT NULL,
    reservation JSONB NOT NULL,
    reservation_hash CHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    terminal_kind VARCHAR(20) NOT NULL DEFAULT '',
    actual_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost_cents BIGINT NOT NULL DEFAULT 0,
    terminal_event_key VARCHAR(200) NOT NULL DEFAULT '',
    completed_at TIMESTAMPTZ,
    terminal_outcome VARCHAR(40) NOT NULL DEFAULT '',
    last_error VARCHAR(512) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (operation_id, hook_type),
    CONSTRAINT runtime_terminal_hook_org_positive CHECK (org_id > 0),
    CONSTRAINT runtime_terminal_hook_type CHECK (hook_type IN ('agent_ancestor', 'limit_policy')),
    CONSTRAINT runtime_terminal_hook_reservation_object CHECK (jsonb_typeof(reservation) = 'object'),
    CONSTRAINT runtime_terminal_hook_reservation_size CHECK (octet_length(reservation::text) <= 65536),
    CONSTRAINT runtime_terminal_hook_terminal_kind CHECK (terminal_kind IN ('', 'settled', 'cancelled')),
    CONSTRAINT runtime_terminal_hook_actual_nonnegative CHECK (actual_tokens >= 0 AND actual_cost_cents >= 0)
);

CREATE INDEX IF NOT EXISTS idx_runtime_terminal_hooks_pending
    ON runtime_terminal_hooks (expires_at, created_at)
    WHERE completed_at IS NULL;

COMMIT;
