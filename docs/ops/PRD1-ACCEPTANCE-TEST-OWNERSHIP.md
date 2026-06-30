# PRD1 Acceptance Test Ownership

Status: active
Owner: Codex for execution where safe credentials and test scope exist
Last updated: 2026-07-01

## Purpose

This file turns the remaining PRD1 acceptance gaps into an executable testing ownership plan. The user does not need to manually test flows that Codex can safely automate, but Codex must not mutate production data, spend provider budget, or run disruptive load/fault scenarios without explicit scope.

## What Codex Can Test Without Extra Input

These checks are safe because they do not require privileged credentials and do not mutate business data.

| Area | Codex can execute | Evidence produced |
|------|-------------------|-------------------|
| S0 cloud availability | frontend root, backend live/ready, metrics, docs, Prometheus healthy, Grafana login page | curl command, status, response snippet |
| S0/S1 negative auth | gateway missing-key 401, admin missing-token 401, public default admin rejection | curl command, status, response snippet |
| Local source verification | `go test ./...`, frontend tests, frontend build, route parity guard | command output summary |
| Static PRD/source reconciliation | route table, OpenAPI path scan, doc status consistency | file/line evidence |
| Unauthenticated browser smoke | cloud login page and public docs route visual screenshot | Playwright screenshots |

Already completed after redeploy:

- `scripts/prd1-cloud-smoke.sh`: PASS.
- Cloud `/health/ready`: `{"status":"ok","db":"ok","redis":"ok"}`.
- Cloud `admin/admin123456`: 403.

Refreshed by Codex on 2026-07-01 without extra credentials:

- `scripts/prd1-cloud-smoke.sh`: PASS.
- `go test ./...`: PASS.
- `cd frontend && npm test -- --run`: PASS, 36/36 tests.
- `cd frontend && npm run build`: PASS, with existing Vite large chunk warning.
- `go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v`: PASS.
- `GET http://8.130.18.85:3000/docs`: PASS, frontend SPA shell returned.
- `GET http://8.130.18.85:8080/docs`: PASS, backend `UniRouter API Documentation` returned.

## What Codex Can Test If Given A Safe Admin Credential Or Token

These are low-impact authenticated read checks. Admin login creates audit-log side effects, but does not mutate business entities.

Required input:

- `ADMIN_TOKEN`, or
- `ADMIN_USERNAME` and `ADMIN_PASSWORD` for a safe cloud admin account.

How to obtain `ADMIN_TOKEN`:

1. Browser method:
   - Log in to the cloud admin console.
   - Open browser DevTools Console.
   - Run `localStorage.getItem('token')`.
   - Copy the returned JWT into `/tmp/uniroute-prd1-acceptance.env` as `ADMIN_TOKEN=...`.
2. CLI method:
   - Fill `ADMIN_USERNAME` and `ADMIN_PASSWORD` in `/tmp/uniroute-prd1-acceptance.env`.
   - Codex can run the login request and extract `.token` with `jq`.

Codex can then run:

```bash
RUN_ADMIN_READ_SMOKE=1 ADMIN_TOKEN='<jwt>' scripts/prd1-cloud-smoke.sh
```

Evidence produced:

- Dashboard stats read.
- Channels list read.
- API keys list read.
- Logs list read.
- Wallet read.
- Audit logs read.
- Settings read.
- Browser walkthrough screenshots for Dashboard, Channels, Keys, Wallet, Logs, Settings, Docs.

## What Codex Can Test If Given A Dedicated Test Scope

These tests mutate data and should only run against a dedicated test org/account/channel/API key that may be created and disabled during the test.

Required input:

- Safe admin token or credential.
- Permission to create and disable test channels and test API keys.
- Confirmation that wallet top-up/adjustment may be performed in the test org/account.

Codex can then execute:

| PRD area | Test Codex can run | Why a dedicated scope is required |
|----------|--------------------|-----------------------------------|
| Admin channel CRUD | create/edit/disable a test channel | Mutates channel config |
| Admin API key CRUD | create/disable a test API key | Creates credentials |
| Wallet management | top up/adjust test wallet, verify transaction rows | Mutates balance |
| Sensitive words | add a unique test word, verify 400 rejection, remove it | Mutates global or org filter config |
| Audit logs | verify the above writes create audit rows | Writes audit records |

## What Codex Can Test If Provider Budget Is Approved

These tests call real upstream providers through UniRouter and can spend money.

Required input:

- A UniRouter API key for a test org.
- A configured test provider/channel/model set.
- Budget ceiling, recommended `<= ¥5` for PRD1 smoke.
- Permission to create call-log and wallet-transaction rows.

Codex can then execute:

| PRD area | Test Codex can run | Evidence produced |
|----------|--------------------|-------------------|
| Non-stream gateway | `POST /v1/chat/completions` with a cheap model | 200 response, `_uniroute.cost`, call log row |
| Streaming gateway | stream request with `[DONE]` | SSE chunks, settlement evidence, `is_stream=true` log |
| Insufficient balance | lower test wallet then call | 402 response and unchanged settlement |
| Disabled key | disable test key then call | 401 response |
| Idempotency | repeat same `X-Request-Id` | no double charge evidence |
| Request logging | inspect logs after calls | request_id/org_id/channel_id fields |

## What Codex Can Test Only In A Safe Test Window

These tests are disruptive or intentionally abnormal. They should not run on shared production traffic unless a test window and rollback plan exist.

Required input:

- Explicit test window.
- Dedicated test org/API key/channel.
- Acceptable error-rate/latency impact.
- Permission to run load or fault simulation.

Codex can then execute:

| PRD area | Test Codex can run | Risk |
|----------|--------------------|------|
| 100 concurrent billing requests | k6/wrk or scripted concurrent calls | Provider spend and service load |
| Interrupted streaming settlement | open stream and disconnect client | partial billing/log side effects |
| Failover/circuit breaker | point primary test channel to failing upstream and verify backup | intentional upstream failures |
| Admission limit 429 | concurrent request burst | temporary 429/error spike |

## Recommended Closure Path

1. Codex finishes all S0/S1 non-mutating evidence.
2. User provides a safe admin credential or token; Codex runs admin read smoke and browser walkthrough.
3. User authorizes a dedicated test org/account; Codex runs admin CRUD, wallet, sensitive-word, and audit acceptance.
4. User authorizes a small provider budget; Codex runs non-stream/stream gateway, billing, log, disabled-key, insufficient-balance, and idempotency acceptance.
5. User authorizes a short test window; Codex runs load, interrupted stream, and failover/circuit acceptance.
6. If steps 3-5 are intentionally deferred to PRD2 or ops hardening, record that as explicit user-approved deferral in `PRD-ACCEPTANCE-MATRIX.md`.
