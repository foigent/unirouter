# Notification And Event Foundation

> Version: v1.0
> Status: Current implementation contract
> Source of truth: `internal/admin/notification_handlers.go`, `internal/billing/notifications*.go`, and notification routes in `cmd/uniroute/main.go`

## Goals

The notification/event foundation persists enterprise events, routes matching events to webhooks, and records delivery state so admins can inspect failures and retry timing.

Batch 7 focused on the backend foundation for P2-F08:

- persistent notification events.
- webhook route configuration.
- delivery attempt state.
- project budget threshold event generation.

Batch 19 adds:

- frontend Notification Center at `/notifications`.
- webhook route create/edit UI.
- background retry worker for failed deliveries.
- delivery-state aggregation after retry.
- emitters for key disabled, model deprecated/retired, provider/channel failure, license warning, and high estimated-usage ratio.
- local real-backend project budget browser/API smoke with a local webhook receiver.

## Event Model

`notification_events` stores:

- `org_id`
- optional `project_id`
- `event_type`
- `severity`: `info`, `warning`, or `critical`
- `title`
- `message`
- `resource`
- `resource_id`
- structured `payload`
- `status`: `open`, `acknowledged`, or `resolved`
- `dedupe_key`
- `delivery_state`: `not_configured`, `pending`, `delivered`, or `failed`
- `last_delivery_at`
- timestamps

`dedupe_key` prevents repeated threshold events for the same project and period.

## Webhook Routing

`notification_webhooks` stores one route per org or project:

- `org_id`
- optional `project_id`
- `name`
- `url`
- `event_types` JSON array
- `enabled`
- `scope_verified` plus the write generation used to quarantine edited or historical routes

An empty `event_types` array means all event types for that org/project route.

When an event is created, the service delivers to enabled webhooks where:

- route `org_id` matches event `org_id`.
- route `project_id` is empty or matches event `project_id`.
- route `event_types` is empty or contains event `event_type`.

Stored targets are not returned verbatim by the list API. The operator surface
uses a masked target and sanitized delivery errors so credentials embedded in a
legacy URL or upstream error cannot be exposed in the UI or audit detail.

## Delivery State

`notification_deliveries` records one delivery attempt per event/webhook:

- `event_id`
- `webhook_id`
- `status`: `pending`, `delivered`, or `failed`
- `attempts`
- `response_status`
- `error`
- `next_retry_at`
- `delivered_at`
- timestamps

Failed deliveries record `next_retry_at`. Batch 19 runs a background retry worker in the server process. The worker calls `EventService.RetryDueDeliveries` immediately on startup and then once per minute. A retried event is marked `delivered` only when all delivery rows for that event are delivered; otherwise it remains `failed`.

## Project Budget Threshold

`EventService.CheckProjectBudget` reads an active project budget in cents, converts it to micros, and sums `call_logs.cost_micros` from the current budget period.

Supported periods:

- `daily`
- `monthly`

When spending reaches `budget_cents`, the service creates a deduplicated `project_budget.threshold` event with payload fields:

- `org_id`
- `project_id`
- `project_name`
- `budget_cents`
- `budget_micros`
- `spent_micros`
- `currency` (`CNY`)
- `amount_scale` (`1000000` micros per CNY)
- `budget_period`
- `period_start`
- `threshold_percent`
- `usage_percent`

Gateway relay invokes this check after settlement and after the request is recorded in `call_logs`, so the current request is included in the aggregate.

## Admin APIs

The admin API exposes:

- `GET /api/v1/notification-events`
- `POST /api/v1/notification-events/{id}/acknowledge`
- `POST /api/v1/notification-events/{id}/resolve`
- `GET /api/v1/notification-deliveries`
- `GET /api/v1/notification-webhooks`
- `POST /api/v1/notification-webhooks`
- `PUT /api/v1/notification-webhooks/{id}`
- `POST /api/v1/notification-webhooks/{id}/verify-scope`

Project-scoped roles must provide a readable project id for event/delivery visibility. Webhook writes require project write permission when scoped to a project.
Every route above requires an enforceable commercial License. Notifications are
a core management module and do not require a separate signed feature item.

Role boundaries:

- `platform_admin` and `org_admin` can read and transition events/deliveries and operate Webhook routes in their authorized scope.
- `project_admin` must select a project where it has writable administrator membership.
- `billing_admin` can read, acknowledge, and resolve only `project_budget.threshold` and `token_metering.estimated_ratio.high` in its JWT organization. The allowlist is applied in SQL to events and joined delivery rows, including lifecycle lookups.
- `billing_admin` has no Webhook authority. All four Webhook handlers return 403 before querying or mutating persistence, in addition to route-level role exclusion.

Event lifecycle (`open -> acknowledged -> resolved`, or `open -> resolved`) is
independent from delivery state. Acknowledging does not retry/cancel delivery;
resolved events cannot be reopened through these APIs. Event transitions and
their audit rows commit atomically.
Rows created before migration `000020` are retained but excluded from new and
retry delivery until an authorized actor reviews the stored organization,
project and URL and explicitly verifies the scope. New Webhooks are verified at
creation after the same ownership checks. Verification is audited and does not
delete or silently rewrite an existing route. Every edit re-quarantines the
route; confirmation is bound to the displayed version and commits atomically
with its audit record. Pre-migration failed deliveries are retained but never
automatically replayed after route confirmation.

Migration `000020` also rejects Webhook writes from older binaries and makes its
down path a delivery lockdown. Older read handlers still lack tenant-aware SQL,
so deployment must drain/block the old notification control plane before the
new notification UI is exposed; this is not an ordinary mixed-version rolling
control-plane migration.

## Frontend

The Notification Center at `/notifications` is role-shaped:

- Events tab: event type, severity, delivery state, resource, message, and time.
- Deliveries tab: webhook name, status, attempts, response status, next retry, error, and update time.
- Webhook tab for platform/org/project operators: masked route target, scoped project, event types, enabled state, scope
  verification state, create/edit actions, and explicit re-verification for
  quarantined historical routes.
- Billing administrators receive a finance-only center with financial events
  and read-only delivery records. They are not shown the Webhook tab, create/edit
  controls, or a request path that would invoke Webhook APIs.

The topbar notification button routes to `/notifications`.

## Event Emitters

Currently wired emitters:

- `project_budget.threshold`
- `token_metering.estimated_ratio.high`
- `api_key.disabled`
- `model.deprecated`
- `model.retired`
- `provider.channel.failure`
- `license.warning`

## Current Limits

- No email or IM channel; webhook delivery is the implemented channel.
- Production webhook delivery evidence is not yet claimed. Batch 19 adds local production-like webhook receiver smoke only.
- API key revoke has no separate runtime path beyond disabling in current code; `api_key.disabled` is the wired key lifecycle event.
