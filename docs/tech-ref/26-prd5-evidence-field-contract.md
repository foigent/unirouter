# PRD5 Evidence Field Contract

> Version: v0.1
> Date: 2026-07-09
> Status: Batch 1A/1B/1C, Batch 1D, Batch 1E, Batch 1F, Batch 1G, Batch 1H,
> Batch 1I, Batch 1J, Batch 1K, Batch 1L, and Batch 1M Canary/Sentinel
> complete. Batch 1N snapshot capture-gap candidates fixed awaiting Canary.
> Related: `docs/PRD-Phase5.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`,
> X-Insight response
> `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-08-x-insight-prd5-integration-answers.md`

## Purpose

This contract defines the structural fields UniRouter should add before a native
exporter/outbox.

These fields decide whether UniRouter evidence can evolve from "single model
request logs" to "multi-agent, multi-tool, multi-business-system execution-chain
evidence." They are not cosmetic UI fields.

## Field Dictionary

| Field | Type | First Target | Meaning |
|-------|------|--------------|---------|
| `trace_id` | nullable string | `request_trace_events`; maybe `call_logs` for query | Execution trace root. Preserve trusted upstream value or generate gateway root when missing |
| `span_id` | nullable string | `request_trace_events` | UniRouter-emitted span/event identifier |
| `parent_span_id` | nullable string | `request_trace_events` | Parent span from trusted upstream or previous UniRouter stage |
| `session_id` | nullable string | trace/log where provided | Business/app session. UniRouter should not invent business sessions |
| `upstream_request_id` | nullable string | `call_logs`; provider trace events | Provider request ID when returned by upstream |
| `schema_version` | string | trace events | UniRouter evidence schema, initially `unirouter.evidence.v1` |
| `source_component` | string | trace events | UniRouter component emitting the evidence |
| `capture_mode` | string | trace/log/export metadata | How the evidence was captured |
| `capture_gap` | nullable string or gap records | trace/export/import | Why evidence is missing, redacted, sampled, or inferred |
| `candidate_capture_gaps` | array of strings | snapshot/import gap records | Possible gap categories when UniRouter cannot assert one definitive cause |
| `evidence_ref` | nullable string | trace/log/export metadata | Stable source or evidence object reference, not a payload dump |
| `source_event_hash` | nullable string | trace/log/export metadata | Hash of sanitized source event for dedupe/integrity |
| `decision_kind` | nullable string | decision trace events | Type of gateway decision |
| `decision_result` | nullable string | decision trace events | Result of the decision |
| `decision_reason` | nullable string | decision trace events | Stable reason code; do not include raw payload content |
| `actor_type` | nullable string | trace/log where known | Human, service, agent, API key, system, or unknown |
| `actor_ref` | nullable string | trace/log where known | Stable non-secret actor reference |
| `on_behalf_of` | nullable string | trace/log where known | Delegation target for agent/app/service use cases |
| `payload_capture_mode` | string | trace/log/export metadata | Payload state. Active Batch 1F values are `metadata_only` and `disabled`; redacted/hash/reference modes remain later gated vocabulary |
| `payload_policy_reason` | string metadata | payload policy trace events | Stable payload policy reason code such as `payload.metadata_only` or `payload.disabled`; never raw payload content |
| `payload_ref_present` | boolean metadata | payload policy trace events | Whether an approved external payload reference is present; Batch 1I always emits `false` |
| `raw_payload_stored` | boolean metadata | payload policy trace events | Whether UniRouter stored raw prompt/response payload content; Batch 1I always emits `false` |
| `payload_ref` | nullable string | future payload policy | Reference to an approved payload object, not raw content |

## Recommended Enum Values

### `source_component`

- `gateway`
- `auth`
- `quota`
- `budget`
- `routing`
- `provider`
- `billing`
- `logging`
- `trace`
- `payload_policy`
- `admin`
- `unknown`

### `capture_mode`

- `native`
- `callback`
- `db_import`
- `api_import`
- `otel`
- `openinference`
- `sdk`
- `mcp`
- `derived`
- `manual`

### `capture_gap`

