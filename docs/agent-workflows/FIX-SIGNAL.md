# FIX-SIGNAL.md — PRD2 Batch 23 P2 Product Features

**修复者：** main agent
**日期：** 2026-07-04
**修复轮次：** PRD2 Batch 23

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 18 real upstream provider acceptance | 仍 Open/Deferred | User 已明确延后，Batch 23 使用本地 mock upstream，不替代 OpenRouter/Anthropic 真实上游验收 |
| P2-F10 License production/approved-staging evidence | 仍 Open | Batch 23 不处理 License target-environment promotion |
| OIDC first SSO | 仍为后续 Batch 24 | Batch 23 只处理 user-confirmed P2-F13/F14/F15 和 internal status/error docs |
| Runtime cache | 不得默认启用 | P2-F15 仅为 research spike，PRD2 不启用默认 prompt/response cache |

> Canary 验收时：以上未关闭项不算本轮 FAIL；需要确认文档没有把 Batch 23 本地 smoke 过度标记为 Canary/Sentinel/Production verified。

---

## Fix #1: Error Docs Registry And Gateway `doc_url`

**文件：** `internal/errdocs/errdocs.go`, `internal/gateway/relay.go`, `internal/middleware/auth.go`, `frontend/src/pages/ApiDocsPage.tsx`

**原问题：** PRD2 P2-F13 要求 Playground 和 gateway 错误能指向错误文档，但旧响应没有统一 `doc_url`，认证中间件错误尤其不会返回可跳转文档。

**当前代码：**

- 新增 error-doc registry，覆盖 validation/auth/model/billing/upstream/internal 常见错误。
- Relay `writeError` 返回 OpenAI-compatible `error.type/code/message/doc_url/param`。
- Gateway API Key auth 错误现在也返回 JSON `code` 和 `doc_url`。
- `/api/v1/error-docs` 暴露错误文档注册表；API Docs 页面渲染可跳转 anchor。

**验证方法：**

```bash
go test ./internal/errdocs ./internal/middleware ./internal/gateway ./cmd/uniroute
npm --prefix frontend test -- --run client
```

---

## Fix #2: P2-F13 Playground Integrated With Approved Catalog

**文件：** `frontend/src/pages/DeveloperConsolePage.tsx`, `frontend/src/pages/__tests__/DeveloperConsolePage.test.tsx`

**原问题：** Developer Console 只有 quickstart 和项目 Key，没有实际 Playground；用户无法在项目范围内用 approved model/alias 做一次 OpenAI-compatible 调用并看到 request id、tokens、cost、latency 或错误文档。

**当前代码：**

- Playground 只接受项目 API Key，不暴露共享平台 Key。
- 模型下拉来自当前项目 approved catalog 的 model/alias。
- 支持非流式和流式调用 `/v1/chat/completions`。
- 成功结果展示 response content、request_id、tokens、cost、latency。
- 错误结果展示 message 和 `doc_url` 链接。
- Prompt/response 只保留在当前页面 state，不新增持久化历史。
- 创建项目 Key 后会自动填入 Playground Key 输入框，仍保留一次性完整 Key 展示规则。

**验证方法：**

```bash
npm --prefix frontend test -- --run DeveloperConsolePage
npm --prefix frontend run build
```

---

## Fix #3: P2-F14 Advanced Usage Analytics

**文件：** `internal/admin/prd2_product_handlers.go`, `frontend/src/pages/UsageAnalyticsPage.tsx`, `frontend/src/api/client.ts`

**原问题：** PRD2 要求按 org/project/key/model/alias/provider/channel/status/usage_source drill-down，并支持 CSV 导出和 estimated usage ratio 可见。

**当前代码：**

- 新增 `GET /api/v1/usage/analytics`。
- `group_by` 白名单限制为 `project/key/model/alias/provider/channel/status/usage_source`，避免拼接任意 SQL。
- 支持 org/project/key/model/alias/provider/channel/status/usage_source/start_time/end_time filters。
- 项目范围角色必须提供 `project_id`，非平台角色按 JWT org 限制。
- Summary 返回 requests、success/failed、tokens、cost、avg latency、estimated/upstream tokens、estimated_ratio、CSV scope、prompt/response retention。
- `format=csv` 返回 filtered-query CSV。
- UI 提供筛选、KPI、drill-down 表格和 CSV 导出。

**验证方法：**

```bash
go test ./internal/admin
npm --prefix frontend test -- --run UsageAnalyticsPage client navigation
```

---

## Fix #4: Internal Status Page And Incident History

**文件：** `internal/admin/prd2_product_handlers.go`, `frontend/src/pages/InternalStatusPage.tsx`, `frontend/src/navigation.tsx`

**原问题：** PRD2 P2 scope 要求 internal status page / incident history，但没有 API/UI。

**当前代码：**

- 新增 `GET /api/v1/internal-status`。
- 从 `notification_events` 聚合 open critical/warning，派生 `degraded/warning/operational`。
- 返回最近 20 条 incident history 和 maintenance window 占位。
- 支持 org/project scoped filtering；项目角色无 `project_id` 时拒绝。
- Admin/Billing navigation 暴露 `/status`，Developer 不暴露。
- UI 显示当前状态、critical/warning 数、维护窗口数量和事件历史表。

**验证方法：**

```bash
go test ./internal/admin
npm --prefix frontend test -- --run InternalStatusPage navigation client
```

---

## Fix #5: P2-F15 Cache Research Boundary

