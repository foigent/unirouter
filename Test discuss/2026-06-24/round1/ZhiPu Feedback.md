总体判断
当前测试体系的核心问题不是覆盖率低，而是基础设施不可靠。20 个失败测试如果持续存在，团队会养成"忽略红灯"的习惯，这比没有测试更危险。优先级应该是：先让现有测试全绿 → 建立可靠的测试基础设施 → 再扩大覆盖面。

问题 1: "不改生产代码"约束下的测试策略
约束本身的审视
这个约束在实际操作中会严重限制测试效果。直接依赖 *pgxpool.Pool 意味着所有涉及 DB 的业务逻辑都无法隔离测试。这不是测试技巧的问题，是代码结构的问题。

建议：将"不改生产代码"重新定义为"Canary 不直接修改生产代码，但可以提出重构需求，由 Codex 执行"。 两个 agent 之间应该有明确的协作通道，否则 Canary 只能写出质量天花板很低的测试。

分层策略
不要选单一方案，按时间线分层推进：

第一层：Canary 立即可做（不改生产代码）
httptest + 真实 DB 的集成测试，但必须解决隔离问题：

text

测试入口: httptest.NewServer() → Gin engine → middleware → handler → 真实 DB
隔离方式: 每个测试套件启动前 truncate 所有表，套件内按顺序执行
这实际上就是方案 D，但关键改进点是环境隔离（见问题 5）。方案 A（纯 DB 集成测试）和方案 D 的区别在于：D 能测到 middleware 链 + handler 的完整交互，A 只能测单个包。

选择 D 而不是 A 的理由：

middleware/auth.go 和 billing/billing.go 的交互（认证后计费）是核心链路，只在包级别测试会遗漏
httptest 不需要额外依赖，Go 标准库自带
测试的是 API 行为而非实现细节，重构时测试不需要改
第二层：请求 Codex 做最小重构
重构范围极其有限——只给每个包加一个接口定义，把 *pgxpool.Pool 的使用收窄到构造函数：

go

// billing/store.go — 新增文件，约 30 行
type BillingStore interface {
    QueryEffectiveGrants(ctx context.Context, orgID string) ([]Grant, error)
    FreezeAmount(ctx context.Context, orgID string, amount float64) error
    // ... 只列出 billing 包实际调用的 DB 方法
}

// billing/billing.go — 只改构造函数
type BillingService struct {
    store BillingStore  // 之前是 *pgxpool.Pool
}
改动量：每个包新增一个接口文件（~30 行），修改构造函数和字段类型。业务逻辑零改动。

这步做完后，Canary 就能用 mock 写真正的单元测试，覆盖 billing 的核心计费逻辑。

第三层：长期
对纯逻辑函数（resolveModelMapping、熔断器状态机等）维持现有单元测试。这些已经做得不错，不需要改动。

方案对比表
可行性
覆盖率潜力
隔离性
执行速度
需要 Codex
A: DB 集成	✅ Canary 可做	60-70%	差	慢	否
B: 接口+mock	❌ Canary 不能做	85%+	好	快	是
C: 纯逻辑	✅ Canary 可做	5-10%	好	快	否
D: httptest+DB	✅ Canary 可做	50-65%	中	慢	否
推荐: D→B→C	✅	85%+	好	混合	是（最小改动）

问题 2: billing 集成测试 — 重写
判断依据
17 个集成测试全部以 SQLSTATE 25P02 失败，这是级联失败。根因是第一个 SQL 操作失败后，同一事务内所有后续操作都被 PostgreSQL 拒绝。这意味着：

测试数据准备不充分（缺少 Freeze 依赖的 grant 记录）
测试之间没有隔离（前一个测试的失败污染了事务状态）
debug 测试中的 duplicate key 证明测试不做清理
3 个 debug 测试是开发探索代码，不是真正的测试——删除。

重写方案
核心原则：每个测试自己准备数据、自己清理、不依赖执行顺序。

go

// billing_integration_test.go — 重写框架

func setupBillingTest(t *testing.T) (*BillingService, func()) {
    t.Helper()
    // 1. 连接测试数据库 uniroute_test
    pool := connectTestDB(t)
    
    // 2. 在事务中 truncate 所有相关表
    tx, err := pool.Begin(context.Background())
    require.NoError(t, err)
    // truncate 相关表: wallets, grants, transactions, freeze_idempotency
    for _, table := range []string{"freeze_idempotency", "transactions", "wallets", "grants"} {
        _, err = tx.Exec(context.Background(), "TRUNCATE "+table+" CASCADE")
        require.NoError(t, err)
    }
    require.NoError(t, tx.Commit(context.Background()))
    
    // 3. 创建 billing service
    svc := NewBillingService(pool)
    
    // 4. 返回清理函数
    cleanup := func() {
        // truncate again for next test
        for _, table := range []string{"freeze_idempotency", "transactions", "wallets", "grants"} {
            pool.Exec(context.Background(), "TRUNCATE "+table+" CASCADE")
        }
        pool.Close()
    }
    return svc, cleanup
}
优先重写的测试用例（按业务风险排序）：

