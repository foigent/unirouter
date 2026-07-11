# PRD6 Batch 0C P6-F03 Retry Classification Evidence

> Date: 2026-07-11
> Scope: UniRouter Chat provider retry/failover correctness
> Risk: R3 request routing and billing behavior
> Evidence level: CI verified / closed
> Deployment/target/production claim: none

## 1. Confirmed pre-change defect

The Chat relay asked each provider adapter to classify non-200 responses, but
the route loop ignored `Retryable` and unconditionally continued to the next
channel. As a result:

- deterministic client failures such as HTTP 400 could be sent to every
  configured channel and finally rewritten as a misleading 502;
- provider authentication failures such as 401/403 also continued to another
  route;
- every `http.Client.Do` error was treated as a channel failure, including
  caller cancellation and deterministic transport/configuration errors;
- provider error text was logged and copied into provider-failure notification
  payloads without a bounded safe-message contract.

The loop was already bounded by the finite `RouteAll` result. The defect was
which classified failures were eligible to consume the remaining routes, not
an unbounded retry loop.

## 2. Implemented behavior

- OpenAI-compatible and Anthropic-native adapters now share one HTTP status
  matrix: 408, 429, 500, 502, 503 and 504 are retryable; Anthropic 529 remains
  retryable; all other 4xx responses are non-retryable client/auth failures;
  unknown status classes fail closed as non-retryable.
- Transport classification distinguishes retryable deadline/connection reset,
  refusal, EOF, broken-pipe and temporary network failures from caller
  cancellation and unknown deterministic transport/configuration failures.
- Chat failover now continues only when the classification says `Retryable`.
- A non-retryable upstream client response preserves its safe HTTP status and
  returns a generic OpenAI-compatible error. Upstream auth/unknown failures
  return a generic 502 so clients are not told that their own credential is
  invalid. Provider diagnostic bodies are not returned or persisted in
  notification messages.
- Every provider rejection trace records the safe `failure_kind`, `retryable`
  value and `retry_decision` (`retry` or `stop`). These fields are explicitly
  allowed by metadata sanitization; raw prompt, response and provider error
  content remain excluded.
- Billing remains one Freeze before routing, then exactly one Settle after a
  successful failover or exactly one Cancel after a terminal failure. A failed
  terminal attempt receives one call-log record with its own route, status,
  attempt and upstream identity.
- Existing local content-policy and API-key model-policy rejection remains
  before billing Freeze and routing.

This change does not add retries, queues, background dispatch, a new endpoint,
an API schema, a migration, a credential operation or deployment behavior.
Embeddings parity remains separately scoped as P6-F04.

## 3. Acceptance matrix exercised

The handler regression matrix uses two independently observable upstreams:

| First outcome | Expected second-channel calls | Downstream outcome | Billing terminal action |
|---------------|-------------------------------|--------------------|-------------------------|
| 400 with secret provider text | 0 | safe 400 | Cancel once |
| 401 with secret provider text | 0 | safe 502 | Cancel once |
| 501 unknown classification | 0 | safe 502 | Cancel once |
| 429 | 1 | second route succeeds with 200 | Settle once |
| 503 | 1 | second route succeeds with 200 | Settle once |

The same matrix verifies one Freeze, one call log, no provider-secret marker in
the client response, correct failed-attempt identity, and persisted trace
decision metadata. The existing two-route all-fail regression proves bounded
exhaustion and final-attempt status/upstream identity isolation. Adapter unit
tests additionally cover 400/401/403/404/409/413/422, 408/429,
500/502/503/504, 501/599 and Anthropic 529, plus cancellation, deadlines, EOF,
connection reset and deterministic TLS-style failures.

## 4. Local verification

```text
go test ./internal/adapters ./internal/traceutil ./internal/gateway \
  -run 'TestClassify|TestSanitizeMetadata|TestHandleChatCompletion_RetryClassification|TestHandleChatCompletion_UpstreamError_AllFail|TestHandleChatCompletion_SensitiveContent|TestHandleChatCompletion_ModelNotAllowed' \
  -count=1
```

Result: PASS for all three packages.

The same focused adapter/trace/handler matrix also passed with
`-race -count=10`, including ten repetitions of every two-route retry decision
case.

```text
go vet ./...
go test -race ./... -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
```

Result: PASS for all Go packages and the complete isolated admin integration
suite. The latter passed in 29.603 seconds. The macOS linker retained the known
non-failing `LC_DYSYMTAB` warning; the race detector reported no race.

```text
npm run type-check
npm test -- --run
npm run build
```

Result: PASS for type-check, 27 files / 137 frontend tests and production
build. Existing React/Semi warnings and the bundle-size warning remain
non-failing. Docker is not installed in the local shell, so Docker evidence is
reserved for the required `Docker Build` job rather than inferred.

`git diff --check` also passes.

## 5. Acceptance and remaining boundary

PR #156 implementation head `34e916ee7efb09d98103d86ba36634cfd083f080`
passed Backend Tests, Frontend Build and Docker Build in required run
`29158184240`. It was squash-merged as
`f6f075aa4333f2b81f888f3505c2c2b44e77c449`; post-merge main run
`29158395734` passed the same three jobs. P6-F03 is CI verified / closed.

This evidence does not close P6-F04 Embeddings failover parity, P6-F05 effective
configuration truth, P6-F06 multiplier under-freeze delta, any later Batch 0C
row, target deployment or production behavior.