**文件：** `docs/tech-ref/17-cache-research.md`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`

**原问题：** User 确认 P2-F15 是 PRD2 必做项，但范围是 cache research spike，不是默认 runtime cache。

**当前文档：**

- 记录 exact cache 与 semantic cache 的可行性和风险。
- 覆盖 privacy、billing、audit、correctness、tenant isolation、invalidation/TTL、encryption、purge controls。
- 明确 PRD2 不默认启用 prompt/response cache。
- 建议未来只能做显式 opt-in、metadata-first、audit-on-hit、tenant-scoped 的设计。

**验证方法：**

```bash
rg -n "P2-F15|Cache Research|no runtime cache|opt-in" docs/tech-ref/17-cache-research.md docs/tech-ref/README.md docs/DOC-INVENTORY.md
```

---

## Fix #6: Batch 23 Smoke, Evidence, Status, Matrix, Issues

**文件：** `scripts/prd2-batch23-product-smoke.sh`, `scripts/prd2_batch23_product_seed.go`, `scripts/prd2_batch23_product_smoke.mjs`, `docs/ops/evidence/PRD2-B23-product-local-smoke-20260704.md`, status/matrix/issues/completion docs

**原问题：** Batch 23 产品面需要可重复真实后端/browser 验证和状态同步，不能只停留在单测或口头计划。

**当前代码/文档：**

- 新增 Batch 23 seed，幂等创建 platform admin、developer、project、approved model alias、mock OpenAI-compatible channel、analytics logs、warning incident。
- 新增 Batch 23 Playwright/API smoke，验证 error docs、invalid-key `doc_url`、usage analytics/CSV、internal status、Developer Console Playground、Admin product pages。
- 本地 current-code smoke 已 PASS，证据写入 `docs/ops/evidence/PRD2-B23-product-local-smoke-20260704.md`。
- 状态、矩阵、completion plan、open issue 需保持 `Implemented not accepted` / `Fixed awaiting Canary`，等待 Canary/Sentinel。

**验证方法：**

```bash
bash -n scripts/prd2-batch23-product-smoke.sh
node --check scripts/prd2_batch23_product_smoke.mjs
go run ./scripts/prd2_batch23_product_seed.go
```

`go run` seed 需要可连接 DB，且必须设置 `UNIROUTE_SMOKE_UPSTREAM_URL` 与运行中 backend 一致的 32-byte `ENCRYPTION_KEY` / `UNIROUTE_SMOKE_ENCRYPTION_KEY`。

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/errdocs ./internal/middleware ./internal/admin ./internal/gateway ./cmd/uniroute
npm --prefix frontend test -- --run navigation client DeveloperConsolePage UsageAnalyticsPage InternalStatusPage
bash -n scripts/prd2-batch23-product-smoke.sh
node --check scripts/prd2_batch23_product_smoke.mjs
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

### 可选真实后端/浏览器 smoke

在本地或用户批准的预发/目标环境启动 current-code backend/frontend 后运行：

```bash
set -a
source .env.production
set +a
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18083 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
scripts/prd2-batch23-product-smoke.sh
```

Codex 本地实际输出：

```json
{
  "status": "PASS",
  "batch": 23,
  "backend_url": "http://localhost:18083",
  "frontend_url": "http://localhost:3000",
  "org_id": 43,
  "project_id": 35,
  "model_alias": "batch23-company-chat",
  "provider": "batch23-mock",
  "upstream_calls": 1,
  "analytics_summary": {
    "total_requests": 4,
    "success_requests": 3,
    "failed_requests": 1,
    "total_tokens": 77,
    "cost_cents": 7,
    "estimated_tokens": 20,
    "upstream_tokens": 57,
    "estimated_ratio": 0.2597402597402597
  },
  "internal_status": "warning",
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch23-product.png"
}
```

---

## 期望输出格式

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 23 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 23 P2 Product Features

## 逐条结果

- Fix #1: PASS/FAIL — [error docs and doc_url result]
- Fix #2: PASS/FAIL — [Playground result]
- Fix #3: PASS/FAIL — [usage analytics result]
- Fix #4: PASS/FAIL — [internal status result]
- Fix #5: PASS/FAIL — [cache research boundary result]
- Fix #6: PASS/FAIL/SKIP — [smoke/evidence/status/matrix/issues result]

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/errdocs ./internal/middleware ./internal/admin ./internal/gateway ./cmd/uniroute` | PASS/FAIL | |
| `npm --prefix frontend test -- --run navigation client DeveloperConsolePage UsageAnalyticsPage InternalStatusPage` | PASS/FAIL | |
| `bash -n scripts/prd2-batch23-product-smoke.sh` | PASS/FAIL | |
| `node --check scripts/prd2_batch23_product_smoke.mjs` | PASS/FAIL | |
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | large chunk warning is existing/non-blocking unless new errors appear |
| `git diff --check` | PASS/FAIL | |
| Batch 23 real backend/browser smoke | PASS/FAIL/SKIP | 如果 SKIP，写明缺少 backend/frontend/DB/Redis/Playwright 哪一项 |

## 新发现

- [如无，写“无新发现”]

## 结论

PASS/FAIL。若 PASS 但未运行真实后端/browser smoke，请明确说明：本轮可交给 Sentinel，但 P2 product rows 只能在 Sentinel/merge 后再按验收纪律推进；Batch 18 real-provider 和 P2-F10 target-environment License 仍保持独立 open gap。
```
