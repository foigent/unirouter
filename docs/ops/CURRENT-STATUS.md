# UniRouter Current Status

> Purpose: single current-state entrypoint for humans, Codex, and OpenClaw Canary.
> Last verified: 2026-07-02 PRD2 Batch 14 API key hash secret and deployment env hardening fixed locally by Codex; full local verification passed; awaiting Canary verification.

## Summary

Per User direction in the PRD2 planning session, PRD1 is treated as closed/frozen for forward planning. Historical PRD1 closeout evidence remains in this file and related PRD1 ops documents, but new product planning should use the PRD2 documents listed below. PRD2 has formal requirements, an acceptance matrix, and initial Provider Adapter, Project/RBAC, and Developer Console specs. OpenClaw Canary verified PRD2 planning docs round 1 as 6/6 PASS with no new issues, and User reported Sentinel confirmed PRD2 planning round 1 complete with CI green on the first run. PRD2 implementation batch 1 Project/Workspace backend foundation passed Canary 5/5, and User reported Sentinel completed. PRD2 implementation batch 2 RBAC backend route gates and project-scope checks passed Canary 5/5, and User reported Sentinel completed. PRD2 implementation batch 3 Model Governance backend foundation passed Canary 6/6, and User reported Sentinel completed. PRD2 implementation batch 4 Provider Adapter backend foundation passed Canary 6/6, and User reported Sentinel completed. PRD2 implementation batch 5 Token Metering V2 backend foundation passed Canary 6/6, and User reported Sentinel completed. PRD2 implementation batch 6 Audit Schema Upgrade backend foundation passed Canary 6/6, and User reported Sentinel completed. PRD2 implementation batch 7 Notification/Event backend foundation passed Canary 7/7, and User reported Sentinel completed. PRD2 implementation batch 8 HA Readiness backend/ops foundation passed Canary 7/7, and User reported Sentinel completed. PRD2 implementation batch 9 P0 E2E Acceptance Consolidation passed Canary 8/8, and User reported Sentinel completed. Canary also completed a PRD2 batch 1-9 full-context dynamic regression sweep: `go test ./...`, PRD2 integration tests, billing concurrency integration, frontend tests/build, and `git diff --check` all passed. No High/Critical findings or Batch 10 blockers were found. PRD2 Batch 9.5 hardening passed Canary 4/4 and User reported Sentinel completed; `PRD2-ERR-001` is now Canary passed. PRD2 Batch 10 Developer Console MVP passed Canary 5/5 and User reported Sentinel completed. PRD2 Batch 11 Model Governance Admin UI passed Canary 4/4 and User reported Sentinel completed. PRD2 Batch 12 Anthropic Native Adapter Proof passed Canary 5/5, Sentinel merged, and User reported Atlas completed. PRD2 Batch 13 Anthropic Native Stream Translation passed Canary/Sentinel/Atlas per User report and PR #44 was deployed; User reported the Anthropic stream feature is online. During PR #44 deployment, `rsync --delete` removed the production `.env`; `ENCRYPTION_KEY` was recovered so upstream channel keys remained usable, but the original `JWT_SECRET` could not be recovered, causing admin re-login and invalidating 31 API Keys because current code hashes API Keys with `JWT_SECRET`. PRD2 Batch 14 is fixed locally and full local verification passed; it is awaiting Canary. Batch 14 introduces `API_KEY_HASH_SECRET` with backward-compatible fallback to `JWT_SECRET`, wires Admin API key creation and Gateway API key verification to the new secret, updates compose/env docs, and adds deployment runbook protections for `.env`/`.env.production`. Real Anthropic upstream E2E remains pending configured provider credentials. Remaining Low hardening follow-ups stay tracked in `docs/issues/OPEN-ISSUES.md`.

Historical PRD1 summary: Core backend modules, admin APIs, frontend pages, migrations, Docker deployment files, and monitoring assets exist. The default-admin cloud security blocker and the channel-key/reconciliation/Dashboard follow-up fixes were redeployed and verified in cloud. The Mimo provider key was cloud-verified as valid; latency persistence and decimal pricing were production-verified after redeploy. Dashboard trend UX passed Canary and local visual smoke. User/Atlas reported the Grafana runtime fix resolved, and Codex verified Grafana default `admin/admin` is rejected. Some PRD1 status rows below still preserve historical follow-up language; they should not be interpreted as PRD2 scope unless explicitly converted into `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`.

