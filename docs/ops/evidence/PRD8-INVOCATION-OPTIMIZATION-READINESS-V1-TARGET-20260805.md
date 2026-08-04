# PRD8 Invocation Optimization Canary Readiness V1 — CI And Target Evidence

> Work item: `UR-PRD8-INVOCATION-OPTIMIZATION-READINESS-V1`
> Evidence date: 2026-08-05 (Asia/Shanghai)
> Environment: existing Alibaba Cloud UniRouter development/test ECS
> Status: `CI_MERGED_AND_DEVELOPMENT_TARGET_VERIFIED_DEFAULT_OFF`
> Production claim: none

## Outcome And Runtime Boundary

The read-only `unirouter.invocation-optimization-readiness.v1` contract,
shared server-side Canary admission resolver and persistent Policy Inspector
are merged and deployed to the existing development Target. The release keeps
Invocation Optimization fail-closed and default-off:

- no Response Cache key or key version was injected;
- no policy, revision, Shadow sample, Canary assignment or lifecycle receipt
  was created;
- no Exact Cache entry exists in Redis;
- no Provider request, credential change, data reset, Feed Cursor or
  Checkpoint operation ran; and
- the readiness response remains technical evidence only: it explicitly grants
  neither activation authority nor traffic authority.

This release proves that the exact readiness facts and blockers are available
to an authenticated operator without changing the request path. It is not
evidence of a cache hit, Compression quality, cost reduction or customer
Production.

## Repository, CI And Merge

- Baseline `main` commit/tree:
  `b1c7dafb32a04a4c4725fd9e3f4a05722632ab31` /
  `1e04820f799816e5c915f3985cd73cd5941f8273`.
- Exact CI-passed head/tree:
  `f0a54facc9969438487fa10a66ea0d307abb294e` /
  `aacb40c932c668e802aeb31feeda43d4abede0b4`.
