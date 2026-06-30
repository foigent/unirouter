# FIX-SIGNAL.md — Bug #23~#28 + 前端动态模型列表

**修复者：** codex agent
**日期：** [见文件系统时间戳]
**修复轮次：** 第 14 轮

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 后端 Go 测试 | 559+ PASS | 仅修改 admin handler 和 mock 测试 |
| 前端编译 | PASS | 新增 API 方法 + 3 页面 Select 改造 |
| `go build ./...` | PASS | 新增 GetModelNames handler + route |

---

## Fix #23: ListLogs SELECT 列数不匹配 (High 后端)

**文件：** `internal/admin/handlers.go:822-830`

**原问题：** ListLogs SELECT 只查 10 列（id, request_id, model, provider, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, created_at），缺少 `channel`, `total_tokens`（重复列已删）, `ttft_ms`, `upstream_status_code`, `upstream_error`, `usage_source`, `is_stream`。前端 columns 定义 17 列，Scan 时类型对不上会 panic。

**修复内容：** SELECT 扩展为 17 列，Scan dest 对齐。同步修复两个 mock 测试（TestListLogs_Success、TestListLogs_WithFilters）的 scan 函数。

---

## Fix #24: TestChannel URL 双重 `/v1/` (High 后端)

**文件：** `internal/admin/handlers.go:1077-1082`

**原问题：** TestChannel 拼接 URL 时 `strings.TrimRight(baseURL, "/") + "/v1/chat/completions"`，如果用户输入 `https://api.openai.com/v1` 则变成 `https://api.openai.com/v1/v1/chat/completions`。

**修复内容：** 先 `strings.TrimRight(baseURL, "/")`，再 `strings.TrimSuffix(result, "/v1")`，确保不重复。

---

## Fix #25: TestChannel 成功响应缺少 success 字段 (Medium 后端)

**文件：** `internal/admin/handlers.go:1103-1105`

**原问题：** TestChannel 成功时只返回 `model`、`latency_ms`、`status_code`，前端依赖 `data.success` 判断是否成功。

**修复内容：** 响应体增加 `"success": true`。

---

## Fix #26: ModelPricesPage 模型名称手动输入 (Medium 前端)

**文件：** `frontend/src/pages/ModelPricesPage.tsx:17,27,133`

**原问题：** 新增/编辑价格时模型名为 `Form.Input`，用户需手动输入，容易拼错。

**修复内容：** 改为 `Form.Select` + `filter` 搜索，数据源从 `GET /model-names` 动态获取。新增 `modelNames` state 和 `api.getModelNames()` 调用。

---

## Fix #27: OrgModelPricesPage 模型名称手动输入 (Medium 前端)

**文件：** `frontend/src/pages/OrgModelPricesPage.tsx:27,45,153`

**原问题/修复：** 同 Fix #26，组织倍率页面同步改造为 `Form.Select` 动态下拉。

---

## Fix #28: ReconciliationPage 模型列表硬编码 (Medium 前端)

**文件：** `frontend/src/pages/ReconciliationPage.tsx:18,37-38,83`

**原问题：** 模型筛选下拉硬编码 `['gpt-4', 'gpt-3.5-turbo', 'claude-3-opus']`，与实际渠道配置不匹配。

**修复内容：** 替换为 `modelNames` state，从 `/model-names` API 动态加载。

---

## 附带修复

### channel_keys key_prefix 字段 (High 后端)
- `handlers.go:1204` — `req.KeyPrefix` → `req.Prefix`
- `handlers.go:1218` — return 缺失 → 添加 `return`

### GetModelNames 新端点 (Enhancement 后端+前端)
- `handlers.go:1632-1643` — 新增 `GetModelNames` handler，查询 `SELECT DISTINCT model_name FROM model_prices ORDER BY model_name`
- `routes.go:23` — 注册 `GET /model-names`
- `client.ts:203-204` — 新增 `getModelNames()` API 方法

---

**验证方法：**
```bash
cd /Users/m4/Desktop/projects/UniRouter
go build ./...           # 期望：无输出
go test ./... -count=1   # 期望：16/16 packages OK
```

---

**请 Canary 验收：** `docs/agent-workflows/FIX-SIGNAL.md`
