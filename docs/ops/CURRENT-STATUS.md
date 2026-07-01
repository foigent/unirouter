# UniRouter Current Status

> Purpose: single current-state entrypoint for humans, Codex, and OpenClaw Canary.
> Last verified: 2026-07-01 local workspace.

## Summary

UniRouter Phase 1 is in MVP hardening. Core backend modules, admin APIs, frontend pages, migrations, Docker deployment files, and monitoring assets exist. The project is not an early scaffold, but it is not yet fully PRD-accepted because several end-to-end and production-style acceptance cases still need current evidence, fixes, valid provider capacity, or explicit user-approved deferral. The default-admin cloud security blocker found on 2026-07-01 has been redeployed and verified as fixed.

## Current Verification

| Area | Status | Evidence |
|------|--------|----------|
| Go tests | PASS | `go test ./...` passed on 2026-07-01 |
| Frontend tests | PASS | `npm test -- --run` passed in `frontend/` with 36 tests on 2026-07-01; Canary verified the previous 35-test round as PASS |
| Frontend build | PASS with warning | `npm run build` passed; Canary verified PASS; Vite reported large chunk warning |
| Route parity guard | PASS | `go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v` passed on 2026-07-01 |
| Local current-source smoke | PASS | Temporary `go run ./cmd/uniroute` on port 18080 passed health, metrics, login, `/me`, settings, dashboard, and unauthenticated password-route checks |
| PRD1 latest Canary | PASS | `VERIFY-RESULT.md` reports 4/4 PASS for the latest PRD1 reference-inspired UI redesign; earlier closeout rounds also passed through the protocol, while external-only items remain in `PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| Browser/manual frontend validation | Codex visual QA PASS locally | Codex Playwright screenshots captured Login, Dashboard, Logs detail, Wallet, API Docs, Channels, mobile Dashboard, mobile Logs, and the reference-inspired Login/Dashboard/mobile pass under local `output/playwright/`; Canary/OpenClaw visual acceptance remains not applicable |
| Real upstream provider E2E | Partially accepted | `moonshot-v1-8k` non-stream and stream calls reached a real provider and settled; Mimo channel key is invalid; deployed stream final metadata order still needs the local fix redeployed |
| Production deployment status | PASS for S0/S1 smoke | `scripts/prd1-cloud-smoke.sh` passed against Alibaba Cloud on 2026-07-01: frontend, live/ready, metrics, docs, Prometheus, Grafana login, unauthenticated 401 checks, and default-admin rejection |
| Default admin password protection | Production verified | Cloud `POST /api/v1/auth/login` with `admin/admin123456` returned 403 on 2026-07-01; script check `default-admin-password-rejected` passed |
| `/health/ready` PRD response shape | Production verified | Cloud `GET http://8.130.18.85:8080/health/ready` returned `{"status":"ok","db":"ok","redis":"ok"}` on 2026-07-01 |

## Phase 1 Delivery State

| PRD Area | Current State | Notes |
|----------|---------------|-------|
| F01 OpenAI-compatible gateway | Implemented, partially cloud-accepted | Non-stream real provider path works for `moonshot-v1-8k`; stream final chunk ordering fixed locally and awaiting Canary/redeploy; Mimo provider config blocked by invalid key |
| F02 Channel management | Implemented | Admin API and frontend page exist |
| F03 API key management | Implemented | Includes allowed models and disable path |
| F04 Gateway auth middleware | Implemented | Tests pass |
| F05 Pre-freeze billing | Implemented | Needs current E2E balance and concurrency acceptance evidence |
| F06 Call logs | Implemented | Logs page and admin APIs exist |
| F07 Admin API | Implemented | Broad handler coverage exists |
| F08 Wallet management | Implemented | Top-up, adjustment, transactions exist |
| F09 Model pricing | Implemented | Base and org pricing exist |
| F10 Admin frontend | Implemented | Build, component tests, Canary source verification, and Codex local visual screenshot pass are complete; user/cloud browser visual acceptance still gates strict close |
| F11 Rate limit and admission control | Implemented | Needs current concurrency acceptance evidence |
| F12 Failover and circuit breaker | Implemented | Needs current failover/circuit acceptance evidence |
| F13 Sensitive-word filter | Implemented | Needs current E2E request rejection evidence |
| F14 Idempotency | Implemented | Needs current repeated request acceptance evidence |
| F15 Token estimation | Implemented | Tests pass |
| F16 Budget alert | Implemented | Needs current webhook acceptance evidence |
| F17 System settings | Implemented | Settings page/API exist |
| F18 Health checks | Production verified for PRD shape | `/health/live`, `/health/ready`, `/metrics` exist; cloud `/health/ready` returned `{"status":"ok","db":"ok","redis":"ok"}` after redeploy |

## Known Gaps

1. PRD acceptance checklist is partially reconciled with current evidence; cloud API smoke and Moonshot real-provider E2E are recorded, while Mimo provider credentials, stream final chunk redeploy, capacity load, and failover scenarios still need closure.
2. Some historical status files disagree about whether specific bugs are open or fixed.
3. `docs/testing/archive/` contains useful history but should not be used as the current status source.
4. Active issues are now centralized in `docs/issues/OPEN-ISSUES.md`; older issue status still needs item-by-item reconciliation.
5. Frontend API/client route contract needs continued explicit verification; this round fixed confirmed `KeysPage`, `WalletPage`, and `/me/password` route drift, added `ApiError`, added a route parity guard, and Canary passed the remaining closeout verification.
6. Historical docs mention XOR channel-key encryption, but current code uses AES-GCM helpers in `internal/auth/auth.go`; `docs/ops/test-report.md` now marks the XOR note as stale, and Canary passed the correction.
7. UI quality, frontend architecture, component reuse, and browser/manual validation are explicit PRD1 closeout items, not deferred wholesale to PRD2. Current source audit is recorded in `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; PRD1 UI redesign execution is recorded in `docs/design/PRD1-UI-REDESIGN-SPEC.md`; Codex completed a local Playwright visual screenshot pass and a reference-inspired shell/login/Dashboard pass, while Canary remains responsible only for non-visual source/build/test verification. User/cloud visual acceptance remains the final visual gate.
8. Cloud smoke found API list handlers could hide row scan errors and emit zero-value fields when nullable database columns were scanned into non-nullable Go types. Codex fixed `ListAPIKeys` and `ListLogs`; after deployment, authenticated cloud reads for `/api/v1/keys` and `/api/v1/logs` returned 200 on 2026-07-01.
9. User-reported PRD1 UI findings on logs filters, Dashboard trend/health cards, wallet edit actions, audit login records, `/docs`, and overall visual quality were classified in `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`; concrete bugs and the PRD1 visual redesign passed Canary non-visual verification.
10. Alibaba Cloud strict S0/S1 closeout smoke passed on 2026-07-01 after redeploy. The former default-admin blocker is fixed in cloud, and `/health/ready` now exposes the PRD DB/Redis shape. Remaining PRD1 closure depends on acceptance evidence or explicit deferral for the still-open matrix items.

## Active Sources Of Truth

| Need | Use This File |
|------|---------------|
| Product requirements | `docs/PRD-Phase1.md` |
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
