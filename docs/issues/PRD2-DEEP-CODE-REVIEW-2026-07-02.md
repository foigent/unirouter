# PRD2 Deep Code Review — 2026-07-02

**审查者：** OpenClaw Canary
**范围：** PRD2 Batch 1-9 全部代码变更（14 migrations, 5 新包, gateway/admin/middleware 变更）
**方法：** 基于风险的 7 区域代码级审查（只读，不修改代码）

---

## 区域 1：Migration 安全性（000009-000014）

### Finding M-1: `audit_logs.project_id` 未回填历史数据（Low）

**文件：** `migrations/000009_prd2_project_foundation.up.sql`
**问题：** Migration 为 `api_keys`、`call_logs`、`transactions` 都写了回填 UPDATE，但 `audit_logs` 只添加了 `project_id` 列，没有回填。现有审计日志的 `project_id` 将永久为 NULL。
**影响：** 历史审计日志无法按项目过滤。不是 bug（可能是有意设计，因为旧审计日志确实没有项目信息），但与其它表的回填策略不一致。
**严重性：** Low — 仅影响历史数据查询，不影响新数据正确性。

### Finding M-2: 000012 down migration 不回滚 source 迁移（Low）

**文件：** `migrations/000012_prd2_token_metering_v2.down.sql`
**问题：** Up migration 将 `usage_source = 'api'` 改为 `'upstream'`，`'stream_estimate'` 改为 `'counter_v2'`。Down migration 只 DROP 列，不回滚 source 值。回滚后 `usage_source` 将保留新值。
**对比：** 000013 的 down migration 正确回滚了 `admin_console → admin`。
**影响：** 如果需要回滚 000012，历史 `usage_source` 值不可逆。
**严重性：** Low — 回滚场景罕见，且新值语义更清晰。

### Migration 总体评估

- 所有 migration 都在 `BEGIN/COMMIT` 事务中 ✅
- `CREATE TABLE IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS` 保证幂等 ✅
- 000009 回填逻辑正确（先 via api_keys，再 fallback via org_id） ✅
- 000010 seed 使用 `ON CONFLICT DO NOTHING` 防重复 ✅
- 000014 dedupe 使用 partial unique index（`WHERE dedupe_key <> ''`） ✅
- Down migration 顺序正确（先 DROP INDEX，再 DROP COLUMN/TABLE） ✅

---

## 区域 2：Gateway Auth + RBAC

### Finding R-1: `NormalizeRole` 对未知角色透传（Design Choice, Not Bug）

**文件：** `internal/auth/auth.go:149-155`
**问题：** `NormalizeRole` 只映射 `admin → platform_admin` 和 `org_viewer → viewer`，其它角色原样返回。未知角色（如空字符串或垃圾值）不会被规范化。
**分析：** 这不是安全漏洞，因为 `RequireRoles` 使用 allowlist 检查（`RoleIn(role, allowed...)`），未知角色不会匹配任何 allowlist，会被拒绝。
**验证：** `IsValidRole` 在 user create/update 时校验，防止脏数据入库。
**严重性：** None — 设计正确。

### RBAC 总体评估

- `RequireRoles` 使用 `RoleIn` 做规范化比较，legacy `admin` 能匹配 `platform_admin` ✅
- `AdminAuth` 从 JWT 注入 role 到 context，`RequireRoles` 从 context 读取 ✅
- 路由分组正确（channel=platform, wallet=billing, audit=platform+org+billing） ✅
- `canReadProject`/`canWriteProject` 使用 `hasProjectMembership` 查询 DB ✅
- `hasProjectMembership` 使用参数化查询（`$1, $2, $3::text[]`），无 SQL 注入风险 ✅

---

## 区域 3：Billing Freeze + Idempotency

### Finding B-1: Freeze 竞态处理正确（确认）

**文件：** `internal/billing/billing.go:206-218`
**分析：** 当两个实例并发使用同一 `request_id` 调用 Freeze 时：
1. 实例 A 成功插入 `freeze_idempotency` 并 commit
2. 实例 B 的 INSERT 因 unique violation 失败
3. 实例 B rollback（撤销钱包扣款）
4. 实例 B 回读已存在的 `frozen_amount`，返回 `Success=true`

**关键验证：** 步骤 3 的 rollback 会撤销步骤 2 之前的所有 SQL（包括钱包 UPDATE），所以钱包不会被多扣。Integration test `TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration` 验证了这个行为。
**严重性：** None — 实现正确。

### Billing 总体评估

- Freeze 使用 `SELECT ... FOR UPDATE` 锁定钱包行 ✅
- `isUniqueViolation` 精确检查 PG 错误码 `23505` ✅
- Settle 使用独立事务，失败时 `defer tx.Rollback` 保证回滚 ✅
- `EstimateFreezeAmount` 使用 `minFreezeAmount` 兜底 ✅

