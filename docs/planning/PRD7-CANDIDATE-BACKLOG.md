# PRD7 Candidate Backlog（候选输入，不是 Accepted Scope）

> Date: 2026-07-11
> Status: Candidate / first future direction selected; all implementation deferred until PRD6 closeout
> Authority: 本文只做 PRD6 收尾后的候选分流、排序和验收设计；不授权实现、部署或外部集成。
> Source of truth: PRD6 的已接受范围仍以 `docs/PRD-Phase6.md`、
> `docs/ops/PRD6-ACCEPTANCE-MATRIX.md` 和用户已确认的 D6 决策为准。

## 1. 结论先行

PRD7 不应成为“把 PRD5/PRD6 所有 deferred 项重新装回去”的容器。建议先让
PRD6 完成当前正确性、租户隔离、计费、链路身份、分布式策略、Guardrail、流式
安全与可升级基础，再从本候选池中逐项重新立项。

用户已在 2026-07-11 接受 D7-UPGRADE-001/002 作为未来第一个 PRD7 产品化方向：
**客户本地、人工批准、可验证签名的离线升级包与只读优先的 Update Center**。
它直接解决 2B 私有化客户打补丁、回滚和证明“装了什么版本”的问题，又不要求
UniRouter 变成托管 SaaS 或自动控制客户环境。该方向选择不是当前实施授权；全部
PRD7 实现必须等 PRD6 闭环后再以独立 PRD7、acceptance row、分支、PR 和 CI 启动。

本文中的 `P6-F12`—`P6-F15`、`P6-F22`—`P6-F31` 仅沿用 PRD6 编号便于追溯。
若其中某项正式进入 PRD7，必须新增 D7 决策和 PRD7 acceptance row，不能把本文的
“候选优先级”解释成接受或实施授权。

## 2. 范围分水岭

| 分类 | 判断标准 | 当前处理 |
|------|----------|----------|
| 留在 PRD6 | 已接受范围内的当前正确性、安全边界和后续能力共同前置条件 | 按 PRD6 matrix、独立 PR、required CI 继续完成 |
| 候选 PRD7 | 有明确 2B 产品价值，但不影响 PRD6 当前正确性，且仍需产品/安全/依赖决策 | 保留候选，不开始大规模实现 |
| 仅目标环境证据 | 源码能力或 harness 已存在，缺少指定环境、凭证、拓扑或授权运行证据 | 不倒推为源码缺陷，不因 PRD7 自动执行 |
| X-Insight/其他项目所有 | 聚合、分析、训练、工具副作用或基础设施运营不应由网关承担 | UniRouter 只定义最小异步合同或本地执行边界，不规划外部实现 |
| 延期/拒绝/需决定 | 当前价值、风险、运营模型或供应链成本不足以接受 | 保持关闭；必须通过新决策显式重开 |

### 2.1 必须留在 PRD6 的工作

以下工作决定现有产品是否安全、可解释、可扩展，不能为了“开始 PRD7”而后移：

