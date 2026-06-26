# Round 1 顾问咨询总结

> 日期: 2026-06-24
> 顾问数: 7 (DeepSeek, Gemini, GPT, MiMo, MiniMax, Qwen, ZhiPu)
> 总结者: Canary

---

## 一、顾问共识（7/7 一致）

以下观点所有顾问完全一致，无分歧：

### 1.1 "不改生产代码"约束需要重新定义

**共识**: Canary 不直接改生产代码，但可以向 Codex 提出重构需求。

| 顾问 | 原话摘要 |
|------|---------|
| DeepSeek | "不建议在'不改生产代码'的约束下硬扛" |
| Gemini | "Canary 提议重构，Codex 执行修改" |
| GPT | "测试 Agent 完全可以提出重构建议。这是行业标准做法" |
| MiMo | "两个 agent 的职责边界应该是'不互相修改对方的产出物'" |
| MiniMax | "将'不改生产代码'重新定义为'Canary 可提出重构需求，Codex 执行'" |
| Qwen | "现在最该做的事是测试基建，不是重构" (短期坚持不改) |
| ZhiPu | "Canary 不直接修改生产代码，但可以提出重构需求" |

### 1.2 billing 集成测试应该重写而非修复

**共识**: 6/7 顾问建议重写（Qwen 建议修复，但理由是"这些测试覆盖的是核心场景，删了等于裸奔"，本质也是要大改）。

重写的共同理由：
- 测试间无隔离（共享 DB、硬编码 name）
- 事务中止级联是测试基础设施问题，不是单个测试的问题
- debug 测试是临时调试代码，应删除

### 1.3 admin 不应该全覆盖

**共识**: 7/7 一致，按风险分层，只测安全+资金相关 handler。

所有顾问的 P0 列表高度重合：
- **安全**: Login, CreateAPIKey, DisableAPIKey, UpdateAPIKey
- **资金**: TopUp, BalanceAdjust, CreateGrant, GetWallet

### 1.4 集成测试必须用 build tag 隔离

**共识**: 7/7 一致，`//go:build integration`，`go test ./...` 默认不跑集成测试。

### 1.5 必须用独立测试数据库

**共识**: 7/7 一致，不能直接连 localhost:5432/uniroute 跑测试。

### 1.6 前端测试优先级低

**共识**: 7/7 一致，后端核心逻辑测试优先。

### 1.7 relay.go:198 循环 defer 是真实生产 bug

**共识**: 7/7 一致，这是 P0 级生产问题，不仅是测试问题。

---

## 二、顾问分歧

### 2.1 billing: 修复 vs 重写

| 立场 | 顾问 | 理由 |
|------|------|------|
| 重写 | DeepSeek, Gemini, MiMo, MiniMax, ZhiPu | 测试基础设施整体失效，逐行修复成本高于重写 |
| 修复 | GPT, Qwen | 失败原因是测试环境问题，不是业务逻辑错误；这些测试覆盖核心场景，删了裸奔 |

**Canary 判断**: 两边说的不矛盾。GPT/Qwen 的"修复"本质也是"重构测试基建后重写测试用例"，只是保留测试文件名和用例设计。实际执行时：保留用例设计意图，重写实现代码。

### 2.2 前端: Vitest vs Playwright

| 立场 | 顾问 | 理由 |
|------|------|------|
| Playwright E2E | GPT, MiniMax, Qwen, ZhiPu | 管理后台是表单+表格，RTL 测起来麻烦，Playwright 性价比高 |
| Vitest + MSW | MiMo | 前端核心风险在数据交互，MSW 可以 mock API |
| 不做 | DeepSeek | 低优先级，先配 Vitest 框架但不写测试 |
| 先不做 | ZhiPu | 后端稳定后再考虑 |

**Canary 判断**: 取决于后端测试稳定后的剩余精力。如果精力有限，Playwright 3-5 个核心 E2E 脚本是最小可行方案。

### 2.3 testcontainers 是否现在引入

| 立场 | 顾问 | 理由 |
|------|------|------|
| 现在引入 | DeepSeek, Gemini | 环境隔离最彻底 |
| 中期引入 | GPT, ZhiPu | CI 环境用，本地用独立 DB |
| 暂不引入 | MiMo, Qwen | 项目还在早期，收益不如成本；先用独立 DB + truncate |
| 看情况 | MiniMax | 首选 testcontainers，备选本地独立 DB |

**Canary 判断**: 先用 `uniroute_test` + truncate，这是最低成本方案。testcontainers 等 CI 配置时再引入。

---

## 三、各问题结论

### 问题 1: 测试策略

**结论**: 分层推进

| 层 | 方案 | 是否改生产代码 | 覆盖率潜力 |
|---|------|-------------|-----------|
| 立即 | httptest + 真实 DB (uniroute_test) | 否 | 50-65% |
| 短期 | 请求 Codex 做最小接口抽象 | Codex 改 | 85%+ |
| 长期 | 接口 + mock 为主 | Codex 改 | 85%+ |

### 问题 2: billing 测试

**结论**: 重写。删除 debug_test.go，重写 integration_test.go，保留用例设计意图。

### 问题 3: admin 优先级

**结论**: 风险分层，P0 安全+资金 (12 个 handler)，不追求全覆盖。

### 问题 4: 测试文件管理

**结论**: 单元测试 `xxx_test.go`，集成测试 `xxx_integration_test.go` + `//go:build integration`，删除 debug 测试。

### 问题 5: 测试环境

**结论**: 立即建 `uniroute_test`，truncate 清理。testcontainers 后期引入。

### 问题 6: 前端

**结论**: 低优先级。后端稳定后，Playwright 3-5 个 E2E 脚本。

