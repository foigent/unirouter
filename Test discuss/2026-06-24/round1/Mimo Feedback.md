UniRouter 测试策略咨询 — 详细回复


问题 1: "不改生产代码"约束下的测试策略

先回答核心问题：应该允许测试 agent 提出重构建议

"不改生产代码"约束在当前架构下会导致严重的覆盖天花板。这不是一个观点，而是结构性事实：


gateway/relay.go 787 行中有 7 处直接 DB 调用，纯逻辑函数只有 resolveModelMapping 和 estimateTokens 两个
billing/billing.go 344 行中 8 个函数全部依赖 *pgxpool.Pool
admin/handlers.go 2242 行中 52 个 handler 全部需要 DB

不改生产代码，gateway/billing/admin 的覆盖率上限大约在 15-20%。这不是策略选择的问题，是数学问题。


推荐的实际方案：分层推进

第一层（立即可做，不改生产代码）：


把现有纯函数的测试补全：router.go 中的路由选择逻辑如果有任何纯判断逻辑（比如权重计算、fallback 顺序），单独测
admin/handlers.go 中的 parsePagination、maskKey、writeJSON、joinStrings 这些纯辅助函数，补充边界用例
models/models.go 当前 0% 覆盖，补充序列化/反序列化测试，验证 JSON tag 和 omitempty 行为是否符合预期

这层能增加大约 3-5% 的覆盖率，但价值在于兜底。


第二层（需要 Codex 配合，提出具体接口抽象方案）：


这是真正推动覆盖率突破 50% 的关键。我建议 Canary 向 Codex 提交一份接口抽象建议，具体方案如下：


go
// 需要抽象的核心接口，按优先级排列

// 1. 数据访问层 — 影响最大
type Querier interface {
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    Exec(ctx context.Context, sql string, args ...any) (pgtag.CommandTag, error)
}

// 2. 计费仓库 — billing 包专用
type BillingRepository interface {
    FreezeBalance(ctx context.Context, req FreezeRequest) (*FreezeResult, error)
    SettleFreeze(ctx context.Context, orgID string, freezeID string, actualCost int64) error
    CancelFreeze(ctx context.Context, freezeID string) error
    QueryModelPrice(ctx context.Context, orgID string, model string) (*ModelPrice, error)
    GetEffectiveGrant(ctx context.Context, orgID string) (*Grant, error)
}
// 需要抽象的核心接口，按优先级排列

// 1. 数据访问层 — 影响最大
type Querier interface {
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    Exec(ctx context.Context, sql string, args ...any) (pgtag.CommandTag, error)
}

// 2. 计费仓库 — billing 包专用
type BillingRepository interface {
    FreezeBalance(ctx context.Context, req FreezeRequest) (*FreezeResult, error)
    SettleFreeze(ctx context.Context, orgID string, freezeID string, actualCost int64) error
    CancelFreeze(ctx context.Context, freezeID string) error
    QueryModelPrice(ctx context.Context, orgID string, model string) (*ModelPrice, error)
    GetEffectiveGrant(ctx context.Context, orgID string) (*Grant, error)
}

这里有个常见的误区需要澄清：抽象不是为了 mock 而 mock。接口抽象的真正价值在于：


1.测试隔离 — 集成测试可以在测试事务中运行，测试完 rollback，不污染真实数据
2.契约明确 — BillingRepository 接口清晰定义了 billing 包对数据层的所有依赖
3.并行开发 — Canary 可以基于接口写测试，Codex 同步重构实现

第三层（集成测试，不改生产代码但需要环境管理）：


对于当前的真实 DB 依赖场景，可以先用 testing.TB + 事务回滚策略：


