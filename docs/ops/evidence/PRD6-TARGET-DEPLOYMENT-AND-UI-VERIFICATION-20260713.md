# PRD6 Target Deployment And UI Verification Evidence

**Execution window:** 2026-07-13 through 2026-07-14 (Asia/Shanghai)

**Owner:** Codex

**Environment:** Alibaba Cloud shared development/test ECS

**Status:** Target verified for the deployment and UI surfaces explicitly listed below

## Scope And Authorization

The User explicitly authorized updating the existing UniRouter ECS and checking
whether the PRD6 UI and related functions were actually present. The authorized
execution included the existing stable/canary application, its existing
PostgreSQL/Redis dependencies, an isolated temporary QA account, read-only
inspection of historical target records, and the minimum frontend correction
required by the target browser result.

The execution did not authorize or perform a security-group change, persistent
service/provider credential change, real or paid provider/detector request,
additional database/export scope, external exporter, X-Insight runtime change,
TLS or migration-down drill, capacity/load test, or managed-HA change. The
isolated QA account credential was regenerated only for the final browser
readback and the account was then deleted.

## Repository And CI

- The PRD6 accepted implementation was merged by PR #189 as `4c78a5b7`; PR #190
  published the Accepted Scope Complete state as
  `ce9307a7082ff5ad6e49c0380e36b18186e0c3be`.
- The initial target release used exact commit `ce9307a7082ff5ad6e49c0380e36b18186e0c3be`.
- Target browser verification found one stale Vite starter favicon request.
  PR #191 removed the missing `/vite.svg` reference and added the migration
  rehearsal guard to the deployment runbook. Exact PR head `046033a` passed
  required run `29264998431`: Backend Tests, Frontend Build and Docker Build.
- PR #191 was squash-merged as
  `581e9d7fa68fbefcaf14b8bdcc285a0eb03d1af7`.
- Post-merge `main` run `29265887632` passed Backend Tests, Frontend Build and
  Docker Build.
- Removing the explicit missing asset exposed the browser's default
  `/favicon.ico` lookup. PR #192 added an inline SVG icon so no separate asset
  request is required. Exact implementation head `ad1b0121b8cc5834896189aebe42c1fa2ba85003`
  passed required run `29270879406`: Backend Tests, Frontend Build and Docker
  Build.
- PR #192 was squash-merged as
  `f4fe41ddbe10a7f5c6531084bf9aaab9d6299924`; post-merge `main` run
  `29271703816` passed the same three jobs before target cutover.

No PRD5 source scope was reopened. This evidence promotes only the selected
target facts below; it does not promote the whole PRD6 matrix to Production
verified.

## Backup And Release Controls

- A pre-migration PostgreSQL custom-format backup exists at
  `/opt/unirouter/backups/pre-prd6-ce9307a-20260713T1540Z.dump` with mode `0600`.
- Backup size: 160,954 bytes.
- Backup SHA-256:
  `1366103601dd8e1a42ff97080b0da93edb4cad3d58465e75331a166596f00dd9`.
- `pg_restore -l` succeeded before migration; the source database size was
  approximately 11 MB.
- The initial release archive size was 5,675,318 bytes with SHA-256
  `52967c8896d8daa0e81fb23df2ad36868f53ebd916c9a24d1e2b40d25409d8cd`.
- The initial release was extracted to the commit-addressed directory
  `/opt/unirouter/releases/ce9307a7082ff5ad6e49c0380e36b18186e0c3be`.
- Existing stable, canary and frontend image IDs were retained under explicit
  rollback tags. The current `.env` and `.env.production` files were copied
  with mode `0600`; no environment credential value was changed or recorded.
- The final frontend-only release is
  `/opt/unirouter/releases/f4fe41ddbe10a7f5c6531084bf9aaab9d6299924`.
- Its archive is 5,676,124 bytes with SHA-256
  `af55f373cbd13a375c32628cdbd3ee82a719885110d23e498d0effbaca372404`.
  Final frontend image ID:
  `sha256:b16bcd0ab870cb425238c2adf6a603a7ceb5a88a6cc5f5375d81699031658eb1`.
- Release transfer invocation `t-wl06qtj295uxla8` verified the exact commit,
  archive digest, inline-favicon source and both `0600` environment files.

## Migration Execution And Reconciliation

The pre-deploy target contained application migrations through `000019`. The
PRD6 release requires `000020` through `000034`.

