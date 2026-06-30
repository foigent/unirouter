# PRD1 Closeout Plan

> Purpose: define the exit path from PRD1 to PRD2.
> Owner model: User confirms PRD, Codex develops and self-tests, OpenClaw Canary performs independent verification, Sentinel handles GitHub PR/merge/CI, Atlas handles cloud deployment.

## Exit Goal

PRD1 can close when the product has a stable MVP baseline with:

1. Core gateway and billing flows accepted with current evidence.
2. Admin workflows accepted through API validation plus a separate browser/manual validation step.
3. UI structure is coherent enough for real use and future PRD2 iteration.
4. Critical security and data isolation issues are either fixed or explicitly blocked.
5. OpenClaw Canary has passed the final verification round.
6. Sentinel has completed PR/merge/CI.
7. Atlas has completed cloud deployment verification.

## Required Workflow

1. User and Codex confirm the PRD1 closeout scope.
2. Codex implements fixes and necessary tests.
3. Codex updates `docs/agent-workflows/FIX-SIGNAL.md`.
4. User asks OpenClaw Canary to verify.
5. OpenClaw Canary tests and writes `docs/agent-workflows/VERIFY-RESULT.md`.
6. If Canary reports bugs, Codex fixes and returns to step 3.
7. If Canary passes, user notifies Sentinel for GitHub PR, merge, and CI.
8. After Sentinel passes, user notifies Atlas for cloud deployment.
9. Deployment feedback that requires code changes returns to Codex through the same `VERIFY-RESULT.md` / `FIX-SIGNAL.md` loop.

## PRD1 Exit Criteria

| Area | Exit Criteria | Evidence Required |
|------|---------------|-------------------|
| Gateway | `/v1/models`, `/v1/chat/completions`, stream and non-stream paths work with configured providers | Commands, request/response samples, call log rows |
| Billing | Top-up, freeze, settle, insufficient balance, idempotency, and reconciliation are accepted | Balance before/after, transaction rows, idempotency rows |
| Routing | Allowed models, failover, circuit breaker, and channel-key cooldown behavior are accepted | Test scenario and DB/API evidence |
| Admin API | Channel, channel key, API key, wallet, logs, prices, users, settings, audit, sensitive words operate correctly | API/manual acceptance notes |
| Frontend UI | Admin UI has consistent layout, reusable patterns, no critical broken flows, and current browser/manual validation from a capable step | Browser/manual test notes if available; Canary/OpenClaw is limited to non-visual verification |
| Observability | Health, metrics, and Grafana dashboard are verified in a running environment | curl output and dashboard/manual evidence |
| Deployment | Docker Compose and cloud deployment are verified | Sentinel CI and Atlas deployment notes |

## UI Closeout Scope

The UI is part of PRD1 closeout because the admin console is required for operation. It should not be postponed wholesale to PRD2.

### UI Architecture Checks

| Check | Current Concern | PRD1 Target |
|-------|-----------------|-------------|
| Layout shell | Sidebar and content shell exist, but page-level patterns may be inconsistent | One stable admin shell with predictable navigation and responsive behavior |
| Component reuse | Pages appear to repeat table/form/modal patterns directly | Identify common resource-table/form/action patterns; refactor only where it reduces clear duplication |
| Visual system | Semi Design is used, but page styling may be ad hoc | Consistent spacing, card usage, table density, button hierarchy, empty/error/loading states |
| UX flows | Several pages exist, but full browser flow is not currently verified | Validate core admin tasks in a separate browser/manual acceptance step |
| API contracts | Frontend client may expose endpoints not registered in backend | Add or verify route contract coverage before PRD1 close |

### UI Pages To Review

| Page | PRD1 Review Focus |
|------|-------------------|
| Dashboard | Activity overview, time range behavior, wallet/channel/model data clarity |
| Channels | CRUD, channel test, model field UX, destructive action confirmation |
| Channel Keys | Key creation/editing, key masking, allowed model UX |
| API Keys | Allowed IP/model UX, disable behavior, generated key handling |
| Wallet | Top-up, adjustment, transaction list, export |
| Logs | Filters, detail panel, request ID copy, empty/error states |
| Model Prices / Org Prices | Model selection, pricing units, pagination |
| Users / Audit Logs | Basic admin operations and traceability |
| Settings | Settings update, password/change-profile contract mismatch check |
| Test Page | Channel/model selection and result clarity |

## Known PRD1 Work Items

| ID | Area | Item | Status |
|----|------|------|--------|
| PRD1-QA-001 | E2E | Run current real-provider acceptance matrix | Open |
| PRD1-QA-002 | Billing | Verify interrupted streaming settlement | Open |
| PRD1-QA-003 | Stability | Verify 100 concurrent billing requests do not create negative balance | Open |
| PRD1-QA-004 | Routing | Verify failover and circuit breaker behavior | Open |
| PRD1-UI-001 | UI | Audit frontend architecture, component reuse, and visual consistency | Canary passed |
| PRD1-UI-002 | UI | Browser/manual validation of core admin flows by a capable step, not Canary/OpenClaw | Codex visual QA complete locally; Canary non-visual verification passed; user/cloud visual acceptance still pending |
| PRD1-UI-003 | UI | PRD1 visual redesign and reusable UI foundation | Canary passed |
| PRD1-SEC-002 | Security | Public seeded default admin password must not work in cloud | Production verified: cloud returned 403 for `admin/admin123456` |
| PRD1-OPS-001 | Ops | `/health/ready` must match PRD response shape | Production verified: cloud returned `{"status":"ok","db":"ok","redis":"ok"}` |
| PRD1-ENG-001 | Contract | Resolve frontend `/me/password` client/backend route mismatch | Canary passed |
| PRD1-ENG-002 | Contract | Verify and prevent frontend route drift in key/wallet/admin flows | Canary passed |
| PRD1-DOC-001 | Docs | Reconcile historical docs only where they affect current open issues | Canary passed |

