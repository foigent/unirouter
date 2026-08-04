# Exact Response Cache V2

> Contract: `unirouter.response-cache.v2`
> Scope: PRD8 CO-2 / CO-3
> Implementation status: Local verified candidate
> Runtime status: default-off; no target key material or Canary is authorized by this document

## 1. Product boundary

Exact Response Cache avoids a repeated model-provider execution only when the
post-policy request bytes and every execution authority are identical. It is
not a semantic cache, does not use embeddings or generated equivalence, and is
not shared with Tool Gateway, MCP Gateway or X-Insight.

V2 is integrated into UniRouter's existing non-streaming Chat Completions
path. Authorization, model permission, Guardrail pinning, reservation, routing,
credential selection, delivery evidence, settlement, request logs and final
execution remain owned by the normal UniRouter path. A cache hit performs zero
provider attempts and does not make X-Insight a synchronous dependency.

Only text-only requests are eligible. Streaming, tools/functions, structured
content, image/audio/video, duplicate JSON keys and request shapes that cannot
be proven safe are bypassed.

## 2. Default-off and request permission

Runtime evaluation requires all of the following:

1. an active product-local policy at `stage=shadow` or `stage=canary`;
2. `response_cache_target=canary` with a reviewed allocation of `1..1000`
   basis points;
3. persistence explicitly allowed by the policy;
4. exact request header `X-UniRouter-Response-Cache: allow`;
5. no `Cache-Control: no-store` directive; and
6. a complete, server-injected response-cache key binding.

Shadow computes metadata-only eligibility and identity evidence. It does not
read, write or decrypt cache entries. Deterministic assignment is computed
only after the policy enters Canary; Shadow receipts always record
`assigned=false` and `mutation_applied=false`.

Canary assignment is content-free and binds the policy revision, tenant,
project, API-key identity, driver and server-owned operation ID. The maximum
allocation is 10%. Omission of the opt-in header never enables persistence.

Response headers are bounded signals:

- `X-UniRouter-Response-Cache: HIT` — current response came from an exact local
  entry;
- `MISS` — this request owns the bounded upstream fill lease;
- `BYPASS` — the request made no cache-hit claim; and
- no header — no applicable cache policy was evaluated.

## 3. Exact identity

The HMAC input binds:

- organization, explicit project scope, region and residency;
- API-key ID and monotonic `auth_epoch`;
- endpoint and exact post-input-policy request bytes plus byte length;
- requested model, resolved model and provider;
- channel ID, routing epoch and circuit epoch;
- credential ID and credential state epoch;
- adapter-profile digest and route-policy version;
- runtime release, stage and allocation identity;
- pinned Guardrail policy-set version;
- invocation-optimization policy reference and revision;
- request-time customer-pricing version and the independent
  `unirouter.response-cache-zero-charge.v1` commercial version;
- request/response schema versions; and
- the scope purge generation.

Request JSON is not canonicalized. Whitespace, key order and every other byte
difference intentionally produce a miss. Backend keys are opaque
HMAC-SHA-256 values; they do not contain tenant, model, provider, prompt or
response text.

## 4. Source entry and poisoning boundary

An entry is committed only after the original provider execution has:

- provider-reported final `unirouter.usage.v1` counters with a lossless prompt
  token partition;
- at least one provider request and HTTP 200;
- the same resolved model, provider and request-time pricing identity;
- completed local response delivery; and
- settled financial finality.

Estimated/missing usage, pending settlement, incomplete delivery, oversized
responses, invalid token partitions, request-local `_uniroute`/legacy
`uniroute` extensions and tool/media responses are rejected.

The source response and source proof are encrypted evidence, not a wire
response. On a hit, the Gateway replaces the historical provider execution ID
and timestamp with the current server operation identity/time, replaces
historical usage with an OpenAI-compatible all-zero usage object, and rebuilds
the current `_uniroute` delivery/billing extension. The encrypted source bytes
remain unchanged and are linked by `source_response_sha256` in the commercial
receipt.

## 5. Storage, encryption and concurrency

Values use AES-256-GCM. Additional authenticated data binds the contract and
opaque entry key, so ciphertext copied between entries cannot decrypt. Redis
uses the isolated prefix `uniroute:response-cache:v2`.

A monotonic per-scope generation makes old entries unreachable after logical
purge. TTL is policy-bounded to `60..86400` seconds. One atomic lease owns a
miss fill; waiters may consume the committed hit or become the next owner only
after release/expiry. A normal contention timeout rejects rather than creating
a provider stampede. Backend outage, decryption failure, malformed data or
missing key versions become a miss/bypass and never fabricate a hit.

## 6. Key injection and rotation

The process stays disabled unless all three values are present and valid:

- `RESPONSE_CACHE_INDEX_KEY`: independent exact 32-byte HMAC secret;
- `RESPONSE_CACHE_ENCRYPTION_KEY`: independent exact 32-byte AES secret; and
- `RESPONSE_CACHE_KEY_VERSION`: portable non-secret immutable label.

These values must not reuse JWT, API-key hashing, provider credential or
general encryption material. Development, canary and customer Compose only
pass external values through; no real value belongs in the repository.

The current self-hosted adapter loads one encryption version. A version/key
rotation therefore makes older ciphertext fail closed and cold-miss until its
bounded TTL expires; it must never reuse a version label. A future managed
secret backend may provide bounded historical-key reads through the existing
`EncryptionKeyring` interface without changing cache identity semantics.

## 7. Delivery and commercial truth

A completed hit records:

- current provider requests: `0`;
- current provider cost: `0` micro-CNY;
- current customer charge: `0` micro-CNY;
- avoided customer cost: recomputed from the current frozen customer price;
- usage authority: `local_cache`, purpose `customer_charge`, finality `final`,
  contract `unirouter.usage.v2`; and
- prompt, cache-read, cache-create and completion counters: all `0` for the
  current delivery.

The historical provider usage remains only inside encrypted source evidence.
It is never re-attributed to the cache-hit request. The response-cache price
version is separate from provider Prompt Cache ratios.

Before any bytes are written, billing records a funded zero-charge delivery
preparation. Completed or failed local delivery has an append-only receipt and
settles the existing reservation to zero; recoverable post-write gaps remain
in the existing settlement-recovery state machine. The commercial receipt is
append-only and can be aggregated only after completed delivery.

## 8. Lifecycle, rollback and evidence

Policy lifecycle is `draft -> validated -> shadow -> canary -> rolled_back`.
Every transition uses exact revision locking and a required 1–500 character
audit reason. Canary entry fails closed unless every targeted driver has at
least the configured Shadow samples, at least one eligible sample, and zero
assigned, mutated, safety-failed or quality-failed Shadow receipts. Cache
Canary additionally requires the key runtime to be ready.

Rollback stops future assignments. The new policy revision also makes prior
entries unreachable from the current identity; encrypted values age out under
TTL. It does not rewrite append-only receipts.

Current repository evidence covers strict JSON, identity vectors, encryption,
tamper, TTL/generation, Redis lease behavior, key failures, request headers,
Shadow/Canary separation, zero-provider/zero-charge settlement, failed-delivery
recovery and migration rollback guards. It is Local evidence only until exact
CI, target deployment and read-only target verification are recorded.

## 9. Non-goals

- semantic/similarity cache;
- streaming/tool/media response cache;
- shared high-privilege plugin runtime;
- Provider Prompt Cache pricing reuse;
- raw prompt/response in policy receipts or logs;
- automatic Canary activation, test traffic or credential creation; or
- an X-Insight, Tool Gateway or MCP Gateway dependency in the request path.
