# FIX-SIGNAL.md - PRD4 Batch 1M Trace Metadata Read-Side Redaction

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R3 secret/redaction and trace API response behavior
**Scope:** P4-F12 trace metadata read-side redaction

## Verification Request

Please verify PRD4 Batch 1M: Trace Metadata Read-Side Redaction.

This batch closes the next concrete `P4-F12` redaction slice tracked as
`PRD4-REDACTION-002`:

- sanitizes `request_trace_events.metadata` again at the
  `GET /api/v1/logs/{id}/trace` response boundary;
- mirrors the existing trace write-side allowlist for read-side responses;
- preserves safe scalar trace metadata such as `estimated_tokens`,
  `usage_source`, route counters, token counts, costs, and latency fields;
- drops restricted or unsupported metadata such as `api_key`, `authorization`,
  `access_token`, `request_body`, `prompt`, nested objects, and arrays;
- adds handler and helper regressions proving historical or manually inserted
  sensitive metadata is not returned by the trace API.

## Scope To Verify

Product/test files:

- `internal/admin/log_handlers.go`
- `internal/admin/log_handlers_test.go`
- `internal/gateway/trace.go`
- `internal/traceutil/metadata.go`
- `internal/traceutil/metadata_test.go`

Status/docs files:

- `docs/agent-workflows/FIX-SIGNAL.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/CODEBASE-MAP.md`

## Expected Behavior

- Trace events already written by the gateway should continue to expose safe
  metadata fields needed for request investigation.
- The trace API must not rely only on write-side sanitization, because older,
  manually inserted, or future buggy rows may contain restricted metadata keys.
- `GET /api/v1/logs/{id}/trace` must return only allowlisted scalar metadata.
- Safe strings are trimmed and capped at 256 bytes before they are returned.
- Project-scoped trace access, request lookup, event ordering, route
  registration, and response shape must remain unchanged.

## Non-Scope

Do not expect this batch to:

- add prompt/response plaintext storage, trace payload capture, OpenTelemetry,
  trace partitioning, archival, search indexing, or runtime cache;
- change the trace event write-side sampling/storage model;
- change Logs page frontend layout, OpenAPI routes, RBAC, audit, billing,
  routing, auth, SSO, upstream, wallet, or provider dispatch;
- change runtime monitoring URL redaction already accepted in Batch 1L;
- claim target-environment `Production verified` evidence.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "traceutil|SanitizeMetadata|sanitizeTraceResponseMetadata|TestSanitizeTraceResponseMetadata|PRD4-REDACTION-002|P4-F12|Batch 1M" internal/admin internal/gateway internal/traceutil docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md

go test ./internal/traceutil -count=1
go test ./internal/admin -run 'TestGetRequestTrace|TestSanitizeTraceResponseMetadata' -count=1
go test ./internal/gateway -run 'TestRecordTraceEvent_SanitizesMetadataAndSnapshotsOwner' -count=1
go test ./internal/admin -count=1
go test ./... -count=1

git diff --check
```

## Acceptance Criteria

1. `GetRequestTrace` sanitizes metadata after JSON unmarshalling and before
   writing the response.
2. Allowlisted safe scalar fields remain visible for trace investigation.
3. Restricted keys such as `api_key`, `authorization`, `access_token`,
   `request_body`, and `prompt` are absent from API responses.
4. Unsupported nested objects and arrays are dropped even when their key is
   allowlisted.
5. Safe string values are trimmed and capped at 256 bytes.
6. The handler regression proves response bodies do not contain seeded secrets.
7. No route, RBAC, frontend layout, DB schema, migration, trace write-side
   sampling/storage, billing, routing, auth, SSO, upstream, wallet, or
   provider-dispatch behavior changes.
8. Focused admin tests, broad Go regression, and `git diff --check` pass.
