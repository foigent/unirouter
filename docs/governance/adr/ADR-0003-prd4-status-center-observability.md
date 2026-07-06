# ADR 0003: PRD4 Status Center And Runtime Observability

> Status: Proposed
> Date: 2026-07-07
> Owners: User, Codex
> Related: `docs/PRD-Phase4.md`, `docs/planning/PRD4-BACKLOG.md`,
> `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`

## Context

Current Internal Status is event-derived from notification events. The User wants
CPU, memory, network, DB, Redis, Prometheus, Grafana, and similar runtime state to
be visible, but also noted that forcing operators to open multiple monitoring
windows increases management complexity.

Many 2B customers already have their own monitoring platforms. UniRouter should
summarize and link to those platforms without replacing them.

## Decision

PRD4 defines a broader Status Center with separate surfaces:

| Surface | Meaning |
|---------|---------|
| Event Status | Existing notification-event lifecycle and incident-like status |
| Runtime Environment | Deployment mode, version, commit, instance role, freshness |
| Capacity Health | CPU, memory, disk, network, DB, Redis, throughput, latency, error pressure |
| Observability Integrations | Prometheus/Grafana/external monitoring connection state and deep links |
| Maintenance Windows | Future planned-operations model unless implemented in a later batch |

The Status Center may display CPU, memory, disk, network, DB, Redis, Prometheus,
and Grafana summaries, but it must not mix those metrics into the existing event
status semantics.

Raw topology, private IPs, VPC/subnet details, security groups, cloud account
identifiers, and full hostnames are restricted to platform/ops roles and should be
redacted by default.

## Consequences

Benefits:

- Operators get a single UniRouter entry point for runtime health.
- Customers with existing Grafana or Prometheus can use summaries and deep links
  instead of duplicate dashboards.
- Event lifecycle remains clear and does not become a noisy metrics dump.

Tradeoffs:

- UniRouter needs freshness, missing-data, and external-monitoring failure states.
- Some deployments may have only partial telemetry.
- Role-based redaction adds testing overhead.

## Alternatives Considered

- Put CPU/memory/network directly into current Internal Status: rejected because
  it mixes infrastructure metrics with notification-event lifecycle semantics.
- Embed full Grafana dashboards in UniRouter: rejected because it duplicates an
  existing operator tool and complicates auth/session handling.
- Require customers to use only UniRouter observability: rejected because it is
  unfriendly to 2B environments with existing monitoring platforms.

## Verification

- Verify connected, stale, unavailable, and misconfigured external-monitoring
  states.
- Verify platform/ops role visibility and lower-role redaction.
- Verify missing telemetry does not break the main app.
- Verify Event Status remains event-derived and distinct from runtime/capacity
  health.

## Rollback Or Exit Criteria

If a deployment does not provide runtime telemetry, the Status Center should
degrade to event status and explicit unknown/stale runtime cards rather than
blocking the application.
