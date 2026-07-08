# PRD5 Evidence Field Contract

> Version: v0.1
> Date: 2026-07-08
> Status: Batch 1A/1B/1C and Batch 1D Canary/Sentinel complete. Batch 1E
> event capture-gap emission fixed awaiting Canary.
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
| `evidence_ref` | nullable string | trace/log/export metadata | Stable source or evidence object reference, not a payload dump |
| `source_event_hash` | nullable string | trace/log/export metadata | Hash of sanitized source event for dedupe/integrity |
| `decision_kind` | nullable string | decision trace events | Type of gateway decision |
| `decision_result` | nullable string | decision trace events | Result of the decision |
| `decision_reason` | nullable string | decision trace events | Stable reason code; do not include raw payload content |
| `actor_type` | nullable string | trace/log where known | Human, service, agent, API key, system, or unknown |
| `actor_ref` | nullable string | trace/log where known | Stable non-secret actor reference |
| `on_behalf_of` | nullable string | trace/log where known | Delegation target for agent/app/service use cases |
| `payload_capture_mode` | string | trace/log/export metadata | Payload state such as metadata-only, redacted, hash-only, reference-only |
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

## Writer Behavior

Gateway evidence writers should populate fields where UniRouter knows facts
directly:

- gateway received/auth/admission decisions: `gateway` or `auth` component;
- quota/budget decisions: `quota` / `budget` plus decision fields;
- routing/provider selection: `routing` / `provider` plus selected/failed reason;
- provider response evidence: include sanitized provider-returned
  `upstream_request_id` when available;
- metadata safety evidence: include `capture_gap='redacted'` when metadata was
  intentionally dropped or truncated by UniRouter;
- billing freeze/settle/cancel: `billing` plus decision/result where applicable;
- trace sampling: `trace_sampling` decision;
- payload state: `payload_capture` decision and `payload_capture_mode`.

Missing upstream facts should not be guessed.

## Payload Boundary

Default mode is metadata-only.

Allowed first modes:

- `metadata_only`
- `redacted`
- `hash_only`
- `reference_only`
- `disabled`

Raw prompt/response capture requires a separate explicit enterprise opt-in
decision covering encryption/key management, retention, deletion/export, RBAC,
audit-on-read, legal hold, and redaction.

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