| 领域 | PRD6 工作 | 价值 | 主要依赖/风险 | 优先级 | 最低验收证据 |
|------|-----------|------|---------------|--------|--------------|
| 租户与管理员边界 | 完成 P6-F32—P6-F39；当前 F32/F33/F37/F38/F39 已 CI verified（F32/F39 待 PR #145 合并），F34—F36 仍待修复 | 防止跨租户读写、财务影响和证据泄漏 | 历史数据归属、platform exception、迁移回滚 | P0 | 两租户签名 JWT 矩阵；跨租户无副作用；迁移 up/down（适用时）；CI |
| 财务正确性 | P6-F18、P6-F06，以及钱包冻结/可用余额/结算显示语义一致 | 防止重复扣减、漏收、已输出但未结算 | 热钱包行、崩溃恢复、价格/倍率版本 | P0 | 并发单终态、pool-size barrier、故障恢复、账本守恒和 UI/API 一致性 |
| 链路身份 | P6-F17：拆分 operation、client idempotency、trace/span、workflow、Agent、attempt 和 upstream identity | 同一业务调用可跨 retry/failover/billing/audit 追查，又不把外部 ID 当财务主键 | 兼容旧 request_id、跨租户碰撞、索引迁移 | P0 | replay/409/collision 负例；100 子 Agent；retry/failover attempt 证据；迁移 |
| 路由正确性 | P6-F03—P6-F05：错误分类、Embeddings parity、有效配置真值 | 避免错误重试、隐藏硬编码和端点可靠性不一致 | provider 差异、响应已开始后的 retry | P0 | provider 错误矩阵；有界 retry；Chat/Embeddings parity；配置版本证据 |
| Agent 与分布式策略 | P6-F07、P6-F19、P6-F21：短期衰减能力令牌、祖先预算、共享限流和工作负载隔离 | 防止 Agent 爆炸导致鉴权/配额放大与长流饥饿 | Redis 故障策略、令牌刷新、撤销 SLO、基数 | P1 | 多副本并发/故障注入；祖先预算；不提权；公平性和 bounded-cardinality 指标 |
| Routing Policy V1 | P6-F08 的静态优先级/权重、候选排除和 failover 解释 | 为自适应路由提供可信基础 | 必须先完成身份、错误分类和配置真值 | P1 | 确定性选择；候选/排除/回退原因；版本与回滚证据 |
| 用量与持久化 | P6-F09、P6-F20：缓存 token 计费真值、durable finalization、bounded internal outbox | 防止收入和审计因请求结束、进程重启或 worker 失控而丢失 | outbox 租约、幂等、背压；不得扩成 external exporter | P1 | crash/replay/lease 测试；缓存 token 对账；队列上限；无原始内容扩张 |
| Guardrail 与 Streaming | P6-F10、P6-F11：版本化输入/输出策略、PII adapter、逐块检查、终止与结算 | 提供企业安全最小闭环，避免流式输出绕过策略 | detector 故障模式、跨块匹配、可接受披露上限 | P1 | shadow/enforce/failure-mode；跨块 fixture；取消上游；断流后单终态结算 |
| UI 与可运营性 | 审计分类/易懂命名/时间范围、对账语义、request trace、现有后端能力可见性和真实错误状态 | 让 2B 管理员能够理解、定位和纠错 | 不能用 UI 掩盖后端边界缺陷；敏感字段脱敏 | P1 | 角色化页面测试；空/错/加载态；术语合同；API/UI parity；可访问性 |
| 可升级基础 | 统一 SemVer/build/schema metadata、兼容窗口、迁移锁/校验/dirty-state、防并发升级、release manifest schema、备份/回滚检查和 CI release gate | 让未来 patch 可安全产品化 | 迁移不可逆、混合版本 writer、供应链和回滚数据库 | P1 | 重复/并发迁移；旧 writer 拒绝或兼容；manifest/hash；恢复演练；required CI |
| 架构演进 | 保持 modular monolith，逐步拆分 relay orchestration、worker lease、billing finalization 和 admin domain seam | 降低大文件/大流程变更风险，不引入分布式重写 | 大爆炸重构、隐式事务边界、worker 多副本重复 | P1/P2 | 行为保持测试；依赖方向检查；race/load/failure test；无 API 破坏 |

PRD6 的“可升级基础”不等于已经交付 Update Center。PRD6 只建立可验证版本、兼容、
迁移和发布原语；客户可见的离线包、导入/校验/应用体验和发布渠道属于待接受的
PRD7 产品化层。

## 3. PRD7 候选功能矩阵

下表优先级表示“若 PRD7 立项后的建议顺序”，不是 Accepted Scope。

