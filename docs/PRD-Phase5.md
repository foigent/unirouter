# UniRouter Product Requirements Document - Phase 5

> Version: Phase 5 Active Scope Draft
> Date: 2026-07-08
> Status: Batch 1A/1B/1C, Batch 1D, and Batch 1E Canary/Sentinel complete.
> Batch 1F payload capture mode configuration fixed awaiting Canary. PRD4
> remains complete for the confirmed current scope and is not reopened by PRD5.
> Source: `docs/planning/PRD5-BACKLOG.md`,
> `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`, X-Insight shared-folder
> responses on 2026-07-08, current UniRouter code/schema review, and official
> source refresh for LiteLLM, New API, one-api, Langfuse, and Helicone on
> 2026-07-08.

---

## 1. Product Positioning

PRD5 moves UniRouter from a request-level enterprise AI gateway toward a
high-fidelity evidence source for AI and Agent execution governance.

Core product sentence:

> UniRouter PRD5 should keep UniRouter focused as an enterprise AI gateway and
> management surface, while making gateway-native request, routing, billing,
> policy, actor, payload-policy, and provider evidence good enough for X-Insight
> and future ecosystem integrations without creating a synchronous dependency on
> X-Insight.

X-Insight is a separate project and should own broader AI/Agent observability,
normalization, capture-gap analysis, governance investigation, policy simulation,
and cross-system evidence views. UniRouter should not duplicate that product.

## 2. Scope Principles

PRD5 may:

- add controlled snapshot and export tooling for X-Insight Gate A;
- define and later add nullable structural evidence fields;
- generate missing gateway `trace_id` / `span_id` values while preserving trusted
  upstream IDs when allowed;
- add `upstream_request_id` capture where provider responses expose it;
- encode source refs, capture mode, capture gaps, decision records, actor and
  delegation fields, and payload-policy metadata;
- keep a lightweight request trace UI focused on UniRouter gateway debugging;
- add X-Insight deep-link hooks after X-Insight exposes stable execution URLs;
- compare LiteLLM, New API, one-api, Langfuse, Helicone, and similar systems to
  decide which gateway features UniRouter should catch up on versus leave to
  X-Insight;
- reopen real upstream and real SSO target-environment evidence only when
  approved credentials and environments exist.

PRD5 must not:

- reopen PRD4 or change PRD4 acceptance counts;
- block UniRouter request handling on X-Insight availability;
- make X-Insight a required runtime dependency for model requests;
- store raw prompts or responses by default;
- add a broad exporter/outbox before X-Insight Gate A proves the current-table
  import gaps;
- create shared private GitHub organization policy for UniRouter and X-Insight;
- create or update shared ECS staging before local X-Insight Gate A import works
  and CI is green;
- turn UniRouter into a full LLM observability/evals product, prompt-management
  platform, agent graph UI, or X-Insight clone.

## 3. Scope Summary

### P0

| ID | Workstream | Summary |
|----|------------|---------|
| P5-F01 | PRD5 Scope And Acceptance Baseline | Create formal PRD5 scope, acceptance matrix, implementation plan, competitor comparison, and current source routing |
| P5-F02 | X-Insight Gate A Sanitized Snapshot | Provide explicit-request controlled snapshot tooling and runbook for `call_logs` and `request_trace_events` |
| P5-F03 | Structural Evidence Field Contract | Define field dictionary, enum values, trust boundaries, nullable migration shape, and writer behavior before schema changes |
| P5-F04 | Payload Policy And Evidence References | Keep raw payload off by default and define payload capture mode, hash/ref behavior, retention and ownership split |

### P1

| ID | Workstream | Summary |
|----|------------|---------|
| P5-F05 | Nullable Structural Field Implementation | Add nullable DB/API/writer/read-side support for trace/span/source/decision/actor/session/evidence fields |
| P5-F06 | Lightweight Trace UI Boundary | Keep UniRouter request-level trace investigation and later add X-Insight deep links without duplicating X-Insight UI |
| P5-F07 | Competitor-Informed Feature Catch-Up | Add UniRouter-owned gateway feature gaps revealed by LiteLLM/New API/one-api/Helicone/Langfuse comparison |
| P5-F08 | Read-Only DB/View Gate | Consider restricted read-only views only after X-Insight local snapshot import succeeds |
| P5-F13 | Upstream Request Correlation | Capture provider-returned upstream request IDs into gateway trace/log evidence without trusting public inbound headers |
| P5-F14 | Event Capture Gap Emission | Mark trace events as redacted when UniRouter drops or truncates trace metadata during safe evidence capture |
| P5-F15 | Payload Capture Mode Configuration | Expose restricted runtime configuration for metadata-only or disabled payload evidence state without enabling raw capture |

