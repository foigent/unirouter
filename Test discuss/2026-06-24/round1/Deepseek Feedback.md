UniRouter 测试策略咨询报告
执行摘要
UniRouter 当前测试覆盖率约 22%，核心包（gateway 5%、billing 0.5%、admin 0.5%）几乎完全没有有效测试。核心问题不是“写更多测试”，而是“代码本身不可测试” ——所有核心包直接依赖 *pgxpool.Pool 具体类型，没有接口抽象层。在这个约束下，任何测试策略都必须直面这个根本性架构问题。

以下是针对六个问题的逐项分析和建议。

问题 1：测试策略 — “不改生产代码”约束下的可行方案
核心矛盾
不改生产代码 vs 测试核心业务逻辑，这两个目标在 UniRouter 当前架构下存在根本性冲突。gateway/billing/admin 的核心逻辑与数据库操作深度耦合，不引入接口抽象就无法在单元测试中隔离数据库依赖。

分层策略建议
不建议在“不改生产代码”的约束下硬扛。更好的做法是：

层级	策略	是否需要改生产代码	优先级
纯函数（filter、tokenutil、circuitbreaker）	继续完善单元测试	否	P0（已完成）
有 DB 依赖的业务逻辑（billing 核心、gateway 路由）	由 Codex 引入接口抽象，Canary 随后写基于接口的单元测试	是（Codex 改）	P0
HTTP handler 层（admin）	集成测试 + httptest	否（但需要测试数据库）	P1
端到端流程	集成测试（testcontainers）	否	P2
具体到“接口抽象”这个动作：Codex 需要做的不是大改架构，而是最小化改动——在现有代码中提取接口，让 *pgxpool.Pool 作为接口的实现被注入。可以参考以下模式：

go
// 当前代码（不可测试）
type Handler struct {
    db *pgxpool.Pool
}

// 重构后（可测试）
type Querier interface {
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type Handler struct {
    db Querier  // 接口，测试时可注入 mock
}
*pgxpool.Pool 天然实现了这些方法，生产代码零改动，测试代码可以注入 pgxpoolmock或手写 mock。

关于“不改生产代码”约束的重新审视
建议：Canary 向 Codex 提出接口抽象的重构建议，由 Codex 执行。 这不是“改生产代码”的违规操作，而是“为了可测试性进行的必要重构”——这是测试驱动开发的标准实践。如果坚持完全不改生产代码，gateway/billing/admin 的测试覆盖率将永远被困在个位数。

问题 2：billing 集成测试全挂 — 修复还是重写
失败根因分析
失败类型	根因	严重程度
SQLSTATE 25P02（事务中止）	测试中某个操作失败后未回滚，后续所有操作在同一事务中继续执行	高
唯一键冲突（orgs_name_key）	多个测试共用同一数据库，重复创建同名组织	高
nil pointer panic（billing_debug_test.go:131）	result.Success 在 err != nil 检查前访问	中
建议：不修复，直接重写
理由：

当前测试设计有根本性缺陷——它们依赖共享的真实数据库，测试间互相污染。修复单个测试无法解决系统性问题。

当前测试与生产代码强耦合——billing 包没有接口抽象，测试被迫启动真实 DB 才能跑，违背了单元测试“快速、独立、可重现”的核心原则。

17 个集成测试全挂说明测试基础设施（setup/teardown、事务管理、数据清理）整体失效，逐行修复的成本高于重写。

重写路线
text
第一步：Codex 引入 Querier 接口抽象（见问题 1）
第二步：Canary 基于接口重写 billing 单元测试（使用 pgxpoolmock）
第三步：保留少量集成测试（使用 testcontainers + build tag 隔离），验证真实 DB 交互
关键原则：billing 的核心逻辑（冻结、结算、取消）应该在单元测试层覆盖 80% 以上，集成测试只覆盖“与真实 DB 的交互是否正确”这一小部分。

问题 3：admin 测试的优先级
不建议追求 52 个 handler 全覆盖
2242 行、52 个 handler，如果全部追求单元测试覆盖，投入产出比极低。建议采用 “风险优先”策略：

优先级分层
优先级	Handler 类别	数量	测试策略	理由
P0（必须测）	安全（Login、CreateAPIKey、DisableAPIKey）	5	单元测试 + 集成测试	资金安全、认证授权的核心入口
P0（必须测）	资金（TopUp、BalanceAdjust、GetWallet、CreateGrant）	7	单元测试 + 集成测试	直接涉及钱，不能出错
P1（应该测）	渠道（CreateChannel、UpdateChannel、TestChannel）	6	集成测试为主	路由核心，但变更频率较低
P1（应该测）	用户管理（CreateUser、UpdateUser、DeleteUser）	4	集成测试为主	CRUD 操作，风险中等
P2（可选）	查询类（ListLogs、ListSettings、DashboardStats）	10	简单的集成测试	读操作，风险低
P3（低优先级）	内部辅助（writeJSON、parsePagination、maskKey）	8	已有部分覆盖，维持即可	纯函数，风险低
核心原则：先测“钱”和“安全”，再测“功能” 。12 个 P0 handler 的覆盖，比 52 个 handler 的“形式覆盖”有价值得多。

问题 4：测试文件管理
命名规范建议
text
internal/billing/
├── billing_test.go          # 单元测试（无 build tag）
├── billing_integration_test.go  # 集成测试（//go:build integration）
└── billing_bench_test.go    # 基准测试（可选）
规则：

单元测试文件：{package}_test.go，无 build tag，go test ./... 自动运行

集成测试文件：{package}_integration_test.go，顶部加 //go:build integration

同一个包下不要有多个带相同 build tag 的文件，否则编译报错

运行方式
bash
# 只跑单元测试（快速，毫秒级）
go test ./...

# 跑单元测试 + 集成测试
go test -tags=integration ./...

# 串行执行（避免包间 DB 冲突）[reference:14]
go test -p=1 -tags=integration ./...
文档共存问题
docs/ops/test-plan.md 和 docs/ops/test-report.md 是验收测试文档（面向产品和运维），docs/testing/ 是技术测试文档（面向开发）。两者不冲突：

文档类型	位置	受众	内容
验收测试	docs/ops/	产品、运维、PM	测试计划、测试报告、通过标准
技术测试	docs/testing/	开发、QA	测试策略、测试环境搭建、mock 指南
建议在 docs/testing/README.md 中说明这种分工，避免混淆。

问题 5：测试环境管理
当前问题
测试直接连接 localhost:5432 的 uniroute 数据库，这是生产环境级别的风险。多个测试包并行运行时，会互相污染数据。

建议方案：三层隔离
第一层：单元测试（无 DB）

使用 pgxpoolmock 或手写 mock，完全不需要数据库

执行时间：毫秒级

适用：billing 核心逻辑、gateway 路由逻辑、admin 业务逻辑

第二层：集成测试（testcontainers）

使用 testcontainers-go 动态启动 PostgreSQL 容器

每个测试包独立容器，测试结束后自动销毁

执行时间：秒级（容器启动 ~2s）

适用：验证真实 DB 交互、migration 测试

第三层：验收测试（独立测试数据库）

使用独立的 PostgreSQL 实例（如 uniroute_test）

每次运行前 reset schema

适用：端到端流程验证

具体实施
go
// testutil/testdb.go
//go:build integration

package testutil

import (
    "context"
    "testing"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/wait"
)

func SetupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
    ctx := context.Background()
    req := testcontainers.ContainerRequest{
        Image: "postgres:15",
        Env: map[string]string{
            "POSTGRES_DB": "testdb",
            "POSTGRES_USER": "testuser",
            "POSTGRES_PASSWORD": "testpass",
        },
        WaitingFor: wait.ForLog("database system is ready to accept connections"),
    }
    container, err := testcontainers.GenericContainer(ctx, ...)
    // ... 返回连接池和 cleanup 函数
}
数据清理策略
单元测试：无数据，无需清理

