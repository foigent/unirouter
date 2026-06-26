# UniRouter 测试执行计划

> 维护者: Canary (测试 agent)
> 创建日期: 2026-06-24
> 最后更新: 2026-06-24

## 原则

1. **不改生产代码** — 只写测试和报告。需要接口抽象的，出技术需求文档给 Codex
2. **先基线后改进** — 每阶段先跑当前状态，再补测试
3. **风险驱动** — 安全+资金 > 核心业务 > CRUD > 前端
4. **可量化** — 每阶段有明确覆盖率目标和通过率
5. **集成测试隔离** — 用 build tag `//go:build integration` 隔离需要 DB 的测试

## 当前基线 (2026-06-24)

| 包 | 行数 | 当前覆盖率 | 测试文件 | 测试数 | 状态 |
|---|------|-----------|---------|-------|------|
| filter | 95 | 100% | 2 | 12 | ✅ |
| tokenutil | 74 | 93.5% | 2 | 13 | ✅ |
| circuitbreaker | 138 | 93.0% | 2 | 25 | ✅ |
| config | 153 | 95.8% | 1 | 16 | ✅ |
| auth | 151 | 86.0% | 2 | 31 | ✅ |
| middleware | 340 | 40.0% | 2 | 36 | ⚠️ |
| models | 197 | 0% | 1 | 9 | ⚠️ 只有序列化 |
| gateway | 1016 | 5.0% | 2 | 22 | ❌ |
| billing | 523 | 0.5% | 3 | 7/27 | ❌ 20个失败 |
| admin | 2242 | 0.5% | 1 | 7 | ❌ |
| db | 65 | 0% | 0 | 0 | ❌ |
| redisx | 76 | 0% | 0 | 0 | ❌ |
| migrate | 90 | 0% | 0 | 0 | ❌ |
| **整体** | **5160** | **~22%** | **18** | **163** | |

## 阶段计划

### 阶段 0: 修复阻断性问题 (P0)

**目标**: 消除编译失败和测试 panic，建立可运行的测试基线

| 任务 | 文件 | 具体工作 | 预期结果 |
|------|------|---------|---------|
| 修复 testutil 编译 | `internal/testutil/db.go:43` | 添加 `pgx` import | `go build ./internal/testutil/` 通过 |
| Debug billing 集成测试 | `billing_debug_test.go` | 跑 TestDebug_DBConnection + TestDebug_FreezeSimple 找根因 | 确定是 schema 不匹配还是 seed 问题 |
| 修复 billing 测试 panic | `billing_debug_test.go:131` | result nil check 在 result.Success 访问之前 | 不再 panic |
| 重新跑全量测试 | - | `go test ./... -cover` | 0 FAIL, 0 panic |

**验收标准**: `go test ./...` 全部通过（不含 integration build tag 的测试）

### 阶段 1: billing 核心逻辑 (P1)

**目标**: billing 覆盖率 0.5% → 60%+

| 任务 | 函数 | 测试类型 | 用例 |
|------|------|---------|------|
| Freeze 正常路径 | `Freeze()` | 集成 | 成功冻结、余额变化正确 |
| Freeze 边界 | `Freeze()` | 集成 | 余额不足、最小冻结金额 |
| Freeze 幂等 | `Freeze()` | 集成 | 相同 request_id 不重复扣费 |
| Settle 正常 | `Settle()` | 集成 | 结算+退款 |
| Settle 边界 | `Settle()` | 集成 | 未找到、重复结算 |
| Cancel | `Cancel()` | 集成 | 取消冻结 |
| EstimateFreezeAmount | `EstimateFreezeAmount()` | 单元 | 各种 model/token 组合 |
| CalculateCost | `CalculateCost()` | 单元 | 基本计算、最小 1 分 |
| QueryModelPrice | `QueryModelPrice()` | 集成 | 找到、未找到 fallback |
| AlertService | `alert.go` | 单元 | 阈值触发、Webhook 发送 |

