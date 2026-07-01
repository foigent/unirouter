# Phase 1 PRD Acceptance Matrix

> Purpose: track acceptance against `docs/PRD-Phase1.md`.
> Status values: `Not started`, `Implemented not accepted`, `Unit tested`, `E2E accepted`, `Production verified`, `Out of scope`.
> Last verified: 2026-07-01 local workspace plus Alibaba Cloud S0/S1 smoke, real-provider E2E, 20 VU load attempts, UI/API follow-up deployment verification, and Mimo provider recheck.

## Core Flow

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Change base URL and call DeepSeek/Qwen/GLM/GPT/Claude/Gemini | Implemented not accepted | `moonshot-v1-8k` real provider call through the `kimi` channel returned 200; after key update, channel 8 `mimo-v2.5` returned HTTP 200 through both channel test and UniRouter gateway | Wider provider matrix still needed if PRD1 requires every named provider family |
| Create API key, call API, return response and `_uniroute.cost` | E2E accepted | Existing dedicated test API key successfully called `moonshot-v1-8k`; a temporary API key id 30 called `mimo-v2.5` through UniRouter and returned `_uniroute.channel=Mimo Token Plan`, `_uniroute.cost.charged=1`, then was disabled | Keep create-key-to-call in release smoke; avoid leaving active temporary keys |
| Top up, call, deduct balance, reconcile | Implemented not accepted | Wallet decreased by 1 cent for non-stream and 1 cent for stream single calls after stream-fix redeploy; load spend matched successful calls only | Current top-up/reconciliation mutation evidence still needed |
| Insufficient balance returns 402 | Unit tested | Billing and relay tests pass | Current E2E evidence needed |
| Disabled key returns 401 | Unit tested | Auth/key tests pass | Current E2E evidence needed |
| Streaming request returns fully and settles correctly | E2E accepted | After stream-fix redeploy, cloud stream for `moonshot-v1-8k` returned 200, emitted `usage` + `_uniroute` before a single `[DONE]`, logged `is_stream=true`, and settled 1 cent | Interrupted-stream settlement remains separate |
| Interrupted streaming settles by received/estimated tokens | Implemented not accepted | Stream accounting code exists | Controlled disconnect acceptance needed |

## Admin Capability

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Admin can create/edit/disable channels | Implemented not accepted | Admin routes and frontend page exist | Current manual/API CRUD evidence needed |
| Admin can create/disable API keys | Implemented not accepted | Routes and frontend page exist | Current manual/API CRUD evidence needed |
| Admin can view billing and call logs | E2E accepted | Non-destructive cloud API smoke verified authenticated GETs for dashboard, wallet, wallet transactions, logs, reconciliation, users, grants, audit logs, settings, sensitive words, model prices, org prices, keys, channel keys, and channels; deployment follow-up verified reconciliation `page_size=20` returns 20 rows and Dashboard trend sums match range-scoped totals; latency persistence is fixed locally and awaiting deploy | Browser UX walkthrough still needed |
| Admin can top up and adjust wallet | Implemented not accepted | Routes and frontend page exist; wallet admin action visibility and adjust request contract fixed locally | Current manual/API mutation evidence needed in safe test conditions |
| Docker Compose one-command deployment succeeds | Production verified | Alibaba Cloud smoke passed: frontend `:3000`, backend health `:8080`, Prometheus `:9090`, Grafana login `:3001`, admin login/me/dashboard/settings through nginx | Keep Sentinel/Atlas deployment logs as release evidence |
| Seeded default admin password is not usable in public deployment | Production verified | `scripts/prd1-cloud-smoke.sh` passed `default-admin-password-rejected`; direct cloud login with `admin/admin123456` returned 403 on 2026-07-01 | None |

