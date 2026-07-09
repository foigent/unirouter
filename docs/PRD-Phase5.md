# UniRouter Product Requirements Document - Phase 5

> Version: Phase 5 Active Scope Draft
> Date: 2026-07-09
> Status: Batch 1A/1B/1C, Batch 1D, Batch 1E, Batch 1F, Batch 1G, Batch 1H,
> Batch 1I, Batch 1J, Batch 1K, Batch 1L, and Batch 1M Canary/Sentinel
> complete. Batch 1N snapshot capture-gap candidates fixed awaiting Canary.
> PRD4 remains complete for the confirmed current scope and is not reopened by
> PRD5.
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
| P5-F16 | Trusted Evidence Context Intake | Accept trace/session/actor/delegation context only from explicitly trusted internal callers with a shared secret |
| P5-F17 | Gateway Span Hierarchy | Parent UniRouter-emitted trace events to a per-request gateway root span so execution chains are queryable without X-Insight dependency |
| P5-F18 | Payload Policy Decision Evidence | Emit explicit payload capture policy decision trace events without storing raw prompt or response payloads |
| P5-F19 | Provider Upstream Capture Gap | Mark provider response trace events with `capture_gap=provider_not_returned` when the upstream provider does not return a usable request ID |
| P5-F20 | Gate A Snapshot Handoff Note | Generate a sanitized Markdown handoff note alongside explicit-request X-Insight snapshots without writing DSNs or credentials |
| P5-F21 | Component Span Refinement | Parent repeated same-component trace events to the component's first span while preserving the request gateway root |
| P5-F22 | Provider Failure Capture Gaps | Mark provider failure events that occur before any provider response with `capture_gap=not_available_from_source` when no upstream request ID can exist |
| P5-F23 | Snapshot Capture Gap Candidates | Add structured candidate capture-gap categories to Gate A snapshots for missing trace rows without fabricating runtime evidence |

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
restricted runtime setting with only `metadata_only` and `disabled` active.
Batch 1G adds explicitly trusted evidence context intake for internal callers
using `EVIDENCE_CONTEXT_TRUST_SECRET`; it can populate trusted `trace_id`,
`parent_span_id`, `session_id`, `actor_*`, and `on_behalf_of` fields when the
shared secret matches. Batch 1H makes UniRouter-emitted trace events within the
same request share a gateway root span: the first recorded UniRouter span
preserves a trusted inbound parent when present, and later UniRouter spans parent
to that root. The trace API returns the new fields through the existing route and
RBAC path. Batch 1B/1D/1F/1G/1H do not add raw payload capture, exporter/outbox,
frontend behavior, or new API routes.

Batch 1I adds explicit `payload.policy.applied` trace events for chat and
embeddings requests. These events record sanitized payload policy metadata such
as `payload_capture_mode`, `payload_policy_reason`, `raw_payload_stored=false`,
and `payload_ref_present=false`, and map to `decision_kind=payload_capture`.
Batch 1I does not inspect, persist, hash, or export raw prompt/response bodies.

Batch 1J adds a concrete provider evidence gap: provider response trace events
now record `capture_gap=provider_not_returned` when no sanitized
`upstream_request_id` is available from the upstream response headers. Existing
provider-returned upstream request IDs still win and call-log
`upstream_request_id` null behavior is unchanged.

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

### P5-F16: Trusted Evidence Context Intake

Requirements:

- Add a default-off trust gate for inbound evidence context headers.
- Require `EVIDENCE_CONTEXT_TRUST_SECRET` to be configured before any inbound
  evidence context is accepted.
- Require callers to send `X-UniRouter-Evidence-Auth` matching that shared
  secret before reading any `X-UniRouter-*` evidence headers.
- Accept only bounded printable non-whitespace values for:
  `X-UniRouter-Trace-Id`, `X-UniRouter-Parent-Span-Id`,
  `X-UniRouter-Session-Id`, `X-UniRouter-Actor-Type`,
  `X-UniRouter-Actor-Ref`, and `X-UniRouter-On-Behalf-Of`.
- Support trusted W3C `Traceparent` as a fallback source for `trace_id` and
  `parent_span_id`.
- Persist trusted context to existing `request_trace_events` and `call_logs`
  structural columns, and include it in source-event hashes.

Acceptance:

- With no configured secret, UniRouter ignores inbound trace/session/actor and
  delegation headers.
