# FIX-SIGNAL.md - PRD4 Batch 1K Trace Retention And Cleanup

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R3 trace retention / cleanup behavior
**Scope:** metadata-only `request_trace_events` retention configuration and cleanup wiring

## Verification Request

Please verify PRD4 Batch 1K: Trace Retention And Cleanup.

This batch closes the first production-safety gap from `PRD4-TRACE-002`:

- adds `TRACE_EVENT_RETENTION_DAYS`;
- defaults trace-event retention to `LOG_RETENTION_DAYS`;
- validates trace and log retention values are non-negative;
- exposes retention knobs in `.env.example`, `docker-compose.yml`, and the
  deployment runbook;
- wires the existing cleanup loop to delete expired `request_trace_events`;
- keeps metadata-only trace schemas, redaction behavior, RBAC, and UI unchanged.

## Scope To Verify

Product/config files:

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/admin/audit_handlers.go`
- `internal/admin/admin_mock_test.go`
- `cmd/uniroute/main.go`
- `.env.example`
- `docker-compose.yml`
- `docs/ops/deployment-runbook.md`

Status/docs files:

- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PRD-Phase4.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/CODEBASE-MAP.md`
- `docs/planning/PRD4-BACKLOG.md`

Pre-existing status reconciliation in these docs also reflects PRD4 Batch 1J
Runtime Host Telemetry after PR #94. It should be verified only for consistency;
it is not a new product change in Batch 1K.

## Expected Behavior

- `TRACE_EVENT_RETENTION_DAYS` defaults to `LOG_RETENTION_DAYS`.
- `TRACE_EVENT_RETENTION_DAYS=0` skips trace-event cleanup but does not disable
  trace writes. Trace writes are still controlled by `TRACE_EVENTS_ENABLED`.
- Negative `TRACE_EVENT_RETENTION_DAYS` or `LOG_RETENTION_DAYS` fails config
  validation.
- `startCleanupLoop` passes log retention and trace-event retention separately.
- Cleanup deletes expired `request_trace_events` only when trace retention is
  greater than zero.
- Existing `CleanupOldData(ctx, retentionDays)` default behavior remains
  compatible for existing callers and tests.

## Non-Scope

Do not expect this batch to:

- add OpenTelemetry ingestion/export;
- add trace partitioning, archival, or cold storage;
- add prompt/response plaintext storage;
- add runtime cache;
- add new trace UI or global request-id lookup;
- change trace event schema, migration, RBAC, redaction allowlist, or sampling
  hash behavior;
- change billing, routing, SSO, upstream, auth, wallet, or provider dispatch;
- claim target-environment `Production verified` evidence.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "TRACE_EVENT_RETENTION_DAYS|CleanupOldDataWithTraceRetention|request_trace_events|LOG_RETENTION_DAYS" internal cmd .env.example docker-compose.yml docs/ops/deployment-runbook.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md

go test ./internal/config -count=1
go test ./internal/admin -run 'TestCleanupOldData|TestCleanupOldDataWithTraceRetention' -count=1
go test ./internal/admin -count=1
go test ./cmd/uniroute -count=1
go test ./... -count=1

git diff --check
```

## Acceptance Criteria

1. Config exposes `TraceEventRetentionDays` and loads it from
   `TRACE_EVENT_RETENTION_DAYS`.
2. `TRACE_EVENT_RETENTION_DAYS` defaults to `LOG_RETENTION_DAYS`.
3. Config validation rejects negative trace or log retention values.
4. The cleanup loop passes log retention and trace-event retention separately.
5. Cleanup deletes old `request_trace_events` using trace retention when it is
   greater than zero.
6. Trace cleanup is skipped when trace retention is zero.
7. Existing cleanup default behavior remains compatible for existing callers.
8. Env/compose/runbook docs expose the retention controls and clarify that
   disabling cleanup is separate from disabling trace writes.
9. No trace schema, prompt/response storage, RBAC, sampling, billing, routing,
   auth, SSO, upstream, or Production verified behavior is changed.
10. Focused tests, broad Go regression, and `git diff --check` pass.
