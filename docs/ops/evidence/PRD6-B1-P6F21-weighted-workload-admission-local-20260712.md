# PRD6 Batch 1 P6-F21 Weighted Workload Admission — Repository And CI Evidence

> Date: 2026-07-12
> Scope: repository, local and required-CI evidence; no target/deployment claim
> Status: CI verified / closed
> Parent row: P6-F21

## Accepted contract exercised

- UniRouter derives one of three current workload classes after parsing the
  authenticated model request: ordinary Chat is `interactive`, streaming Chat
  is `stream`, and Embeddings is `embedding`. The caller cannot provide an
  arbitrary priority or weight.
- Default weights are 1/4/1. Platform and organization capacity are shared
  across replicas at 100/10 weighted units, with 10/2 units reserved for short
  work so long streams cannot occupy all capacity.
- A bounded virtual-finish queue gives short work fast turns while eventually
  holding enough released units for a fair long-work turn. A saturated
  organization may be bypassed so it cannot head-of-line block another tenant.
- Interactive/Embedding wait at most 50 ms and Stream waits at most 250 ms by
  default. The total queue and per-script scan are bounded. Exhaustion returns
  typed HTTP 429 with `Retry-After`; Redis uncertainty returns typed 503 before
  wallet mutation or provider dispatch.
- Every admission is one atomic Redis consistency decision over global and
  organization totals, queue state, operation identity and expiring leases.
  Redis `TIME`, not application clocks, drives wait and lease expiry.
- Healthy operation pre-issues only one two-second last-known-safe admission
  per process. It is consumed once during Redis uncertainty; subsequent
  uncertainty fails closed.

## Repository implementation evidence

- `internal/workloadadmission/` owns the fixed-class scheduler, Redis Lua
  reserve/release/abandon transitions, bounded queue, short-work reserve,
  per-organization fairness expiry, operation fingerprint and local emergency
  lease. Every key shares Redis Cluster tag `{admission-v1}`.
- Global leases, queue metadata and organization totals are bounded by active
  capacity plus a 512-entry global / 64-entry per-org queue. Global fairness uses fixed fields; per-org
  fairness uses an expiring per-org hash instead of a forever-growing global
  field set. Metrics use only bounded mode/decision/reason labels.
- `internal/limitpolicy/` keeps API-key attempt rate and explicit selector
  policies first, then runs workload admission. A workload rejection rolls
  back token/concurrency reservations but intentionally retains RPM/RPS
  attempts, preventing rejected-work amplification.
- P6-F21 supersedes only the P6-F07 built-in global/organization request-count
  buckets. Explicit global/org/project/key/model/label concurrency policies
  remain additional request-count constraints and can only tighten admission.
- Settlement and cancellation release both explicit policy and weighted
  capacity. Process death retains no permanent slot: the lease expires after
  the server-selected invocation lifetime plus a five-second grace.
- `internal/redisx` now enables go-redis context deadlines, so the admission
  store's 100-ms bound is not silently replaced by the three-second socket
  default and retries.
- `WORKLOAD_*` startup variables are validated fail-closed, included in Docker
  defaults, exposed read-only in the existing limit-policy API/UI and folded
  into `X-UniRouter-Limit-Policy-Version`.
- Active replicas write the same workload configuration fingerprint to Redis.
  A mixed-version/config replica returns typed 503
  `workload_admission.configuration_conflict` while old leases or queued work
  remain; after drain, the new configuration can activate without deleting
  unrelated Redis data.
- Gateway evidence adds the server-derived class, weight, bounded wait and
  queue depth to metadata-only `limit_policy.admission`; response CORS/OpenAPI
  expose only class and version, never prompt, response, credential, operation
  member or raw tenant data.
- The Limit Policies UI explains capacity units, stream weight, short reserve,
  bounded wait, 429 and Retry-After in non-technical language.

## Local verification

Full local verification passed:

```text
go test -race ./internal/workloadadmission -count=3
go test -race ./internal/limitpolicy -run TestP6F21WorkloadAdmissionRunsAfterAPIKeyAttemptRate -count=3
go test -race ./internal/config ./cmd/uniroute ./internal/errdocs ./internal/workloadadmission ./internal/limitpolicy ./internal/policybilling ./internal/gateway ./internal/middleware ./internal/redisx -count=1
go test -race -coverprofile=coverage.out ./...
go test -tags=integration -race ./internal/workloadadmission -run TestP6F21 -count=10
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -run TestP6F07LimitPolicyControlPlaneIntegration -count=1
npm test -- --run src/pages/__tests__/LimitPoliciesPage.test.tsx
npm run type-check
npm run build
go vet ./...
ruby YAML.load_file(OpenAPI, Compose and CI workflow)
git diff --check
```