go
func setupTestDB(t *testing.T) *pgxpool.Pool {
    pool, err := pgxpool.New(context.Background(), "postgres://localhost:5432/uniroute_test")
    require.NoError(t, err)
    t.Cleanup(func() { pool.Close() })

    // 开启事务，所有测试操作在事务内
    tx, err := pool.Begin(context.Background())
    require.NoError(t, err)
    t.Cleanup(func() { tx.Rollback(context.Background()) })

    // 返回包装了 tx 的 pool... 但这里卡住了
    // 因为现有代码期望 *pgxpool.Pool，不是 pgx.Tx
}
func setupTestDB(t *testing.T) *pgxpool.Pool {
    pool, err := pgxpool.New(context.Background(), "postgres://localhost:5432/uniroute_test")
    require.NoError(t, err)
    t.Cleanup(func() { pool.Close() })

    // 开启事务，所有测试操作在事务内
    tx, err := pool.Begin(context.Background())
    require.NoError(t, err)
    t.Cleanup(func() { tx.Rollback(context.Background()) })

    // 返回包装了 tx 的 pool... 但这里卡住了
    // 因为现有代码期望 *pgxpool.Pool，不是 pgx.Tx
}

注意到了吗？这正好说明为什么需要接口抽象。*pgxpool.Pool 不是接口，无法用事务替换。


对问题 1 的直接回答

不应该坚持"不改生产代码"约束。 正确的工作模式是：


Canary 分析代码结构，识别依赖关系，输出《接口抽象建议书》
Codex 按建议书重构，引入接口层
Canary 基于接口层编写完整测试

两个 agent 的职责边界应该是"不互相修改对方的产出物"，而不是"不提出跨领域建议"。否则测试覆盖率永远卡在 20%，项目实际上在裸奔。



问题 2: billing 集成测试 — 修复还是重写

直接回答：重写，不要修复

逐个分析三个失败模式：


失败模式 1：唯一键冲突 (orgs_name_key)


text
billing_debug_test.go:44: create org: ERROR: duplicate key value violates
unique constraint "orgs_name_key"
billing_debug_test.go:44: create org: ERROR: duplicate key value violates
unique constraint "orgs_name_key"

这说明测试用硬编码的 org name，没有做清理，也没有用唯一名称。这是测试设计的根本问题 — 测试之间有隐式依赖。修复意味着加 DELETE 语句或随机化名称，但隐藏的问题是：你永远不确定测试通过是因为逻辑正确，还是因为上次留下的脏数据恰好满足了条件。


失败模式 2：nil 指针 panic (billing_debug_test.go:131)


go
// 猜测当前代码大致是：
result, err := billing.Freeze(ctx, req)
// 缺少 if err != nil 的检查
assert.True(t, result.Success) // panic: result 是 nil
// 猜测当前代码大致是：
result, err := billing.Freeze(ctx, req)
// 缺少 if err != nil 的检查
assert.True(t, result.Success) // panic: result 是 nil

这个连基本的错误处理都缺失，说明是调试时临时写的代码，不是正经测试。


失败模式 3：事务中止级联 (SQLSTATE 25P02)


text
current transaction is aborted, commands ignored until end of transaction block
current transaction is aborted, commands ignored until end of transaction block

这个错误意味着：测试中的某一步 SQL 执行失败，事务被 PostgreSQL 标记为 aborted，后续所有操作在同一事务内都失败了。这表明测试的 setup 阶段就有问题（可能是 schema 不匹配、缺少 seed 数据、或测试间状态污染），但错误信息被吞掉了，只在后续操作才暴露。


重写方案

go
// 建议的 billing 测试文件结构：
internal/billing/
├── billing.go              # 生产代码
├── billing_unit_test.go    # 纯逻辑单元测试（如果有）
└── billing_integration_test.go  # 集成测试

// 集成测试必须满足的条件：
// 1. 使用独立测试数据库 uniroute_test
// 2. 每个 test case 自己 setup 所需数据
// 3. 使用 t.Cleanup 清理
// 4. 每个 test 独立，不依赖执行顺序

