总判断
你现在最大的问题不是缺测试，而是测试基线是破的。20 个失败测试 + testutil 编译失败 + 数据库无隔离，这些没解决之前，加新测试等于在沙地上盖楼。先修地基，再扩覆盖。

而且我看你的数字挺有意思：~22% 覆盖率看着还行，但分布是 filter 100% / admin 0.5%——这种覆盖率是失真的，因为它在掩盖 admin/billing/gateway 三个核心包几乎没被验证的事实。覆盖率数字本身不是目标，关键路径的覆盖才是。

问题 1：测试策略——"不改生产代码"约束下的可行方案
坚持不改生产代码。但理由不是"边界神圣"，而是当前最该做的事是测试基建，不是重构。

为什么：

接口抽象（方案 B）确实好，但它是 Codex 的工作，不是 Canary 的。如果 Canary 现在花精力提抽象建议、推给 Codex、改完再回来写测试，这是跨 agent 串行工作，节奏很慢
billing 集成测试已经全挂，说明测试基线（环境、隔离、helper）还没搭好——重构完生产代码，新测试一样会挂
我的建议路线：

阶段	方案	工作量	覆盖目标
现在	A 集成测试（真实 DB）+ 修测试基建	2-3 天	billing 修通，gateway 补到 30%
短期	A + 关键纯函数单测	1 周	gateway 补到 50%, admin 补关键路径
中期	推 Codex 加 Repository 接口	2 周+	单元测试为主，覆盖率 60%+
长期	接口 + mock 为主，集成回归	持续	覆盖率 80%+，单测跑 <10s
现在能立刻做的两件事：

1.
定义 Repository 接口作为"接口契约"放在 internal/testutil/fakes/ 或 internal/billing/repo_iface.go（测试侧）——这样虽然生产代码还是直接调 *pgxpool.Pool，但测试时可以用接口 mock（如果生产代码碰巧依赖了接口）或者在测试里用 sqlmock 拦截。注意 pgx 难 mock，但事务级别的回滚测试不需要 mock。
2.
提接口抽象方案给 Codex：写一份 docs/testing/refactor-proposal.md，列清楚：
哪些函数应该抽接口（WalletRepo, ChannelRepo, APIKeyRepo, FreezeRepo）
接口签名怎么定义
重构路径（按包分批，避免一次性大改）
不要自己动手，让 Codex 实施，你做 reviewer。
问题 2：billing 集成测试全挂——修复还是重写
修，但大刀阔斧地修。不要逐个 debug，要重构测试基建。

失败原因分析：

失败	根因	严重度
testutil 编译失败	缺 import	🔴 阻塞一切
orgs_name_key 冲突	测试间无隔离，依赖固定 name	🔴 测试不独立
SQLSTATE 25P02	事务内有失败未回滚，后续 query 全挂	🔴 测试基础设施错
nil pointer panic	result.Success 在 err 检查前	🟡 明显 bug
修复顺序（按依赖关系）：

1.
第一步：修 testutil（30 分钟）
go

复制
// internal/testutil/db.go 加 import

import "github.com/jackc/pgx/v5"
没这个啥都跑不了。
2.
第二步：建立测试基建（半天）
testutil.SetupTestDB(t)：连 uniroute_test，跑 migrate，truncate 所有表
testutil.NewTestOrg(t, db)：生成 UUID-based org，避免冲突
testutil.WithTx(t, db, fn)：每个测试在事务里跑，结束 rollback
testutil.SeedWallet(t, db, orgID, balance)：seed 标准测试数据
核心：所有 ID 都要用 uuid.New() 或 t.Name() + timestamp，不要用固定字符串。
3.
第三步：批量修 billing 集成测试（1-2 天）
删 billing_debug_test.go（或者改名 TestFreeze_Diagnose_NilPointerBug 然后用 t.Skip 标记，将来真出问题再用）
billing_integration_test.go 每个测试加 setup/teardown
nil pointer 那行就是 bug：if result != nil && result.Success 或者先 if err != nil { t.Fatal() }
事务处理：每个测试包成 WithTx，自然不会有 25P02
4.
第四步：建立回归基线（半天）
跑通后，把这套测试加进 CI 必须通过的清单
这 20 个测试修复完成就是第一阶段里程碑
绝对不要重写。billing 是财务逻辑，这些测试覆盖的是核心场景（Freeze/Settle/Cancel/QueryModelPrice），删了等于把项目最关键的部分变成裸奔。

问题 3：admin 测试的优先级
不全覆盖。admin 是 HTTP handler 层，全覆盖 ROI 极低——52 个 handler 大部分是 CRUD 模板。

分层策略：

text

复制
P0（必须测，覆盖安全+资金）        P1（应该测）              P2（选择性测）

