# PRD4 Batch 1R Runtime Build Chain Local Evidence

**Date:** 2026-07-08
**Scope:** `PRD4-RUNTIME-002` repo-controllable runtime/build-chain follow-up
**Risk:** R2/R4-adjacent runtime configuration and deployment-build evidence
**Result:** fixed locally, awaiting Canary

## Trigger

`docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md` recorded remaining
runtime/build-chain gaps after the PRD4 Batch 1N target smoke:

- Go standard-library vulnerabilities requiring a newer Go patch release;
- target Grafana URL misconfiguration;
- missing runtime version/commit injection;
- `RUNTIME_ENVIRONMENT` unset;
- missing `uniroute-api` container health marker;
- target test database cleanup pending Atlas.

## Changes

- `go.mod` now pins `toolchain go1.25.12`.
- `Dockerfile` now builds with `golang:1.25.12-alpine` and injects
  `main.buildVersion` / `main.buildCommit` through `-ldflags`.
- `cmd/uniroute/main.go` uses build-injected metadata as the fallback for
  `RUNTIME_VERSION` and `RUNTIME_COMMIT` when env vars are blank.
- `docker-compose.yml` passes `RUNTIME_VERSION` / `RUNTIME_COMMIT` build args,
  defaults `RUNTIME_ENVIRONMENT` to `local`, uses `GRAFANA_URL=http://grafana:3000`,
  and adds a backend `healthcheck` against `/health/ready`.
- `.env.example` and `docs/ops/deployment-runbook.md` now use the compose-safe
  Grafana URL and document release metadata injection.

## Local Verification

```bash
go test ./cmd/uniroute ./internal/config ./internal/admin -run 'TestApplyBuildRuntimeMetadata|TestLoad_RuntimeExposureEnvOverrides|TestRuntimeStatus' -count=1
```

Result:

```text
ok github.com/uniroute/unirouter/cmd/uniroute
ok github.com/uniroute/unirouter/internal/config
ok github.com/uniroute/unirouter/internal/admin
```

```bash
go test ./... -count=1
```

Result: all Go packages passed.

```bash
go build -ldflags='-X main.buildVersion=20260708.test -X main.buildCommit=abc1234' -o /tmp/uniroute-prd4-b1r ./cmd/uniroute
rm /tmp/uniroute-prd4-b1r
```

Result: build passed and the explicit temporary binary was removed.

```bash
go env GOVERSION GOTOOLCHAIN
```

Result:

```text
go1.25.12
auto
```

```bash
ruby -e 'require "yaml"; YAML.load_file("docker-compose.yml"); YAML.load_file("docker-compose.canary.yml") if File.exist?("docker-compose.canary.yml"); puts "yaml ok"'
```

Result:

```text
yaml ok
```

```bash
git diff --check
```

Result: clean.

## Not Verified Locally

`docker compose config` and live container health checks were not run because the
current local machine does not have the `docker` CLI available (`command not
found: docker`). Canary, Sentinel CI, or Atlas should verify the compose config
and container `healthy` state in an environment with Docker.

## Remaining Target-Environment Boundary

This batch does not delete the target `uniroute_test` database and does not claim
production deployment. Target test database cleanup remains an Atlas/target
environment task tracked separately as `PRD4-DEPLOY-002`.