## Frontend UI

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Admin frontend has a coherent navigation and layout shell | Implemented not accepted | `frontend/src/App.tsx` defines grouped sidebar, protected layout, `/docs` nav entry, and refreshed console shell; Codex Playwright screenshots captured desktop and mobile layouts; Canary non-visual verification passed | User/cloud visual acceptance still needed |
| Admin pages use consistent table/form/modal patterns | Implemented not accepted | PRD1 UI layer added under `frontend/src/ui/`; major resource pages now use shared `PageHeader`/`Surface` and table shell patterns; Canary non-visual verification passed | Deeper typed resource-page abstraction can move to PRD2 after user accepts PRD1 UI |
| Core admin workflows are usable from browser | Implemented not accepted | Frontend build/tests pass; Codex screenshot pass covered Login, Dashboard, Logs detail, Wallet, API Docs, Channels, mobile Dashboard, and mobile Logs with mocked read-only data; cloud UI follow-up verified the Mimo Token Plan channel-key edit drawer can expand the channel selector and accept a new key input | Human/cloud browser walkthrough still needed before strict close |
| UI has consistent visual hierarchy, spacing, empty/error/loading states | Implemented not accepted | `docs/design/PRD1-UI-REDESIGN-SPEC.md` records the implemented visual system; Dashboard chart/health, Logs explorer, Wallet actions, API Docs, and mobile hamburger overlap were visually checked by Codex; Canary non-visual verification passed | Canary cannot judge visual quality; user acceptance remains the final visual gate |
| Frontend API client matches backend routes | Unit tested | `ApiError` added; `/me/password` backend route added; `route_parity_test.go` guards frontend admin routes; Canary verification passed for route parity round | Keep route parity checks in future frontend/API changes |

## Pricing

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Model price configuration preserves small CNY prices | Fixed awaiting Canary | Local fix migrates `model_prices` price columns to `NUMERIC(18,6)`, updates admin/billing/frontend handling to decimal cents, and adds `0.002 yuan -> 0.2 cents` tests | Deploy, re-save `mimo-v2.5` price as `0.002/0.001`, then verify API and UI show non-zero values |

## Stability

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| 100 concurrent requests do not create negative wallet balance | Implemented not accepted | 20 VU / 60s k6 attempts did not create uncontrolled spend; wallet spend matched successful calls only | Capacity acceptance failed under current provider key configuration; add provider capacity/keys or defer capacity target |
| Circuit breaker switches to backup channel | Implemented not accepted | Circuit breaker and relay tests pass | Current failover simulation needed |
| Sensitive-word request returns 400 | Implemented not accepted | DFA filter and routes exist | Current gateway E2E evidence needed |
| Same `X-Request-Id` does not double charge | Implemented not accepted | Idempotency table and billing paths exist | Current repeated-request E2E evidence needed |
| Admission over concurrency limit returns 429 | Unit tested | Admission middleware tests pass; load attempts produced controlled 429/503/502/402 rather than runaway spend | Need targeted admission-only test separate from provider cooldown behavior |

## Observability

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Each request log includes request_id/org_id/channel_id | Implemented not accepted | Cloud call logs recorded Mimo 401/503 failures and Moonshot 200 successes with request IDs and costs; list response currently shows channel fields as names/nullable values | Verify channel_id/org_id visibility if PRD requires them in admin list response |
| Prometheus `/metrics` returns metrics | Production verified | Local current-source `/metrics` returned metrics; Alibaba Cloud backend `:8080/metrics` returned Prometheus text metrics; Prometheus `:9090/-/healthy` returned 200 | Keep Atlas deployment logs as release evidence |
| `/health/ready` returns PRD shape with DB/Redis status | Production verified | Cloud `GET http://8.130.18.85:8080/health/ready` returned `{"status":"ok","db":"ok","redis":"ok"}` on 2026-07-01; `scripts/prd1-cloud-smoke.sh` passed `backend-ready-prd-shape` | None |
| Grafana dashboard shows request rate, latency, error rate | Implemented not accepted | Grafana `:3001/login` is reachable on Alibaba Cloud | Current running Grafana panel screenshot/manual evidence needed |

## Acceptance Rule

An item should not move to `E2E accepted` unless the evidence includes:

1. Exact command or manual path.
2. Environment used.
3. Expected result.
4. Actual result.
5. Timestamp or commit/worktree reference.