Do not use `none` as a gap value. If there is no gap, omit the gap or mark the
claim as direct/not applicable at the evidence layer.

Recommended values:

- `not_instrumented`
- `not_available_from_source`
- `suppressed_by_payload_policy`
- `redacted`
- `sampled_out`
- `permission_denied`
- `retention_expired`
- `provider_not_returned`
- `inferred_only`
- `business_outcome_unknown`
- `tool_side_effect_unknown`
- `identity_unknown`
- `decision_not_emitted`

### `decision_kind`

- `admission`
- `auth`
- `quota`
- `budget`
- `routing`
- `provider`
- `billing`
- `payload_capture`
- `trace_sampling`
- `retry`
- `failover`
- `policy`
- `unknown`

### `decision_result`

- `allowed`
- `rejected`
- `selected`
- `failed`
- `skipped`
- `simulated_match`
- `simulated_no_match`
- `unknown`

### `actor_type`

- `human`
- `service`
- `agent`
- `api_key`
- `system`
- `unknown`

## Trust Boundary

UniRouter may accept external correlation fields only from trusted/internal
callers or from explicitly configured headers/metadata.

Default behavior for new requests:

- if trusted `trace_id` exists, preserve it;
- otherwise generate a gateway root `trace_id`;
- generate UniRouter-owned `span_id` values for gateway evidence;
- use trusted `parent_span_id` when present;
- keep `session_id` null unless supplied by the app/business system;
- capture `upstream_request_id` only when the provider returns one or the adapter
  can safely derive it.

Do not trust arbitrary public request headers as evidence-chain authority by
default.

Batch 1D response-header allowlist:

- `X-Request-Id`
- `X-Request-ID`
- `Request-Id`
- `Request-ID`
- `OpenAI-Request-ID`
- `X-OpenAI-Request-ID`
- `Anthropic-Request-Id`
- `X-Anthropic-Request-Id`
- `Apim-Request-Id`
- `X-MS-Request-ID`

Values are trimmed, must be printable ASCII, must not contain whitespace or
control characters, and are bounded to 128 bytes before being written as
`upstream_request_id`.

Batch 1G trusted evidence context intake:

- Disabled by default when `EVIDENCE_CONTEXT_TRUST_SECRET` is empty.
- Requires `X-UniRouter-Evidence-Auth` to match the configured secret before
  reading any inbound evidence context.
- Accepts `X-UniRouter-Trace-Id`, `X-UniRouter-Parent-Span-Id`,
  `X-UniRouter-Session-Id`, `X-UniRouter-Actor-Type`,
  `X-UniRouter-Actor-Ref`, and `X-UniRouter-On-Behalf-Of`.
- Supports trusted W3C `Traceparent` as a fallback source for `trace_id` and
  `parent_span_id`.
- Drops empty, overlong, whitespace-containing, control-character-containing, or
  unsupported enum values rather than fabricating evidence.
- Does not trust arbitrary public request headers by default.

Batch 1H gateway span hierarchy:

- Gateway request contexts carry an in-memory UniRouter span state.
- The first UniRouter-emitted trace event for a request becomes the gateway root
  span.
- If a trusted inbound `parent_span_id` exists, it is stored as the parent of
  that gateway root span.
- Later UniRouter-emitted trace events in the same request use the gateway root
  span as their `parent_span_id`.
- This creates a queryable UniRouter-local request hierarchy without claiming a
  full cross-system agent graph.

Batch 1L component span refinement:

- The first UniRouter trace event remains the request gateway root span.
- The first trace event for each `source_component` becomes that component's
  local span root.
- Later trace events from the same `source_component` parent to that component
  span root.
- First events for new components still parent to the request gateway root.
- This is a request-local UniRouter evidence hierarchy only; it does not infer
  external agent, tool, or business-system spans.

Batch 1M provider failure capture gaps:

- Provider response trace events keep the Batch 1J rule:
  `capture_gap=provider_not_returned` means an upstream provider response exists
  but did not expose a usable request ID.
- Provider-stage failure events that happen before an upstream response source
  exists use `capture_gap=not_available_from_source` when no sanitized
  `upstream_request_id` can exist.
