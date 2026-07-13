# PRD6 Batch 4 P6-F10 Guardrail Kernel — Repository Evidence

> Date: 2026-07-13
> Decision: D6-15 G1-R / S1-v2 accepted
> Branch: `codex/prd6-guardrail-kernel`
> Base: post-P6-F41 `main` at
> `fbfde8816a6a6676cd0c65a489659d246c5541fd`
> Evidence level: full local and focused PostgreSQL/race verified; PR and
> required CI promotion pending
> Scope: UniRouter repository only; no deployment, target mutation, real or
> paid provider/detector call, credential change, raw-payload persistence,
> external exporter/outbox activation, extra database access or X-Insight work

## 1. Confirmed Pre-Change Findings

The following were source-confirmed rather than inferred:

1. The existing sensitive-word filter was a direct Chat input check. There was
   no versioned global/tenant/project/key/model Guardrail chain, no non-stream
   output stage, no local PII/secret adapter and no registered external
   classifier contract.
2. Request bodies, provider error bodies and successful non-stream responses
   used unbounded `io.ReadAll` paths. `Content-Length` was not sufficient safety
   authority, and no node/organization retained-byte budget existed.
3. A provider response intentionally withheld after provider acceptance had no
   dedicated no-delivery/no-customer-charge financial state. A policy failure
   could therefore lose provider cost authority or tempt an unsafe retry.
4. Repeated middleware and handler body reads created avoidable copies, and the
   final response after adding `_uniroute` metadata had no authoritative size
   check.
5. Detector-controlled category strings could become an evidence exfiltration
   channel unless external categories were sanitized independently of their
   lexical shape.

These are repository findings. They do not establish a target capacity,
production detector quality or any supplier's final invoice.

## 2. G1-R Guardrail Contract

- Detectors produce bounded findings; the UniRouter kernel alone owns
  `allow`, `redact`, `block` and degraded behavior.
- Policies are versioned and selectable by global/organization/project/API
  key/model scope. Platform-mandatory policies are global, enforced and
  fail-closed; lower scopes can add restrictions but cannot weaken them.
- One request pins one immutable policy and sensitive-word snapshot across
  input and non-stream output evaluation.
- Modes are `disabled`, `shadow` and `enforce`; actions are `audit`, `redact`
  and `block`; detector outcomes are `pass`, `match`, `transform`,
  `indeterminate`, `unavailable` or `skipped`.
- Non-stream inspection covers ordinary/legacy text, structured text parts,
  refusal text and modern/legacy function or tool argument strings. A
  transformed argument must remain valid JSON, and all output transforms are
  shape/path-validated before any mutation. An unrecognized output shape is
  `skipped/degraded` and follows the policy failure mode instead of becoming a
  false pass.
- Existing Chat sensitive-word behavior runs through the kernel without
  automatically extending that historical behavior to Embeddings or enabling
  a new PII/semantic block.
- The built-in deterministic adapter detects configured email, phone,
  Luhn-valid card and secret/token patterns. Equivalent policies reuse one
  pinned detector result instead of duplicating the same call.
- Generic external classifiers must be registered by platform assembly at a
  fixed HTTPS endpoint. A tenant cannot supply a URL, header or credential.
  All external policies in one Guardrail evaluation share one one-second total
  deadline; they do not receive a fresh second per text or per rule. The
  adapter uses a 32-call isolated pool, no automatic retry, no redirects, a
  64 KiB response limit and at most 256 findings.
- An enabled external classifier necessarily receives the selected raw text
  for synchronous classification. Its use therefore requires a separate
  tenant/data-residency/retention/supplier approval; this repository batch
  registers no real endpoint and sends no such content.
- Every synchronous policy evaluation is bounded to 256 text units to prevent
  low-byte/high-cardinality detector-call amplification. PII/secret evaluation
  is additionally bounded to 128 KiB total text per policy; an external policy
  is bounded to 256 KiB. An overrun is explicitly `indeterminate`, never a fake pass: `fail_closed` blocks and
  `degraded_allow` records the gap and continues. Semantic/large-content work
  remains shadow/async or future scope.
