# FIX-SIGNAL.md — PRD2 Batch 13 Anthropic Native Stream Translation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 13

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 12 | Canary 5/5 PASS; Sentinel merged; Atlas completed per User report | `VERIFY-RESULT.md` 已记录 Batch 12 PASS；`main` 当前提交为 `24e0abf`；本轮在此基础上继续 |
| 下游协议 | 仍纯 OpenAI-compatible | 企业开发者继续调用 UniRouter `/v1/*`，provider native 差异由上游 adapter 吸收 |
| Anthropic embeddings | 未实现 | Anthropic Messages API proof 只覆盖 chat completions |
| 真实 Anthropic upstream E2E | 未执行 | 本轮使用 mock upstream，不依赖外部 provider key |
| Anthropic tool/vision/advanced content blocks | 未实现 | Batch 13 只覆盖文本 chat stream proof |
| PRD2 Low hardening follow-ups | 仍 Open | `PRD2-MIG-001/002`, `PRD2-GOV-001`, `PRD2-AUDIT-001` 不混入 Batch 13 |

> Canary 验收时：上述未完成项不算 FAIL。若本轮声称完成 embeddings、真实 Anthropic E2E、tool/vision content blocks 或生产验证，应标为 FAIL。

---

## Fix #1: Adapter stream-normalization interface

**文件：** `internal/adapters/types.go`

**当前实现：**

- 新增可选接口 `StreamNormalizer`。
- 新增 `StreamState`、`StreamChunkInput`、`NormalizedStreamChunk`。
- 现有 `Adapter` 接口不强制所有 adapter 实现 stream translation；`openai_compatible` 不受影响。
- Gateway 只在 adapter 实现 `StreamNormalizer` 时进入 native stream normalization 分支。

**建议验证：**

```bash
go test ./internal/adapters -run 'TestAnthropicNative' -v
```

---

## Fix #2: `anthropic_native` supports `stream:true`

**文件：** `internal/adapters/anthropic_native.go`, `internal/adapters/anthropic_native_test.go`

**当前实现：**

- `BuildRequest()` 不再拒绝 `stream:true`。
- 下游 OpenAI-compatible `stream:true` 转换为 Anthropic `/v1/messages` body 中的 `"stream": true`。
- headers 仍使用 `x-api-key`、`anthropic-version`、可选 `anthropic-beta`。
- embeddings 仍明确不支持。
- 新增测试验证 stream flag 被写入 Anthropic request body。

**建议验证：**

```bash
go test ./internal/adapters -run 'TestAnthropicNativeBuildRequest_ConvertsStreamFlag|TestAnthropicNativeBuildRequest_RejectsUnsupportedEndpoints' -v
```

---

## Fix #3: Anthropic SSE translates to OpenAI-compatible stream chunks

**文件：** `internal/adapters/anthropic_native.go`, `internal/adapters/anthropic_native_test.go`

**当前实现：**

- `message_start` 转换为 OpenAI-compatible role chunk。
- `content_block_delta` 转换为 OpenAI-compatible content delta chunk。
- `message_delta.stop_reason` 转换为 OpenAI-compatible finish chunk。
- `message_stop` 结束 upstream stream，但不向下游透出 Anthropic-native event。
- `ping` 被跳过。
- `error` 转换为 downstream error data chunk。
- Anthropic `message_start.usage.input_tokens` 与 `message_delta.usage.output_tokens` 合并为 UniRouter upstream usage。
- 如果 Anthropic stream 没有完整 completion usage，content delta 保留给现有 stream counter fallback。

**建议验证：**

```bash
go test ./internal/adapters -run 'TestAnthropicNativeNormalizeStreamChunk' -v
```

---

## Fix #4: Gateway native stream relay uses adapter translation

**文件：** `internal/gateway/relay.go`, `internal/gateway/relay_mock_test.go`

**当前实现：**

- `HandleChatCompletion()` 在 `stream:true` 时把选中的 adapter 传入 `relayStreamRequest()`。
- `relayStreamRequest()` 对实现 `StreamNormalizer` 的 adapter：
  - 跳过 Anthropic `event:`、空行和 comment line。
  - 只处理 `data:` payload。
  - 输出 OpenAI-compatible `data: {...}\n\n` SSE chunk。
  - 继续复用现有 balance check、token counter、billing settle、call log、budget alert、final `_uniroute` 和 `[DONE]` 逻辑。
- 没有实现 `StreamNormalizer` 的 adapter 继续走原 OpenAI-compatible SSE 透传/解析逻辑。
- 新增 handler-level 测试验证：
  - 下游仍 POST `/v1/chat/completions` with `stream:true`。
  - 上游 mock 接收 Anthropic `/v1/messages` 且 body 中 `stream=true`。
  - 上游使用 `x-api-key`。
  - Anthropic SSE event type 不泄漏给下游。
  - 下游收到 OpenAI-compatible `chat.completion.chunk`。
  - billing settle 使用 Anthropic upstream stream usage。
  - `_uniroute` 在唯一 `[DONE]` 前发送。

**建议验证：**

```bash
go test ./internal/gateway -run 'TestHandleChatCompletion_AnthropicNativeStream|TestRelayStreamRequest' -v
```

---

## Fix #5: PRD2 docs/matrix updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/tech-ref/10-provider-adapter-framework.md`, `docs/agent-workflows/FIX-SIGNAL.md`

**当前实现：**

- `CURRENT-STATUS.md` 标记 Batch 13 fixed locally awaiting Canary。
- Batch 12 状态更新为 Canary PASS、Sentinel merge reflected in `main`、Atlas completed per User report。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 Batch 13 native stream 工作写为本地证据，不标 E2E/Production。
- Adapter tech-ref 记录 Anthropic native non-stream + stream proof，并保留 embeddings、real upstream、tool/vision 边界。

**建议验证：**

```bash
rg -n 'Batch 12|Batch 13|anthropic_native|Anthropic Native|native stream|stream:true|real upstream' docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/tech-ref/10-provider-adapter-framework.md docs/agent-workflows/FIX-SIGNAL.md
```

---

## 本地已执行并通过

```bash
go test ./internal/adapters -run 'TestAnthropicNative' -count=1
go test ./internal/gateway -run 'TestRelayStreamRequest|TestHandleChatCompletion_AnthropicNative' -count=1
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
```

`npm --prefix frontend test` 仍显示既有 React Router/Semi UI jsdom warning；测试退出码为 0。`npm --prefix frontend run build` 仍显示既有 Vite large chunk warning；构建退出码为 0。

---

## Canary 全局验证建议

请从仓库根目录执行：

```bash
go test ./internal/adapters -run 'TestAnthropicNative' -v
go test ./internal/gateway -run 'TestRelayStreamRequest|TestHandleChatCompletion_AnthropicNative' -v
go test ./internal/admin ./internal/adapters ./internal/gateway
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
rg -n 'Batch 12|Batch 13|anthropic_native|Anthropic Native|native stream|stream:true|real upstream' docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/tech-ref/10-provider-adapter-framework.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

Browser smoke is not required for this backend stream batch. If a browser-capable role wants extra UI coverage, Batch 12 channel adapter UI smoke remains useful but is not a Batch 13 gate.

---

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 13 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 13 Anthropic Native Stream Translation

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/adapters -run 'TestAnthropicNative' -v` | PASS/FAIL | |
| `go test ./internal/gateway -run 'TestRelayStreamRequest|TestHandleChatCompletion_AnthropicNative' -v` | PASS/FAIL | |
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

PASS/FAIL. 明确说明 Batch 13 是否可交给 Sentinel，以及是否存在 Batch 14 blocker。
```
