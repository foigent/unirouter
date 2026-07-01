# FIX-SIGNAL.md — PRD2 implementation batch 4 Provider Adapter backend foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 4

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 3 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步状态并推进 batch 4 |
| Frontend | 预期无功能变化 | 本轮只做 Provider Adapter 后端基础，不做 Developer Console UI |
| Native adapter proof | 仍未完成 | 本轮只提供 `openai_compatible` adapter 底座 |
| Stream response normalization | 仍未完整迁移 | 本轮 stream 请求构造经过 adapter，响应流仍沿用现有 relay 逻辑 |
| Token Metering V2 / reconciliation UI | 仍未完成 | 本轮只让非流式成功响应通过 adapter 归一化 usage/meta |
| 真实上游 E2E / 浏览器验收 | 仍未完成 | 本轮提供 schema、adapter、gateway/admin 接入和单元测试证据 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成 native provider adapter、完整 stream 归一化、Token Metering V2、Developer Console UI 或生产/E2E 验收，则应标为 FAIL。

---

## Fix #1: Batch 3 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** User 报告 batch 3 的 Canary 和 Sentinel 已完成，但当前状态需要同步到 PRD2 状态入口。

**当前状态：**

- `CURRENT-STATUS.md` 记录 batch 3 为 Canary passed / Sentinel complete。
- `PRD2-ACCEPTANCE-MATRIX.md` 保留 Model Governance 为单测支撑状态，未提升为 E2E。
- 当前活跃轮次更新为 batch 4 fixed locally awaiting Canary。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '42,70p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: Channel adapter schema and admin metadata

**文件：** `migrations/000011_prd2_provider_adapter_foundation.up.sql`, `migrations/000011_prd2_provider_adapter_foundation.down.sql`, `internal/models/models.go`, `internal/admin/channel_handlers.go`

**原问题：** PRD2 P2-F05 要求 provider adapter framework，但 channel 仍只有 `base_url/models/model_mapping`，无法标识 adapter 类型或保存 adapter 配置。

**当前实现：**

- 新增 `channels.adapter_type`，默认 `openai_compatible`。
- 新增 `channels.adapter_config`，默认 `{}`。
- `models.Channel` 增加 `AdapterType` 和 `AdapterConfig`。
- channel list/get/create/update 读写 adapter metadata。
- channel create/update 对未知 `adapter_type` 返回 400。

**关键测试：**

- `TestCreateChannel_DefaultsAdapterType`
- `TestCreateChannel_InvalidAdapterType`
- `TestUpdateChannel_InvalidAdapterType`

**验证方法：**

```bash
go test ./internal/admin -run 'Test(CreateChannel_DefaultsAdapterType|CreateChannel_InvalidAdapterType|UpdateChannel_InvalidAdapterType)' -v
rg -n "adapter_type|adapter_config" migrations/000011_prd2_provider_adapter_foundation.up.sql internal/admin/channel_handlers.go internal/models/models.go
```

---

## Fix #3: Adapter interface, registry, and openai_compatible implementation

**文件：** `internal/adapters/types.go`, `internal/adapters/registry.go`, `internal/adapters/openai_compatible.go`

**原问题：** UniRouter 下游保持 OpenAI-compatible，但上游 provider 需要独立 adapter 边界；否则后续 Anthropic/Gemini/Baidu 等 native 协议会继续污染 relay 主流程。

**当前实现：**

- 定义 `Adapter` interface：`BuildRequest`, `ParseResponse`, `Test`, `ClassifyHTTPStatus`。
- 定义归一化输出：`NormalizedResponse`, `Usage`, `ProviderMeta`, `NormalizedError`。
- 定义失败类型：`client_error`, `auth_error`, `rate_limit`, `channel_failure`。
- `DefaultRegistry` 注册 `openai_compatible`，空 adapter type 兼容映射为 `openai_compatible`。
- `OpenAICompatible` 负责 URL 拼接、模型映射、请求头、usage/meta 解析、HTTP 状态分类和 channel smoke test。

**关键测试：**