func TestFreeze_Success(t *testing.T) {
    ctx := context.Background()
    pool := setupTestDB(t) // uniroute_test, t.Cleanup => close

    // Setup: 创建独立的 org、wallet、grant
    orgID := createTestOrg(t, pool, "test-freeze-success-"+uuid.New().String())
    createTestWallet(t, pool, orgID, 10000) // 100.00 元
    createTestGrant(t, pool, orgID, 5000, time.Now().Add(24*time.Hour))

    // Act
    result, err := Freeze(ctx, pool, &FreezeRequest{
        OrgID:     orgID,
        Model:     "gpt-4",
        RequestID: uuid.New().String(),
        EstTokens: 100,
    })

    // Assert
    require.NoError(t, err)
    require.NotNil(t, result)
    assert.True(t, result.Success)
    assert.Greater(t, result.FrozenAmount, int64(0))

    // Verify: 检查钱包余额减少了
    balance := getWalletBalance(t, pool, orgID)
    assert.Equal(t, int64(10000-result.FrozenAmount), balance)
}
// 建议的 billing 测试文件结构：
internal/billing/
├── billing.go              # 生产代码
├── billing_unit_test.go    # 纯逻辑单元测试（如果有）
└── billing_integration_test.go  # 集成测试

// 集成测试必须满足的条件：
// 1. 使用独立测试数据库 uniroute_test
// 2. 每个 test case 自己 setup 所需数据
// 3. 使用 t.Cleanup 清理
// 4. 每个 test 独立，不依赖执行顺序

func TestFreeze_Success(t *testing.T) {
    ctx := context.Background()
    pool := setupTestDB(t) // uniroute_test, t.Cleanup => close

    // Setup: 创建独立的 org、wallet、grant
    orgID := createTestOrg(t, pool, "test-freeze-success-"+uuid.New().String())
    createTestWallet(t, pool, orgID, 10000) // 100.00 元
    createTestGrant(t, pool, orgID, 5000, time.Now().Add(24*time.Hour))

    // Act
    result, err := Freeze(ctx, pool, &FreezeRequest{
        OrgID:     orgID,
        Model:     "gpt-4",
        RequestID: uuid.New().String(),
        EstTokens: 100,
    })

    // Assert
    require.NoError(t, err)
    require.NotNil(t, result)
    assert.True(t, result.Success)
    assert.Greater(t, result.FrozenAmount, int64(0))

    // Verify: 检查钱包余额减少了
    balance := getWalletBalance(t, pool, orgID)
    assert.Equal(t, int64(10000-result.FrozenAmount), balance)
}

每个集成测试的核心模式：独立 setup → 执行 → 验证 → 自动 cleanup。用 UUID 避免名称冲突，用 t.Cleanup 保证即使 panic 也不留脏数据。


那 3 个 debug 测试应该直接删除

它们不是测试，是调试代码。在 billing_debug_test.go 中直接删掉，不要留着"以后参考"。Git 有历史记录。



问题 3: admin 测试的优先级

不要全覆盖，按风险分层

52 个 handler 全覆盖是不现实也不经济的。admin/handlers.go 是管理后台，用户是内部运维人员，不是面向外部用户的 API。但它控制着资金操作和安全配置，所以风险不对等。


第一优先级 — 必须测试（资金 + 安全）

Handler	风险	测试重点
TopUp	资金：直接操作钱包余额	金额边界（负数、零、溢出）、幂等性、并发充值
BalanceAdjust	资金：管理员手动调账	审计日志是否生成、调整方向是否正确
CreateGrant	资金：额度发放	过期时间校验、重复发放、超额发放
CreateAPIKey	安全：密钥生成	权限范围、密钥格式、密钥不可预测性
DisableAPIKey	安全：密钥失效	禁用后是否立即不可用、缓存一致性
Login	安全：认证入口	密码错误锁定、JWT 有效性、SQL 注入

这 6 个 handler 的测试投入产出比最高。一个 TopUp 的金额溢出 bug 可能造成直接的财务损失。


第二优先级 — 建议测试（渠道 + 模型价格）

Handler	理由
CreateChannel / UpdateChannel	渠道配置错误会导致请求转发失败，影响所有用户
DeleteChannel	删除时是否有引用检查？正在使用的 channel 能否被删？
TestChannel	测试连接是否真的验证了上游可用性
CreateModelPrice / UpdateModelPrice	价格配置错误直接导致计费偏差

第三优先级 — 可以先不测

