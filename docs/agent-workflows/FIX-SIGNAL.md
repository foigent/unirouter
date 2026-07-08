# FIX-SIGNAL.md - PRD5 Batch 1A/1B/1C Scope / Snapshot / Structural Evidence / Trace UI

**Date:** 2026-07-08
**Risk level:** R3 database/gateway evidence write/read change plus R2
acceptance-sensitive documentation/product planning and frontend trace UI
exposure plus R1 read-only evidence tooling
**Status:** awaiting Canary verification

## Context

PRD4 is complete for the confirmed current scope. This signal must not reopen
PRD4, change PRD4 acceptance counts, or promote any PRD4 row to
`Production verified`.

This round starts PRD5 with three bounded slices:

- Batch 1A closes the missing PRD5 planning surfaces and adds the sanitized,
  explicit-request X-Insight Gate A snapshot generator.
- Batch 1B adds nullable/default structural evidence fields and populates
  gateway-known values on new trace/log writes.
- Batch 1C exposes the structural evidence in the existing lightweight Logs
  request detail UI without adding an X-Insight timeline/graph surface.

UniRouter and X-Insight remain separate projects. X-Insight owns broader
AI/Agent observability and governance. UniRouter owns gateway-native evidence
quality and lightweight request trace/debug surfaces.

## Intended Changes

UniRouter docs/planning:

- Add `docs/PRD-Phase5.md`.
- Add `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`.
- Add `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`.
- Add `docs/planning/PRD5-COMPETITOR-COMPARISON.md`.
- Add `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md`.
- Add `docs/tech-ref/26-prd5-evidence-field-contract.md`.
- Update `docs/README.md`, `docs/DOC-INVENTORY.md`,
  `docs/PROJECT-HANDOFF.md`, `docs/ops/CURRENT-STATUS.md`,
  `docs/CODEBASE-MAP.md`, `docs/tech-ref/README.md`, and
  `docs/planning/PRD5-BACKLOG.md` so PRD5 Batch 1A/1B/1C is discoverable.

UniRouter code/tooling:

- Add `scripts/prd5_xinsight_snapshot.go`.
- Add migration `migrations/000018_prd5_structural_evidence_fields.up.sql`.
- Add rollback `migrations/000018_prd5_structural_evidence_fields.down.sql`.
- Update `internal/gateway/trace.go` to populate trace/span/source/capture,
  decision, actor, payload mode, evidence ref, and source event hash fields on
  trace events.
- Update `internal/gateway/relay.go` to populate selected structural call-log
  fields on request log writes.
- Update `internal/admin/log_handlers.go` to return structural fields from the
  existing `GET /api/v1/logs/{id}/trace` route.
- Update focused gateway/admin tests.
- Update `frontend/src/pages/LogsPage.tsx` to show an evidence quality summary
  and per-event structural evidence tags in the existing request detail panel.
- Update `frontend/src/pages/__tests__/LogsPage.test.tsx`.

Shared folder:

- Add
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-08-unirouter-prd5-gate-a-snapshot-contract.md`.
- Update
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/README.md`
  to list the new contract.

## Non-Goals

This round does not:

- add a new API route;
- modify deployment scripts, Docker, compose, or monitoring config;
- change routing, billing, wallet, auth, provider dispatch, or admission
  semantics beyond metadata/evidence writes;
- add an X-Insight timeline, graph, simulation, evidence-pack, or cross-system
  investigation UI inside UniRouter;
- add X-Insight deep links before X-Insight provides a stable URL shape;
- trust new inbound trace/session headers;
- create or expose read-only DB credentials, DSNs, passwords, tokens, or secrets;
- create shared ECS staging;
- implement X-Insight importer;
- implement UniRouter exporter/outbox;
- store raw prompts or raw responses;
- expose raw IP addresses or raw upstream error text in the snapshot;
- change PRD4 acceptance matrix counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Re-read:
   - `docs/PRD-Phase5.md`
   - `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
   - `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`
   - `docs/planning/PRD5-COMPETITOR-COMPARISON.md`
   - `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md`
   - `docs/tech-ref/26-prd5-evidence-field-contract.md`
   - `docs/planning/PRD5-BACKLOG.md`
2. Verify PRD4 remains closed for the confirmed current scope and is not
   reopened by PRD5 Batch 1A/1B/1C.
3. Verify `docs/PRD-Phase5.md` records:
   - UniRouter remains the gateway and management surface;
   - X-Insight owns broader AI/Agent observability/governance;
   - structural fields come before exporter/outbox;
   - raw prompt/response capture is default off;
   - lightweight UniRouter trace UI remains request-level.
4. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks Batch 1A/1B/1C rows as
   `Fixed awaiting Canary`, including nullable structural field implementation,
   lightweight trace UI exposure,
   and leaves read-only DB/view, exporter/outbox, and real target rows deferred
   pending prerequisites.
5. Verify migration `000018_prd5_structural_evidence_fields`:
   - adds nullable/default structural fields to `request_trace_events`;
   - adds selected structural fields to `call_logs`;
   - adds trace/upstream indexes only;
   - has a matching down migration.
6. Verify gateway trace writes populate only gateway-known metadata fields:
   `trace_id`, `span_id`, `schema_version`, `source_component`, `capture_mode`,
   `evidence_ref`, `decision_kind`, `decision_result`, `decision_reason`,
   `actor_type`, `actor_ref`, `payload_capture_mode`, and `source_event_hash`.
7. Verify call-log writes populate only selected structural fields and do not
   change usage/cost/status persistence semantics.
8. Verify `GET /api/v1/logs/{id}/trace` returns the structural fields through
   the existing route and keeps the existing RBAC and metadata sanitizer.
9. Verify `frontend/src/pages/LogsPage.tsx` keeps the trace UI request-level and
   displays evidence quality / structural evidence fields without adding
   X-Insight timeline, graph, simulation, or evidence-pack UI.
10. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` states UniRouter can
   proceed independently and only gates read-only DB/view, shared ECS staging,
   exporter/outbox, and real target E2E on later prerequisites.
