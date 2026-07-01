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
scripts/prd1-cloud-smoke.sh
curl -fsS http://8.130.18.85:3000/docs
curl -fsS http://8.130.18.85:8080/docs
```

Results:

- Go tests: PASS
- Frontend tests: PASS, latest source has 38 tests after Dashboard trend control coverage
- Frontend build: PASS, with existing Vite large chunk warning
- Route parity guard: PASS
- Cloud S0/S1 smoke: PASS after redeploy
- Cloud frontend `/docs`: PASS, returns frontend SPA shell
- Cloud backend `/docs`: PASS, returns `UniRouter API Documentation`
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
# 200 {"status":"ok","db":"ok","redis":"ok"}

curl -i http://8.130.18.85:3000/
# 200 frontend HTML, asset bundle index-kz7ioahi.js

curl -i http://8.130.18.85:3000/health/ready
# 200 {"status":"ok","db":"ok","redis":"ok"}

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
- Cloud smoke exposed a handler quality issue where nullable database fields could be scanned into non-nullable Go types while `rows.Scan` errors were ignored, producing zero-value timestamps. Codex fixed this for API key and call-log list handlers; after deployment, authenticated cloud reads for `/api/v1/keys` and `/api/v1/logs` returned 200.
- Strict closeout smoke after redeploy on 2026-07-01 passed. The public cloud rejected `admin/admin123456` with 403, and `/health/ready` returned the PRD DB/Redis shape.

## Alibaba Cloud Dashboard/Grafana Follow-up

Environment:

- Frontend / nginx base: `http://8.130.18.85:3000`
- Prometheus: `http://8.130.18.85:9090`
- Grafana: `http://8.130.18.85:3001`
- Verified: 2026-07-01 after the user reported Dashboard trend and Grafana concerns

Results:

- Dashboard trend API returned range-specific buckets. For the sampled 24h window, non-zero requests were in the latest two hourly buckets; for 7d/30d, non-zero buckets were limited to the days with actual calls. This means the backend range aggregation is not the primary defect for the reported behavior.
- Dashboard trend UI still used one selected metric at a time on a single axis, so a recent active bucket visually looked like it was always stuck at the right edge and users could not compare 请求数 and 费用 together.
- Prometheus `/-/ready` and `/-/healthy` returned 200.
- Prometheus `/api/v1/targets` showed the `unirouter` target `backend:8080/metrics` as `up`.
- Prometheus had UniRouter metrics including `unirouter_http_requests_total`, `unirouter_http_request_duration_seconds_*`, `unirouter_upstream_latency_seconds_*`, `unirouter_tokens_processed_total`, and `unirouter_billing_freeze_total`.
- Grafana `/api/health` returned 200, but the provisioned datasource UID was generated (`PBFA97CFB590B2093`) while the dashboard JSON references datasource UID `prometheus`.
- Grafana accepted the default `admin/admin` login and showed the password-change prompt. This is separate from the UniRouter seeded admin remediation and must be rotated in deployment.

Local fixes prepared:

- Dashboard AI activity trend now uses checkbox controls and can show 请求数 and 费用 simultaneously with separate Y axes.
- Codex local Playwright visual smoke generated `output/playwright/prd1-dashboard-trend-dual-metric.png` using mocked Dashboard data; it verified 2 trend checkboxes and rendered chart SVGs without obvious overlap.
- Grafana datasource provisioning now pins UID `prometheus`.
- Grafana dashboard PromQL now aggregates histogram buckets correctly and uses `$__rate_interval`.
- Docker Compose now reads `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD` from `.env` instead of hardcoding `admin/admin`.

Acceptance still needed:

- Canary should verify source/config/tests from `docs/agent-workflows/FIX-SIGNAL.md`.
- Atlas should redeploy the fixed Grafana provisioning, set a strong `GRAFANA_ADMIN_PASSWORD`, restart Grafana, and confirm the existing persisted Grafana admin password is rotated.
- Browser-capable validation should capture or manually record that UniRouter Overview panels resolve the Prometheus datasource and display request-rate/latency/error-rate data.

## Alibaba Cloud UI/API Follow-up Verification

Environment:

- Frontend / nginx base: `http://8.130.18.85:3000`
- Backend through nginx: `http://8.130.18.85:3000/api/v1`
- Verified: 2026-07-01 after deployment

Results:

- Canary protocol verification: `docs/agent-workflows/VERIFY-RESULT.md` reports 3/3 PASS for channel-key edit, Token reconciliation pagination, and Dashboard trend fixes.
- Cloud smoke: `RUN_ADMIN_READ_SMOKE=1 scripts/prd1-cloud-smoke.sh` PASS after deployment.
- Token reconciliation pagination: `GET /api/v1/reconciliation?page=1&page_size=1/5/20/50` returned `total=145` and `data_len=1/5/20/50`, so the former one-row page bug is resolved in cloud.
- Dashboard trend range semantics: `GET /api/v1/dashboard/stats` for 24h, 7d, and 30d returned range-scoped `total_requests` values equal to the sum of trend buckets; zero buckets were present.
- Channel key edit UI: Playwright opened the Mimo Token Plan edit drawer, verified the channel selector is clickable and expands with channel options, and verified the API key field is empty with the new-key prompt. Codex did not submit a real provider key.

Conclusion:

- `PRD1-UI-006`, `PRD1-API-001`, and `PRD1-DASH-001` are closed as deployed code bugs.
- The actual Mimo provider credential was later corrected by the user and rechecked; see "Alibaba Cloud Mimo Provider Recheck" below.

## Alibaba Cloud Provider E2E Attempt

Environment:

- Frontend / nginx base: `http://8.130.18.85:3000`
- Admin user: `codex-test`
- Test model requested: `mimo-v2.5`
- Verified: 2026-07-01, rerun after gateway `/v1` fix deployment

Results:

- Admin login: PASS, returned JWT.
- `RUN_ADMIN_READ_SMOKE=1 scripts/prd1-cloud-smoke.sh`: PASS.
- `GET /v1/models` with the dedicated UniRouter test API key: PASS, returned 200 with 3 models including `mimo-v2.5`.
- `POST /v1/chat/completions` with model `mimo-v2.5`: FAIL, returned 502 with `All upstream channels failed (last status: 401)`.
- `POST /v1/chat/completions` with `stream=true`: FAIL, returned 503 `No available channel for model: mimo-v2.5` after the 401 path put the invalid channel key on cooldown.
- Admin channel test for channel 8 `Mimo Token Plan` / `mimo-v2.5`: PASS for corrected semantics, returned `success=false`, `status_code=401`, `error=upstream returned HTTP 401`, and upstream body `Invalid API Key`.
- Channel test curl example no longer contains `/v1/v1/`, confirming the gateway/admin path normalization fix is deployed.
- `POST /v1/chat/completions` with model `moonshot-v1-8k`: PASS, returned 200, response content `pong`, usage `prompt_tokens=12`, `completion_tokens=2`, `total_tokens=14`, `_uniroute.cost.charged=1`, provider/channel `kimi`.
- `POST /v1/chat/completions` with `stream=true` and model `moonshot-v1-8k`: PASS after stream-fix redeploy. Returned 200 with `pong`, usage, one terminal `[DONE]`, and a final data chunk containing `_uniroute` before `[DONE]`.
- Wallet evidence: failed Mimo calls cost `0`; successful Moonshot non-stream and stream calls each cost `1` cent.
- Latest call logs include failed `mimo-v2.5` rows with `status_code=401` and `status_code=503`, both with `cost_cents=0`, and successful `moonshot-v1-8k` rows with `status_code=200`, `cost_cents=1`, including `is_stream=true` and `usage_source=api`.

Conclusion:

- Cloud channel `Mimo Token Plan` has `base_url = https://token-plan-cn.xiaomimimo.com/v1`.
- The previous `/v1/v1/chat/completions` 404 blocker is resolved in cloud.
- Current blocker is provider credential/configuration: the active channel key for channel 8 is rejected by upstream as `Invalid API Key`.
- The subsequent stream 503 is explained by the router excluding keys with `cooldown_until > NOW()` after a 401/403 auth failure.
- The real-provider happy path works through `moonshot-v1-8k` on the `kimi` channel.
- The stream metadata ordering fix is deployed and accepted: the relay suppresses upstream `[DONE]`, emits a final chunk containing both `usage` and `_uniroute`, then emits one terminal `[DONE]`.

Post stream-fix deployment verification:

- Verified: 2026-07-01 01:04 UTC.
- Non-stream `moonshot-v1-8k`: PASS, HTTP 200, content `pong`, usage `12/2/14`, `_uniroute.cost.charged=1`.
- Stream `moonshot-v1-8k`: PASS, HTTP 200, `STREAM_DONE_COUNT=1`, `STREAM_UNIROUTE_COUNT=1`, `STREAM_USAGE_COUNT=3`, `STREAM_UNIROUTE_BEFORE_DONE=True`.
- Wallet moved from `balance_cents=291`, `total_spent_cents=141` to `balance_cents=289`, `total_spent_cents=143`, matching two successful one-cent calls.

Safety decision:

- Codex did not run load against the invalid Mimo route. Load testing used the successful `moonshot-v1-8k` route.

## Alibaba Cloud Mimo Provider Recheck

Environment:

- Frontend / nginx base: `http://8.130.18.85:3000`
- Admin API through nginx: `http://8.130.18.85:3000/api/v1`
- Model: `mimo-v2.5`
- Verified: 2026-07-01 after user updated the `Mimo Token Plan` key