Handler	理由
查询类 (ListLogs, DashboardStats, ListAuditLogs 等)	只读操作，出错影响低
parsePagination, maskKey 等辅助函数	简单逻辑，目测审查即可
CleanupOldData	后台维护任务，出错影响低且可恢复

一个需要注意的问题

admin/handlers.go 有 2242 行，52 个函数。这本身就是一个代码组织问题。正常情况下 52 个 handler 应该按领域拆分到不同文件。但这属于 Codex 的重构范围，不在测试范围内。


测试 agent 可以在报告中注明这一点：如果后续拆分了文件，测试文件也应该对应拆分。



问题 4: 测试文件管理

测试文件命名规范

text
internal/billing/
├── billing.go
├── billing_test.go              # 单元测试（不需要外部依赖）
├── billing_integration_test.go  # 集成测试（需要 DB）
internal/billing/
├── billing.go
├── billing_test.go              # 单元测试（不需要外部依赖）
├── billing_integration_test.go  # 集成测试（需要 DB）

这是标准 Go 命名，没有什么争议。


关键问题：build tag 隔离

是的，集成测试必须用 build tag 隔离。 理由：


1.go test ./... 默认会跑所有测试。集成测试如果连不上 DB 直接全部失败，给 CI 制造噪音
2.本地开发时开发者经常跑 go test ./internal/billing/ 快速验证，不希望等 DB 连接

推荐方式：


go
//go:build integration

package billing

import "testing"

func TestFreeze_Success(t *testing.T) {
    // ...
}
//go:build integration

package billing

import "testing"

func TestFreeze_Success(t *testing.T) {
    // ...
}

CI 中用 go test -tags=integration -v ./... 单独跑集成测试。


但注意：不要用文件名后缀 _integration_test.go 来代替 build tag。Go 的文件名没有特殊语义，billing_integration_test.go 和 billing_test.go 会被一起编译。build tag 是唯一可靠的隔离手段。


Codex 写的验收测试文档如何共存

docs/ops/test-plan.md 和 docs/ops/test-report.md 是验收性质的，描述的是"系统整体是否符合需求"。docs/testing/ 是开发性质的，描述的是"代码级别的测试设计"。


建议结构：


text
docs/
├── ops/
│   ├── test-plan.md       # Codex: 验收测试计划（功能清单、通过标准）
│   └── test-report.md     # Codex: 验收测试结果
├── testing/
│   ├── strategy.md        # Canary: 测试策略文档（本文件对应内容）
│   ├── coverage.md        # Canary: 覆盖率报告
│   └── fixtures.md        # Canary: 测试数据设计文档
docs/
├── ops/
│   ├── test-plan.md       # Codex: 验收测试计划（功能清单、通过标准）
│   └── test-report.md     # Codex: 验收测试结果
├── testing/
│   ├── strategy.md        # Canary: 测试策略文档（本文件对应内容）
│   ├── coverage.md        # Canary: 覆盖率报告
│   └── fixtures.md        # Canary: 测试数据设计文档

两者不冲突，但 docs/testing/strategy.md 应该由 Canary 维护，Codex 不应修改。反之亦然。



问题 5: 测试环境管理

必须用独立测试数据库

当前直接连 localhost:5432, db: uniroute 是危险的。一次测试 bug 就可能清空生产数据（虽然这个项目看起来是开发阶段，但习惯不能养成）。


最低要求：


bash
# 创建测试数据库
createdb uniroute_test
# 运行迁移
MIGRATE_DB_URL=postgres://localhost:5432/uniroute_test go run cmd/migrate/main.go
# 创建测试数据库
createdb uniroute_test
# 运行迁移
MIGRATE_DB_URL=postgres://localhost:5432/uniroute_test go run cmd/migrate/main.go

测试代码中：


go
const testDBURL = "postgres://localhost:5432/uniroute_test?sslmode=disable"

func getTestPool(t *testing.T) *pgxpool.Pool {
    t.Helper()

    // 检查连接
    pool, err := pgxpool.New(context.Background(), testDBURL)
    if err != nil {
        t.Skipf("无法连接测试数据库: %v", err) // 优雅跳过，不硬失败
    }

    t.Cleanup(func() { pool.Close() })
    return pool
}
const testDBURL = "postgres://localhost:5432/uniroute_test?sslmode=disable"

