# FIX-SIGNAL.md — PRD2 implementation batch 5 Token Metering V2 backend foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 5

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 4 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步状态并推进 batch 5 |
| Frontend | 预期无功能变化 | 本轮只做 Token Metering V2 后端基础，不做 reconciliation UI 筛选 |
| Notification/dashboard delivery | 仍未完成 | 本轮只在 reconciliation response 暴露 estimated-ratio alert signal |
| Full-query analytics aggregation | 仍未完成 | 本轮 summary scope 是 `current_page`，P2-F14 再做高级分析聚合 |
| Native adapter-specific estimates | 仍未完成 | 本轮只定义 source contract 和 OpenAI-compatible/stream fallback 路径 |
| 真实上游 E2E / 浏览器验收 | 仍未完成 | 本轮提供 schema、gateway write path、reconciliation API 和单元测试证据 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成前端 usage_source 筛选、通知发送、Dashboard 告警卡、全量聚合分析、native adapter-specific estimate 或生产/E2E 验收，则应标为 FAIL。

---

## Fix #1: Batch 4 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** User 报告 batch 4 的 Canary 和 Sentinel 已完成，但当前状态需要同步。

**当前状态：**

- `CURRENT-STATUS.md` 记录 batch 4 为 Canary passed / Sentinel complete。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 Provider Adapter 证据更新为 Canary 6/6 PASS + User reported Sentinel completed。
- 当前活跃轮次更新为 batch 5 fixed locally awaiting Canary。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '48,58p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: Token Metering V2 contract and schema foundation

