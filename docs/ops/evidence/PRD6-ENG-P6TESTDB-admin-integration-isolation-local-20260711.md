# PRD6 P6-TEST-DB-001 Admin Integration Isolation Evidence

> Date: 2026-07-11
> Scope: UniRouter repository test and required-CI integrity only
> Risk: R1 test harness / CI; no production-path behavior change
> Evidence level: Local verified; required PR CI pending
> Deployment/target/production claim: none

## 1. Confirmed pre-change failure

The legacy `internal/admin/admin_integration_test.go` file used
`testutil.SetupTestDB(t)`. That helper connects to one externally provisioned
`uniroute_test` database and truncates whatever schema happens to be present; it
does not run repository migrations and does not isolate tests from other
processes.

With local PostgreSQL accepting connections, this command failed:

```text
go test -tags=integration ./internal/admin -count=1
```

The run reported twelve failing handler tests. Confirmed schema errors included
missing `projects`, `request_idempotency`, notification/model-governance tables,
and `audit_logs.project_id`. These were stale shared-schema failures rather than
current handler regressions.

## 2. Change

- all 48 database setups in the legacy admin integration file now use
  `testutil.SetupMigratedTestDB(t)`;
- each test creates a uniquely named temporary PostgreSQL database, applies all
  repository migrations through `000023`, and drops that database during test
  cleanup;
- the Backend required-CI job now runs the complete admin integration package
  with the `integration` build tag and Go race detector after the normal suite;
- required CI sets `REQUIRE_TEST_DATABASE=1`, converting database connection,
  creation or ping skips into hard failures so a missing service cannot produce
  a false-green gate. Local developer runs retain the existing skip behavior
  when that explicit variable is absent.

The change does not alter an API, handler, migration, runtime configuration,
authorization rule, billing rule, deployment, credential, or data scope.

## 3. Local verification

```text
go test -tags=integration ./internal/admin -count=1
```

Result: PASS in 16.535 seconds.

```text
go test -tags=integration ./internal/admin -count=2
```

Result: PASS twice in one invocation in 32.782 seconds.

```text
go test -tags=integration -race ./internal/admin -count=1
```

Result: PASS in 31.095 seconds. The macOS linker emitted the already known
`LC_DYSYMTAB` warning; the race detector reported no race.

```text
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
```

Result: PASS for all packages. The full race run retained only the known
non-failing macOS linker warning.

The exact required-CI database posture was also verified locally:

```text
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
```

Result: PASS in 31.086 seconds. With the same variable set and the database URL
deliberately pointed to an unreachable local port, the selected integration
test failed with `cannot ping PostgreSQL admin database`; it did not skip or
return a false-green result.

```text
npm test -- --run
npm run type-check
npm run build
```

Result: PASS for 27 files / 137 tests, type-check and production build. The
existing bundle-size warning remains non-failing.

The post-test PostgreSQL query for databases named `uniroute_it_%` returned no
rows, proving the observed runs left no temporary database behind.

## 4. Acceptance and boundary

`P6-TEST-DB-001` can advance to Local verified because the previously failing
legacy admin suite is now current-schema, isolated, repeatable and included in
the repository's required CI definition. It must not advance to CI verified
until the exact PR head passes Backend Tests, Frontend Build and Docker Build.

This engineering-integrity issue is outside the 25 accepted PRD6 feature-row
count. Closing it does not complete P6-F03 through P6-F11 or P6-F19 through
P6-F21.
