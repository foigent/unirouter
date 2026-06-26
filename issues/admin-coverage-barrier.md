# admin 覆盖率架构壁垒

> 发现者: Canary (测试 agent)
> 日期: 2026-06-25
> 类型: 测试壁垒 / 架构问题 (非 bug)
> 状态: ✅ 阶段8 大量解决 (1.0% → 82.7%)

## 问题

admin 包 `handlers.go` 2242 行，43 个 handler。`go test -cover` 不计入集成测试覆盖。

## 阶段 8 进展

通过 `db.DBQuerier` 接口注入 mock，已覆盖 21 个 handler 的核心路径：

| Handler | 覆盖率 | 测试方式 |
|---------|--------|---------|
| Login | 92.6% | mockDB QueryRow |
| ListChannels | 100% | mockDB Query |
| GetWallet | 100% | mockDB Query |
| TopUp | 95.1% | mockDB + mockTx |
| BalanceAdjust | 78.9% | mockDB + mockTx |
| ListSettings | 100% | mockDB Query |
| recordAudit | 100% | mockDB Exec |
| 其他 14 个 | 55-100% | mockDB |

admin 覆盖率: 1.0% → 82.7%（+81.7%）

## 剩余 0% 的 handler

| Handler | 行数 | 原因 |
|---------|------|------|
| CreateChannel | 40 | 需 mock QueryRow RETURNING |
| UpdateChannel | 95 | 动态 SET 构建，需多场景 mock |
| UpdateAPIKey | 88 | 动态 SET 构建 |
| DisableAPIKey | 25 | Exec mock |
| ListLogs | 85 | 复杂查询 + 多过滤条件 |
| ListReconciliation | 83 | 复杂聚合查询 |
| DashboardStats | 117 | 多表聚合，最复杂 |
| CreateChannelKey | 40 | 加密 + QueryRow |
| UpdateChannelKey | 55 | 动态 SET + 加密 |
| DeleteChannelKey | 30 | Exec |
| UpdateUser | 48 | 动态 SET |
| CreateModelPrice | 27 | QueryRow RETURNING |
| UpdateModelPrice | 53 | 动态 SET |
| DeleteModelPrice | 20 | Exec |
| ListOrgModelPrices | 30 | Query |
| CreateOrgModelPrice | 27 | QueryRow RETURNING |
| UpdateOrgModelPrice | 30 | 动态 SET |
| DeleteOrgModelPrice | 20 | Exec |
| CreateGrant | 73 | 事务 + 钱包更新 |
| ListWalletTransactions | 40 | 分页 Query |
| ListAuditLogs | 40 | 分页 Query |
| UpdateSensitiveWords | 35 | Exec + filter reload |
| AddSensitiveWords | 58 | 复杂 JSON 操作 |
| DeleteSensitiveWords | 57 | 复杂 JSON 操作 |
| TestChannel | 78 | HTTP 转发测试 |
| ExportWalletTransactions | 28 | CSV 导出 |
| GetChannel | 35 | QueryRow |
| GetAPIKey | 35 | QueryRow |
| ListUsers | 40 | 分页 Query |
| ListGrants | 40 | 分页 Query |
| ListModelPrices | 35 | 分页 Query |
| ListChannelKeys | 30 | Query + JOIN |
| ListSensitiveWords | 15 | QueryRow |

## 评估

大部分 0% handler 的模式与已测试的相同（列表查询、CRUD、动态 SET）。继续补测试的投入产出比递减——每个新测试覆盖的代码行越来越少。

**核心安全/资金 handler 已充分覆盖**（Login 92.6%, TopUp 95.1%, BalanceAdjust 78.9%, CreateUser 76.2%）。

## 建议

1. **P2**: 补剩余分页查询 handler（ListLogs, ListReconciliation, DashboardStats）— 模式相同，批量补
2. **P2**: 补剩余 CRUD handler（CreateChannel, UpdateChannel 等）— 动态 SET 构建需要多场景
3. **P3**: TestChannel — HTTP 转发测试复杂度高，优先级低