### P2

| ID | Workstream | Summary |
|----|------------|---------|
| P5-F09 | Cursor Export API Or Outbox | Design non-blocking exporter/outbox only after Gate A import reveals real capture gaps and volume needs |
| P5-F10 | Real Upstream Target E2E | Run PRD4 real-upstream harness in an approved target environment when credentials/model slugs exist |
| P5-F11 | Real SSO Target E2E | Run PRD4 real-SSO harness against an approved IdP/browser target when tenant/test user exists |
| P5-F12 | Larger Deferred Backlog Screening | Keep scheduler, plugin marketplace, managed HA/IaC, full SDK, hard budget block, runtime cache, and new roles behind separate explicit reopen decisions |

## 4. Functional Requirements

### P5-F01: PRD5 Scope And Acceptance Baseline

Requirements:

- Create this PRD5 scope document.
- Create `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`.
- Create `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`.
- Create a source-grounded competitor comparison and feature catch-up plan.
- Update first-read docs and indexes so PRD5 becomes discoverable without
  changing PRD4 status.

Acceptance:

- Current docs route PRD5 work through PRD5 documents, not PRD4.
- PRD4 remains complete for confirmed scope.
- Planned, fixed-awaiting-Canary, deferred, and blocked states are explicit.

### P5-F02: X-Insight Gate A Sanitized Snapshot

Requirements:

- Provide a read-only snapshot generator for selected `request_id` values.
- Export only `call_logs` and `request_trace_events` in the first slice.
- Preserve IDs, `request_id`, ordering, timing, usage, cost, status, sanitized
  metadata, and source refs.
- Do not export raw prompt/response, API keys, credentials, raw upstream errors,
  raw IPs, or customer payloads.
- Represent missing rows as capture gaps instead of fabricating evidence.

Acceptance:

- `go run scripts/prd5_xinsight_snapshot.go -help` compiles and documents the
  tool.
- The runbook explains DSN handling, request selection, output safety, and handoff
  to X-Insight.
- The shared folder contains a snapshot contract X-Insight can review.

### P5-F03: Structural Evidence Field Contract

Requirements:

- Define `trace_id`, `span_id`, `parent_span_id`, `session_id`,
  `upstream_request_id`, `schema_version`, `source_component`, `capture_mode`,
  `capture_gap`, `evidence_ref`, `source_event_hash`, `decision_kind`,
  `decision_result`, `decision_reason`, `actor_type`, `actor_ref`, and
  `on_behalf_of`.
- Align enum values with X-Insight's 2026-07-08 guidance.
- Preserve trusted external correlation IDs only from approved internal callers
  or configured headers/metadata.
- Generate missing gateway trace/span IDs for new evidence once implementation
  starts.
- Keep fields nullable so historical data does not get fabricated.

Acceptance:

- Field dictionary, enum values, trust rules, and migration/writer plan exist in
  `docs/tech-ref/26-prd5-evidence-field-contract.md`.
- Implementation batches can add nullable fields without changing gateway
  behavior first.

### P5-F04: Payload Policy And Evidence References

Requirements:

- Default raw prompt/response capture to off.
- Track payload capture state through metadata such as `payload_capture_mode`,
  `payload_ref`, `evidence_ref`, hashes, and redaction state.
- Keep UniRouter responsible for gateway capture decisions and safe metadata.
- Keep X-Insight responsible for broader payload-state display, evidence object
  references, audit-on-read, retention/delete/export workflows, and future
  encrypted evidence object store if enabled.

Acceptance:

- Snapshot output is metadata-only.
- New request trace evidence exposes `payload_capture_mode` and evidence
  references without exposing raw prompt or response bodies.
