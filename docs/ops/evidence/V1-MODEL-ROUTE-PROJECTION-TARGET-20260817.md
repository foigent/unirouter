# V1 Canonical Model Route Projection — Target Evidence

**Date:** 2026-08-17 (Asia/Shanghai)

**Target:** existing Alibaba Cloud development/test ECS at
`https://8.130.18.85/`; this is not a Production claim

**Result:** **Target verified / closed** for the canonical model-endpoint
operational projection shared by Model Endpoints and Dashboard. No Provider
request, credential change, model lifecycle mutation, financial mutation or
database migration ran during this release.

## Repository And CI Truth Chain

| Item | Exact evidence | Result |
|---|---|---|
| PR | #296, `Unify model route status projections` | Squash-merged |
| CI head / tree | `78f19bfe2fcd2c5fcb68995f5b8b0cb94d9f9cbc` / `5ce271327abd768e1809190bc6c67832a9227f36` | Fixed throughout required CI |
| CI run | `31999837473` | Change Routing 8 s; Backend 33 m 01 s; Frontend 4 m 26 s; Browser E2E 4 m 12 s; Docker 1 m 48 s; all passed |
| Merge / tree | `f5465cf99a09c96fc132af571765fdd6a6aae02c` / `5ce271327abd768e1809190bc6c67832a9227f36` | Merge tree equals the CI-passed head tree |

Local verification before CI included the complete admin integration suite in
36.166 seconds, 70 frontend files / 517 tests in 40.63 seconds, TypeScript and
the production build. The CI Backend wall time remains a measured engineering
debt: database-template reuse removed repeated migrations, but many small Go
integration commands still run serially. That optimization is a separate
follow-up and is not hidden by this release result.

## Bundle, Build And Backup

- Incremental Git bundle:
  `/opt/unirouter/bundles/unirouter-f5465cf99a09c96fc132af571765fdd6a6aae02c.bundle`.
- Bundle size / SHA-256: 258,983 bytes /
  `c4fea63560165c9eb5d3633ef0040a1ab941d31393caa975e1380fe9958a87a4`.
  Local and Target `git bundle verify` passed; the staged detached worktree
  read back the exact merge commit/tree and was clean.
- The first SCP attempt produced a zero-byte Target file. Size and Git bundle
  validation rejected it before fetch; the exact failed file was removed. The
  checksum-mode retransmission matched the Local SHA-256 before any staging or
  build action.
- Runtime version: `1.1.29-route-projection`.
- Backend image:
  `sha256:f69ed797a7cfc71242d75a21855cb870d7d44cc23a5366e957af3a1b14dd0998`.
- Frontend image:
  `sha256:86039b7d72e709eb19e9ae2fc7a553f8ea43945e8399cd64f0f32db51d3a7946`.
- The build helper passed its fail-closed storage gate at 70% used and 11.16
  GiB available. The official Go proxy was unreachable from this China-region
  Target; a bounded probe timed out while `https://goproxy.cn/` returned 200.
  The abandoned download process was terminated, its lifecycle lock released,
  and no candidate tag existed. The supported `GOPROXY=https://goproxy.cn,direct`
  retry retained `go.sum` and checksum-database verification and completed
  `go mod download` in 4.0 seconds.
- Frontend Target build transformed 4,335 modules in 31.50 seconds; the bundle
  budget passed with a 362,328-byte largest JavaScript asset against the
  460,800-byte limit.
- Pre-switch PostgreSQL custom-format backup:
  `/opt/unirouter/backups/pre-route-projection-f5465cf9-20260817T065329Z.dump`.
  Mode / size / SHA-256: `0600` / 690,450 bytes /
  `530dd11e60da9b9575cb5feae901c79b8a9988af4cb55d4c721f5a13758e7c53`.
  PostgreSQL 16 `pg_restore -l` passed before cutover.

## Target Switch And Runtime Verification

There is no migration diff from the deployed predecessor
`016ab005515b01e60246e43da1fd95a8474d4e1b`. The new image read all 65
`schema_migrations` rows through `000066_runtime_assignment_projection` as
applied. Switch order was canary, stable, then frontend. Each backend passed
Docker health before the next component changed.

Stable and canary now share the exact backend image, report commit
`f5465cf99a09c96fc132af571765fdd6a6aae02c`, version
`1.1.29-route-projection`, the commit-addressed release worktree and restart
count zero. Frontend reports the same commit/version and restart count zero.
PostgreSQL, Redis, Prometheus, Grafana and TLS Nginx were not recreated.

Public readback passed without disabling certificate verification:

- HTTP `/` returned 308 to `https://8.130.18.85/`;
- HTTPS `/` returned 200 with the security headers;
- `/health/live` returned `status=ok`;
- public stable and direct canary `/health/ready` returned
  `status=ok`, `db=ok`, `redis=ok`; and
- recent stable, canary and frontend logs contained no panic, fatal,
  segmentation, invalid-License or server-error signal.

The following read-only counts were identical before and after the switch:

| Fact | Before | After |
|---|---:|---:|
| migration ledger | 65 | 65 |
| organizations / users / projects | 1 / 2 / 1 | 1 / 2 / 1 |
| channels / channel credentials | 2 / 2 | 2 / 2 |
| model catalog records | 2 | 2 |
| runtime releases / slots | 2 / 2 | 2 / 2 |

## Authenticated Browser Acceptance

The existing Chrome administrator session loaded the newly deployed frontend
without console error or warning:

- Customer Overview showed `当前可路由连接 2/2` and explicitly stated that
  the value is decided by Model Endpoints and the Gateway current route.
- Model Endpoints showed two public models, two routable models, zero requiring
  action and both DeepSeek records as `稳定路由`.
- Embedded connection diagnosis required only a model dropdown, not a project.
  For DeepSeek V4 Flash it reported the commercial License, Provider
  connection, Provider credential and current route as `就绪`. The diagnostic
  remained read-only and did not call the Provider.
- Runtime Status showed
  `1.1.29-route-projection · f5465cf99a09`, healthy PostgreSQL/Redis and the
  same current runtime identity as Docker.

## Image Lifecycle And Boundaries

After stable verification, `/opt/unirouter/image-retention.keep` was atomically
advanced to N=`f5465cf99a09`, N-1=`016ab005515b`, N-2=`4b1b33d88fa6` for both
backend and frontend. The read-only lifecycle report resolved every keep
reference and reported seven running-protected images, eight additional
keep-protected images, zero stopped-container holds and zero deletion
candidates. No image or tag was removed. Root disk use after the release was
72% with 11 GiB available, still inside the 80% / 8 GiB build gate.

This evidence does not claim Production, paid Provider traffic, load/HA/DR, or
automatic image or release-directory deletion. The CI command-consolidation
debt measured during this release was subsequently completed by PR #298; see
`V1-BACKEND-CI-OPTIMIZATION-20260817.md`.
