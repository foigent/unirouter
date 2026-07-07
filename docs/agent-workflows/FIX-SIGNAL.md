# FIX-SIGNAL.md - PRD4 Batch 1L Runtime Monitoring URL Redaction

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R3 secret/redaction and runtime status behavior
**Scope:** P4-F12 runtime observability monitoring URL redaction

## Verification Request

Please verify PRD4 Batch 1L: Runtime Monitoring URL Redaction.

This batch closes the first concrete `P4-F12` redaction slice tracked as
`PRD4-REDACTION-001`:

- redacts Prometheus and Grafana `url` / `health_url` response fields from
  `GET /api/v1/runtime/status`;
- strips URL userinfo, query strings, and fragments from displayed monitoring
  URLs;
- keeps monitoring probes on the original configured URL so integrations that
  rely on URL userinfo/basic auth still work;
- adds an httptest regression proving credentials are used for probes but absent
  from the API response;
- leaves runtime status RBAC, route registration, DB/Redis/traffic telemetry,
  host CPU/disk/network summaries, and frontend rendering structure unchanged.

## Scope To Verify

Product/test files:

- `internal/admin/runtime_status_handlers.go`
- `internal/admin/runtime_status_handlers_test.go`

Status/docs files:

- `docs/agent-workflows/FIX-SIGNAL.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`

## Expected Behavior

- `PROMETHEUS_URL` / `GRAFANA_URL` may contain URL userinfo or query params for
  deployment-specific monitoring access.
- The backend must use the original configured URL for probes.
- The runtime status response must not echo URL userinfo, query-string tokens,
  fragments, or malformed raw URL strings.
- `url` and `health_url` in runtime status should remain useful display/deep-link
  values when a valid monitoring URL is configured.
- Existing status outcomes such as `connected`, `stale`, `misconfigured`,
  `unavailable`, and `not_configured` must remain unchanged.

## Non-Scope

Do not expect this batch to:

- add metrics authentication configuration beyond existing URL-based deployment
  settings;
- add embedded Grafana dashboards or datasource validation;
- add OpenTelemetry export, node exporter, cloud metadata, cluster inventory, or
  metric-alert notification routing;
- change runtime status RBAC or frontend page layout;
- change trace metadata redaction, SSO evidence redaction, upstream evidence
  redaction, billing, routing, auth, wallet, or provider dispatch;
- claim target-environment `Production verified` evidence.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "monitoringDisplayURL|RedactsMonitoringURLs|PRD4-REDACTION-001|P4-F12|Batch 1L" internal/admin docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md

go test ./internal/admin -run 'TestRuntimeStatus_RedactsMonitoringURLsButUsesRawProbeURL|TestRuntimeStatus_ReportsExternalMonitoringProblems|TestRuntimeStatus_ReturnsRuntimeHealthAndHostTelemetry' -count=1
go test ./internal/admin -count=1
go test ./... -count=1

git diff --check
```

## Acceptance Criteria

1. `runtimePrometheusIntegration` and `runtimeGrafanaIntegration` probe the raw
   configured URL, not the redacted display URL.
2. Runtime status `url` and `health_url` strip URL userinfo, query strings, and
   fragments.
3. Malformed monitoring display URLs are not echoed back to users.
4. A regression test proves basic-auth URL userinfo is still used for probes.
5. The same regression test proves monitoring passwords and query tokens are not
   present in the runtime status response body.
6. Existing monitoring states and Runtime Observability summaries still pass.
7. No route, RBAC, frontend layout, DB schema, migration, trace storage, billing,
   routing, auth, SSO, upstream, wallet, or provider-dispatch behavior changes.
8. Focused admin tests, broad Go regression, and `git diff --check` pass.