An attempted transaction-wrapped migration rehearsal exposed an operational
mistake: every PRD6 migration file contains its own `BEGIN` and `COMMIT`. The
first inner `COMMIT` ended the outer transaction, so the final `ROLLBACK` did
not undo the migrations. PostgreSQL emitted the relevant transaction warnings,
all fifteen scripts completed with `ON_ERROR_STOP=1`, and their schema changes
were committed while the migration ledger still lacked those fifteen rows.

The executor stopped before starting the new application, disclosed the event,
and reconciled the target as follows:

1. verified the explicit transaction boundary of each `000020` through
   `000034` up migration;
2. read back the expected PRD6 tables, columns and constraints rather than
   assuming rollback had occurred;
3. atomically inserted the exact fifteen applied versions into
   `schema_migrations`; and
4. ran the new PRD6 image's `./uniroute migrate status`, which reported every
   migration through `000034_prd6_stream_policy_termination` as applied.

Stable/canary health remained green throughout reconciliation. No destructive
restore was required, and no target data was deleted. PR #191 updates the
runbook to forbid an outer-transaction pseudo-dry-run for self-transactional
migration files and instead require an isolated restored database.

Relevant Cloud Assistant invocation IDs are retained for operator audit:

- initial migration execution: `t-wl06qt75umuxm2o`;
- schema and ledger reconciliation: `t-wl06qt799f2r6kg`;
- new-image migration preflight: `t-wl06qt7iwjla2gw`; and
- initial deployment: `t-wl06qt7l0e1shds`.

## Runtime Deployment Readback

- Backend stable image:
  `sha256:55088e726a5d59b4482695a3b3827fe85db3977c8ecab16657777b85ed19c553`.
- Backend canary image:
  `sha256:dc81dbc3f7e90909a6bd169dd787fc0a2b2b7751d69156c007b04194a8283ead`.
- Initial frontend image:
  `sha256:f422bcfe832fd1612bdaaccafdeba81140801cfe3d37b3793ac63f9d534bc6fb`.
- PR #191 frontend image:
  `sha256:040d84a44aa57a6205882fa9ac0d68de22b3ec3ddcd69190bc38aad5dcbc6bb3`.
- Final PR #192 frontend image:
  `sha256:b16bcd0ab870cb425238c2adf6a603a7ceb5a88a6cc5f5375d81699031658eb1`.
- Stable and canary report `RUNTIME_VERSION=prd6`, exact commit
  `ce9307a7082ff5ad6e49c0380e36b18186e0c3be`, and their respective
  `stable` / `canary` roles.
- Direct stable `:8080`, direct canary `:8081`, and public Nginx
  `/health/ready` all returned
  `{"status":"ok","db":"ok","redis":"ok"}`.
- The first final-release command, invocation `t-wl06qtj4vkxrls0`, built the
  correct source under a commit-derived Compose project name and stopped before
  cutover because the fixed `unirouter-frontend` image assertion did not match.
  Readback invocation `t-wl06qtj8s4x2ygw` proved that the old frontend image,
  work directory and public health were unchanged. No runtime rollback was
  needed.
- The corrected command explicitly fixed the existing Compose project name to
  `unirouter`. Invocation `t-wl06qtjc0iu9fcw` recreated only the frontend and
  passed public, stable and canary health plus inline-HTML checks. Both backend
  containers, PostgreSQL, Redis, Prometheus, Grafana and Nginx retained their
  exact pre-cutover container IDs.
- Final readback invocation `t-wl06qtjhe4yfj0g` returned all eight containers
  running, public/stable/canary
  `{"status":"ok","db":"ok","redis":"ok"}`, final frontend work directory
  `f4fe41ddbe10a7f5c6531084bf9aaab9d6299924`, and latest migration
  `000034_prd6_stream_policy_termination`.

The stable and canary containers run on the same ECS host. Canary traffic is a
release-safety mechanism, not machine-level HA. The runtime UI correctly
reports multi-instance topology as not configured; this target does not satisfy
P6-E04 capacity or HA/DR evidence.

## Authenticated Target Browser Evidence

An isolated platform-admin QA account was created through the supported CLI,
used only for authenticated console verification, and removed through the
supported admin API after the final browser check. No password or token is
recorded in this evidence.

