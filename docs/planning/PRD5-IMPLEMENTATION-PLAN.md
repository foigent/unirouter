# PRD5 Implementation Plan

> Date: 2026-07-10
> Status: Batch 1A/1B/1C, Batch 1D, Batch 1E, Batch 1F, Batch 1G, Batch 1H,
> Batch 1I, Batch 1J, Batch 1K, Batch 1L, Batch 1M, Batch 1N, Batch 1O, and
> Batch 1P, Batch 1Q, Batch 1R, Batch 1S, Batch 1T, and Batch 1U
> Historical Canary/Sentinel evidence is complete through Batch 1V (`cc38094` / PR #125). Batch 1V
> target execution produced a sanitized
> snapshot with a denied-scenario correlation/evidence gap. X-Insight persistent
> import/readback is External integration verified at `4a562a6` / PR #9 and
> `5f0fa49` / PR #10. Batch 1W Pre-Relay Model Permission Evidence Parity is
> target verified under `UR-XI-GATE-A-20260710-02` on target
> `e823abc293f996264763c6c1aca19d3d4d6e705f`, containing `931400a` / PR
> #126. Keys 36/37 are disabled and the second X-Insight snapshot passed
> persistence, idempotent rerun, and readback. Batch 1X repository-wide audit,
> P5-F12 screening, Logs status-class filter, and frontend type-check gate
> hardening are Local verified in
> `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`. Batch 1Y
> accepted UniRouter-owned completion is Local verified in
> `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`.
> X-Insight ECS
> target verification and real upstream provider E2E remain pending.
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

Status: Canary/Sentinel complete (`92cb8ce` / PR #117).

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

### Batch 1O: Gate A Controlled Snapshot Handoff Prep

Risk: R2 because it updates PRD5 acceptance/status documents and shared-folder
coordination only. It does not change runtime request handling, routing,
billing, provider dispatch, response payloads, API routes, frontend behavior,
or DB schema.

Status: Canary/Sentinel complete. Canary PASS 8/8 is recorded in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by
`b2eea2e` / PR #118.

Implemented in this round:

- reconcile Batch 1N to Canary/Sentinel complete using `92cb8ce` / PR #117;
- add P5-F24 to the PRD5 scope and acceptance matrix;
- document the next Gate A controlled sanitized snapshot handoff prerequisites;
- add a shared-folder request that asks for approved request IDs, scenario
  labels, source instance naming, and X-Insight import/read confirmation;
- keep read-only DB/view, shared ECS staging, and exporter/outbox gated behind
  controlled snapshot import/read evidence and User approval.

Non-goals:

- no real snapshot artifact is generated;
- no DSN, credential, token, raw prompt, raw response, raw IP, or raw upstream
  error is written to the repo or shared folder;
- no new migration;
- no runtime request-path change;
- no new API route;
- no frontend behavior change;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no exporter/outbox;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1P: Model Permission Decision Evidence

Risk: R3 because it touches the auth-denial request path and gateway trace
evidence. Scope is restricted to the existing chat `allowed_models` rejection
path and does not change successful request handling, billing, routing, provider
dispatch, DB schema, API routes, or frontend behavior.

Status: Canary/Sentinel complete. Canary PASS 6/6 is recorded in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by
`61091c` / PR #119.

Implemented in this round:

- emit `auth.model_not_allowed` trace evidence before the existing chat
  `allowed_models` 403 response;
- write `capture_gap=permission_denied` for this runtime auth decision so later
  missing billing/routing/provider evidence has a factual denial reason;
- classify the event as `decision_kind=auth`, `decision_result=rejected`, and
  `decision_reason=auth_error.model_not_allowed`;
- preserve the existing behavior that model-permission denial returns before
  billing freeze, routing, or provider dispatch;
- add focused gateway tests for both direct trace insertion and the chat handler
  denial path;
- publish a shared-folder contract note for X-Insight explaining the import
  semantics and boundaries.

Non-goals:

- no DB migration;
- no new API route;
- no frontend behavior change;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no exporter/outbox;
- no raw prompt/response capture;
- no X-Insight runtime dependency;
- no Embeddings authorization behavior change;
- no `decision_not_emitted` backfill or trace-sampling override;
- no PRD4 matrix/status change.

### Batch 1Q: Gate A Request Approval Manifest

Risk: R2 because it changes PRD5 snapshot tooling and status/coordination docs
without changing runtime request handling, DB schema, API routes, frontend
behavior, credentials, or generated snapshot artifacts.

Status: Canary/Sentinel complete. Canary PASS 6/6 is recorded in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by
`ed8c69d` / PR #120.

Implemented in this round:

- add `--approval-manifest <path>` to `scripts/prd5_xinsight_snapshot.go`;
- define approval manifest schema
  `unirouter.prd5.xinsight.approval.v1`;
- allow manifest row statuses `approved`, `pending`, and `unavailable`;
- export only `approved` rows with concrete request IDs;
- reject placeholder approved IDs such as `TBD`, `n/a`, or `unavailable`;
- reject combining `--approval-manifest` with direct `--request-id`,
  `--request-ids`, or `--scenario` flags;
- use manifest `source_instance` unless `--source-instance` is explicitly set;
- mark snapshot safety selection mode as `approved_request_id_manifest` for
  manifest-based exports;
- answer X-Insight's approved request ID request in the shared folder without
  supplying fabricated IDs.

Non-goals:

- no real snapshot artifact is generated;
- no approved request IDs are invented or written by Codex;
- no DSN, credential, token, raw prompt, raw response, raw IP, or raw upstream
  error is written to the repo or shared folder;
- no DB migration;
- no runtime request-path change;
- no new API route;
- no frontend behavior change;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no exporter/outbox;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1R: Gate A Request ID Approval Runbook

Risk: R1 because it changes only PRD5 documentation and shared-folder
coordination files. It does not change runtime request handling, DB schema, API
routes, frontend behavior, credentials, or generated snapshot artifacts.

Status: Canary/Sentinel complete. Canary PASS 6/6 is recorded in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by
`c15cd7d` / PR #121.

Implemented in this round:

- add `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md` for non-technical approval
  explanation;
- explain that a `request_id` is a single request receipt and that approval
  authorizes only metadata-only sanitized evidence for that exact request;
- document the meanings of `approved`, `pending`, and `unavailable`;
- compare approval manifest, auto-discovery, read-only DB/view, and
  exporter/outbox tradeoffs;
- add a shared-folder worksheet:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`;
- reconcile Batch 1Q completion as `ed8c69d` / PR #120 and make Batch 1R the
  current Canary signal.

Non-goals:

- no real snapshot artifact is generated;
- no approved request IDs are invented or written by Codex;
- no DSN, credential, token, raw prompt, raw response, raw IP, or raw upstream
  error is written to the repo or shared folder;
- no DB migration;
- no runtime request-path change;
- no new API route;
- no frontend behavior change;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no exporter/outbox;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1S: X-Insight Data Exchange Control Surface

Risk: R1 because it changes only PRD5 product/architecture documentation and
shared-folder coordination. It does not change runtime request handling, DB
schema, API routes, frontend behavior, credentials, or generated snapshot
artifacts.

Status: Canary/Sentinel complete (`c6ae2a7` / PR #122).

Implemented in this round:

- add `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`;
- recommend a future UniRouter UI entry, but as a Data Exchange Control surface
  rather than a full X-Insight audit console;
- define UniRouter ownership as outbound approval, export ledger, handoff
  artifacts, import receipts, and X-Insight deep links;
- keep X-Insight ownership over importer internals, normalized evidence,
  read-side audit, investigation timeline/graph, policy simulation, and
  evidence packs;
- define staged implementation from current Markdown/manifest flow to later UI
  readiness design, minimal approval/export ledger UI, receipt reconciliation,
  and only later exporter/outbox;
- add a shared-folder review request asking X-Insight which receipt and deep
  link fields can be echoed back for UniRouter reconciliation.

Non-goals:

- no UI code;
- no DB migration;
- no API route;
- no runtime request-path change;
- no real snapshot artifact;
- no approved request IDs are invented or written by Codex;
- no DSN, credential, token, raw prompt, raw response, raw IP, or raw upstream
  error is written to the repo or shared folder;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no exporter/outbox;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1T: Gate A Exchange Envelope And Controlled Run Authorization

Risk: R2 overall because it combines R1 read-only snapshot tooling/tests with
PRD/status and human authorization contract changes. It does not change
UniRouter runtime request handling, DB schema, API routes, or frontend behavior.

Status: Canary/Sentinel complete (`fc62e65` / PR #123).

Implemented in this round:

- add source-created `exchange_id` to every generated Gate A snapshot and
  handoff note;
- add exact-byte `approval_manifest_sha256` for manifest-selected snapshots;
- keep artifact SHA-256 outside the snapshot so X-Insight computes it and
  reconciles `(exchange_id, artifact.sha256)`;
- preserve `--approval-manifest` in generated handoff commands;
- require manifest `source_instance` and reject conflicting CLI overrides;
- reject approval manifest files containing a second JSON value;
- add focused unit tests for exchange ID, manifest hash, parser, and handoff
  command behavior;
- record X-Insight's implemented `xinsight.import-receipt.v1` statuses and the
  fact that no durable `import_job_id` or deep link exists yet;
- add a controlled-run authorization runbook and shared worksheet covering
  scenarios, data classes, source instance, safety, retention, and operator;
- define a narrow `X-Request-ID` operator path for newly generated synthetic
  requests without broad request-ID discovery.

Non-goals:

- no real controlled request is sent;
- no real snapshot, manifest with approved IDs, or receipt artifact is created;
- no DSN, credential, token, API key, raw prompt, raw response, raw IP, or raw
  upstream error is written to the repo/shared folder;
- no UI, API route, DB migration, or runtime request-path change;
- no read-only DB/view credential handoff;
- no shared ECS staging;
- no exporter/outbox;
- no X-Insight runtime dependency;
- no PRD4 matrix/status change.

### Batch 1U: Embeddings Model Permission Parity

Risk: R3 because it closes an authorization bypass in the live Embeddings
request path. The slice is isolated from billing amounts, routing selection,
provider adapters, DB schema, API routes, frontend behavior, and X-Insight
runtime availability.

Status: Canary PASS 7/7; Sentinel merge reflected by `b2ba892` / PR #124.

Implemented in this round:

- extract the existing Chat `allowed_models` semantics into one shared gateway
  predicate;
- preserve unrestricted, wildcard, resolved-model, and requested-alias behavior;
- apply the same predicate to Embeddings immediately after model parsing;
- return `403 auth_error.model_not_allowed` before billing freeze, routing, or
  provider dispatch;
- return `X-Request-ID` on Embeddings responses for request correlation;
- emit the existing `auth.model_not_allowed` decision evidence with
  `capture_gap=permission_denied`, `decision_kind=auth`,
  `decision_result=rejected`, and the stable decision reason;
- add focused helper, Chat regression, Embeddings denial, pre-billing, response,
  and trace-field tests.

Non-goals:

- no API route or DB migration;
- no frontend behavior change;
- no billing/routing/provider success-path semantic change;
- no call-log fabrication for pre-admission denial;
- no raw prompt/response capture;
- no exporter/outbox, read-only DB/view, or shared ECS change;
- no X-Insight runtime dependency;
- no real Gate A controlled request or target-environment claim;
- no PRD4 matrix/status change.

### Batch 1V: Gate A Authorized Controlled Execution

Risk: R2 for authorization/status documentation and R4 for Atlas target
deployment, controlled request execution, sanitized target-data handling, and
X-Insight persistent import/readback evidence.

Status: Canary/Sentinel complete (`cc38094` / PR #125). Atlas executed both
scenarios against deployed application commit `b2ba892` and generated exchange
`uxchg_f7de31e541516893a0d9b31cc0f3892a`; success evidence is present, while
the denied scenario failed returned-ID correlation and has no source trace row.
X-Insight persistent import/readback is externally verified. Test-key disable
confirmation remains pending.

Authorized work:

- use the current UniRouter Alibaba Cloud ECS shared development/test
  environment only after Atlas confirms it is not production;
- deploy `b2ba892` or a later `origin/main` commit containing Batch 1P/1T/1U;
- run exactly `successful_non_stream` and `model_permission_denied` with
  synthetic non-sensitive content and discarded response bodies;
- let Atlas mark only the two verified returned request IDs approved;
- generate a manifest-selected sanitized snapshot and handoff note using a DSN
  supplied out of band;
- obtain X-Insight persistent import/readback and
  `xinsight.import-receipt.v1` evidence;
- delete the temporary snapshot seven days after receipt reconciliation.

Non-goals:

- no production traffic or automatic operator reassignment;
- no additional scenario or broad request discovery;
- no credentials, DSN, token, raw prompt/response, raw IP, raw upstream error,
  or broad query output in repo/shared files;
- no X-Insight read-only DB/view, shared cross-project ECS staging, or
  exporter/outbox;
- no PRD4 reopen or `Production verified` promotion.

Actual evidence boundary:

- artifact SHA-256 is
  `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`;
- counts are one call log, seven trace events, and two capture gaps;
- the denied request is retained only for diagnostic missing-source gap import;
- X-Insight receipt is `persisted`: 10 accepted, first PostgreSQL persistence
  inserted 10, the rerun produced 10 duplicate/no-op with 0 conflict, and
  readback returned 10 events/3 gaps;
- X-Insight merged the detector fix as `4a562a6` / PR #9 and closeout as
  `5f0fa49` / PR #10;
- receipt reconciliation date is 2026-07-10 and temporary snapshot deletion is
  due 2026-07-17;
- the snapshot and manifest remain immutable; Batch 1W does not rewrite target
  history.

### Batch 1W: Pre-Relay Model Permission Evidence Parity

Risk: R3 because it changes the live API-key model-denial ownership path while
preserving the existing denial result and pre-billing boundary.

Status: target verified under `UR-XI-GATE-A-20260710-02` on
`e823abc293f996264763c6c1aca19d3d4d6e705f`; second X-Insight snapshot
persistence/idempotency/readback verified.

Implemented in this round:

- remove the duplicate `apiKeyAllowsModel` rejection from
  `cmd/uniroute/gateway_handlers.go`;
- keep model-governance alias resolution and project policy in the wrapper;
- pass project-allowed requests to Relay so its shared Chat/Embeddings predicate
  owns API-key `allowed_models` rejection;
- add full handler integration coverage for HTTP 403, matching
  `X-Request-ID`, `auth.model_not_allowed` decision trace fields, zero call-log
  rows, and zero billing freezes;
- preserve the immutable Gate A snapshot as evidence of the pre-fix gap.

Non-goals:

- no change to project model-set/retired-model denial behavior;
- no route, migration, frontend, billing amount, routing success path, provider
  adapter, or raw payload change;
- no target rerun under the existing two-request authorization;
- no read-only X-Insight DB/view, shared staging, or exporter/outbox;
- no PRD4 change or production verification claim.

### Batch 1X: Repository Audit, P5-F12 Screening, Logs/CI Hygiene

Risk: R2 for docs/status/matrix synchronization and R1 for the scoped Logs/CI
fixes.

Status: Local verified in
`docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`.

Implemented in this round:

- repository-wide architecture, function, UI/UX, data, deployment, and security
  audit with confirmed findings separated from hypotheses;
- P5-F12 accept/defer/reject screening for Scheduler V2, plugins/marketplace,
  IAM rewrite, managed HA/IaC, infrastructure topology, hard budget blocking,
  runtime cache, SDK, roles/service accounts, and SLA operations;
- P0-P3 batch sequencing with INFRA-SEC-001 plan/evidence design only;
- Logs `2xx`/`4xx`/`5xx` status-class backend filtering to match existing UI;
- frontend `type-check` script and CI enforcement.

Non-goals:

- no ECS mutation, deployment, real upstream request, credential change, broad
  export, X-Insight runtime dependency, raw payload capture, or PRD4 reopen;
- no implementation of full Scheduler V2, runtime plugin platform, marketplace,
  IAM rewrite, full managed HA/IaC product, budget hard-blocking, runtime
  cache, full SDK suite, persistent service accounts, or customer-facing SLA
  product.

Verification:

- `go test ./internal/admin -count=1`;
- `go test ./... -count=1`;
- `go vet ./...`;
- `npm --prefix frontend run type-check`;
- `npm --prefix frontend test -- --run`;
- `npm --prefix frontend run build`;
- `git diff --check`.

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

- UniRouter has approved request IDs, scenario labels, and a non-secret source
  instance label for a controlled sanitized snapshot;
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
| UI | Lightweight request trace/debug view plus implemented Data Exchange Control for outbound approvals, export/delivery ledger, and receipt reconciliation | Operator investigation console, durable import-job/deep-link, execution detail, timeline, node detail, policy simulation, evidence packs, and read-side audit |
| Import/export | Snapshot, handoff notes, future views/export/outbox if needed | Importer, dedupe, source refs, read APIs, adapters |
| LiteLLM/New API/one-api | Compare for UniRouter feature catch-up | Public/open-source adapter strategy |

## Current Blocking Conditions

There is no repository-integration or denied-path target blocker for UniRouter
Batch 1A through Batch 1Y. Historical Canary/Sentinel evidence is complete
through Batch 1V, Batch 1W is target verified under
`UR-XI-GATE-A-20260710-02` on a target containing `931400a` / PR #126, and
Batch 1X and Batch 1Y are Local verified only.

### Batch 1Y: UniRouter-Owned Accepted Scope Completion

Status: Local verified in
`docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`.

Implemented:

- structural evidence exact/global log search and URL-persisted controls;
- route-attempt/failover summary from existing trace events;
- platform-admin Data Exchange Control migration/API/UI/audit for approval,
  request sealing, exchange/delivery/re-delivery/delete ledger, and receipt
  reconciliation;
- redacted current-process topology in Runtime Status;
- implementation-ready budget hard-block, service-account, and incident/SLO
  designs while preserving their deferred runtime status;
- consolidated X-Insight required-information request in the shared workspace.

Non-goals:

- no deployment, real request, target migration, credential, exporter/outbox,
  raw payload, broad export, X-Insight runtime dependency, ECS mutation, or PRD4
  reopen;
- no full Scheduler V2, plugins/marketplace, IAM rewrite, managed HA/IaC,
  budget hard-block runtime, plaintext cache, SDK suite, service-account runtime,
  or customer SLA product.

These later actions are gated:

- controlled request/snapshot execution: the original two-scenario run completed
  under `UR-XI-GATE-A-20260710-01`; the exact denied-path follow-up completed
  under `UR-XI-GATE-A-20260710-02`;
- X-Insight Gate A persistence/readback: externally verified for the immutable
  snapshot; this does not prove the missing UniRouter denial source event;
- temporary Gate A test API keys 36 and 37 are disabled;
- denied-scenario target closure is complete: the exact 403 correlation/trace
  passed with no call log, billing freeze, transaction, or provider event;
- read-only DB/view access: the import/persistence/readback prerequisite now
  passes across two controlled cycles, but access remains unapproved pending
  network, privilege, audit, retention, and failure-mode design;
- X-Insight ECS target verification remains unperformed; no shared ECS staging
  or X-Insight target-runtime claim follows from the local/PostgreSQL receipt;
- exporter/outbox: remains deferred pending an explicit architecture decision;
  two low-volume controlled cycles prove compatibility and idempotency but do
  not establish throughput, cursor, backpressure, or availability requirements;
- current ECS security exposure and insufficient shared-host capacity are
  tracked separately; X-Insight should not be added to that host;
- real upstream/SSO target E2E: blocked until approved target environments and
  credentials exist.