**文件：** `docs/tech-ref/12-token-metering-v2.md`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`, `migrations/000012_prd2_token_metering_v2.up.sql`, `migrations/000012_prd2_token_metering_v2.down.sql`, `internal/models/models.go`

**原问题：** PRD2 要求 usage normalization、source confidence 和 estimated-vs-upstream variance，但 PRD1 `call_logs` 只有历史 `usage_source` 字符串，无法保存预冻结估算或置信度。

**当前实现：**

- 新增 tech-ref，定义 canonical usage source：
  - `upstream`
  - `counter_v2`
  - `adapter_estimated`
  - `estimated`
  - `no_route`
  - `upstream_failed`
- migration 新增 `call_logs.estimated_tokens`。
- migration 新增 `call_logs.usage_confidence`。
- migration 将 legacy `api` 迁移为 `upstream`。
- migration 将 legacy `stream_estimate` 迁移为 `counter_v2`。
- migration 新增 `idx_call_logs_usage_source_created`。
- migration seed `settings` key `token_metering.estimated_ratio_alert_threshold`，默认 `0.2`。
- `models.CallLog` 增加 `EstimatedTokens` 和 `UsageConfidence`。

**验证方法：**

```bash
test -f docs/tech-ref/12-token-metering-v2.md
test -f migrations/000012_prd2_token_metering_v2.up.sql
test -f migrations/000012_prd2_token_metering_v2.down.sql
rg -n "estimated_tokens|usage_confidence|counter_v2|token_metering.estimated_ratio_alert_threshold" migrations/000012_prd2_token_metering_v2.up.sql internal/models/models.go docs/tech-ref/12-token-metering-v2.md
```

---

## Fix #3: Shared metering source normalization

**文件：** `internal/metering/metering.go`, `internal/metering/metering_test.go`, `internal/adapters/openai_compatible.go`

**原问题：** usage source 字符串分散在 gateway、adapter 和测试里，且 `api`/`stream_estimate` 与 PRD2 source 语义不一致。

**当前实现：**

- 新增 `internal/metering` 包。
- `NormalizeUsageSource` 把 `api` 映射为 `upstream`。
- `NormalizeUsageSource` 把 `stream_estimate` 映射为 `counter_v2`。
- `SourceConfidence` 为不同 source 提供置信度。
- `EstimatedTokenRatio` 支持 estimated-ratio alert signal。
- `openai_compatible` adapter 成功响应的 usage source 改为 `upstream`。

**关键测试：**

- `TestNormalizeUsageSource_LegacyAliases`
- `TestSourceConfidence`
- `TestEstimatedTokenRatio`

**验证方法：**

```bash
go test ./internal/metering ./internal/adapters -v
```

---

## Fix #4: Gateway writes estimated tokens, canonical usage source, and confidence

**文件：** `internal/gateway/relay.go`, `internal/gateway/relay_mock_test.go`

**原问题：** gateway 只写最终 tokens 和历史 source，reconciliation 不能看见预冻结估算，也无法区分 upstream 与 deterministic fallback 的可信度。

**当前实现：**

- `recordRequest` 写入 `estimated_tokens` 和 `usage_confidence`。
- `recordRequest` 写入前统一调用 `metering.NormalizeUsageSource`。
- chat no-route 写 `no_route` 和预冻结估算。
- chat all-upstream-failed 写 `upstream_failed` 和预冻结估算。
- non-stream successful adapter usage 写 canonical `upstream`。
- stream upstream final usage 写 canonical `upstream`。
- stream missing upstream usage 写 canonical `counter_v2`。
- embeddings 写入 input estimate 作为 `estimated_tokens`。

**关键测试：**

- `TestRecordRequest_Success`
- `TestRelayStreamRequest_NoUpstreamUsage`
- `TestRelayStreamRequest_UpstreamUsageSource`

**验证方法：**

```bash
go test ./internal/gateway -run 'Test(RecordRequest_Success|RelayStreamRequest_NoUpstreamUsage|RelayStreamRequest_UpstreamUsageSource)' -v
```

---

## Fix #5: Reconciliation supports usage_source filter and variance signal

**文件：** `internal/admin/model_handlers.go`, `internal/admin/admin_mock_test.go`

**原问题：** `/api/v1/reconciliation` 只能按 model/status 过滤，且 `estimated_tokens` 实际由 prompt+completion 推导，无法表达预冻结估算、usage source、source confidence 或 estimated-ratio alert。

**当前实现：**

- `GET /api/v1/reconciliation?usage_source=...` 支持 source 筛选。
- legacy filter alias `usage_source=api` 映射为 `upstream`。
- legacy filter alias `usage_source=stream_estimate` 映射为 `counter_v2`。
- record 增加：
  - `usage_source`
  - `variance_tokens`
  - `variance_ratio`
  - `source_confidence`
- 保留现有字段：
  - `estimated_tokens`
  - `actual_tokens`
  - `delta`
- response 增加 page-scoped `summary`：
  - `estimated_tokens`
  - `upstream_tokens`
  - `estimated_ratio`
  - `estimated_ratio_alert_threshold`
  - `estimated_usage_alert`
  - `estimated_ratio_alert_threshold_setting`
  - `estimated_ratio_scope`

**关键测试：**

- `TestListReconciliation_Success`
- `TestListReconciliation_UsageSourceFilterNormalizesLegacyAlias`

**验证方法：**

```bash
go test ./internal/admin -run 'TestListReconciliation_(Success|UsageSourceFilterNormalizesLegacyAlias)' -v
```

---

## Fix #6: PRD2 status, matrix, and acceptance discipline

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**当前状态：**

- `CURRENT-STATUS.md` 标记 batch 5 为 fixed locally awaiting Canary。
- `P2-F06 Token Metering V2`、missing stream usage、estimated-vs-upstream variance rows 标记为 `Unit tested`。
- 未把 frontend usage_source filter、notification delivery、Dashboard alert、full-query analytics aggregation、native adapter-specific estimates 或真实上游 E2E 标记完成。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '50,60p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/metering ./internal/adapters ./internal/gateway ./internal/admin ./cmd/uniroute
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
**对应轮次：** PRD2 implementation batch 5

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
- Notification/dashboard delivery 未声称完成: PASS/FAIL
- Full-query analytics aggregation 未声称完成: PASS/FAIL
- Native adapter-specific estimates 未声称完成: PASS/FAIL
- 真实上游 E2E / 浏览器验收未声称完成: PASS/FAIL
- PRD2 acceptance matrix 未过度标注: PASS/FAIL

## 全局验证

- go test ./internal/metering ./internal/adapters ./internal/gateway ./internal/admin ./cmd/uniroute: PASS/FAIL
- go test ./...: PASS/FAIL
- git diff --check: PASS/FAIL
- git diff --check -- $(git ls-files --others --exclude-standard): PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
