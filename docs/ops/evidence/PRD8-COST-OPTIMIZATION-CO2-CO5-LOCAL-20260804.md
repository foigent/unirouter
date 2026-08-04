# PRD8 Cost Optimization CO-2 Through CO-5 — Local Evidence

> Work item: `UR-PRD8-COST-OPTIMIZATION-V1`
> Evidence date: 2026-08-04 (Asia/Shanghai)
> Baseline commit/tree: `b46cbd9f1fdcc419a856666893defeba3d711e76` / `b0dd75ca0c4cf05fe050f240e3826b324779abd1`
> Branch: `codex/cost-optimization-runtime-v1-20260804`
> Status: `LOCAL_VERIFIED_CANDIDATE`
> Product/runtime mutation: none

## Outcome

CO-2 through CO-5 implement the default-off Exact Response Cache and Prompt
Compression control plane specified by PRD8. The implementation adds:

- `unirouter.response-cache.v2`: exact post-policy request identity, opaque
  HMAC keys, AES-256-GCM values, bounded TTL/generation and one-owner fill
  leases in the existing Redis dependency;
- zero-Provider/zero-charge cache delivery, recovery and append-only
  commercial evidence using `unirouter.usage.v2` with `local_cache` authority;
- `unirouter.invocation-optimization-policy.v1`: tenant/project/model scope,
  optimistic revisions and `draft -> validated -> shadow -> canary ->
  rolled_back` lifecycle;
- `unirouter.prompt-compression.whitespace-safe.v1`: a conservative,
  deterministic, text-only evaluator with exact numeric-lexeme preservation;
- content-free deterministic Canary assignment capped at 10%, explicit caller
  opt-in and current-revision Shadow evidence gates; and
- `模型治理 -> 调用优化` management plus read-only realized/estimated outcome
  presentation in `成本与财务 -> 成本优化`.

Migration `000056_prd8_invocation_optimization` creates policy, revision,
decision, delivery and commercial evidence state; adds monotonic API-key
`auth_epoch`; and extends the existing usage/recovery constraints only for the
zero-token local-cache contract. Compose files expose optional environment
pass-throughs, but no real key is present in the repository.

## Safety and truth corrections found during implementation

The final Local candidate includes fixes discovered by negative and full
regression testing:

1. Shadow and unassigned Canary evaluations cannot inherit an invalid-cache
   rejection and alter the original Provider path. Only an assigned effective
   Canary may consume a hit, own a fill lease or reject bounded healthy-cache
   contention.
2. Shadow receipts always record `assigned=false` and
   `mutation_applied=false`.
3. Policy resolution has a one-second bound; metadata/commercial evidence
   writes have a two-second bound and fail open for the original Provider
   request. X-Insight is never consulted synchronously.
4. Provider request count in source proof is the number of actual
   `http.Client.Do` dispatches, not the number of route candidates.
5. Cache decision receipts are appended only for an actual hit/reject or after
   the route has successfully acquired its probe; skipped half-open candidates
   cannot pollute Shadow evidence.
6. Drafts with no enabled Canary driver, or with disabled status, remain
   editable and cannot enter an unreturnable validated state.
7. Cache-hit wire responses replace the source execution ID/time and expose
   all-zero current-delivery usage; encrypted source bytes and their hash remain
   unchanged evidence.
8. Migration checks reject non-zero local-cache counters and reserve the
   bounded request-idempotency v2 metadata combination for
   `local_cache/customer_charge/final`.
9. Cost Optimization counts a commercial receipt as realized only when the
   corresponding reservation is terminal `settled`, usage authority is
   response-cache v2/local-cache/final and calculated/customer charge are both
   zero; delivery-complete but recovery-pending work is not shown early.
10. Customer preflight treats the three response-cache bindings as optional
    only when all are absent. The default-off path passes, while a partial
    binding or an invalid 32-byte key still fails closed.

## Exact Local verification

The final state passed:

```text
TEST_DATABASE_URL=postgres://m4@127.0.0.1:55432/postgres?sslmode=disable \
REQUIRE_TEST_DATABASE=1 go test -tags=integration -p 1 ./... -count=1
  PASS: every package, including migration 000056, billing, Gateway,
  completed-execution Feed and legacy rollback ordering

GOCACHE=/private/tmp/unirouter-go-cache-20260804 \
go test -p 1 ./... -count=1
  PASS: every package

go vet ./...
  PASS

GOCACHE=/private/tmp/unirouter-go-cache-20260804 \
go test -race ./internal/responsecache ./internal/promptcompression \
  ./internal/invocationopt ./internal/gateway -count=1
  PASS: all four packages

cd frontend && npm test
  56/56 files, 369/369 tests PASS

cd frontend && npm run build
  TypeScript PASS
  Vite production build PASS: 4,320 transformed modules, 80 JavaScript assets
  largest asset 362,347 bytes; enforced limit 460,800 bytes

git diff --check
ruby YAML parse of docs/tech-ref/05-openapi-spec.yaml
bash -n deploy/customer/preflight.sh
  PASS

isolated fake-Compose preflight matrix
  PASS: all three response-cache values absent
  PASS: all three response-cache values valid
  PASS: partial binding rejected
  PASS: short encryption key rejected
```

The first parallel `go test ./...` attempt is not counted as a pass. While the
Go suite, frontend test and frontend build competed for local resources, one
existing concurrent-top-up test completed its business assertions but timed
out dropping its temporary database. The exact test passed in isolation and
the complete Go suite then passed with package serialization against the
isolated PostgreSQL instance. The one exact temporary database left by the
failed cleanup was verified by name, dropped, and confirmed absent. No product
database or business row was changed.

A later sandboxed `go test -p 1 ./...` attempt is also not counted as a pass:
the execution sandbox denied loopback listeners used by `httptest` and
`miniredis` (`bind: operation not permitted`). The exact command was rerun
outside that network sandbox and exited successfully as reported above; no
test assertion from the sandboxed attempt is represented as product evidence.

The frontend suite emits existing Semi UI/React Router deprecation and test
act warnings; it has zero failed files or cases. The focused race build emits
one macOS linker `LC_DYSYMTAB` warning and exits successfully; no race is
reported.

## Compatibility conclusion

With no policy and no three-part response-cache key binding, the Gateway
follows the existing request path. Even after a future administrator creates a
policy, request mutation still requires current Shadow evidence, explicit
Canary transition, deterministic assignment and the exact caller opt-in
header. `Cache-Control: no-store` always prevents response-cache use.

The full regression confirms compatibility for current request admission,
routing/failover, Provider credentials, pricing/reservation/settlement,
Outbox identity, Source Snapshot bytes/hash, completed-execution Feed
endpoint/DTO/order and Cursor behavior. Exact Response Cache does not reuse
Provider Prompt Cache price semantics. Compression receipts contain no prompt,
response or candidate body. Tool result caching remains Tool Gateway-owned;
MCP resource/session caching remains MCP Gateway-owned; X-Insight remains the
cross-execution analysis owner.

## Not claimed by this evidence

- no commit, push, PR, CI, merge or Target deployment;
- no real response-cache index/encryption key or key version;
- no optimization policy, Shadow sample, Canary assignment or model traffic;
- no Provider/channel credential, data reset, Cursor/Checkpoint or X-Insight
  flow change; and
- no customer Production, load, HA/DR or real POC result.

CI and development-Target evidence must be recorded separately against their
exact commit/tree. The authorized Target rollout must keep all response-cache
key variables absent and policy count zero, then verify the authenticated UI
and pre/post compatibility readbacks without generating model traffic.
