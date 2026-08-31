# Kimi K3 Chat Core development/test evidence — 2026-08-31

## Scope and result

This evidence closes the bounded `Kimi K3 × Kimi Platform OpenAI-compatible
Chat × Core Conformance` development/test batch at its actual evidence level.
It does not claim Production, product maturity `released`, a global canonical
switch, Alias promotion, active pricing, customer charging, Dynamic Tools,
search, media or JSON Schema.

The exact K3 tuple has eight current receipt-aware capability qualifications.
The development/test Release and initial stable-slot activation establish only
a test route. Public fixed-SDK delivery succeeded for non-streaming text,
`reasoning_effort=low`, tools, required tool choice, JSON Object and cache-read
usage. Public fixed-SDK streaming did not close: two separately counted
single-attempt requests returned provider HTTP 503. The successful typed SSE
Probe remains capability evidence; it is not substituted for public SDK
delivery evidence.

## Exact artifact and M102 publication

- Target artifact: version `1.2.28`, merge commit
  `16bf398aed4122c52a174a4d3340b4bc399cd3b7`, tree
  `a6efc7c13aadcb3fa235f3665b430fd94becf9aa`.
- Stable and canary backend image ID:
  `sha256:b0f2d7e979352dc40db3439578aefc128556691870b13c2af8dd7992575c02ef`.
- Frontend image ID:
  `sha256:afa99dfd79c677fdf04a320a3582d63938a374257269ab393e45fd6a06c361b6`.
- Nginx image ID:
  `sha256:0d3b80406a13a767339fbe2f41406d6c7da727ab89cf8fae399e81f780f814d1`.
- Stable and canary are healthy; public `/health/ready` returns
  `{"status":"ok","db":"ok","redis":"ok"}`.
- PostgreSQL has 101 ordered migration rows ending at
  `000102_kimi_k3_chat_core_qualification`.

The zero-conflict M102 Publication V1 plan used declaration digest
`a8d6178ca8238cc03fce345a847d8dba4b13665a42d9ba90af9a127aa5c040f5`
and plan digest
`905790bd90914c648259ea3b78ba7af80d11d325bb3acc4fae150e0dd1254af5`.
It advanced the exact ledger predecessor M101 to M102, appended eight K3
Profile 1.9 bindings, promoted eight exact selectors and performed no
Deployment projection write because the exact Deployment did not yet exist.
The later administrator-created Deployment 6 received its declared capability
rows from the server-owned current bindings; M102 itself created no Probe,
Approval, qualification, Release or route.

## Operation and capability qualification

Deployment 6 binds public model 817, connection 10, credential binding 10,
operation `openai.chat_completions.v1`, Profile `kimi.k3.api@1.9.0` and protocol
`protocol.kimi-openai-chat.k3.v5@1.0.0`. Operation Probe 6 succeeded with
provider HTTP 200 in 3,027 ms and reported exact model `kimi-k3`.

The current qualification resolver returns exactly these eight qualified
receipts:

| Capability | Probe | Approval | Probe contract |
|---|---:|---:|---|
| function tools | 64 | 54 | `kimi-chat-function-tools.v1@4.0.0` |
| JSON Object | 65 | 55 | `kimi-chat-json-object.v1@2.0.0` |
| reasoning | 66 | 56 | `kimi-chat-reasoning.v1@2.0.0` |
| reasoning effort | 68 | 57 | `kimi-chat-reasoning-effort.v1@1.0.0` |
| streaming | 69 | 58 | `kimi-chat-sse.v1@4.0.0` |
| tool choice auto | 70 | 59 | `kimi-chat-function-tools.v1@4.0.0` |
| tool choice required | 71 | 60 | `kimi-chat-function-tools.v1@4.0.0` |
| tools | 72 | 61 | `kimi-chat-function-tools.v1@4.0.0` |

Failed Probe receipts 62, 63 and 67 remain immutable and unapproved. They
record, respectively, an insufficient always-on-reasoning response budget, the
already known optional Kimi child tool-call index, and detailed usage reaching
the wrong contract-specific decoder. Source releases 1.2.26 through 1.2.28
fixed those exact defects without changing Profile 1.9, protocol K3 v5, M102 or
the historical receipts.

## Development/test test route

The administrator UI authored Release
`rcr_deployment_20260831t144223_84716647c210ac9c812f217f` (database ID 27,
authoring receipt 8) with one sealed route item. Its lifecycle row is `active`
version 3 and the development/test slot points stable to Release 27 with no
canary and slot version 1.

This is an internal development/test routing activation only. Public model 817
remains `lifecycle=staging` and `approved_catalog=false`. The row and slot do
not prove capability maturity `released`, Production readiness, Alias or
global canonical authority.

## Fixed-SDK public delivery

The temporary application credential was restricted to `kimi-k3`. The client
was exact `openai-python@3.5.0` with `max_retries=0`. Call logs 413–420 each
bind public model 817, Deployment 6, connection/binding 10, Chat Completions and
development/test Release 27. Every request created exactly one provider
attempt (`route_sequence=1`, `route_total=1`).

| Call | Checkpoint | HTTP | Provider usage | Cache-read | Result |
|---:|---|---:|---|---:|---|
| 413 | non-stream text + live `reasoning_effort=low` | 200 | 98 input / 37 output | 0 | delivered |
| 414 | public SDK streaming | 503 | missing | 0 | provider `channel_failure` |
| 415 | one manually authorized, separately counted streaming retry | 503 | missing | 0 | provider `channel_failure` |
| 416 | automatic function-tool choice | 200 | 197 / 101 | 0 | delivered |
| 417 | required function-tool choice | 200 | 233 / 99 | 0 | delivered |
| 418 | JSON Object | 200 | 164 / 83 | 0 | delivered |
| 419 | cache workflow, first request | 200 | 1,719 / 60 | 0 | delivered |
| 420 | cache workflow, second request | 200 | 1,719 / 69 | 1,536 | delivered |

The six HTTP-200 rows are `provider_reported/final`. Calls 414 and 415 are
`known_rejected` with no usage and no hidden provider retry; the second was a
deliberate one-request retry after the first failure was classified. Local
value-domain tests cover `low|high|max`, but only `low` has live public evidence.

The full K3 batch consumed all 20 authorized supplier calls: one operation
Probe, eleven capability Probe attempts (including three immutable failures)
and eight public SDK requests. No further call was made after the ceiling was
reached.

## Financial and access cleanup

All six settled public attempts have calculated and charged cost of zero under
the current missing-price/cost-observation mode. This is not evidence that the
supplier's native cost was zero. CNY 5 remains only the approved supplier-spend
ceiling; there is no supplier invoice or `SupplierCostObservation`.

Customer wallet balance, granted balance, frozen amount and cumulative spend
remain zero CNY, and the transaction table remains empty. The temporary
Workload Application 8 is `archived` revision 2, its `project_default` Grant 16
is `revoked` revision 2, and its only credential 36 is `revoked` with
`allowed_models=kimi-k3`. The task-created local secret file and test virtual
environment were deleted after the database/UI readback. Immutable Probe,
Approval, Release, call, attempt and audit evidence was retained.

## Final boundary

K3 Chat Core is capability-qualified on the exact development/test tuple and
has bounded public delivery evidence for the non-streaming subset above. It is
not accurately described as fully public-SDK verified because the remaining
streaming checkpoint returned two provider 503 responses. Any later streaming
retry requires a new explicit supplier-call and spend authorization; it cannot
be inferred from the successful capability Probe or performed automatically.