集成测试（testcontainers） ：容器自动销毁，无需手动清理

验收测试：每次运行前执行 migrate down + up，确保干净状态

问题 6：前端测试优先级
现状评估
22 个 TS/TSX 文件，零测试，无 Vitest 配置。前端是管理后台，运维人员每天使用。

建议：低优先级，但要有基础
前端测试的优先级低于后端核心逻辑（billing/gateway 的测试覆盖率提升），原因：

前端出错的后果（用户体验差）低于后端出错的后果（资金损失、数据错误）

管理后台的用户是运维人员，容错性相对较高

前端代码变更频率通常低于后端核心逻辑

最小可行方案
优先级	内容	方式	估计工作量
P1	配置 Vitest + 基础测试环境	一次性配置	0.5 天
P2	核心 API 调用函数（src/api/*）	单元测试	1 天
P3	关键页面（登录、仪表盘、资金管理）	组件测试（RTL）	2-3 天
P4	完整 E2E（Playwright）	暂不实施	-
不建议现在做 Playwright E2E——投入大、维护成本高，且收益有限。

实施路线图（建议）
第一阶段（1-2 周）：基础设施 + billing 修复
任务	负责 Agent	产出
Codex 为 *pgxpool.Pool 引入 Querier 接口	Codex	接口定义，生产代码适配
Canary 重写 billing 单元测试（基于接口 + mock）	Canary	billing 覆盖率 > 70%
Canary 配置 testcontainers + build tag	Canary	集成测试基础设施
修复 testutil 编译问题	Codex	testutil 可编译通过
第二阶段（1-2 周）：gateway + admin 核心
任务	负责 Agent	产出
Codex 为 gateway 引入接口抽象	Codex	gateway 可测试化
Canary 写 gateway 核心逻辑单元测试	Canary	gateway 覆盖率 > 50%
Canary 写 admin P0 handler 测试（安全+资金）	Canary	12 个核心 handler 有测试
第三阶段（1 周）：前端基础 + 文档整合
任务	负责 Agent	产出
Canary 配置 Vitest	Canary	前端测试可运行
Canary 写 API 层测试	Canary	API 调用有覆盖
整合测试文档	Canary	docs/testing/ 完整
第四阶段（持续）：覆盖率提升 + CI 集成
将测试纳入 CI 流程

设定覆盖率门槛（核心包 > 70%）

持续补充边缘 case

总结
问题	核心建议
问题 1	不坚持“不改生产代码” ，由 Codex 引入接口抽象，Canary 随后写测试
问题 2	不修复，重写——当前测试基础设施整体失效
问题 3	风险优先——先测 12 个 P0 handler（安全+资金），不追求全覆盖
问题 4	统一命名规范 + build tag 隔离，两类文档明确分工
问题 5	单元测试用 mock，集成测试用 testcontainers，彻底隔离
问题 6	低优先级，先配 Vitest + 测 API 层，暂不做 E2E
最关键的一句话：UniRouter 当前 22% 的覆盖率不是“测试没写好”，而是“代码不可测试”。先让 Codex 做接口抽象，再让 Canary 写测试——这个顺序不能颠倒。