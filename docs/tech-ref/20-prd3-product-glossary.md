# PRD3 Product Glossary

> Version: v0.1
> Scope: PRD3 consistency cleanup for existing PRD1/PRD2 functionality. This document defines terms only; it does not introduce new runtime capability.

## Purpose

PRD1 used simple admin-console terms. PRD2 added projects, developer console, provider adapters, notification events, usage analytics, and internal status. This glossary is the canonical wording for current UI, docs, and tests until a later PRD changes the product model.

## Traffic And Provider Terms

| Term | Canonical Meaning | Current Code Mapping |
|------|-------------------|----------------------|
| Provider / Vendor / Supplier | Provider family, upstream vendor, or adapter-family dimension such as OpenAI, Anthropic, DeepSeek, or Mimo. It is used for comparison and reporting, not for credential identity. | `channels.provider`, `call_logs.provider`, provider metrics labels |
| Upstream Channel | One concrete configured route instance. It includes endpoint/base URL, adapter type, model mapping/list, priority/weight, circuit/cooldown state, and associated upstream credentials. | `channels`, `channel_keys`, `call_logs.channel` |
| Access Credential | UniRouter-issued credential used by a client or project to call UniRouter. | `api_keys` |
| Upstream Credential | Provider key or provider-side credential used by UniRouter when calling an upstream provider through an upstream channel. | `channel_keys.key_enc` |
| Model | The resolved model used for routing, metering, governance, and logs. | `call_logs.model`, model registry rows |
| Model Alias | A UniRouter-facing name that maps to one or more allowed/resolved models. | `call_logs.requested_model`, model alias/model-set configuration |

## Usage Analytics Terms

| Term | Canonical Meaning | Current Behavior |
|------|-------------------|------------------|
| Overall / All | The current filtered query as one aggregate. If no filter is set, it means all data visible to the current role and scope. | `summary` in `GET /api/v1/usage/analytics`; PRD3 also allows `group_by=overall` for one-row breakdown/CSV |
| Breakdown / Grouping | A single selected dimension for splitting the current filtered query, such as project, access credential, model, provider, upstream channel, request status, or usage source. | `group_by` whitelist in `internal/admin/prd2_product_handlers.go` |
| Usage Source | The accounting source used for token totals. Stable API values must remain compatible; UI may localize labels. | `usage_source` values such as `upstream`, `counter_v2`, `adapter_estimated`, `estimated`, `no_route`, `upstream_failed` |
| Upstream Tokens | Tokens returned by an upstream provider or legacy equivalent values normalized as upstream-returned usage. | `usage_source in ('upstream', 'api', '')` |
| Estimated Tokens | Tokens counted or estimated by UniRouter when upstream usage is unavailable or partial. | `usage_source in ('estimated', 'adapter_estimated', 'counter_v2')` |

PRD3 must not rewrite settlement, reconciliation, wallet, or `usage_source` accounting semantics while improving labels.

## Notification And Status Terms

| Term | Canonical Meaning | Current Code Mapping |
|------|-------------------|----------------------|
| Notification Event | Persistent enterprise event that can be inspected and optionally delivered by webhook. | `notification_events` |
| Incident | Product-facing name for a notification event that affects service, provider, budget, model, key, or license state. It is not a separate incident-management object in PRD3. | Rows returned by `GET /api/v1/internal-status` |
| Event Status | Lifecycle of the notification event: `open`, `acknowledged`, or `resolved`. | `notification_events.status` |
| Severity | Impact level: `info`, `warning`, or `critical`. | `notification_events.severity` |
| Delivery State | Webhook delivery state, separate from event status: `not_configured`, `pending`, `delivered`, or `failed`. | `notification_events.delivery_state`, `notification_deliveries.status` |
| Internal Status | Event-derived service status page. It is `degraded` when open critical events exist, `warning` when open warning events exist, otherwise `operational`. | `GET /api/v1/internal-status` |
| Maintenance Window | Reserved product term for planned maintenance scheduling. PRD3 does not implement scheduling, SLA/SLO, or upgrade communication workflow. | Current API returns `maintenance_windows: []` only |
| Runtime Environment / Infrastructure Snapshot | PRD4 candidate term for deployment and instance information such as environment, deployment mode, active instances, version, commit, and telemetry freshness. It is not part of the PRD3 Internal Status API. | Not implemented in PRD3 |
| Capacity Health | PRD4 candidate term for resource pressure and saturation such as CPU, memory, disk, DB connections, Redis health, throughput, latency, and network error rate. It must not be confused with notification event severity. | Not implemented in PRD3 |

## Audit Terms

| Term | Canonical Meaning | Current Code Mapping |
|------|-------------------|----------------------|
| Audit Source | Source surface that produced an audit row, such as admin console, developer console, gateway API, or system task. It is separate from provider/channel and separate from notification delivery state. | `audit_logs.source`; frontend labels in `auditSourceLabel` |

## Role Terms

| Term | Canonical Meaning |
|------|-------------------|
| Platform Administrator | Full platform-management role. Legacy `admin` remains compatibility input and should normalize to `platform_admin`. |
| Organization Administrator | Organization-level admin over supported org/admin routes. It should not imply platform-only or billing-only control. |
| Billing Administrator | Finance-governance role for wallet, reconciliation, grants, usage, notifications, audit, status, and docs where backend authorization allows it. |
| Project Administrator | Project-scoped role with project workbench, status, and notification visibility where backend authorization allows it. |
| Developer / Project Member | Project-scoped developer role. Avoid the ambiguous label "normal user" in B2B UI. |
| Viewer / Project Read-only Member | Project-scoped read-only member role. |
| Organization Viewer | Organization read-only role. If it has no active workbench, it should land on access state instead of API Docs. |

## Boundaries

- Runtime plugins, marketplace installation, dynamic plugin lifecycle, new persistent authority surfaces, and third-party plugin management remain PRD4/later.
- Maintenance scheduling, SLA/SLO, upgrade communication, and full incident operations remain PRD4/later.
- Infrastructure observability, runtime environment visibility, host inventory, cluster topology, and capacity-health telemetry remain PRD4/later.
- Provider real-upstream production evidence, customer IdP evidence, and production license evidence are verification tracks, not glossary changes.
