# PRD7 B14 Async Media Image V1 Runtime Local Evidence

> Date: 2026-07-31
> Evidence level: Local verified
> Runtime/external evidence: none
> Product gate: P7-D03 resolved for repository-local Image V1 only
> Release authority: none

## Decision And Boundary

The User selected GPT Image V1, tenant-scoped S3-compatible object storage,
seven-day retention and a charge-eligibility trigger that requires both a
validated terminal provider success and durable output. The current text-token
price table is not reused for media: Image V1 records
`pricing_pending_media_rate_card` and does not debit a wallet or invent an
amount. Video remains disabled until it has an independent Endpoint Profile
and conformance evidence.

The exact repository-local contract is
`docs/architecture/44-async-media-image-v1-runtime-contract.md`. This work does
not turn Endpoint Profiles, Connector SDKs, Tool Gateway, MCP Gateway or the
completed-execution Feed into a universal high-privilege plugin runtime.

## Implemented

- Added strict public `POST/GET/cancel/content` Image V1 job endpoints and
  project-scoped JWT administrator list/detail/cancel/content endpoints.
- Added a no-redirect Provider HTTP boundary; a 3xx response cannot cause a
  second credential-bearing request.
- Bound the 32 MiB decoded-image and Base64 JSON-envelope ceilings in the Image
  V1 Endpoint Profile and reused them in Provider and object persistence.
- Added org/project/API-key/idempotency HMAC identity and a separate normalized
  request SHA-256. Exact replay is checked atomically before admission and
  capacity; semantic conflicts do not mutate the original job.
- Added a guarded PostgreSQL migration for orthogonal admission, queue, lease,
  execution, provider outcome, cancellation, object and settlement axes,
  monotonic fences, encrypted queued request bytes and immutable charge facts.
- Added `FOR UPDATE SKIP LOCKED` worker claims, bounded leases/attempts,
  pre-dispatch cancellation, retry classification, recovery-only processing
  after the durable provider-success fence and fail-closed unknown outcomes
  after ambiguous dispatch, unclassified Provider errors or lease expiry. A
  lost fence-write response is accepted only after exact live-lease read-back.
- Added immutable tenant-derived S3-compatible image and recovery-receipt
  objects. Callers cannot supply or read locators. Recovery checks exact
  receipt/metadata equivalence, strict JSON, content hashes and usage
  partitions; purge re-derives the expected keys and fails closed on locator
  drift.
- Added seven-day read expiry and a separate purge lease. Reads fail closed at
  `expires_at` even if physical deletion is temporarily pending.
- Added four provider-observed image unit classes to an amount-free charge
  fact only after output and receipt persistence. Provider success without a
  recoverable output remains non-eligible.
- Added independent runtime-readiness and new-admission controls, plus atomic
  limits of 10 nonterminal jobs per project and 100 per organization. Both
  controls default false; runtime readiness never implicitly enables admission.
- Added the Media Jobs console with project scope, lifecycle axes, retention
  and charge facts. The Inspector is rendered without a detail-open action;
  output bytes require a separate authenticated action and are revoked at
  expiry or selection change.
- Updated the GPT Image Endpoint Profile evidence state to
  `repository_local_image_v1` while keeping `network_verified=false`.
  Responses and rerank remain contract-only.

## Exact Artifact Identities

| Artifact | SHA-256 |
|----------|---------|
| `internal/endpointprotocol/artifacts/endpoint-profile.v1.schema.json` | `75a3f5a6bf601c8b041f2f1f6aef00f0a97e55d72422d1cb0222b9d74391222f` |
| `internal/endpointprotocol/artifacts/manifests/openai-gpt-image.v1.json` | `deef774305f8b6210c011d6e606420bd6cce2f215bc3d779effbdca217f7ed0c` |
| `internal/endpointprotocol/artifacts/fixtures/openai-gpt-image-v1.json` | `3b802f15539ff4fe0309ca93a779a3639da5910b0f28df3087aa24520fa16755` |
| `internal/endpointprotocol/artifacts/reports/core.v1.json` | `135874494377e6ec1b25a300c4983d3248a5b0a74c8df9c4d88efbad915aa214` |
| `migrations/000046_v1_async_media_image_jobs.up.sql` | `63b6abcf70df6ee928651652e631da85942f1854bbf49c1b5e9f77ed77e2411c` |
| `migrations/000046_v1_async_media_image_jobs.down.sql` | `236d944e1bd6c525b9e7aa5e48847c1e1cf9a7d222c59eeb8ef465b023337d39` |

The historical B13 artifacts remain byte-identical:

| Historical artifact | SHA-256 |
|---------------------|---------|
| `internal/asyncmedia/artifacts/async-media-job.v1.schema.json` | `17afc6a64c9bc5fc1c454fc5cc777b049bfd3827725eae238bbbdd2f8d5d8d80` |
| `internal/asyncmedia/artifacts/fixtures/image-to-video-plan.v1.json` | `78dfebc8be843238cd14c6c00cdde9ea8c4371a3037f674be79e4a611d5f777f` |
| `internal/asyncmedia/artifacts/fixtures/text-to-image-plan.v1.json` | `5caeed8cc3188c45de99e3b29ceb4b758ed7e92a02e2d60f6baac06394f170fd` |
| `internal/asyncmedia/artifacts/reports/core.v1.json` | `0b88936597f24c842951994adf0c06580854eecf0a0efc38d961fb95074096b1` |

B13 is an immutable historical planning snapshot; its former
`unresolved_p7_d03` values are not dynamically reinterpreted after the
decision.