- The initial provider failure set is `provider.key_decrypt_failed`,
  `provider.adapter_unknown`, `provider.request_build_failed`,
  `provider.request_failed`, and `provider.response_parse_failed`.
- If metadata sanitization already recorded `capture_gap=redacted`, that
  redaction gap takes priority.
- If a sanitized upstream request ID is available, UniRouter writes it and does
  not add a missing-source gap.

Batch 1N snapshot capture-gap candidates:

- Gate A snapshot-generated gap records can include
  `candidate_capture_gaps`.
- Candidate gaps are used when the primary missing-row `capture_gap` must remain
  conservative but X-Insight needs structured possible causes.
- Missing trace rows with an existing call log use candidates:
  `sampled_out`, `retention_expired`, and `not_instrumented`.
- Missing call-log rows use `not_available_from_source`.
- This is a snapshot/import aid only. It does not add a new
  `request_trace_events` or `call_logs` column and does not fabricate runtime
  trace events.

Batch 1I payload policy decision evidence:

- Chat and embeddings requests emit `payload.policy.applied` after
  `gateway.request.accepted`.
- The event uses `source_component=payload_policy`.
- The event maps to `decision_kind=payload_capture`.
- `metadata_only` maps to `decision_result=allowed` and
  `decision_reason=payload.metadata_only`.
- `disabled` maps to `decision_result=skipped` and
  `decision_reason=payload.disabled`.
- Metadata is restricted to sanitized scalar policy state:
  `payload_capture_mode`, `payload_policy_reason`,
  `payload_ref_present=false`, and `raw_payload_stored=false`.
- Batch 1I does not inspect, persist, hash, export, or reference raw
  prompt/response bodies.

## Migration Strategy

Batch 1B adds nullable/default fields first.

Recommended first migration target:

- `request_trace_events`: all trace/span/source/capture/decision/actor/evidence
  fields;
- `call_logs`: `trace_id`, `session_id`, `upstream_request_id`,
  `schema_version`, `capture_mode`, `evidence_ref`, `source_event_hash`,
  actor fields if query performance or X-Insight import needs them.

Do not backfill historical rows with fabricated facts.

For historical rows:

- keep nulls where UniRouter did not record the fact;
- let X-Insight record capture gaps during import;
- optionally derive `source_event_hash` from sanitized historical rows in a
  controlled backfill only if the User approves it.

Implemented first slice:

- `migrations/000018_prd5_structural_evidence_fields.up.sql` adds structural
  fields to `request_trace_events` and `call_logs`.
- Gateway trace writes populate `trace_id`, `span_id`, `schema_version`,
  `source_component`, `capture_mode`, `evidence_ref`, `decision_*`,
  `actor_*`, `payload_capture_mode`, and `source_event_hash` where UniRouter
  knows the fact directly.
- Gateway call-log writes populate `trace_id`, `schema_version`, `capture_mode`,
  `evidence_ref`, `actor_*`, `payload_capture_mode`, and
  `source_event_hash`.
- Existing trace API read-side returns these fields through the current RBAC and
  metadata-redaction path.
- Batch 1D adds provider response-header extraction for `upstream_request_id`.
  The value is written to provider response trace events and request-level call
  logs when present. Missing provider IDs remain null.
- Batch 1E adds event-level `capture_gap='redacted'` when UniRouter's trace
  metadata sanitizer drops restricted keys, rejects unsupported value shapes, or
  truncates allowed strings. Fully retained safe metadata leaves `capture_gap`
  null.
- Batch 1F adds `PAYLOAD_CAPTURE_MODE` with active values `metadata_only` and
  `disabled`. Gateway trace/log writes persist the configured mode and include
  it in source-event hashes. Unsupported values fail config validation.
- Batch 1G adds default-off trusted evidence context intake. When the shared
  secret matches, gateway trace/log writes can populate `trace_id`,
  `parent_span_id`, `session_id`, `actor_*`, and `on_behalf_of` from sanitized
  internal context headers and include those fields in source-event hashes.
- Batch 1H adds per-request gateway root span parenting for UniRouter-emitted
  trace events and includes the resulting `parent_span_id` in trace
  source-event hashes.