---

## 区域 4：Model Governance

### Finding G-1: 未注册模型默认可调用（Design Choice）

**文件：** `internal/modelgov/modelgov.go:195-201`
**问题：** `modelLifecycle` 在 `model_registry` 无记录时返回空字符串，`IsCallableLifecycle("")` 返回 true。即不在注册表中的模型默认可调用。
**分析：** 这是有意的 PRD1 向后兼容设计。如果突然拦截所有未注册模型，现有用户会中断。
**风险：** 如果管理员拼错注册表中的模型名（如 `gpt-4o` 写成 `gpt4o`），两个变体都不会被 retired 检查拦截。
**建议：** 未来可考虑在 Developer Console UI 中增加"未注册模型调用告警"。
**严重性：** Low — 设计权衡，非 bug。

### Model Governance 总体评估

- `CheckAccess` 先查 alias → 再查 lifecycle → 再查 project model set，顺序正确 ✅
- Retired 模型在 relay/billing 前被拦截（403），不会进入上游 ✅
- `projectAllowsModel` 同时检查 requested 和 resolved model ✅
- `model_set_items` cascade delete 在 model_registry 删除时触发 — 需注意操作顺序 ✅
- SQL 查询使用参数化（`$1, $2, $3, $4`），无注入风险 ✅

---

## 区域 5：Project Isolation

### Finding P-1: 项目隔离实现正确（确认）

**文件：** `internal/admin/rbac_helpers.go`, `internal/admin/log_handlers.go`, `internal/admin/key_handlers.go`
**分析：**
- `canReadProject` / `canWriteProject` 对 project-scoped 角色强制检查 `hasProjectMembership`
- `ListLogs` 对 project-scoped 角色要求 `project_id` 参数，否则 403
- `CreateAPIKey` 对 developer 要求显式 `project_id`
- `projectID == 0` 时 project-scoped 角色被拒绝（`return false`）
- PlatformAdmin/OrgAdmin 可以绕过项目检查（有全局视野）

**无跨项目泄露路径。**
**严重性：** None — 实现正确。

---

## 区域 6：Audit Completeness

### Finding A-1: 非 PRD2 核心操作缺少审计（Low）

**文件：** `internal/admin/channel_handlers.go`, `internal/admin/test_handlers.go`
**问题：** Channel CRUD 和 channel test 操作没有写审计日志。这些是 platform_admin 操作，属于安全敏感操作。
**影响：** 无法追踪谁在什么时候修改了渠道配置。
**对比：** API key create/update、model registry update、wallet adjust 都有审计。
**严重性：** Low — 不影响 PRD2 验收，但应在后续补全。

### Audit 总体评估

- `recordAuditEntry` 统一写 org_id/project_id/user_id/ip/user_agent/request_id/source/before/after ✅
- `detail.before` / `detail.after` 自动抽取为结构化字段 ✅
- `normalizeAuditSource` 将 legacy `admin` 映射为 `admin_console` ✅
- Audit 写入是非阻塞的（失败只 log，不阻断业务） ✅

---

## 区域 7：Adapter 请求/响应正确性

### Finding AD-1: `BuildRequest` URL 拼接安全（确认）

**文件：** `internal/adapters/openai_compatible.go:24-51`
**分析：** `BuildRequest` 使用 `strings.TrimRight(baseURL, "/") + "/v1/chat/completions"`。即使 `baseURL` 末尾有多余 `/`，也会被 TrimRight 去除。Bug #41 修复了 double `/v1` 问题。
**严重性：** None — 实现正确。

### Adapter 总体评估

- `ParseResponse` 正确处理 nil Usage 和 nil Choices ✅
- `ClassifyHTTPStatus` 覆盖 401(auth_error)、429(rate_limit)、4xx(client_error)、5xx(channel_failure) ✅
- `Test` 方法使用最小 payload（1 message, max_tokens=1） ✅
- `DefaultRegistry` 使用 `NormalizeType` 处理空 adapter_type ✅

---

## 总结

| 区域 | 发现数 | 严重性分布 |
|------|--------|-----------|
| Migration 安全性 | 2 | Low: 2 |
| Gateway Auth + RBAC | 0 | — |
| Billing Freeze | 0 | — |
| Model Governance | 1 | Low: 1（设计权衡） |
| Project Isolation | 0 | — |
| Audit Completeness | 1 | Low: 1 |
| Adapter 正确性 | 0 | — |

**结论：无 High/Critical 发现。** PRD2 batch 1-9 的后端代码在安全、数据完整性、权限隔离方面实现正确。4 个 Low 发现均为历史数据/非核心操作的补全建议，不影响 PRD2 功能正确性或安全性。
