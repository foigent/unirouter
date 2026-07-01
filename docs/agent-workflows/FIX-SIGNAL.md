# FIX-SIGNAL.md — PRD1 UI/API Follow-up Fix 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 channel-key edit, token reconciliation pagination, and Dashboard trend fix

---

## 本轮背景

用户在云端 PRD1 继续验收时报告三类问题：

- `上游密钥 -> 渠道密钥管理 -> 编辑渠道密钥` 中，“所属渠道”下拉无法点击，且需要更新 Mimo key。
- `Token 对账` 每页只显示一条数据。
- `仪表盘 -> AI 活动趋势` 图表是否正确存在疑问。

代码与云端接口核查结论：

- 前端编辑渠道密钥时禁用了 `所属渠道` 下拉；后端 `UpdateChannelKey` 也没有接收 `key` 字段，所以用户即使输入新 key 也不会真正更新上游密钥。
- `UpdateChannelKey` 更新 key 后应清理 `fail_count/cooldown_until/last_used_at`，否则 Mimo key 即使替换后仍可能被冷却状态挡住。
- `ListReconciliation` 把 `created_at` timestamp 扫到 `string`，并忽略 `rows.Scan` 错误；云端 `total=145` 但任意 `page_size` 都只返回 1 行。
- `ReconciliationPage` 请求里把 `page_size` 写死为 `20`，并使用了后端不支持的 `pending` 状态筛选。
- Dashboard `total_requests` 查询没有使用所选时间范围，导致 24h KPI/趋势图口径不一致；趋势图也没有补零 bucket，24h/7d/30d 在稀疏数据下很容易看起来“不变”。

---

## Fix #1: 渠道密钥编辑可更新所属渠道和上游 key

**文件：**

- `frontend/src/pages/ChannelKeysPage.tsx`
- `internal/admin/channel_key_handlers.go`
- `internal/admin/admin_mock_test.go`

**修复内容：**

- 编辑渠道密钥时不再禁用 `所属渠道` 下拉。
- 编辑表单不回显旧 key，提示用户输入新的完整上游 API key。
- 后端 `UpdateChannelKey` 支持 `key` 字段，更新时重新加密写入 `key_enc`。
- 更新 key 时自动重置 `fail_count=0`、`cooldown_until=NULL`、`last_used_at=NULL`。
- 后端同时补齐 `allowed_models` 更新路径和 `channelkey.update` audit。
- 增加 `TestUpdateChannelKey_KeyResetsCooldown`。

---

## Fix #2: Token 对账分页返回多行并使用真实 pageSize

**文件：**

- `internal/admin/model_handlers.go`
- `internal/admin/admin_mock_test.go`
- `frontend/src/pages/ReconciliationPage.tsx`

**修复内容：**

- `ListReconciliation` 将 `created_at` 扫描为 `time.Time`，再格式化为 RFC3339 字符串。
- `ListReconciliation` 不再忽略 count/row scan/rows errors。
- 前端请求使用当前 `pageSize`，不再写死 `page_size=20`。
- 前端分页使用 `onPageChange`，pageSize 改变时重新拉取。
- 状态筛选改为后端真实支持的 `reconciled` / `error`。
- `TestListReconciliation_Success` 改为覆盖 2 行 timestamp 结果。

---

## Fix #3: Dashboard 活动趋势与时间范围口径一致

**文件：**

- `internal/admin/dashboard_handlers.go`
- `internal/admin/admin_mock_test.go`

**修复内容：**

- `dashboardOverview.total_requests` 改为按 `start_time/end_time` 查询，不再返回全量历史。
- `dashboardDailyTrend` 使用 `time.RFC3339Nano` 解析前端 ISO 时间。
- 趋势图按时间范围补零 bucket：短范围按小时，长范围按天。
- bucket 统一 UTC 格式化，避免数据库时区导致 bucket 对不上。
- 增加 `TestDashboardOverview_TotalUsesTimeRange`。

---

## Codex 已执行验证

```bash
go test ./internal/admin -run 'TestUpdateChannelKey_KeyResetsCooldown|TestListReconciliation|TestDashboardOverview|TestDashboardStats' -v
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
git diff --check
```

结果：