- Batch 1I adds explicit `payload.policy.applied` trace events for chat and
  embeddings. These events write sanitized payload policy metadata and
  `decision_kind=payload_capture` while keeping raw payload storage disabled.
- Batch 1J adds `capture_gap=provider_not_returned` to provider response trace
  events when no sanitized upstream request ID is available. Returned upstream
  request IDs still populate `upstream_request_id`; existing redaction gaps keep
  `capture_gap=redacted`.
- Batch 1L refines `parent_span_id` for repeated same-component events:
  repeated events from a component parent to that component's first span, while
  first component events still parent to the request gateway root.
- Batch 1M adds `capture_gap=not_available_from_source` for provider-stage
  failure events that happen before any upstream response source exists.
  Provider response accepted/rejected events keep the Batch 1J
  `provider_not_returned` rule, and redaction gaps keep priority.
- Batch 1N adds snapshot/import `candidate_capture_gaps` for generated
  missing-row gap records so X-Insight can review possible sampling, retention,
  or non-instrumented causes without UniRouter asserting one definitive cause.

## Writer Behavior

Gateway evidence writers should populate fields where UniRouter knows facts
directly:

- gateway received/auth/admission decisions: `gateway` or `auth` component;
- quota/budget decisions: `quota` / `budget` plus decision fields;
- routing/provider selection: `routing` / `provider` plus selected/failed reason;
- provider response evidence: include sanitized provider-returned
  `upstream_request_id` when available, otherwise mark provider response trace
  events with `capture_gap=provider_not_returned`;
- provider pre-response failure evidence: when key decryption, adapter lookup,
  request build, outbound request, or response parse failure events have no
  sanitized upstream request ID because the source is unavailable, mark
  `capture_gap=not_available_from_source`;
- trusted caller context: include `trace_id`, `parent_span_id`, `session_id`,
  `actor_*`, and `on_behalf_of` only when the configured evidence-context shared
  secret matches;
- gateway span hierarchy: parent the first UniRouter trace event to a trusted
  inbound parent when present, then parent first component events to the gateway
  root span and repeated same-component events to that component's first span;
- metadata safety evidence: include `capture_gap='redacted'` when metadata was
  intentionally dropped or truncated by UniRouter;
- payload policy decision evidence: emit `payload.policy.applied` with
  `source_component=payload_policy`, `decision_kind=payload_capture`,
  a stable decision result/reason, `raw_payload_stored=false`, and
  `payload_ref_present=false`;
- billing freeze/settle/cancel: `billing` plus decision/result where applicable;
- trace sampling: `trace_sampling` decision;
- payload state: `payload_capture` decision and configured
  `payload_capture_mode`.

Missing upstream facts should not be guessed.

## Payload Boundary

Default mode is `metadata_only`.

Active runtime modes in Batch 1F and Batch 1I:

- `metadata_only`
- `disabled`

Planned but not active without later approval:

- `redacted`
- `hash_only`
- `reference_only`

Raw prompt/response capture requires a separate explicit enterprise opt-in
decision covering encryption/key management, retention, deletion/export, RBAC,
audit-on-read, legal hold, and redaction.

Batch 1I only records the policy decision. It deliberately sets
`raw_payload_stored=false` and `payload_ref_present=false` for every emitted
payload policy event. Redacted summaries, payload hashes, object-store
references, raw prompt/response storage, exporter/outbox transport, and
X-Insight importer behavior remain later gated work.

## Read-Side Behavior

Admin trace APIs and future exports must:

- preserve project/org RBAC;
- apply metadata allowlisting/redaction;
- not expose raw API keys, upstream credentials, raw prompts, raw responses, raw
  IdP tokens, raw IPs, or raw upstream errors;
- show missing data as null or capture gaps.

## Exporter/Outbox Timing

Do not implement exporter/outbox before Batch 1B and X-Insight Gate A feedback.

Exporter/outbox design must reference:

- confirmed structural fields;
- X-Insight importer findings;
- cursor and dedupe requirements;
- retry/backpressure behavior;
- retention and redaction rules;
- failure isolation from the UniRouter request path.
