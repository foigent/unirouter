# FIX-SIGNAL.md — PRD2 implementation batch 6 Audit Schema Upgrade backend foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 6

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 5 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步状态并推进 batch 6 |
| Frontend | 预期无功能变化 | 本轮只做 Audit Schema Upgrade 后端基础，不做 Audit Logs UI 筛选 |
| Gateway-originated audit events | 仍未完成 | 本轮只定义 `gateway_api` source contract，不在 gateway 发审计事件 |
| All historical audit actions before/after | 仍未完成 | 本轮只覆盖关键 P2-F07 验收动作，不批量改写所有历史 handler |
| 真实 JWT/API E2E / 浏览器验收 | 仍未完成 | 本轮提供 schema、helper、filters 和单元测试证据 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成前端 Audit Logs 筛选、gateway audit event emission、所有历史动作 before/after 全覆盖或真实 E2E/浏览器验收，则应标为 FAIL。

---

## Fix #1: Batch 5 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** User 报告 batch 5 的 Canary 和 Sentinel 已完成，但当前状态需要同步。

**当前状态：**

- `CURRENT-STATUS.md` 记录 batch 5 为 Canary passed / Sentinel complete。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 Token Metering V2 证据更新为 Canary 6/6 PASS + User reported Sentinel completed。
- 当前活跃轮次更新为 batch 6 fixed locally awaiting Canary。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '52,62p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: Audit schema contract and migration

**文件：** `docs/tech-ref/13-audit-schema-upgrade.md`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`, `migrations/000013_prd2_audit_schema_upgrade.up.sql`, `migrations/000013_prd2_audit_schema_upgrade.down.sql`, `internal/models/models.go`

**原问题：** PRD2 P2-F07 要求 actor/org/project/source/request metadata 和 before/after，但当前 schema/文档没有结构化 before/after contract。

**当前实现：**

- 新增 tech-ref，定义 canonical audit source：
  - `admin_console`
  - `developer_console`
  - `gateway_api`
  - `system`
- migration 新增 `audit_logs.before_state JSONB`。
- migration 新增 `audit_logs.after_state JSONB`。
- migration 将 legacy `source='admin'` 归一为 `admin_console`。
- migration 新增 action/resource/source 相关索引。
- `models.AuditLog` 增加 `Before`、`After`、`Source` 字段。

**验证方法：**

```bash
test -f docs/tech-ref/13-audit-schema-upgrade.md
test -f migrations/000013_prd2_audit_schema_upgrade.up.sql
test -f migrations/000013_prd2_audit_schema_upgrade.down.sql
rg -n "before_state|after_state|admin_console|developer_console|gateway_api|system" docs/tech-ref/13-audit-schema-upgrade.md migrations/000013_prd2_audit_schema_upgrade.up.sql internal/models/models.go
```

---

## Fix #3: Audit helper writes source, request metadata, project, before, and after

**文件：** `internal/admin/audit_handlers.go`, `internal/admin/admin_mock_test.go`

**原问题：** `recordAudit` 只写 `detail`，不统一写 `source`、`user_agent`、`request_id`、`before_state`、`after_state`，且 source 仍可能是 legacy `admin`。

**当前实现：**

- 新增 `recordAuditFromRequest`。
- 新增 `recordAuditForProjectFromRequest`。
- 新增 `recordAuditForOrgProjectFromRequest`。
- `recordAuditEntry` 统一写：
  - `org_id`
  - `project_id`
  - actor `user_id`
  - `ip`
  - `user_agent`
  - `request_id`
  - canonical `source`
  - `before_state`
  - `after_state`
- `detail.before` / `detail.after` 自动抽取为结构化 fields，同时保留原始 `detail`。
- source 根据上下文角色映射为 `admin_console` 或 `developer_console`；空系统上下文为 `system`。

**关键测试：**

- `TestRecordAudit_Success`

**验证方法：**

```bash
go test ./internal/admin -run TestRecordAudit_Success -v
```

---

## Fix #4: Audit list supports PRD2 filters and returns enriched fields

**文件：** `internal/admin/audit_handlers.go`, `internal/admin/admin_mock_test.go`

**原问题：** `/api/v1/audit-logs` 只能全量分页读取，不能按 org/project/actor/action/resource/time/source 过滤，也不返回 PRD2 audit metadata。

**当前实现：**

- 支持过滤：
  - `org_id`
  - `project_id`
  - `actor_id`
  - `user_id`
  - `action`
  - `resource`
  - `resource_id`
  - `source`
  - `start_time`
  - `end_time`
- `source=admin` 作为 legacy alias 归一化为 `admin_console`。
- 响应返回：
  - `actor_id` 和兼容字段 `user_id`
  - `org_id`
  - `project_id`
  - `detail`
  - `before`
  - `after`
  - `ip`
  - `user_agent`
  - `request_id`
  - `source`
  - pagination metadata。

**关键测试：**

- `TestListAuditLogs_Success`
- `TestListAuditLogs_Filters`
- `TestListAuditLogs_QueryError`

**验证方法：**

```bash
go test ./internal/admin -run 'TestListAuditLogs' -v
```

---

## Fix #5: Critical PRD2 audit actions record before/after evidence

**文件：** `internal/admin/key_handlers.go`, `internal/admin/model_governance_handlers.go`, `internal/admin/wallet_handlers.go`, `internal/admin/admin_mock_test.go`, `internal/admin/model_governance_handlers_test.go`

**原问题：** P2-F07 acceptance 要求 project key creation、model lifecycle change、wallet adjustment 等关键动作有 actor/project/before/after 审计证据。

**当前实现：**

- Project API key creation 使用 request-aware project audit helper，并写 after-state。
- Model registry update 在更新前读取 lifecycle baseline，并写 before/after state。
- Wallet top-up 使用统一 audit helper。
- Wallet balance adjustment 写 before/after balance。

**关键测试：**

- `TestCreateAPIKey_AuditIncludesProjectAndAfterState`
- `TestUpdateModelRegistry_LifecycleAuditBeforeAfter`
- `TestBalanceAdjust_Success`

**验证方法：**

```bash
go test ./internal/admin -run 'Test(CreateAPIKey_AuditIncludesProjectAndAfterState|UpdateModelRegistry_LifecycleAuditBeforeAfter|BalanceAdjust_Success)' -v
```

---

## Fix #6: PRD2 status, matrix, and acceptance discipline

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**当前状态：**

- `CURRENT-STATUS.md` 标记 batch 6 为 fixed locally awaiting Canary。
- `P2-F07 Audit schema upgrade`、Model lifecycle before/after、Project key audit rows 标记为 `Unit tested`。
- 未把 frontend audit filters、gateway audit event emission、所有历史动作 before/after 全覆盖或真实 JWT/API E2E 标记完成。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '56,64p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/admin ./cmd/uniroute
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
**对应轮次：** PRD2 implementation batch 6

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
- Gateway-originated audit events 未声称完成: PASS/FAIL
- All historical audit actions before/after 未声称完成: PASS/FAIL
- 真实 JWT/API E2E / 浏览器验收未声称完成: PASS/FAIL
- PRD2 acceptance matrix 未过度标注: PASS/FAIL

## 全局验证

- go test ./internal/admin ./cmd/uniroute: PASS/FAIL
- go test ./...: PASS/FAIL
- git diff --check: PASS/FAIL
- git diff --check -- $(git ls-files --others --exclude-standard): PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
