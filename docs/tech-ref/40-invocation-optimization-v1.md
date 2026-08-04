# Invocation Optimization And Compression V1

> Policy contract: `unirouter.invocation-optimization-policy.v1`
> Receipt contract: `unirouter.invocation-optimization-receipt.v1`
> Compression algorithm: `unirouter.prompt-compression.whitespace-safe.v1`
> Scope: PRD8 CO-4 / CO-5
> Status: Local verified candidate; target policies and Canary remain disabled

## 1. Ownership and placement

Invocation Optimization is a UniRouter product-local control plane. It changes
model request execution, so configuration lives at `模型治理 -> 调用优化`.
Read-only results live at `成本与财务 -> 成本优化`.

UniRouter continues to own IAM, tenant/project scope, routing, provider
credentials, Guardrail, reservation/settlement and final execution. X-Insight
may later analyze exported terminal evidence, but its availability is never in
the request path. Tool result caching and MCP resource/session caching remain
owned by their respective gateways.

## 2. Policy scope and lifecycle

A policy binds organization, optional project, exact model or `*`, region,
residency, driver targets, canary basis points, safety thresholds and one
monotonic revision. Project-specific and exact-model policies win over
organization/wildcard policies.

Lifecycle:

```text
draft -> validated -> shadow -> canary -> rolled_back
```

- create/update saves only a draft;
- validate freezes a checked revision without changing requests;
- Shadow evaluates eligibility, safety, quality, latency and estimated token
  change but never assigns or mutates;
- Canary is limited to `1..1000` basis points, requires explicit request opt-in
  and is available only after the server verifies Shadow evidence;
- rollback stops future assignment and preserves append-only evidence; and
- every transition requires `expected_revision` plus an auditable reason.

Both drivers are default-off. A policy cannot disable the request opt-in
requirement. Platform and organization administrators own the API; project,
billing, developer and viewer roles cannot mutate it.

## 3. Content-free deterministic assignment

Assignment is evaluated only in Canary. Its SHA-256 input binds the policy
contract/reference/revision, organization, project, driver, API-key ID and the
server-owned operation ID. It does not hash, persist or derive identifiers from
prompt content.

The assignment is stable for one operation and cannot exceed the policy's
reviewed basis points. A non-assigned request follows the unchanged provider
path.

## 4. Compression V1

Compression V1 is deliberately narrow: it normalizes redundant whitespace in
plain user-message strings. It does not call a model or tokenizer service. It
rejects:

- streaming requests;
- tools, functions, tool choice and parallel tool controls;
- structured response formats, modalities and audio;
- non-string/multimodal content;
- code fences, code-like syntax, URLs, Markdown/table/list structure, XML,
  JSON-in-string, YAML-like key/value content and long base64-like text; and
- invalid or duplicate-key JSON.

Unknown numeric fields are decoded with `json.Number`, preserving exact int64
and decimal lexemes during candidate serialization. Only user message content
is changed; system/developer/assistant content is unchanged. Safety requires
the non-whitespace code-point sequence to remain exact. Quality requires the
word sequence to remain exact. Minimum input, minimum estimated saved tokens
and maximum reduction basis points are policy-controlled within server bounds.

Shadow builds the candidate in memory and records metadata only. Canary applies
it only when all gates pass, the request is assigned and the caller sends:

```http
X-UniRouter-Prompt-Compression: allow
```

`X-UniRouter-Prompt-Compression: APPLIED` is the only response value that
claims request mutation. `SHADOW` and `CANARY` report the effective stage, not
an applied change.

## 5. Evidence and privacy

Each driver writes at most one append-only receipt per organization,
server-operation ID and driver. It contains policy identity, stage/mode,
decision/reason, assignment/eligibility/mutation flags, byte/token estimates,
safety/quality booleans, latency, route/channel/credential IDs and a
content-free identity digest.

Receipts do not contain prompt, response, candidate body, secret material or a
prompt-derived fingerprint. Compression savings remain estimated tokens; they
are not presented as realized money. Exact Response Cache realized avoided
cost is separately sourced from completed zero-charge commercial receipts.

Canary admission for every targeted driver requires:

- `stage=shadow` and `effective_mode=shadow` evidence for the current revision;
- samples at or above `minimum_shadow_samples`;
- at least one eligible sample; and
- zero `assigned`, `mutation_applied`, eligible safety failures or eligible
  quality failures.

## 6. API and UI

Control endpoints:

- `GET /api/v1/invocation-optimization/contract`
- `GET|POST /api/v1/invocation-optimization/policies`
- `GET|PUT /api/v1/invocation-optimization/policies/{ref}`
- `POST .../{ref}/validate`
- `POST .../{ref}/shadow`
- `POST .../{ref}/canary`
- `POST .../{ref}/rollback`

The UI keeps a persistent right inspector with scope, revision, exact driver
allocation, evidence gate, content-retention boundary and only the legal next
actions. Draft creation never starts runtime evaluation.

## 7. Current evidence boundary

Repository-local tests cover normalization, structured-content exclusions,
duplicate JSON, exact numeric lexemes, no-content receipts, content-free
assignment, Shadow non-assignment, explicit opt-in, Canary mutation,
tenant/RBAC isolation, optimistic revision locking and database evidence gates.

No target policy, Cache key, Canary, provider traffic, credential, data reset,
X-Insight flow or customer-production result is authorized or claimed by this
document.