- External finding category labels are reduced to `external_finding`; durable
  receipts contain policy/detector versions, outcome, bounded count/category,
  latency and HMAC fingerprints only. Prompt, response, match text, arbitrary
  detector label and credential do not enter trace/outbox/audit evidence.

The local 16,057-byte deterministic fixture ran 500 iterations with observed
p95 `2.168458ms` on Apple M4. Three one-second benchmark samples were
approximately `1.88ms/op`, `8.5 MB/s`, 18 allocations. This is local
engineering evidence for that fixture, not a production latency SLO or an
8 MiB full-payload throughput claim.

Sensitive-word request pinning shares the prior immutable DFA root rather than
rebuilding the word trie per request. A 5,000-word local snapshot benchmark
observed approximately `24.7-25.4ns/op`, 80 bytes and one allocation; race
coverage proves the pinned root keeps the old behavior across a concurrent
source update. This removes word-list-size work from the request pin path.

## 3. S1-v2 Content And Memory Boundary

Application-authoritative defaults are:

| Profile | Default |
|---|---:|
| Chat synchronous request | 8 MiB |
| Embeddings synchronous batch request | 16 MiB |
| final non-stream customer response | 16 MiB |
| platform hard maximum for any profile | 64 MiB |
| per-replica node retained-body budget | 256 MiB |
| per-organization/per-replica retained-body budget | 64 MiB |

- Actual bounded reads are authoritative for fixed-length, chunked and false
  `Content-Length` requests.
- Gateway auth runs before organization accounting. The body is then read once
  into a request envelope shared by idempotency, model governance and relay.
  A server-owned redaction/rewrite must atomically resize the same lease.
- Input oversize returns OpenAI-compatible 413 before billing freeze, routing
  or provider dispatch. Capacity exhaustion returns typed 503 with
  `Retry-After: 1`.
- Provider error bodies are capped at 64 KiB. Successful non-stream bodies are
  bounded and the server reserves 64 KiB inside the 16 MiB final limit for the
  `_uniroute` extension. Any upstream-supplied `_uniroute` value is discarded;
  the final server extension is injected once and checked against the final
  limit.
- Local capacity exhaustion or an oversize response does not mark the provider
  channel unhealthy. The request does not retry or fail over.
- Images/audio/files remain future tenant-scoped object references; video and
  large batch workloads remain a future asynchronous job plane. The 64 MiB
  hard maximum is not a normal tenant setting or a promise to carry media
  inline.

The byte budget accounts retained raw envelopes; Go object-graph/JSON decode
overhead still requires target heap and concurrency measurement. No target or
100x capacity claim is made.

## 4. Output Delivery And Cost Integrity

- Input block occurs before billing/provider dispatch and therefore creates no
  provider cost.
- Non-stream output block occurs after a parseable provider response. UniRouter
  stores authoritative provider usage as `provider_evidence`, creates no local
  delivery receipt and no customer charge, moves the attempt to
  `output_withheld`, and blocks the affected paid key on the existing provider
  cost reconciliation path.
- The same rule applies when the final encoded response exceeds its size
  profile. It uses `response_too_large`, returns 502, does not retry/fail over
  and retains known provider usage without customer debt.
- If UniRouter cannot safely apply an output transform after provider
  acceptance, the response is also withheld and its provider usage enters the
  explicit hold before returning an error.
- Provider-attempt completion, no-delivery output hold and normal non-stream
  delivery preparation use independent bounded financial contexts after
  provider acceptance. Client cancellation cannot cancel these database
  transitions; the output hold retries only the same idempotent mutation up to
  the existing bounded three-attempt/ten-second limit.
- An operator must explicitly record a provider refund or bounded platform
  subsidy to release the hold. D6-13 forbids automatic customer debt and silent
  platform subsidy; this batch does not choose a new commercial allocation.
- A provider might still charge when detector infrastructure fails after the
  upstream call. The accepted customer rule remains zero charge for
  undelivered content while provider COGS is retained for reconciliation.

