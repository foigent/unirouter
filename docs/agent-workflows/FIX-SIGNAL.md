# FIX-SIGNAL.md - PRD4 Batch 1R Runtime Build Chain Follow-up

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R2/R4-adjacent runtime configuration and build-chain evidence
**Scope:** `PRD4-RUNTIME-002` repo-controllable gaps

## Verification Request

Please verify PRD4 Batch 1R: Runtime Build Chain Follow-up.

This batch addresses the repo-controllable findings from
`docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`:

- Go standard-library vulnerability follow-up through `go1.25.12` toolchain pin;
- Docker build version/commit injection;
- runtime metadata fallback when `RUNTIME_VERSION` / `RUNTIME_COMMIT` env vars
  are blank;
- compose/env/runbook Grafana service URL correction;
- compose runtime environment default;
- backend container healthcheck marker.

Target test database cleanup is not a repo-code fix and remains split to
`PRD4-DEPLOY-002`.

## Scope To Verify

Files:

- `go.mod`
- `Dockerfile`
- `docker-compose.yml`
- `.env.example`
- `cmd/uniroute/main.go`
- `cmd/uniroute/main_test.go`
- `docs/ops/deployment-runbook.md`
- `docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PRD-Phase4.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/CODEBASE-MAP.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Expected Behavior

- `go.mod` declares `toolchain go1.25.12`.
- `Dockerfile` uses `golang:1.25.12-alpine` and injects
  `main.buildVersion` / `main.buildCommit` with `-ldflags`.
- `cmd/uniroute` preserves configured `RUNTIME_VERSION` / `RUNTIME_COMMIT` and
  falls back to build-injected metadata when those env vars are blank.
- `docker-compose.yml` passes build args, defaults `RUNTIME_ENVIRONMENT=local`,
  defaults `GRAFANA_URL=http://grafana:3000`, and adds a backend healthcheck for
  `/health/ready`.
- `.env.example` and the deployment runbook reflect the compose-safe Grafana URL
  and release metadata injection.
- `PRD4-RUNTIME-002` is `Fixed awaiting Canary`.
- Target test database cleanup is tracked separately as `PRD4-DEPLOY-002`.

## Non-Scope

Do not expect this batch to:

- change frontend behavior, API routes, migrations, billing, routing, SSO,
  upstream adapters, trace storage, or RBAC;
- delete target databases or mutate target infrastructure;
- claim target deployment, live container health, or `Production verified`;
- reopen real upstream or real SSO target execution from PRD5.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "go1.25.12|toolchain go1.25.12|buildVersion|buildCommit|RUNTIME_VERSION|RUNTIME_COMMIT|http://grafana:3000|healthcheck|PRD4-RUNTIME-002|PRD4-DEPLOY-002|PRD4-B1R-runtime-build-chain-local-20260708" go.mod Dockerfile docker-compose.yml .env.example cmd/uniroute/main.go cmd/uniroute/main_test.go docs/ops/deployment-runbook.md docs/issues/OPEN-ISSUES.md docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md docs/CODEBASE-MAP.md docs/agent-workflows/FIX-SIGNAL.md

go test ./cmd/uniroute ./internal/config ./internal/admin -run 'TestApplyBuildRuntimeMetadata|TestLoad_RuntimeExposureEnvOverrides|TestRuntimeStatus' -count=1

go test ./... -count=1

go build -ldflags='-X main.buildVersion=20260708.test -X main.buildCommit=abc1234' -o /tmp/uniroute-prd4-b1r ./cmd/uniroute

rm /tmp/uniroute-prd4-b1r

go env GOVERSION GOTOOLCHAIN

ruby -e 'require "yaml"; YAML.load_file("docker-compose.yml"); YAML.load_file("docker-compose.canary.yml") if File.exist?("docker-compose.canary.yml"); puts "yaml ok"'

git diff --check
```

If Docker is available, additionally run:

```bash
docker compose config
```

If a full target/container environment is available, verify `uniroute-api` moves
to healthy after dependencies are healthy. That is optional for Canary if Docker
is unavailable, but should be completed by Sentinel CI or Atlas before any
Production verified runtime claim.

## Acceptance Criteria

1. Source/config grep confirms Go toolchain patch pin, Docker `ldflags`,
   runtime metadata fallback, compose Grafana URL, backend healthcheck, and
   status docs.
2. Focused Go tests pass.
3. Full Go regression passes.
4. `go build` with the same `-ldflags` variables succeeds.
5. `go env GOVERSION GOTOOLCHAIN` shows `go1.25.12` and `auto`.
6. YAML parse passes.
7. `git diff --check` passes.
8. `PRD4-RUNTIME-002` is `Fixed awaiting Canary`, while target DB cleanup remains
   open as `PRD4-DEPLOY-002`.
9. No PRD4 row is promoted to `Production verified`.