- targeted admin tests: PASS
- `go test ./...`: PASS
- `npm run build`: PASS，保留既有 Vite large chunk warning
- `npm test -- --run`: PASS，36/36 tests passed
- `git diff --check`: PASS

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./internal/admin -run 'TestUpdateChannelKey_KeyResetsCooldown|TestListReconciliation|TestDashboardOverview|TestDashboardStats' -v
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
```

Source-level checks:

```bash
rg -n "Key \\*string|key_enc =|cooldown_until = NULL|last_used_at = NULL|TestUpdateChannelKey_KeyResetsCooldown" internal/admin/channel_key_handlers.go internal/admin/admin_mock_test.go
rg -n "createdAt time.Time|rows.Scan|rows.Err|page_size: String\\(pageSize\\)|onPageChange|value: 'error'" internal/admin/model_handlers.go frontend/src/pages/ReconciliationPage.tsx
rg -n "COUNT\\(\\*\\).*created_at >= \\$2|RFC3339Nano|Truncate\\(time.Hour\\)|TestDashboardOverview_TotalUsesTimeRange" internal/admin/dashboard_handlers.go internal/admin/admin_mock_test.go
rg -n "optionList=\\{channels\\}|输入新的上游 API Key|已有密钥不会回显" frontend/src/pages/ChannelKeysPage.tsx
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 UI/API Follow-up Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 channel-key edit, token reconciliation pagination, and Dashboard trend fix

## 逐条结果

- Fix #1: PASS/FAIL — 渠道密钥编辑是否可改渠道和 key，且后端更新 key 会清理冷却状态
- Fix #2: PASS/FAIL — Token 对账是否按 page/page_size 返回多行，并修正前端分页和状态筛选
- Fix #3: PASS/FAIL — Dashboard total_requests 与趋势图是否使用同一时间范围，趋势 bucket 是否补零

## 全局验证

- `git diff --check`: PASS/FAIL
- targeted admin tests: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm run build`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```

---

## 历史上一轮背景

## 背景

用户在 PRD1 验收准备中发现三类问题：

- 创建 API key 后虽然弹出完整 key，但点击复制在 HTTP 云端页面失败。
- 新建 API key 可以使用重复名称，没有明确提示。
- 渠道测试选择 `Mimo Token Plan` / `mimo-v2.5` 后，结果显示“成功”，但上游状态码是 401。
- 真实网关非流式调用 `mimo-v2.5` 返回 502，最后上游状态 404。云端渠道 `Mimo Token Plan` 的 `base_url` 是 `https://token-plan-cn.xiaomimimo.com/v1`，而 relay 无条件再拼 `/v1/chat/completions`，会形成 `/v1/v1/chat/completions`。
- 真实流式调用 `moonshot-v1-8k` 返回 200，但部署版本先转发 `[DONE]`，再追加成本/渠道元数据，且该元数据没有 `_uniroute` 包装；PRD1 要求最后 chunk 含 `usage` 和 `_uniroute`。

代码核查结论：

- 浏览器 `navigator.clipboard.writeText()` 在非 secure context 下可能失败，需要 fallback。
- 后端 `api_keys` 只对 `key_prefix` 有唯一约束，未校验同 org 下 name 重复。
- 渠道测试接口只要收到上游 HTTP 响应就设置 `success: true`，且旧逻辑固定测 `/v1/models`，没有使用前端选择的 model/prompt。
- gateway relay 对 `base_url` 已包含 `/v1` 的渠道没有做路径归一化。
- gateway stream relay 直接转发上游 `[DONE]`，导致 UniRouter final metadata 在 `[DONE]` 之后，OpenAI-compatible 客户端可能停止读取。

---

## Fix #1: API key 完整密钥复制 fallback

**文件：**

- `frontend/src/pages/KeysPage.tsx`

**修复内容：**

- 保留 secure context 下的 `navigator.clipboard.writeText()`。
- 非 secure context 或 Clipboard API 不可用时，使用临时 `textarea + document.execCommand('copy')` fallback。
- 复制失败时仍保留手动复制提示。

---

## Fix #2: API key 重名校验

**文件：**

- `internal/admin/key_handlers.go`
- `internal/admin/admin_mock_test.go`
- `frontend/src/pages/KeysPage.tsx`

**修复内容：**

- 创建 API key 前检查同 `org_id + name` 是否已存在。
- 重名时返回 HTTP 409 和 `API key name already exists`。
- 前端创建/更新失败 Toast 展示后端错误信息，不再只显示泛化“创建失败”。
- 增加 `TestCreateAPIKey_DuplicateName`。

---

## Fix #3: 渠道测试 401 不再算成功，并真正使用所选模型

**文件：**

- `internal/admin/test_handlers.go`
- `internal/admin/admin_mock_test.go`

**修复内容：**

