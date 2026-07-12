# PRD6 Batch 2 P6-F08 Routing Policy V1 — Repository And CI Evidence

> Date: 2026-07-12
> Owner: Codex
> Branch: `codex/prd6-routing-policy-v1`
> Base: post-P6-F21 closeout `main` at `7df87ce476a7b56a8dcc1fee48dfb3f0c6b86413`
> Evidence level: CI verified / closed; no target, production or capacity claim
> Scope: UniRouter repository only; no deployment, target mutation, paid request,
> credential change, external data access, exporter/outbox or X-Insight runtime work

## 1. Confirmed Pre-Change Findings

The following were source-confirmed, not hypotheses:

1. Every routing operation queried PostgreSQL directly and received rows in
   priority/key order. There was no bounded immutable route snapshot or
   last-known-good failure posture.
2. Equal-priority channels had no weight. Every replica saw the same ordered
   first channel, creating a first-channel concentration risk.
3. Half-open admission used a process-local random percentage. Multiple
   replicas could independently dispatch recovery probes.
4. Channel success/failure and key cooldown/failure writes had no captured
   epoch/CAS. A late success could overwrite a newer circuit/control change.
5. The existing per-channel `degradation_threshold` was visible in the admin
   UI but did not drive gateway breaker transitions.
6. Chat and Embeddings already shared classified bounded retry/failover from
   P6-F03/P6-F04, but neither had a versioned candidate/exclusion plan.

## 2. Implemented Contract

### 2.1 Static, deterministic routing

- `routing-policy.v1` first sorts priority tiers from smaller to larger.
- The embedded startup contract advances additively from `routing-config.v1`
  to `routing-config.v2`; the outer `effective-config.v1` API remains stable.
- Within one equal-priority tier, `routing_weight` (1-1000, default 100)
  controls a deterministic weighted permutation derived from the server-owned
  operation/request ID. A client cannot provide a separate routing hint.
- The same operation ID produces the same route plan on different replicas.
- One deterministically selected active key is used per channel, so adding
  keys does not accidentally multiply channel weight.
- Chat and Embeddings both call `RouteAllWithKey(..., requestID)` and retain
  the existing classified retry budget.
- Latency-, cost-, quota- and canary-adaptive routing remain P6-F14 advanced
  scope and are not silently implemented here.

### 2.2 Bounded immutable snapshots

- Each replica singleflights one per-model PostgreSQL snapshot refresh.
- Default refresh TTL is one second.
- A refresh failure may use the immutable last-known-good snapshot for at most
  30 seconds by default; after that routing fails closed.
- Candidate count is bounded at 32 by default.
- Per-replica model snapshots are oldest-first bounded at 1,024 by default,
  and overlong model identifiers are rejected before querying PostgreSQL.
- Healthy response writes do not invalidate the snapshot, avoiding a database
  query per successful request. State transitions, failures, cooldowns and
  operator mutations invalidate the local snapshot; other replicas converge
  through the one-second refresh bound.
- One PostgreSQL `NOW()` value is read with the snapshot so every key/channel
  makes the same cooldown and recovery-deadline decision.

### 2.3 Breaker, key state and probe ownership

- `routing_epoch` invalidates results from an older operator configuration.
- `circuit_epoch` invalidates results from an older breaker generation.
- `channel_keys.state_epoch` invalidates results from an older key status or
  cooldown generation.
- The channel-specific `degradation_threshold` now drives the breaker; the
  startup threshold is only a compatibility fallback for invalid/legacy rows.
- A half-open route must atomically claim a PostgreSQL token/expiry lease just
  before provider dispatch. Only one replica can claim it.
- The lease is renewed while a slow upstream request/stream remains active,
  so an operation longer than the base lease does not create a second live
  probe. Lease expiry still recovers a crashed owner.
- Probe success/failure clears ownership and advances the circuit epoch.
- If every planned route becomes unusable before provider dispatch (for
  example, another replica owns the only probe), the gateway cancels the
  reservation and returns 503 with `Retry-After` instead of a misleading 502.
- Streaming HTTP 200 headers alone no longer heal a channel. A valid normalized
  data frame or valid terminal frame is required; invalid/oversized/incomplete
  upstream frames terminate safely and are recorded as channel failures.

### 2.4 Operator surface and evidence

- Platform operators can set priority, equal-priority weight and breaker
  threshold with plain-language UI help.
- UI writes send `expected_routing_epoch`; stale writes receive HTTP 409.
  API v1 callers that omit the epoch remain compatible, but every successful
  update still advances the epoch and invalidates late gateway results.
- `GET /api/v1/routing/preview?model=...` is platform-admin-only and read-only.
  It returns a repeatable example order, excluded channels/reasons, key counts,
  policy/snapshot versions and degraded-source state; the UI explicitly says
  real operations use their server-owned request ID.
- The preview never returns upstream URLs, encrypted/provider credentials,
  adapter configuration, probe tokens, request payloads or provider responses.
- Sanitized route trace metadata includes policy/snapshot versions, source,
  degraded flag, candidate/exclusion counts/reasons, priority, weight,
  selection reason and probe requirement.
- Runtime Status explains the active selection version, refresh/LKG window,
  renewable probe lease and per-instance candidate/snapshot memory bounds.

## 3. Schema, Cutover And Rollback

