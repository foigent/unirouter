# FIX-SIGNAL.md — PRD2 Batch 12 Anthropic Native Adapter Proof

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 12

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 1-11 | Canary/Sentinel 已完成 | User 已报告 Batch 11 Canary 和 Sentinel 完成 |
| Atlas 部署 | 未执行 | 本轮是本地 native adapter proof，不触发部署 |
| 下游协议 | 仍纯 OpenAI-compatible | PRD2 决策是企业开发者只调用 UniRouter `/v1/*`，provider native 差异由上游 adapter 吸收 |
| Anthropic native streaming | 未实现 | 当前 relay 流式路径仍是 OpenAI SSE 透传/解析，native SSE 翻译应单独做后续 batch |
| Anthropic embeddings | 未实现 | Anthropic Messages API proof 只覆盖 chat completions non-stream |
| 真实 Anthropic upstream E2E | 未执行 | 本轮使用 mock upstream，不依赖外部 provider key |
| PRD2 Low hardening follow-ups | 仍 Open | `PRD2-MIG-001/002`, `PRD2-GOV-001`, `PRD2-AUDIT-001` 不混入 Batch 12 |

> Canary 验收时：上述未完成项不算 FAIL。若本轮声称完成 native stream、embeddings、真实 Anthropic E2E 或部署，应标为 FAIL。

---

## Fix #1: `anthropic_native` adapter registered and normalized

**文件：** `internal/adapters/types.go`, `internal/adapters/registry.go`, `internal/adapters/anthropic_native.go`, `internal/adapters/anthropic_native_test.go`

**原问题：** PRD2 P2-F05 已有 adapter framework，但默认 registry 只有 `openai_compatible`，无法证明 UniRouter 能像 oneapi 一样通过独立 adapter 接入非 OpenAI-native provider，同时保持下游 OpenAI-compatible API 不变。

**当前实现：**

- 新增 `TypeAnthropicNative = "anthropic_native"`。
- `DefaultRegistry()` 注册 `NewAnthropicNative()`。
- `BuildRequest()` 将下游 OpenAI-compatible chat body 转换为 Anthropic `/v1/messages` body：
  - `model` 使用 `model_mapping` 后的 `MappedModel`。
  - `system` messages 合并到 Anthropic `system` 字段。
  - `user/assistant` messages 转换到 Anthropic messages。
  - `max_tokens` 使用请求值、channel default，或 1024 fallback。
  - headers 使用 `x-api-key`、`anthropic-version`、可选 `anthropic-beta`。
- `ParseResponse()` 将 Anthropic native response 转回 OpenAI-compatible `chat.completion` body。
- Anthropic `input_tokens/output_tokens` 映射为 `prompt_tokens/completion_tokens`，`usage_source=upstream`。
- `ClassifyHTTPStatus()` 支持 Anthropic error body，并把 400/401/429/5xx/529 映射到统一 `FailureKind`。
- `stream:true` 和 embeddings 明确返回 unsupported error，避免错误透传 native SSE。

**建议验证：**

```bash
go test ./internal/adapters -run 'TestAnthropicNative|TestDefaultRegistryIncludesAnthropicNative|TestOpenAICompatible' -v
```

---

## Fix #2: Relay uses adapter-normalized body and failure classification

**文件：** `internal/gateway/relay.go`, `internal/gateway/relay_mock_test.go`

**原问题：**

1. Relay 非流式成功响应注入 `_uniroute` 时使用原始 upstream body。对 native adapter 来说会把 Anthropic 原生响应泄漏给下游。
2. Relay 非 200 分类写死在 gateway 层，provider-specific native HTTP 语义无法由 adapter 控制。

**当前实现：**

- 非流式成功响应优先使用 `normalized.Body` 注入 `_uniroute`。
- 非 200 响应通过 `adapter.ClassifyHTTPStatus()` 得到 `FailureKind`：
  - `FailureChannel` 标记 channel/key failure。
  - `FailureAuth` 标记 key cooldown 3600s。
  - `FailureRateLimit` 标记 key cooldown 60s。
  - client request errors 不触发 channel failure。
