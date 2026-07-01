# Notification And Event Foundation

> Version: v0.1
> Scope: PRD2 batch 7 backend foundation.

## Goals

The notification/event foundation persists enterprise events, routes matching events to webhooks, and records delivery state so admins can inspect failures and retry timing.

Batch 7 focuses on the backend foundation for P2-F08:

- persistent notification events.
- webhook route configuration.
- delivery attempt state.
- project budget threshold event generation.

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

An empty `event_types` array means all event types for that org/project route.

When an event is created, the service delivers to enabled webhooks where:

- route `org_id` matches event `org_id`.
- route `project_id` is empty or matches event `project_id`.
- route `event_types` is empty or contains event `event_type`.

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

Batch 7 records retry timing for failed delivery but does not run a background retry worker.

## Project Budget Threshold

`EventService.CheckProjectBudget` reads an active project budget and sums `call_logs.cost_cents` from the current budget period.

Supported periods:

- `daily`
- `monthly`

When spending reaches `budget_cents`, the service creates a deduplicated `project_budget.threshold` event with payload fields:

- `org_id`
- `project_id`
- `project_name`
- `budget_cents`
- `spent_cents`
- `budget_period`
- `period_start`
- `threshold_percent`
- `usage_percent`

Gateway relay invokes this check after settlement and after the request is recorded in `call_logs`, so the current request is included in the aggregate.

## Admin APIs

Batch 7 adds:

- `GET /api/v1/notification-events`
- `GET /api/v1/notification-deliveries`
- `GET /api/v1/notification-webhooks`
- `POST /api/v1/notification-webhooks`
- `PUT /api/v1/notification-webhooks/{id}`

Project-scoped roles must provide a readable project id for event/delivery visibility. Webhook writes require project write permission when scoped to a project.

## Current Limits

- No frontend notification center in batch 7.
- No email, IM, or background retry worker in batch 7.
- No production webhook E2E in batch 7.
- Event emitters for key disabled/revoked, model deprecated/retired, provider/channel failure, license warning, and high estimated-usage ratio are reserved but not fully wired in batch 7.