| Surface | Target result |
|---|---|
| Dashboard | `/api/v1/dashboard/stats` returned 200 and the page rendered channel health, wallet, request, latency and usage-derived cards. |
| Shared limits | `/api/v1/limit-policies` returned 200. The UI rendered RPM/RPS/TPM/concurrency, weighted workload classes, shadow/enforce and bounded last-known-safe failure behavior. The target has no enabled policy, so this is control/UI verification, not enforcement or load evidence. |
| Content safety | `/api/v1/guardrail-policies` returned 200. The UI rendered input/non-stream/stream stages, detector/action/failure selectors, bounded payload/stream semantics and upstream-cost hold wording. The target has no enabled policy or real detector. |
| Channels and routing | The four existing channels rendered with provider, adapter, model, priority, equal-priority weight, threshold and breaker health. Read-only route preview returned a versioned snapshot and an explicit exclusion reason without calling an upstream. |
| Usage reconciliation | The page rendered 188 historical rows and renamed the ambiguous aggregate to `估算与实际总偏差`, explicitly defined as final metered total minus pre-request estimate. Prompt Cache read/write return status and cost fields were visible without inventing cache hits. |
| Request logs and correlation | Exact operation/client/Trace/workflow/Agent/attempt/upstream/evidence/hash filters rendered. Historical request `gatea-success-bfcd47b2b0679f8d` opened a seven-event trace with one operation ID across gateway, payload policy, billing freeze, routing, provider acceptance, billing settlement and logging. The drawer showed one route attempt, zero failovers, provider-returned usage and one explicit provider-ID capture gap. No new provider request was sent. |
| Runtime status | Database, Redis, Prometheus and Grafana were available. The UI rendered effective retry/circuit/cooldown, routing snapshot/probe/memory bounds, stream frame/idle/heartbeat/duration settings, the five-minute Agent capability contract and project-budget alert authority. |
| API docs | Agent exchange/delegation, limit-policy and Guardrail control routes plus typed failure responses rendered from the deployed contract. |
| Responsive layout | Shared Limits and Audit were checked at 390 x 844. The page layout remained usable and wide tables retained contained scrolling. |

The selected historical request detail satisfies P6-E01's separately authorized
target route/failover-detail requirement. It proves the target UI/readback for
one existing request; it is not a new paid failover exercise or capacity claim.

## Target-Confirmed UX Boundaries

The authenticated check also confirmed that the following audit improvements
are not in the deployed UI:

- the ledger records successful login and provides a source filter for
  management console, developer console, gateway API and system task, but does
  not yet provide the proposed six business-category views;
- actor, organization, project and target cells still use raw IDs such as
  `User #1` rather than joined names/snapshots;
- action and target codes remain technical strings; and
- start/end time remain RFC3339 text inputs rather than a date-range control.

These are not omissions from the 27 accepted PRD6 rows. P6-F37 closed the
tenant read boundary; the operator-UX items remain explicitly queued under the
PRD7 candidate `UI 与可运营性` row and the P6-AUDIT follow-up. They must not be
reported as implemented or used to reopen PRD6 silently.

The top bar also displays `环境未配置`. This is truthful: the frontend build
has no explicit `VITE_APP_ENV` / `VITE_DEPLOY_ENV`, while the backend runtime
uses the separate value `RUNTIME_ENVIRONMENT=local`. The target was not
silently labeled production. Selecting `开发环境` versus `预发环境` remains an
operator/product configuration decision.

The initial browser pass first exposed the missing `/vite.svg` starter asset.
PR #191 removed that reference, after which the browser's implicit
`/favicon.ico` lookup remained. PR #192 replaced both failure modes with an
inline icon. Final authenticated Playwright readback after the exact merged-main
cutover returned login, `/api/v1/me` and Dashboard 200; the Dashboard marker and
inline icon were present; console errors, failed requests, `/vite.svg` requests
and `/favicon.ico` requests were all zero. Screenshot:
`output/playwright/prd6-ecs-dashboard-final.png`.

## Cleanup And Remaining Boundaries

- The temporary QA browser session was cleared and the supported admin API
  deleted user ID 11; database readback returned zero matching temporary users:
  final readback invocation `t-wl06qtjhe4yfj0g` returned `qa_user_count=0`.
- No policy, external detector, provider credential, notification route,
  security group or external integration was enabled or changed.
- P6-E02 migration down/TLS 443, P6-E03 real upstream/enterprise SSO and P6-E04
  multi-instance capacity/HA/DR remain unverified and later.
- The pre-migration backup and rollback image tags remain retained.
- PRD6 remains Accepted Scope Complete. This target evidence does not claim
  Production verified behavior, 100x capacity, zero leakage, real detector
  quality, supplier-invoice reconciliation, managed HA, response Cache Service,
  async media, PRD7 implementation or X-Insight runtime work.
