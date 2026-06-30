# PRD1 Remaining Acceptance Pack

Status: Codex local closeout evidence and external acceptance queue
Last updated: 2026-07-01

## Purpose

This file separates PRD1 items that Codex verified locally from items that require a capability not available in this workspace, such as Docker, real upstream providers, browser/manual visual validation, or cloud deployment.

## Local Evidence Completed By Codex

Environment:

- Workspace: `/Users/m4/Desktop/projects/UniRouter`
- Local backend source verified with `go run ./cmd/uniroute` on port `18080`
- Existing local services detected: PostgreSQL on `127.0.0.1:5432`, Redis on `127.0.0.1:6379`
- Docker: unavailable in this workspace (`docker: command not found`)

Commands completed:

```bash
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
```

Results:

- Go tests: PASS
- Frontend tests: PASS, 36 tests
- Frontend build: PASS, with existing Vite large chunk warning
- Route parity guard: PASS
- Temporary current-source backend health: PASS
- Temporary current-source `/metrics`: PASS, returned Prometheus metric output
- Temporary current-source admin login: PASS
- Temporary current-source `/api/v1/me`: PASS
- Temporary current-source `/api/v1/settings`: PASS
- Temporary current-source `/api/v1/dashboard/stats`: PASS
- Temporary current-source `/api/v1/me/password` unauthenticated behavior: PASS, returned 401 through `AdminAuth`

## Alibaba Cloud Smoke Evidence

Environment:

- Frontend / nginx base: `http://8.130.18.85:3000`
- Backend direct health: `http://8.130.18.85:8080`
- Prometheus: `http://8.130.18.85:9090`
- Grafana: `http://8.130.18.85:3001`
- Verified: 2026-07-01

Commands/results:

```bash
curl -i http://8.130.18.85:8080/health/live
# 200 {"status":"ok","service":"uniroute"}

curl -i http://8.130.18.85:8080/health/ready
# 200 {"status":"ready"}

curl -i http://8.130.18.85:3000/
# 200 frontend HTML, asset bundle index-kz7ioahi.js

curl -i http://8.130.18.85:3000/health/ready
# 200 {"status":"ready"}

curl -i http://8.130.18.85:9090/-/healthy
# 200 Prometheus Server is Healthy.

curl -i http://8.130.18.85:3001/login
# 200 Grafana login HTML
```

Admin API smoke through nginx:

- `POST /api/v1/auth/login`: PASS, returned JWT for admin
- `GET /api/v1/me`: PASS, returned active admin user
- `GET /api/v1/dashboard/stats`: PASS
- `GET /api/v1/channels?page=1&page_size=5`: PASS
- `GET /api/v1/keys?page=1&page_size=5`: PASS
- `GET /api/v1/channel-keys?page=1&page_size=5`: PASS
- `GET /api/v1/wallet`: PASS
- `GET /api/v1/wallet/transactions?page=1&page_size=5`: PASS
- `GET /api/v1/logs?page=1&page_size=5`: PASS
- `GET /api/v1/model-prices?page=1&page_size=5`: PASS
- `GET /api/v1/model-names`: PASS
- `GET /api/v1/org-model-prices?org_id=1&page=1&page_size=5`: PASS
- `GET /api/v1/reconciliation?page=1&page_size=5`: PASS
- `GET /api/v1/users?page=1&page_size=5`: PASS
- `GET /api/v1/grants?page=1&page_size=5`: PASS
- `GET /api/v1/audit-logs?page=1&page_size=5`: PASS
- `GET /api/v1/settings`: PASS
- `GET /api/v1/sensitive-words?page=1&page_size=5`: PASS

Negative auth smoke:

- `GET /v1/models` without API key through nginx: PASS, returned 401 `auth_error.invalid_api_key`
- `GET /api/v1/channels?page=1&page_size=1` without admin token through nginx: PASS, returned 401 `auth_error.missing_token`

Metrics smoke:

- `GET http://8.130.18.85:8080/metrics`: PASS, returned Prometheus text metrics
- `GET http://8.130.18.85:9090/-/healthy`: PASS, returned Prometheus healthy response

Notes:

- `http://8.130.18.85:3000/metrics` returns frontend HTML, so metrics should be verified via backend `:8080/metrics` or Prometheus `:9090`.
- Scope was intentionally non-destructive: no real provider paid requests, no API key/channel/wallet mutations, no load test, no stream disconnect, and no failover/fault injection.
- Cloud smoke exposed a handler quality issue where nullable database fields could be scanned into non-nullable Go types while `rows.Scan` errors were ignored, producing zero-value timestamps. Codex fixed this locally for API key and call-log list handlers; deployment needs a new build before that fix is visible in cloud.
- Strict closeout smoke on 2026-07-01 found the public cloud accepted `admin/admin123456` and returned a JWT. This is a PRD1 security blocker until the patched build is deployed and the default login returns 401/403.

## Code Fixes Completed

### PRD1-ENG-001: `/me/password` contract mismatch

Completed:

- Added backend handler `ChangePassword`
- Registered `PUT /api/v1/me/password`
- Added bcrypt old-password verification
- Added bcrypt new-password hashing
- Added authenticated current-user scoping through `AdminAuth` context
- Added frontend confirm-password equality check
- Added unit tests for success, wrong old password, and unauthenticated requests
- Added API client test for `changePassword`

