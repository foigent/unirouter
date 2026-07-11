# PRD6 Batch 1 P6-F19B Agent Ancestor Budget — Local Evidence

> Date: 2026-07-12
> Scope: repository/local evidence only
> Status: locally verified; pull request and required CI pending
> Parent row: P6-F19 is ready to close only after exact-head and post-merge CI

## Accepted contract exercised

- A capability-authenticated Chat or Embeddings request reserves its estimated
  cost and tokens against the calling Agent and every signed ancestor before
  UniRouter selects or calls an upstream provider.
- The same atomic Redis Lua admission applies the Agent and every ancestor's
  exact rolling RPS, exact rolling RPM and shared in-flight concurrency limit.
  Sibling Agents therefore cannot multiply a parent's quota.
- Cost/token reservations and concurrency are released on a pre-output billing
  cancellation. RPM/RPS represent admission attempts and are not refunded.
- Successful billing settlement replaces every ancestor's estimate with actual
  prompt-plus-completion tokens and actual calculated cost. An anomalous actual
  overrun marks the lineage exhausted so no later request can compound it.
- The existing root API-key limiter and wallet remain aggregate outer
  boundaries. Ordinary root API-key traffic keeps its existing behavior; the
  new shared ancestor state applies only when authentication produced a signed
  Agent capability.
- Redis `TIME`, rather than an application-replica clock, drives sliding windows
  and concurrency lease deadlines. Every workflow key uses the same Redis
  Cluster hash tag, and operation identifiers are stored only as domain-
  separated hashes.
- The concurrency lease covers the endpoint's maximum transport lifetime plus
  a five-minute terminal grace. Redis state survives a further five minutes for
  bounded finalization. A capability token may expire after admission without
  killing that admitted request; an expired token cannot create a new
  reservation.
- Runtime budget, identity and revocation uncertainty fails closed. If shared
  admission fails after the durable wallet freeze, UniRouter cancels that
  freeze and does not select or call an upstream provider.

## Repository implementation evidence

- `internal/agentcap/invocation.go` defines the server-owned reservation and
  terminal API. It contains no bearer token and grants no Tool, IAM, database,
  write or export authority.
- `internal/agentcap/invocation_store.go` provides one atomic lineage admission
  script plus idempotent settle/cancel scripts. Registered root/child state now
  retains the signed cost, token, rate and concurrency ceilings and aggregate
  reserved/used counters.
- `internal/capbilling/` wraps the existing durable billing provider. It joins
  a successful Freeze to shared admission and joins Settle/Cancel to the
  ancestor terminal state without changing the PostgreSQL wallet state
  machine.
- Chat uses its configured stream maximum (or the existing five-minute non-
  stream timeout) as the lease input; Embeddings uses its existing 120-second
  timeout. Typed capability denials return 401/403/409/429/503 with stable docs
  codes and `Retry-After` where a retry window exists.
- Runtime status and Internal Status UI expose that ancestor budgets and
  subtree rate/concurrency are enforced. OpenAPI documents the shared-subtree
  meaning of the limits and the new gateway error statuses.

## Local verification

Passed:

```text
go vet ./...
go test -race -coverprofile=coverage.out ./...
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/gateway -run TestRoutingPolicyPostgresStateTransitions -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/billing -run TestEventService_CheckProjectBudgetTenantScope_Integration -count=1
REQUIRE_TEST_DATABASE=1 go test -race ./internal/billing -run TestP6F06 -count=1
go test -race ./internal/gateway ./internal/middleware -run Test(P6StreamTransport|SSEStreamWriter|MetricsMiddleware_ResponseController) -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/agentcap -count=1
npm test -- --run                         # 27 files / 137 tests
npm run type-check
npm run build
ruby YAML.load_file(OpenAPI)              # parsed successfully
git diff --check
```

High-risk fixtures include:

- two sibling Agents consume one root's shared used-plus-reserved cost/token
  ceiling; cancellation releases reservations without refunding prior usage;
- 20 concurrent admissions split across two service instances under a limit of
  three produce exactly three successes and 17 concurrency denials;
- duplicate server operation admission is idempotent and does not consume rate
  or budget twice;
- cancelled work continues to count in the one-second and one-minute windows;
- concurrency is released on terminal cancellation and its lease deadline is
  proven to use Redis time despite a two-hour application-clock skew;
- a real Redis 7 plus migrated PostgreSQL integration fixture admits exactly
  two concurrent operations, rejects the third, settles actual usage and reads
  back zero outstanding reservations; and
- Redis outage denies admission, while actual-cost overrun exhausts all future
  lineage admissions.

## Explicit non-claims and remaining work

- This slice is sufficient to close P6-F19 only after pull-request and post-
  merge required CI are green. It does **not** close P6-F07: selector
  precedence, disabled/shadow/enforce modes, general API-key TPM and shared
  policy-version evidence remain separate.
- D6-10 dynamic long-stream reservation extension is not implemented here.
  This slice reserves the current estimated token/freeze envelope; P6-F11 must
  still extend before its guard band, stop upstream on extension failure and
  settle only delivered output.
- The normal request process joins billing and ancestor terminal transitions.
  The existing durable settlement recovery worker intentionally still owns the
  underlying billing service. If a process dies or recovery wins after an
  ambiguous immediate settlement attempt, the ancestor reservation is retained
  conservatively until its bounded Redis TTL instead of being incorrectly
  refunded. A durable cross-process terminal hook/outbox remains P6-F20.
- P6-F21 still owns general workload weights, replica-invariant global/org
  admission and short/long-work fairness. F19B enforces only the signed Agent
  lineage's own concurrency ceiling.
- A deployment must not claim F19B enforcement until all serving replicas run
  this source and supervisors re-exchange workflows created by a pre-F19B
  binary. No deployment or rolling-upgrade action is authorized or performed in
  this repository-only slice.
- No target deployment, paid/real upstream request, credential mutation,
  additional data access, exporter/outbox enablement or X-Insight change is
  performed or claimed.

## Pull request and required CI

Pending. Exact implementation head, PR run, squash merge and post-merge main run
will be appended only after readback from GitHub.
