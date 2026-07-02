# FIX-SIGNAL.md — PRD2 Batch 19 Notification/Event And Project Budget Completion

**修复者：** Codex
**日期：** 2026-07-02
**修复轮次：** PRD2 Batch 19

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 18 | Open / deferred | User explicitly said Batch 18 should not be done now; it is recorded as later unfinished work |
| Production webhook evidence | Not claimed | This batch has local production-like webhook receiver evidence only |
| Frontend warnings | Existing non-blocking warnings may appear | Current Vitest/build already has React Router future flag, Semi `findDOMNode`, and Vite large chunk warnings |
| Smoke runtime | Requires real backend, real frontend, PostgreSQL, Redis | `scripts/prd2-batch19-notifications-budget-smoke.sh` is a real-backend/browser smoke |

---

## Fix #1: Notification webhook retry worker and correct aggregate delivery state

**Files:**

- `internal/billing/notifications.go:153`
- `internal/billing/notifications.go:388`
- `internal/billing/notifications.go:456`
- `cmd/uniroute/main.go:113`
- `cmd/uniroute/main.go:487`

**Original issue:**

- Batch 7 recorded `next_retry_at` but did not run a background retry worker.
- A retried delivery could incorrectly mark the event `delivered` even if another delivery for the same event remained failed.
- Real PostgreSQL exposed a parameter-type issue in `updateEventDeliveryState`; the fix now avoids reusing one placeholder for both assignment and conditional logic.

**Current code:**

- `EventService.RetryDueDeliveries(ctx, limit)` loads due failed deliveries and retries them.
- `startNotificationRetryLoop` runs once on startup and then every minute.
- `refreshEventDeliveryStateFromDeliveries` marks an event `delivered` only when every delivery row for that event is delivered; otherwise it remains failed.

**Verification method:**

```bash
go test ./internal/billing
go test ./cmd/uniroute
```

Expected:

- `TestEventService_RetryDueDeliveries` passes.
- `TestEventService_RetryDueDeliveriesKeepsEventFailedWhenOtherDeliveriesRemain` passes.

---

## Fix #2: Remaining notification emitters are wired

**Files:**

- `internal/billing/notifications.go:21`
- `internal/admin/handlers.go:53`
- `internal/admin/key_handlers.go:373`
- `internal/admin/model_governance_handlers.go:310`
- `internal/admin/license_handlers.go:71`
- `internal/gateway/relay.go:67`
- `internal/gateway/relay.go:322`
- `internal/gateway/relay.go:336`
- `internal/gateway/relay.go:987`

**Original issue:**

- P2-F08 reserved emitters for key/model/provider/license events, but most were not wired.

**Current code:**

- `api_key.disabled` emits when an API key transitions to disabled.
- `model.deprecated` and `model.retired` emit when model lifecycle changes to those states.
- `provider.channel.failure` emits on upstream transport failures and classified channel/auth/rate-limit failures.
- `license.warning` emits after license update when the license status is warning-worthy.
- `token_metering.estimated_ratio.high` remains from Batch 17.

**Verification method:**

```bash
go test ./internal/admin ./internal/gateway ./internal/billing
```

Expected:

- Admin model-governance tests still pass with the updated lifecycle scan shape.
- API key disable notification test passes.
- Gateway tests pass with provider failure emitter wiring.

---

## Fix #3: Frontend Notification Center

**Files:**

- `frontend/src/pages/NotificationCenterPage.tsx:113`
- `frontend/src/api/client.ts:261`
- `frontend/src/navigation.tsx:75`
- `frontend/src/navigation.tsx:129`
- `frontend/src/navigation.tsx:196`
- `frontend/src/main.tsx:97`
- `frontend/src/App.tsx:90`
- `frontend/src/pages/__tests__/NotificationCenterPage.test.tsx:21`
- `frontend/src/api/__tests__/client.test.ts:272`
- `frontend/src/__tests__/navigation.test.tsx:41`

**Original issue:**

- P2-F08 had backend APIs but no frontend notification center for events, delivery state, or webhook routes.

**Current code:**

- `/notifications` shows Events, Deliveries, and Webhook tabs.
- Admin/billing navigation includes Notification Center.
- The topbar bell opens `/notifications`.
- Webhook routes can be created and edited from the UI.

**Verification method:**

