# PRD5 Implementation Plan

> Date: 2026-07-08
> Status: Active plan. Batch 1A/1B/1C fixed awaiting Canary.
> Scope owner: UniRouter. X-Insight remains a separate project and owns broader
> observability/governance product surfaces.

## Current Decision

UniRouter should not wait for X-Insight to finish before making PRD5 progress.

The safe parallel path is:

- UniRouter implements its own evidence-readiness work: snapshot tooling,
  structural field contract, nullable fields, payload boundary, and trace UI
  boundary.
- X-Insight imports the sanitized snapshot locally and reports Gate A gaps.
- UniRouter uses Gate A findings to prioritize field implementation and later
  exporter/outbox design.

This prevents both projects from blocking each other while avoiding a premature
exporter or synchronous dependency.

## Batch Plan

### Batch 1A: Scope, Contract, Snapshot Tooling

Risk: R2 documentation/product acceptance plus R1 read-only tooling.

Deliverables:

- `docs/PRD-Phase5.md`
- `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
- `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`
- `docs/planning/PRD5-COMPETITOR-COMPARISON.md`
- `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md`
- `docs/tech-ref/26-prd5-evidence-field-contract.md`
- `scripts/prd5_xinsight_snapshot.go`
- shared-folder snapshot contract under
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/`

Non-goals:

- no migration;
- no request-path behavior change;
- no frontend UI change;
- no read-only credential handoff;
- no shared ECS staging;
- no exporter/outbox.

Verification:

- `go run scripts/prd5_xinsight_snapshot.go -help`
- docs grep from `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
- `git diff --check`

### Batch 1B: Nullable Structural Fields

Risk: R3 because it touches DB schema and gateway evidence writes.

Implemented in this round, awaiting Canary:

- add nullable fields to `request_trace_events` first;
- evaluate whether selected fields also belong on `call_logs` for query speed or
  historical import compatibility;
- generate gateway `trace_id` / `span_id` when missing;
- preserve trusted external trace/session IDs only from approved headers/metadata;
- capture `upstream_request_id` where adapters/provider responses expose it;
- emit `schema_version`, `source_component`, `capture_mode`,
  `decision_kind/result/reason`, `actor_type/ref`, `on_behalf_of`, `session_id`,
  `evidence_ref`, and `source_event_hash` where known;
- add migration rollback notes and focused DB/API/gateway tests.

Do not backfill historical rows with fabricated facts. Historical missing values
should remain null or become X-Insight capture gaps.

Current Batch 1B boundary:

- adds migration `000018_prd5_structural_evidence_fields`;
- populates gateway-known fields on new trace/log writes;
- exposes new trace fields through existing `GET /api/v1/logs/{id}/trace`;
- extends snapshot output with structural fields;
- does not add frontend behavior, exporter/outbox, raw payload capture, trusted
  inbound correlation headers, or a new route.

### Batch 1C: Read-Side Exposure And Lightweight Trace UI

Risk: R2/R3 depending on API and frontend scope.

Implemented in this round, awaiting Canary:

- expose new fields through the existing `GET /api/v1/logs/{id}/trace` route
  after redaction review;
- add request-level evidence quality labels for trace ID, source components,
  capture modes, payload mode, upstream request ID, decision count, actor type,
  and capture gaps;
- add per-event structural evidence tags for source, capture, payload, schema,
  gap, trace/span, upstream, actor, session, evidence ref, and source hash;
- avoid full X-Insight timeline/graph UI;
- add placeholder support for future X-Insight execution deep links only after
  X-Insight provides stable URL shape.

### Batch 1D: Payload Policy Metadata

Risk: R3 if schema/API changes; R4 only if raw payload capture is introduced,
which is not planned by default.

Implemented in this round, awaiting Canary:

- keep raw prompt/response off;
- add metadata-only payload state fields;
- expose metadata-only payload mode in trace API, snapshot output, and the
  lightweight trace UI;

Planned later:

- support redacted/hash/reference modes;
- document enterprise opt-in requirements for raw payload capture.

### Batch 1E: X-Insight Gate A Feedback Incorporation

Risk: depends on findings.

Prerequisite:

- X-Insight imports the sanitized snapshot locally;
- X-Insight records import, dedupe, source refs, capture gaps, and read API
  evidence.

Possible outcomes:

- adjust structural field priorities;
- create restricted read-only views;
- define cursor export API;
- design non-blocking exporter/outbox.

### Batch 1F: Real Target Evidence

Prerequisites:

- approved real upstream credentials/model slugs for `PRD5-UPSTREAM-001`; or
- approved Logto/enterprise OIDC tenant/client/test user for `PRD5-SSO-001`.

Use existing PRD4 harnesses, but record target evidence under PRD5.

## Ownership Split

| Area | UniRouter Owns | X-Insight Owns |
|------|----------------|----------------|
| Gateway execution facts | Admission, auth, routing, provider, billing, trace, usage, key ownership | Normalized evidence model and cross-source interpretation |
| Trace/span generation | Gateway-local IDs and trusted inbound preservation | Cross-system trace stitching and capture-gap display |
| Payload capture decision | Gateway decision point, metadata, hashes/refs when safe | Payload state UI, audit-on-read, retention/delete/export for X-Insight-managed evidence |
| UI | Lightweight request trace/debug view | Operator investigation console, execution detail, timeline, node detail, policy simulation, evidence packs |
| Import/export | Snapshot, future views/export/outbox if needed | Importer, dedupe, source refs, read APIs, adapters |
| LiteLLM/New API/one-api | Compare for UniRouter feature catch-up | Public/open-source adapter strategy |

## Current Blocking Conditions

There is no blocking condition for UniRouter Batch 1A through Batch 1D.

These later actions are gated:

- read-only DB/view access: blocked until X-Insight local snapshot import passes
  and User approves credential handoff;
- shared ECS staging: blocked until local Gate A import works and CI is green;
- exporter/outbox: blocked until Gate A shows concrete field/volume/cursor gaps;
- real upstream/SSO target E2E: blocked until approved target environments and
  credentials exist.