- `TestOpenAICompatibleBuildRequest_RewritesModelAndURL`
- `TestOpenAICompatibleParseResponse_Usage`
- `TestOpenAICompatibleClassifyHTTPStatus`
- `TestOpenAICompatibleTest`

**验证方法：**

```bash
go test ./internal/adapters -v
```

---

## Fix #4: Gateway relay uses adapter boundary for chat and embeddings

**文件：** `internal/gateway/router.go`, `internal/gateway/relay.go`

**原问题：** relay 直接按 OpenAI-compatible 上游构造请求，Provider Adapter Framework 即使有 interface 也不会真正接入网关主路径。

**当前实现：**

- router 查询 channel 时读取 `adapter_type` 和 `adapter_config`。
- 空 adapter type 在 route 层归一化为 `openai_compatible`。
- chat completions 通过 adapter registry 获取 adapter。
- chat 非流式请求通过 `BuildRequest` 构造上游请求，通过 `ParseResponse` 解析 usage/meta 后进入扣费、日志和指标。
- chat 流式请求构造通过 adapter，响应流处理仍沿用现有 relay 逻辑，未声称完整 stream adapter normalization。
- embeddings 请求通过 adapter 构造上游请求并解析 usage/meta。
- 未知 adapter type 会跳过该 channel，不会按错误协议静默调用。

**验证方法：**

```bash
go test ./internal/gateway -v
rg -n "DefaultRegistry|BuildRequest|ParseResponse|AdapterType" internal/gateway/router.go internal/gateway/relay.go
```

---

## Fix #5: Channel test endpoint uses adapter smoke path

**文件：** `internal/admin/test_handlers.go`, `internal/admin/admin_mock_test.go`

**原问题：** Admin channel test 仍手写 OpenAI-compatible curl/request，后续 adapter 类型不同会测试错协议。

**当前实现：**

- `/api/v1/channels/{id}/test` 查询 channel 的 adapter metadata。
- 通过 adapter registry 执行 smoke test。
- 返回结果包含 `adapter_type` 和 adapter 生成的 request URL。
- 未知 adapter type 返回 400 `adapter_error`。

**验证方法：**

```bash
go test ./internal/admin -run TestTestChannel -v
rg -n "adapter_type|adapter_error|DefaultRegistry" internal/admin/test_handlers.go internal/admin/admin_mock_test.go
```

---

## Fix #6: PRD2 matrix records Provider Adapter foundation without overclaiming

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**当前状态：**

- `CURRENT-STATUS.md` 标记 batch 4 为 fixed locally awaiting Canary。
- `P2-F05 Provider Adapter Framework`、`channels.adapter_type`、current relay `openai_compatible`、adapter usage/error/meta 归一化只标记为 `Unit tested`。
- native adapter、完整 stream normalization、真实上游 E2E、迁移-on-existing-DB 仍列为 gap。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '56,76p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/adapters ./internal/gateway ./internal/admin ./cmd/uniroute
go test ./...
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
git status --short
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — 验收结果

**验收者：** canary agent
**日期：** 文件系统时间戳为准
**对应轮次：** PRD2 implementation batch 4

---

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #3: PASS/FAIL — [原因]
- Fix #4: PASS/FAIL — [原因]
- Fix #5: PASS/FAIL — [原因]
- Fix #6: PASS/FAIL — [原因]

## 前置条件核验

- Frontend 无功能变化: PASS/FAIL
- Native adapter proof 未声称完成: PASS/FAIL
- Stream response normalization 未声称完整迁移: PASS/FAIL
- Token Metering V2 / reconciliation UI 未声称完成: PASS/FAIL
- 真实上游 E2E / 浏览器验收未声称完成: PASS/FAIL
- PRD2 acceptance matrix 未过度标注: PASS/FAIL

## 全局验证

- go test ./internal/adapters ./internal/gateway ./internal/admin ./cmd/uniroute: PASS/FAIL
- go test ./...: PASS/FAIL
- git diff --check: PASS/FAIL
- git diff --check -- $(git ls-files --others --exclude-standard): PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