func getTestPool(t *testing.T) *pgxpool.Pool {
    t.Helper()

    // 检查连接
    pool, err := pgxpool.New(context.Background(), testDBURL)
    if err != nil {
        t.Skipf("无法连接测试数据库: %v", err) // 优雅跳过，不硬失败
    }

    t.Cleanup(func() { pool.Close() })
    return pool
}

用 t.Skip 而不是 t.Fatal，这样本地没有测试 DB 时 go test 不会报错，只是跳过集成测试。


testcontainers-go：现阶段不需要

testcontainers-go 适合 CI 环境和团队协作场景。当前 UniRouter 是一个 5000 行的项目，大概率还在早期阶段，引入 testcontainers 的收益不如成本：


增加测试启动时间（拉镜像、启动容器、等健康检查）
增加 CI 配置复杂度
本地开发需要 Docker

什么时候引入： 当团队超过 2 人，或有 CI 流水线时。


测试数据清理策略

推荐 transaction rollback 方案（前提是有接口抽象后）：


go
func withTestTransaction(t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx)) {
    tx, err := pool.Begin(context.Background())
    require.NoError(t, err)
    defer tx.Rollback(context.Background()) // 自动回滚

    fn(tx)
}
func withTestTransaction(t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx)) {
    tx, err := pool.Begin(context.Background())
    require.NoError(t, err)
    defer tx.Rollback(context.Background()) // 自动回滚

    fn(tx)
}

如果当前不想改生产代码，用 truncation 方案：