Files:

- `internal/admin/auth_handlers.go`
- `cmd/uniroute/main.go`
- `internal/admin/admin_mock_test.go`
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/api/__tests__/client.test.ts`

### PRD1-ENG-002: frontend/backend route parity

Completed:

- Added `cmd/uniroute/route_parity_test.go`
- Guard covers current frontend admin API routes, including `/api/v1/me/password`

### DOC-005: OpenAPI Admin spec drift

Completed:

- Identified that the previous `docs/tech-ref/05-openapi-spec.yaml` contained older `/api/v1/admin/*` routes
- Rewrote `docs/tech-ref/05-openapi-spec.yaml` as a current PRD1 API path snapshot
- Verified the YAML parses and contains no `/api/v1/admin/*` paths
- Updated `CODEBASE-MAP.md`, `DOC-INVENTORY.md`, and `docs/tech-ref/README.md`

Remaining:

- Expand schema detail before using generated clients in PRD2

### PRD1-ENG-003: admin list scan errors can produce zero-value fields

Completed:

- Made `ListAPIKeys` null-safe for `api_keys.budget_limit` with `COALESCE`
- Made `ListLogs` null-safe for `call_logs.upstream_error` with `COALESCE`
- Added row-level `Scan` error handling in both list handlers
- Added regression tests for scan-error paths

Files:

- `internal/admin/key_handlers.go`
- `internal/admin/log_handlers.go`
- `internal/admin/admin_mock_test.go`

Cloud follow-up:

- After deployment, authenticated GET `/api/v1/keys?page=1&page_size=5` returned 200.
- After deployment, authenticated GET `/api/v1/logs?page=1&page_size=5` returned 200.
- Verified on Alibaba Cloud on 2026-07-01.

### PRD1-SEC-002: public default admin password works in cloud

Completed locally:

- Web login rejects `admin/admin123456` unless `UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN=true`.
- Added `./uniroute admin set-password --username admin --password <strong-password> --org-id 1`.
- Added `./uniroute admin create --username <name> --password <strong-password> --role admin --org-id 1`.
- CLI refuses to create or set `admin/admin123456` by default.
- Added regression tests for default admin login blocking and CLI validation.

Remaining:

- OpenClaw Canary must verify code/tests/docs.
- Sentinel/Atlas must redeploy.
- Cloud smoke must show `admin/admin123456` returns 401/403.

### PRD1-OPS-001: `/health/ready` PRD response shape

Completed locally:

- `/health/ready` now returns `{"status":"ok","db":"ok","redis":"ok"}` when PostgreSQL and Redis are healthy.

Remaining:

- OpenClaw Canary must verify code/tests/docs.
- Sentinel/Atlas must redeploy.
- Cloud smoke must show the PRD response shape.

## External Acceptance Queue

These items are not honestly closable from this workspace.

| ID | Required Capability | Owner Stage | Blocking Reason | Evidence Needed |
|----|---------------------|-------------|-----------------|-----------------|
| EXT-001 | Docker Compose / cloud deployment | Sentinel/Atlas | Basic Alibaba Cloud smoke passed; local Docker remains unavailable | Keep deployment logs/CI output as final release evidence |
| EXT-002 | Real upstream provider E2E | Sentinel/Atlas or configured test environment | Requires valid provider keys and configured channels | Non-stream and stream `/v1/chat/completions` responses with `_uniroute`, call log rows, wallet before/after |
| EXT-003 | Interrupted streaming settlement | Capable runtime tester | Requires controlled client disconnect against real stream | Call log with estimated usage and wallet settlement evidence |
| EXT-004 | 100 concurrent billing requests | Capable runtime tester | Requires load tool and controlled test wallet | No negative balance, idempotency/freeze rows, latency/error summary |
| EXT-005 | Failover and circuit breaker | Capable runtime tester | Requires controlled upstream failures | Primary channel fail counts, backup channel selected, circuit state/cooldown evidence |
| EXT-006 | Browser/manual frontend validation | Human or browser-capable agent | Canary/OpenClaw has no visual capability | Walkthrough notes for Dashboard, Channels, Keys, Wallet, Logs, Settings |
| EXT-007 | Grafana dashboard visual verification | Atlas or browser-capable operator | Grafana login is reachable; panels not visually verified by Codex | Dashboard import/provisioning evidence and panel screenshot/manual notes |
| EXT-008 | Full typed OpenAPI schemas | Codex/Sentinel before typed-client work | Current spec is path-accurate but intentionally uses generic schemas for many admin resources | Detailed schemas matching handler payloads |
| EXT-009 | Default admin remediation redeploy | Sentinel/Atlas | Current public cloud accepted `admin/admin123456`; local fix is not production-verified yet | Patched deployment plus `scripts/prd1-cloud-smoke.sh` showing default login rejected |

## Recommended Handoff

1. Canary should verify the code/docs in this pack without modifying files.
2. If Canary passes, Sentinel can include local test/build evidence in the PR.
3. Sentinel should run CI and, if available, Docker Compose validation.
4. Atlas should run cloud deployment and complete EXT-001, EXT-002, EXT-007.
5. Browser/manual UI validation remains separate from Canary.