| 候选 | 产品价值 | UniRouter 所有权 | 外部所有权/依赖 | 主要风险 | 候选优先级 | 建议验收证据 |
|------|----------|------------------|-----------------|----------|--------------|--------------|
| D7-UPGRADE-001 签名离线升级包 | 私有化客户可离线打安全 patch、核验来源并可控回滚 | 版本/兼容 manifest、包验证器、迁移 preflight、人工 apply/rollback workflow | 客户运维决定窗口、备份和最终应用 | 供应链伪造、错误包、数据库不可逆、混合版本 | P1（推荐首项） | 篡改/错版本/错架构拒绝；签名/哈希；无网安装；备份恢复；N-1 升级；SBOM/构建溯源 |
| D7-UPGRADE-002 Local Update Center | 管理员清楚当前版本、可用 patch、影响、前置条件和回滚状态 | 默认只读检查、本地导入、preflight、审计、人工批准 | 可选离线介质或客户配置的受信更新源 | 变成远程控制面、自动外联、权限误用 | P1 | 无外网默认；RBAC；CSRF/重放；导入/验证/拒绝/审批审计；失败不改变运行态 |
| D7-UPGRADE-003 Release channels/lifecycle | 为安全修复、稳定版和 LTS 提供清晰支持窗口 | 版本通道元数据、兼容矩阵、deprecation 和 support bundle | 商业支持政策由产品/运营决定 | 通道碎片、长期维护负担 | P2 | stable/security/LTS 升降级矩阵；EOL 提示；可复现 support bundle；不含秘密 |
| P6-F12 外部 secret reference/version/rotation | 跨云凭证安全轮换，避免配置文件静态秘密 | 选定 backend 的引用、版本 pin、热切换、回滚和审计 | Vault/KMS/云 secret 服务的可用性与运营 | 旧 worker、失联、权限扩大、供应商耦合 | P1/P2 | stale-worker、轮换、回滚、outage/fail mode、secret 不出日志；先选一个 backend |
| P6-F15 policy simulation/BYO guardrail/config rollback | 变更前看到影响，支持企业自有 detector，并快速回滚 | policy lifecycle、dry-run、版本/RBAC、adapter contract | BYO detector 的模型/服务由客户或供应方拥有 | 第三方代码/数据泄漏、兼容性、模拟与线上漂移 | P1/P2 | 固定 fixture diff；RBAC/审计；版本兼容；超时/故障隔离；一键回滚但非自动放权 |
| P6-F13 opt-in exact/semantic response cache | 降低延迟和成本 | tenant/auth-aware cache key、TTL、purge、stampede、计费语义 | 可选 Redis/vector backend | 跨租户泄漏、语义误命中、投毒、删除不彻底 | P2 | 租户/权限隔离；加密；TTL/purge；并发 stampede；billing truth；semantic red team；默认关闭 |
| P6-F14 latency/cost/rate-aware 与 canary routing | 在 SLO、成本和容量间动态选择，支持小流量验证 | 可信 telemetry 上的路由决策、约束、解释与回滚 | provider 指标质量、客户成本授权 | 反馈环振荡、局部指标误导、预算失控 | P2 | 离线 replay；稳定性/迟滞；硬约束不被优化器绕过；解释；canary budget/kill switch |
| P6-F22 异步图像/视频 job plane | 将长耗时多模态任务与同步 chat 隔离 | tenant-scoped object reference、job/status/cancel、queue lease、最终结算 | object store、media provider、retention owner | 大对象泄漏、长连接耗尽、重复任务和巨额成本 | P2 | 引用而非大 body；幂等 completion；cancel/refund；retention/delete；负载隔离；恢复测试 |
| P6-F25 显式授权 live shadow routing | 用真实选定流量比较候选模型/策略，不影响主响应 | 独立 shadow request/settlement、paired experiment ID、严格路由边界 | 数据权利、provider、预算和地域授权 | 双倍成本、数据外发、shadow 结果误返回 | P2 | 默认关闭；逐租户/模型/地域/budget gate；永不返回 shadow；独立账本；kill switch |
| P6-F27 verifiable MaaS trust receipt | 客户可验证网关记录的模型、策略和用量声明 | canonical receipt、issuer key/version、签发与验证 | 客户验证工具；不能把声明包装成客观真相 | 隐私、密钥轮换、重放、过度承诺 | P2 | tamper/replay/rotation；最小字段；canonical form；明确 statement-not-truth |
| P6-F30 multimodal provenance passport | 标记多模态输入/输出来源和处理链 | 在 media plane 传播、验证和记录所选 provenance 标准 | 标准/提供商支持；内容所有者 | 标准不成熟、元数据剥离、假来源 | P3，依赖 F22 | 有/无/无效 provenance fixture；签名生命周期；跨模态兼容；未知状态不伪装可信 |
| P6-F23 adaptive-risk assertion 的本地消费 | 让本地网关按短期、只降权的风险快照执行模型侧动作 | 验签、audience/tenant/epoch/TTL、local policy snapshot 和降级执行 | 风险生成/聚合和 policy authority 不属于 UniRouter | 同步耦合、陈旧状态、错误广泛封禁、变相提权 | P3/研究 | 断网可运行；过期/重放/错 audience 拒绝；只能 attenuation；blast-radius 限制 |
| P6-F24 Governance Digital Twin 接口 | 在不触发真实副作用的环境复现策略/模型差异 | 仅提供固定、脱敏、版本化 gateway fixture/manifest 边界 | X-Insight/隔离 evaluation runner 负责分析和评估实现 | 原始数据外流、fixture 不代表生产、误称真实验证 | P3/外部主导 | 零付费调用/外部副作用；provenance；确定性 replay；隐私批准；只称 fixture verified |
| P6-F26 lineage passport 的网关部分 | 把 actor/workflow/purpose/data class/sink 意图可信传递给真正 action owner | 签名上下文传播；仅执行网关可见的模型动作 | Tool Gateway、IAM、数据库/导出服务执行不可逆动作 | 把“携带声明”误称为“已经阻止导出” | P3/跨项目 | forgery/tenant/delegation 负例；capture-gap 明示；sink owner 确定性 allow/deny |
| P6-F28 claim assurance 的网关部分 | 高风险领域可选择更强模型、证据要求或 abstain 策略 | 本地路由/策略执行和可解释决策 | X-Insight 可做异步校准；权威源由领域 owner 维护 | “保证真相”误导、延迟/成本、资料过期 | P3/研究 | 权威域 fixture；coverage/contradiction/abstention 指标；无 truth guarantee；预算边界 |
| P6-F29 approved failure-case manifest | 将经批准失败样本交给外部模型生命周期，而非让网关训练模型 | 只有在新合同获批后，产生最小脱敏 manifest/引用 | 训练、评估、注册、人工 promotion/rollback 均属外部 lifecycle owner | 投毒、隐私、数据权利、自动晋升 | P3/外部主导 | data rights；poisoning screen；holdout；人审 promotion；rollback；禁止原始线上 case 自动训练 |
| P6-F31 collusion/cascade signal 的消费边界 | 未来可对跨 Agent 串谋风险做提示或保守降级 | 只消费已接受、可验签、有 TTL 的策略；不以分数单独硬拒绝 | X-Insight 负责异步语义分析；Tool/data owner 执行动作控制 | 误报正常协作、证据缺口、同步依赖 | P3/研究 | ground-truth fixtures；precision/recall/abstention；乱序/改写/缺失测试；shadow-only 起步 |