11. Verify `docs/planning/PRD5-COMPETITOR-COMPARISON.md` compares UniRouter with
    LiteLLM, New API, one-api, Langfuse, and Helicone, and separates
    UniRouter-owned catch-up from X-Insight-owned observability/governance.
12. Verify `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md` and
    `scripts/prd5_xinsight_snapshot.go` enforce explicit request selection and
    do not support broad table export.
13. Verify the snapshot tool does not select or output raw prompt, raw response,
    API key plaintext, credentials, DSNs, tokens, raw IP addresses, or raw
    `upstream_error` text.
14. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` includes
    `trace_id`, `span_id`, `parent_span_id`, `schema_version`,
    `source_component`, `capture_mode`, `capture_gap`, `evidence_ref`,
    `upstream_request_id`, `decision_kind`, `decision_result`,
    `decision_reason`, `actor_type`, `actor_ref`, `on_behalf_of`, and
    `session_id`.
15. Verify docs routing files link the new PRD5 docs:
    - `docs/README.md`
    - `docs/DOC-INVENTORY.md`
    - `docs/PROJECT-HANDOFF.md`
    - `docs/ops/CURRENT-STATUS.md`
    - `docs/CODEBASE-MAP.md`
    - `docs/tech-ref/README.md`
16. Verify the shared snapshot contract exists:
    - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-08-unirouter-prd5-gate-a-snapshot-contract.md`
17. Run:

```bash
go run scripts/prd5_xinsight_snapshot.go -help
go run scripts/prd5_xinsight_snapshot.go
go run scripts/prd5_xinsight_snapshot.go --request-id req-a --scenario success=req-b
go run scripts/prd5_xinsight_snapshot.go --request-id req-a
go test ./internal/gateway -run 'TestRecord(Request|TraceEvent)' -count=1
go test ./internal/admin -run 'TestGetRequestTrace|TestSanitizeTraceResponseMetadata' -count=1
npm --prefix frontend test -- LogsPage.test.tsx
npm --prefix frontend run build
rg -n "trace_id|span_id|source_component|capture_mode|decision_kind|actor_type|payload_capture_mode|source_event_hash" migrations/000018_prd5_structural_evidence_fields.up.sql internal/gateway/trace.go internal/gateway/relay.go internal/admin/log_handlers.go scripts/prd5_xinsight_snapshot.go docs/tech-ref/26-prd5-evidence-field-contract.md
rg -n "P5-F01|P5-F12|PRD5|X-Insight|sanitized snapshot|trace_id|capture_gap|decision_kind|LiteLLM|New API|one-api|Helicone|Langfuse|LogsPage" docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-COMPETITOR-COMPARISON.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/planning/PRD5-BACKLOG.md frontend/src/pages/LogsPage.tsx frontend/src/pages/__tests__/LogsPage.test.tsx
git diff --check
```

Expected command behavior:

- `go run scripts/prd5_xinsight_snapshot.go` should fail safely because no
  request ID was provided.
- `go run scripts/prd5_xinsight_snapshot.go --request-id req-a --scenario success=req-b`
  should fail safely because the scenario request ID is not selected.
- `go run scripts/prd5_xinsight_snapshot.go --request-id req-a` should fail
  safely if no read-only DSN env var is set.

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered verification instruction;
- any PRD4 boundary regression as a blocking failure;
- any secret/DSN/raw-payload leakage as a blocking failure;
- any snapshot broad-export behavior as a blocking failure;
- any migration rollback mismatch as a blocking failure;
- any gateway request semantic change outside evidence writes as a blocking
  failure;
- any frontend expansion into X-Insight-owned timeline/graph/simulation/evidence
  pack behavior as a scope finding;
- any mismatch between UniRouter and X-Insight ownership as a documentation
  finding;
- a note that no new API route, deployment config, exporter/outbox, read-only
  credential, or production evidence validation was required for Batch
  1A/1B/1C.