Freeze → Settle 完整流程 — 这是资金链路的核心
Freeze 余额不足 — 边界条件
Freeze 幂等性 — request_id 重复请求
Cancel 解冻 — 资金回退正确性
并发 Freeze — 余额竞争条件
每个测试必须：

自行创建 org + wallet + grant
验证每个步骤后的 DB 状态（余额、冻结金额、事务记录）
自行清理
关于 nil pointer dereference
billing_debug_test.go:131 的 result.Success 在 err != nil 之前访问——这不仅是测试 bug，billing 包的生产代码很可能也有同样的模式。Canary 应该在报告中标记这个风险，让 Codex 排查生产代码中是否有相同的 result 未做 nil 检查就访问字段的情况。

问题 3: admin 测试优先级
52 个 handler 不应该全覆盖
全覆盖 52 个 handler 的 ROI 极低。大部分是标准 CRUD，bug 风险低，而且 CRUD 的正确性本质上由 DB 约束和框架保证。

优先级排序
text

P0 — 资金安全 (7 个 handler):
  TopUp, BalanceAdjust, CreateGrant, GetWallet, 
  ListWalletTransactions, ExportWalletTransactions, ListGrants

P1 — 认证授权 (5 个 handler):
  Login, CreateAPIKey, DisableAPIKey, UpdateAPIKey, GetAPIKey

P2 — 路由正确性 (4 个 handler):
  CreateChannel, UpdateChannel, DeleteChannel, TestChannel

P3 — 其余 (28 个 handler):
  暂不测试，等 P0-P2 稳定后再考虑
P0 的测试要点
资金 handler 的测试不是测 CRUD，是测资金安全边界：

测试场景
风险
TopUp 负数金额	余额被清零或变负
BalanceAdjust 超额扣减	余额变负
CreateGrant 过期时间在过去	无效 grant 被使用
并发 TopUp 同一 wallet	余额不一致
CreateGrant 金额为 0 或负数	逻辑错误
权限: 非本 org 用户能否查看 wallet	越权

测试方式
用 httptest 测 HTTP 层，验证：

请求参数校验（非法输入返回 400）
权限校验（非授权用户返回 403）
业务规则（余额不能为负等）
响应结构（字段完整性、敏感信息脱敏）
问题 4: 测试文件管理
命名规范
text

billing_test.go              ← 单元测试（纯逻辑，无外部依赖）
billing_integration_test.go  ← 集成测试（需要 DB/Redis），加 build tag
billing_debug_test.go        ← 删除
Build Tag 规范
集成测试必须用 build tag 隔离：

go

//go:build integration

package billing
执行方式：

bash

# 日常开发 — 只跑单元测试，快
go test ./...

# CI / 提交前 — 跑全部测试
go test -tags=integration ./...

# 只跑集成测试
go test -tags=integration ./internal/billing/
文档共存
text

docs/
├── ops/
│   ├── test-plan.md        ← Codex 的验收测试（高层、面向产品）
│   └── test-report.md      ← 验收报告
└── testing/
    ├── strategy.md          ← Canary 的测试策略（本咨询文档的持久化版本）
    ├── infrastructure.md    ← 测试环境搭建指南（DB、Redis、testcontainers）
    └── conventions.md       ← 命名规范、build tag、fixture 管理
两套文档职责不同，不冲突。docs/ops/ 是"测什么"，docs/testing/ 是"怎么测"。

问题 5: 测试环境管理
当前状态的危险性
测试连接 localhost:5432/uniroute，和开发数据库相同。这意味着：

测试可能破坏开发数据
开发数据可能影响测试结果
并发测试和开发操作可能互相干扰
方案
立即执行：独立测试数据库

bash

createdb uniroute_test
测试代码通过环境变量区分：

go

func connectTestDB(t *testing.T) *pgxpool.Pool {
    t.Helper()
    dsn := os.Getenv("UNIROUTE_TEST_DSN")
    if dsn == "" {
        dsn = "postgres://localhost:5432/uniroute_test"
    }
    // ...
}
中期：testcontainers-go

CI 环境用 testcontainers 自动拉起 PostgreSQL 容器：

go

func setupTestContainer(t *testing.T) (*pgxpool.Pool, func()) {
    ctx := context.Background()
    req := testcontainers.ContainerRequest{
        Image:        "postgres:16",
        ExposedPorts: []string{"5432/tcp"},
        Env: map[string]string{
            "POSTGRES_DB":       "uniroute_test",
            "POSTGRES_PASSWORD": "test",
        },
        WaitingFor: wait.ForListeningPort("5432/tcp"),
    }
    pgC, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: req,
        Started:          true,
    })
    // ...
}
本地开发可以不用 testcontainers（直接用 uniroute_test），CI 必须用。