## Phase Status

| Phase | Status | Active Documents |
|-------|--------|------------------|
| PRD1 | Closed/frozen for forward planning per User direction | `docs/PRD-Phase1.md`, `docs/ops/PRD-ACCEPTANCE-MATRIX.md`, PRD1 ops evidence docs |
| PRD2 | Batch 14 API key hash secret and deployment env hardening fixed locally; full local verification passed; awaiting Canary | `docs/planning/PRD2-DISCOVERY.md`, `docs/PRD-Phase2.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/agent-workflows/FIX-SIGNAL.md`, `docs/issues/OPEN-ISSUES.md`, `docs/ops/deployment-runbook.md` |

## Current Verification

| Area | Status | Evidence |
|------|--------|----------|
| PRD2 Batch 14 API key hash secret and deployment env hardening | Fixed locally; full local verification passed; awaiting Canary | Codex added `API_KEY_HASH_SECRET` config with fallback to `JWT_SECRET`, wired `admin.NewHandlerWithAPIKeyHashSecret` and `middleware.GatewayAuthWithAPIKeyHashSecret`, updated `docker-compose.yml`, `docker-compose.canary.yml`, `.env.example`, `README.md`, `deployment-runbook.md`, `canary-testing.md`, `OPEN-ISSUES.md`, `PRD2-ACCEPTANCE-MATRIX.md`, `CODEBASE-MAP.md`, `FIX-SIGNAL.md`, and this status file. Local verification passed: targeted Go tests, `go test ./... -count=1`, `npm --prefix frontend test`, `npm --prefix frontend run build`, docs grep, `git diff --check`, and untracked diff check |
| PRD2 Batch 13 Anthropic Native Stream Translation | Canary/Sentinel/Atlas complete per User report; PR #44 deployed with env incident | User reported Canary, Sentinel, and Atlas completed and PR #44 deployed; new Anthropic stream conversion is online. Deployment incident: `rsync --delete` deleted production `.env`; `ENCRYPTION_KEY` was recovered, preserving upstream channel keys, but `JWT_SECRET` was regenerated because the old value was unrecoverable, invalidating 31 API Keys and requiring admin re-login. Local `.env.production` backup now exists per User report; Batch 14 adds code/docs hardening for this failure mode |
| PRD2 Batch 12 Anthropic Native Adapter Proof | Canary passed; Sentinel complete; Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS for `anthropic_native` non-stream request/response conversion, adapter-driven failure classification, admin channel create/test support, admin UI adapter configuration, and docs. Latest `main` includes Sentinel merge commit `24e0abf` (`feat: PRD2 Anthropic native adapter — request/response conversion, relay normalization, and admin UI (#43)`). User reported Sentinel and Atlas completed. Browser smoke was skipped by Canary; real Anthropic upstream E2E remains pending configured provider credentials |
| PRD2 Batch 11 Model Governance Admin UI | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 4/4 PASS: org-scoped model-set item read endpoint, frontend API/navigation/routing, admin Model Governance page, and status docs. Commands passed: targeted backend route tests, `go test ./internal/admin ./cmd/uniroute`, `go test ./... -count=1`, `npm --prefix frontend test -- ModelGovernancePage client navigation`, `npm --prefix frontend test`, `npm --prefix frontend run build`, `git diff --check`, and untracked diff check. User reported Sentinel completed. Browser smoke was skipped by Canary and remains a later environment/manual gap |
| PRD2 Batch 10 Developer Console MVP | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS: project-filtered key listing, auth profile refresh, role-aware routing, Developer Console page, API client contracts, and status docs. Commands passed: targeted `internal/admin` tests, `go test ./internal/admin ./cmd/uniroute`, `go test ./... -count=1`, `npm --prefix frontend test`, `npm --prefix frontend run build`, and `git diff --check`. User reported Sentinel completed. Codex Playwright mock-browser smoke also passed for `/login`, unauthenticated `/developer` redirect, and mocked `/developer` desktop/mobile rendering; screenshots are local ignored artifacts under `output/playwright/` |
| PRD2 Batch 9.5 hardening | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 4/4 PASS and all validation commands PASS. `PRD2-ERR-001` can be marked Canary passed; settle retry, cancel-on-failure, and stale frozen cleanup are verified. User reported Sentinel completed |
| PRD2 batch 1-9 full-context dynamic regression sweep | PASS; no Batch 10 blocker | `docs/agent-workflows/VERIFY-RESULT.md` reports `go test ./... -count=1`, PRD2 migration/gateway/billing integration tests, frontend `npm test -- --run`, frontend build, and `git diff --check` all PASS; `docs/issues/PRD2-REGRESSION-SWEEP-2026-07-02.md` reports 12/12 manual mutations covered and one Medium hardening issue `PRD2-ERR-001` |
| PRD2 implementation batch 9 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 8/8 PASS for P0 E2E Acceptance Consolidation, including migrated DB E2E, developer project-key gateway E2E, model governance denial E2E, and route-level RBAC negative E2E; User reported Sentinel completed |
| PRD2 batch 1-9 deep code review | No High/Critical findings; Low follow-ups tracked | `docs/issues/PRD2-DEEP-CODE-REVIEW-2026-07-02.md` reports 7-area review with 4 Low findings: audit historical project backfill, 000012 down migration source rollback, unregistered model visibility, and Channel CRUD audit coverage |
| PRD2 implementation batch 8 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 7/7 PASS for HA Readiness backend/ops foundation, global Go tests PASS, HA integration PASS with nonfatal truncate warnings, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 7 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 7/7 PASS for Notification/Event backend foundation, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 6 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS for Audit Schema Upgrade, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 5 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 4 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 3 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 2 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 1 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 planning docs round 1 | Canary passed; Sentinel CI green | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, no new issues, no code changes; User reported Sentinel confirmed PRD2 planning documentation round 1 complete and CI passed on the first run |
| Go tests | PASS | `go test ./...` passed on 2026-07-01 |
| Frontend tests | PASS | `npm test -- --run` passed in `frontend/` with 43 tests on 2026-07-01; Canary verified earlier rounds as PASS |
| Frontend build | PASS with warning | `npm run build` passed; Canary verified PASS; Vite reported large chunk warning |
| Route parity guard | PASS | `go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v` passed on 2026-07-01 |
| Local current-source smoke | PASS | Temporary `go run ./cmd/uniroute` on port 18080 passed health, metrics, login, `/me`, settings, dashboard, and unauthenticated password-route checks |
| PRD1 latest Canary | PASS | `VERIFY-RESULT.md` reports 4/4 PASS for the latest PRD1 reference-inspired UI redesign; earlier closeout rounds also passed through the protocol, while external-only items remain in `PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| PRD1 latest follow-up fixes | Canary passed and production verified | `VERIFY-RESULT.md` reports 3/3 PASS for channel-key edit, Token reconciliation pagination, and Dashboard trend fixes; cloud follow-up verified the channel selector expands, reconciliation `page_size=20` returns 20 rows, and 24h/7d/30d Dashboard totals match trend sums |
| PRD1 latency/decimal pricing fixes | Production verified | After redeploy, temporary request `codex-latency-smoke-1782874625` logged `latency_ms=1290`; Dashboard 24h average became `11.4159ms`; re-saving `mimo-v2.5` preserved `input_price_cents=0.2/output_price_cents=0.1` |
| PRD1 Dashboard/Grafana follow-up fixes | Production verified with operator evidence | Dashboard trend UX passed Canary and local Playwright visual smoke. User/Atlas reported Grafana runtime fixed; Codex verified Grafana health, Prometheus query success, and `admin/admin` rejection with HTTP 401 |
| PRD1 Logs/Dashboard UI follow-up fixes | Fixed awaiting Canary | Local source now renders call-log timestamps as absolute time, adds Request ID copy fallback/toast, reorders Provider before Model, improves request detail layout, caps model distribution to Top 6, and clarifies channel health provider labels. Targeted frontend tests, full frontend tests, build, `git diff --check`, and Codex Playwright smoke passed locally |
| PRD1 Dashboard percentile/layout closeout | Fixed awaiting Canary | Local source adds Dashboard API `p95_latency_ms/p99_latency_ms`, changes the main latency KPI to P95 with Avg/P99 context, aligns topbar/content at 1440px, replaces the loose hero with a compact status strip, pairs trend with a latency percentile panel, and adds channel-health status icons/progress. Admin targeted Go tests, Dashboard targeted frontend tests, full frontend tests, and build passed locally |
| Browser/manual frontend validation | Codex visual QA PASS locally | Codex Playwright screenshots captured Login, Dashboard, Logs detail, Wallet, API Docs, Channels, mobile Dashboard, mobile Logs, and the reference-inspired Login/Dashboard/mobile pass under local `output/playwright/`; Canary/OpenClaw visual acceptance remains not applicable |
| Real upstream provider E2E | E2E accepted for Moonshot and Mimo single calls | `moonshot-v1-8k` non-stream and stream calls reached a real provider and settled; after the user updated the key, channel 8 `mimo-v2.5` test returned HTTP 200 and a UniRouter gateway call returned HTTP 200 with `_uniroute.channel=Mimo Token Plan` |
| Production deployment status | PASS for S0/S1 smoke | `scripts/prd1-cloud-smoke.sh` passed against Alibaba Cloud on 2026-07-01: frontend, live/ready, metrics, docs, Prometheus, Grafana login, unauthenticated 401 checks, and default-admin rejection |
| Default admin password protection | Production verified | Cloud `POST /api/v1/auth/login` with `admin/admin123456` returned 403 on 2026-07-01; script check `default-admin-password-rejected` passed |
| `/health/ready` PRD response shape | Production verified | Cloud `GET http://8.130.18.85:8080/health/ready` returned `{"status":"ok","db":"ok","redis":"ok"}` on 2026-07-01 |

