# FIX-SIGNAL.md — PRD2 Batch 9.5 Billing Settle Hardening

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 9.5

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 1-9 | Canary full-context regression PASS | User 已转发 Canary 报告，Batch 10 无阻断项 |
| `PRD2-ERR-001` | Fixed awaiting Canary | 本轮只修 Medium settle/frozen hardening |
| `PRD2-AUDIT-001` | 仍 Open | 当前代码已有 `channel.create/update/delete` 基础审计；剩余是 before/after 细化和 `channel.test` 审计增强，不混入本轮 |
| Batch 10 Developer Console | 未实现 | 本轮不做前端 Developer Console |
| Atlas 部署 / 真实 upstream provider E2E | 未执行 | 本轮是本地后端 hardening，不触发部署 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成 Developer Console、Atlas 部署、真实 upstream E2E 或 `PRD2-AUDIT-001` 全量审计增强，应标为 FAIL。

---

## Fix #1: Settlement retry and cancel fallback

**文件：** `internal/gateway/relay.go:90`

**原问题：** `PRD2-ERR-001` 指出 chat 非流式和流式路径在 `billing.Settle` 返回错误时只写日志，然后继续成功响应，可能让 `freeze_idempotency.status='frozen'` 和 `wallets.frozen` 长期残留。

**当前实现：**

- 新增 `settleBillingWithRetry`。
- 对 settlement 执行最多 3 次重试。
- 使用独立 `context.Background()` + timeout，避免客户端断开导致关键 billing 操作被取消。
- 3 次仍失败时调用 `billing.Cancel` 释放冻结金额。
- `Settle` 返回 `Success=false` 或 nil result 也视为 settle failure。

**验证方法：**

```bash
go test ./internal/gateway -run 'TestRelayNonStreamRequest_Settle|TestRelayStreamRequest_SettleError' -v
```

期望：

- `TestRelayNonStreamRequest_SettleRetrySuccess` PASS：前两次 settle 失败，第三次成功，不调用 Cancel，最终 HTTP 200。
- `TestRelayNonStreamRequest_SettleError` PASS：三次 settle 失败后调用 Cancel，最终 HTTP 500，响应包含 `billing_error.settle_failed`。
- `TestRelayStreamRequest_SettleError` PASS：三次 settle 失败后调用 Cancel，SSE 仍以 `[DONE]` 正常收尾，`_uniroute.billing_status=settle_failed_cancelled` 且 charged 为 0。

---

## Fix #2: Non-stream and embeddings no longer return success on unrecovered settle failure

**文件：** `internal/gateway/relay.go:373`, `internal/gateway/relay.go:881`

**原问题：** 非流式 chat 和 embeddings 在上游成功后，如果 settlement 无法完成，继续返回成功会造成“客户端成功、资金仍冻结”的不一致状态。

**当前实现：**

- 非流式 chat 在 settle 重试后仍失败时返回 HTTP 500，不记录成功 call log，不触发 budget alert。
- embeddings 路径也使用同一 settlement helper；settle unrecovered 时返回 HTTP 500。
- 流式路径因为内容可能已写出，不能改 HTTP status；因此取消冻结、记录 charged=0，并在 `_uniroute.billing_status` 中暴露状态。

**验证方法：**

```bash
go test ./internal/gateway
go test -count=1 -tags integration ./cmd/uniroute -run 'TestPRD2P0' -v
```

---

## Fix #3: Cleanup no longer deletes frozen idempotency rows directly

**文件：** `internal/admin/audit_handlers.go:279`

**原问题：** 旧 cleanup 会删除 30 天以上的 `freeze_idempotency` 行，不区分 `status='frozen'`。如果某笔 frozen 因 settle 异常残留，删除幂等记录后将更难恢复钱包冻结金额。

**当前实现：**

- cleanup 先释放 `updated_at < NOW() - INTERVAL '24 hours'` 且仍为 `status='frozen'` 的 stale freeze。
- 钱包按 org 聚合释放 frozen，并把对应 idempotency 行标记为 `cancelled`。
- 30 天清理只删除 `status <> 'frozen'` 的旧幂等记录。