数据清理策略
不要用事务回滚——billing 代码内部管理自己的事务，外层套事务会冲突。用 truncate：

go

// 按依赖顺序 truncate
var tablesToTruncate = []string{
    "freeze_idempotency",
    "transactions", 
    "call_logs",
    "audit_logs",
    "wallets",
    "grants",
    "api_keys",
    "channel_keys",
    "channels",
    "org_model_prices",
    "model_prices",
    "users",
    "orgs",
}

func truncateAll(t *testing.T, pool *pgxpool.Pool) {
    for _, table := range tablesToTruncate {
        _, err := pool.Exec(context.Background(), 
            fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
        require.NoError(t, err)
    }
}
每个测试套件的 TestMain 做 truncate，不是每个测试用例。同一套件内的测试用例如果需要隔离，各自在 setup 中创建独占的 org（用 UUID name 前缀）。

问题 6: 前端测试优先级
结论：低优先级，但不是零
后端有 20 个失败测试、核心资金链路零覆盖，前端测试不应该占资源。

如果做，怎么做
不做组件级单元测试。 Semi Design 组件库已经测过，不需要再测按钮点击。

只做两类：

共享工具函数 — 如果有的话，用 Vitest 测。但看代码结构，前端 22 个文件大概率都是页面组件，工具函数很少。
关键流程 E2E — 用 Playwright，只覆盖 2-3 条链路：
登录 → 创建 API Key → 用 Key 调用 API
充值 → 查看余额 → 查看事务记录
E2E 测试直接测前端+后端完整链路，ROI 远高于前端单元测试。
前期建议：先不配 Vitest，省下时间投到后端测试。 等后端测试稳定在 60%+ 覆盖率后再考虑前端。

额外建议
1. 代码 bug 比测试覆盖更紧急
文档列出的 4 个代码问题，按严重性排序：

优先级
问题
影响
行动
P0	XOR 加密替代 AES-GCM	数据泄露风险	立即报告 Codex
P0	relay.go:198 defer 在循环内	failover 时连接泄漏，生产环境 OOM	立即报告 Codex
P1	router.go:75 Exec 返回值未检查	熔断状态更新静默失败，路由决策错误	报告 Codex
P2	debug 测试 nil pointer	仅影响测试	重写测试时修复

Canary 的约束是"不改生产代码"，但报告生产代码的 bug 完全在职责范围内。建议在报告中新增一个 BUGS.md，专门跟踪这些问题。

2. testutil 编译失败是阻塞项
text

internal/testutil/db.go:43:49: undefined: pgx
这个不修，所有依赖 testutil 的集成测试都无法编译。Canary 应该在测试文件中修复这个 import（改测试工具代码不算改生产代码），或请求 Codex 修复。

3. 测试失败报警阀值
建立硬性规则：测试套件中失败测试数量超过 0 时，不允许合并。 当前 20 个失败测试如果被无视，团队会习惯红灯，测试体系形同虚设。

短期处置：先 skip 或 delete 失败的测试，让 CI 绿回来，再逐步补回正确版本。

4. 建议的行动时序
text

Week 1:
  ├─ 修复 testutil 编译错误
  ├─ 删除 debug 测试
  ├─ skip 当前失败的 17 个 billing 集成测试
  ├─ 建立 uniroute_test 数据库 + truncate 清理逻辑
  ├─ 向 Codex 提交 bug 报告（XOR、defer、Exec 返回值）
  └─ 向 Codex 提交接口抽象重构需求（billing 包优先）

Week 2:
  ├─ 重写 billing 集成测试（5 个核心场景）
  ├─ 用 httptest 写 admin P0 handler 测试（资金安全）
  └─ 集成测试加 build tag

Week 3:
  ├─ Codex 完成接口抽象 → Canary 补充 billing 单元测试
  ├─ admin P1 handler 测试（认证授权）
  └─ CI 配置 testcontainers
总结
问题
建议
测试策略	httptest+DB 集成测试（立即可做）→ 请求 Codex 加接口抽象 → 单元测试补覆盖
billing 集成测试	重写，不修复。删除 debug 测试
admin 测试优先级	P0 资金安全(7) > P1 认证(5) > P2 渠道(4) > 其余暂缓
测试文件管理	单元/集成分离，集成用 //go:build integration，删除 debug 测试
测试环境	立即建 uniroute_test，CI 用 testcontainers，truncate 清理
前端测试	低优先级，后端稳定后再做，Playwright E2E 优于 Vitest 单元
约束重新定义	"Canary 不改生产代码"应改为"Canary 可提出重构需求，Codex 执行"