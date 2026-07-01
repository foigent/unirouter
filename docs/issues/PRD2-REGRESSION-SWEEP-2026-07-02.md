# PRD2 Full-context Dynamic Regression Sweep — 深度测试补充报告

**审查者：** OpenClaw Canary
**日期：** 2026-07-02 02:57 CST
**方法：** 手动变异分析 + 错误路径追踪 + 测试断言质量审查

---

## 方法 1：手动变异分析

对 12 个关键函数注入假设性缺陷，检查现有测试能否抓住。

| # | 函数 | 变异 | 会被哪个测试抓住？ | 结果 |
|---|------|------|-------------------|------|
| 1 | `isUniqueViolation` | 改 `"23505"` → `"23506"` | `TestIsUniqueViolation` | ✅ 被抓 |
| 2 | `Freeze` idempotency | 删除 `isUniqueViolation` 检查 | `TestFreeze_ConcurrentDuplicate` | ✅ 被抓 |
| 3 | `CheckAccess` retired | 改 `LifecycleRetired` → `"deleted"` | `TestPRD2P0DeveloperProjectKeyGatewayE2E` (403 model_retired) | ✅ 被抓 |
| 4 | `projectAllowsModel` | 改 JOIN 条件用错表 | `TestPRD2P0DeveloperProjectKeyGatewayE2E` (403 project_model_not_allowed) | ✅ 被抓 |
| 5 | `NormalizeRole` | 改 `"admin"` → `"administrator"` | `TestNormalizeRole` + `TestRequireRoles` | ✅ 被抓 |
| 6 | `apiKeyAllowsModel` | 改 `"*"` → `"all"` | `TestChatHandler_AllowedModels_Bug_Wildcard` | ✅ 被抓 |
| 7 | `recordAuditEntry` | 删除 `project_id` 写入 | `TestPRD2P0DeveloperProjectKeyGatewayE2E` (audit project_id 断言) | ✅ 被抓 |
| 8 | `upstreamEndpoint` | 改 `TrimRight` → `TrimLeft` | `TestUpstreamEndpoint_DoesNotDoubleAppendV1` | ✅ 被抓 |
| 9 | `NormalizeUsageSource` | 改 `"api"` → `"API"` | `TestNormalizeUsageSource_LegacyAliases` | ✅ 被抓 |
| 10 | `SourceConfidence` | 改 `upstream` 值 `1.0` → `0.5` | `TestSourceConfidence` | ✅ 被抓 |
| 11 | Freeze grant priority | 改 `granted_balance` 优先 → `balance` 优先 | `TestFreeze_Success` (balance 断言) | ✅ 被抓 |
| 12 | Freeze `FOR UPDATE` | 删除锁 | `TestFreeze_ConcurrentDuplicate` (balance 只扣一次) | ✅ 被抓 |

**结论：12/12 关键变异均被现有测试覆盖。测试在关键路径上有效。**

---

## 方法 2：错误路径追踪

追踪 billing/gateway/modelgov 的错误传播链路。

### Finding E-1: Billing Settle 失败时静默继续（Medium）

**文件：** `internal/gateway/relay.go:322-324`（非流式），`relay.go:557-559`（流式）
**问题：** 当 `billing.Settle` 返回错误时，代码只 `reqLog.Error` 记录日志，然后继续发送成功响应给客户端。
**影响：**
- Freeze 金额保持 frozen 状态，不退还到 balance
- 客户端收到 200 响应，但钱包中该笔金额永久冻结
- `freeze_idempotency.status` 保持 `frozen`（不变成 `settled`）
**实际触发概率：** 低 — Settle 失败需要 DB 在 freeze 成功后、settle 时出错（极罕见）
**建议：** 增加 settle 重试或在 freeze_idempotency 上设置 TTL + 定期清理任务
**严重性：** Medium — 资金安全问题，但触发条件极罕见

### 其他错误路径评估