- 新增 gateway E2E-style 单测：
  - 下游仍 POST `/v1/chat/completions` OpenAI-compatible body。
  - 上游 mock 接收 Anthropic `/v1/messages`。
  - 验证 `x-api-key` 使用解密后的 channel key。
  - 验证 model_mapping 到 Claude upstream model。
  - 验证 system prompt 转换。
  - 验证 billing settle 使用 Anthropic upstream usage。
  - 验证下游响应是 OpenAI-compatible body 并包含 `_uniroute`。

**建议验证：**

```bash
go test ./internal/gateway -run 'TestHandleChatCompletion_AnthropicNativeNonStream|TestHandleChatCompletion_Success_NonStream|TestHandleChatCompletion_UpstreamError_AllFail' -v
```

---

## Fix #3: Admin channel create/test supports `anthropic_native`

**文件：** `internal/admin/channel_handlers.go`, `internal/admin/channel_adapter_test.go`, `internal/admin/test_handlers.go`, `internal/admin/admin_mock_test.go`

**当前实现：**

- Existing channel create/update validation now accepts `anthropic_native` through `DefaultRegistry().Has()`.
- Added test proving `CreateChannel` persists `adapter_type=anthropic_native` and JSON `adapter_config`.
- Channel test now uses the selected adapter:
  - Anthropic native channel test calls `/v1/messages`.
  - Response is normalized to OpenAI-compatible body before returning in `response`.
  - `tokens_used` comes from Anthropic native usage.
  - `curl_example` uses `x-api-key` and `anthropic-version`, not OpenAI Bearer header.

**建议验证：**

```bash
go test ./internal/admin -run 'TestCreateChannel_AcceptsAnthropicNativeAdapterType|TestCreateChannel_InvalidAdapterType|TestUpdateChannel_InvalidAdapterType|TestTestChannel_AnthropicNativeUsesMessagesEndpoint|TestTestChannel_Upstream401IsFailureAndUsesSelectedModel' -v
```

---

## Fix #4: Channel admin UI exposes adapter type/config

**文件：** `frontend/src/pages/ChannelsPage.tsx`

**原问题：** 后端已经有 `adapter_type/adapter_config`，但渠道管理 UI 没有配置入口。新增 native adapter 后，如果 UI 不补齐，管理员只能靠 API 手工创建 Anthropic native channel。

**当前实现：**

- Channel table 增加 Adapter 列。
- Channel create/edit drawer 增加 `Adapter Type` select：
  - `openai_compatible`
  - `anthropic_native`
- 增加 `Adapter Config (JSON)` textarea。
- 提交前解析 JSON，非法 JSON 直接 Toast 错误，不发送请求。
- 编辑时兼容 object/string 两种 `adapter_config` 返回形态，避免双重转义。

**建议验证：**

```bash
npm --prefix frontend run build
```

---

## Fix #5: PRD2 adapter docs and matrix updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/tech-ref/10-provider-adapter-framework.md`, `docs/agent-workflows/FIX-SIGNAL.md`

**当前实现：**

- `CURRENT-STATUS.md` 标记 Batch 12 fixed locally awaiting Canary。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 Native adapter proof 从 `Planned` 推进到 `Implemented not accepted`，保留 Canary、native stream、real upstream E2E 缺口。
- `P2-F05` 和 adapter output rows 增加 Batch 12 本地证据，但不标 E2E/Production。
- Adapter tech-ref 记录 Batch 12 当前实现能力和明确边界。

**建议验证：**

```bash
rg -n "Batch 12|anthropic_native|Anthropic Native|Native adapter proof|native stream" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/tech-ref/10-provider-adapter-framework.md docs/agent-workflows/FIX-SIGNAL.md
```

---

## 本地已执行并通过