### 3.1 不属于 UniRouter 的实现计划

本文只划定边界，不替 X-Insight 或其他项目安排产品功能：

- X-Insight 可拥有异步/近实时状态聚合、跨请求风险分析、漂移/串谋研究、调查 UI
  和评估结果；它不能成为 UniRouter 请求成功的同步依赖。
- 训练、微调、模型注册、promotion 和 rollback 属于独立模型生命周期系统；UniRouter
  只路由到已经批准并注册的模型版本。
- Tool Gateway、IAM、数据库、文件和导出服务必须在真正副作用发生处做授权；
  UniRouter 不能声称阻止了它看不到的工具或数据操作。
- Vault/KMS、对象存储、消息队列和客户基础设施由相应平台/客户 operator 运营；
  UniRouter 只实现经接受的客户端语义和故障策略。
- 当前跨项目合同仍只有 operator-mediated `xinsight.import-receipt.v1` sidecar
  reconciliation。PRD7 候选不自动接受 exporter、outbox 外送或 continuous ingestion。

## 4. 仅缺目标环境证据

以下事项应保留在 target-evidence queue，不放进 PRD7 源码 backlog：

用户已决定将该队列的执行后置到 PRD6 源码闭环之后。后置不等于删除、关闭、已满足
或授权执行；每项仍需要原有的 target/operator/凭证/费用/数据/清理授权。Gate A
临时 snapshot 的 2026-07-17 精确路径删除义务是独立的既有定时决定，不随本队列
后置而延期。