- Future raw payload capture requires explicit enterprise opt-in, encryption/key
  management, retention, deletion/export, RBAC, audit-on-read, and legal-hold
  decisions.

### P5-F05: Nullable Structural Field Implementation

Requirements:

- Add nullable structural columns to trace/log evidence surfaces in a dedicated
  migration batch.
- Populate new fields from gateway-native execution points.
- Preserve current API compatibility and existing trace UI behavior.
- Add read-side redaction and response tests before exposing fields.

Acceptance:

- New requests have stable gateway trace/span/source/decision/actor evidence
  where UniRouter can know it directly.
- Missing or external-only facts remain null or capture gaps.
- Existing PRD4 trace behavior remains backward compatible.

Batch 1B implementation note: migration
`migrations/000018_prd5_structural_evidence_fields.up.sql` adds nullable/default
structural evidence fields to `request_trace_events` and `call_logs`. Gateway
trace/log writes now populate gateway-known `trace_id`, generated `span_id` for
trace events, schema/capture/source fields, decision fields, actor fields,
metadata-only payload mode, evidence refs, and source event hashes. Batch 1D
extends this by capturing provider-returned upstream request IDs from a bounded
response-header allowlist and writing them to provider trace events plus call
logs. Batch 1F makes the existing payload capture state configurable through a
restricted runtime setting with only `metadata_only` and `disabled` active. The
trace API returns the new fields through the existing route and RBAC path. Batch
1B/1D/1F do not add trusted inbound headers, raw payload capture,
exporter/outbox, frontend behavior, or new API routes.

### P5-F06: Lightweight Trace UI Boundary

Requirements:

- Keep UniRouter UI focused on why a gateway request routed, costed, retried,
  failed, or was blocked.
- Do not build X-Insight's cross-system investigation console inside UniRouter.
- Add X-Insight deep-link placeholders only after X-Insight exposes stable
  execution identifiers and URL shape.

Acceptance:

- UniRouter trace UI remains request-level and lightweight.
- X-Insight owns multi-source execution search, timeline, node detail, capture
  gaps, policy simulation, and evidence packs.

Batch 1C implementation note: the existing Logs request detail view now displays
an evidence quality summary and per-event structural evidence tags from
`GET /api/v1/logs/{id}/trace`, including trace/span IDs, capture mode, payload
mode, source component, decision fields, actor/session fields, evidence refs,
source hashes, and capture gaps. It does not add X-Insight timeline/graph UI or
a new route.

### P5-F07: Competitor-Informed Feature Catch-Up

Requirements:

- Compare UniRouter against LiteLLM, New API, one-api, Langfuse, Helicone, and
  X-Insight research inputs.
- Split catch-up work into UniRouter-owned gateway capability, X-Insight-owned
  observability/governance capability, and deferred/out-of-scope capability.
- Add only UniRouter-position-compatible catch-up work to PRD5.

Acceptance:

- `docs/planning/PRD5-COMPETITOR-COMPARISON.md` exists and links source basis.
- The PRD5 matrix records the catch-up items without turning UniRouter into a
  generic observability or prompt/eval platform.

### P5-F08: Read-Only DB/View Gate

Requirements:

- Consider read-only database access or views only after local X-Insight snapshot
  import passes.
- Prefer restricted views over whole-table privileges.
- Use bounded cursors and avoid broad scans.
- Never write secrets or DSNs into the shared folder or repo docs.

Acceptance:

- Gate conditions are documented before any credential handoff.
- No shared ECS staging update happens before local Gate A and CI pass.

### P5-F13: Upstream Request Correlation

Requirements:

- Extract provider-returned upstream request IDs only from a bounded response
  header allowlist.
- Reject empty, non-printable-ASCII, whitespace-containing, or
  control-character-containing values.
- Store the value in `request_trace_events.upstream_request_id` for provider
  response events and in `call_logs.upstream_request_id` for request-level
  lookup/import.
- Preserve request-path behavior, provider routing, billing, response payloads,
  and existing API routes.

Acceptance:

- Gateway tests prove sanitized upstream request IDs are captured in trace and
  call-log evidence writes.
- No arbitrary inbound public request header becomes evidence authority.
- Missing provider IDs remain null; UniRouter does not fabricate them.

