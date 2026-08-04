# PRD8 Cost Optimization CO-2 Through CO-5 CI And Target Verification

> Work item: `UR-PRD8-COST-OPTIMIZATION-V1`
> Evidence date: 2026-08-04 (Asia/Shanghai)
> Environment: existing Alibaba Cloud UniRouter development/test ECS
> Status: `CO2_CO5_CI_MERGED_AND_DEVELOPMENT_TARGET_VERIFIED_DEFAULT_OFF`
> Production claim: none

## Outcome And Runtime Boundary

CO-2/CO-3 Exact Response Cache and CO-4/CO-5 Compression Shadow/Canary are
implemented, merged, migrated and visible in the authenticated development
Target console. The runtime was deliberately released in its fail-closed
default state:

- no `RESPONSE_CACHE_INDEX_KEY`, `RESPONSE_CACHE_ENCRYPTION_KEY` or
  `RESPONSE_CACHE_KEY_VERSION` value was injected;
- no Invocation Optimization policy, revision or receipt was created;
- no Exact Cache entry exists in Redis;
- no Compression Shadow or Canary was started; and
- no Provider request, credential, data reset, Feed Cursor or Checkpoint
  operation was performed.

The release proves that the product and control surfaces can be deployed
without activating either optimization. It is not evidence of a real cache
hit, Compression quality, provider-cost reduction or customer Production.

## Repository, CI And Merge

- Baseline `main` commit:
  `b46cbd9f1fdcc419a856666893defeba3d711e76`.
- Main implementation commit:
  `deb540c2a4c0b091c642ab1affb786814c547840`.
- Narrow deployment-preflight correction:
  `eb4bfad8a379d05d20872d8405f31a5bac3c3b47`.
  The correction keeps required customer secrets fail-closed while allowing
  all three optional Response Cache values to be absent. Partial bindings,
  short keys and invalid versions remain rejected.