| 事项 | 为什么不是当前源码缺陷 | 开始证据前的授权/输入 |
|------|------------------------|-----------------------|
| P5-F35 具体 route/failover 详情 | PRD5 accepted source scope 已完成 | 明确 target、operator、一次受控请求和脱敏规则 |
| migration `000019` down 与 TLS 443 | 需要选定部署和回滚路径才能证明 | 维护窗口、备份、回滚 owner、HTTPS endpoint |
| 真实 upstream | harness 与本地/CI 不能替代真实 provider | 指定凭证/模型、费用、数据、清理和请求授权 |
| 真实 enterprise SSO | 需要真实 IdP、callback 和测试用户 | IdP owner、测试账号、浏览器和 audit readback 授权 |
| 多实例 100x load/capacity | 必须先定义基准 workload 与拓扑 | 目标 QPS/stream/Agent/媒体 mix、SLO、资源上限和环境 |
| HA/DR/restore | managed topology 尚未选择 | RPO/RTO、备份介质、故障域、恢复 operator |
| Compose binding/生产硬化复核 | 属于具体部署维护 | 精确主机、端口、网络和变更窗口 |
| Gate A 临时 snapshot 删除 | 已有精确日期和路径决策，不是功能需求 | 2026-07-17 前按已接受程序逐个精确路径执行并保留 receipts/hashes/evidence |

目标证据必须继续区分 `Local verified`、`CI verified`、`Target verified` 和
`Production verified`。没有目标运行证据时，不以产品文档、fixture 或 synthetic
结果补写为 Production verified。

## 5. 延期、拒绝或需单独决定

| 输入 | 当前结论 | 重新打开的最低条件 |
|------|----------|--------------------|
| Scheduler V2 全量重写 | 延期；Routing Policy V1 稳定前禁止静默恢复 | 基准负载、V1 瓶颈证据、迁移兼容和回滚方案 |
| Plugin/marketplace/runtime 第三方安装 | 延期 | 明确客户价值；manifest、签名、sandbox、权限、secret 隔离、生命周期和卸载回滚 |
| IAM rewrite | 延期 | 证明窄能力令牌和现有 tenant predicate 无法满足需求，并完成独立威胁模型 |
| Plaintext prompt/response cache | 拒绝 | 不建议重开；与默认不保存原始内容的姿态冲突 |
| External exporter/continuous ingestion | 未授权、不是 internal outbox 的自然延伸 | 新跨项目合同、数据最小化、认证、重放、retention/delete/audit 和 operator 授权 |
| Managed HA/IaC/customer SLA operation | 商业/目标 prerequisite，不是通用代码任务 | 选定云/拓扑、责任模型、RPO/RTO、支持承诺和成本 |
| Public recharge/payment/referral/resale | 按 enterprise-local 定位拒绝 | 产品定位明确转为公共 token storefront 后重新立项 |
| Broad SDK | 延期 | 明确客户语言/框架需求、版本兼容和长期维护 owner |
| Service-account runtime permission expansion | 延期 | 身份模型、最小权限、撤销、审计和威胁模型均被接受 |
| 自动原始线上 case 训练/自动 promotion | 拒绝 | 不应直接重开；即使有外部 lifecycle 也必须数据权利、投毒检查、holdout、人审和回滚 |
| 自动在线升级/远程强制 patch | 默认拒绝 | 只有客户明确选择托管模式、控制权/窗口/回滚/审计合同完整时才另行讨论 |
| 大对象直接穿过同步 chat gateway | 拒绝为默认架构 | 若接受 media roadmap，改为 object reference + async job plane |

## 6. 四象限审计

### 6.1 Known Knowns（已知的已知）

- PRD6 仍有已确认的租户、计费、链路身份、分布式基础和运营 UX 工作；这些是
  PRD7 的前置条件，不是 PRD7 候选。
