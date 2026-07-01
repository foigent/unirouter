# Audit Schema Upgrade

> Version: v0.1
> Scope: PRD2 batch 6 backend foundation.

## Goals

Audit Schema Upgrade makes admin and developer operations auditable by actor, org, project, source, request metadata, and structured before/after state.

## Source Values

Canonical `audit_logs.source` values:

| Value | Meaning |
|-------|---------|
| `admin_console` | Admin Console or privileged org/platform operation |
| `developer_console` | Project-scoped Developer Console operation |
| `gateway_api` | Gateway/API-key-originated system action |
| `system` | Background or unauthenticated system action |

Legacy `admin` is normalized to `admin_console`.

## Audit Fields

`audit_logs` stores:

- `org_id`
- `project_id`
- `user_id` as actor id
- `action`
- `resource`
- `resource_id`
- `detail`
- `before_state`
- `after_state`
- `ip`
- `user_agent`
- `request_id`
- `source`
- `created_at`

API responses expose `actor_id` as an alias of `user_id` for PRD2 readability while preserving the existing `user_id` field.

## Filtering Contract

`GET /api/v1/audit-logs` supports:

- `org_id`
- `project_id`
- `actor_id`
- `user_id`
- `action`
- `resource`
- `resource_id`
- `source`
- `start_time`
- `end_time`

`source=admin` remains accepted as a legacy filter alias and is normalized to `admin_console`.

## Structured Before/After

The audit helper extracts `before` and `after` keys from `detail` and writes them to `before_state` and `after_state` while preserving the original `detail` JSON.

Batch 6 explicitly covers:

- project API key creation after-state with project id.
- model registry lifecycle update before/after state.
- wallet balance adjustment before/after balance.

## Current Limits

- Not every historical audit call has been migrated to explicit before/after yet.
- Gateway-originated audit events are reserved by source contract but not emitted in batch 6.
- Frontend audit filters are not implemented in batch 6.
- Browser/API E2E with real JWTs remains a later acceptance step.
