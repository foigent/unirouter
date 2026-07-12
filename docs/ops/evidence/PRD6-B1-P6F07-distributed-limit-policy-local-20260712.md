# PRD6 Batch 1 P6-F07 Distributed Limit Policy — Local Evidence

> Date: 2026-07-12
> Scope: repository/local evidence only
> Status: Local verified; required CI pending
> Parent row: P6-F07

## Accepted contract exercised

- Chat and Embeddings pass server-owned organization, project, API-key and
  model identity into one Redis-backed admission service before the durable
  wallet freeze and before any upstream selection or dispatch.
- Every matching global, organization, project, API-key, model and API-key
  policy-label selector applies. Selector fields inside one policy are ANDed;
  a more specific policy can only tighten, never override or widen, another
  matching policy.
- Existing API-key RPS/RPM limits are now built-in shared policies for model
  calls. Built-in shared concurrency remains 100 globally and 10 per
  organization until P6-F21 adds accepted workload weighting and fairness.
  The control API and UI expose these values instead of hiding them.
- Explicit policies support disabled, shadow and enforce modes plus exact
  rolling RPS/RPM, reserved-then-actual TPM and expiring shared concurrency.
  Shadow mode records `would_deny` without blocking.
- `fail_closed` rejects unverifiable shared state with a typed 503. The
  accepted capacity-only `last_known_safe` mode can consume only one locally
  pre-issued admission per policy population and replica, expires in about two
  seconds, and is issued to at most five concurrently healthy replica IDs for
  that population. It is not a general fail-open mode.
- Request attempts remain in RPS/RPM windows after cancellation. A
  pre-dispatch rollback releases TPM and concurrency; successful settlement
  replaces reserved TPM with actual prompt-plus-completion usage. Actual
  overrun remains visible and prevents later compounding.
- Redis `TIME` drives windows and leases. Every policy population uses one
  Redis Cluster hash tag across its rate, token, concurrency, operation and
  fallback keys. Server operation identity is hashed and idempotently bound to
  the authoritative subject and reservation shape.

## Repository implementation evidence

- Migration `000024_prd6_distributed_limit_policy` adds server-owned API-key
  policy labels and versioned policies. Its down migration removes its ledger
  entry for safe roll-forward and refuses to drop the schema once any policy
  or label exists, preventing silent configuration loss or quota widening.
- `internal/limitpolicy/` implements indexed snapshots, exact Redis Lua
  admission/terminal transitions, authoritative labels, a maximum 32-policy
  chain, 30-second snapshot staleness denial, low-cardinality metrics, bounded
  fallback state and selector-stable runtime buckets.
- Limit-only changes rotate the policy-set evidence fingerprint without
  resetting rolling counters. Selector changes rotate the runtime population
  so one tenant/scope cannot inherit another scope's usage.
- `internal/policybilling/` composes the shared policy outside the existing
  Agent ancestor and durable wallet providers. Policy denial happens before
  wallet mutation. Terminal uncertainty retains Redis quota conservatively for
  its bounded TTL without leaking in-process tracking entries.
- Chat reserves estimated input plus the allowed completion ceiling;
  Embeddings reserves input only. Both return typed 409/429/503 errors,
  `Retry-After`, and `X-UniRouter-Limit-Policy-Version`. CORS exposes the
  version header to browser clients.
- One sanitized `limit_policy.admission` trace event records the policy-set
  fingerprint, compact policy chain, result/reason and bounded counts. It does
  not persist prompt, response, credentials or arbitrary caller metadata.
  Aggregate evidence distinguishes normal allow, degraded last-known-safe or
  shadow-state allow, shadow-would-deny and enforced rejection.
- The control plane provides tenant-scoped list/create/update APIs. Platform
  and organization administrators may mutate their allowed scope; billing
  administrators are read-only. Updates require the version last read and
  return 409 on a stale edit, preventing silent lost updates and misleading
  audit readback.