- PR: [#257](https://github.com/foigent/unirouter/pull/257).
- Exact CI-passed head/tree:
  `eb4bfad8a379d05d20872d8405f31a5bac3c3b47` /
  `fa307303f68891327960e3cffd1b38d617473491`.
- Exact application CI run `30917124632` passed:
  - Change Routing: 6 seconds;
  - Frontend Build: 8 minutes 20 seconds;
  - Backend Tests: 21 minutes 24 seconds; and
  - Docker Build: 1 minute 33 seconds.
- PR Protocol Conformance run `30917121908` passed in 2 minutes 35 seconds.
- GitHub reported `CLEAN` / `MERGEABLE` for the unchanged head and base before
  an exact-head protected squash merge.
- Squash merge commit:
  `e8931c78d982b3d2adaf77b38361b74a6f84394d`.
- The merge tree is byte-identical to the exact CI-passed head tree:
  `fa307303f68891327960e3cffd1b38d617473491`.
- Post-merge Protocol Conformance run `30919221503` passed the exact merge
  commit. The repository does not run the complete application matrix again
  on a `main` push; this evidence does not invent such a run.

The first PR attempt correctly failed Docker Build because customer preflight
treated an absent optional Cache triple as a missing required value. The
narrow fix above was committed, pushed and then re-ran the complete exact-head
matrix. Only the second terminal-green result is used as release authority.

## Local Verification Before CI

The exact implementation tree passed:

- full serialized isolated-PostgreSQL integration tests;
- full serialized Go regression;
- focused race tests for Response Cache, Compression, Invocation Optimization
  and Gateway;
- `go vet ./...`;
- 56 frontend files and 369 frontend tests;
- frontend production build with 4,320 transformed modules, 80 JavaScript
  assets and largest asset 362,347 bytes under the 460,800-byte gate;
- OpenAPI YAML parsing, deployment preflight syntax and `git diff --check`;
  and
- four offline preflight cases: Cache triple absent, complete valid triple,
  partial binding rejection and short-key rejection.

Detailed implementation evidence remains in
`PRD8-COST-OPTIMIZATION-CO2-CO5-LOCAL-20260804.md`.

## Backup, Bundle And Release Staging

- Incremental Git bundle:
  `/opt/unirouter/bundles/unirouter-e8931c78d982b3d2adaf77b38361b74a6f84394d.bundle`.
- Bundle mode / size / SHA-256:
  `0644` / 117,988 bytes /
  `a82a8e72fb895e57633665f29081ac8540e60822f3c6a53d0639e2792356b0cb`.
- Local and Target `git bundle verify` passed. The bundle requires the exact
  deployed predecessor `dde6eff214067a594f26b6aa080286f6cb4682d9`.
- Pre-migration PostgreSQL custom-format backup:
  `/opt/unirouter/backups/pre-invopt-e8931c78-20260804T143646Z.dump`.
- Backup mode / size / SHA-256:
  `0600` / 569,487 bytes /
  `47a22012226d686693d57b675816c9043267f8e2ae1219133c344db3ac5b5200`.
- The exact backup passed read-only `pg_restore -l` validation inside the
  running PostgreSQL 16 container.
- Commit-addressed release directory:
  `/opt/unirouter/releases/e8931c78d982b3d2adaf77b38361b74a6f84394d`.
- Staged checkout readback returned the exact commit/tree and zero tracked or
  untracked changes.
- The predecessor mode-`0600` `.env` was copied without printing secrets.
  Only `RUNTIME_VERSION` and `RUNTIME_COMMIT` changed; the filtered hash of all
  other lines remained
  `608c57a936c8f552c0b019f7c72b97dd00e6b96b2ddd99e19062f1d1668dc7ba`.
- All three Cache keys were absent in the release `.env`. Compose renders them
  as empty container environment values, which the application correctly
  treats as runtime disabled.
- The predecessor images were retained as
  `unirouter-rollback/backend:dde6eff2`,
  `unirouter-rollback/backend-canary:dde6eff2` and
  `unirouter-rollback/frontend:dde6eff2`.

## Target Build, Migration And Switch

The Target built backend, canary and frontend sequentially. The running Compose
project is explicitly `unirouter`; a first backend build using the release
directory's implicit project name created only an unused staging image. No
container was switched to that image. The backend was rebuilt with
`-p unirouter`, and every later build/switch used the explicit existing project.

Target frontend build independently passed the same budget: 4,320 modules,
80 JavaScript assets and largest asset 362,347 bytes.

The new backend migration status reported exactly one pending migration:
`000056_prd8_invocation_optimization`. The same new binary applied it in one
transaction in 157 ms. Post-migration readback returned 55 migrations with
`000056_prd8_invocation_optimization` as the maximum.

Switch order was canary, stable, then frontend. Stable and canary each passed
their Compose health gate before the next component changed. PostgreSQL,
Redis, Prometheus, Grafana and TLS Nginx were not recreated.

Exact running identities:

| Component | Image identity | Runtime result |
|---|---|---|
| canary backend | `sha256:1e5777e27acddaf411da167b463e09a285b86fd8e910c3f2d88af1727762641a` | healthy, restart `0` |
| stable backend | `sha256:5b4ae33176185f8c2561e95ec87fa38bed6088b7e09b7fcc822e5968fb562efc` | healthy, restart `0` |
| frontend | `sha256:6abb499f5e7cbb9f733be6501bae7857dab922948d3f12274b5309d508a8f88c` | running, restart `0` |

Both backend startup logs read back:

- `Release trust mode: development`; and
- `Exact response-cache runtime disabled: encryption/index key references are not configured`.

The first HTTPS root probe was issued immediately after the frontend container
started and returned `502`. The frontend container was running with restart
count zero and normal Nginx startup logs; the first bounded readiness retry
returned `200`. Subsequent root and readiness probes remained `200`, so no
rollback was required. This transient startup window is recorded rather than
silently rewritten as a first-attempt pass.

## Zero-Drift Data Readback

The same read-only facts were captured before migration and after the complete
switch. Only the expected schema ledger changed:

| Fact | Before | After |
|---|---:|---:|
| migrations / maximum | `54 / 000055_prd8_deployment_profiles` | `55 / 000056_prd8_invocation_optimization` |
| organizations / users / projects | `1 / 20 / 1` | same |
| API keys / channels / channel credentials | `56 / 3 / 3` | same |
| call logs / transactions | `60 / 286` | same |
| retained Feed items / maximum retained sequence | `0 / 0` | same |
| Feed retained floor / acknowledged sequence | `12 / 12` | same |
| workload applications / deployment profiles / Gateway Endpoints | `0 / 0 / 0` | same |
| pending hooks / Outbox / unresolved Provider attempts / non-terminal reservations | `0 / 0 / 0 / 0` | same |

New runtime state after migration:

| Fact | Result |
|---|---:|
| Invocation Optimization policies | `0` |
| policy revisions | `0` |
| lifecycle receipts | `0` |
| Cache delivery receipts | `0` |
| Cache commercial receipts | `0` |
| API keys with non-default `auth_epoch` | `0` |
| Redis keys under `uniroute:response-cache:v2*` | `0` |

The empty retained Feed is not a reset: its durable retained floor and
acknowledgement were already 12 and remain 12. No existing request, pricing,
wallet, transaction, Outbox, Feed, Source Instance, Cursor or Checkpoint fact
was changed.

## Public And Authenticated Verification

Public boundaries passed after the switch:

| Check | Result |
|---|---|
| HTTP root | `308` to `https://8.130.18.85/` |
| HTTPS root | `200` |
| HTTPS `/health/ready` | `200` |
| unauthenticated Runtime Status | `401` |
| unauthenticated Cost Optimization | `401` |
| unauthenticated Invocation Optimization contract | `401` |
| unauthenticated `/v1/models` | `401` |
| TLS Nginx configuration | `nginx -t` PASS |

The existing authenticated browser session was reused. No password, token or
secret was read or submitted, and no model or Provider test action ran.

Authenticated UI readback confirmed:

- `成本与财务 → 成本优化` renders Provider Prompt Cache, Exact Response Cache
  and Compression separately;
- Exact Cache shows `运行时密钥未配置`, zero completed local deliveries and
  zero realized avoided cost;
- Compression shows `运行时就绪 / 默认关闭`, zero Shadow samples and zero
  applied requests;
- the existing Prompt Cache baseline remains `¥0.810139`, coverage `25.5%`
  (`13 / 51`) and read-token ratio `69.0%`; these historical facts were not
  rewritten by the release;
- `模型治理 → 调用优化` shows zero Shadow, zero Canary, zero disabled/rolled
  back policies and `Exact Cache 运行时：未配置`;
- the empty workspace explicitly states that the default state does not cache
  responses or modify Prompt content;
- the Policy Inspector is persistently visible without an additional open
  action; at the observed 2,060-pixel viewport it occupies 480 pixels beside
  the policy workspace, with no horizontal overflow;
- Runtime Status reads
  `1.1.4-prd8-invopt · e8931c78d982`, `development / stable`, with PostgreSQL,
  Redis, Prometheus and Grafana available; and
- captured browser console warnings/errors were empty.

The browser was left on `模型治理 → 调用优化` for Product Owner review.

## Compatibility Conclusion

The release keeps current request admission, route selection, Provider
dispatch, credential use, pricing, settlement and finality unchanged while no
policy exists. Exact Cache cannot initialize without all three deployment
values and cannot enter Canary without an explicit policy, persistent-cache
permission, request opt-in and evidence gate. Compression remains a
conservative product-local request transform and cannot enter Canary without a
current-revision Shadow gate, explicit caller opt-in and maximum 10% assignment.

X-Insight remains outside UniRouter's request path and continues to own
cross-source investigation. Tool result caching remains Tool Gateway scope;
MCP resource/session caching remains MCP Gateway scope. Completed-execution
Feed DTO, Source Snapshot bytes/hash, credential, event identity, endpoint,
ordering and Cursor/Checkpoint behavior remain compatible.

## Remaining Gates

- Real Response Cache enablement requires an independently authorized key
  ceremony, rotation/recovery plan, policy draft and bounded request cohort.
- Compression must first collect representative, content-free Shadow quality
  evidence; any Canary remains a separate Product Owner decision.
- The upcoming second integration data reset, fresh channels/credentials and
  real Agent traffic were not started.
- ORG-3 and FIN-4 still require named customer directory and ERP/GL systems.
- Root disk has approximately 5.9 GB free and is 85% used. No prune, bulk
  deletion or destructive cleanup ran.
- The existing top-level Compose `version` warning remains a separate minimal
  maintenance item.
- This is a development/test Target result, not Production, load, HA/DR,
  provider-savings, Compression-quality or customer-site evidence.