- UniRouter 的核心产品边界是请求路径 admission/routing/billing/safety 和本地
  权威事实；异步跨请求分析、训练和不可见工具副作用有外部 owner。
- 私有化 2B 客户需要可审计 patch、兼容检查和回滚，但当前产品化的签名离线包与
  Update Center 尚未被接受或交付。
- 自动升级、外部 exporter、continuous ingestion、真实/付费请求和目标环境变更
  均不在本文授权范围。

### 6.2 Known Unknowns（已知的未知）

| 未知 | 为什么重要 | 在立项前怎样消除 |
|------|------------|------------------|
| 客户首选交付介质、CPU 架构和部署形态 | 决定离线包格式和兼容矩阵 | 选择首个受支持矩阵；建议先 Compose + 明确单一架构，不承诺全平台 |
| N-1/N-2 支持窗口和数据库 downgrade 责任 | 决定 rollback 是否真实可行 | 定义版本政策；做至少一次备份恢复和 N-1 升级演练 |
| 外部 secret backend 首选 | 避免同时维护 Vault/KMS/多云矩阵 | 用一名真实客户需求选择第一个 backend |
| response cache 的真实命中率和风险收益 | 低命中率不足以承担隐私/投毒成本 | 只采集无内容的 workload 指标，先做离线价值模型 |
| 100x workload 的组成 | QPS、长流、Agent fan-out、视频任务的瓶颈不同 | 固定 workload envelope、SLO、预算和故障模型后压测 |
| Guardrail/route simulation 是否能代表线上 | 决定 dry-run 结论可信度 | 固定版本 fixture，并测线上/离线 decision divergence |
| 哪些创新接口真的有客户付费价值 | 防止为了“超越竞品”堆研究功能 | customer problem interview + opt-in design partner + success KPI |

### 6.3 Unknown Knowns（组织里已有但未被产品化的知识）

- 现有后端 API、trace、wallet transaction/export、runtime metadata、notification
  状态等能力可能在 UI 中不可见或命名不清；PRD6 应先做 capability inventory 和
  API/UI parity，而不是在 PRD7 重写一套。
- 历史迁移、事故证据、回滚记录和旧 worker 兼容问题已经包含升级产品的重要约束；
  D7-UPGRADE-001 立项前应把这些约束转为 machine-checkable preflight。
- provider adapter、notification worker 和静态 extension seam 已提供部分扩展能力，
  但不等于 runtime plugin platform；应复用窄接口，不误报 marketplace 已存在。
- 客户/operator 可能已经有离线镜像导入、备份和变更窗口流程；Update Center 应
  对接其流程，而不是默认接管基础设施。

发现动作：代码/API/UI inventory、客户运维访谈、历史 migration/incident review、
现有部署脚本 dry-run。发现结果仍需进入正式 PRD7 matrix，不能自动变成范围。

### 6.4 Unknown Unknowns（未知的未知）

- provider 协议、token/cache 计费或媒体任务语义发生不兼容变化；
- 供应链签名 key、离线介质或 build provenance 被攻击；
- 大量 Subagent 形成从未压测过的身份基数、撤销风暴和级联重试；
- 视频/多模态任务把连接、对象存储、队列和结算拖入不同的超长生命周期；
- adaptive routing/guardrail/risk signal 形成反馈环，导致振荡、误封或成本放大；
- 数据驻留、模型许可或行业监管改变可接受的路由和证据保留方式。

控制方式不是提前猜完所有风险，而是保持版本化合同、默认关闭、tenant opt-in、
限幅/TTL/kill switch、bounded queue、兼容矩阵、故障注入、canary 和可回滚发布。

## 7. 建议执行 batches