## Phase 1 Delivery State

| PRD Area | Current State | Notes |
|----------|---------------|-------|
| F01 OpenAI-compatible gateway | Implemented, cloud-accepted for sampled providers | Non-stream and stream real provider paths work for `moonshot-v1-8k`; after key update, `mimo-v2.5` works through `Mimo Token Plan` for a sampled non-stream call |
| F02 Channel management | Implemented | Admin API and frontend page exist |
| F03 API key management | Implemented | Includes allowed models and disable path |
| F04 Gateway auth middleware | Implemented | Tests pass |
| F05 Pre-freeze billing | Implemented | Needs current E2E balance and concurrency acceptance evidence |
| F06 Call logs | Implemented, latest UI follow-up awaiting Canary | Logs page and admin APIs exist; local follow-up now uses absolute timestamps, Request ID copy feedback/fallback, and a structured detail inspector |
| F07 Admin API | Implemented | Broad handler coverage exists |
| F08 Wallet management | Implemented | Top-up, adjustment, transactions exist |
| F09 Model pricing | Production verified for decimal cents | Base and org pricing exist; cloud API preserved `mimo-v2.5` decimal-cent prices `0.2/0.1` after redeploy |
| F10 Admin frontend | Implemented, latest UI follow-up awaiting Canary | Build, component tests, Canary source verification, and Codex local visual screenshot pass are complete; latest Logs/Dashboard UI fixes and Dashboard percentile/layout closeout passed local tests/build and await Canary; user/cloud browser visual acceptance still gates strict close |
| F11 Rate limit and admission control | Implemented | Needs current concurrency acceptance evidence |
| F12 Failover and circuit breaker | Implemented | Needs current failover/circuit acceptance evidence |
| F13 Sensitive-word filter | Implemented | Needs current E2E request rejection evidence |
| F14 Idempotency | Implemented | Needs current repeated request acceptance evidence |
| F15 Token estimation | Implemented | Tests pass |
| F16 Budget alert | Implemented | Needs current webhook acceptance evidence |
| F17 System settings | Implemented | Settings page/API exist |
| F18 Health checks | Production verified for PRD shape | `/health/live`, `/health/ready`, `/metrics` exist; cloud `/health/ready` returned `{"status":"ok","db":"ok","redis":"ok"}` after redeploy |
| Observability dashboards | Production verified with operator evidence | Prometheus target and UniRouter metrics are present in cloud; Grafana dashboard config pins datasource UID `prometheus` and uses aggregated PromQL; user/Atlas reported the runtime Grafana issue resolved, and Codex verified Grafana health plus default credential rejection |

