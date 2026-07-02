# FIX-SIGNAL.md — PRD2 Batch 17 Model Governance And Usage/Metering

**修复者：** Codex
**日期：** 2026-07-02
**修复轮次：** PRD2 Batch 17

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 16 | User reported Canary/Sentinel/Atlas completed; `main` includes PR #48 at `6876746` | Batch 17 builds on Developer Console real-backend smoke and catalog/log fixes |
| Running services | Backend, frontend, PostgreSQL, Redis | Batch 17 includes real-backend browser smoke |
| Backend under test | Must run current worktree, not stale `./uniroute` binary | Batch 17 validates new notification/reconciliation code |
| Smoke DB DSN | `UNIROUTE_SMOKE_DB_DSN` may be required | Seed script writes isolated Batch17 fixtures |

---

## Fix #1: Full-query reconciliation summary and estimated-ratio event

**Files:**

- `internal/admin/model_handlers.go`
- `internal/admin/handlers.go`
- `internal/billing/notifications.go`
- `cmd/uniroute/main.go`
- `internal/admin/admin_mock_test.go`

**Implementation:**

- `ListReconciliation` now computes summary metrics over the full filtered query before pagination.
- Summary includes:
  - `estimated_tokens`
  - `upstream_tokens`
  - `preflight_estimated_tokens`
  - `actual_tokens`
  - `variance_tokens`
  - `variance_ratio`
  - `average_source_confidence`
  - `estimated_ratio`
  - `estimated_ratio_alert_threshold`
  - `estimated_usage_alert`
  - `estimated_ratio_scope=filtered_query`
- When the filtered estimated ratio exceeds the configured threshold, the handler emits `token_metering.estimated_ratio.high` through the existing notification event service.
- The admin handler now receives the shared event service in `cmd/uniroute/main.go`.

**Expected Canary checks:**

- Unit tests verify full-query summary differs from page row totals.
- Unit tests verify high estimated-ratio event emission is deduplicated through notification events.
- Smoke verifies `page_size=2` still returns full-query `estimated_tokens=80`, `upstream_tokens=20`, and `estimated_ratio=0.8`.

---

## Fix #2: Usage-source filters in frontend usage surfaces

**Files:**

