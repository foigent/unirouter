本回答不迎合、不猜测，直击当前工程实践中的痛点，并为 Canary（测试 Agent）提供可落地的执行策略。
问题 1: 测试策略 — "不改生产代码"约束下的可行方案
核心结论：短期内坚决执行“真实 DB 集成测试”，中长期以“技术债报告”倒逼架构重构。
在硬编码 *pgxpool.Pool 且无接口抽象的现状下，Mock 是死路（除非修改生产代码）。Canary 当前的可行策略如下：
当前迭代（执行层）：全面转向基于独立环境的集成测试（选项 A + D）
放弃对 DB 强依赖包的单元测试幻想。对于 gateway、billing、admin，直接编写集成测试。
使用 httptest 构建 API 级别的集成测试，这比直接调用 handler 函数更能暴露中间件（Auth, RateLimit）和路由的集成问题。
数据隔离策略（解决无接口导致的测试污染）
由于不能改代码注入 mock DB，测试必须依赖数据隔离。
方案：在测试 Setup 阶段，生成全局唯一的 UUID 作为 org_id 或 tenant_id。所有测试数据的创建、查询、断言都严格绑定这个 UUID。测试 Teardown 阶段，直接通过 SQL DELETE FROM orgs WHERE id = $1 CASCADE 进行物理清理。
向 Codex 提交《架构可测试性技术债报告》（推动层）
Canary 的职责虽然是不改代码，但有责任指出阻碍测试的架构缺陷。
在测试报告中明确指出：“由于缺乏 Repository 模式或 DB 接口抽象，核心业务包的单元测试覆盖率存在物理天花板（最高约 30%），且集成测试运行缓慢（每次需连接真实 DB）。建议 Codex 在下一迭代引入 DBTX 接口（包含 pgxpool.Pool 和 pgx.Tx 的交集）。”
问题 2: billing 集成测试全挂 — 修复还是重写
核心结论：坚决重写（Rewrite），现有的测试代码不仅是“失败”，而是“有毒”。
当前的失败模式暴露了测试代码和生产代码的双重严重问题：
删除 billing_debug_test.go
包含 TestDebug 且出现 nil pointer panic 的文件，说明这是开发人员在本地调试时随手写的“脏代码”，根本不具备 CI 运行价值，直接删除。
重写 billing_integration_test.go，建立标准脚手架
唯一键冲突 (orgs_name_key)：说明测试没有数据隔离，或者反复使用相同的硬编码数据。重写时必须在 TestMain 或每个测试的 Setup 中使用随机生成的 org_name 和 org_id。
事务中止级联 (SQLSTATE 25P02)：这是 PostgreSQL 的标准错误 InFailedSqlTransaction。注意：这不仅仅是测试问题，这极大概率是生产代码 billing.go 的 Bug！
深度分析：当 billing.go 在一个事务中执行多个 SQL 时，如果第一个 SQL 失败（例如余额不足），代码没有正确执行 Rollback 或提前 Return，导致事务处于 aborted 状态，随后继续执行第二个 SQL 触发了 25P02。
Canary 行动：在测试中捕获此错误，并将其作为 P0 级生产 Bug 提交给 Codex。预冻结计费逻辑如果出现事务未回滚，会导致严重的资金账本不一致。
修复测试代码的 Nil Panic
重写时，严格遵守 Go 的错误处理规范：if err != nil { t.Fatalf(...) }，在确认 result 不为 nil 之前，绝不访问 result.Success。
问题 3: admin 测试的优先级
核心结论：放弃“全覆盖”执念，实施基于风险的分级测试（Risk-Based Testing）。
2242 行、52 个 Handler 的 CRUD 后台，追求 100% 覆盖率是典型的“虚荣指标”。管理后台的核心风险在于越权和资金算错。
P0 级（必须覆盖，核心资金与安全）：
安全 (5)：Login (防爆破/鉴权), Create/Disable/Update APIKey (防越权，确保 A 组织不能操作 B 组织的 Key)。
资金 (7)：TopUp, BalanceAdjust, CreateGrant。这些直接修改 wallets 和 transactions 表，必须验证金额计算、事务一致性和审计日志 (audit_logs) 的正确记录。
P1 级（应该覆盖，核心路由配置）：
渠道与密钥 (10)：Create/Update/Test Channel。配置错误会导致线上请求全部 502。需要验证渠道状态机的流转。
模型价格 (8)：Create/Update ModelPrice。价格配错会导致网关疯狂亏钱或乱扣费。
P2 级（延后或抽样，常规 CRUD）：
用户管理、常规查询 (ListLogs, DashboardStats)、敏感词管理。这些大多是简单的 DB 映射，出严重 Bug 的概率极低，可依赖前端联调和人工验收。
问题 4: 测试文件管理
核心结论：建立严格的物理隔离与命名规范，区分“工程文档”与“运营文档”。
文件命名与 Build Tags（强制规范）
单元测试：xxx_test.go。只测纯逻辑（如 tokenutil, filter），无外部依赖。
集成测试：xxx_integration_test.go。必须在文件顶部添加 //go:build integration。
理由：集成测试依赖 DB/Redis，运行慢。日常 go test ./... 只跑单元测试（秒级），CI 流水线或发版前使用 go test -tags=integration ./... 跑集成测试。
清理垃圾文件
全局搜索并删除所有 *_debug_test.go 或包含 fmt.Println 调试输出的测试文件。
文档共存策略
docs/testing/ (技术文档)：面向 Agent 和开发者。包含《测试架构设计》、《Mock 策略》、《如何本地运行集成测试》。
docs/ops/test-plan.md & test-report.md (运营/验收文档)：面向项目经理和人类。包含《v1.2 版本测试覆盖率趋势》、《遗留 Bug 清单》、《发版门禁评估结论》。两者定位完全不同，互不冲突。
问题 5: 测试环境管理
核心结论：绝对禁止连接开发/生产库，引入轻量级容器化或严格的 Schema 隔离。
直连 localhost:5432/uniroute 是极度危险的行为，测试产生的脏数据会污染开发环境，甚至触发定时任务导致灾难。
数据库隔离方案
首选方案 (Testcontainers-go)：在 TestMain 中动态拉起一个 PostgreSQL Docker 容器，运行 migrate，测试结束后销毁。这是目前 Go 生态最干净、最标准的做法，完美解决环境一致性问题。
备选方案 (本地独立 DB)：如果 Docker 环境不可用，必须在本地创建 uniroute_test 数据库。在 TestMain 中执行 TRUNCATE TABLE orgs, users, wallets... CASCADE 进行硬清理。
Redis 隔离方案
修改测试配置，强制连接 Redis 的 DB 15 (redis://localhost:6379/15)。
在测试 Setup 中执行 FLUSHDB，确保缓存隔离。
问题 6: 前端测试优先级
核心结论：低优先级。放弃全面的组件单元测试，仅针对核心链路引入轻量级 E2E。
管理后台（React + Semi Design）的特点是：UI 组件库成熟、表单逻辑多、复杂交互少。写大量的 Vitest + RTL (React Testing Library) 单元测试，维护成本极高，且往往沦为“测试代码比业务代码还多”的窘境。
放弃前端单元测试 (Vitest)
不要去测 Button 是否渲染，不要去测 Semi Design 的 Table 是否正确分页。这些是组件库的责任，不是你的。
引入轻量级 E2E (Playwright)
只覆盖 P0 黄金链路：
管理员登录。
创建组织并充值 (TopUp)。
创建 API Key。
配置渠道和模型价格。
使用 Playwright 编写 3-5 个端到端脚本，集成到 CI 中。这比 100 个前端单元测试更能保障运维人员每天使用的核心功能不崩溃。
🚨 额外建议：Canary 的质量门禁职责 (Quality Gate)
作为测试 Agent，Canary 的核心价值不仅是“写测试”，而是“守住质量底线”。基于你提供的代码问题，Canary 必须立即向 Codex 发起阻断性报告，在以下问题修复前，拒绝签署任何版本的测试通过报告：
P0 资源泄漏 (relay.go:198)：循环内 defer resp.Body.Close() 是经典的 Go 内存/FD 泄漏陷阱。在 Failover 频繁发生时，网关会在几分钟内耗尽文件描述符并崩溃。必须移出循环，在每次迭代末尾手动 Close。
P0 状态不一致 (router.go:75)：r.db.Exec() 返回值未检查。如果 DB 更新路由状态失败，网关会认为更新成功，导致后续请求路由到已失效的渠道。
P0 安全漏洞 (encryptString)：上下文交接明确要求 AES-GCM，代码却使用 XOR。XOR 在现代算力下等同于明文，这是严重的安全合规事故。
资金账本隐患 (billing 事务)：如前文所述，SQLSTATE 25P02 极大概率暴露了计费模块在异常情况下没有正确回滚事务。在预冻结计费场景下，这会导致“钱扣了但订单状态没变”或“订单成功但钱没扣”的严重客诉。