Migration `000025_prd6_routing_policy_v1` adds:

- channel weight, routing epoch and circuit epoch;
- half-open probe token/expiry;
- key state epoch; and
- bounded constraints plus a routing-order index.

The migration may be applied before the new binary because every new column has
a compatibility default. Runtime activation must nevertheless be non-rolling:
old gateway binaries do not claim the PostgreSQL probe lease and must be drained
before the new routing behavior is exposed.

The down migration succeeds only before any non-default weight, routing/circuit
epoch, active probe field or key-state epoch exists. Once routing-policy state
has been activated it refuses destructive down and requires roll-forward. This
prevents an older binary/schema from silently discarding ownership generations.

## 4. Acceptance Evidence Map

| Acceptance claim | Evidence |
|---|---|
| deterministic priority/weight | `TestP6F08DeterministicWeightedRoutePlanUsesOneSnapshot`; 2,000 server IDs; 300:100 fixture; priority tier cannot be crossed |
| cross-replica deterministic plan | `TestP6F08RoutingPolicyV1MultiReplicaLeaseEpochAndWeightIntegration`; two Router instances and 1,000 shared request IDs |
| bounded snapshot/LKG/fail-closed | `TestP6F08SnapshotLastKnownGoodIsBoundedThenFailsClosed` |
| bounded cache/input cardinality | `TestP6F08SnapshotCacheAndModelInputAreBounded` |
| single probe plus long-operation renewal | real PostgreSQL concurrent claim plus operation beyond the one-second test lease in `TestP6F08RoutingPolicyV1MultiReplicaLeaseEpochAndWeightIntegration` |
| lease contention terminal | `TestP6F08ProbeLeaseContentionReturnsRetryable503WithoutDispatch` proves zero provider calls, one reservation cancellation and 503/`Retry-After` |
| stale routing/circuit/key result rejection | real PostgreSQL routing-, circuit- and key-epoch assertions in the same integration test |
| per-channel breaker threshold | real PostgreSQL threshold=2 transition while global threshold=5 in the same integration test |
| candidate/exclusion evidence without secrets | `TestP6F08PreviewExplainsExclusionsWithoutCredentials` and `TestP6F08PreviewRoutingReturnsOnlySafeContract` |
| operator CAS/invalidation | `TestP6F08ChannelUpdateUsesOptionalRoutingEpochCASAndInvalidatesSnapshot` |
| Chat/Embeddings parity and bounded retry | shared `RouteAllWithKey` source path plus existing retry/failover endpoint matrices; routing trace test asserts the new plan metadata |
| UI semantics | `ChannelsPage.test.tsx` verifies weight explanation, safe preview and exclusion labels |
| rollback | `TestP6F08RoutingPolicyV1MigrationUpDownGuardAndRollForward` verifies safe down/up and refusal after weight or key-epoch activation |

## 5. Repository And CI Verification

Passed on 2026-07-12:

```text
git diff --check
go vet ./...
go test -race ./...
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/gateway -run 'Test(RoutingPolicyPostgresStateTransitions|P6F08RoutingPolicyV1)' -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -run TestP6F08RoutingPolicyV1MigrationUpDownGuardAndRollForward -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
frontend: npm run type-check (pass)
frontend: npm test -- --run (29 files / 143 tests pass)
frontend: npm run build (pass)
```

The local frontend build retains the pre-existing large-chunk warning. The
local macOS race linker retains its pre-existing malformed `LC_DYSYMTAB`
warning; the race test exits successfully. These warnings do not change the
test result. Local Docker verification was unavailable because the machine has
no `docker` CLI; the successful Docker results below are required-CI evidence.

Required-CI closure evidence:

- final implementation head
  `0e374d324708b130d961105b9f3c1155a4d49427` passed PR #172 exact-head run
  `29178419505`: Backend Tests in 5m32s, Frontend Build in 3m24s and Docker
  Build in 1m3s;
- Backend Tests included the explicit P6-F08 real-PostgreSQL routing, admin and
  guarded migration rollback steps; Frontend Build included required Vitest,
  type-check and production build;
- PR #172 was squash-merged as
  `54d88335126a5e9829cf28c720ed98ad6258fb52`;
- post-merge `main` run `29178588075` passed Backend Tests in 6m26s, Frontend
  Build in 3m24s and Docker Build in 1m3s; and
- GitHub's automatic source-branch deletion was reversed. Historical branch
  `codex/prd6-routing-policy-v1` is retained at the exact implementation head.

The successful post-merge run emitted non-failing GitHub runner annotations
about the Node 20 action runtime being forced to Node 24 and one Go cache
restore `tar` miss. They did not skip or fail a required job and are recorded
as CI hygiene observations, not P6-F08 acceptance failures.

## 6. Explicit Non-Claims

- No target, production, throughput-capacity or 100x verification.
- No dynamic latency/cost/rate-aware routing or canary/shadow traffic.
- No response cache or Cache Service.
- No external secret backend, IAM rewrite or service-account runtime change.
- No semantic Guardrail Kernel completion and no D6-10 dynamic stream-budget
  extension.
- No P6-F20 outbox/durable evidence-worker completion.
- No deployment, ECS/security-group change, real/paid upstream request,
  credential change, extra database access, exporter/outbox or X-Insight work.
