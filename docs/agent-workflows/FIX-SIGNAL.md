# FIX-SIGNAL.md - PRD4 Batch 1C Flow/Event/Trace Foundation

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-TRACE-001`
**Matrix Row:** `P4-F04`

## Verification Request

Please verify PRD4 Batch 1C: Flow/Event/Trace Foundation first slice.

This batch adds a metadata-only request trace/event foundation so a call log can
be inspected across gateway, billing, routing, provider, and call-log recording
milestones. It does not store prompt plaintext, response plaintext, upstream
provider keys, request bodies, response bodies, or authorization headers.

## Scope To Verify

Data/config:

- Adds migration `000017_prd4_request_trace_events` with
  `request_trace_events`, request/org/project/key/owner/type snapshots, event
  stage/severity/model/provider/channel/status/latency, JSONB metadata, and
  request/org/project indexes.
- Adds `TRACE_EVENTS_ENABLED` and `TRACE_EVENT_SAMPLE_RATE` config, env example,
  compose, and deployment-runbook entries. Sampling is deterministic by
  `request_id` so sampled requests keep complete event sequences.

Backend:

- Records gateway accepted, billing freeze, routing, provider response/failure,
  billing settle, and call-log recorded events for chat completions and
  embeddings.
- Trace recording is best-effort: insert failures are logged and do not block
  the gateway request.
- Sanitizes trace metadata with an allowlist; sensitive fields such as API keys
  and request/response bodies are not persisted.
- Adds `GET /api/v1/logs/{id}/trace`, using the call-log id to load
  `request_id/org_id/project_id` and reusing project usage RBAC before reading
  trace events.

Frontend/API contract:

- Adds `api.getLogTrace(id)`.
- Shows request path events in the existing Logs detail inspector.
- Updates backend `/docs`, frontend API Docs, OpenAPI, and route parity tests.

Docs/status:

- Marks `P4-F04` / `PRD4-TRACE-001` fixed awaiting Canary.
- Keeps PRD2 frozen and PRD3 complete.
- Keeps Batch 1B as Canary/Sentinel complete, with Atlas not reported.

## Non-Scope

Do not expect this batch to implement:

- OpenTelemetry ingestion/export, distributed span semantics, or third-party
  tracing backends.
- Runtime Status Center CPU/memory/network panels.
- Prometheus/Grafana integration status.
- Full PlungeIn runtime plugin framework.
- Scheduler V2, quota enforcement, or capacity-aware dispatch.
- Direct global request-id search across all tenants.
- Trace retention cleanup, archival, or production-scale storage policy.
- Prompt/response plaintext storage.

Trace retention/sampling hardening remains a follow-up P4-F12/P4-F06 concern.
This batch provides a basic enable switch and deterministic sample rate only.

## Required Verification Commands

```bash
rg -n "request_trace_events|TRACE_EVENTS_ENABLED|TRACE_EVENT_SAMPLE_RATE|GetRequestTrace|getLogTrace|gateway.request.accepted|logging.call_log.recorded" migrations internal cmd frontend/src docs/tech-ref/05-openapi-spec.yaml docs/ops/deployment-runbook.md .env.example docker-compose.yml
rg -n "PRD4-TRACE-001|P4-F04|Batch 1C|Flow/Event/Trace" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md
go test ./internal/config ./internal/gateway ./internal/admin ./cmd/uniroute -count=1
go test ./...
npm --prefix frontend test -- --run LogsPage client ApiDocsPage
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Acceptance Criteria

1. Migration adds and rolls back `request_trace_events` without destructive
   data operations.
2. Trace recording captures gateway, billing, routing, provider, settle, and
   call-log milestones for chat and embeddings.
3. Trace inserts are best-effort and cannot fail the gateway request.
4. Metadata is allowlisted and does not persist prompt/response plaintext,
   provider keys, authorization headers, request bodies, or response bodies.
5. `GET /api/v1/logs/{id}/trace` applies the same project usage RBAC boundary
   as log detail and filters events by `request_id`, `org_id`, and `project_id`.
6. Logs detail UI can load and render trace events for the selected call log.
7. Backend route registration, OpenAPI, frontend API Docs, and route parity
   tests are synchronized.
8. Trace enablement and sample rate are configurable and validated.
9. Full Go/frontend tests, frontend build, and `git diff --check` pass.
10. Docs consistently show Batch 1C as fixed awaiting Canary, not accepted.