```bash
npm --prefix frontend test -- --run NotificationCenterPage client navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

Expected:

- Targeted Notification Center/client/navigation tests pass.
- Full frontend test suite passes.
- Build passes; large chunk warning is non-blocking and pre-existing.

---

## Fix #4: Project budget period-boundary and browser/API smoke evidence

**Files:**

- `internal/billing/billing_integration_test.go:184`
- `scripts/prd2-batch19-notifications-budget-smoke.sh`
- `scripts/prd2_batch19_notifications_budget_seed.go`
- `scripts/prd2_batch19_notifications_budget_smoke.mjs`

**Original issue:**

- Project budget alerts had backend unit coverage but lacked explicit daily/monthly boundary coverage and real-backend browser/API evidence.

**Current code:**

- Integration test proves daily budgets ignore previous-day spend and monthly budgets ignore previous-month spend.
- Batch 19 smoke starts a local webhook receiver, seeds a budget project, triggers `project_budget.threshold`, verifies a 204 webhook delivery, checks backend APIs, and verifies the Notification Center in Chromium.

**Verification method:**

```bash
go test -tags=integration ./internal/billing -run TestEventService_CheckProjectBudgetPeriodBoundaries_Integration -count=1 -v
```

For smoke, start a current-code backend and frontend first, then run:

```bash
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18080 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch19-notifications-budget-smoke.sh
```

Codex local actual result:

```json
{
  "status": "PASS",
  "batch": 19,
  "backend_url": "http://localhost:18080",
  "frontend_url": "http://localhost:3000",
  "project_id": 34,
  "event_id": 10,
  "webhook_id": 5,
  "webhook_receipts": 1,
  "budget_cents": 1000,
  "spent_cents": 1200,
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch19-notifications-budget.png"
}
```

---

## Fix #5: Status docs record Batch 18 deferred and Batch 19 awaiting Canary

**Files:**

- `docs/ops/CURRENT-STATUS.md:4`
- `docs/ops/PRD2-COMPLETION-PLAN.md:181`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:67`
- `docs/issues/OPEN-ISSUES.md:42`
- `docs/issues/OPEN-ISSUES.md:43`
- `docs/tech-ref/14-notification-event-foundation.md:4`

**Original issue:**

- After the user deferred Batch 18, current docs still treated it as the immediate next blocker.
- Notification tech reference still described Batch 7 limits after Batch 19 code existed.

**Current state:**

- Batch 18 is still open but explicitly deferred to later unfinished work.
- `PRD2-B19-001` is `Fixed awaiting Canary`.
- Matrix keeps P2-F08 and Project budget alerts at `Implemented not accepted`, with local evidence recorded but no Canary/production overclaim.
- Notification tech reference records the Batch 19 frontend/retry/emitter state and remaining production webhook boundary.

**Verification method:**

```bash
rg -n "Batch 19|PRD2-B19-001|Batch 18|production webhook" \
  docs/ops/CURRENT-STATUS.md \
  docs/ops/PRD2-COMPLETION-PLAN.md \
  docs/ops/PRD2-ACCEPTANCE-MATRIX.md \
  docs/issues/OPEN-ISSUES.md \
  docs/tech-ref/14-notification-event-foundation.md
```

Expected:

- Batch 19 is fixed locally and awaiting Canary.
- Batch 18 is deferred, still open, and not marked complete.
- Production webhook evidence is not overclaimed.

---

## Global Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/billing
go test -tags=integration ./internal/billing -run TestEventService_CheckProjectBudgetPeriodBoundaries_Integration -count=1 -v
go test ./...
npm --prefix frontend test -- --run NotificationCenterPage client navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
node --check scripts/prd2_batch19_notifications_budget_smoke.mjs
git diff --check
```

Optional real-backend browser/API smoke:

```bash
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18080 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch19-notifications-budget-smoke.sh
```

Codex local notes:

- All global commands passed on 2026-07-02.
- The optional real-backend smoke passed on 2026-07-02.
- The temporary backend logged an existing `sensitive_words` JSON parse warning from local settings; health remained `{"status":"ok","db":"ok","redis":"ok"}` and smoke passed.

---

## Expected Canary Output

Please overwrite `docs/agent-workflows/VERIFY-RESULT.md` using this structure:

```markdown
# VERIFY-RESULT.md — PRD2 Batch 19 Notification/Event And Project Budget 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 19

## 逐条结果

- Fix #1: PASS/FAIL — retry worker and aggregate delivery state are correct.
- Fix #2: PASS/FAIL — key/model/provider/license notification emitters are wired without leaking secrets.
- Fix #3: PASS/FAIL — Notification Center frontend/API/navigation works.
- Fix #4: PASS/FAIL — project budget period-boundary test and optional smoke are valid.
- Fix #5: PASS/FAIL — docs record Batch 18 as deferred and Batch 19 as awaiting Canary without production overclaim.

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/billing` | PASS/FAIL | |
| `go test -tags=integration ./internal/billing -run TestEventService_CheckProjectBudgetPeriodBoundaries_Integration -count=1 -v` | PASS/FAIL/SKIP | If no test DB, record environment reason |
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test -- --run NotificationCenterPage client navigation` | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | Large chunk warning is non-blocking |
| `node --check scripts/prd2_batch19_notifications_budget_smoke.mjs` | PASS/FAIL | |
| `scripts/prd2-batch19-notifications-budget-smoke.sh` | PASS/FAIL/SKIP | Requires backend/frontend/PostgreSQL/Redis |
| `git diff --check` | PASS/FAIL | |

## 新发现

- [如有，列出]

## 结论

PASS/FAIL. 明确说明 Batch 19 是否可以交给 Sentinel，以及 Batch 18 是否仍作为 later unfinished item 而不是本轮阻断。
```
