# PRD6 Batch 0C P6-F04 Embeddings Failover Parity Evidence

> Date: 2026-07-11
> Scope: UniRouter Embeddings route/failover, billing and trace parity
> Risk: R3 request routing and billing behavior
> Evidence level: Local verified; required PR CI pending
> Deployment/target/production claim: none

## 1. Confirmed pre-change behavior

`HandleEmbeddings` used `Router.Route`, while Chat used `Router.RouteAll`.
Embeddings therefore selected exactly one candidate and had no route-level
failover. The pre-change regression run confirmed more than a missing retry:

- a first-route 429 or 503 made zero calls to the second route and returned the
  provider failure directly;
- a provider 401 returned the upstream 401 body, including its diagnostic text;
- a provider 400/401/429/503 response went through the success settlement path,
  producing one Settle and no Cancel even though no embedding was delivered;
- a two-route failure attempted only the first route and was not bounded by the
  actual candidate count because that count was never loaded;
- an Embeddings-incompatible adapter on the first route returned a terminal 500
  instead of allowing a later compatible route;
- transport failure returned immediately rather than using the P6-F03 transport
  classification;
- a malformed HTTP 200 body could reach a nil response map and panic after
  settlement.

The red command was:

```text
go test ./internal/gateway \
  -run 'TestHandleEmbeddings_(RetryClassificationAndFailover|AllRoutesFailBounded|SkipsUnsupportedAdapterAndRetriesTransport)' \
  -count=1
```

Result: FAIL in all three matrices. Observed outcomes included 429/503 instead
of fallback 200, leaked `embeddings-provider-secret`, Settle on error, zero
second-route calls and a terminal 500 for the unsupported first adapter.

## 2. Implemented behavior

- Embeddings loads the finite ordered `RouteAll` candidate set and assigns one
  distinct attempt ID plus route sequence/total to every candidate.
- Key decryption, unknown-adapter and endpoint-build failures safely skip only
  that unusable candidate; they do not expose configuration or credential
  details to the client.
- Transport and HTTP responses use the shared P6-F03 classifications. Client,
  provider-auth, cancellation and unknown deterministic failures stop;
  classified transient failures alone consume a later route.
- The trace preserves both eligibility (`retryable`) and the actual action.
  The last eligible failure records `retry_decision=stop` when no candidate
  remains rather than claiming a retry that did not occur. Chat receives the
  same accuracy correction.
- Retryable channel failures update channel/key health; rate limits and provider
  auth use the existing key cooldown semantics; a valid response restores
  channel/key health.
- A non-retryable provider response is canceled and logged once with its own
  route/status/upstream identity. If every retryable route fails, the final
  attempt is logged once and a safe generic 502 is returned.
- Successful fallback performs one Settle, produces one call log for the actual
  successful route, and returns `_uniroute.fallback=true` with the truthful
  attempt count, route count and final attempt ID.
- A malformed 200 response is validated before settlement and extension
  mutation; it returns a safe error, cancels once, never settles and does not
  panic.
- Model authorization remains before Freeze/routing. The request still performs
  one Freeze and then exactly one Settle or Cancel.

No endpoint, request/response schema, migration, queue, background dispatch,
credential, deployment or X-Insight behavior is added. P6-F05 effective
configuration truth and P6-F06 multiplier under-freeze remain separate.

## 3. Acceptance matrix exercised

| Scenario | Attempts | Downstream | Billing | Evidence |
|----------|----------|------------|---------|----------|
| first 400 | 1 of 2 | safe 400 | Cancel once | stop decision; first route/status logged |
| first 401 | 1 of 2 | safe 502 | Cancel once | provider text absent; stop decision |
| first 429, second 200 | 2 of 2 | 200 | Settle once | retry then fallback route/attempt evidence |
| first 503, second 200 | 2 of 2 | 200 | Settle once | retry then fallback route/attempt evidence |
| 503 then 429 | exactly 2 | safe 502 | Cancel once | decisions `retry`, then exhausted `stop`; final route logged |
| unsupported adapter, refused connection, then 200 | exactly 3 | 200 | Settle once | truthful attempt count 3 / route count 3 |
| malformed 200 | 1 | safe 500 | Cancel once; no Settle | no panic and parse-failure trace |

Existing Embeddings model-policy tests continue to prove rejection before
Freeze. Existing Chat retry and mixed HTTP/network final-attempt regressions are
included in the focused repeated race run to prevent endpoint parity from
regressing P6-F03.

## 4. Local verification

```text
go test -race ./internal/gateway \
  -run 'TestHandleEmbeddings|TestHandleChatCompletion_RetryClassification|TestHandleChatCompletion_UpstreamError_AllFail' \
  -count=10
```

Result: PASS, including ten repetitions of every new multi-route, policy,
malformed-response and prior Chat retry case.

```text
go vet ./...
go test -race ./... -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
```

Result: PASS for all Go packages and the isolated admin integration suite; the
latter passed in 30.582 seconds. The known non-failing macOS `LC_DYSYMTAB`
linker warning remains; the race detector reported no race.

```text
npm run type-check
npm test -- --run
npm run build
```

Result: PASS for type-check, 27 files / 137 tests and production build. Existing
React/Semi and bundle-size warnings remain non-failing. Docker is not installed
in the local shell; required CI remains the Docker evidence source.

`git diff --check` also passes.

## 5. Acceptance and remaining boundary

P6-F04 is Local verified. It can advance to CI verified / closed only after the
exact PR head passes Backend Tests, Frontend Build and Docker Build, is merged,
and post-merge `main` required CI passes.

This evidence closes neither P6-F05 nor P6-F06 and makes no target or production
claim. It also does not claim new Anthropic embeddings support: an
`anthropic_native` route remains endpoint-incompatible and is now safely skipped
in favor of a compatible candidate when one exists.
