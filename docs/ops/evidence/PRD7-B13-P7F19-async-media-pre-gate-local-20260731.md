# PRD7 B13 Async Media Pre-Gate Local Evidence

> Date: 2026-07-31
> Evidence level: Local verified for the pre-gate contract only
> Runtime/external evidence: none
> Product gate: P7-D03 remains open

## Implemented

- Added `unirouter.async-media-job-plan.v1` as a strict metadata-only,
  repository-local planning contract.
- Added deterministic text-to-image and image-to-video fixtures with exact
  tenant/object/idempotency identity.
- Pinned the exact B6 endpoint-profile Schema, decision-gated GPT Image
  manifest/fixture and golden report. Image coverage remains decision gated;
  video endpoint coverage is explicitly missing.
- Kept provider, object store, retention and customer charge trigger at
  `unresolved_p7_d03`, with direct declared-gap provenance.
- Added a non-runtime lifecycle boundary proving that no queue, lease,
  execution, cancellation, settlement or terminal result exists.
- Added nine required threat classes and a dedicated threat model.
- Added an offline zero-network conformance CLI and deterministic golden
  report.

No runtime API/UI route, worker, queue, lease, object store, provider adapter,
callback, credential, persistent row, customer debt or retention policy was
added.

## Immutable Local Artifacts

| Artifact | SHA-256 |
|----------|---------|
| `internal/asyncmedia/artifacts/async-media-job.v1.schema.json` | `17afc6a64c9bc5fc1c454fc5cc777b049bfd3827725eae238bbbdd2f8d5d8d80` |
| `internal/asyncmedia/artifacts/fixtures/image-to-video-plan.v1.json` | `78dfebc8be843238cd14c6c00cdde9ea8c4371a3037f674be79e4a611d5f777f` |
| `internal/asyncmedia/artifacts/fixtures/text-to-image-plan.v1.json` | `5caeed8cc3188c45de99e3b29ceb4b758ed7e92a02e2d60f6baac06394f170fd` |
| `internal/asyncmedia/artifacts/reports/core.v1.json` | `0b88936597f24c842951994adf0c06580854eecf0a0efc38d961fb95074096b1` |

The two fixtures pass 55/55 deterministic cases across 36 distinct classes.

## Exact B6 Dependency Pins

| Dependency | SHA-256 |
|------------|---------|
| Endpoint-profile Schema | `e1546a5a7638e400f89b43ea6684e2879a072eb13f0a74a312979aea356752cc` |
| GPT Image fixture | `3b802f15539ff4fe0309ca93a779a3639da5910b0f28df3087aa24520fa16755` |
| GPT Image manifest | `792d54b5341ae13425afd24ffa864c068b705043f2b09f29c6c717963b9febd7` |
| Endpoint conformance golden | `ab3d4ec66077922ce921f1def0ccfa501ba8bf5866190c3581fa5c71a91b5759` |

## Verified Behaviors

- recursive duplicate-key, unknown field, trailing JSON and null rejection;
- canonical positive int64 tenant/object byte-size strings and overflow
  rejection;
- idempotency binding to tenant, operation, intent, immutable ordered object
  metadata and output expectation;
- cross-tenant, mutable, duplicate and digest-invalid object rejection;
- actual provider/store/retention/charge decisions rejected before P7-D03;
- raw prompt, storage locator, signed URL, credential and raw media excluded;
- runtime registration, provider network and customer charge expansion rejected;
- no lifecycle state can start before the gate;
- all nine threat classes required exactly;
- four common product gaps plus the video endpoint-profile gap preserved;
- request path, routing, billing, completed Feed, Outbox identity and Cursor
  compatibility asserted unchanged.

## Commands

```text
GOCACHE=/private/tmp/unirouter-go-cache go test ./internal/asyncmedia -count=1
GOCACHE=/private/tmp/unirouter-go-cache go run ./cmd/uniroute-async-media-conformance --report /private/tmp/unirouter-async-media-report-a.json
GOCACHE=/private/tmp/unirouter-go-cache go run ./cmd/uniroute-async-media-conformance --report /private/tmp/unirouter-async-media-report-b.json
cmp /private/tmp/unirouter-async-media-report-a.json /private/tmp/unirouter-async-media-report-b.json
cmp /private/tmp/unirouter-async-media-report-a.json internal/asyncmedia/artifacts/reports/core.v1.json
GOCACHE=/private/tmp/unirouter-go-cache go test ./...
GOCACHE=/private/tmp/unirouter-go-cache go vet ./...
node node_modules/vitest/vitest.mjs run --maxWorkers=1 --reporter json --outputFile /private/tmp/unirouter-prd7-final-vitest.json
npm run type-check
npm run build
```

The focused package, two runner invocations, byte comparisons, JSON summary,
full Go suite/vet, full frontend suite, type-check and production build all
pass. The frontend bundle emits the existing large-chunk advisory and still
builds successfully. No browser test was required because B13 intentionally
adds no UI or runtime route.

## Explicit Non-Claims

- P7-F19 is not a working asynchronous execution plane.
- The two fixture operations are contract probes, not a supported provider
  catalog or pricing promise.
- No provider or object-store selection, capacity, residency, encryption,
  retention, deletion SLA, callback authentication or media safety limit is
  claimed.
- No queue/lease/cancel/retry/finality/settlement state machine is frozen.
- No customer charge, refund or provider-liability semantics is selected.
- No deployment, network traffic, credential, database, Cursor/Checkpoint,
  push, PR or merge occurred.

Runtime design and implementation must stop until the User resolves P7-D03.
