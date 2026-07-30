# Exact Response Cache Contract

> Contract: `unirouter.response-cache.v1`
> Scope: PRD7 B5 / P7-F07
> Runtime status: standalone service boundary with memory and Redis adapters;
> Gateway hit serving remains decision-gated

## 1. Product Boundary

The exact response cache reduces repeated provider execution only when the
post-policy request bytes and every relevant authority, route, model, pricing
and residency identity are identical. It is not a semantic cache and never
uses similarity, embeddings or model-generated equivalence.

The implementation lives in `internal/responsecache`. It has no dependency on
the Gateway Relay, X-Insight, an analytics engine or a dynamic plugin runtime.
It does not alter existing request records, routing, policy enforcement,
provider attempts, billing settlement, Outbox events or the
completed-execution Feed.

Raw prompt and response persistence remains default-off. A lookup is eligible
only when the caller explicitly opts in, the effective data policy allows
persistence and the request does not carry `no-store`.

## 2. Exact Identity

Every key binds:

- organization and explicit project scope (`project_id=0` means an
  organization-scoped key, not an unknown project);
- region and residency;
- API-key identity and authorization epoch;
- endpoint;
- requested and resolved model;
- adapter-profile, route-policy and guardrail-policy versions;
- customer pricing version and the separate response-cache price version;
- request and response schema versions;
- scope purge generation;
- SHA-256 and exact length of the post-input-policy request bytes.

Whitespace, object-key order or any other request-byte difference intentionally
creates a miss. The cache does not canonicalize requests because
canonicalization could erase adapter-significant distinctions.

The index is HMAC-SHA-256 under a dedicated 32-byte index key. Signed inputs use
fixed-width big-endian integers and four-byte length-prefixed UTF-8 strings or
byte arrays. Redis and memory backends receive only opaque HMAC-derived scope,
entry and lease keys; prompt, response, model, tenant, provider and policy
values are not embedded in backend keys.

The deterministic synthetic vectors are:

- scope:
  `rc:v1:scope:f326daea3bf39bb1982c92558b8fa604555999b87b28bdd75560359185e668cc`;
- generation-zero entry:
  `rc:v1:entry:2e3c1d1bf915257844868441b8e4779e921acaedcd5f4f7446917a93df8d7ca5`.

They are regression vectors, not production keys or data.

## 3. Eligibility And Poisoning Boundary

B5 admits only text-only, non-streaming Chat Completions:

- `messages` must be a non-empty array;
- roles are limited to `system`, `developer`, `user` and `assistant`;
- structured content, tool/function roles, calls or results, images, audio,
  video and other media are excluded;
- top-level tools/functions, tool choice, parallel tool calls and modalities
  are excluded;
- strict recursive JSON parsing rejects duplicate keys at any depth.

A stored `SourceResponse` must contain at least one message choice and cannot
contain tool/function/media results or the UniRouter request-local `uniroute`
extension. The extension is rebuilt, if applicable, by the future current
request path; replaying an old request identifier or receipt is prohibited.
`Hit` is deliberately not a wire-response type: the source response identifier
and usage belong to the historical provider execution. A future endpoint
adapter must construct current request-local identity and cache-aware usage
before any bytes can be returned to a caller.

HTTP 200 alone is insufficient. A write requires an exact encrypted
`UsageProof` with:

- provider-reported upstream usage under `unirouter.usage.v1`;
- a lossless prompt-token partition and bounded non-negative counters;
- current customer-pricing-version identity;
- exact resolved-model identity;
- at least one source provider request;
- completed local delivery;
- settled financial finality.

Estimated usage, missing usage, incomplete delivery, pending settlement,
pricing/model mismatch and invalid token partitions cannot be cached.

## 4. Encryption And Key Rotation

Values are encrypted with AES-256-GCM. The authenticated additional data binds
the cache contract and exact opaque entry key, so ciphertext copied to another
entry cannot decrypt. The only outer fields are contract version, encryption
key version, nonce and ciphertext.

`EncryptionKeyring` provides a current immutable key version and exact
historical version lookup. New writes use the current version; existing values
remain readable while their historical key version is available. Missing
historical keys, modified ciphertext, invalid envelopes and invalid decrypted
records are never served. The entry is invalidated and one miss owner may
recover it.

The static keyring is a local fixture boundary. A production key backend and
operator remain subject to P7-D01 and must preserve bounded lookup, versioned
read and no-readback behavior.

## 5. TTL, Purge And Stampede Semantics

Each scope has a monotonic purge generation. Purge increments the generation
before any later lookup; old encrypted values may age out physically, but no
new lookup can derive an old-generation entry key. This is a tombstone, not a
claim of immediate storage destruction.

On a miss, an atomic lease creates exactly one owner. Other contenders receive
opaque wait capabilities. A successful owner commit compares the exact lease
token, writes the encrypted entry and releases the lease atomically.

Waiters may:

- read the committed hit;
- become the single new owner after a released or expired lease;
- fail closed on bounded waiter capacity or normal wait timeout;
- bypass only when the cache dependency is unavailable.

A normal contention timeout never becomes an upstream bypass, preventing a
healthy but slow owner from creating a provider stampede. Memory and Redis
backends implement the same lease and generation contract; the memory backend
is a deterministic fixture, not a multi-replica production store.

All backend and encryption-keyring operations have a hard dependency timeout.
Backend failure does not fabricate a hit, usage or price.

## 6. Billing Truth

A cache hit reports:

- current provider request count: `0`;
- current provider cost in micros: `"0"`;
- encrypted source execution proof: historical evidence only;
- current customer charge state:
  `requires_current_response_cache_pricing`.

The existing `cache_read_ratio` describes supplier prompt-token cache pricing.
It is not a response-cache customer price and must not be reused.

P7-D07 must choose the customer commercial rule before Gateway hit serving:
free, fixed request price, subscription/quota consumption, a versioned
discount, or another explicitly defined policy. That decision must specify
ledger unit, tax/rounding behavior, refunds, plan/version binding and UI/API
representation. Until then the service returns an explicit unpriced state and
cannot create customer debt.

## 7. Verified Local Boundary And Remaining Work

The repository-local package and Redis adapter cover exact identity, encrypted
values, strict eligibility, proof finality, lease ownership, bounded waiting,
TTL, generation purge, outage, tamper and key-rotation behavior with synthetic
tests.

Not included:

- Gateway, admin API or UI integration;
- a production Redis/KV deployment, KMS or credential;
- live traffic, load/soak or multi-region evidence;
- response-cache customer pricing or billing-ledger writes;
- semantic cache;
- target, CI, external-integration or production claims.

After P7-D07, runtime integration must still execute current authorization,
policy, data-residency and billing checks before serving a hit, rebuild only
request-local response metadata, and preserve the existing finality and
evidence model.
