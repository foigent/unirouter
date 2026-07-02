# FIX-SIGNAL.md — PRD2 Batch 16 Developer Console Real-Backend Browser E2E

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 16

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 full-completion plan | Canary passed and Sentinel merged PR #47 at `66bc000` | User reported Canary/Sentinel completed; current `main` includes the plan |
| Batch 16 scope | Real backend browser Developer Console E2E | `docs/ops/PRD2-COMPLETION-PLAN.md` Batch 16 |
| `VERIFY-RESULT.md` | Still records the prior full-completion plan verification until Canary reruns | Codex must not forge Canary results |
| Required secret | `ENCRYPTION_KEY` or `UNIROUTE_SMOKE_ENCRYPTION_KEY` must match the backend under test | Seed script encrypts a mock upstream channel key that the running backend decrypts |

---

## Fix #1: Add real-backend Developer Console browser smoke

**Files:**

- `scripts/prd2-developer-console-smoke.sh`
- `scripts/prd2_developer_console_seed.go`
- `scripts/prd2_developer_console_smoke.mjs`
- `frontend/package.json`
- `frontend/package-lock.json`

**Original problem:** PRD2 Batch 10 had component tests and mock-browser evidence, but not a real-backend browser flow proving that a developer can create a project key, call the gateway, and troubleshoot the resulting request without restricted data exposure.

**Current implementation:**

- Adds a script-owned mock OpenAI-compatible upstream.
- Seeds a dedicated org, developer user, project, other project, approved model catalog, alias `company-chat`, project model set, wallet, and a dedicated mock channel.
- Uses a Batch 16-only target model `batch16-smoke-gpt-4o` so the smoke channel does not hijack real `gpt-4o` routes.
- Runs Playwright Chromium against the real frontend and backend.
- Verifies:
  - developer login and `/developer` landing;
  - project selector/context;
  - approved model catalog and alias visibility;
  - project API Key creation and one-time plaintext modal;
  - real `/v1/chat/completions` call with the created project key;
  - usage/log table updates;
  - Request Lookup shows metadata summary;
  - Request Lookup does not show provider key, full project API key after modal close, prompt plaintext, or response plaintext;
  - cross-project logs API returns 403;
  - direct `/api/v1/channels` returns 403 for developer JWT;
  - browser navigation to `/channels` redirects back to `/developer`.
- Adds Playwright as a frontend dev dependency so the smoke script does not rely on fragile `npx --package` module resolution.

**Codex local evidence:**

```bash
cd /Users/m4/Desktop/projects/UniRouter
set -a; source .env.production; set +a
LISTEN_PORT=18080 \
DB_DSN='postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable' \
REDIS_ADDR='127.0.0.1:6379' \
MIGRATIONS_DIR='migrations' \
go run ./cmd/uniroute
```

In another shell:

```bash
cd /Users/m4/Desktop/projects/UniRouter
set -a; source .env.production; set +a
UNIROUTE_SMOKE_BACKEND_URL='http://localhost:18080' \
UNIROUTE_SMOKE_FRONTEND_URL='http://localhost:3000' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-developer-console-smoke.sh
```

Actual local result:

```json
{
  "status": "PASS",
  "backend_url": "http://localhost:18080",
  "frontend_url": "http://localhost:3000",
  "project_id": 4,
  "other_project_id": 5,
  "request_id": "batch16-browser-1782957796339",
  "model_alias": "company-chat",
  "upstream_calls": 1,
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch16-developer-console.png"
}
```

Canary may use its own ports and DB; set `UNIROUTE_SMOKE_DB_DSN`, `UNIROUTE_SMOKE_BACKEND_URL`, and `UNIROUTE_SMOKE_FRONTEND_URL` as needed.

---

## Fix #2: Developer Console accepts array or CSV catalog fields

**File:** `frontend/src/pages/DeveloperConsolePage.tsx`

**Original problem:** The real backend returns model catalog `capability_tags` and `aliases` as arrays, while `DeveloperConsolePage` assumed CSV strings. The real-browser smoke crashed with `(value || "").split is not a function`.

**Current implementation:**

- `CatalogModel.capability_tags` and `CatalogModel.aliases` now accept `string | string[]`.
- `splitCSV()` now normalizes both arrays and comma-separated strings.

**Verification:**

- Batch 16 browser smoke now reaches the Developer Console instead of the app error page.
- Existing frontend tests pass.

---

## Fix #3: Request Lookup log detail is null-safe for `upstream_error`

**Files:**

- `internal/admin/log_handlers.go`
- `internal/admin/log_handlers_test.go`

**Original problem:** `ListLogs` used `COALESCE(upstream_error, '')`, but `GetLogDetail` scanned nullable `upstream_error` into a Go `string`. Successful gateway logs often have NULL `upstream_error`, so Request Lookup received `{"error":"log not found"}`.

**Current implementation:**

- `GetLogDetail` now selects `COALESCE(upstream_error, '')`.
- Added `TestGetLogDetail_NullSafeUpstreamError`.

**Verification:**

- `go test ./internal/admin` passes.
- Batch 16 browser smoke verifies Request Lookup detail for the real gateway request and renders status/model/resolved/provider/channel/tokens/cost/latency.

---

## Fix #4: Update PRD2 status, matrix, and issue tracking

**Files:**

- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/issues/OPEN-ISSUES.md`

**Current implementation:**

- Records that the full-completion plan passed Canary/Sentinel and PR #47 is on `main`.
- Records Batch 16 as fixed locally and awaiting Canary.
- Moves relevant P0 browser-flow rows to `Implemented not accepted` until Canary independently verifies.
- Adds `PRD2-DEVCON-001` as `Fixed awaiting Canary`.
- Keeps remaining PRD2 later items open: OIDC first, budget hard-blocking, production HA, P2-F13/F14/F15, License production evidence, and real upstream Anthropic E2E.

---

## Global Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
npm audit --prefix frontend --omit=dev --audit-level=moderate
git diff --check
```

Codex local results:

- `go test ./...` PASS.
- `npm --prefix frontend test -- --run` PASS: 10 files, 68 tests.
- `npm --prefix frontend run build` PASS, with existing Vite large chunk warning.
- `npm audit --prefix frontend --omit=dev --audit-level=moderate` PASS, 0 production vulnerabilities.
- `git diff --check` PASS.
- Batch 16 real-backend browser smoke PASS.

---

## Expected Canary Output

Please overwrite `docs/agent-workflows/VERIFY-RESULT.md` using this structure:

```markdown
# VERIFY-RESULT.md — PRD2 Batch 16 Developer Console Real-Backend Browser E2E 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 16

## 逐条结果

- Fix #1: PASS/FAIL — real-backend browser smoke script and seed are correct, isolated, and runnable.
- Fix #2: PASS/FAIL — Developer Console handles array and CSV catalog fields.
- Fix #3: PASS/FAIL — Request Lookup log detail handles NULL upstream_error.
- Fix #4: PASS/FAIL — status/matrix/issues accurately reflect Batch 16 without overclaim.

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | |
| `npm audit --prefix frontend --omit=dev --audit-level=moderate` | PASS/FAIL | |
| `scripts/prd2-developer-console-smoke.sh` | PASS/FAIL | Include backend/frontend URLs and request id |
| `git diff --check` | PASS/FAIL | |

## 新发现

- [如有，列出]

## 结论

PASS/FAIL. 明确说明 Batch 16 是否可交给 Sentinel，以及下一轮是否应进入 Batch 17。
```
