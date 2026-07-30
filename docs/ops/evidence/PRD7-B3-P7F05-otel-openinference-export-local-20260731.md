# PRD7 B3 OTel/OpenInference Metadata Export — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B3-OTEL-OPENINFERENCE-METADATA-EXPORT`
> Requirement: `P7-F05`
> Status: Local verified
> Start head: `742e3b21d7743e5c92492f2011e1805fd92bf744`
> Start tree: `71b002bca2b628a4a33730334f9fed6c4dc1a3a6`
> Branch: `codex/product-iteration-20260730`

## Delivered result

- `internal/telemetryexport` implements
  `unirouter.telemetry-export.v1` as a zero-SDK, OTLP/HTTP JSON producer with
  OpenInference semantic attributes.
- Export is default off. Enabling it requires the existing trace source,
  an explicit collector endpoint and bounded queue/batch/flush/timeout/retry
  settings.
- The request path performs no network I/O: a newly inserted transactional
  Outbox trace event is projected and offered to a fixed in-memory queue with a
  non-blocking send. Queue full, invalid metadata and collector failure are
  fail-open export observations only.
- Outbox duplicates do not create a second export span. The synchronous legacy
  trace-insert fallback is not exported because its database-generated
  `occurred_at` is not available as an exact Source snapshot.
- Observed Chat provider request/response steps map to OpenInference `LLM`,
  observed Embeddings provider steps to `EMBEDDING`, guardrails to `GUARDRAIL`,
  and pre-dispatch provider failures or other control steps to `CHAIN`. This
  derives operation kind from endpoint context and Source event coverage rather
  than guessing from a model name or claiming a call that did not occur.
- Client-requested model text is not exported. Only the route-derived
  `mapped_model` can populate a bounded OpenInference model attribute.
- Valid OTel trace/span IDs are preserved. Other UniRouter IDs are
  deterministically SHA-256-derived into valid OTel widths; the raw request ID
  is replaced by an org/project-scoped SHA-256 reference attribute.
- The exact retry allowlist is network errors plus HTTP
  `429/502/503/504`. A `200 partialSuccess` is acknowledged and never retried.
  Bounded `Retry-After` is honored and retries reuse byte-identical request
  bytes.

The complete mapping and boundary are
`docs/tech-ref/32-otel-openinference-metadata-export.md`.

## Privacy and cardinality evidence

The export DTO has no prompt, response, message, input/output, tool argument,
credential, actor reference, channel name, upstream request ID,
session/workflow/Agent ID, cursor or checkpoint field. Restricted metadata
keys are recursively rejected before queueing even after the existing Source
sanitizer. Unknown metadata is dropped rather than converted into dynamic
attributes.

Each span is capped at 32 attributes; portable token/string values are bounded;
token counters must be exact non-negative int64 values; metadata shape is
bounded; and each OTLP request is capped at one MiB. Every span declares the
`raw_content_policy_excluded` gap. Additional drops produce explicit
export-side gaps rather than truncating a value into a different fact.

Deterministic OTLP golden:

- path:
  `internal/telemetryexport/testdata/chat-provider-otlp.json`
- SHA-256:
  `25cead642e4e9a59f47e514580b5a69e889d8f78022117f2c32dd4e8fd6efe56`

## Verification

| Command/check | Actual result |
|---|---|
| `GOCACHE=/tmp/unirouter-go-cache go test ./internal/telemetryexport ./internal/config` | PASS — 13 focused exporter/OTLP/privacy/cardinality/exact-int64 cases plus config defaults/negative coverage |
| focused Gateway Outbox/export tests | PASS — new Outbox event exports; duplicate and timestamp-inexact fallback do not; exporter failure does not alter source persistence |
| `GOCACHE=/tmp/unirouter-go-cache go test -race ./internal/telemetryexport` | PASS |
| `GOCACHE=/tmp/unirouter-go-cache go test ./...` | PASS — all Go packages, including existing Trace/Outbox/billing/provider/feed regressions |
| `GOCACHE=/tmp/unirouter-go-cache go vet ./...` | PASS |
| `npm test -- --run` in `frontend/` | PASS — 42 files / 308 tests |
| `npm run type-check` in `frontend/` | PASS |
| `npm run build` in `frontend/` | PASS — TypeScript plus Vite production build; existing large-chunk warning remains |
| `ruby -e ... YAML.safe_load ...` against `docker-compose.yml` | PASS — exporter environment wiring parsed |
| `docker compose config --quiet` | NOT RUN — Docker CLI is not installed in this workspace |
| `git diff --check` | recorded after documentation finalization |

The repository script is named `type-check`; an initial non-authoritative
`npm run typecheck` invocation reported the expected missing-script error, then
the exact repository command above passed.

The frontend suite retained pre-existing React Router future-flag,
`findDOMNode`, Semi Form and selected test `act(...)` warnings. B3 changes no
frontend source. The production build retained the existing large-chunk
warning.

## Source compatibility

- Existing `traceOutboxPayload`, Source event hash inputs, Outbox event key and
  Outbox payload JSON fields remain unchanged.
- Export observation begins only after `outbox.Enqueue` reports a newly created
  durable event.
- Export result is deliberately ignored by the Gateway Relay.
- Request records, router decisions, provider dispatch, customer response,
  billing/finality, audit, IAM and payload policy are unchanged.
- Completed-execution Feed endpoint, DTO, credentials, event identity, source
  bytes/hash, page and cursor behavior are unchanged.

## Runtime and evidence boundary

No collector was started or contacted. No deployment, traffic, credentials,
external system, non-test database, cursor/checkpoint, shared protocol,
X-Insight, push, PR or merge action occurred. Configuration remains default
off. This evidence is Local verified only, not CI, Target, external-integration
or Production evidence.

The next dependency-ordered repository-local batch is B4: backend-neutral
secret references and a local fixture-backed stage/activate/rollback/revoke
rotation lifecycle with no secret readback or logging.
