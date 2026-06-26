# UniRouter 测试咨询 — Round 1

> 日期: 2026-06-24
> 提交者: Canary (测试 agent)
> 目的: 测试策略咨询，不包含 Canary 的预判

---

## 一、项目背景

### 项目概述

UniRouter 是一个 API 网关/路由器，将 AI 模型请求（OpenAI Chat Completions 格式）转发到多个上游渠道。核心功能：
- 多渠道路由 + failover
- 预冻结计费（先冻结余额，用完后结算）
- API Key 认证 + IP 白名单 + 模型权限
- 管理后台（前端 React + 后端 Go）

### 技术栈

| 层 | 技术 |
|---|------|
| 后端 | Go 1.22 + Gin + pgx + go-redis |
| 前端 | React 18 + Vite + Semi Design |
| 数据库 | PostgreSQL (localhost:5432, db: uniroute) |
| 缓存 | Redis (localhost:6379) |

### 代码规模

| 包 | 行数 | 函数数 | 职责 |
|---|------|-------|------|
| admin/handlers.go | 2242 | 52 | 管理后台 API handlers |
| gateway/relay.go | 787 | 13 | 请求转发（流式+非流式） |
| billing/billing.go | 344 | 8 | 预冻结计费逻辑 |
| billing/alert.go | 179 | - | 预算告警 |
| gateway/router.go | 229 | 7 | 渠道路由+熔断 |
| middleware/auth.go | 212 | 10 | 认证中间件 |
| models/models.go | 197 | - | 数据模型 |
| config/config.go | 153 | - | 配置加载 |
| auth/auth.go | 151 | 10 | JWT + API Key 认证 |
| circuitbreaker/breaker.go | 138 | - | 熔断器 |
| middleware/ratelimit.go | 107 | - | 限流 |
| filter/filter.go | 95 | - | 敏感词过滤 (DFA) |
| migrate/migrate.go | 90 | - | 数据库迁移 |
| redisx/redis.go | 76 | - | Redis 操作 |
| tokenutil/tokenutil.go | 74 | - | Token 估算 |
| db/postgres.go | 65 | - | 数据库连接池 |
| **总计** | **5160** | | |

### 数据库表 (15 个)

orgs, users, api_keys, channels, channel_keys, wallets, grants, transactions, freeze_idempotency, call_logs, model_prices, org_model_prices, settings, audit_logs, schema_migrations

### 依赖关系

所有核心包都直接依赖 `*pgxpool.Pool`，没有接口抽象层：

```
admin/handlers.go  → *pgxpool.Pool (直接)
gateway/relay.go   → *pgxpool.Pool (直接, 7 处 DB 调用)
gateway/router.go  → *pgxpool.Pool (直接, 6 处 DB 调用)
billing/billing.go → *pgxpool.Pool (直接)
middleware/auth.go → *pgxpool.Pool (直接, 查询 api_keys)
```

---

## 二、测试现状

### 测试基线

| 包 | 行数 | 覆盖率 | 测试文件 | 测试数 | 通过 | 失败 |
|---|------|--------|---------|-------|------|------|
| filter | 95 | 100% | 2 | 12 | 12 | 0 |
| tokenutil | 74 | 93.5% | 2 | 13 | 13 | 0 |
| circuitbreaker | 138 | 93.0% | 2 | 25 | 25 | 0 |
| config | 153 | 95.8% | 1 | 16 | 16 | 0 |
| auth | 151 | 86.0% | 2 | 31 | 31 | 0 |
| middleware | 340 | 40.0% | 2 | 36 | 36 | 0 |
| models | 197 | 0% | 1 | 9 | 9 | 0 |
| gateway | 1016 | 5.0% | 2 | 22 | 22 | 0 |
| billing | 523 | 0.5% | 3 | 27 | 7 | 20 |
| admin | 2242 | 0.5% | 1 | 7 | 7 | 0 |
| db | 65 | 0% | 0 | 0 | 0 | 0 |
| redisx | 76 | 0% | 0 | 0 | 0 | 0 |
| migrate | 90 | 0% | 0 | 0 | 0 | 0 |
| **整体** | **5160** | **~22%** | **18** | **163** | **143** | **20** |

### 测试分类

**真实业务逻辑测试**（验证代码正确性）:
- filter: 敏感词匹配
- tokenutil: Token 估算
- circuitbreaker: 熔断器状态转换
- config: 配置加载和验证
- auth: JWT 生成验证、API Key 认证
- middleware: IP 白名单、CORS、限流
- gateway: resolveModelMapping、estimateTokens（纯函数）

**结构性测试**（不验证业务逻辑）:
- admin: writeJSON 辅助函数、NewHandler 构造、nil DB panic
- billing: 结构体字段验证 (TestFreezeRequest_Fields 等)
- models: 结构体序列化

**集成测试**（需要 DB，当前全挂）:
- billing: 17 个集成测试 (Freeze/Settle/Cancel/QueryModelPrice)
- billing: 3 个 debug 测试

---

## 三、当前测试失败

### 失败 1: testutil 编译失败

```
$ go test ./internal/testutil/
internal/testutil/db.go:43:49: undefined: pgx
```

文件 `internal/testutil/db.go` 第 43 行使用 `pgx.Tx` 但只 import 了 `pgxpool`，缺少 `"github.com/jackc/pgx/v5"` 导入。

### 失败 2: billing debug 测试