| 路径 | 错误处理 | 评估 |
|------|----------|------|
| Freeze 失败 → 请求拒绝 | 返回 503 + `insufficient_balance` | ✅ 正确 |
| RouteAll 失败 → 无可用渠道 | Cancel freeze + 返回 503 + 记录 `no_route` | ✅ 正确 |
| 上游请求失败 → failover | MarkChannelFail + continue 到下一个 route | ✅ 正确 |
| 上游 401/403 → key cooldown | MarkKeyCooldown 3600s | ✅ 正确 |
| 上游 429 → rate limit cooldown | MarkKeyCooldown 60s | ✅ 正确 |
| 所有渠道失败 → 最终回退 | Cancel freeze + 返回 502 + 记录 `upstream_failed` | ✅ 正确 |
| 响应解析失败 → 内部错误 | Cancel freeze + 返回 500 | ✅ 正确 |
| Model governance 403 | 返回 403 + reason，不进入 relay | ✅ 正确 |
| RBAC 拒绝 | 返回 403 + allowed_roles | ✅ 正确 |
| `recordAuditEntry` 失败 | `log.Printf` 记录，不阻断业务 | ✅ 正确（审计不应阻断业务） |

---

## 方法 3：测试断言质量审查

检查关键测试的断言是否真正验证了行为（而非只检查"不崩溃"）。

### PRD2 P0 Developer Gateway E2E 测试

**断言数量：** 16+ 个独立断言
**覆盖链路：**
- project key 创建 → `require.Equal(projectID)` + `require.NotEmpty(key)`
- gateway 调用 → `require.Equal(200)` + `require.Equal("gpt-4o", upstreamModel)` + `require.Equal(requestID)` + `require.Equal("Bearer upstream-test-key")`
- response → `require.Contains("_uniroute")`
- call_logs → 6 个字段断言（project_id, key_id, model, requested_model, status_code, usage_source, total_tokens）
- freeze_idempotency → `require.Equal("settled")`
- audit_logs → 4 个字段断言（project_id, user_id, source, name）
- RBAC 负向 → 4 个 `require.Equal(403)`

**质量评级：优秀** — 每个断言验证具体字段值，不是简单的"不崩溃"。

### Billing Concurrent Duplicate 测试

**断言数量：** 8+ 个独立断言
**覆盖：**
- 两个 freeze 调用都 `require.NoError` + `require.True(Success)`
- 两个返回相同 freeze amount
- `require.Equal(1, idemRows)` — 唯一性
- `require.Equal(1, freezeTxRows)` — 无重复交易
- `require.Equal(10000-result.Amount, balance)` — balance 只扣一次

**质量评级：优秀** — 直接验证资金安全不变量。

### 其他关键测试

| 测试 | 断言质量 | 评级 |
|------|----------|------|
| `TestNormalizeRole` | 检查每种映射的返回值 | 优秀 |
| `TestRequireRoles_DeniesBillingAdminForPlatformOnlyRoute` | 检查 403 + allowed_roles | 优秀 |
| `TestIsUniqueViolation` | 正例 + 反例 | 优秀 |
| `TestSourceConfidence` | 检查每种 source 的置信度值 | 优秀 |
| `TestCheckAccess_DeniesRetiredModel` | 检查 Allowed=false + Reason | 优秀 |
| `TestListAuditLogs_Filters` | 检查过滤参数正确性 | 良好 |

**未发现"假绿"测试。**

---

## 综合评估

| 维度 | 结果 | 详情 |
|------|------|------|
| 变异覆盖 | 12/12 关键变异被抓住 | 测试在关键路径上有效 |
| 错误路径 | 1 个 Medium 发现 | Settle 失败静默继续（极罕见触发条件） |
| 断言质量 | 优秀 | E2E 16+ 断言，billing 8+ 断言，无"假绿" |
| 阻断 Batch 10 | **无** | Settle 静默失败可在后续 hardening batch 处理 |

---

## 新发现

| ID | 标题 | 严重性 | 来源 |
|----|------|--------|------|
| PRD2-ERR-001 | Billing Settle 失败时静默继续，freeze 金额永久冻结 | Medium | 错误路径分析 |

**注意：** PRD2-ERR-001 的触发条件极罕见（DB 在 freeze 成功后、settle 时出错），且不影响客户端响应正确性（客户端仍收到 200）。可在 hardening batch 中通过 settle 重试或 freeze TTL 清理解决。