**依赖**: 需要 Codex 修复 testutil 编译问题
**验收标准**: billing 覆盖率 ≥ 60%，所有新增测试通过

### 阶段 2: gateway 核心路由 (P2)

**目标**: gateway 覆盖率 5% → 40%+

| 任务 | 函数 | 测试类型 | 用例 |
|------|------|---------|------|
| Route 正常 | `Route()` | 集成 | 找到可用 channel、model mapping |
| Route 熔断 | `Route()` | 集成 | half_open 10% 流量、无可用 channel |
| RouteAll | `RouteAll()` | 集成 | 多 channel 排序、failover |
| MarkChannelSuccess/Fail | `MarkChannelSuccess/Fail()` | 集成 | 熔断状态转换 |
| HandleChatCompletion | `HandleChatCompletion()` | 集成 | 非流式转发、流式 SSE |
| HandleChatCompletion 错误 | `HandleChatCompletion()` | 集成 | 无路由、上游失败、余额不足 |
| HandleEmbeddings | `HandleEmbeddings()` | 集成 | 正常转发 |
| relay 循环 defer bug | `relay.go:198` | 集成 | 多 channel failover 时资源关闭 |
| estimateTokens 边界 | `estimateTokens()` | 单元 | 空消息、CJK、超长 |

**依赖**: 需要真实 DB + 上游 mock (httptest)
**验收标准**: gateway 覆盖率 ≥ 40%

### 阶段 3: admin handlers (P2)

**目标**: admin 覆盖率 0.5% → 30%+

**handler 分类**:

| 类别 | handlers | 优先级 | 理由 |
|------|----------|--------|------|
| 安全 | Login, CreateAPIKey, DisableAPIKey, UpdateAPIKey | P0 | 认证绕过=全系统沦陷 |
| 资金 | TopUp, BalanceAdjust, CreateGrant, GetWallet | P0 | 资金操作出错=直接损失 |
| 核心业务 | CreateChannel, UpdateChannel, DeleteChannel, TestChannel | P1 | 渠道管理影响转发 |
| CRUD | List*, Get*, 各种分页查询 | P2 | 逻辑简单，测试 ROI 低 |
| 敏感词 | *SensitiveWords | P2 | 功能正确性 |

**测试方式**: httptest.NewRequest + httptest.NewRecorder + 真实 DB
**依赖**: 需要 Codex 提供 DB 接口抽象（出技术需求文档）
**验收标准**: admin 覆盖率 ≥ 30%，安全+资金 handler 100% 覆盖

### 阶段 4: 基础设施层 (P3)

**目标**: db/redisx/migrate 从 0% 到可运行

| 包 | 测试策略 | 覆盖率目标 |
|---|---------|-----------|
| db | 连接池配置、健康检查 | 50% |
| redisx | 连接、基本操作 | 50% |
| migrate | 迁移脚本执行 | 验证可运行 |

**依赖**: testcontainers-go 或真实 DB/Redis
**验收标准**: 各包有基本测试覆盖

### 阶段 5: 前端 (P3)

**目标**: 关键页面有测试覆盖

| 任务 | 具体工作 |
|------|---------|
| 配置 Vitest + RTL | `vitest.config.ts`, `@testing-library/react` |
| 测试关键页面 | Login, Dashboard, Channel 管理, Wallet 操作 |
| 覆盖率目标 | 关键页面 50%+ |

**依赖**: 后端 P0-P2 完成
**验收标准**: 关键页面有测试覆盖

## 文档维护

每个阶段完成后更新：
- `docs/testing/TEST-STATUS.md` — 覆盖率 + 用例统计
- `docs/testing/COVERAGE.md` — 覆盖率详细数据
- `docs/testing/BUG-LOG.md` — Bug 记录
- `MEMORY.md` — 关键发现索引

## 当前阶段

**阶段**: 0 — 修复阻断性问题
**状态**: 待执行
**上次更新**: 2026-06-24