- 渠道测试从固定 `GET /v1/models` 改为 `POST /v1/chat/completions`。
- 使用前端提交的 `model` 和 `prompt` 生成 OpenAI-compatible 测试 payload。
- 只有上游 HTTP 2xx 才返回 `success: true`。
- 401/403/404/5xx 等上游响应返回 `success: false`，并带 `status_code` 和错误信息。
- 解析 `usage.total_tokens` 作为 `tokens_used`。
- 增加 `TestTestChannel_Upstream401IsFailureAndUsesSelectedModel`，覆盖 401 必须失败且模型为用户选择值。

---

## Fix #4: Gateway 上游 URL 不再双拼 `/v1`

**文件：**

- `internal/gateway/relay.go`
- `internal/gateway/relay_mock_test.go`

**修复内容：**

- 新增 `upstreamEndpoint(baseURL, endpoint)` 统一拼接上游 URL。
- 当渠道 `base_url` 已以 `/v1` 结尾，endpoint 也以 `/v1/` 开头时，自动去掉 endpoint 的重复 `v1/`。
- `HandleChatCompletion` 和 `HandleEmbeddings` 都使用该 helper。
- 增加 `TestUpstreamEndpoint_DoesNotDoubleAppendV1`，覆盖 base_url 带 `/v1`、不带 `/v1`、带 trailing slash 的情况。

---

## Fix #5: Streaming final chunk 顺序和 `_uniroute` 包装

**文件：**

- `internal/gateway/relay.go`
- `internal/gateway/relay_mock_test.go`

**修复内容：**

- `relayStreamRequest` 不再直接转发上游 `data: [DONE]`。
- 结算完成后发送一个 final data chunk，包含：
  - `usage.prompt_tokens`
  - `usage.completion_tokens`
  - `usage.total_tokens`
  - `_uniroute.request_id`
  - `_uniroute.cost`
  - `_uniroute.provider`
  - `_uniroute.channel`
- 最后只发送一个 terminal `data: [DONE]`。
- 更新流式单测，明确校验 `_uniroute` 在 `[DONE]` 前，且 `[DONE]` 只出现一次。

---

## Codex 已执行验证

```bash
go test ./internal/admin -run 'TestCreateAPIKey|TestTestChannel' -v
go test ./internal/gateway -run 'TestRelayStreamRequest|TestUpstreamEndpoint|TestHandleChatCompletion' -v
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
git diff --check
```

结果：

- targeted admin tests: PASS
- targeted gateway tests including stream final chunk: PASS
- `go test ./...`: PASS
- `npm run build`: PASS，保留既有 Vite large chunk warning
- `npm test -- --run`: PASS，36/36 tests passed
- `git diff --check`: PASS

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./internal/admin -run 'TestCreateAPIKey|TestTestChannel' -v
go test ./internal/gateway -run 'TestRelayStreamRequest|TestUpstreamEndpoint|TestHandleChatCompletion' -v
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
```

Source-level checks:

```bash
rg -n "fallbackCopyText|document.execCommand\\('copy'\\)|window.isSecureContext" frontend/src/pages/KeysPage.tsx
rg -n "API key name already exists|SELECT 1 FROM api_keys WHERE org_id" internal/admin/key_handlers.go
rg -n "TestCreateAPIKey_DuplicateName|TestTestChannel_Upstream401IsFailureAndUsesSelectedModel" internal/admin/admin_mock_test.go
rg -n "chat/completions|success := resp.StatusCode >= 200|tokens_used|upstream returned HTTP" internal/admin/test_handlers.go
rg -n "func upstreamEndpoint|upstreamEndpoint\\(route.Channel.BaseURL|TestUpstreamEndpoint_DoesNotDoubleAppendV1" internal/gateway/relay.go internal/gateway/relay_mock_test.go
rg -n "data: \\[DONE\\]|\\\"_uniroute\\\"|TestRelayStreamRequest_UnirouteExtension" internal/gateway/relay.go internal/gateway/relay_mock_test.go
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 API Key and Channel Test Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 API key, channel test, upstream URL, and stream final chunk fix

## 逐条结果

- Fix #1: PASS/FAIL — API key 完整密钥复制是否有 HTTP fallback
- Fix #2: PASS/FAIL — API key 重名是否返回 409 且前端显示后端错误
- Fix #3: PASS/FAIL — 渠道测试是否用所选 model/prompt 调 chat completions，且 401 不算成功
- Fix #4: PASS/FAIL — gateway relay 是否避免对 `/v1` base_url 双拼 `/v1`
- Fix #5: PASS/FAIL — stream final chunk 是否在 `[DONE]` 前包含 `usage` 和 `_uniroute`，且只发送一个 `[DONE]`

## 全局验证

- `git diff --check`: PASS/FAIL
- targeted admin tests: PASS/FAIL
- targeted gateway tests including stream final chunk: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm run build`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