**验证方法：**

```bash
go test ./internal/admin -run 'TestCleanupOldData' -v
go test -count=1 -tags integration ./internal/admin -run TestCleanupOldData_ReleasesStaleFrozen_Integration -v
```

期望：

- `TestCleanupOldData_ReleasesStaleFrozenBeforeDeletingIdempotency` PASS。
- 测试确认 cleanup SQL 包含 stale frozen release，并且 delete 语句包含 `status <> 'frozen'`。
- `TestCleanupOldData_ReleasesStaleFrozen_Integration` PASS，真实迁移库中 stale frozen 被标记 `cancelled`，钱包 `frozen` 归零并退回 `balance`。

---

## Fix #4: Current status and issue queue updated without overclaim

**文件：** `docs/issues/OPEN-ISSUES.md`, `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**当前实现：**

- `PRD2-ERR-001` 标记为 `Fixed awaiting Canary`。
- `CURRENT-STATUS.md` 标记 Batch 9.5 fixed locally awaiting Canary。
- PRD2 matrix 只写本地修复证据，不把 Batch 9.5 提升为 Canary accepted。
- `PRD2-AUDIT-001` 修正为审计增强项并保持 Open。

**验证方法：**

```bash
rg -n "PRD2-ERR-001|Batch 9.5|settle-error|PRD2-AUDIT-001" docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

Canary 请从仓库根目录执行：

```bash
go test ./internal/gateway -run 'TestRelayNonStreamRequest_Settle|TestRelayStreamRequest_SettleError' -v
go test ./internal/admin -run 'TestCleanupOldData' -v
go test ./internal/gateway ./internal/admin
go test ./internal/billing
go test ./...
go test -count=1 -tags integration ./internal/admin -run TestCleanupOldData_ReleasesStaleFrozen_Integration -v
go test -count=1 -tags integration ./cmd/uniroute -run 'TestPRD2P0' -v
go test -count=1 -tags integration ./internal/billing -run TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration -v
go test -count=1 -tags integration ./internal/migrate -run TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration -v
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

本地已执行并通过：

- `go test ./internal/gateway -run 'TestRelayNonStreamRequest_Settle|TestRelayStreamRequest_SettleError' -v`
- `go test ./internal/admin -run 'TestCleanupOldData' -v`
- `go test ./internal/gateway ./internal/admin`
- `go test ./internal/billing`
- `go test ./...`
- `go test -count=1 -tags integration ./internal/admin -run TestCleanupOldData_ReleasesStaleFrozen_Integration -v`
- `go test -count=1 -tags integration ./cmd/uniroute -run 'TestPRD2P0' -v`
- `go test -count=1 -tags integration ./internal/billing -run TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration -v`
- `go test -count=1 -tags integration ./internal/migrate -run TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration -v`

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 9.5 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 9.5 Billing Settle Hardening

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/gateway -run 'TestRelayNonStreamRequest_Settle|TestRelayStreamRequest_SettleError' -v` | PASS/FAIL | |
| `go test ./internal/admin -run 'TestCleanupOldData' -v` | PASS/FAIL | |
| `go test ./internal/gateway ./internal/admin` | PASS/FAIL | |
| `go test ./internal/billing` | PASS/FAIL | |
| `go test ./...` | PASS/FAIL | |
| `go test -count=1 -tags integration ./internal/admin -run TestCleanupOldData_ReleasesStaleFrozen_Integration -v` | PASS/FAIL/SKIP | note DB/skips if any |
| PRD2 integration tests | PASS/FAIL/SKIP | note DB/skips if any |
| `git diff --check` | PASS/FAIL | |

## 新发现

- None / list issue paths.

## 结论

PASS/FAIL. 明确说明 `PRD2-ERR-001` 是否可改为 Canary passed，以及是否存在 Batch 10 blocker。
```
