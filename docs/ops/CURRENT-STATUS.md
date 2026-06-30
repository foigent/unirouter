# UniRouter Current Status

> Purpose: single current-state entrypoint for humans, Codex, and OpenClaw Canary.
> Last verified: 2026-07-01 local workspace.

## Summary

UniRouter Phase 1 is in MVP hardening. Core backend modules, admin APIs, frontend pages, migrations, Docker deployment files, and monitoring assets exist. The project is not an early scaffold, but it is not yet fully PRD-accepted because several end-to-end and production-style acceptance cases still need current evidence. PRD1 must not close until the default-admin cloud security blocker found on 2026-07-01 is deployed and verified.

## Current Verification

| Area | Status | Evidence |
|------|--------|----------|
| Go tests | PASS | `go test ./...` passed on 2026-07-01 |
| Frontend tests | PASS | `npm test -- --run` passed in `frontend/` with 36 tests on 2026-07-01; Canary verified the previous 35-test round as PASS |
| Frontend build | PASS with warning | `npm run build` passed; Canary verified PASS; Vite reported large chunk warning |
| Local current-source smoke | PASS | Temporary `go run ./cmd/uniroute` on port 18080 passed health, metrics, login, `/me`, settings, dashboard, and unauthenticated password-route checks |
| PRD1 remaining closeout Canary | PASS | `VERIFY-RESULT.md` reports 6/6 PASS for PRD1 remaining closeout; external-only items remain in `PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| Browser/manual frontend validation | Codex visual QA PASS locally | Codex Playwright screenshots captured Login, Dashboard, Logs detail, Wallet, API Docs, Channels, mobile Dashboard, mobile Logs, and the reference-inspired Login/Dashboard/mobile pass under local `output/playwright/`; Canary/OpenClaw visual acceptance remains not applicable |
| Real upstream provider E2E | Partially evidenced historically | Needs current repeatable evidence for PRD acceptance |
| Production deployment status | PARTIAL / blocked | Alibaba Cloud availability smoke passes, and authenticated admin read APIs return 200. However `admin/admin123456` still returned a JWT on 2026-07-01, so the patched default-admin protection must be deployed before PRD1 can close |
| Default admin password protection | Fixed locally, awaiting Canary and redeploy | Web login now rejects `admin/admin123456` unless `UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN=true`; CLI `admin set-password` and `admin create` were added for deployment remediation |
| `/health/ready` PRD response shape | Fixed locally, awaiting redeploy | Code now returns `{"status":"ok","db":"ok","redis":"ok"}`; current cloud may still return the old `{"status":"ready"}` until redeployed |

## Phase 1 Delivery State

| PRD Area | Current State | Notes |
|----------|---------------|-------|
| F01 OpenAI-compatible gateway | Implemented, tested by unit/mock paths | Real multi-provider E2E still needs current acceptance evidence |
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
| F18 Health checks | Implemented, patched for PRD shape | `/health/live`, `/health/ready`, `/metrics` exist; `/health/ready` PRD response shape fixed locally and awaiting redeploy verification |

## Known Gaps

1. PRD acceptance checklist is partially reconciled with current evidence; non-destructive cloud API smoke is now recorded, while real provider E2E and stress/failover scenarios still need current evidence.
2. Some historical status files disagree about whether specific bugs are open or fixed.
3. `docs/testing/archive/` contains useful history but should not be used as the current status source.
4. Active issues are now centralized in `docs/issues/OPEN-ISSUES.md`; older issue status still needs item-by-item reconciliation.
5. Frontend API/client route contract needs continued explicit verification; this round fixed confirmed `KeysPage`, `WalletPage`, and `/me/password` route drift, added `ApiError`, added a route parity guard, and Canary passed the remaining closeout verification.
6. Historical docs mention XOR channel-key encryption, but current code uses AES-GCM helpers in `internal/auth/auth.go`; `docs/ops/test-report.md` now marks the XOR note as stale, and Canary passed the correction.
7. UI quality, frontend architecture, component reuse, and browser/manual validation are explicit PRD1 closeout items, not deferred wholesale to PRD2. Current source audit is recorded in `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; PRD1 UI redesign execution is recorded in `docs/design/PRD1-UI-REDESIGN-SPEC.md`; Codex completed a local Playwright visual screenshot pass and a reference-inspired shell/login/Dashboard pass, while Canary remains responsible only for non-visual source/build/test verification. User/cloud visual acceptance remains the final visual gate.
8. Cloud smoke found API list handlers could hide row scan errors and emit zero-value fields when nullable database columns were scanned into non-nullable Go types. Codex fixed `ListAPIKeys` and `ListLogs`; after deployment, authenticated cloud reads for `/api/v1/keys` and `/api/v1/logs` returned 200 on 2026-07-01.
9. User-reported PRD1 UI findings on logs filters, Dashboard trend/health cards, wallet edit actions, audit login records, `/docs`, and overall visual quality were classified in `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`; concrete bugs and the PRD1 visual redesign passed Canary non-visual verification.
10. Alibaba Cloud accepted the public seeded default admin password `admin/admin123456` on 2026-07-01. This is a PRD1 security blocker until the local fix is Canary-verified, deployed, and `scripts/prd1-cloud-smoke.sh` confirms the default password is rejected.

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
| PRD1 UI redesign execution | `docs/design/PRD1-UI-REDESIGN-SPEC.md` |
| Remaining PRD1 acceptance pack | `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| PRD acceptance progress | `docs/ops/PRD-ACCEPTANCE-MATRIX.md` |
| Active unresolved issues | `docs/issues/OPEN-ISSUES.md` |
| Agent collaboration protocol | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Current Codex to Canary verification request | `docs/agent-workflows/FIX-SIGNAL.md` |
| Current Canary verification result | `docs/agent-workflows/VERIFY-RESULT.md` |
