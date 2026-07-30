# PRD7 B5 Exact Response Cache — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B5-EXACT-RESPONSE-CACHE`
> Requirement: `P7-F07`
> Status: Local verified through standalone service/Redis-fixture boundary;
> Gateway hit serving decision-gated
> Start head: `eb654334671d00dbcd2d6a51bda1db0a6e731b74`
> Start tree: `917a5a739821a917acc45940f8832511ca44095c`
> Branch: `codex/product-iteration-20260730`

## Delivered Result

- `internal/responsecache` defines `unirouter.response-cache.v1` as a
  backend-neutral exact-cache service boundary.
- Organization, project, region, residency, API key, authorization epoch,
  endpoint, requested/resolved model, adapter, route policy, guardrail policy,
  customer price, separate response-cache price, schemas, purge generation and
  exact request bytes are bound into one HMAC-SHA-256 index.
- Backend keys are opaque. Values use versioned AES-256-GCM with the exact
  entry key as authenticated additional data.
- Only explicitly opted-in, persistence-allowed, text-only, non-stream Chat
  Completions are eligible. Strict recursive parsing rejects duplicate JSON
  keys, tools/functions and media.
- A write requires provider-reported final usage, exact token partition,
  completed delivery and settled financial finality. HTTP 200 alone cannot
  create a cache entry.
- A `Hit` is not a wire response. Its `SourceResponse` and `SourceProof`
  describe the historical provider execution; current request identity, usage
  and billing metadata require a later endpoint adapter.
- A hit states current provider requests and cost as `0` and `"0"`. Customer
  charge remains explicitly
  `requires_current_response_cache_pricing`; supplier Prompt Cache pricing is
  not reused.
- Memory and Redis adapters implement TTL, generation tombstone purge,
  token-bound atomic lease commit/release and bounded dependency failure.
- Normal contention timeout rejects instead of bypassing to a provider.
  Backend outage bypasses without fabricating a hit.
- The complete contract and runtime boundary are
  `docs/tech-ref/34-exact-response-cache.md`.

## Deterministic Metadata Vectors

With the synthetic test identity, fixture index key and generation zero:

- scope:
  `rc:v1:scope:f326daea3bf39bb1982c92558b8fa604555999b87b28bdd75560359185e668cc`;
- entry:
  `rc:v1:entry:2e3c1d1bf915257844868441b8e4779e921acaedcd5f4f7446917a93df8d7ca5`.

The vectors are asserted by tests and contain no production tenant, request,
response, credential or encryption key.

## Verification

| Command/check | Actual result |
|---|---|
| `go test -count=1 ./internal/responsecache` | PASS — 19 top-level tests plus table/subtests covering exact isolation, strict eligibility, final proof, tamper, ciphertext relocation, key rotation, TTL, purge, outage and Redis CAS |
| `go test -race -count=1 ./internal/responsecache` | PASS — including 32 simultaneous misses with exactly one owner and 31 waiters |
| `go vet ./internal/responsecache` | PASS |
| `go test -p 1 ./...` | PASS — all Go packages, including Gateway, billing, Outbox, completed Feed, event connector and response-cache regressions |
| `go vet ./...` | PASS |
| `npm test -- --run` in `frontend/` | PASS — 42 files / 308 tests |
| `npm run type-check` in `frontend/` | PASS |
| `npm run build` in `frontend/` | PASS — existing large-chunk warning remains |
| exact opaque scope/entry vectors | PASS — exact values above |
| encrypted-at-rest inspection | PASS — memory and miniredis keys/values contain no synthetic prompt, response, provider or model plaintext |
| `git diff --check` | run again at batch close |

The restricted filesystem sandbox initially denied the loopback socket needed
by miniredis. The same exact tests were rerun with permission only for the
local ephemeral loopback fixture and passed. No external Redis, network
service or runtime was contacted.

The frontend suite retained existing React Router future-flag,
`findDOMNode`, Semi Form and selected `act(...)` warnings. B5 changes no
frontend source.

## Exact Component Digests

| Path | SHA-256 |
|---|---|
| `internal/responsecache/contract.go` | `7f07cdef4c805887b31b2ebd173b0ee2431e784a7767da34b4d36be0a8fdfe93` |
| `internal/responsecache/keyring.go` | `8a3f4cd929c9e55c0b1c78868644f8ab8a1fe439659b22951715f8c7ebf731a3` |
| `internal/responsecache/memory_backend.go` | `b82a6ade3a4ace7dd1bc9591374dc625c06471d7c07db6e7f28f8247a4a5be7a` |
| `internal/responsecache/redis_backend.go` | `c37d85631b3bd3c43375f8a1588acf5617a85ee42b126f94cc673599a5a04c09` |
| `internal/responsecache/service.go` | `d3eb97916750671ddba5ab4cb3fcb4809fd807e7d6610b516dffca4236ba38ac` |
| `internal/responsecache/service_test.go` | `5d8519c3834cde71ac02973db7234732cd30748e0bbfbfb161dfb91394937e13` |
| `internal/responsecache/redis_backend_test.go` | `a8ef85ce88c78c1fb2ca2171f2ea91673ea70cbfbaf90f700eef8f00cc2b14c8` |
| `docs/tech-ref/34-exact-response-cache.md` | `62949b84999764e5cb12a9a4569d2cb2f933e5a54650f3ed890eb399271b833f` |

## Current Product Compatibility

- No current package imports or calls `internal/responsecache`.
- Gateway Relay, Admin API and frontend behavior are unchanged.
- No response-cache config, migration, table, customer ledger entry or runtime
  toggle was added.
- Existing supplier Prompt Cache token accounting and prices are unchanged.
- Request records, route selection, provider attempts, guardrails,
  delivery/finality and billing results are unchanged.
- Outbox event identity, source snapshots and persistence are unchanged.
- Completed-execution Feed endpoint, DTO, credential, event identity, source
  bytes/hash, page and cursor behavior are unchanged.

## Runtime And Decision Boundary

No deployment, traffic, real Redis/KV/KMS, credential, provider, database,
cursor/checkpoint, external integration, push, PR or merge was used or changed.

P7-D07 remains open. Before any Gateway hit can be served, the User must choose
the customer response-cache commercial rule and ledger semantics. B5 does not
create customer debt or silently select free, provider-equivalent,
subscription, quota or discounted pricing.

P7-D01 still gates a production encryption-key backend/operator. P7-D06 still
gates target deployment, traffic, migration and release.

This Local-verified result does not imply CI, Target, external-integration or
Production evidence. The next repository-local batch is B6 endpoint protocol
coverage.