- Login                            - 渠道 6 个                - 查询 10 个

- CreateAPIKey / DisableAPIKey     - 模型价格 8 个              （只测参数解析和权限）

- UpdateAPIKey                     - recordAudit

- TopUp                            - DashboardStats

- BalanceAdjust

- GetWallet

- ListWalletTransactions

- CreateGrant / ListGrants
为什么这么排：

安全：API Key 是访问凭证，Login 是入口，错了整个系统裸奔
资金：TopUp / BalanceAdjust 直接动钱，错了是法务事件
渠道：影响业务可用性，但出错了影响范围有限
模型价格：影响计费，归到 P1
查询：基本都是 SELECT + 分页，测一遍参数解析就够了
测试方式：用 httptest.NewRecorder() + gin.TestMode，每个 handler 一个测试，重点测：

路由正确性（method、path）
认证（401/403 路径）
参数验证（400 路径）
业务逻辑正确性（200 路径，只测关键 case）
副作用（DB 状态变化）
不要测：

写 JSON 的格式（writeJSON 测过了）
半透密钥脱敏（maskKey 内部 helper）
简单的字符串拼接
问题 4：测试文件管理
当前命名混乱是真问题，但优先级低于修 billing 测试。先修测试，后整理命名。

我的命名建议（不强制，照着来就行）：

text

复制
internal/<package>/

  <name>.go                  # 生产代码

  <name>_test.go             # 单元测试（无外部依赖）

  <name>_integration_test.go # 集成测试（需要 DB/Redis）

                               # 文件顶部加：

                               # //go:build integration

                               # // +build integration
build tag 用 integration——这是 Go 社区惯例，go test ./... 默认不跑，需要 go test -tags=integration ./... 才跑。

billing_debug_test.go：直接删掉或者改成 TestFreeze_DiagnoseResultNil 这种名字，加 t.Skip("diagnostic, skip in CI")，留作将来的诊断工具。

文档共存方案：

文档	受众	内容
docs/ops/test-plan.md	PM / 运维	测试范围、覆盖策略、CI 流程
docs/ops/test-report.md	PM / 运维	测试结果、覆盖率趋势、失败分析
docs/testing/README.md	开发（Canary 自己）	测试规范、helper 用法、如何写新测试
docs/testing/strategy.md	开发	当前测试策略、各包覆盖目标、已知 gap
docs/testing/refactor-proposal.md	Codex	接口抽象建议（如果你要提的话）
关键：docs/ops/test-plan.md 必须反映真实测试覆盖，不要画饼。如果现在 gateway 5%，plan 里就写 "gateway 当前 5%，目标 50%，计划 Q3 完成"——这是诚实的，PM 能据此排期。

问题 5：测试环境管理
这是当前最严重的问题，比测试覆盖率严重得多。你直接连生产 DB 跑测试，等于测试在裸奔。

两层防护：

第一层（必须做，立刻）：

独立 test DB：uniroute_test，连接配置在 config/test.yaml
测试启动时 DROP DATABASE IF EXISTS uniroute_test; CREATE DATABASE uniroute_test; 或者 TRUNCATE 所有表
CI 配置：test 环境用 test DB URL，不许用 localhost:5432/uniroute
第二层（推荐做）：

引入 testcontainers-go：CI 每次跑测试启动一个干净 Postgres 容器
本地：docker-compose 起一个 uniroute_test 服务
测试结束时销毁容器/服务
具体落地：

go

复制
// internal/testutil/db.go

func SetupTestDB(t *testing.T) *pgxpool.Pool {

    t.Helper()

    dsn := os.Getenv("TEST_DATABASE_URL")

    if dsn == "" {

        t.Skip("TEST_DATABASE_URL not set, skipping integration test")

    }

    pool, err := pgxpool.New(context.Background(), dsn)

    require.NoError(t, err)

    

    // 跑迁移

    runMigrations(pool)

    

    // 清表（按依赖倒序）

    tables := []string{"audit_logs", "call_logs", "transactions", "freeze_idempotency",

                       "grants", "wallets", "api_keys", "users", "orgs", /* ... */}

    for _, table := range tables {

        _, _ = pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE %s CASCADE", table))

    }

    

    t.Cleanup(func() {

        pool.Close()

    })

    return pool

}
永远不要：

在生产 DB 上跑测试
在共享 DB 上跑并行测试（除非有 schema 隔离）
假设测试前表是空的（必须显式 truncate）
问题 6：前端测试优先级
低优，但不是零。管理后台的特点：内部使用，UX bug 不致命，但功能 bug 影响业务。

不要做的事：

给 Semi Design 组件写测试（库的责任）
给每个组件全覆盖（管理后台不值得）
值得做的事（按 ROI 排序）：