go
func cleanupTestData(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    // 按依赖顺序 truncate，外键约束在前
    tables := []string{
        "call_logs", "freeze_idempotency", "transactions",
        "grants", "wallets", "channel_keys", "model_prices",
        "org_model_prices", "api_keys", "audit_logs",
    }
    for _, table := range tables {
        _, err := pool.Exec(context.Background(),
            fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
        if err != nil {
            t.Logf("cleanup warning: %v", err)
        }
    }
}
func cleanupTestData(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    // 按依赖顺序 truncate，外键约束在前
    tables := []string{
        "call_logs", "freeze_idempotency", "transactions",
        "grants", "wallets", "channel_keys", "model_prices",
        "org_model_prices", "api_keys", "audit_logs",
    }
    for _, table := range tables {
        _, err := pool.Exec(context.Background(),
            fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
        if err != nil {
            t.Logf("cleanup warning: %v", err)
        }
    }
}

在 TestMain 中调用一次，或在每个测试的 t.Cleanup 中调用。



问题 6: 前端测试优先级

先说结论：优先级低，但不是零

理由：


1.前端是管理后台，用户群体小（运维人员），不像面向用户的产品那样需要覆盖各种浏览器/设备组合
2.React + Semi Design 是成熟的组件库，UI 层 bug 概率低
3.前端的核心风险不在 UI 渲染，而在数据交互 — 提交表单时发错字段、金额显示格式错误、权限判断遗漏

如果做，覆盖什么

不覆盖：

页面布局渲染（Semi Design 组件的正确性）
路由跳转
样式/响应式

应该覆盖：


页面/功能	风险	测试方式
充值/调账表单	金额字段、确认流程	单元测试：表单验证逻辑
API Key 创建/禁用	密钥显示/隐藏逻辑	单元测试
权限控制	不同角色看到不同菜单	单元测试
表单提交 → API 调用	请求参数是否正确	MSW mock + 测试

推荐方案：Vitest + MSW，不做 E2E

text
前端测试投入：
├── Vitest + @testing-library/react  # 组件逻辑测试
├── MSW (Mock Service Worker)        # 拦截 API 请求
└── 不引入 Playwright/Cypress        # E2E 投入产出比太低
前端测试投入：
├── Vitest + @testing-library/react  # 组件逻辑测试
├── MSW (Mock Service Worker)        # 拦截 API 请求
└── 不引入 Playwright/Cypress        # E2E 投入产出比太低

Vitest 配置成本低（Vite 原生支持），MSW 让你不需要真实后端就能测前端的数据流。


前端测试的实际建议

如果前端只有 22 个文件且是管理后台，我的真实建议是：不写前端测试。 把测试精力放在后端的资金和安全逻辑上。管理后台出一个 UI bug，运维人员刷新一下页面；后端出一个计费 bug，可能是真金白银的损失。


但如果一定要做，从金额相关的表单开始。



额外建议

你没有问但应该知道的

1. relay.go:198 循环内 defer 是严重 bug


go
for _, channel := range channels {
    resp, err := doRequest(channel)
    if err != nil { continue }
    defer resp.Body.Close()  // BUG: 这个 defer 在函数返回时才执行
    if resp.StatusCode == 200 {
        return resp
    }
    // failover 到下一个 channel 时，前一个 resp.Body 没有关闭
}
for _, channel := range channels {
    resp, err := doRequest(channel)
    if err != nil { continue }
    defer resp.Body.Close()  // BUG: 这个 defer 在函数返回时才执行
    if resp.StatusCode == 200 {
        return resp
    }
    // failover 到下一个 channel 时，前一个 resp.Body 没有关闭
}

这不是测试问题，是生产 bug。在 failover 场景下会泄漏 HTTP 连接。Canary 应该在报告中明确标记为 P0，要求 Codex 修复。


测试 agent 虽然不能改生产代码，但可以写一个测试来验证这个 bug 的存在：


go
func TestRelay_FailoverClosesPreviousBody(t *testing.T) {
    // 启动两个 httptest.Server
    // 第一个返回 500
    // 第二个返回 200
    // 发送请求触发 failover
    // 检查第一个 server 的连接是否被正确关闭
}
func TestRelay_FailoverClosesPreviousBody(t *testing.T) {
    // 启动两个 httptest.Server
    // 第一个返回 500
    // 第二个返回 200
    // 发送请求触发 failover
    // 检查第一个 server 的连接是否被正确关闭
}

2. router.go:75 未检查 Exec 返回值


go
r.db.Exec(ctx, "UPDATE channels SET ...")  // err 被丢弃
r.db.Exec(ctx, "UPDATE channels SET ...")  // err 被丢弃

如果这次 UPDATE 失败（比如通道不存在），熔断器状态不会被更新，可能导致：

一个已经超时的通道永远留在 circuit-open 状态
或者一个正常的通道被错误地标记为 open

3. 测试报告应该包含一个"技术债务清单"


当前项目 5000 行代码，0% 覆盖率的包有 4 个（db, redisx, migrate, models），20 个测试失败。这些数字本身就是技术债务的量化。


建议 Canary 在每轮测试报告末尾附上：


text
## 技术债务清单

| 项目 | 严重程度 | 负责方 | 状态 |
|------|---------|--------|------|
| billing 集成测试全挂 (20/20) | P0 | Codex + Canary | 重写中 |
| testutil 编译失败 | P1 | Codex | 待修复 |
| relay.go 循环内 defer | P0 | Codex | 待修复 |
| router.go 未检查 Exec 返回值 | P1 | Codex | 待修复 |
| 无接口抽象层 | P2 | Codex | 待规划 |
| db/redisx/migrate 零测试 | P2 | Canary | 待安排 |
| 前端零测试 | P3 | 待决策 | - |
## 技术债务清单

| 项目 | 严重程度 | 负责方 | 状态 |
|------|---------|--------|------|
| billing 集成测试全挂 (20/20) | P0 | Codex + Canary | 重写中 |
| testutil 编译失败 | P1 | Codex | 待修复 |
| relay.go 循环内 defer | P0 | Codex | 待修复 |
| router.go 未检查 Exec 返回值 | P1 | Codex | 待修复 |
| 无接口抽象层 | P2 | Codex | 待规划 |
| db/redisx/migrate 零测试 | P2 | Canary | 待安排 |
| 前端零测试 | P3 | 待决策 | - |


以上是我基于你提供的信息的实际分析。如果你有具体的技术细节需要深入（比如某个 handler 的具体实现、数据库 schema 的完整定义、或者某段代码的逻辑分支），可以继续展开讨论。