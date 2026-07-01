# FIX-SIGNAL.md — PRD2 implementation batch 8 HA Readiness backend/ops foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 8

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 7 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步状态并推进 batch 8 |
| Managed PostgreSQL HA / Redis Sentinel | 仍未完成 | PRD2 P2-F09 要求 HA readiness，不强制完整 HA 平台栈 |
| 真实负载均衡器双实例线上 smoke | 仍未完成 | 本轮提供脚本、runbook 和本地 integration 证据，实际目标环境 smoke 需后续执行 |
| 全局分布式 admission semaphore | 仍未完成 | 当前 admission controller 是单实例容量保护；PRD2 batch 8 文档明确容量口径 |
| 生产备份恢复演练 | 仍未完成 | 本轮文档化备份/恢复/回滚步骤，不声称生产演练完成 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称已经完成 Redis Sentinel、PG replica、线上 LB smoke、全局 admission semaphore 或生产备份恢复演练，则应标为 FAIL。

---

## Fix #1: Batch 7 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** User 报告 batch 7 的 Canary 和 Sentinel 已完成，但状态文档仍停留在 awaiting Canary。

**当前实现：**

- `CURRENT-STATUS.md` 记录 batch 7 为 Canary passed / Sentinel complete。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 P2-F08 Notification/Event 证据更新为 Canary 7/7 PASS + User reported Sentinel completed。
- 当前活跃轮次更新为 batch 8 fixed locally awaiting Canary。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '64,72p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: HA readiness contract and runbook

**文件：** `docs/tech-ref/15-ha-readiness.md:8`, `docs/ops/deployment-runbook.md:481`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`

**原问题：** PRD2 P2-F09 要求 multi-instance assumptions、shared DB/Redis state、backup/restore/rollback 和 two-instance smoke，但当前只有 PRD1/Canary 部署说明，缺少 PRD2 HA readiness contract。

**当前实现：**

- 新增 HA Readiness tech-ref。
- 明确应用实例本地状态和 DB/Redis 共享状态边界。
- 明确 secondary/canary 必须 `SKIP_MIGRATION=true`。
- 明确 `/health/ready` 需要返回 `{"status":"ok","db":"ok","redis":"ok"}`。
- 明确 rollback 和 schema-bearing release 的恢复步骤。
- deployment runbook 新增 PRD2 HA readiness smoke 章节。

**验证方法：**

```bash
test -f docs/tech-ref/15-ha-readiness.md
rg -n "SKIP_MIGRATION|Two-Instance Smoke|Wallet And Idempotency Safety|Rollback" docs/tech-ref/15-ha-readiness.md docs/ops/deployment-runbook.md
```

---

## Fix #3: Two-instance HA smoke script

**文件：** `scripts/prd2-ha-smoke.sh:1`

**原问题：** P2-F09 要求 two-instance smoke procedure where practical；当前没有可复制执行的 PRD2 双实例 smoke 脚本。

**当前实现：**

- 新增 `scripts/prd2-ha-smoke.sh`。
- 默认验证 stable/canary `/health/ready`。
- 可选验证 proxy `/health/ready`。
- 默认不发真实模型请求。
- 只有显式设置 `UNIROUTE_RUN_GATEWAY_SMOKE=1` 和 `UNIROUTE_API_KEY` 时才发 gateway smoke。
- 脚本已设置可执行权限。

**验证方法：**

```bash
bash -n scripts/prd2-ha-smoke.sh
test -x scripts/prd2-ha-smoke.sh
rg -n "UNIROUTE_STABLE_URL|UNIROUTE_CANARY_URL|UNIROUTE_RUN_GATEWAY_SMOKE|health/ready" scripts/prd2-ha-smoke.sh
```

---

## Fix #4: Duplicate freeze race hardening for shared-DB HA

**文件：** `internal/billing/billing.go:206`, `internal/billing/billing.go:231`, `internal/billing/billing.go:239`, `internal/billing/billing_test.go:102`

**原问题：** 两个实例并发收到同一个 request id 时，PostgreSQL 主键能避免双扣，但输掉 `freeze_idempotency` 插入竞争的一方可能把唯一键冲突暴露成错误。

**当前实现：**

- `Freeze` 在 `freeze_idempotency` unique violation 后主动 rollback。
- rollback 后回读已存在的 `frozen_amount`。
- 如果回读成功，返回 `Success=true` 和既有冻结金额。
- 新增 `isUniqueViolation` 单元测试。

**关键测试：**

- `TestIsUniqueViolation`

**验证方法：**

```bash
go test ./internal/billing -run TestIsUniqueViolation -v
```

---

## Fix #5: Shared-DB idempotency and wallet safety integration evidence

**文件：** `internal/billing/billing_integration_test.go:114`

**原问题：** P2-F09 acceptance 要求 repeated requests through two instances do not violate idempotency or wallet safety；已有单实例 freeze idempotency 测试，但没有 stable/canary 双实例视角。

**当前实现：**

- 新增 `TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration`。
- 使用两个 `Billing` 实例共享同一个 PostgreSQL pool 来模拟 stable/canary。
- 两个实例并发使用同一 `request_id` 调用 `Freeze`。
- 断言：
  - 两个调用都不暴露 idempotency race。
  - `freeze_idempotency` 只有一行。
  - `transactions` 中 freeze 交易只有一行。
  - 钱包 `balance` 只扣一次。
  - 钱包 `frozen` 只增加一次。

**验证方法：**

```bash
go test -tags integration ./internal/billing -run TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration -v
```

> 本地运行结果：2026-07-02 PASS。本机测试库缺少部分较新 PRD2 表，`testutil.TruncateAll` 会输出 nonfatal truncate warning；目标测试通过。

---

## Fix #6: Sensitive-word cache convergence across instances

**文件：** `cmd/uniroute/main.go:101`, `cmd/uniroute/main.go:549`

**原问题：** 敏感词配置保存在 DB，但每个实例有本地 DFA cache。此前启动加载和本实例热更新可用，但多实例下其它实例不会自动收敛。

**当前实现：**

- 用 `startSensitiveWordsReloadLoop` 替换一次性加载。
- 每个实例启动时加载一次。
- 每分钟从 DB-backed `settings.sensitive_words` 重新加载。
- 保持 admin handler 本实例热更新路径不变。

**验证方法：**

```bash
go test ./cmd/uniroute -v
rg -n "startSensitiveWordsReloadLoop|loadSensitiveWords" cmd/uniroute/main.go
```

---

## Fix #7: PRD2 matrix/status updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** P2-F09 was still Planned and batch 7 status was stale.

**当前实现：**

- P2-F09 HA readiness is marked `Unit tested`, not E2E accepted.
- Remaining gaps explicitly include Canary verification, live two-instance target smoke, load-balancer/proxy evidence, and production backup/restore rehearsal.
- P2-F12 acceptance discipline keeps HA readiness at backend/ops foundation scope.

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '64,72p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

Canary 请从仓库根目录执行：

```bash
go test ./internal/billing ./cmd/uniroute
go test ./...
go test -tags integration ./internal/billing -run TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration -v
bash -n scripts/prd2-ha-smoke.sh
test -x scripts/prd2-ha-smoke.sh
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