Results:

- Channel key state: channel 8 key prefix changed to `****df24`; `fail_count=0`; `cooldown_until=null`.
- Admin channel test: `POST /api/v1/channels/8/test` with `model=mimo-v2.5` returned `success=true`, HTTP 200, `latency_ms=1385`, `tokens_used=267`.
- Gateway E2E: Codex created temporary API key id `30` with `allowed_models=mimo-v2.5`, called `POST /v1/chat/completions`, received HTTP 200, `usage.total_tokens=259`, `_uniroute.channel=Mimo Token Plan`, and `_uniroute.cost.charged=1`.
- Cleanup: temporary API key id `30` was disabled; disable API returned HTTP 200.
- Latest Mimo call log for request `req_18be0749be049809` records `status_code=200`, `cost_cents=1`, and `usage_source=api`. Its `latency_ms=0` reflects the pre-fix logging bug now tracked as `PRD1-DASH-002`.

Conclusion:

- `PRD1-PROVIDER-001` is closed as a provider-key blocker.
- `PRD1-LOAD-001` remains open because single-call provider success does not prove 20 VU / 60s capacity.

## Alibaba Cloud 20 VU / 60s Load Attempt

Environment:

- Frontend / nginx base: `http://8.130.18.85:3000`
- Model: `moonshot-v1-8k`
- Tool: `k6 v2.1.0`
- Script: `infra/loadtest/k6-prd1-acceptance.js`
- Budget guard: max tokens `8`, expected cost `1` cent per successful request

Run 1:

- Shape: 20 VU / 60s, `SLEEP_SECONDS=12`, no initial stagger.
- Result: FAIL. `requests=100`, `successes=3`, `error_rate=0.97`, `charged_cents=3`, `tokens_total=42`, `p95_ms=336.17`.
- Failure evidence: backend metrics showed `/v1/chat/completions` status counts including `429`, `503`, `502`, and `402`; wallet spent only 3 cents.
- Cause: first burst exceeded the API key/provider capacity and put the channel key into cooldown.

Run 2:

- Shape: 20 VU / 60s, `STAGGER_SECONDS=20`, `SLEEP_SECONDS=25`.
- Result: FAIL. `requests=50`, `successes=20`, `error_rate=0.60`, `charged_cents=20`, `tokens_total=280`, `p95_ms=640.49`.
- Wallet moved from `balance_cents=311`, `total_spent_cents=121` to `balance_cents=291`, `total_spent_cents=141`, matching the 20 successful one-cent calls.
- Failure evidence: Kimi channel key entered cooldown until `2026-07-01T00:52:45.7264Z`; later call-log rows were `503` with `usage_source=no_route` and `cost_cents=0`.

Conclusion:

- The current deployment is safe from uncontrolled spend under this load, but it does not meet a "20 VU / 60s mostly successful requests" acceptance bar.
- This is primarily a capacity/configuration blocker: the available `kimi` channel key appears to rate-limit quickly, and the current router returns no route while the only usable key is cooling down.
- Capacity config after stream-fix redeploy remained unchanged: the dedicated API key has `rpm_limit=60`, `qps_limit=10`, and the `kimi` channel has one active key. Codex did not rerun 20 VU / 60s after redeploy because the capacity prerequisites had not changed.

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

Cloud follow-up:

- OpenClaw Canary verified the relevant code/tests/docs in the protocol flow.
- Sentinel/Atlas redeployed the patched build.
- Cloud smoke on 2026-07-01 showed `admin/admin123456` returns 403.

### PRD1-OPS-001: `/health/ready` PRD response shape

Completed locally:

- `/health/ready` now returns `{"status":"ok","db":"ok","redis":"ok"}` when PostgreSQL and Redis are healthy.

Cloud follow-up:

- OpenClaw Canary verified the relevant code/tests/docs in the protocol flow.
- Sentinel/Atlas redeployed the patched build.
- Cloud smoke on 2026-07-01 showed `{"status":"ok","db":"ok","redis":"ok"}`.

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
| EXT-009 | Default admin remediation redeploy | Sentinel/Atlas | Completed on 2026-07-01 | Patched deployment plus `scripts/prd1-cloud-smoke.sh` showing default login rejected with 403 |

## Recommended Handoff

1. Canary should verify the code/docs in this pack without modifying files.
2. If Canary passes, Sentinel can include local test/build evidence in the PR.
3. Sentinel should run CI and, if available, Docker Compose validation.
4. Atlas should run cloud deployment and complete EXT-001, EXT-002, EXT-007.
5. Browser/manual UI validation remains separate from Canary.

For exact Codex-vs-user testing ownership and required credentials/safety scope, use `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md`.
