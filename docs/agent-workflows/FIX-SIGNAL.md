# FIX-SIGNAL.md - PRD4 Batch 1D Runtime Observability Foundation

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-RUNTIME-001`
**Matrix Row:** `P4-F06`

## Verification Request

Please verify PRD4 Batch 1D: Runtime Observability Foundation.

This batch keeps the existing Internal Status event semantics intact and adds a
separate runtime observability surface for platform, org, and billing operators.
It exposes app runtime metadata, process memory, DB/Redis health, recent traffic
summary, and configured Prometheus/Grafana entry points without fabricating host
CPU, disk, or network telemetry.

## Scope To Verify

Backend/config:

- Adds `GET /api/v1/runtime/status` behind platform/org/billing roles only.
- Wires Redis into admin runtime status for read-only health checks.
- Adds runtime metadata config: `RUNTIME_ENVIRONMENT`,
  `RUNTIME_DEPLOYMENT_MODE`, `RUNTIME_DEPLOYMENT_ROLE`, `RUNTIME_VERSION`,
  and `RUNTIME_COMMIT`.
- Adds external monitoring deep-link config: `PROMETHEUS_URL` and
  `GRAFANA_URL`, validated as optional HTTP(S) URLs.
- Returns structured `runtime_environment`, `capacity_health`, and
  `observability_integrations` sections.
- Returns partial diagnostic status instead of failing the whole endpoint when
  DB/Redis/traffic checks are unavailable.
- Marks CPU, disk, and network telemetry as `unavailable` with
  `telemetry_source_not_configured` until a telemetry source of truth exists.

Frontend/API contract:

- Adds `api.getRuntimeStatus()`.
- Keeps `/status` as the operator entry point while separating event status
  cards from runtime observability cards.
- Project-scoped roles can still view event-derived status, but do not request
  the restricted runtime endpoint.
- Updates backend `/docs`, frontend API Docs, OpenAPI, and route parity tests.

Docs/status:

- Records User-reported Batch 1C Canary/Sentinel completion after `4f62e34` /
  PR #86.
- Marks `P4-F06` / `PRD4-RUNTIME-001` fixed awaiting Canary.
- Keeps `P4-F07` external monitoring integration as planned because this batch
  adds configuration/deep-link state only, not Prometheus/Grafana active probes
  or embedded dashboards.

## Non-Scope

Do not expect this batch to implement:

- Host CPU utilization, disk usage, network throughput, node exporter, cloud
  API, or cluster inventory collection.
- Prometheus/Grafana active health probes, datasource validation, screenshots,
  or embedded dashboards.
- OpenTelemetry ingestion/export or distributed span semantics.
- Full PlungeIn runtime plugin framework.
- Metric-alert-to-notification routing.
- Production-scale trace retention, archival, or partitioning.
- Scheduler V2 or capacity-aware dispatch.

## Required Verification Commands

```bash
rg -n "RuntimeStatus|runtime/status|RUNTIME_ENVIRONMENT|RUNTIME_DEPLOYMENT|PROMETHEUS_URL|GRAFANA_URL|telemetry_source_not_configured" internal cmd frontend/src docs/tech-ref/05-openapi-spec.yaml docs/ops/deployment-runbook.md .env.example docker-compose.yml
rg -n "PRD4-RUNTIME-001|PRD4-INFRA-001|P4-F06|Batch 1D|Runtime Observability|runtime/status" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md
go test ./internal/config ./internal/admin ./cmd/uniroute -count=1
go test ./...
npm --prefix frontend test -- --run InternalStatusPage client ApiDocsPage navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Acceptance Criteria

1. `GET /api/v1/runtime/status` is registered, documented, covered by route
   parity, and restricted to platform/org/billing operators.
2. Current `GET /api/v1/internal-status` remains event-derived and is not
   repurposed for infrastructure metrics.
3. Runtime response includes environment metadata, uptime, Go runtime summary,
   app memory, DB/Redis health, recent traffic summary, and Prometheus/Grafana
   configuration state.
4. Missing host telemetry is explicit as unavailable; CPU/disk/network values
   are not fabricated.
5. The frontend `/status` page renders runtime observability for authorized
   operators and avoids calling the restricted endpoint for project-scoped
   roles.
6. Optional Prometheus/Grafana URLs are validated and surfaced as configured or
   not configured.
7. Backend config/admin/cmd tests, frontend focused tests, full Go/frontend
   tests, frontend build, and `git diff --check` pass.
8. Docs consistently show Batch 1C as Canary/Sentinel complete per User report
   and Batch 1D as fixed awaiting Canary.