## Current External Blockers

Codex completed the local code/test closeout items that are possible in this workspace. The remaining open PRD1 items require an external capability and are tracked in `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`:

- Docker Compose clean deployment cannot be run here because `docker` is unavailable.
- Real upstream provider E2E requires configured provider keys and channels.
- Browser/manual UI validation requires a human or browser-capable step; Codex completed a local Playwright screenshot pass, while Canary/OpenClaw cannot perform visual acceptance.
- Grafana dashboard validation requires a running Grafana environment.

## Cloud Deployment Evidence

Alibaba Cloud non-destructive API smoke passed on 2026-07-01:

- `http://8.130.18.85:3000/` returns frontend HTML.
- `http://8.130.18.85:8080/health/live` returns 200.
- `http://8.130.18.85:8080/health/ready` returns 200 with `{"status":"ok","db":"ok","redis":"ok"}`.
- `http://8.130.18.85:8080/metrics` returns Prometheus text metrics.
- `http://8.130.18.85:3000/health/ready` returns 200 through nginx with the PRD DB/Redis shape.
- `http://8.130.18.85:9090/-/healthy` returns 200.
- `http://8.130.18.85:3001/login` returns Grafana login HTML.
- Gateway no-key smoke through `:3000/v1/models` returns expected 401.
- Admin no-token smoke through `:3000/api/v1/channels` returns expected 401.
- Authenticated admin API smoke through `:3000` passed for login, `/api/v1/me`, dashboard, channels, keys, channel keys, wallet, wallet transactions, logs, model prices, model names, org model prices, reconciliation, users, grants, audit logs, settings, and sensitive words.
- No real provider paid calls, write mutations, pressure tests, stream disconnects, or fault injection were run in this pass.

Still external after cloud smoke:

- Real provider E2E and billing/reconciliation evidence.
- Browser/manual UI walkthrough.
- Grafana panel-level visual verification.
- Stress, failover, and interrupted-streaming scenarios.

## User-Reported UI Findings

On 2026-07-01, the user reported seven UI/product issues. Codex classified them in `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`.

Fixed locally and Canary passed:

- Logs model filter options now come from model names instead of time-range Dashboard distribution.
- Dashboard trend buckets now differ for 24h versus longer ranges.
- Dashboard channel health card layout no longer relies on crowded inline fields.
- Wallet admin actions are visible when backend role is admin/platform_admin, and adjustment uses the backend `target_balance` contract.
- Login audit writes synchronously with actor context, and the audit page maps backend fields correctly.
- `/docs` has a frontend route and a less-placeholder backend page.

PRD1 visual redesign completed locally and Canary non-visual verification passed:

- Added `frontend/src/ui/` design-layer primitives: `PageHeader`, `Surface`, `MetricTile`, `MoneyText`, `LatencyText`, `StatusTag`, `EmptyState`.
- Updated the admin shell, login, Dashboard, Logs, Wallet, API Docs, Channels, Keys, Channel Keys, Model Prices, Org Model Prices, Grants, Users, Audit Logs, Settings, Test, and Reconciliation pages to use a shared visual system.
- Added `docs/design/PRD1-UI-REDESIGN-SPEC.md` as the executable design-system handoff.
- Codex local screenshot evidence is kept under ignored `output/playwright/`.

## Security Blocker Resolved During Strict Closeout

On 2026-07-01, Codex confirmed the public Alibaba Cloud deployment accepted `admin/admin123456` and returned a JWT. After the patched build was redeployed, the same default login returned 403 and `scripts/prd1-cloud-smoke.sh` passed `default-admin-password-rejected`.

Remediation completed in this round:

- Web login rejects the public default admin password unless `UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN=true`.
- `./uniroute admin set-password --username admin --password <strong-password> --org-id 1` supports secure bootstrap remediation.
- `./uniroute admin create` supports creating additional admins without relying on a nonexistent historical command.
- `/health/ready` now returns the PRD shape `{"status":"ok","db":"ok","redis":"ok"}` in cloud through both backend direct and nginx routes.

This specific security/ops blocker is closed. PRD1 remains open only if the remaining acceptance-matrix items are not evidenced or explicitly deferred.

## What Can Move To PRD2

Only move an item to PRD2 if PRD1 can still be operated safely and the user explicitly agrees. Candidates may include deeper UI redesign, advanced analytics, SSO/2FA, SaaS multi-tenancy, semantic cache, and broader provider-specific capability tuning.