- With a configured secret but mismatched request auth header, UniRouter ignores
  inbound evidence context.
- With a matching secret, UniRouter writes trusted trace/session/delegation
  context to new evidence rows.
- No public inbound header becomes evidence authority by default.
- No migration, API route, frontend behavior, exporter/outbox, read-only DB
  access, raw payload capture, or PRD4 matrix change is introduced.

### P5-F17: Gateway Span Hierarchy

Requirements:

- Create a per-request UniRouter span state for gateway-emitted trace events.
- Treat the first UniRouter-emitted trace event in a request as the gateway root
  span.
- If a trusted inbound `parent_span_id` exists, persist it as the parent of the
  gateway root span.
- Parent subsequent UniRouter-emitted trace events in the same request to the
  gateway root span.
- Preserve the existing `trace_id` behavior: trusted inbound trace ID when
  present, otherwise the request ID.
- Include the resulting `parent_span_id` in source-event hashes.

Acceptance:

- A request with a trusted inbound parent records the first UniRouter span under
  that parent and later UniRouter spans under the gateway root span.
- A request without trusted inbound context still produces a stable per-request
  UniRouter span hierarchy.
- Existing trace/log writes, request handling, routing, billing, provider
  dispatch, response payloads, API routes, and frontend behavior remain
  unchanged.
- No migration, exporter/outbox, read-only DB access, raw payload capture,
  X-Insight runtime dependency, or PRD4 matrix change is introduced.

### P5-F18: Payload Policy Decision Evidence

Requirements:

- Emit an explicit `payload.policy.applied` trace event for chat and embeddings
  gateway requests after request admission and before billing freeze.
- Use `source_component=payload_policy`.
- Populate sanitized metadata for `payload_capture_mode`,
  `payload_policy_reason`, `raw_payload_stored`, and `payload_ref_present`.
- Map the event to `decision_kind=payload_capture`.
- Use `decision_result=allowed` for `metadata_only` and `decision_result=skipped`
  for `disabled`.
- Preserve raw prompt/response default-off behavior.

Acceptance:

- New request traces include a payload policy decision event when trace sampling
  records the request.
- Payload policy metadata survives trace metadata sanitization without creating
  a redacted capture gap.
- Disabled payload mode records a skipped payload capture decision without
  storing raw prompt or response bodies.
- Existing request handling, routing, billing, provider dispatch, response
  payloads, API routes, and frontend behavior remain unchanged.
- No migration, exporter/outbox, read-only DB access, raw payload capture,
  X-Insight runtime dependency, or PRD4 matrix change is introduced.

### P5-F19: Provider Upstream Capture Gap

Requirements:

- When provider response trace events do not have a sanitized
  `upstream_request_id`, write `capture_gap=provider_not_returned`.
- Apply this only to `provider.response_accepted` and
  `provider.response_rejected` trace events.
- Preserve provider-returned upstream request IDs when present.
- Preserve `capture_gap=redacted` when the same event already has a redaction
  gap from metadata sanitization.
- Keep request-level `call_logs.upstream_request_id` null when no provider ID is
  available.

Acceptance:

- Provider response trace events distinguish missing provider request IDs from
  absent instrumentation.
- Existing upstream request ID extraction, routing, billing, provider dispatch,
  response payloads, API routes, and frontend behavior remain unchanged.
- No migration, exporter/outbox, read-only DB access, raw payload capture,
  X-Insight runtime dependency, or PRD4 matrix change is introduced.

### P5-F20: Gate A Snapshot Handoff Note

Requirements:

- Add an optional `--handoff-note` flag to the PRD5 X-Insight snapshot tool.
- Write a Markdown note only after the selected snapshot has been loaded and the
  JSON output has been written successfully.
- Include generated date, source instance, schema version, selected request IDs,
  scenario labels, counts, safety confirmation, snapshot output path, and command
  shape without DSN.
- Never write DSNs, credentials, tokens, raw prompts, raw responses, raw IP
  addresses, or raw upstream error text into the note.
- Reuse the same `--overwrite` behavior as the snapshot output path and reject
  stdout for the handoff note path.

Acceptance:

- `go run scripts/prd5_xinsight_snapshot.go -help` documents
  `--handoff-note`.