```bash
go test ./internal/adapters -run 'TestAnthropicNative|TestDefaultRegistryIncludesAnthropicNative|TestOpenAICompatible' -v
go test ./internal/gateway -run 'TestHandleChatCompletion_AnthropicNativeNonStream|TestHandleChatCompletion_Success_NonStream|TestHandleChatCompletion_UpstreamError_AllFail' -v
go test ./internal/admin -run 'TestCreateChannel_AcceptsAnthropicNativeAdapterType|TestCreateChannel_InvalidAdapterType|TestUpdateChannel_InvalidAdapterType|TestTestChannel_AnthropicNativeUsesMessagesEndpoint|TestTestChannel_Upstream401IsFailureAndUsesSelectedModel' -v
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
rg -n "Batch 12|anthropic_native|Anthropic Native|Native adapter proof|native stream" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/tech-ref/10-provider-adapter-framework.md
git diff --check
```

`npm --prefix frontend test` 仍显示既有 React Router/Semi UI jsdom warning；测试退出码为 0。`npm --prefix frontend run build` 仍显示既有 Vite large chunk warning；构建退出码为 0。

---

## Canary 全局验证建议

请从仓库根目录执行：

```bash
go test ./internal/adapters -run 'TestAnthropicNative|TestDefaultRegistryIncludesAnthropicNative|TestOpenAICompatible' -v
go test ./internal/gateway -run 'TestHandleChatCompletion_AnthropicNativeNonStream|TestHandleChatCompletion_Success_NonStream|TestHandleChatCompletion_UpstreamError_AllFail' -v
go test ./internal/admin -run 'TestCreateChannel_AcceptsAnthropicNativeAdapterType|TestCreateChannel_InvalidAdapterType|TestUpdateChannel_InvalidAdapterType|TestTestChannel_AnthropicNativeUsesMessagesEndpoint|TestTestChannel_Upstream401IsFailureAndUsesSelectedModel' -v
go test ./internal/admin ./internal/adapters ./internal/gateway
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
rg -n "Batch 12|anthropic_native|Anthropic Native|Native adapter proof|native stream" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/tech-ref/10-provider-adapter-framework.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

如具备浏览器自动化能力，可补充：

```bash
npm --prefix frontend run dev
# Browser smoke:
# 1. org_admin/platform_admin opens /channels.
# 2. Create/edit drawer shows Adapter Type select.
# 3. Adapter Type includes Anthropic Native.
# 4. Adapter Config rejects invalid JSON before submit.
# 5. Adapter column is visible in the channel table.
```

---

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 12 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 12 Anthropic Native Adapter Proof

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/adapters -run 'TestAnthropicNative|TestDefaultRegistryIncludesAnthropicNative|TestOpenAICompatible' -v` | PASS/FAIL | |
| `go test ./internal/gateway -run 'TestHandleChatCompletion_AnthropicNativeNonStream|TestHandleChatCompletion_Success_NonStream|TestHandleChatCompletion_UpstreamError_AllFail' -v` | PASS/FAIL | |
| `go test ./internal/admin -run 'TestCreateChannel_AcceptsAnthropicNativeAdapterType|TestCreateChannel_InvalidAdapterType|TestUpdateChannel_InvalidAdapterType|TestTestChannel_AnthropicNativeUsesMessagesEndpoint|TestTestChannel_Upstream401IsFailureAndUsesSelectedModel' -v` | PASS/FAIL | |
| `go test ./internal/admin ./internal/adapters ./internal/gateway` | PASS/FAIL | |
| `go test ./... -count=1` | PASS/FAIL | |
| `npm --prefix frontend test` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | note large chunk warning if present |
| docs grep command | PASS/FAIL | |
| `git diff --check` | PASS/FAIL | |
| `git diff --check -- $(git ls-files --others --exclude-standard)` | PASS/FAIL | |

## 浏览器烟测（如执行）

- PASS/FAIL/SKIP — evidence

## 新发现

- None / list issue paths.

## 结论

PASS/FAIL. 明确说明 Batch 12 是否可交给 Sentinel，以及是否存在 Batch 13 blocker。
```