- PR: [#259](https://github.com/foigent/unirouter/pull/259).
- Exact application CI run `30930453717` passed:
  - Change Routing: 9 seconds;
  - Frontend Build: 8 minutes 46 seconds;
  - Backend Tests: 20 minutes 31 seconds; and
  - Docker Build: 1 minute 44 seconds.
- PR Protocol Conformance run `30930453275` passed in 2 minutes 15 seconds.
- The unchanged PR was `OPEN`, non-draft and mergeable immediately before an
  exact-head protected squash merge.
- Squash merge commit:
  `0def6b436b8b92bd68bf666c6ecc59072f6251ec`.
- The merge tree is byte-identical to the exact CI-passed head tree:
  `aacb40c932c668e802aeb31feeda43d4abede0b4`.
- Post-merge Protocol Conformance run `30932426744` passed the exact merge
  commit in 2 minutes 18 seconds. The repository does not run the complete
  application matrix again on a `main` push; this record does not invent one.

Local verification is recorded independently in
`PRD8-INVOCATION-OPTIMIZATION-READINESS-V1-LOCAL-20260805.md`. Its counted
result is full serialized Go, vet, exact isolated-PostgreSQL lifecycle/tenant
integration, 56 frontend files and 373 tests with one worker, TypeScript/Vite
production build, bundle budget, OpenAPI parsing and diff checks. The earlier
default-parallel frontend timeout remains recorded as a non-pass and is not
used as release evidence.

## Backup, Bundle And Release Staging

- Incremental Git bundle:
  `/opt/unirouter/bundles/unirouter-0def6b436b8b92bd68bf666c6ecc59072f6251ec.bundle`.
- Bundle mode / size / SHA-256:
  `0644` / 26,305 bytes /
  `7f4f0fe1484fb21ea9a493a83c6c50d238e7662aec68f3206c13ed0096bfdd5c`.
- Local and Target `git bundle verify` passed. The bundle requires the exact
  deployed predecessor `e8931c78d982b3d2adaf77b38361b74a6f84394d`.
- Pre-release PostgreSQL custom-format backup:
  `/opt/unirouter/backups/pre-readiness-0def6b43-20260804T171300Z.dump`.
- Backup mode / size / SHA-256:
  `0600` / 609,078 bytes /
  `d2073fa1f84cbf6c92b738a834b6f34224e7233a0ff1f9868ea15a21388f8cf6`.
- The backup passed read-only `pg_restore -l` validation inside the running
  PostgreSQL 16 container.
- Commit-addressed release directory:
  `/opt/unirouter/releases/0def6b436b8b92bd68bf666c6ecc59072f6251ec`.
- Staged checkout readback returned the exact commit/tree and a clean Git
  worktree. The predecessor mode-`0600` `.env` was copied without printing
  secrets.
- Only `RUNTIME_VERSION` and `RUNTIME_COMMIT` changed. The filtered hash of all
  other lines remained
  `608c57a936c8f552c0b019f7c72b97dd00e6b96b2ddd99e19062f1d1668dc7ba`.
- `RESPONSE_CACHE_INDEX_KEY`, `RESPONSE_CACHE_ENCRYPTION_KEY` and
  `RESPONSE_CACHE_KEY_VERSION` were all absent.
- The staged version/commit are
  `1.1.5-prd8-invopt-readiness` /
  `0def6b436b8b92bd68bf666c6ecc59072f6251ec`.
- Compose rendering passed. The existing top-level Compose `version` warning
  remains a separate maintenance item.
- The predecessor images remain addressable as
  `unirouter-rollback/backend:e8931c78`,
  `unirouter-rollback/backend-canary:e8931c78` and
  `unirouter-rollback/frontend:e8931c78`.

## Target Build And Switch

No migration file changed between the deployed predecessor and the new release.
The Target still built stable backend, canary backend and frontend sequentially
under the explicit existing Compose project `unirouter`. The pending-release
binary read all 55 migrations as applied with
`000056_prd8_invocation_optimization` as the maximum; no `migrate up` ran.

Target frontend build independently passed the same budget: 4,320 transformed
modules, 80 JavaScript assets and largest asset 362,347 bytes under the
460,800-byte limit.

Switch order was canary, stable, then frontend. Canary and stable each passed
their Compose health gate before the next component changed. PostgreSQL,
Redis, Prometheus, Grafana and TLS Nginx were not recreated.

Exact running identities:

| Component | Image identity | Runtime result |
|---|---|---|
| canary backend | `sha256:4cf81996321276151bbf0213268f6e6339b5729fff6508a8fbd5930d45f26fa2` | healthy, restart `0` |
| stable backend | `sha256:0de92e4ab456a8bbe995d0e7a2d5771dfa0b4746ca499d25f3f43df9e2fcafcb` | healthy, restart `0` |
| frontend | `sha256:3ee48db89c972d6f9d10bdbf00694836a443d4e43ea38b29e115f1f77d477e78` | running, restart `0` |

Both backends read back the new release directory and logged:

- `Release trust mode: development`;
- `Exact response-cache runtime disabled: encryption/index key references are not configured`; and
- completed-execution Reference Feed mode `neutral_canary`, protocol `1.1.0`
  and connector `foigent.unirouter.completed-execution`.

The first post-switch public probes passed; no startup `502` or rollback was
observed.

## Zero-Drift Data Readback

The same read-only facts were captured immediately before and after the switch.
They were byte-for-byte equal at the reported value level:

| Fact | Before | After |
|---|---:|---:|
| migrations / maximum | `55 / 000056_prd8_invocation_optimization` | same |
| organizations / users / projects | `1 / 20 / 1` | same |
| API keys / channels / channel credentials | `56 / 3 / 3` | same |
| call logs / transactions | `60 / 286` | same |
| retained Feed items / maximum retained sequence | `0 / 0` | same |
| Feed retained floor / acknowledged sequence | `12 / 12` | same |
| workload applications / deployment profiles / Gateway Endpoints | `0 / 0 / 0` | same |
| pending Outbox / dead-letter Outbox | `0 / 0` | same |
| Invocation Optimization policies / revisions | `0 / 0` | same |
| optimization / Cache-delivery / Cache-commercial receipts | `0 / 0 / 0` | same |
| Redis keys under `uniroute:response-cache:v2*` | `0` | same |

The empty retained Feed is not a reset: its durable retained floor and
acknowledgement remain 12. No existing request, route, credential, pricing,
wallet, transaction, Outbox, Feed, Source Instance, Cursor or Checkpoint fact
was changed.

## Public And Authenticated Verification

Public boundaries passed after the switch:

| Check | Result |
|---|---|
| HTTP root | `308` to `https://8.130.18.85/` |
| HTTPS root | `200` |
| HTTPS `/health/ready` | `200`, PostgreSQL and Redis `ok` |
| unauthenticated Runtime Status | `401` |
| unauthenticated per-policy readiness | `401` |
| unauthenticated `/v1/models` | `401` |
| TLS Nginx configuration | `nginx -t` PASS |

The existing authenticated browser session was reused. No password, token or
secret was read or submitted, and no model or Provider action ran.

Authenticated UI readback confirmed:

- all left-navigation secondary items remain expanded;
- `模型治理 → 调用优化` shows zero Shadow, zero Canary, zero disabled/rolled
  back policies and `Exact Cache 运行时：未配置`;
- the empty workspace explicitly states that the default state neither caches
  responses nor changes Prompt content;
- `Policy Inspector / 证据门禁与执行边界` is persistently visible without an
  additional open action;
- at the observed 2,060-pixel viewport the Inspector is visible at 480 pixels
  wide and document `scrollWidth` equals `clientWidth` at 2,060 pixels, so no
  horizontal overflow exists;
- Runtime Status reads
  `1.1.5-prd8-invopt-readiness · 0def6b436b8b`, `development / stable`, with
  PostgreSQL, Redis, Prometheus and Grafana available; and
- browser diagnostic logs were empty.

There is no policy in the Target, so no per-policy readiness response exists to
render. This verification deliberately did not create a policy merely to
populate the Inspector. The browser was left on `模型治理 → 调用优化` for
Product Owner review.

## Compatibility Conclusion

The release changes no request admission, route selection, Provider dispatch,
credential, pricing, settlement or finality behavior while no optimization
policy exists. The same Go resolver supplies the read-only evidence and the
transactional Canary gate; browser state cannot grant activation. Compression-
only readiness remains independent of Cache keys, while Cache readiness stays
blocked until all three deployment values exist.

X-Insight remains outside UniRouter's request path. Tool-result caching remains
Tool Gateway scope and MCP resource/session caching remains MCP Gateway scope.
Completed-execution Feed DTO, Source Snapshot bytes/hash, event identity,
endpoint, ordering and Cursor/Checkpoint behavior remain compatible.

## Remaining Gates

- Real Cache enablement still requires a separately authorized key ceremony,
  rotation/recovery plan, policy draft and bounded request cohort.
- Compression must first collect representative, content-free Shadow evidence;
  any Canary remains a separate Product Owner activation/cohort decision.
- The upcoming second-integration data reset, fresh channels/credentials and
  real Agent traffic were not started.
- ORG-3 and FIN-4 still require named customer directory and ERP/GL systems.
- Root disk has approximately 6.3 GB free and is 84% used. No prune, bulk
  deletion or destructive cleanup ran.
- GitHub Actions emitted a Node 20 deprecation annotation for upstream Actions;
  all exact Protocol checks passed and the maintenance warning does not alter
  this release result.
- This is development/test Target evidence, not Production, load, HA/DR,
  provider-savings, Compression-quality or customer-site evidence.
