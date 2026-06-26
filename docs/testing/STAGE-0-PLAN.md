# 阶段 0 执行计划 — 修复测试基础设施

> 创建: 2026-06-24 12:25
> 最后更新: 2026-06-24 22:54
> 状态: ✅ 已完成
> 目标: 消除编译失败和测试 panic，建立可运行的测试基线

## 待办步骤

- [x] 0.1 创建 `uniroute_test` 数据库
- [x] 0.2 修复 testutil/db.go 编译（加 pgx import）
- [x] 0.3 建立测试基建（SetupTestDB, truncate, UUID org）
- [x] 0.4 Debug billing 25P02 根因（跑 TestDebug_DBConnection，找第一个失败 SQL）
- [x] 0.5 删除 billing_debug_test.go
- [x] 0.6 重写 billing 集成测试（12 个集成测试 + 7 个单元测试，共 19 PASS）— 经 4 轮 Codex 修复验收后全部通过
- [x] 0.7 跑全量测试确认 0 FAIL — `go test ./...` 10/10 包 PASS
- [x] 0.8 写 admin handler 测试 POC（验证 httptest + DB 方案可行）
- [x] 0.9 向 Codex 提交 bug 报告（XOR, defer, Exec, 事务回滚）— 经 4 轮 FIX-SIGNAL/VERIFY-RESULT 全部 PASS
- [ ] 0.10 向 Codex 提交接口抽象需求（refactor-proposal.md，含具体接口签名）
- [x] 0.11 更新 TEST-STATUS.md / COVERAGE.md / BUG-LOG.md — 2026-06-24 22:54 完成

## 关键决策

- 0.1: `uniroute` 用户无 CREATEDB 权限，先用 postgres 超级用户 `ALTER USER uniroute CREATEDB`，再创建数据库。Migration 用 psql 逐个执行 .up.sql 文件（不能用 go run 因为 internal 包限制）。
- 0.2: 添加 `"github.com/jackc/pgx/v5"` import 到 testutil/db.go。`go build` 和 `go vet` 均通过。
- 0.3: 新增 `testutil/testdb.go`，包含 SetupTestDB/TruncateAll/NewTestOrg/NewTestWallet/NewTestGrant/NewTestModelPrice。使用 `uniroute_test` 数据库，UUID 命名避免冲突。引入 `stretchr/testify` 依赖。go.mod 升级到 go 1.25（go mod tidy 自动升级）。
- 0.4: Debug 结果 — 25P02 在新测试库上也复现，确认是**生产代码 bug**，不是测试环境问题。`billing.go:68` 用 `input_cost_cents` / `model` 但 DB 列名是 `input_price_cents` / `model_name`。`QueryModelPrice()` 查询永远失败，fallback 到默认价格 (2/10)。`Freeze()` 内 `EstimateFreezeAmount()` 调用 `QueryModelPrice()` 失败后在同一事务内继续执行后续 SQL，触发 25P02 级联。**这是一个 P1 生产 bug** — 计费使用默认价格而非配置价格。
- 0.5-0.6: 随 Bug #0/#0.1/#0/#3/#4/#5 经 Codex 4 轮修复全部 PASS，billing 集成测试全绿（19/19）。
- 0.8: admin httptest + 真实 DB 方案可行。Login/TopUp/GetWallet 共 11 个测试全部通过。关键点：需要 `t.Context()` 而非 `context.Background()`；TopUp 不需要设 context（handler 从 request 取 wallet）；Login 需要用 `bcrypt.GenerateFromPassword` 生成真实 hash。
- 0.10 (跳过): 接口抽象需求改为在阶段 2 gateway 测试时按需评估。当前不需要阻塞后续工作。
- 0.11: 2026-06-24 22:54 完成全部文档同步。

## 中断恢复

1. 读此文件了解上次做到哪
2. 用 `ls -la` 或 `git diff` 验证已完成步骤
3. 从第一个未完成的 `[ ]` 继续

## 阻塞项

| 阻塞 | 等待方 | 说明 |
|------|--------|------|
| 无 | - | 阶段 0 所有阻塞已解除，进入阶段 1 |
