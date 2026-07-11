# PRD6 Batch 0C P6-STREAM-002A Stream Transport Evidence

> Date: 2026-07-12
> Evidence level: Local verified; PR and required CI pending
> Base: `1103f4bd6afef9f55ad7a1fcb4be8bd78031ae44`
> Branch: `codex/prd6-stream-transport`
> Scope: transport prerequisite only; P6-F11 remains open

## 1. Confirmed Pre-Change Findings

- `cmd/uniroute/main.go` configured a 120-second server `WriteTimeout` while
  streaming provider requests could run for 300 seconds.
- `internal/gateway/relay.go` limited `bufio.Scanner` to 64 KiB and logged a
  scanner error without converting it into a failed stream terminal. The code
  could continue through settlement and emit a success extension plus `[DONE]`.
- heartbeat writes and response flushing were not uniformly inside one writer
  critical section.
- multiple branches closed the provider response body, while detailed final
  evidence could reuse a canceled request context.
- the outer metrics response writer preserved `http.Flusher` but did not expose
  `Unwrap()`. Consequently `http.ResponseController` could not reach the native
  server writer to renew a per-write deadline beyond the aggregate server
  timeout.
- the periodic balance check reads wallet balance without accounting for the
  current request reservation. This separate financial issue is not repaired by
  this transport slice.

These are repository/source findings. They are not target, load, 100x or
production observations.

## 2. Implemented Transport Contract

- `stream-transport.v1` is an immutable, startup-validated policy with defaults:
  2 MiB maximum SSE line, 30-second write-idle timeout, 15-second heartbeat and
  30-minute maximum provider stream duration.
- unsafe line, heartbeat/write-timeout and duration bounds fail startup
  validation. The effective values are available through Runtime Status and the
  administrator UI without exposing secrets.
- provider stream lifetime uses the configured maximum duration; each
  downstream write renews a write deadline and serializes `Write` plus `Flush`
  in one critical section.
- metrics and idempotency wrappers preserve the ResponseController unwrap chain,
  so the per-write deadline reaches the native server writer while non-stream
  handlers retain the existing global timeout.
- Scanner accepts bounded frames above 64 KiB. An over-limit frame emits
  `stream_error.frame_too_large`, omits the success extension and `[DONE]`, and
  cancels or settles according to whether billable output crossed the durable
  marker.
- upstream read failure after delivered output persists final usage, settles
  delivered usage, records `settled_partial_stream`, emits a safe terminal error
  when the downstream is still writable, and never advertises success.
- the provider body has one idempotent lifecycle owner and is released before
  finalization. Final evidence receives a fresh bounded 30-second context.
- final extension or `[DONE]` write failure records a sanitized
  `stream.terminated` event; it does not undo an already durable settlement.
- `.env.example`, Compose passthrough, Runtime Status UI and a dedicated required
  Backend race step expose and guard the same policy.

## 3. Local Verification

Passed on 2026-07-12:

```text
go test ./internal/streampolicy ./internal/config ./internal/runtimeconfig \
  ./internal/traceutil ./internal/middleware ./internal/admin ./internal/gateway \
  -run 'Test(P6StreamTransport|SSEStreamWriter|DefaultValid|ValidateRejectsUnsafeBounds|Load_StreamPolicy|NewBuildsTypedEffectiveConfiguration|NewRejectsInvalidStream|SanitizeMetadata_StreamPolicy|MetricsMiddleware_ResponseController|RuntimeStatus_ReturnsRuntimeHealth)' \
  -count=1

PASS: 7 packages

go test -race ./internal/gateway ./internal/middleware \
  -run 'Test(P6StreamTransport|SSEStreamWriter|MetricsMiddleware_ResponseController)' \
  -count=1

PASS: gateway and middleware

npm test -- --run src/pages/__tests__/InternalStatusPage.test.tsx
PASS: 1 file / 1 test

npm run type-check
PASS

git diff --check
PASS
```

Full repository and required local gates also passed:

```text
go vet ./...
PASS

go test -race ./... -count=1
PASS: every Go package

REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
PASS

REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/gateway \
  -run TestRoutingPolicyPostgresStateTransitions -count=1
PASS

REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/billing \
  -run TestEventService_CheckProjectBudgetTenantScope_Integration -count=1
PASS

go test -race ./internal/billing -run TestP6F06 -count=1
PASS

npm test -- --run
PASS: 27 files / 137 tests

npm run type-check
PASS

npm run build
PASS

Go route/OpenAPI/frontend-doc parity tests
PASS

GitHub Actions and Compose YAML parse
PASS
```

The local machine does not have a `docker` CLI, so no local image-build claim is
made. `Docker Build` remains an independent required GitHub job and must pass on
the exact PR head before merge.

Focused fixtures prove 64-KiB-plus-one and 1-MiB SSE lines, a configured
over-limit frame, upstream read failure after partial delivery, final downstream
write failure, a normalized content delta that fails before delivery and is not
added to fallback completion-token settlement, exact one-time body close and
concurrent writer serialization.

## 4. Explicit Non-Claims

This slice does not implement or claim:

- D6-10 reservation-aware dynamic hard-budget extension;
- P6-F11 cross-chunk semantic inspection, guardrail buffering, measured
  disclosure or protocol-specific policy termination;
- P6-F20 process-kill journal/outbox guarantees;
- target deployment, proxy/load-balancer timeout compatibility, real-provider
  long-stream evidence, multi-replica capacity or production verification;
- any X-Insight runtime dependency or contract change.

P6-STREAM-002 can be promoted from Local to CI verified only after the exact PR
head passes `Backend Tests`, `Frontend Build` and `Docker Build`. P6-F11 remains
in progress until its separate policy/interception acceptance evidence closes.