- `frontend/src/api/client.ts`
- `frontend/src/api/__tests__/client.test.ts`
- `frontend/src/pages/ReconciliationPage.tsx`
- `frontend/src/pages/__tests__/ReconciliationPage.test.tsx`
- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/pages/__tests__/LogsPage.test.tsx`

**Implementation:**

- `api.getReconciliation()` now accepts `usage_source`.
- Reconciliation page uses the API client instead of raw `api.request`.
- Reconciliation page renders source filter, source-confidence/variance columns, and full-query metric cards.
- Logs page adds `usage_source` filter and table column.

**Expected Canary checks:**

- Frontend tests pass.
- Browser smoke can see Reconciliation `来源筛选` and summary cards.
- API client encodes `usage_source` for logs and reconciliation.

---

## Fix #3: Developer Console shows deprecated migration recommendation

**Files:**

- `frontend/src/pages/DeveloperConsolePage.tsx`
- `frontend/src/pages/__tests__/DeveloperConsolePage.test.tsx`
- `frontend/src/index.css`

**Implementation:**

- Approved Catalog rows in Developer Console show `migration_recommendation` for `deprecated` models.
- Unit fixture verifies `Migrate to gpt-4.1-mini` renders.
- Batch17 smoke verifies a real deprecated fixture renders `Migrate to batch17-ga-chat before the next renewal window`.

---

## Fix #4: Batch17 real-backend smoke

**Files:**

- `scripts/prd2-batch17-modelgov-usage-smoke.sh`
- `scripts/prd2_batch17_modelgov_usage_seed.go`
- `scripts/prd2_batch17_modelgov_usage_smoke.mjs`

**Implementation:**

- Seed creates isolated Batch17 org, platform admin, developer, project, model set, GA/deprecated/unapproved/retired models, aliases, wallet, reconciliation logs, and estimated-ratio threshold.
- Smoke verifies:
  - Model Registry create/edit/list through real backend.
  - Project model-set assignment through real backend.
  - Approved Catalog hides unapproved and retired models.
  - Deprecated migration recommendation is visible in Developer Console.
  - Retired model gateway call returns 403 with `model_retired`.
  - Reconciliation full-query aggregate remains correct with paginated data.
  - High estimated-ratio notification event is persisted and queryable.
  - Admin Model Governance and Reconciliation browser surfaces render.

**Codex local command and result:**

```bash
cd /Users/m4/Desktop/projects/UniRouter
LISTEN_PORT=18080 \
DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
JWT_SECRET='uniroute-jwt-secret-2024' \
API_KEY_HASH_SECRET='uniroute-jwt-secret-2024' \
ENCRYPTION_KEY='0123456789abcdef0123456789abcdef' \
SKIP_MIGRATION=true \
go run ./cmd/uniroute
```

In another shell:

```bash
cd /Users/m4/Desktop/projects/UniRouter
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18080 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch17-modelgov-usage-smoke.sh
```

Actual local result:

```json
{
  "status": "PASS",
  "batch": 17,
  "backend_url": "http://localhost:18080",
  "frontend_url": "http://localhost:3000",
  "project_id": 27,
  "model_set_id": 13,
  "retired_model": "batch17-retired-chat",
  "reconciliation_model": "batch17-recon-metering",
  "reconciliation_summary": {
    "estimated_tokens": 80,
    "upstream_tokens": 20,
    "preflight_estimated_tokens": 90,
    "actual_tokens": 100,
    "variance_tokens": 10,
    "variance_ratio": 0.1111111111111111,
    "average_source_confidence": 0.7666666666666667,
    "estimated_ratio": 0.8,
    "estimated_ratio_alert_threshold": 0.2,
    "estimated_usage_alert": true,
    "estimated_ratio_alert_threshold_setting": "token_metering.estimated_ratio_alert_threshold",
    "estimated_ratio_scope": "filtered_query"
  },
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch17-modelgov-usage.png"
}
```

---

## Documentation Updates

**Files:**

- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/issues/OPEN-ISSUES.md`

**Expected status discipline:**

- Batch16 is recorded as completed per User report.
- Batch17 is recorded as fixed locally and awaiting Canary.
- P2-F13/P2-F14/P2-F15 remain mandatory and not complete.
- OIDC-first SSO, project budget hard-blocking, production HA, License production evidence, and real upstream Anthropic E2E remain open/later work.

---

## Global Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
node --check scripts/prd2_batch17_modelgov_usage_smoke.mjs
git diff --check
```

Codex local results:

- `go test ./...` PASS.
- `npm --prefix frontend test -- --run` PASS: 11 files, 73 tests.
- `npm --prefix frontend run build` PASS, with existing Vite large chunk warning.
- `node --check scripts/prd2_batch17_modelgov_usage_smoke.mjs` PASS.
- `scripts/prd2-batch17-modelgov-usage-smoke.sh` PASS with current-code backend on `http://localhost:18080`.

---

## Expected Canary Output

Please overwrite `docs/agent-workflows/VERIFY-RESULT.md` using this structure:

```markdown
# VERIFY-RESULT.md — PRD2 Batch 17 Model Governance And Usage/Metering 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 17

## 逐条结果

- Fix #1: PASS/FAIL — full-query reconciliation summary and estimated-ratio event.
- Fix #2: PASS/FAIL — usage_source filters in frontend usage surfaces.
- Fix #3: PASS/FAIL — Developer Console migration recommendation visibility.
- Fix #4: PASS/FAIL — Batch17 real-backend smoke and seed isolation.
- Fix #5: PASS/FAIL — status/matrix/issues accurately reflect Batch17 without overclaim.

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | |
| `node --check scripts/prd2_batch17_modelgov_usage_smoke.mjs` | PASS/FAIL | |
| `scripts/prd2-batch17-modelgov-usage-smoke.sh` | PASS/FAIL/SKIP | Include backend/frontend URLs and reconciliation model |
| `git diff --check` | PASS/FAIL | |

## 新发现

- [如有，列出]

## 结论

PASS/FAIL. 明确说明 Batch17 是否可交给 Sentinel，以及下一轮是否应进入 Batch18。
```