优先级	内容	工具	工作量
P0	关键 utility 单测（token 计算、金额格式化、时间处理）	Vitest	1 天
P0	登录流程 E2E	Playwright	1 天
P1	API Key 创建/查看 E2E	Playwright	1 天
P1	Wallet/Balance 查看 E2E	Playwright	0.5 天
P2	Channel 创建表单（参数验证、提交）	Vitest + RTL	2 天
P3	其他 CRUD 表单	RTL	看情况
建议：

Vitest 配置先搭起来（半天）
Playwright 跑核心流程（3-5 个 E2E 脚本）
不要做组件全覆盖
为什么 E2E > RTL：管理后台是表格 + 表单 + Modal，RTL 测起来巨麻烦（要 mock 各种 store、API、组件库内部状态），Playwright 一键操作真实页面，性价比高得多。

你没问但我必须说的几件事
1. 测试覆盖率的"质量"陷阱
你现在 admin 0.5% 看着很糟，但如果你把 admin 的 52 个 handler 全写上 happy path 测试，覆盖率拉到 80%，但这些测试只调一次 writeJSON 然后断言 status code——这就是覆盖率高但验证力低的典型陷阱。

要追求的不是行覆盖，而是分支覆盖 + 关键路径覆盖：

每个 handler 的成功路径 + 主要错误路径（401/403/400/500）
billing 的所有状态转换（Freeze → Settle / Cancel）
gateway 的 failover 逻辑（第一个 channel 失败切到第二个）
2. 文档和实际测试必须一致
你提到 docs/ops/test-plan.md 和 docs/testing/ 共存——这两个文档必须互相引用：

plan.md 里的覆盖数字要来自测试实际跑出来的覆盖率
testing/README.md 要明确说"我们当前覆盖策略是 X"
如果有 gap（gateway 5%），要明确写在 plan 里，作为已知问题
不要写那种"我们将全面覆盖 XXX"的虚话——PM 看了以为做完了，运维以为可靠了，结果一上线就炸。

3. CI 集成
所有这一切最终要进 CI：

PR 触发：go test ./...（不带 integration tag）必须通过
合并到 main：go test -tags=integration ./... 必须通过
覆盖率报告：上传到 codecov 或类似工具，看趋势
你现在 18 个测试文件、163 个测试——CI 跑一次应该 < 30s。如果超了，要么是测试太慢（需要并行），要么是集成测试没隔离（需要事务回滚）。

4. 关于生产代码的问题
你列了 4 个 code issue：

relay.go:198 defer in loop — 真问题，但 Canary 不能改，提给 Codex
router.go:75 unchecked Exec — 真问题，同上
encryptString XOR 不是 AES-GCM — 安全问题，高优，提给 Codex
billing_debug_test.go:131 nil check — 这是测试 bug，你可以修
XOR 加密那个：如果真的存的是 channel API key 的加密值，这是个生产安全 bug，应该单独立 issue，不要混在测试咨询里。

落地优先级
按这个顺序执行，每完成一步都是一个可交付的里程碑：

序号	任务	时间	交付物
1	修 testutil 编译	30 分钟	internal/testutil/db.go 可用
2	建立测试基建（SetupTestDB, WithTx, 唯一 ID）	0.5 天	internal/testutil/ 完善
3	修 20 个 billing 测试 + 删 debug 测试	1-2 天	go test ./internal/billing/ -tags=integration 全绿
4	配 CI：unit 必过，integration 合并必过	0.5 天	CI pipeline
5	配独立 test DB + TRUNCATE 策略	0.5 天	uniroute_test 隔离
6	admin 关键路径测试（P0）	2 天	Login / API Key / Wallet 测试覆盖
7	gateway 纯函数 + 关键路径测试	2 天	failover、estimateTokens、resolveModelMapping
8	提接口抽象建议给 Codex（refactor-proposal.md）	1 天	docs/testing/refactor-proposal.md
9	前端 Vitest + 关键 E2E	3-5 天	登录/API Key/Wallet E2E
10	文档同步（plan.md、testing/）	1 天	文档反映真实状态
前 5 项是地基，没有它们，加新测试就是在沙地上盖楼。大概 1 周能把地基打稳。

总结一句话
别追覆盖率数字，先修测试基建。20 个失败的 billing 测试是金矿——它们告诉你"这块逻辑重要到值得被测，只是测试本身写得有 bug"。修好它们，再扩 admin 和 gateway，最后才考虑接口抽象。XOR 加密那个安全 bug 单独提 issue，不要跟测试咨询混在一起。

需要我帮你拉 Codex 进来讨论接口抽象的设计，或者帮你写 refactor-proposal.md 的初稿吗？