```
$ go test ./internal/billing/ -run "TestDebug" -v

TestDebug_FreezeSimple:
  billing_debug_test.go:44: create org: ERROR: duplicate key value violates
  unique constraint "orgs_name_key" (SQLSTATE 23505)

TestDebug_FreezeActual:
  panic: runtime error: invalid memory address or nil pointer dereference
  billing_debug_test.go:131
```

### 失败 3: billing 集成测试

```
$ go test ./internal/billing/ -run "TestFreeze_Success" -v

TestFreeze_Success:
  billing_integration_test.go:120: freeze error: query effective granted:
  ERROR: current transaction is aborted, commands ignored until end of
  transaction block (SQLSTATE 25P02)
```

所有 17 个集成测试都报同样的 SQLSTATE 25P02 错误。

### 数据库状态

`freeze_idempotency` 表存在，schema 包含 `request_id` (PK), `org_id`, `frozen_amount`, `status` 等字段。
`grants` 表存在，schema 包含 `org_id`, `balance`, `status`, `expires_at` 等字段。
两个表当前都是空的。

### 代码问题

| # | 文件 | 行 | 问题 |
|---|------|-----|------|
| 1 | relay.go | 198 | 循环内 defer，failover 时 resp.Body 直到函数返回才关闭 |
| 2 | router.go | 75 | `r.db.Exec()` 返回值未检查 |
| 3 | encryptString | - | XOR 加密，CONTEXT_HANDOFF.md 记录应为 AES-GCM |
| 4 | billing_debug_test.go | 131 | `result.Success` 在 `err != nil` 检查之前访问，result 为 nil 时 panic |

---

## 四、测试约束

- 测试 agent (Canary) 的职责约束是**不改生产代码**，只写测试和报告
- 开发 agent (Codex) 负责生产代码的修改
- 两个 agent 独立工作，通过文档和报告沟通

---

## 五、需要讨论的问题

### 问题 1: 测试策略 — "不改生产代码"约束下的可行方案

gateway/billing/admin 三个核心包都直接依赖 `*pgxpool.Pool`，没有接口抽象层。

在不改生产代码的前提下，测试这三个包的最佳策略是什么？

可选方案：
- A: 集成测试（真实 DB）— 不改生产代码，但测试慢、需要 seed 数据、测试间可能互相影响
- B: 接口抽象 + mock — 需要改生产代码（或由 Codex 改），测试快、隔离好
- C: 只测纯逻辑函数 — 不改代码，但覆盖率天花板很低（gateway 当前 5%）
- D: httptest HTTP 集成 — 测试 API 层面，仍需 DB

是否应该坚持"不改生产代码"约束？还是允许测试 agent 提出接口抽象的重构建议（由 Codex 执行）？

### 问题 2: billing 集成测试全挂 — 修复还是重写

17 个集成测试 + 3 个 debug 测试全部失败。失败模式包括唯一键冲突、事务中止级联、nil 指针 panic。

这些测试是否有修复价值？还是应该重写？如果修复，应该从哪里入手？

### 问题 3: admin 测试的优先级

admin/handlers.go 有 2242 行、52 个 handler。当前只有 7 个测试（全是辅助函数）。

52 个 handler 是否应该全覆盖？还是只测安全+资金相关的？

handler 分类：
- 安全 (5): Login, CreateAPIKey, DisableAPIKey, UpdateAPIKey, GetAPIKey
- 资金 (7): TopUp, BalanceAdjust, GetWallet, ListWalletTransactions, ExportWalletTransactions, CreateGrant, ListGrants
- 渠道 (6): CreateChannel, UpdateChannel, DeleteChannel, GetChannel, ListChannels, TestChannel
- 渠道密钥 (4): CreateChannelKey, UpdateChannelKey, DeleteChannelKey, ListChannelKeys
- 用户 (4): CreateUser, UpdateUser, DeleteUser, ListUsers
- 模型价格 (8): CreateModelPrice, UpdateModelPrice, DeleteModelPrice, ListModelPrices, ListOrgModelPrices, CreateOrgModelPrice, UpdateOrgModelPrice, DeleteOrgModelPrice
- 查询 (10): ListLogs, ListReconciliation, ListSettings, UpdateSetting, DashboardStats, ListSensitiveWords, UpdateSensitiveWords, AddSensitiveWords, DeleteSensitiveWords, ListAuditLogs
- 内部 (8): NewHandler, parsePagination, maskKey, writeJSON, joinStrings, recordAudit, CleanupOldData

### 问题 4: 测试文件管理

当前 billing 有 3 个测试文件（billing_test.go, billing_debug_test.go, billing_integration_test.go），命名不一致。

测试文件命名规范应该是什么？集成测试是否应该用 build tag 隔离（`//go:build integration`）？

Codex 写的验收测试文档（`docs/ops/test-plan.md`, `docs/ops/test-report.md`）和单元/集成测试文档（`docs/testing/`）如何共存？

### 问题 5: 测试环境管理

测试直接连接真实 PostgreSQL (localhost:5432, db: uniroute)，没有隔离。数据库有 15 个表，部分有数据。

测试是否应该用独立的测试数据库（如 `uniroute_test`）？是否应该引入 testcontainers-go？测试数据清理策略是什么？

### 问题 6: 前端测试优先级

22 个 TS/TSX 文件，零测试，没有 Vitest 配置。前端是管理后台，运维人员每天使用。

前端测试的优先级如何？如果做，应该覆盖哪些页面？用什么方式（Vitest + RTL？Playwright E2E？）？

---

**不需要讨好，有什么其他建议可以提出**
**请基于实际情况和最佳实践，在深度全面审慎的思考，不要幻觉不要猜测不要偷懒的详细回复**
