# OTel/OpenInference Metadata Export

> Contract: `unirouter.telemetry-export.v1`
> Scope: PRD7 B3 / P7-F05
> Runtime default: disabled

## 1. Boundary

UniRouter remains the Source Domain Owner. The exporter is an optional observer
of the existing sanitized request-trace fact after that fact has entered the
transactional Outbox. It does not create source truth and it does not alter:

- request IDs, request records, source-event hashes or Outbox event keys;
- billing hold, settlement, cancellation or finality;
- completed-execution Feed endpoint, DTO, credential, page or cursor behavior;
- router, IAM, guardrail, provider execution or customer response behavior.

The exporter is not the Foigent Connector SDK, a runtime plugin process, a
cursor/checkpoint producer, or a dependency of the request critical path.

The synchronous legacy trace-insert fallback does not export because its
database-generated `occurred_at` is not available as an exact Source snapshot
at enqueue time. The normal server wiring always supplies the transactional
Outbox; this fail-closed exception prevents an approximate timestamp from being
presented as exact source provenance.

## 2. Delivery Architecture

`internal/telemetryexport` owns one fixed-size in-memory queue and one worker:

1. the gateway sanitizes metadata and constructs the unchanged Source event
   hash and Outbox payload;
2. successful Outbox enqueue is the source durability boundary;
3. `TryExport` validates and projects a metadata-only snapshot, then performs a
   non-blocking channel send;
4. the worker creates deterministic OTLP/HTTP JSON batches and sends them to
   the configured trace endpoint;
5. shutdown stops new queue entries and attempts a bounded drain under the
   existing server shutdown deadline.

Queue full, invalid export metadata, collector error and shutdown timeout are
export observations only. They never change the source request or Outbox
result. B3 has no disk spool and makes no exactly-once delivery claim.
An Outbox `ON CONFLICT` duplicate does not create a second export span: only a
newly inserted source event is eligible for `TryExport`.

## 3. Source Mapping

| UniRouter Source fact | OTLP/OpenInference projection | Rule |
|---|---|---|
| `trace_id` | `traceId` | valid non-zero 16-byte hex/UUID is preserved; other source IDs are deterministically SHA-256-derived to 16 bytes |
| `span_id` / `parent_span_id` | `spanId` / `parentSpanId` | valid non-zero 8-byte hex is preserved; otherwise deterministic SHA-256 derivation |
| `event_type` | span `name` and `unirouter.event.type` | fixed `unirouter.` prefix; source value is not rewritten |
| `occurred_at`, `latency_ms` | `endTimeUnixNano`, `startTimeUnixNano` | exact Source Outbox timestamp and bounded non-negative latency |
| request entry kind | `openinference.span.kind` | an observed provider request/response step is `LLM` for Chat and `EMBEDDING` for Embeddings; pre-dispatch provider failures and other control steps are `CHAIN`; guardrail is `GUARDRAIL` |
| provider/routed model | OpenInference model attributes plus `unirouter.provider` | only the route-derived `mapped_model` is eligible; client-requested model text is not exported; values must be portable and at most 128 bytes |
| status/severity | OTLP status and bounded attributes | Source `error` or HTTP 5xx maps to OTLP error |
| token counters | OpenInference/custom integer attributes | explicit allowlist; exact non-negative int64 only; OTLP JSON encodes int64 as decimal strings |
| `capture_mode`, `capture_gap`, `payload_capture_mode`, `source_event_hash` | `unirouter.source.*` / `unirouter.payload.*` | copied without changing Source hash semantics |
| organization/project | `unirouter.scope.*` | numeric tenant scope only; no names |
| request ID | `unirouter.request.ref_sha256` | org/project-scoped SHA-256 reference; raw request ID is not an attribute |

The OTLP resource is `service.namespace=foigent`,
`service.name=unirouter`, with optional runtime version/environment. OTLP span
kind remains `SPAN_KIND_INTERNAL`; OpenInference operation kind is the required
`openinference.span.kind` attribute.

## 4. Privacy And Cardinality

The export model has no fields for prompt/response bodies, messages, input or
output values, tool arguments, credentials, actor references, channel names,
upstream request IDs, sessions, workflows, Agent IDs, evidence refs, cursors or
checkpoints.

Defense in depth rejects restricted keys recursively before queueing, even
though the existing Source sanitizer already removes them. Unknown metadata is
dropped rather than dynamically converted into attributes. Attribute count is
bounded to 32, string/token values are bounded, and a one-MiB OTLP request cap
is enforced. Every span declares `raw_content_policy_excluded`; additional
export gaps include non-allowlisted metadata, invalid types, non-portable
values, count overflow and missing Source hash.

No value is truncated into a different semantic fact. Unsafe values are
omitted with an explicit export gap.

## 5. Retry And Acknowledgement

- network errors and HTTP `429`, `502`, `503`, `504` are retryable;
- retry count, per-attempt timeout and exponential delay are bounded;
- `Retry-After` is honored when longer than local backoff but capped at five
  seconds;
- other HTTP failures and invalid responses are not retried;
- OTLP HTTP `200` with `partialSuccess` is an acknowledgement: rejected spans
  are counted and the batch is not retried;
- retries reuse byte-identical JSON payloads.

This follows OTLP/HTTP transport and partial-success semantics. It does not
interpret one collector acknowledgement as end-to-end downstream persistence.

## 6. Configuration And Non-Claims

Export is enabled only by `UNIROUTE_OTEL_EXPORT_ENABLED=true` together with
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. The endpoint permits http/https and
forbids URL credentials, query strings and fragments. When given only an
origin, `/v1/traces` is appended.

B3 intentionally has no custom authorization headers or credential lookup.
That wiring depends on B4 secret references. No live collector, credentials,
deployment, traffic, database, cursor/checkpoint or external integration was
used to verify this local implementation.

## 7. Primary Specifications

- OTLP transport and JSON rules:
  <https://opentelemetry.io/docs/specs/otlp/>
- OpenInference semantic conventions:
  <https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md>
- OpenInference trace model:
  <https://github.com/Arize-ai/openinference/blob/main/spec/traces.md>