High-risk fixtures prove:

- 30 calls spread over two replicas and then three replicas under global 6 /
  organization 5 admit exactly five and deny 25 against real Redis;
- a 20-call three-service race under organization capacity three admits
  exactly three, never a per-replica multiple;
- two weight-four streams consume eight of ten units but cannot take the two
  reserved short units; ordinary Chat and Embeddings can still enter;
- a long-work waiter receives a fair turn while twelve new short requests
  contend; a saturated organization does not block another organization, and
  its second queued request cannot consume the other tenant's queue space;
- the queue refuses a third waiter when its configured depth is two and does
  so without extending the wait bound;
- simulated and real Redis lease expiry recover capacity after a holder dies;
- a deliberately non-responsive Redis endpoint fails within the operation
  bound, while a full outage consumes exactly one healthy-period emergency
  lease and then rejects;
- same-operation replay is idempotent, cross-org rebinding conflicts, all
  script keys share one cluster slot, and mixed configuration fails closed
  until old work drains; and
- workload denial still consumes the caller's API-key RPM attempt, while
  wallet/upstream work remains untouched.

The full CI-style PostgreSQL/Redis integration sequence also passed, including
all admin, routing, budget alert, multiplier, stream transport, Agent
capability, distributed policy, workload admission and migration rollback
steps. The local machine does not provide the `docker` command, so the Docker
image result below is required-CI evidence rather than a local claim.

## Explicit non-claims and remaining work

- P6-F21 does not prove target capacity, 100-times throughput, Redis HA,
  multi-region behavior or any production QPS. The single global Redis hash
  slot is deliberately the exact MVP consistency domain and must be measured
  before a later hierarchical-credit/sharded design is accepted.
- This source replaces the old P6-F07 built-in global/org counter family for
  model calls. A later authorized rollout must drain old model-serving
  binaries and use a non-rolling activation; otherwise old and new binaries
  would enforce different key families. No deployment is performed here.
- Configuration changes also require drain because active mixed workload
  fingerprints fail closed by design. This is a safe upgrade constraint, not
  rolling-update evidence.
- P6-F21 does not implement an image/video async job plane or claim that a
  synchronous stream is appropriate for media generation. Only current Chat,
  Streaming Chat and Embeddings are classified.
- P6-F21 does not implement D6-10/P6-F11 dynamic token reservation or semantic
  stream interception, and does not add P6-F20 durable cross-process terminal
  replay. An ambiguous terminal still retains capacity conservatively until
  its bounded lease expires.
- No migration, target deployment, paid/real upstream request, credential
  mutation, additional data access, exporter/outbox enablement, response cache
  or X-Insight change is performed or claimed.

## Pull request and required CI

- implementation commit `91041a5289b98b7fb13570bd2e5744362c02fe37`
  and final exact head `a9d8f3ef92e71117865df2c49e40c0806044a2d0`;
- PR #170 exact-head run `29176160987` passed Backend Tests in 5m26s,
  Frontend Build in 3m29s and Docker Build in 1m5s;
- the earlier run `29175888305` failed only in the new real-Redis fixture:
  race-enabled CI exceeded the unit fixture's 50-ms healthy-store budget and
  therefore exercised the separately tested emergency fallback. Commit
  `a9d8f3e` isolated healthy-capacity verification with a 500-ms test-only
  budget, retained the exact 5 admitted / 25 denied assertion, changed no
  production defaults and then passed ten consecutive local real-Redis race
  runs plus the successful required run;
- PR #170 was squash-merged as
  `04af91f2d5c829796c394d8f4509678cc674f642`;
- post-merge `main` run `29176358459` passed Backend Tests in 5m13s, Frontend
  Build in 3m11s and Docker Build in 1m7s; and
- GitHub's automatic source-branch deletion was reversed: historical branch
  `codex/prd6-weighted-workload-admission` is restored at exact head
  `a9d8f3ef92e71117865df2c49e40c0806044a2d0`.
