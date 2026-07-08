# PRD4 Candidate Backlog

> Date: 2026-07-05
> Status: Candidate backlog and source record. Formal PRD4 scope now lives in
> `docs/PRD-Phase4.md`.
> Source: PRD3 consistency cleanup decisions and User-confirmed PRD4 boundaries.

## Purpose

PRD3 is limited to cleanup and optimization of already implemented behavior. This document records capabilities that were discussed during PRD3 but require new product models, new data collection, new authority surfaces, or new deployment assumptions. They must not be implemented inside PRD3 unless User explicitly reopens scope.

After PRD4 scope discovery on 2026-07-07, the formal PRD4 implementation draft
is `docs/PRD-Phase4.md`, and explicitly deferred items are tracked in
`docs/planning/PRD5-BACKLOG.md`. This file remains the candidate-source record.

## Candidate Items

| ID | Candidate | Why Not PRD3 | PRD4 Direction |
|----|-----------|--------------|----------------|
| PRD4-PLUGIN-001 | Runtime plugin platform and plugin marketplace | Current code has static provider adapters and configured notification webhooks, but no manifest, runtime loading, sandbox, permission model, lifecycle, signing, or install/uninstall path | Define plugin families, manifest schema, lifecycle, permission boundaries, compatibility, packaging, and audit/accounting invariants |
| PRD4-DEPLOY-001 | Enterprise deployment packaging and managed HA/IaC | Current assets cover Docker Compose, canary overlay, deployment runbook, and backup/restore evidence. No Helm, Terraform, Kubernetes, managed DB/Redis HA, or tested enterprise reference stack exists | Choose target deployment model, then design Helm/Terraform/Kubernetes or managed-HA reference deployment, upgrade/rollback, PITR, off-server backup, and failover drills |
| PRD4-INFRA-001 | Infrastructure observability and runtime environment visibility | Current Internal Status is event-derived from `notification_events`. Batch 1D adds a separate Runtime Observability foundation, Batch 1G adds external monitoring probes, and Batch 1J adds single-node host CPU/disk/network summaries. UniRouter still does not model multi-instance inventory, cluster topology, external collector ownership, or metric-alert routing as a full infrastructure product surface | Continue the separate Runtime Environment / Infrastructure Snapshot / Capacity Health model, visible to platform/ops roles, without mixing it into event lifecycle semantics |
| PRD4-BUDGET-001 | Project budget hard-block enforcement | PRD2 intentionally accepted alert-only budget behavior first. Blocking admission changes billing, wallet, idempotency, and user-facing error semantics | Define policy controls, admission order, grace behavior, audit rows, and regression tests for wallet/idempotency safety |
| PRD4-OPS-001 | Maintenance windows, SLA/SLO, upgrade communication, incident operations | PRD3 only labels current status as notification-event-derived and marks maintenance scheduling as not enabled | Define incident lifecycle beyond events, maintenance scheduling, SLA/SLO policy, operator workflow, customer-visible communication, and notification strategy |
| PRD4-CACHE-001 | Runtime cache or prompt/response plaintext storage | PRD2 explicitly keeps runtime cache research-only and prompt/response plaintext disabled by default | Require tenant isolation, billing correctness, audit-on-read, retention, purge, and opt-in controls before implementation |
| PRD4-SDK-001 | Full SDK suite | Current OpenAPI/API docs are still a PRD3 cleanup target; SDKs would freeze an unstable contract too early | Refresh API contract first, then generate or hand-build SDKs with versioning and compatibility policy |
| PRD4-ROLE-001 | New persistent roles such as audit viewer, support/operator, service account | Existing PRD3 role cleanup can only expose already-supported backend roles. New roles require new authority surfaces and route rules | Define role taxonomy, permission matrix, route guards, project/org scope, service-account credential model, and audit behavior |
| PRD4-EVIDENCE-001 | Production evidence promotion for currently available target evidence | PRD3 keeps `E2E accepted` and `Production verified` distinct; several items require target credentials or environments. User decision on 2026-07-08 moves real upstream and customer-IdP target execution out of current PRD4 because no approved real environments are available | Promote only evidence that already has an approved target environment. Real upstream and real SSO target E2E are tracked in `docs/planning/PRD5-BACKLOG.md` as `PRD5-UPSTREAM-001` and `PRD5-SSO-001` |

## Infrastructure Observability Boundary

User asked whether UniRouter infrastructure information such as server CPU, memory, storage, network state, and cluster state should be displayed in Internal Status. The accepted PRD3 decision is:

- Do not add this to PRD3.
- Do not mix infrastructure metrics into the current event-derived Internal Status model.
- Track it as PRD4 candidate `PRD4-INFRA-001`; Batch 1D implements the first
  Runtime Observability surface, Batch 1G adds external monitoring probes, and
  Batch 1J adds single-node host CPU/disk/network summaries. Cluster inventory,
  external collector ownership, stale-data policy, and metric-alert routing
  remain follow-up work.

Recommended future information architecture:

| Surface | Meaning | Example Fields |
|---------|---------|----------------|
| Event Status | Current PRD3 Internal Status over notification events | service status, open critical events, open warning events, event lifecycle, webhook delivery state |
| Runtime Environment | Deployment and instance snapshot | environment name, deployment mode, active instances, stable/canary role, version, commit, build time, data freshness |
| Capacity Health | Resource pressure and saturation | CPU usage, memory usage, disk usage, DB connections, Redis health, request throughput, p95/p99 latency, network error rate |
| Maintenance Windows | Planned operations model | schedule, scope, impact, status, owner, customer/operator communication |

Recommended access boundary:

- Default visibility should be limited to `platform_admin` and a future ops/support role.
- `org_admin`, `billing_admin`, `developer`, and project roles should not see raw host or network topology by default.
- Public/private IPs, VPC/subnet details, security groups, cloud account identifiers, full hostnames, and exact topology should be hidden or redacted unless explicitly required for an operator workflow.
- Batch 1D allows `org_admin` and `billing_admin` to view runtime summary
  because it does not expose raw hostnames, IPs, VPC/subnet details, or cluster
  topology. Future topology or per-host views should revisit this boundary.

Implementation prerequisites for PRD4:

- Decide source of truth: app runtime, Prometheus, node exporter, cloud API, deployment manifest, or operator-provided metadata.
- Define freshness and missing-data behavior.
- Define redaction policy and RBAC.
- Define whether alerts become notification events or remain metrics-only.
- Add tests for role visibility and stale/missing telemetry states.

## PRD3 Guardrail

PRD3 documents the boundary only. Any implementation that collects or displays infrastructure telemetry, host inventory, cluster topology, or capacity metrics is new functionality and requires PRD4 or a separately approved observability/security-hardening scope.
