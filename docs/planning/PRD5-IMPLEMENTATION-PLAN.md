# PRD5 Implementation Plan

> Date: 2026-07-09
> Status: Batch 1A/1B/1C, Batch 1D, Batch 1E, Batch 1F, Batch 1G, Batch 1H,
> Batch 1I, Batch 1J, Batch 1K, Batch 1L, and Batch 1M Canary/Sentinel
> complete. Batch 1N snapshot capture-gap candidates fixed awaiting Canary.
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

Status: Canary/Sentinel complete (`e1e76e3` / PR #106).

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

Status: Canary/Sentinel complete as part of `e1e76e3` / PR #106.

Implemented:

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

Status: Canary/Sentinel complete as part of `e1e76e3` / PR #106.

Implemented:

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

### Batch 1D: Upstream Request Correlation

Risk: R3 because it touches gateway evidence writes and call-log persistence, but
it does not change routing, billing, provider dispatch, response payloads, API
routes, or DB schema.

Status: Canary/Sentinel complete (`bf435ca` / PR #107).

Implemented:

- extract provider-returned upstream request IDs from a bounded response-header
  allowlist;
- reject empty, non-printable-ASCII, whitespace-containing, or
  control-character-containing values;
- write sanitized values to provider response trace events;
- write sanitized values to `call_logs.upstream_request_id` for request-level
  lookup and X-Insight import;
- keep missing provider IDs null rather than fabricating evidence.

Non-goals:

- no inbound public request-header trust;
- no new migration;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw payload capture;
- no frontend or API route change.

### Batch 1E: Event Capture Gap Emission

Risk: R3 because it touches gateway trace evidence writes, but it does not
change routing, billing, provider dispatch, response payloads, API routes,
frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`a19830f` / PR #108).

Implemented:

- preserve existing metadata allowlist behavior;
- detect when trace metadata is dropped because a key is restricted, a value
  shape is unsupported, or an allowed string is truncated;
- write `capture_gap='redacted'` on the affected `request_trace_events` row;
- keep `capture_gap` null when all safe metadata is retained;
- include `capture_gap` in the trace source-event hash.

Non-goals:

- no new migration;
- no new API route;
- no frontend behavior change;
- no raw prompt/response or nested metadata capture;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no fabricated session/delegation/business-chain facts;
- no PRD4 matrix/status change.

### Batch 1F: Payload Capture Mode Configuration

Risk: R3 because it changes runtime configuration validation and gateway
trace/log evidence writes, but it does not change routing, billing, provider
dispatch, response payloads, API routes, frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`e9670ad` / PR #109).

Implemented:

- add `PAYLOAD_CAPTURE_MODE` to config with default `metadata_only`;
- accept only `metadata_only` and `disabled` in the active implementation;
- reject unsupported values during config validation, including raw and planned
  future modes;
- wire the configured value into gateway relay evidence writes;
- persist the configured value in `request_trace_events.payload_capture_mode`
  and `call_logs.payload_capture_mode`;
- include the configured mode in trace and call-log source-event hashes;
- document `.env.example`, `docker-compose.yml`, and deployment runbook usage.

Non-goals:

- no raw prompt/response capture;
- no encrypted payload object store;
- no redacted/hash/reference active modes yet;
- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no fabricated session/delegation/business-chain facts;
- no PRD4 matrix/status change.

### Batch 1G: Trusted Evidence Context Intake

Risk: R3 because it changes gateway evidence write inputs and config validation,
but it does not change routing, billing, provider dispatch, response payloads,
API routes, frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`c701843` / PR #110).

Implemented:

- add `EVIDENCE_CONTEXT_TRUST_SECRET` config, default empty/disabled;
- reject configured trust secrets shorter than 16 bytes;
- accept inbound evidence context only when `X-UniRouter-Evidence-Auth` matches
  the configured shared secret;
- parse bounded printable non-whitespace `X-UniRouter-*` evidence headers for
  trace ID, parent span ID, session ID, actor type/ref, and `on_behalf_of`;
- parse trusted W3C `Traceparent` as fallback trace/parent-span source;
- write trusted context into existing `request_trace_events` and `call_logs`
  structural fields;
- include trusted context in source-event hashes;
- document `.env.example`, `docker-compose.yml`, and deployment runbook usage.

Non-goals:

- no public inbound request-header trust by default;
- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1H: Gateway Span Hierarchy

Risk: R3 because it changes gateway trace evidence write inputs and source-event
hashes, but it does not change routing, billing, provider dispatch, response
payloads, API routes, frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`61f5610` / PR #111).

Implemented:

- create a per-request in-memory span state on gateway request contexts;
- treat the first UniRouter-emitted trace event for the request as the gateway
  root span;
- preserve a trusted inbound `parent_span_id` as the gateway root span parent;
- parent later UniRouter-emitted trace events in that request to the gateway
  root span;
- preserve existing `trace_id` behavior from Batch 1G;
- include the resulting `parent_span_id` in trace source-event hashes.

Non-goals:

- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no public inbound request-header trust by default;
- no PRD4 matrix/status change.

### Batch 1I: Payload Policy Decision Evidence

Risk: R3 because it changes gateway trace evidence writes and decision metadata,
but it does not change routing, billing, provider dispatch, response payloads,
API routes, frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`1dc20b5` / PR #112).

Implemented in this round:

- emit `payload.policy.applied` trace events for chat and embeddings requests
  after gateway admission and before billing freeze;
- use `source_component=payload_policy`;
- retain sanitized scalar metadata for `payload_capture_mode`,
  `payload_policy_reason`, `raw_payload_stored`, and `payload_ref_present`;
- map metadata-only mode to `decision_kind=payload_capture` and
  `decision_result=allowed`;
- map disabled mode to `decision_kind=payload_capture` and
  `decision_result=skipped`;
- keep raw prompt/response capture disabled and uninspected.

Non-goals:

- no raw prompt/response capture;
- no encrypted payload object store;
- no redacted/hash/reference active modes yet;
- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1J: Provider Upstream Capture Gap

Risk: R3 because it changes gateway trace evidence writes and source-event
hashes, but it does not change routing, billing, provider dispatch, response
payloads, API routes, frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`f5223df` / PR #113).

Implemented in this round:

- when a provider response trace event has no sanitized `upstream_request_id`,
  write `capture_gap=provider_not_returned`;
- apply the gap only to `provider.response_accepted` and
  `provider.response_rejected`;
- preserve provider-returned upstream request IDs when present;
- preserve existing `capture_gap=redacted` priority when metadata sanitization
  already produced a redaction gap;
- keep request-level `call_logs.upstream_request_id` null when no provider ID is
  available.

Non-goals:

- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no public inbound request-header trust by default;
- no provider request/response semantic change;
- no PRD4 matrix/status change.

### Batch 1K: Gate A Snapshot Handoff Note

Risk: R2 because it changes PRD5 evidence tooling and status docs only. It does
not change runtime request handling, database schema, frontend behavior,
provider dispatch, billing, or API routes.

Status: Canary/Sentinel complete (`2db830a` / PR #114).

Implemented in this round:

- add `--handoff-note <path>` to `scripts/prd5_xinsight_snapshot.go`;
- write a Markdown note after snapshot JSON write succeeds;
- include generated date, source instance, schema version, selected request IDs,
  scenario labels, counts, safety confirmation, snapshot output path, and
  command shape without DSN;
- reuse `--overwrite` semantics for the note path;
- reject `--handoff-note -` so snapshot JSON and Markdown notes are not
  interleaved on stdout.

Non-goals:

- no real snapshot artifact is generated without approved request IDs and a
  read-only DSN;
- no DSN, credential, token, raw prompt, raw response, raw IP, or raw upstream
  error is written to the note;
- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1L: Component Span Refinement

Risk: R3 because it changes gateway trace evidence parent-span values and
source-event hashes, but it does not change routing, billing, provider
dispatch, response payloads, API routes, frontend behavior, or DB schema.

Status: Canary/Sentinel complete (`ffebf83` / PR #115).

Implemented in this round:

- keep the first UniRouter trace event in a request as the gateway root span;
- preserve a trusted inbound parent only on that gateway root span;
- track the first trace event for each `source_component` in the request;
- parent later trace events from the same `source_component` to that component's
  first span;
- keep first events for new components parented to the gateway root.

Non-goals:

- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no cross-system agent/tool/business graph inference;
- no PRD4 matrix/status change.

### Batch 1M: Provider Failure Capture Gaps

Risk: R3 because it changes gateway trace evidence `capture_gap` values and
source-event hashes for provider failure events, but it does not change routing,
billing, provider dispatch, response payloads, API routes, frontend behavior, or
DB schema.

Status: Canary/Sentinel complete (`4a4e254` / PR #116).

Implemented in this round:

- add `capture_gap=not_available_from_source` for provider failure trace events
  that happen before an upstream provider response source exists;
- keep `capture_gap=provider_not_returned` for provider response accepted or
  rejected events when a response exists but no usable upstream request ID is
  returned;
- preserve `capture_gap=redacted` priority when metadata sanitization already
  recorded a redaction gap;
- pass available embeddings upstream request IDs into
  `provider.response_parse_failed` trace evidence so parse failures do not
  falsely look source-unavailable when the provider returned an ID;
- add focused gateway coverage for provider failure gaps and redaction priority;
- publish a shared X-Insight contract for the provider failure gap semantics.

Non-goals:

- no new migration;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no provider dispatch semantic change;
- no PRD4 matrix/status change.

### Batch 1N: Snapshot Capture Gap Candidates

Risk: R2 because it changes explicit-request Gate A snapshot JSON/handoff-note
tooling only. It does not change runtime request handling, routing, billing,
provider dispatch, response payloads, API routes, frontend behavior, or DB
schema.

Status: fixed awaiting Canary.

Implemented in this round:

- add optional `candidate_capture_gaps` to generated snapshot `capture_gaps`;
- keep the primary generated missing-row `capture_gap` conservative as
  `not_available_from_source`;
- for missing `request_trace_events` rows where a `call_logs` row exists, include
  `sampled_out`, `retention_expired`, and `not_instrumented` as candidate
  capture-gap categories;
- for missing `call_logs` rows, keep `not_available_from_source` as the only
  candidate gap;
- add a capture-gap table to generated Markdown handoff notes;
- publish a shared X-Insight contract for interpreting these candidates.

Non-goals:

- no new migration;
- no runtime request-path change;
- no new API route;
- no frontend behavior change;
- no exporter/outbox;
- no read-only DB/view credential handoff;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no definitive sampling/retention cause assertion;
- no PRD4 matrix/status change.

### Later Payload Policy Modes

Risk: R3 if schema/API changes; R4 only if raw payload capture is introduced,
which is not planned by default.

Already implemented in Batch 1A/1B/1C:

- keep raw prompt/response off;
- add metadata-only payload state fields;
- expose metadata-only payload mode in trace API, snapshot output, and the
  lightweight trace UI;

Planned later:

- support redacted/hash/reference modes;
- document enterprise opt-in requirements for raw payload capture.

### Later Gate A Batch: X-Insight Gate A Feedback Incorporation

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

### Later Target Evidence Batch: Real Target Evidence

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

There is no blocking condition for UniRouter Batch 1A through Batch 1N.

These later actions are gated:

- read-only DB/view access: blocked until X-Insight local snapshot import passes
  and User approves credential handoff;
- shared ECS staging: blocked until local Gate A import works and CI is green;
- exporter/outbox: blocked until Gate A shows concrete field/volume/cursor gaps;
- real upstream/SSO target E2E: blocked until approved target environments and
  credentials exist.