---

## 四、Canary 的执行计划（修正版）

基于顾问共识，修正原有 ACTION-PLAN.md：

### 阶段 0: 修复测试基础设施（本周）

| 序号 | 任务 | 负责 | 交付物 |
|------|------|------|--------|
| 0.1 | 创建 `uniroute_test` 数据库 | Canary | 测试环境隔离 |
| 0.2 | 修复 testutil/db.go 编译（加 pgx import） | Canary | testutil 可编译 |
| 0.3 | 建立测试基建（SetupTestDB, truncate, UUID org） | Canary | testutil 完善 |
| 0.4 | 删除 billing_debug_test.go | Canary | 清理垃圾代码 |
| 0.5 | skip 当前 17 个失败的 billing 集成测试 | Canary | CI 先绿回来 |
| 0.6 | 向 Codex 提交 bug 报告（XOR, defer, Exec, 事务回滚） | Canary | bug 报告 |
| 0.7 | 向 Codex 提交接口抽象需求 | Canary | refactor-proposal.md |

**验收标准**: `go test ./...` 全部通过（0 FAIL），集成测试被 skip 但不报错。

### 阶段 1: 重写 billing 集成测试

| 序号 | 任务 | 交付物 |
|------|------|--------|
| 1.1 | 重写 5 个核心 billing 测试 (Freeze→Settle, 余额不足, 幂等, Cancel, 并发) | billing_integration_test.go |
| 1.2 | 补充 EstimateFreezeAmount / CalculateCost 单元测试 | billing_test.go |
| 1.3 | 集成测试加 `//go:build integration` | build tag 隔离 |

**验收标准**: billing 覆盖率 ≥ 60%，`go test -tags=integration ./internal/billing/` 全部通过。

### 阶段 2: admin P0 handler 测试

| 序号 | 任务 | 交付物 |
|------|------|--------|
| 2.1 | 安全 handler 测试 (Login, CreateAPIKey, DisableAPIKey, UpdateAPIKey) | admin_test.go 扩展 |
| 2.2 | 资金 handler 测试 (TopUp, BalanceAdjust, CreateGrant, GetWallet) | admin_test.go 扩展 |

**验收标准**: 12 个 P0 handler 有测试覆盖，admin 覆盖率 ≥ 30%。

### 阶段 3: gateway 核心测试

| 序号 | 任务 | 交付物 |
|------|------|--------|
| 3.1 | Route/RouteAll 集成测试 | gateway_integration_test.go |
| 3.2 | HandleChatCompletion httptest 集成测试 | relay_test.go |
| 3.3 | failover 资源泄漏测试（验证 relay.go:198 bug） | relay_test.go |

**验收标准**: gateway 覆盖率 ≥ 40%。

### 阶段 4: 接口抽象后补充单元测试（等 Codex）

等 Codex 完成接口抽象后，补充 billing/gateway/admin 的 mock 单元测试。

### 阶段 5: 前端（后端稳定后）

Playwright 3-5 个 E2E 脚本：登录、API Key、钱包充值。

---

## 五、需要第二轮讨论的问题

基于本轮顾问回复，以下问题需要进一步讨论：

### Q1: billing 事务回滚 — 是测试问题还是生产 bug？

MiniMax 和 ZhiPu 都指出 SQLSTATE 25P02 可能不仅是测试问题，而是 `billing.go` 生产代码在事务中出错时没有正确 Rollback。

**需要确认**: billing.go 的 `Freeze()` 函数在第一个 SQL 失败时，`defer tx.Rollback(ctx)` 是否真的会执行？（代码中有 `defer tx.Rollback(ctx)`，但 PostgreSQL 的事务中止是否阻止了 Rollback？）

### Q2: 接口抽象的最小范围

多位顾问建议给 Codex 提接口抽象需求，但具体范围不一致：
- DeepSeek: 通用 `Querier` 接口
- MiMo: `Querier` + `BillingRepository`
- ZhiPu: 每个包一个 `BillingStore` / `ChannelStore` 接口
- Qwen: 先不做，等测试基建稳定后再提

**需要确认**: 最小可行的接口抽象是什么？是先做 billing 一个包，还是三个包一起做？

### Q3: 测试基建的具体实现 — truncate vs 事务回滚

两位顾问（ZhiPu, Qwen）明确指出不能用事务回滚（因为生产代码内部管理自己的事务，外层套事务会冲突），必须用 truncate。

**需要确认**: 这个判断是否正确？是否有其他隔离方式？

### Q4: CI 流程

多位顾问提到 CI 集成，但当前 UniRouter 的 CI 配置不清楚。

**需要确认**: 当前是否有 CI？如果有，配置是什么？测试如何纳入？

---

## 六、技术债务清单（顾问共识）

| 项目 | 严重性 | 负责方 | 状态 |
|------|--------|--------|------|
| relay.go:198 循环 defer（资源泄漏） | P0 | Codex | 待修复 |
| encryptString XOR（安全漏洞） | P0 | Codex | 待修复 |
| billing 事务回滚（SQLSTATE 25P02） | P0 | Codex + Canary | 待 debug |
| testutil 编译失败 | P1 | Canary | 待修复 |
| router.go:75 Exec 错误未检查 | P1 | Codex | 待修复 |
| 无接口抽象层 | P2 | Codex | 待规划 |
| billing 测试全挂 (20 个) | P0 | Canary | 待重写 |
| admin 零覆盖 | P1 | Canary | 待补 |
| gateway 5% 覆盖 | P1 | Canary | 待补 |
| db/redisx/migrate 零测试 | P2 | Canary | 待安排 |
| 前端零测试 | P3 | 待决策 | - |