| Batch | 目标 | 进入门槛 | 退出证据 |
|-------|------|----------|----------|
| D7-0 Candidate Validation | 确定首个客户问题、支持矩阵、KPI、威胁模型和 owner | PRD6 P0 完成；用户选择首项 | 正式 PRD7、acceptance matrix、安全边界、design partner 输入；仍无部署 |
| D7-1 Offline Upgrade V1（推荐） | 签名离线包、manifest/SBOM、导入验证、preflight、人工 apply/rollback audit；Update Center 先只读 | PRD6 version/migration/release foundation 通过 | tamper/离线/N-1/backup-restore/权限/失败原子性；独立 PR 和 required CI |
| D7-2 Policy Lifecycle And Secret V1 | 选择一个 secret backend；policy simulation/rollback；BYO detector 仅走窄 adapter | D7-1 release safety；明确 backend 和 detector owner | rotation/outage/stale-worker；fixture diff；RBAC/audit；兼容与 rollback |
| D7-3 Cost And Routing Optimization | opt-in exact cache，再决定 semantic cache；adaptive/canary routing | Routing V1、计费真值、telemetry 和价值数据可信 | 隔离/投毒/stampede；离线 replay；约束/预算/稳定性/kill switch |
| D7-4 Async Media Plane | image/video object-reference job/status/cancel/settlement；provenance 可后置 | 用户接受 media roadmap、object store/queue/retention | 幂等、租约、取消、费用、删除、负载隔离、恢复；不发送真实付费任务 |
| D7-5 Trust Interface Experiments | 仅选择一个 F23/F24/F26-F31 的窄接口实验 | 数据/owner/合同清晰，fixture-only 优先 | 验签/TTL/不提权/不做同步依赖；precision/abstention；明确非生产证据 |
| D7-6 Authorized Target Evidence | 在一个明确 target 验证已接受能力 | 独立 operator、凭证、成本、数据和清理授权 | target readback、sanitized evidence、rollback；不从 target 自动晋升 Production verified |

任何 Batch 都应拆成可独立回滚的小 PR；不得以“大版本”为理由绕过 backend tests、
frontend tests/build、Docker Build、migration review、security negative tests 和证据等级。

## 8. PRD7 统一验收门槛

正式接受任一候选前，acceptance matrix 至少回答：

1. 客户问题与成功 KPI 是什么，谁愿意为它承担运营成本；
2. UniRouter、外部项目、客户 operator 和真实 action owner 各负责什么；
3. tenant、actor、project、workflow、request/attempt 和 artifact identity 如何绑定；
4. 默认关闭、失败模式、超时、背压、TTL、撤销、kill switch 和回滚是什么；
5. 是否增加原始内容、数据导出、retention、凭证、外部请求或付费范围；
6. schema/API/artifact 如何版本化，N-1 兼容和 mixed-version writer 如何处理；
7. unit/integration/race/failure/load/security/migration 哪些证据必须通过；
8. Local、CI、Target、Production、External integration 证据分别由谁产生；
9. 每个变更是否有独立分支、PR、required CI 和脱敏 evidence；
10. 哪些营销表述禁止使用，例如“零泄漏”“绝对真实”“自动安全”或把 fixture 称为生产验证。

## 9. 安全与授权边界

本文不授权：

- 部署、修改 ECS/安全组/DNS/TLS/目标数据库或启停客户服务；
- 发送真实或付费 provider/media/shadow 请求；
- 创建、轮换、读取或修改凭证和签名 key；
- 扩大数据库、原始 payload、数据导出、retention 或删除范围；
- 启用 external exporter、continuous ingestion 或新的 X-Insight runtime contract；
- 自动训练、自动 promotion、自动提权、自动在线升级或远程强制 patch；
- 把 X-Insight、Vault/KMS、Tool/IAM、queue/object store 或客户基础设施变成
  UniRouter 请求成功的未经接受的同步依赖；
- 静默恢复 Scheduler V2、plugin marketplace、IAM rewrite、plaintext cache、
  managed HA/IaC、public commerce、broad SDK 或 service-account permission expansion。

## 10. 已解决的顺序决定

用户在 2026-07-11 已明确：

1. D7-UPGRADE-001/002 是未来 PRD7 的第一方向，但当前不启动实现；
2. 所有 PRD7 实施和 target-evidence 队列执行后置到 PRD6 闭环之后，各自原有授权门槛
   保持不变；
3. 本地历史分支全部保留，不授权清理或删除。

因此当前没有阻塞 PRD6 的 PRD7 决定。其余候选进入相应 batch 前，再用已量化客户
需求和依赖证据逐项接受、延期或拒绝。