- Handoff notes give X-Insight enough context to run discovery against a shared
  sanitized snapshot and compare it with the PRD5 snapshot contract.
- The feature does not create read-only DB/view credentials, shared ECS staging,
  exporter/outbox behavior, raw payload capture, or an X-Insight runtime
  dependency.

### P5-F21: Component Span Refinement

Requirements:

- Keep the first UniRouter-emitted trace event as the request gateway root span.
- Preserve trusted inbound `parent_span_id` only as the gateway root parent.
- Treat the first event for each `source_component` as that component's local
  span root.
- Parent later events from the same `source_component` to that component root
  span instead of flattening every event directly under the gateway root.
- Do not claim a cross-system agent graph or infer external tool/business
  system spans.

Acceptance:

- Focused gateway tests prove gateway-root parenting still works and repeated
  same-component events parent to the component span root.
- Existing trusted trace context, provider upstream gaps, redaction gaps,
  routing, billing, provider dispatch, response payloads, API routes, and
  frontend behavior remain unchanged.
- No migration, new route, exporter/outbox, read-only DB/view access, raw
  payload capture, X-Insight runtime dependency, or PRD4 matrix change is
  introduced.

### P5-F22: Provider Failure Capture Gaps

Requirements:

- When provider failure trace events occur before a provider response exists,
  write `capture_gap=not_available_from_source` if there is no sanitized
  `upstream_request_id`.
- Apply this to provider key decryption, adapter selection, request build,
  outbound request failure, and response parse failure trace events.
- Keep `provider.response_accepted` and `provider.response_rejected` on the
  existing `capture_gap=provider_not_returned` rule when a provider response
  exists but does not return a usable request ID.
- Preserve `capture_gap=redacted` when metadata sanitization already recorded a
  redaction gap.
- Preserve sanitized upstream request IDs when they are available, including
  response parse failure paths.

Acceptance:

- Provider failure trace events distinguish "the provider did not return an ID"
  from "no provider response source exists for this event."
- Existing upstream request ID extraction, routing, billing, provider dispatch,
  response payloads, API routes, and frontend behavior remain unchanged.
- No migration, exporter/outbox, read-only DB access, raw payload capture,
  X-Insight runtime dependency, or PRD4 matrix change is introduced.

### P5-F23: Snapshot Capture Gap Candidates

Requirements:

- Extend the explicit-request PRD5 X-Insight snapshot `capture_gaps` output with
  optional `candidate_capture_gaps`.
- Keep the primary `capture_gap` value factual and conservative; do not convert
  unknown missing rows into a single asserted cause.
- For missing `request_trace_events` rows where `call_logs` exists, include
  candidate causes such as `sampled_out`, `retention_expired`, and
  `not_instrumented`.
- For missing `call_logs` rows, keep `not_available_from_source` as the
  candidate because the selected request source cannot be found in the snapshot.
- Include generated capture-gap details in the optional Markdown handoff note so
  X-Insight can review gap candidates without parsing the JSON first.

Acceptance:

- Snapshot JSON remains explicit-request only and metadata-only.
- X-Insight gets structured candidate categories for missing trace rows without
  UniRouter inventing a trace event or a definitive cause.
- Existing snapshot safety behavior, DSN handling, request selection, runtime
  request handling, API routes, frontend behavior, exporter/outbox boundaries,
  and PRD4 status remain unchanged.

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
7. Batch 1G: trusted evidence context intake for internal
   trace/session/actor/delegation propagation.
8. Batch 1H: gateway span hierarchy for UniRouter-emitted request trace events.
9. Batch 1I: explicit payload policy decision evidence for chat and embeddings.
10. Batch 1J: provider upstream request ID capture gap evidence.
11. Batch 1K: Gate A snapshot Markdown handoff note for X-Insight discovery.
12. Batch 1L: component span refinement for repeated same-component trace
    events.
13. Batch 1M: provider failure capture gaps for provider-stage events that
    occur before any upstream response source exists.
14. Batch 1N: snapshot capture-gap candidates for missing trace rows in Gate A
    handoff artifacts.
15. Later payload policy metadata and evidence references, with future raw
   payload modes gated behind enterprise opt-in.
16. Later Gate A batch: read-only view/export decision after X-Insight Gate A
   evidence.
17. Later target/backlog batches: target upstream/SSO evidence or larger
   backlog items only after their prerequisites are explicitly met.

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
