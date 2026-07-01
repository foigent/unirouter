# FIX-SIGNAL.md — PRD1 API Key and Channel Test Fix 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 API key creation, duplicate-name, copy fallback, and channel test semantics

---

## 背景

用户在 PRD1 验收准备中发现三类问题：

- 创建 API key 后虽然弹出完整 key，但点击复制在 HTTP 云端页面失败。
- 新建 API key 可以使用重复名称，没有明确提示。
- 渠道测试选择 `Mimo Token Plan` / `mimo-v2.5` 后，结果显示“成功”，但上游状态码是 401。

代码核查结论：

- 浏览器 `navigator.clipboard.writeText()` 在非 secure context 下可能失败，需要 fallback。
- 后端 `api_keys` 只对 `key_prefix` 有唯一约束，未校验同 org 下 name 重复。
- 渠道测试接口只要收到上游 HTTP 响应就设置 `success: true`，且旧逻辑固定测 `/v1/models`，没有使用前端选择的 model/prompt。

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

## Codex 已执行验证

```bash
go test ./internal/admin -run 'TestCreateAPIKey|TestTestChannel' -v
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
go test ./internal/admin -run 'TestCreateAPIKey|TestTestChannel' -v
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
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 API Key and Channel Test Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 API key creation, duplicate-name, copy fallback, and channel test semantics

## 逐条结果

- Fix #1: PASS/FAIL — API key 完整密钥复制是否有 HTTP fallback
- Fix #2: PASS/FAIL — API key 重名是否返回 409 且前端显示后端错误
- Fix #3: PASS/FAIL — 渠道测试是否用所选 model/prompt 调 chat completions，且 401 不算成功

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
