# PRD7 B4 Secret Reference And Rotation — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B4-SECRET-REFERENCE-ROTATION`
> Requirement: `P7-F06`
> Status: Local verified through backend-neutral/in-memory-fixture boundary
> Start head: `f79af60f7311751535a4d4dd3dd59cf1ddfd3f2b`
> Start tree: `0bb9926de9970cad6059bcd94d3372dd70e643da`
> Branch: `codex/product-iteration-20260730`

## Delivered Result

- `internal/secretref` defines `unirouter.secret-reference.v1`.
- `Reference` separates backend, ownership scope and logical name; strict ASCII
  validation gives it an unambiguous metadata-only canonical form.
- `Registry` is an immutable, fixed backend allowlist assembled in code. It
  does not load plugins or accept runtime registration.
- Every secret version has immutable bytes and explicit
  `staged/active/retired/revoked` metadata.
- Every lifecycle write uses exact expected-revision CAS.
- Active generation changes only when activation or rollback changes the
  selected version. Merely staging a candidate does not invalidate workers.
- A worker pins exact `(reference, version, generation)` identity.
  `WithMaterial` atomically revalidates it before a short material-injection
  callback; an old worker fails with `ErrStaleBinding`.
- Active-version revocation fails closed. A replacement must be activated
  before the retired version can be revoked.
- Backend outage fails every lifecycle, pin, material and metadata operation
  closed without mutating revision/generation or using a stale fallback.
- Callback errors and panics are reduced to the fixed
  `ErrConsumerRejected`; callback material is a temporary copy and is
  overwritten after use.
- Snapshots, bindings and audit receipts contain no plaintext, ciphertext,
  secret-material digest or readback handle.

The complete mapping and future adapter boundary are
`docs/tech-ref/33-secret-reference-rotation.md`.

## Deterministic Fixture

- path:
  `internal/secretref/testdata/rotation-metadata.golden.json`
- SHA-256:
  `b4153d698ac4b93680e74695dd058277f952640523638e2fd4c29768c3f561ac`
- contents:
  metadata-only stage/activate/stage/activate/rollback/revoke snapshot plus six
  ordered audit receipts; no credential material.

## Verification

| Command/check | Actual result |
|---|---|
| `go test ./internal/secretref` | PASS — lifecycle, immutable version, CAS, fixed registry, stale-worker, outage, disclosure, callback sanitization, concurrency and deterministic golden coverage |
| `go test -race ./internal/secretref` | PASS |
| `go vet ./internal/secretref` | PASS |
| `go test -p 1 ./...` | PASS — all Go packages, including existing Gateway/channel-key/routing/billing/Outbox/Feed regressions |
| `go vet ./...` | PASS |
| `npm test -- --run` in `frontend/` | PASS — 42 files / 308 tests |
| `npm run type-check` in `frontend/` | PASS |
| `npm run build` in `frontend/` | PASS — existing large-chunk warning remains |
| deterministic golden SHA-256 | PASS — exact digest above |
| `git diff --check` | PASS |

The frontend suite retained pre-existing React Router future-flag,
`findDOMNode`, Semi Form and selected test `act(...)` warnings. B4 changes no
frontend source.

An initial full Go run was launched concurrently with the frontend suite and
did not pass: several existing PostgreSQL packages exceeded the temporary
database drop timeout during resource contention. Their business assertions
and the B4 package were not the failures. After the frontend run completed,
the exact repository was rerun serially with `go test -p 1 ./...`; every
package passed. No timeout or product code was changed to manufacture that
result.

## Current Product Compatibility

- No migration or model field was added to `channel_keys`.
- Existing AES-GCM `key_enc` write/update/decrypt behavior is unchanged.
- Existing `state_epoch`, route snapshot, circuit, provider retry and adapter
  behavior is unchanged.
- No B4 package is called by the Gateway Relay, Admin API or exporter.
- Request records, provider attempts, billing/finality, Outbox, audit and IAM
  behavior are unchanged.
- Completed-execution Feed endpoint, DTO, credentials, event identity, source
  bytes/hash, page and cursor behavior are unchanged.

## Runtime And Decision Boundary

No secret value, real backend, Vault/cloud account, credential, provider,
collector, deployment, traffic, non-test database, cursor/checkpoint,
external integration, push, PR or merge was used or changed.

P7-D01 remains open: the User must select the first production backend and its
operator ownership before a real adapter, workload identity, migration or
Gateway/OTLP credential wiring begins. This Local-verified B4 result does not
imply CI, Target, external-integration or Production evidence.

The next dependency-ordered repository-local batch is B5: an exact
tenant-isolated response cache with encrypted-value boundary, deterministic
identity, TTL/purge, stampede protection, poisoning defenses and explicit
billing/finality semantics.
