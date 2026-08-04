# PRD8 Cost Optimization CO-1 CI And Target Verification

> Work item: `UR-PRD8-COST-OPTIMIZATION-V1`
> Evidence date: 2026-08-04 (Asia/Shanghai)
> Environment: existing Alibaba Cloud UniRouter development/test ECS
> Status: `CO1_CI_MERGED_AND_DEVELOPMENT_TARGET_VERIFIED`
> Production claim: none

## Outcome And Authorization

The Product Owner accepted the researched Cost Optimization decisions,
authorized autonomous implementation, and then authorized the complete
commit/CI/release chain for the existing UniRouter test environment. CO-0 and
CO-1 are now merged and visible in the authenticated Target console.

This release adds read-only Cost Optimization APIs and UI. It does not enable
Exact Response Cache, Compression, new Provider traffic, new credentials, the
second-integration data reset, ORG-3, FIN-4, Cursor/Checkpoint changes or any
X-Insight, Tool Gateway or MCP Gateway runtime.

## Repository, CI And Merge

- Baseline `main` commit/tree:
  `c93c12b3e082a39f6a29e3c95e3029e640980682` /
  `a826702cfd3ccfa060a217ac961fb62d12835730`.
- PR: [#255](https://github.com/foigent/unirouter/pull/255).
- Exact CI-passed head/tree:
  `20cf0303072713b42c3015ddbcb7211f5e0ca06c` /
  `3b9351b4a18ba032e9b9fa6772eeb71c7cecec2b`.
- Application CI run `30896154703` passed:
  - Change Routing: 8 seconds;
  - Frontend Build: 8 minutes 25 seconds;
  - Backend Tests: 21 minutes 2 seconds; and
  - Docker Build: 1 minute 31 seconds.
- PR Protocol Conformance run `30896151967`, job `91949405876`, passed in
  2 minutes 22 seconds.
- The PR was marked ready only after every check above reached terminal
  success and GitHub reported `CLEAN` / `MERGEABLE` for the unchanged head and
  base.
- Squash merge:
  `dde6eff214067a594f26b6aa080286f6cb4682d9`.
- The merge tree is byte-identical to the exact CI-passed head tree:
  `3b9351b4a18ba032e9b9fa6772eeb71c7cecec2b`.
- The repository does not run the complete application matrix on a `main`
  push. Post-merge Protocol Conformance run `30897936355` independently passed
  the exact merge commit. This evidence does not invent a post-merge Backend,
  Frontend or Docker run.

## Backup And Release Controls

- Pre-switch PostgreSQL custom-format backup:
  `/opt/unirouter/backups/pre-costopt-dde6eff2-20260804T095041Z.dump`.
- Backup mode / size / SHA-256:
  `0600` / 569,486 bytes /
  `c6f34c05cbb85a9531f746d91fda066aaa2cc4b71a7c45b693aace21a1da47f8`.
- The host does not provide `pg_restore`; the exact backup was therefore fed
  read-only to `pg_restore -l` inside the running PostgreSQL 16 container and
  passed validation.
- Incremental Git bundle:
  `/opt/unirouter/bundles/unirouter-dde6eff214067a594f26b6aa080286f6cb4682d9.bundle`.
- Bundle size / SHA-256:
  50,416 bytes /
  `9074b02217e69a0ae1cb9d5ab28934e603b01771d1045361702e0b007b0efaf2`.
- Local and Target bundle verification both passed. The bundle requires the
  exact deployed predecessor `f81d8431aed9fea1f1ff63467f2c4df6cecfd2f5`
  and contains `dde6eff214067a594f26b6aa080286f6cb4682d9`.
- Commit-addressed release directory:
  `/opt/unirouter/releases/dde6eff214067a594f26b6aa080286f6cb4682d9`.
- Target checkout readback returned the exact merge commit/tree and a clean
  tracked/untracked Git state before the build.
- The current release had one mode-`0600` `.env` and no `.env.production`.
  The exact `.env` was copied without reading or printing secret values. Only
  `RUNTIME_VERSION` and `RUNTIME_COMMIT` were changed in the new release;
  `RUNTIME_ENVIRONMENT` and `VITE_DEPLOY_ENV` remained `development`.
- The prior running images were retained under explicit rollback tags:
  `unirouter-rollback/backend:f81d8431`,
  `unirouter-rollback/backend-canary:f81d8431`, and
  `unirouter-rollback/frontend:f81d8431`.

## Target Build And Deployment

The ECS built backend, canary and frontend sequentially to bound peak disk
usage. The Target frontend build independently transformed 4,319 modules and
passed the 460,800-byte JavaScript asset budget: 80 assets, largest asset
362,347 bytes.

Exact running identities after the switch:

| Component | Image identity | Runtime result |
|---|---|---|
| stable backend | `sha256:d6cdba4e446f367b7a71c927ed83088cc20f24969568dc0d38d3ad7e9a13b6d7` | running, healthy, restart `0` |
| canary backend | `sha256:e542c9a845c07f3f3b075a06e843e35a7b54d31d13b823f21c8a0ba986300a8c` | running, healthy, restart `0` |
| frontend | `sha256:43c3872803105942386a2aad4d670706a000aec17c9bb9b07fbd4521b2d9bb89` | running, restart `0` |

The switch order was canary, stable, then frontend. Every service was recreated
with `--no-deps --no-build --force-recreate --wait`; each health gate passed
before the next component changed. PostgreSQL, Redis, Prometheus, Grafana and
the outer TLS Nginx were not recreated. Their recorded start times remain from
July 17 or August 3 and every restart count remains zero.

The outer Nginx passed `nginx -t`; the HTTPS certificate-renewal timer remains
active and enabled. Runtime Status reports
`1.1.3-prd8-costopt · dde6eff21406`, environment `development`, role `stable`,
and available PostgreSQL, Redis, Prometheus and Grafana.

## Zero-Drift Data Readback

The same read-only queries ran immediately before the backup and after the
complete switch. Every result was equal:

| Fact | Before | After |
|---|---:|---:|
| migrations / maximum | `54 / 000055_prd8_deployment_profiles` | same |
| organizations / users / projects | `1 / 20 / 1` | same |
| API keys / channels / channel credentials | `56 / 3 / 3` | same |
| call logs / transactions | `60 / 286` | same |
| retained Feed items / maximum retained sequence | `0 / 0` | same |
| Feed retained floor sum / acknowledged sequence sum | `12 / 12` | same |
| workload applications / deployment profiles / Gateway Endpoints | `0 / 0 / 0` | same |
| pending runtime hooks / internal Outbox / unresolved Provider attempts / non-terminal reservations | `0 / 0 / 0 / 0` | same |

The empty retained Feed page is not treated as a reset: its durable retained
floor and acknowledged sequence were already `12` before this release and
remained `12` afterward. No migration was added or applied. No organization,
user, project, application, API key, channel, credential, request, transaction,
Feed event, Source Instance, Cursor or Checkpoint was created or changed.

## Public And Authenticated Verification

Public checks after the switch passed:

| Check | Result |
|---|---|
| `http://8.130.18.85/` | `308` to `https://8.130.18.85/` |
| HTTPS root | `200` |
| HTTPS `/health/ready` | `200`, PostgreSQL and Redis `ok` |
| unauthenticated Cost Optimization summary | `401` |
| unauthenticated `/v1/models` | `401` |
| stable / canary direct readiness | both healthy |

The existing authenticated Chrome session was reused. No password, token or
secret was read or submitted. No Provider test control or model request was
executed.

Authenticated UI readback confirmed:

- the left navigation contains `成本与财务 → 成本优化` with the agreed icon and
  all second-level navigation remains expanded;
- the page clearly distinguishes Provider Prompt Cache (`已计量`), Exact
  Response Cache (`Gateway 未接入`) and Compression
  (`默认关闭 / Shadow 未启动`);
- the current 30-day Target data renders an ordinary-input baseline of
  `¥0.810139`, evidence coverage `25.5%` (`13 / 51` successful upstream
  requests), a `69.0%` cache-read Token ratio, five model rows and three
  deterministic evidence-gap candidates;
- the current net/read/write monetary results are all `¥0.000000`. This is a
  faithful result of the existing frozen customer-pricing evidence, not a
  claim that cache usage is absent or that missing evidence equals zero;
- the Evidence Inspector is visible without an extra open action and shows
  the selected row's exact baseline, actual cost, read/write Token partitions,
  evidence coverage and trust boundary; and
- Runtime Status renders the exact new version/commit and healthy dependencies.

At 1,440 CSS pixels the result/candidate region and Evidence Inspector remain
side by side. At 1,024 and 767 CSS pixels they stack vertically. All three
sizes had zero horizontal overflow. The browser was restored to its default
2,060-pixel viewport and left on `/cost-optimization`; captured console output
contained zero warnings and zero errors.

## Compatibility Conclusion

This release reads existing terminal `call_logs` facts and frozen customer
price evidence. It changes no request admission, routing, Provider dispatch,
credential behavior, settlement, wallet finality, Outbox identity, Source
Snapshot bytes/hash, completed-execution Feed endpoint/DTO/order or
Cursor/Checkpoint behavior.

Supplier COGS remains owned by Provider reconciliation. Cross-execution causal
investigation remains owned by X-Insight. Tool result caching remains owned by
Tool Gateway, and MCP resource/session caching remains owned by MCP Gateway.
No shared high-privilege plugin runtime was introduced.

## Remaining Boundaries And Risks

- CO-2/CO-3 Exact Response Cache runtime still requires its explicit schema,
  privacy, stampede-control, settlement and rollout gate.
- CO-4/CO-5 Compression still requires offline fixtures, quality gates,
  Shadow evidence and a separate Product Owner approval before any canary.
- ORG-3 and FIN-4 still require named customer systems and are not simulated.
- The second-integration data reset, fresh Provider channels/credentials and
  real Agent traffic remain separate and were not started.
- The root filesystem has approximately 6.1 GB free and is 84% used. No image
  prune, bulk deletion or destructive cleanup ran.
- The existing Compose top-level `version` warning remains. It did not affect
  the deployment and should be handled in a separate minimal maintenance
  change.
- This is development/test Target evidence, not customer Production, paid
  Provider traffic, load, HA/DR, failover, customer directory/ERP or
  second-device POC evidence.
- This evidence document and its later docs-only merge are not part of the
  deployed runtime. The Target remains pinned to code commit
  `dde6eff214067a594f26b6aa080286f6cb4682d9`.
