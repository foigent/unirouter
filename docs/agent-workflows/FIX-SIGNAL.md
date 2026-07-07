# FIX-SIGNAL.md - PRD4 Batch 1G External Monitoring Integration

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-MONITORING-001`
**Matrix Row:** `P4-F07`

## Verification Request

Please verify PRD4 Batch 1G: External Monitoring Integration.

This batch upgrades the PRD4 Runtime Status Center from static
Prometheus/Grafana URL display to active external monitoring health checks.
It keeps UniRouter as a status center and deep-link entrypoint, not as a
replacement for Prometheus or Grafana.

## Scope To Verify

Backend runtime status:

- `GET /api/v1/runtime/status` still returns runtime environment, app memory,
  DB/Redis health, and recent traffic summary.
- Configured `PROMETHEUS_URL` is actively checked with:
  - `/-/ready` readiness probe.
  - `/api/v1/query?query=up{job="unirouter"}` scrape-state probe.
- Prometheus integration reports `connected`, `stale`, `misconfigured`,
  `unavailable`, or `not_configured`, with `health_url`, `checked_at`,
  `latency_ms`, `reason`, and `scrape_status` where applicable.
- Configured `GRAFANA_URL` is actively checked with `/api/health`.
- Grafana integration reports `connected`, `stale`, `misconfigured`,
  `unavailable`, or `not_configured`, with `health_url`, `checked_at`,
  `latency_ms`, `reason`, and `version` where applicable.
- External monitoring failures must not make `/runtime/status` fail; they should
  be represented as integration status fields.

Frontend/runtime UI:

- `/status` Runtime Observability cards show Prometheus and Grafana states using
  the new status vocabulary.
- Configured URLs remain external links.
- Scrape status, latency, Grafana version, and reason text are visible when
  present.
- `StatusTag` color semantics cover connected/available/configured, stale,
  misconfigured, unavailable, and not_configured.

Docs/status:

- `P4-F05` / `PRD4-PLUNGEIN-001` is marked Canary passed after Batch 1F PASS and
  Sentinel merge `87bfc17` / PR #89.
- `P4-F07` / `PRD4-MONITORING-001` is marked fixed awaiting Canary.
- Runbook and env example describe `PROMETHEUS_URL` / `GRAFANA_URL` as Runtime
  Status health/deep-link base URLs.

## Non-Scope

Do not expect this batch to implement:

- Embedded Prometheus or Grafana dashboards inside UniRouter.
- Grafana datasource validation, dashboard UID validation, or panel screenshots.
- New metrics authentication strategy or trusted proxy policy.
- OpenTelemetry export or ingestion.
- Node exporter, cloud host telemetry collectors, cluster inventory, CPU
  utilization, disk usage, or network throughput collection.
- Metric-alert notification routing.
- Generic monitoring connector framework.
- Runtime plugin loading, manifest parsing, sandboxing, marketplace,
  third-party installation, or PlungeIn Mutate behavior.
- Real upstream provider evidence or real SSO evidence.

## Required Verification Commands

```bash
rg -n "runtimePrometheusIntegration|runtimeGrafanaIntegration|prometheusUniRouterTargetQuery|scrape_status|health_url|auth_required|integrationMeta|integrationTone" internal/admin frontend/src/pages frontend/src/ui
rg -n 'PRD4-MONITORING-001|P4-F07|Batch 1G|External Monitoring|PROMETHEUS_URL|GRAFANA_URL|up\{job="unirouter"\}|connected|stale|misconfigured|unavailable' docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md docs/ops/deployment-runbook.md .env.example
go test ./internal/admin -run 'TestRuntimeStatus|TestRuntimeOverallStatus' -count=1
go test ./internal/config ./internal/admin ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run InternalStatusPage client ApiDocsPage navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Acceptance Criteria

1. Runtime Status actively probes configured Prometheus readiness and
   `up{job="unirouter"}` scrape state.
2. Runtime Status actively probes configured Grafana `/api/health`.
3. Prometheus and Grafana failures are represented as integration statuses, not
   as `/runtime/status` request failures.
4. Response fields include status, configured URL, health URL, checked time,
   latency, reasons, and Prometheus scrape status or Grafana version where
   applicable.
5. Frontend Runtime Observability cards render the new status vocabulary,
   external links, reasons, scrape status, latency, and version.
6. Status tag colors are not misleading for connected, stale, misconfigured,
   unavailable, or not_configured states.
7. Docs consistently mark Batch 1F as Canary passed/Sentinel merged and Batch 1G
   as fixed awaiting Canary.
8. No embedded dashboards, datasource validation, host telemetry collector,
   OpenTelemetry, generic connector framework, runtime plugin loading, or
   mutating PlungeIn behavior is introduced.
9. Focused backend/frontend tests, broad Go/frontend tests, frontend build, docs
   grep, and `git diff --check` pass.