- The UI explains RPS/RPM/TPM/concurrency, shadow-first rollout, selector AND
  semantics, built-in hard limits and both failure modes in non-technical
  language. Policy labels are assigned only by administrators and are never
  accepted from gateway headers or Agent claims.

## Local verification

Passed on the implementation worktree:

```text
go vet ./...
go test -race -coverprofile=coverage.out ./...
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/gateway -run TestRoutingPolicyPostgresStateTransitions -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/billing -run TestEventService_CheckProjectBudgetTenantScope_Integration -count=1
REQUIRE_TEST_DATABASE=1 go test -race ./internal/billing -run TestP6F06 -count=1
go test -race ./internal/gateway ./internal/middleware -run 'Test(P6StreamTransport|SSEStreamWriter|MetricsMiddleware_ResponseController)' -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/agentcap -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/limitpolicy -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -run TestP6F07DistributedLimitPolicyMigrationUpDownGuardAndRollForward -count=1
npm test -- --run                         # 28 files / 141 tests
npm run type-check
npm run build
ruby YAML.load_file(OpenAPI and CI workflow)
git diff --check
```

High-risk fixtures prove:

- 20 concurrent calls split across two service instances under a shared
  concurrency limit of three produce exactly three admissions and 17 denials;
- a migrated PostgreSQL policy and server-owned label are read by two services
  sharing real Redis 7, while caller-supplied labels cannot remove or alter the
  authoritative selector or idempotency identity;
- TPM reserves input plus output, denies over-capacity work, settles down to
  actual usage and records actual overrun;
- exact RPS/RPM boundaries, shadow `would_deny`, cancellation, idempotent
  replay, Redis Cluster slot identity, stale snapshot, oversized chain and
  fail-closed behavior all pass race-enabled tests; an explicit-policy denial
  still consumes the caller's API-key attempt window, preventing rejected-work
  amplification from bypassing per-key rate control;
- a healthy last-known-safe lease allows exactly one concurrent fallback
  consumer, a second is rejected, and expired local entries are pruned;
- policy create/list/update/readback, platform-global visibility, tenant
  isolation, billing read-only routing, optimistic version conflict and audit
  rows pass against migrated PostgreSQL; and
- schema-only down→up succeeds, while a configured policy/label makes down
  fail and preserves both schema and data.

## Explicit non-claims and remaining work

- P6-F07 does not close P6-F21. Weighted workload classes, bounded waiting,
  short/long-work fairness and final shared admission tuning remain separate.
- P6-F07 does not implement D6-10/P6-F11 dynamic long-stream token extension.
  The current request reserves its estimated envelope and settles actual usage;
  extension before a guard band remains required for that row.
- P6-F07 does not close P6-F20. If a process dies or durable billing recovery
  wins after an ambiguous immediate terminal attempt, policy state remains
  conservatively held until Redis TTL. A durable cross-process terminal hook
  and bounded replay worker remain P6-F20.
- This source changes model-call API-key rate counters from the legacy
  `rl:qps`/`rl:rpm` family to cluster-safe policy-population keys. A later
  authorized gateway cutover must drain old model-serving binaries and use a
  non-rolling cutover, or separately implement and verify a dual-write bridge.
  No such deployment is authorized or performed here.
- The retained legacy limiter protects Agent capability control endpoints and
  now fails closed with the same typed error family. It is not used for Chat or
  Embeddings after this source cutover.
- No throughput/capacity claim for a target environment is made. Target
  replica count, tenant cardinality, Redis topology and workload mix still need
  separately authorized capacity evidence.
- No target deployment, paid/real upstream request, credential mutation,
  additional data access, exporter/outbox enablement, response cache or
  X-Insight change is performed or claimed.

## Pull request and required CI

- implementation head: pending;
- PR and exact-head required run: pending;
- squash merge and post-merge `main` run: pending; and
- historical implementation branch must be restored at the exact head after
  squash merge.
