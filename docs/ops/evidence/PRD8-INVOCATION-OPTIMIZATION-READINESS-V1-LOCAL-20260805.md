# PRD8 Invocation Optimization Canary Readiness V1 — Local Evidence

> Work item: `UR-PRD8-INVOCATION-OPTIMIZATION-READINESS-V1`
> Evidence date: 2026-08-05 (Asia/Shanghai)
> Baseline commit/tree: `b1c7dafb32a04a4c4725fd9e3f4a05722632ab31` / `1e04820f799816e5c915f3985cd73cd5941f8273`
> Branch: `codex/invocation-optimization-readiness-v1-20260805`
> Status: `LOCAL_VERIFIED_CANDIDATE`
> Runtime/product data mutation: none

## Outcome

This increment closes the information gap between the Invocation Optimization
policy lifecycle and the operator-facing UI without enabling an optimization.
It adds the product-local
`unirouter.invocation-optimization-readiness.v1` contract and the read-only,
organization-scoped endpoint:

```text
GET /api/v1/invocation-optimization/policies/{ref}/readiness
```

For the selected exact policy revision, the response reports a fixed-order
Cache/Compression view of runtime availability, current-revision Shadow sample
count, eligible count and failure count. It also returns stable blocker codes,
`runtime_mutation=none`, `grants_activation_authority=false` and
`grants_traffic_authority=false`.

The persistent Policy Inspector now renders that evidence inline and disables
Canary when the result is blocked, unavailable, contract-incompatible or bound
to another policy revision. The UI still exposes rollback while Shadow is
blocked. It does not add a full-enable action or a secret-management surface.

## Single admission semantics

The read-only API and the mutating Canary transition reuse the same Go
resolver. The transition still reloads authoritative database evidence inside
its transaction and applies optimistic revision locking; it does not trust the
browser result.

The resolver preserves the existing gate exactly:

- policy is active, targets at least one driver and is in `shadow`;
- every targeted driver has at least `minimum_shadow_samples` for the exact
  current revision;
- every targeted driver has at least one eligible sample;
- assigned/mutated Shadow rows and eligible safety/quality failures are zero;
  and
- Exact Response Cache additionally has its all-or-none key backend ready.

Compression-only readiness is intentionally independent of Cache key state.
No evidence row contains Prompt, response or candidate bytes.

## Local verification

The final candidate passed:

```text
GOCACHE=/private/tmp/unirouter-readiness-go-cache \
go test -p 1 ./... -count=1
  PASS: every Go package

TEST_DATABASE_URL=<isolated-loopback-test-postgres> \
REQUIRE_TEST_DATABASE=1 \
go test -tags=integration -p 1 ./internal/admin \
  -run '^TestInvocationOptimizationLifecycleEvidenceGateAndTenantScopeIntegration$' \
  -count=1
  PASS: exact lifecycle gate, current-revision counters and tenant isolation

GOCACHE=/private/tmp/unirouter-readiness-go-cache go vet ./...
  PASS

cd frontend && npm test -- --silent --reporter=dot \
  --maxWorkers=1 --minWorkers=1
  PASS: 56/56 files, 373/373 tests

cd frontend && npm run build
  PASS: TypeScript, Vite production build and bundle budget
  4,320 transformed modules; 80 JavaScript assets
  largest asset 362,347 bytes; enforced limit 460,800 bytes

ruby YAML parse of docs/tech-ref/05-openapi-spec.yaml
git diff --check
  PASS
```

The first integration invocation is not counted as a pass: the former local
test PostgreSQL port was stopped. A second attempt identified the correct
existing isolated test-cluster role, after which the exact integration test
passed. No product database was contacted.

One default-parallel frontend run is also not counted as a full pass: an
existing Notification Center stale-response test timed out at ten seconds
while 372/373 cases passed. The exact failing test passed alone, and the full
suite then passed 373/373 with one worker. No timeout was relabelled as a pass.

## Compatibility and authority boundary

No migration or persisted product field is added. The default-off request path,
route choice, Provider dispatch, credentials, pricing, settlement, Outbox,
Source Snapshot bytes/hash, completed-execution Feed and Cursor/Checkpoint
remain unchanged. X-Insight remains outside the synchronous request path.

This evidence does not claim CI, Target deployment or Production. It does not
create or rotate Cache keys, create/advance a policy, authorize Canary or
traffic, send a model request, reset data, change credentials, or move a
Cursor/Checkpoint. Baseline Agent POC compatibility remains independent of
Invocation Optimization; any later optimization experiment requires separate
Product Owner activation and cohort decisions.