## Verified Behaviors

- strict nested duplicate-key, unknown-field, null, trailing-byte, model,
  one-image, no-stream and prompt-size rejection;
- tenant/project/API-key idempotency isolation, exact replay during paused
  admission or full capacity, and request-byte conflict rejection;
- precise `model_not_allowed`, `model_authority_unavailable` and
  `model_authority_failed` boundaries without leaking internal errors;
- Provider redirects are rejected without a second request;
- project and organization capacity under real PostgreSQL transactions;
- lease fencing, retry timing, stale-worker rejection, cancellation races,
  unclassified/unknown provider outcomes and no redispatch after ambiguous
  execution;
- provider-success fence before object persistence, exact live-lease read-back
  after an ambiguous fence response, strict full receipt/metadata recovery,
  locator/usage-tamper rejection, output-failure non-settlement and purge
  fencing;
- prompt ciphertext erased at every terminal state;
- charge fact requires validated provider success plus durable image and
  receipt; no wallet transaction is created;
- output is unreadable after expiry and object/receipt locators never enter API
  responses;
- public/admin route, frontend client, OpenAPI and API Docs parity;
- runtime disabled and admission paused remain distinct operator states;
- API-key hash secret, bucket and normalized prefix identity cannot be rotated
  in place while unexpired jobs remain.

## Commands And Results

```text
GOCACHE=/tmp/unirouter-go-build-cache go test ./... -count=1
GOCACHE=/tmp/unirouter-go-build-cache go test -race ./internal/mediajob -count=1
GOCACHE=/tmp/unirouter-go-build-cache go vet ./...
REQUIRE_TEST_DATABASE=1 GOCACHE=/tmp/unirouter-go-build-cache \
  go test -tags=integration ./internal/mediajob ./internal/migrate \
  -run 'Test(PostgresStore|AsyncMediaImageMigration)' -count=1
GOCACHE=/tmp/unirouter-go-build-cache go test ./cmd/uniroute \
  -run 'Test(OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage|AdminRoutesRegisteredForFrontendClient)' \
  -count=1 -v
npm test -- --pool=forks --poolOptions.forks.singleFork=true
npm run type-check
npm run build
ruby -e 'require "yaml"; ARGV.each { |p| YAML.load_file(p) }' \
  docs/tech-ref/05-openapi-spec.yaml docker-compose.yml \
  docker-compose.canary.yml deploy/customer/docker-compose.yml
git diff --check
```

Results:

- full Go tests and `go vet` passed;
- media-job race tests passed; the macOS linker emitted a non-failing
  `LC_DYSYMTAB` advisory;
- real isolated-PostgreSQL media lifecycle/capacity and migration/rollback
  integration passed;
- all four route/OpenAPI/frontend-doc parity tests passed;
- frontend regression passed 48/48 files and 336/336 tests;
- frontend type-check and production build passed; the existing large-chunk
  advisory remains non-failing;
- OpenAPI and all three Compose documents parsed successfully;
- final diff whitespace validation passed.

The pre-final environment audit found eight stale `uniroute_it_*` databases
from interrupted earlier test processes. Each was verified as owned by
`uniroute`, with zero active connections, and dropped by exact name. A fresh
full integration run then exited normally and the immediate
`pg_database LIKE 'uniroute_it_%'` query returned no rows, proving the normal
test cleanup path.

## Browser Evidence

An isolated headed Chromium session used only same-session static API mocks
and a non-secret token. No real backend or provider was contacted.

| Viewport | Result |
|----------|--------|
| 1440 x 1000 | two-column queue plus right Inspector; document `1440/1440`; Inspector `clientWidth/scrollWidth=472/472`; 0 console errors |
| 1024 x 900 | queue plus always-rendered stacked Inspector; document `1024/1024`; Inspector right edge `995`; no clipping |
| 390 x 844 | mobile navigation plus always-rendered stacked Inspector; document `390/390`; both panels `356/356`; all Description tables `320/320`; no horizontal overflow |

The browser pass found and closed two real defects: the 1024px fixed-minimum
grid clipped the right Inspector, and the 390px Semi Descriptions table hid
long identities behind internal horizontal scrolling. The final console has
zero product errors. Two React Router v7 future-flag warnings remain.

## Compatibility Conclusion

The implementation adds separate `async_media_jobs` and
`async_media_charge_facts` state. It does not change existing request records,
Chat/Embeddings routing, customer wallet or ledger results, Outbox identities,
completed-execution DTO/source bytes, Feed endpoints/pages/credentials, Cursor
or Checkpoint behavior. Full repository regression and immutable B13 artifact
hashes support the repository-local compatibility claim; no target or live
compatibility claim is made.

## Explicit Non-Claims

- no target migration, deployment, worker process, S3 bucket/lifecycle policy,
  credential or production configuration was applied;
- no real GPT Image or object-store request, provider bill, callback, paid
  traffic or customer charge was observed;
- seven-day read expiry is locally enforced, but no target physical-deletion
  SLA or defense-in-depth bucket lifecycle is proved;
- no customer media price, refund, reconciliation or wallet-debit behavior is
  implemented;
- no video, image editing/variation, multiple output, streaming, public URL,
  marketplace, Connector runtime or generic code execution is enabled;
- no CI, Target, External integration or Production evidence is claimed;
- no push, pull request, merge, release or deployment occurred.

Any target activation requires separate P7-D05 credential/account authority
and P7-D06 migration/deployment/traffic/release authority.