如果 Canary 环境没有 PostgreSQL test DB，integration test 可按现有 `testutil.SetupTestDB` 行为 skip；但若环境可连接 `TEST_DATABASE_URL` 或默认 `uniroute_test`，该测试应 PASS。

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Implementation Batch 8 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 implementation batch 8 — HA Readiness backend/ops foundation

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence
- Fix #6: PASS/FAIL — evidence
- Fix #7: PASS/FAIL — evidence

## 前置条件核验

- Managed PostgreSQL HA / Redis Sentinel 未声称完成: PASS/FAIL
- 真实负载均衡器双实例线上 smoke 未声称完成: PASS/FAIL
- 全局分布式 admission semaphore 未声称完成: PASS/FAIL
- 生产备份恢复演练未声称完成: PASS/FAIL

## 全局验证

| 检查项 | 结果 |
|--------|------|
| `go test ./internal/billing ./cmd/uniroute` | PASS/FAIL |
| `go test ./...` | PASS/FAIL |
| `go test -tags integration ./internal/billing -run TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration -v` | PASS/FAIL/SKIP |
| `bash -n scripts/prd2-ha-smoke.sh` | PASS/FAIL |
| `test -x scripts/prd2-ha-smoke.sh` | PASS/FAIL |
| `git diff --check` | PASS/FAIL |
| HA tech-ref/runbook | PASS/FAIL |
| Matrix 未过度标注 | PASS/FAIL |

## 新发现的问题

无 / 列表

## 结论

PASS/FAIL summary
```