### P5-F14: Event Capture Gap Emission

Requirements:

- Detect when trace event metadata is intentionally dropped, shape-rejected, or
  truncated by UniRouter's metadata sanitizer.
- Store `capture_gap='redacted'` on the affected `request_trace_events` row.
- Preserve sanitized metadata output and existing trace API/frontend behavior.
- Include `capture_gap` in the trace source-event hash so persisted evidence
  integrity reflects the gap state.

Acceptance:

- Gateway trace tests prove restricted or unsupported metadata creates
  `capture_gap='redacted'`.
- Metadata that is fully retained keeps `capture_gap` null.
- No raw prompt/response, secret, nested metadata, new migration, new route,
  exporter/outbox, frontend behavior, or PRD4 matrix change is introduced.

### P5-F15: Payload Capture Mode Configuration

Requirements:

- Add `PAYLOAD_CAPTURE_MODE` as runtime configuration for the payload evidence
  state written to trace events and call logs.
- Default to `metadata_only`.
- Allow only `metadata_only` and `disabled` in the active implementation.
- Reject unsupported values during config validation, including `raw`,
  `redacted`, `hash_only`, and `reference_only` until those modes have explicit
  product, security, and operational acceptance.
- Persist the configured value in `request_trace_events.payload_capture_mode`
  and `call_logs.payload_capture_mode`.
- Include `payload_capture_mode` in source-event hashes so integrity/dedupe
  reflects the active payload evidence state.

Acceptance:

- `PAYLOAD_CAPTURE_MODE=metadata_only` preserves the existing metadata-only
  evidence state.
- `PAYLOAD_CAPTURE_MODE=disabled` records disabled payload evidence state on
  new trace/log writes without storing raw prompt or response bodies.
- Invalid values fail configuration validation before runtime use.
- No migration, API route, frontend behavior, exporter/outbox, read-only DB
  access, raw payload capture, or PRD4 matrix change is introduced.

### P5-F09: Cursor Export API Or Outbox

Requirements:

- Design exporter/outbox only after Gate A current-table import proves specific
  gaps.
- Keep exporter non-blocking and failure-isolated from gateway request handling.
- Define cursor, dedupe, retry, backpressure, retention, and redaction behavior.

Acceptance:

- Exporter/outbox design references Gate A evidence and structural field quality.
- No synchronous dependency on X-Insight is introduced.

### P5-F10 / P5-F11: Real Target Evidence

Requirements:

- Reuse PRD4 real upstream and real SSO harnesses only when approved environments
  exist.
- Keep credentials, DSNs, user accounts, and cleanup plans outside source control.
- Record target evidence separately from local/snapshot evidence.

Acceptance:

- Target evidence includes date, environment, commit/artifact, credential
  boundary, commands/manual path, expected result, actual result, and cleanup.

## 5. Batch Strategy

PRD5 should proceed in independently verifiable batches:

1. Batch 1A: PRD5 baseline, competitor comparison, structural field contract,
   sanitized snapshot tool, and X-Insight snapshot contract.
2. Batch 1B: nullable structural field migration and writer/read-side behavior.
3. Batch 1C: trace UI evidence quality display and API exposure.
4. Batch 1D: provider upstream request ID capture into trace/log evidence.
5. Batch 1E: event capture-gap emission for metadata redaction/truncation.
6. Batch 1F: restricted payload capture mode configuration for
   `metadata_only` and `disabled`.
7. Later payload policy metadata and evidence references, with future raw
   payload modes gated behind enterprise opt-in.
8. Batch 1G: read-only view/export decision after X-Insight Gate A evidence.
9. Batch 1H+: target upstream/SSO evidence or larger backlog items only after
   their prerequisites are explicitly met.

## 6. External Source Refresh

PRD5 competitor and feature planning used these current official sources on
2026-07-08:

- LiteLLM official docs for proxy logging, callbacks, OpenTelemetry, and callback
  configuration.
- New API official GitHub repository and changelog.
- one-api official GitHub repository and README.
- Langfuse official docs and GitHub repository.
- Helicone official docs and GitHub repository.

Before public claims, pricing claims, or adapter implementation, repeat the
source refresh against exact versions or commits.