Migration `000033_prd6_guardrail_kernel` adds the policy table, guarded
`output_withheld` state and pending reasons. Unused schema can down and roll
forward; composite project/organization and API-key/organization/project
foreign keys reject cross-tenant selector combinations even outside the HTTP
handler. Any policy or financial-hold evidence refuses destructive down. Safe
activation is non-rolling: drain old data/control-plane binaries, migrate with
an empty policy table, start only the new version, then create shadow policies.

## 5. Control Plane And Operator UI

- `GET/POST /api/v1/guardrail-policies` and version-checked
  `PUT /api/v1/guardrail-policies/{id}` provide tenant-non-enumerating control.
- Platform administrators may own global/mandatory policy. Organization
  administrators can manage only their own optional policies. Billing admins
  are read-only.
- Project/key selectors are ownership-validated. Stale version updates return
  409. Every mutation writes sanitized audit evidence containing only the
  configuration byte count and SHA-256 version fingerprint, not a second copy
  of detector config values, and requests a local snapshot refresh.
- The approved detector list comes from the running registry. Arbitrary tenant
  endpoints and credential-like nested config keys, including keys inside
  arrays, are rejected.
- The Content Safety UI explains shadow rollout, mandatory stacking,
  fail-closed versus degraded-allow, scan bounds, external timeout/no-retry,
  output cost holds and the separate P6-F11 streaming boundary in plain
  language.

## 6. Local Verification Passed

Passed on 2026-07-13:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go test ./internal/payloadpolicy ./internal/guardrail ./internal/filter ./internal/traceutil ./internal/gateway ./internal/config ./internal/errdocs ./internal/billing ./internal/admin ./cmd/uniroute
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -run TestP6F10
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -run TestP6F10
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/billing -run TestP6F10
REQUIRE_TEST_DATABASE=1 go test -race ./internal/middleware -run TestP6F40CAffectedKeyPaidAdmissionBlockClearsOnlyAfterResolution
go test -run TestLocalPIISecretsDetectorLatencyEvidence -v ./internal/guardrail
go test -run '^$' -bench BenchmarkLocalPIISecretsDetector16KiB -benchmem -benchtime=1s -count=3 ./internal/guardrail
go test -run '^$' -bench BenchmarkDFAFilter_Snapshot -benchmem -benchtime=1s -count=3 ./internal/filter
cd frontend && npm run type-check
cd frontend && npm test -- --run
cd frontend && npm run build
OpenAPI YAML parse
git diff --check
```

Focused PostgreSQL/race evidence covers migration down/roll-forward/refusal,
database-level cross-tenant selector rejection, same-tenant/global policy
visibility, foreign tenant non-enumeration,
mandatory authority, optimistic versioning, mutation audit, runtime snapshot,
shared external deadline, saturation without extra dispatch, concurrent
idempotent output holds, zero customer spend/delivery, explicit
subsidy release, final-response size holds and affected-key release only after
reconciliation. Frontend type-check, 30 files / 148 tests and production build
passed. The build retains the existing large-chunk warning. macOS race linking
retains the existing malformed `LC_DYSYMTAB` warning while all race commands
exit successfully. Required GitHub Backend Tests, Frontend Build and Docker
Build remain promotion gates until their exact-head results are recorded here;
the local host has no `docker` executable, so no local Docker claim is made.

## 7. Explicit Non-Claims And Remaining Boundary

- No real or paid provider request and no real external detector call.
- No production detector precision/recall, latency, throughput, capacity or
  availability verification.
- No stream chunk interception, cross-chunk semantic detection, disclosure
  bound, upstream cancellation or stream financial terminal; those remain
  P6-F11.
- No raw prompt/response storage, response cache, semantic cache, media job
  plane, arbitrary tenant detector URL, detector credential store or BYO
  plugin runtime.
- No deployment, target mutation, credential change, external exporter,
  continuous ingestion, X-Insight runtime dependency or production claim.

P6-F10 can become CI verified / closed only after the exact PR head and
post-merge `main` pass all three required jobs. Until then this document is
local repository evidence, not closure evidence.