## Known Gaps

1. PRD acceptance checklist is partially reconciled with current evidence; cloud API smoke plus Moonshot and Mimo real-provider E2E are recorded, while capacity load and failover scenarios still need closure or explicit deferral.
2. Some historical status files disagree about whether specific bugs are open or fixed.
3. `docs/testing/archive/` contains useful history but should not be used as the current status source.
4. Active issues are now centralized in `docs/issues/OPEN-ISSUES.md`; older issue status still needs item-by-item reconciliation.
5. Frontend API/client route contract needs continued explicit verification; this round fixed confirmed `KeysPage`, `WalletPage`, and `/me/password` route drift, added `ApiError`, added a route parity guard, and Canary passed the remaining closeout verification.
6. Historical docs mention XOR channel-key encryption, but current code uses AES-GCM helpers in `internal/auth/auth.go`; `docs/ops/test-report.md` now marks the XOR note as stale, and Canary passed the correction.
7. UI quality, frontend architecture, component reuse, and browser/manual validation are explicit PRD1 closeout items, not deferred wholesale to PRD2. Current source audit is recorded in `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; PRD1 UI redesign execution is recorded in `docs/design/PRD1-UI-REDESIGN-SPEC.md`; Codex completed a local Playwright visual screenshot pass and a reference-inspired shell/login/Dashboard pass, while Canary remains responsible only for non-visual source/build/test verification. User/cloud visual acceptance remains the final visual gate.
8. Cloud smoke found API list handlers could hide row scan errors and emit zero-value fields when nullable database columns were scanned into non-nullable Go types. Codex fixed `ListAPIKeys` and `ListLogs`; after deployment, authenticated cloud reads for `/api/v1/keys` and `/api/v1/logs` returned 200 on 2026-07-01.
9. User-reported PRD1 UI findings on logs filters, Dashboard trend/health cards, wallet edit actions, audit login records, `/docs`, and overall visual quality were classified in `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`; concrete bugs and the PRD1 visual redesign passed Canary non-visual verification.
10. Alibaba Cloud strict S0/S1 closeout smoke passed on 2026-07-01 after redeploy. The former default-admin blocker is fixed in cloud, and `/health/ready` now exposes the PRD DB/Redis shape. Remaining PRD1 closure depends on acceptance evidence or explicit deferral for the still-open matrix items.
11. Cloud Grafana runtime follow-up was reported resolved by user/Atlas. Codex verified Prometheus query success, Grafana health, and rejection of Grafana `admin/admin`; authenticated datasource UID inspection now requires the rotated Grafana credential.
12. User-reported Logs/Dashboard UI follow-up is fixed locally and tracked in `docs/issues/OPEN-ISSUES.md` plus `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`: absolute log timestamps, Request ID copy feedback/fallback, log detail inspector, Provider-before-Model filters, and Dashboard model/channel health layout. Canary must verify non-visual source/build/tests before deployment.
13. Latest Dashboard percentile/layout closeout is fixed locally and recorded in `docs/agent-workflows/FIX-SIGNAL.md`: Dashboard API P95/P99 latency, P95 main KPI, aligned 1440px content/topbar, compact status strip, trend/latency split, and channel-health icons/progress. Canary must verify non-visual source/build/tests before deployment; user/browser-capable review remains required for final visual judgment.

## Active Sources Of Truth

| Need | Use This File |
|------|---------------|
| PRD1 product requirements | `docs/PRD-Phase1.md` |
| PRD2 discovery and decisions | `docs/planning/PRD2-DISCOVERY.md` |
| PRD2 product requirements draft | `docs/PRD-Phase2.md` |
| PRD2 acceptance planning | `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
| PRD2 provider adapter spec | `docs/tech-ref/10-provider-adapter-framework.md` |
| PRD2 Project/RBAC spec | `docs/tech-ref/11-project-rbac-model.md` |
| PRD2 Developer Console spec | `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md` |
| Agent handoff | `docs/PROJECT-HANDOFF.md` |
| Codebase map | `docs/CODEBASE-MAP.md` |
| Documentation inventory | `docs/DOC-INVENTORY.md` |
| Current project status | `docs/ops/CURRENT-STATUS.md` |
| PRD1 closeout plan | `docs/ops/PRD1-CLOSEOUT-PLAN.md` |
| PRD1 strict closeout gate | `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md` |
| PRD1 acceptance test ownership | `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md` |
| PRD1 UI redesign execution | `docs/design/PRD1-UI-REDESIGN-SPEC.md` |
| Remaining PRD1 acceptance pack | `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| PRD acceptance progress | `docs/ops/PRD-ACCEPTANCE-MATRIX.md` |
| Active unresolved issues | `docs/issues/OPEN-ISSUES.md` |
| Agent collaboration protocol | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Current Codex to Canary verification request | `docs/agent-workflows/FIX-SIGNAL.md` |
| Current Canary verification result | `docs/agent-workflows/VERIFY-RESULT.md` |
