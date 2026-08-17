# V1 Backend CI And Test Database Optimization Evidence

**Date:** 2026-08-17 (Asia/Shanghai)

**Result:** **Complete** for the measured backend CI repetition and migrated
test-database setup debt. This change affects tests and CI only; no runtime
binary, schema, customer data, credential, traffic or Target deployment
changed.

## Repository And CI Truth Chain

| Item | Exact evidence | Result |
|---|---|---|
| Baseline | PR #296 run `31999837473`, Backend job `95297985236` | 33m01s |
| PR | #298, `Consolidate backend integration test gates` | Squash-merged |
| Final head / tree | `5146aa8e20b837dec9d9d03b892537e9e2cfcf7e` / `44f38774034febf3a8f1a2602e860f2c9ce468f4` | Fixed throughout final required CI |
| Final run | `32007253572`, Backend job `95319224493` | 10m38s |
| Merge / tree | `3cc6c448392e387631255fce41cc04fa38d68672` / `44f38774034febf3a8f1a2602e860f2c9ce468f4` | Merge tree equals the CI-passed head tree |

The final fixed-head run passed Change Routing, Backend, Frontend, PR4 Browser
E2E and Docker. Frontend completed in 5m20s, Browser E2E in 3m45s and Docker in
1m53s. Their product coverage did not change in this backend-only batch.

## Measured Result

| Comparable GitHub step | Before | After | Change |
|---|---:|---:|---:|
| Backend job | 33m01s | 10m38s | -22m23s / about 68% |
| Ordinary `go test -race ./...` | 6m16s | 4m52s | -1m24s |
| Source-evidence PostgreSQL | 29s | 7s | -22s |
| Admin integration | 3m59s | 1m38s | -2m21s |
| Former feature steps 17-50 | 20m37s | one consolidated gate at 2m42s | -17m55s |

The Backend timeout is now 25 minutes rather than 50 minutes. This leaves more
than twice the measured final duration for cold-cache and runner variance while
restoring a useful regression bound.

## Implementation Contract

- `SetupMigratedTestDB` still gives every test its own temporary PostgreSQL
  database. Heavy packages now build and clean one process-scoped fully
  migrated template, then use PostgreSQL `CREATE DATABASE ... TEMPLATE` for
  isolated clones.
- Package `TestMain` owns template cleanup and makes a cleanup failure fail the
  package rather than silently leaking a database.
- `NewTestOrg` no longer repeats `TruncateAll` after the caller already created
  a clean test database. Historical partial-migration fixtures that inserted an
  explicit organization ID receive only a narrow primary-key sequence repair;
  no business-table cleanup is hidden in fixture creation.
- The accepted named integration selectors were unioned into one invocation so
  each selected Go package starts once. Untagged tests remain owned by the
  earlier complete `go test -race ./...` suite; Admin and Source-evidence retain
  dedicated integration gates.
- Agent capability, distributed limit policy and workload admission continue
  serially because they intentionally share and reset the dedicated Redis test
  database. A trial concurrent run reproduced cross-package Redis interference;
  the final design preserves isolation without adding Redis instances or CI
  jobs.

## Local Verification

- YAML parse and `git diff --check`: passed.
- `go vet ./...`: passed.
- complete ordinary `go test -race -timeout=20m ./...`: 55.50s, passed.
- final consolidated integration plus race command: 48.82s, passed.
- complete `internal/migrate` integration package: 27.014s, passed.
- expanded PostgreSQL integration regression across Billing, Migrate,
  Middleware, Enterprise Finance, X-Insight Connector, Organization Directory,
  Runtime Change, Reset, Gateway, Source Evidence, Completed Feed, Outbox,
  Media Job and Model Governance: passed after the explicit-ID sequence fixture
  regression was corrected.
- Admin integration: 30.93s versus the prior 36.166s Local baseline.
- Local `uniroute_it_*` database count was zero before and zero after every
  final database gate.

Before implementation, 48 inactive, exactly named `uniroute_it_*` databases
from interrupted historical tests were verified as unreferenced and removed.
The cleanup did not touch `postgres`, `uniroute_test`, application data or
credentials. Subsequent final gates left zero temporary databases.

## Boundaries

This evidence does not claim that every possible future integration test is
fast, that shared Redis state is parallel-safe, or that GitHub runner timings
are constant. It establishes that the current required coverage passed twice
on GitHub, the measured repetition was removed, the timeout again bounds
regression, and no runtime deployment was needed for this test-only change.
