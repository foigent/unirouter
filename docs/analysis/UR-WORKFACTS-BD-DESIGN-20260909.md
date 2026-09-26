# UR-B/D 独立设计与 X 依赖（待独立评审）

2026-09-09。作者：UR owner `01a08339-f189-7703-acd9-5d0a63a2873b`。本稿只交付设计；没有实施 B/D 产品代码。

## 1. 当前门槛、目标和基线

协调任务 `01a08371-5a0f-7d32-856e-340ccc778825` 已允许在 A 待批期间准备 B/D 具体方案。第 42 份第 45 行及已接受主计划 §9 的门槛不变：A 完整交付、独立验收后，才能进入后续产品实施。本稿的接受也不能代替该验收或 source 凭据控制链所需的直接用户批准。

只在 `/private/tmp/unirouter-workfacts-20260909` 工作，base `cd9f354b8a104a6f0677345a5694b19c000ab015`、tree `7725ffa81b6f6897f5ef6ff843f308c05d44fd63`。当前 A 是未提交的 1.3.0 候选；本稿引用当前源码，不把 base 当作 A 已提交。A 截止证据索引仍为 `/private/tmp/ur-workfacts-test.RDgTyF/health-cutoff-evidence.json`，SHA-256 `1898aed0eed25ed1e1e518f80b240b0659e62fd8ca431cc7cb6f57730635149d`，状态 `partial_A_not_accepted`，不覆盖该索引。

原接受主计划 SHA-256 `0e504bffc251c815680485f1b2dab8b69b335e682ec4d66e586b9b7923a07cbd` 保持原样；本稿细化其中 B/D，明确提出两项新增接缝供评审：预算当前快照读取、enterprise project scope 的显式绑定。没有新增计算费用预算、同步外发阻断、账单来源或 XI 内部领域实现。

验收来源为同一报告目录下第 17、20、21、22、23、33、41、42 份及 `supporting/ur-kickoff-20260909/task-plan.json`。报告目录是 `/Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/`。保留原 13 项与 A—E；UR-B/D 局部通过不关闭父级。

## 2. 已核对事实及其设计后果

| 当前事实与实际入口 | 后果 |
| --- | --- |
| `sourcefacts/contract.go` 的 A Document 强制 request_id，身份含 profile/request/stage，stage 只有 authenticated/admission/dispatch/terminal；M116 表和权限只有三个 A profiles | B 新合同独立定义 attempt/observation 身份；D 不伪造 request_id。不改变 A 编码、身份、hash 或 cursor |
| `modelexecution/coordinator.go:105` 的 Authorize 使用 RR，先 BeginProviderAttemptOwnedTx，再 revalidateAndClaim，最后 commit | 登记不证明执行网络 I/O。新 source 锁不能插在登记之后、原权威锁之前；不改 attempt-first、RR、fence、retry 或资格判定 |
| `billing/provider_attempt.go` 完成 writer 锁 reservation/attempt 后写结果；financial、delivery、health 各有原 owner | 同一 attempt 的网络、交付、财务、health 分开表达，不由任一维度推出其他维度 |
| `gateway/public_nonstream_runtime.go:1042` 记录 write_started，真实 Write 后区分短写/失败/completed；`stream_writer.go:33` 返回本地写入字节及 Flush 错误 | 测量终点取实际成功边界，不能用 header、准备好的文本或旧日志时间；本地成功写出不证明客户端消费 |
| Chat/Embeddings 有原重试限制；public Responses/Messages 遍历 prepared routes；媒体有自己的 max_attempts | 不把所有入口强制改成同一个 attempt 上限；每种入口验证原有实际边界，并按真实 attempt 分条记录 |
| `guardrail.go:1308` 主要向可采样 trace 写 receipt；Result/Receipt 已有模式、版本、action、outcome、降级状态 | trace 不能作为完整历史。捕获当次真实 Result 及后续实际执行效果，不重跑 detector、不根据配置推定效果 |
| 项目预算专用 PUT 及 CreateProject/UpdateProject 都可写 budget 字段；旧 GET 使用 COALESCE | 必须覆盖所有受支持 writer；新合同直接读 nullable 值，并使用预算专属修订，不能使用 project.updated_at |
| `billing/outbox.go` 仅在新 settled journal 且项目 active、budget>0、daily/monthly 时增加 project_budget_usage；M27 只曾在迁移时给特定当前周期做基线 | 该计数不是任意完整周期支出；启用前缺口不能用 0 填平，不能再加一次 journal/call_logs |
| `notifications_outbox.go:199` 只按原 100% 阈值检查，去重键不含预算版本；ack/resolve 由原 admin handler 写入 | 原提醒与现在是否超额是不同事实；不为了供数改提醒去重、增加发送或推出人已读 |
| enterprise BudgetPlan 已有 immutable version、maker/checker、report_only；Position 依赖现有 allocation run | 保留预算与分摊权威；actual 的金额性质、run、coverage、币种/scale 原样解释，不改成 gateway calculated/charged |
| `enterprisefinance/contract.go:505` 只验证 scope kind 与非空 ref；当前受检 BudgetPlan/Position 代码没有把任意 project scope_ref 解析为 UR projects.id 的合同 | `project-1`、`p1` 或数字字符串不能自动授权。需要明确映射才可给项目 source key 看对应 enterprise 预算 |

上述是源代码事实，不是 B/D 测试结果。A 已记录的 authorize FK 40001、非支持在线 reconcile/health 重叠发现保留原裁定；本稿不借 B 扩大其修复。

## 3. 合同、读取和兼容选择

采用有限的 Go 类型和显式分派，复用 `sourcefacts`、`eventconnector`、原鉴权和 PG；不建立通用注册框架、第二财务账本、source publisher worker 或新队列。

| 新固定 profile（拟定名称，实施前冻结 schema/向量） | 主要对象/读取形态 |
| --- | --- |
| `unirouter.runtime-observation.v1` | request 的有限阶段、技术状态、coverage、测量；增量事件 |
| `unirouter.attempt-observation.v1` | 原 attempt 身份的登记、结果、交付与财务观察；增量事件 |
| `unirouter.capability-snapshot.v1` | 当次选定 route 的能力与资格依据，不是全模型目录；增量事件 |
| `unirouter.policy-observation.v1` | 当次策略判断与实际作用；增量事件 |
| `unirouter.destination-observation.v1` | 每 attempt 的允许披露的目标与本地派发证据；增量事件 |
| `unirouter.budget-definition.v1` | 当前所选预算的权威定义与历史修订引用；快照读取 |
| `unirouter.budget-period-summary.v1` | 同预算、期间、金额口径、数据库阅读截止的摘要；快照读取 |
| `unirouter.budget-alert.v1` | 原提醒事件及当前处理/投递状态，含缺口；快照读取 |

A 的三个 profiles、严格 V1 completed feed、route evidence 和各自 hash 域均保留。B events 使用新版本入口 `/xinsight/source/v2/events`，D 使用 `/xinsight/source/v2/budgets` 与 `/xinsight/source/v2/budgets/current`。这些是待评审的相对路径；最终按现有 router 前缀冻结 OpenAPI。v2 capabilities 按 profile 声明 `incremental_events` 或 `current_snapshot`；现有 v1 capabilities/events 响应与 cursor 维持 A 合同，不能把全体 bootstrap 标成同一种模式。

B 继续使用已有 source tables、7 天连续保留 floor、每页最多 50 项/8MiB、5 秒读取期限、原共享读取槽。新增 B schema、descriptor 和 typed codec；存储层接受有限的已验证 publication record，A 仍由原 A codec 生成。B 必有原 request_id；attempt_id 只在真实 attempt 存在时填写。source_fact_id 的 length-prefixed hash 域含新 schema、instance、org/project、profile、request、subject kind/id、observation kind；revision 与前驱 hash 规则明确。原 request/stage 不能碰撞多个 attempts。回放同一原 writer 身份不产生新事件，内容冲突拒绝。

B cursor 用独立版本域并绑定 instance、epoch、org/project、profile、scope_generation；不复用 A token。分页、过期 410、领先 409、撤权、恢复负向向量均保留。终态必须含独立理解所需的 request/attempt/context/route 引用与 coverage；不能要求读到已过保留窗的开始记录才能解释。

## 4. B1：原写入边界与运行事实

运行状态拆为 admission、provider、local_delivery、financial、capture 五个维度。request 计数从可信认证后的 UR request 身份开始；没有可信 project 的认证失败不进入项目 feed。candidate、已登记 attempt、观察到 Do、收到响应分别计数。cache hit 仍保留原 Freeze/I01，但没有新 Provider attempt。技术 completed 需要实际协议和本地交付边界，不能从已扣费、HTTP 200 或 guardrail pass 推出。

| 原 writer/接缝 | 持久事实及故障规则 |
| --- | --- |
| 认证后准备/准入分支、已有 Freeze | request_start；明确的 admission_rejected 留 request=1、attempt=0，含有限原因分类，不创建虚假 I01 |
| coordinator 的 revalidateAndClaim 成功之后、原 tx commit 之前 | attempt_registered + 当次 capability/destination 配置快照；claim 或 source 失败一起回滚，未 commit 不调用 Provider |
| 原 HTTPDoer/执行器调用入口及返回 | 进程内捕获 Do 起止、响应状态或有限失败类别；随后由原结果 writer 持久化。登记后进程消失且无结果，只能 unknown，不能声称未发送 |
| CompleteProviderAttempt、原 financial/hold/delivery writer | 在原事务尾发布此次状态观察；同结果 replay 保持原身份，无新计费、重试、健康更新或 owner capability |
| 实际 Write/Flush 回调及请求退出分支 | 有界测量和 local_delivery；request_capture_closed 只表示已覆盖的本地分支结束，financial 可稍后到达。持久化失败不把内存值报告为已保存 |
| mediajob 原 submit/lease/transition/terminal transaction | 使用真实 job/request/attempt 身份，区分接受异步任务、worker attempt 与媒体完成；不编造同步 TTFT 或文本 I01 |

事务实现使用窄的 source observation 输入和有限 batch，不在 helper 内抢先 commit，也不绕过原 tx owner。一个外层事务先完成全部原业务锁和更新，再执行既有 I01（如适用），最后一次获取本次全部 source profile 锁并发布。需要同时产生 A/B 时合并为这一次 source 发布；不按多次局部排序冒充全局锁顺序。原 `created && materializer` 防重放保护保留。若接缝必须拆分 A 组装与发布，A 的 canonical bytes、事件身份、金额、发生时间和失败原子性需 golden/故障测试证明一致，不能仅凭重构意图认定兼容。

不在 `BeginProviderAttemptOwnedTx` 内提前发布，因为 coordinator 后面仍有权威锁；普通独立 BeginProviderAttempt 路径在自己的外层事务尾处理。health 的 fence/replay 不另产生金融终态。`outbox.EventRuntimeTerminal` 仍服务原 Agent/policy/workload terminal hooks，不因名称相似改作本次运行事件。

不写逐 token、逐 heartbeat 或逐 trace 事件。每 request 的阶段集合固定，每真实 attempt 的 observation kind 有限；策略流式检测在内存按已执行 policy/version/stage 汇总，结果数受原加载规则限制。实施时必须列出各真实入口的边界与最坏字节/行数。超出新文档 64KiB 时不能静默截掉必要事实：拆成固定语义的多条有界事实；未支持输入或失败明确标出，不能靠调低原 retry/candidate 能力控制存储。

## 5. B 测量、能力、策略和目的地

新测量结构固定：`measurement_kind/version/value/unit/state/start_boundary/end_boundary/resolution/source/evidence_ref`。数值用整数字符串；duration 以单调时钟计算，UTC 时刻用于关联，不跨进程相减制造 duration。状态区分 observed/not_applicable/not_observed/legacy_unknown/invalid；真 0 与缺失不同。

- request_response_complete：可信入口计时开始，到完整协议响应成功本地写出；失败/中断另列，旧 latency 不改义。
- stream_first_valid_output：入口到首个协议有效、具有输出意义的帧成功 Write 且 Flush；header/heartbeat/控制空帧不计，首写/flush 失败没有 observed 首输出。
- stream_complete：到最后必要输出/终止帧成功完成协议要求；不取“开始准备最后一帧”的时间。
- attempt_http_round_trip：当次 Do 调用至响应/错误返回，明确含本地网络栈和网络，不命名为纯 Provider 推理耗时。
- request_context_done 保留真实可见原因范围，不证明人主动取消。媒体跨 worker 只有各局部 duration；无同一单调时钟则不拼成精确整体 duration。

UR 提供样本和分组语义；XI 拥有百分位/占比查询。交付 T01–10 原数值向量及 operation/stream/definition 分组、纳入/排除依据；不另建 UR P95 查询引擎。

CapabilitySnapshot 只取本次选定且 commit 的 evidence：operation、surface/protocol、Profile ref/version/hash、模型/Provider ref、Release/slot/binding/Probe/Approval 的允许披露引用及其状态。没有当次证据的字段为明确 unavailable；不从当前配置补写历史，不把 Probe 通过说成每次请求成功。原 route evidence hash 与新 source hash 分开。

PolicyObservation 使用当次 `guardrail.Result/Receipt` 的 policy/version、detector/version、stage、shadow/enforce、configured action、observed outcome、failure mode、degraded/mandatory 与实际 effects。输入阻断、替换后确实采用、输出屏蔽、stream 部分写出后终止分开。shadow 命中不是阻断，检测失败/未覆盖附件不能显示 pass。只输出批准的有限分类与计数，不包含 Texts、prompt、响应正文、命中片段、原始 fingerprints、密钥或完整 detector 配置。不新建同步 data-egress 决策器。

DestinationObservation 按真实 attempt 保存 Provider、规范化 origin（scheme/host/允许的 port）及有明确配置来源的 region；禁止 userinfo/path/query/header/credential。origin 字段是否可披露由该固定 profile 授权决定，原始 URL 不进入文档。配置区域不证明数据驻留；本地 request 写完也不证明 Provider 接收或内容入模。无可靠 content/input binding 时该关联保持 unknown；不由资源读取权限推定外发允许。回退后的目的地与实际 policy 依据独立记录，不能自动沿用上一个目标的外发允许。

## 6. D：当前快照与历史事件的明确区别

选择独立只读快照接口，而不把预算周期刷新塞入模型请求交易或增加定时 publisher。第 23 份 §4 已区分执行 I02 与预算/目录快照。此项需协调者明确接受为主计划 D 的具体实现方式。

`GET /v2/budgets` 是当前授权项目的预算引用目录：原项目 gateway 预算固定一个引用，enterprise 只列显式绑定的 project scope。有限 keyset 页，最多 50 项，不读取整个 org 再内存过滤。目录 cursor 绑定 instance/project/credential scope_generation/目录修订/最后引用；预算定义或绑定增删会改变目录修订，旧 cursor 返回明确重启信号。日常交易支出变化不改变目录修订，避免活跃项目无法翻页。目录分页保证固定目录修订下引用集合；不声称其中每个预算金额来自同一个全局数据库快照。

`GET /v2/budgets/current?budget_ref=...` 每次只读取一个明确预算，在同一 RR 事务中完成当前权限、定义、期间、摘要和有界提醒读取，返回同一阅读快照的对象。提醒超出页面上限时返回独立的有界继续读取信息，不能宣称第一页完整；提醒读取 cursor 绑定该预算及当前权限，说明状态可能在后续读取变化。禁止客户端传 org/project 覆盖 credential 范围。返回 Cache-Control:no-store；暂停/撤销/字段 profile 缺权行为与 A 保持对应边界。

D 不是可回放的 append-only events feed。返回 `reading_id`、`observed_at`、原定义/期间/数据依据引用、canonical source hash；快照中的每个 typed document 绑定 reading_id 与各自 profile，预算摘要绝无 request_id。reading_id 是本次只读观察身份，不是预算版本或费用交易；同一响应重新处理必须幂等。XI 若需保持旧阅读，须保存原始响应及 hash；后续 GET 可产生新阅读，不能覆盖旧阅读，也不能把新 reading 计成新费用。断源时保留旧阅读截止和缺口，不用旧 reading 冒充当前。

不在 A source 表中伪造 D 请求/阶段，不给 A 开历史 bootstrap，不因为 D 消费者首次读取当前预算而回放历史模型经济事件。D 当前摘要可以使用原权威现存的周期事实，明确其覆盖；这是当前状态解释，不是重建旧 I01/source 历史。新资料包提供 D 独立 snapshot schema/manifest/向量及读取合同，不能给 XI 一个假的 event cursor。

## 7. D 定义、项目范围与支出覆盖

### 7.1 原项目预算

原 `budget_cents/budget_period` 不改义。新定义显式给出 setting_state：unset、inactive_zero、inactive_period_none、active、unsupported；读取错误返回 unavailable 错误，不生成“预算为零”。金额 cents 转 micros 乘 10000，checked integer；使用原 charged/project/UTC daily-or-monthly 口径。单独保留预算 revision、changed_at、变更权威引用。项目名/描述修改不增加预算修订，no-op 不增；所有原预算 writer 在原授权事务中原子维护此元数据/历史值。首次 D 迁移保存 `observed_baseline`，不把未知历史修改时间赋为 project.updated_at。

预算中途改额形成新定义，同一周期的支出仍从周期开始计算；旧阅读保留旧预算/差额。差额为预算减同口径已确认支出，不是钱包余额、可用额度或执行许可。reserved、calculated、charged 不混合。

摘要最低充分实现：直接从现有 durable financial journal 按项目/原发生期间计算同口径 settled charged subtotal，增加必要的窄索引，不建立第二个金额累计器。异步媒体金额若不是该旧 gateway 预算权威的一部分，明确 excluded，不偷偷加进旧口径。原条件式 project_budget_usage 可作为 `legacy_counter` 对照依据，不能与 journal 相加。M27 历史 call_logs 基线只有存在可证明互斥边界时才可单独引用；本轮不自动补做基线。

新增非金额的 coverage 起点元数据：在所有受支持 writer 的 journal 合同已核对、source schema 扩展前置条件成立的维护迁移中，记录 `certified_from` 与其 runtime/schema 证据；不根据第一行 journal 推断起点。只有 period_start 不早于该起点、原 journal 未经历未证明恢复/擦除，并且本次阅读明确约束 materialization 截止，才能把该区间称完整。既有完整 journal 的历史覆盖如果无法证明，返回 known_subtotal + missing_interval/reason；这不自动阻止未来完整周期的真实正向验收。

`materialization_state` 单列 pending/settled_as_of/unknown：同一 DB 快照检查适用原 financial outbox 的 pending/dead-letter 等状态，已知 pending 不能藏在“完整”后。无待处理只证明截至阅读已提交的终态可见，不承诺未来不会迟到；迟到会产生新阅读/摘要，旧阅读不改。coverage 证明还须核对原 restore/reset 路径，不由“当前查无 DELETE”证明永久完整。恢复身份/截止不确定时降为 gap，不能仅恢复一个布尔位就报完整。

全周期 complete 且 subtotal=0 才能供出“本周期确认为零”；中途开启、断源、未认证 schema、读取失败或未决 materialization 不使用这个结论。月初 UTC/上海 08:00、结束边界 exclusive，以及 999.999999 对 1000 的精确比较均进入原 B 用例。

### 7.2 enterprise 预算和项目 scope 绑定

BudgetPlan/版本/审批、FiscalPeriod、AllocationRun、分摊结果仍由 `enterprisefinance` 原 writer 拥有。源投影继续 report_only，引用原 run/source revision，保留 exact/partial/no_allocation_run/unit_mismatch/not_modeled。不从未建模的 committed/forecast 产生零，不把 allocation actual 重命名为计算费用或新增扣费。

本稿建议新增窄的显式绑定：`(org_id, enterprise_project_scope_ref) -> UR project_id`，带 revision、当前有效状态、批准 actor/audit 引用和双向组织一致性约束。仅允许 ScopeProject；部门/成本中心/org 预算不能借此映射后暴露给单项目。由现有有权管理 enterprise budget 的 org 角色在原管理面明确维护，source credential 只读，不能建立/修改绑定。旧任意 scope_ref 不自动迁移、不按显示名或数字猜测。绑定是当前可见性依据，需独立权限审查；其实现不由本稿自动获得超出已有范围的批准。

读取 D 必须先 SQL 限定当前项目绑定，再取对应 project budget 及 target_kind/ref 匹配的分摊切片；不输出 org 总金额、其他 target、全 org source/result 数量、人员目录或原始行。可返回经 scope 安全审查的原覆盖结论及 run 引用，不能透传整个 Position 结构后再删字段。绑定撤销/变更使对应目录/current/提醒读取失效，更新目录及相关 source scope_generation；旧缓存/链接不能借旧绑定访问当前接口。已有 XI 副本的撤权仍由 XI 当前阅读权限负责，本地源端拒绝不证明远端已删除。

无绑定的 enterprise 预算为“该来源范围未配置”，不能作为有零个 enterprise 预算的组织级断言。完整 D Local 正向测试必须通过原管理权限建立合成明确绑定、创建/批准原 BudgetPlan、应用原 AllocationRun，再用受限 source credential 实读；不能全用 unknown 通过。绑定如果审查发现不应纳入，必须重新确定安全的正式 scope 方案；不能跳过该既有 enterprise 目标称 D 完成。

### 7.3 提醒

读取原 notification event 的触发时金额/周期/阈值、事件引用、当前 ack/resolve、投递状态及各自截止。原事件没有预算版本时返回 version_unknown，不事后按当前 budget 补历史值；新 writer 可在原事件创建时附带当时定义引用，旧 dedupe 与投递仍原样。ack/resolve/投递成功不说明人已读，也不说明现在仍超额；当前 comparison 在 summary 中独立计算。不新增收件人、webhook、提醒频率、重发或 hard block。

## 8. X 配套、迁移与依赖顺序

| 可先确定、不依赖 A 待批控制链的设计 | 必须等待 A 验收/后续批准与证据 |
| --- | --- |
| B 的原 writer、状态/计时定义、最小披露字段、原 R/T/CT2 用例；D 的金额口径、coverage、snapshot 及绑定设计 | B/D 产品编码、最终迁移前驱和 digest、接入权限及新 profile 生效 |
| X 的新增 profile/transport 支持矩阵、默认关闭、候选包结构及恢复场景 | source credential 正向 HTTP/cursor/消费者、管理 UI、绑定的权限实施及浏览器实测 |
| 原合同与新合同分别列字段/未知/单位/截止、独立消费者测试计划 | 用真实已验收 A 对齐新 codec/Envelope/profile grants；不能用当前 A 候选当正式 pin |

B 增量迁移扩展 profile allowlist、连接授权上限及新 source schema 有限集合；D 迁移只增加定义/绑定/coverage 的必要元数据、历史及查询索引，不重建原财务账本。迁移编号在 A 真正交付后按当前 ledger 冻结，当前不创建不可变编号或签名候选。新增 profiles 默认不授予已有 source key；新字段权限由明确 profile grant 决定，不能因升级自动扩大。

沿 A 既有 finite bridge/manifest/compatible-expand、客户 installer/preflight 和恢复合同推进，仍需精确迁移 ledger、活跃与恢复 generation 支持、旧 head、原 stream counter/Release 资格验证。D 预算 writer 新元数据要求所有受支持 writers 匹配；不能只升级 API 而让旧恢复进程继续产生不可解释修订。验证空未使用 down；存在新事实/绑定/运行依赖时受 guard 拒绝，不删证据换取旧 binary 启动。

OpenAPI、路由 parity、接入管理的 profile/transport 说明、默认配置、版本元数据、customer compose/preflight、upgrade validator、合同包及 docs inventory 同步。UI 展示未配置/暂停/可拉取及预算 scope；ack 不显示为“XI 可查询”。不另建预算编辑器或用 source UI 替代原 enterprise 审批面。

仍无新服务、Collector、消息集群、共享数据库、XI 在线依赖或图表库升级。Docker、exact Neutral N1、真实 XI、远端 CI/正式制品/部署分别按当前缺口保留；不安装工具或用文档通过替代未运行检查。

## 9. 验证和完整交付

以下全部是待执行验收，不是已通过。只在 A 通过且本稿达成一致后实施。

| 原验收 | B/D 必需 Local 输入与预期 |
| --- | --- |
| CT2-A01–08、R01–08、T07/T09/T10 | 真实 handler + 原 planner/coordinator + 隔离 PG/Redis + fake Provider。一次完成、准入拒绝无 attempt、两次真实 attempt 回退、部分流中断、登记后崩溃未知、cache、新旧采集开关组合；request/attempt/模型/财务分别守恒 |
| T01–06/T08 | 可控时钟/真实 Write/Flush 故障；3000 vs 旧 5200、2250/6800 vs 旧 TTFT200、首写失败、真零/旧零、原连续插值向量、分组分母、跨日/迟到/重放。数值单元加真实 HTTP 接缝，不只测试 schema |
| COMP/CT-01、S02–03、P2-A03–05 | 当次 Profile/protocol/Probe/Release pin；配置变更后旧快照不变；shadow/enforce、允许/替换/阻断、输出失败/检测失败/降级、工具/附件类型未覆盖；原 route/guardrail 合同回归 |
| DATA-A01–14 的 UR 部分 | 每 attempt 目标与已有决定、回退变更、部分派发/输出屏蔽、凭据/URL/payload 泄露负例、asserted 标签不升权、缺 input binding 不伪造关联。资源访问/真实输入链及 DATA-A14 父级正向仍由实际源与 XI 联合验收，不以全 unknown 关闭 |
| B01–15、FA3-A01–10 | NULL/零/none/错误，600/1200 对 1000，UTC 日/月，月中已知 200+缺口，摘要600/明细200不加成800，1000→1500 后旧阅读保留，原去重，charged0/calculated400，reserved单列，精度/币种/scale/scope不符，提醒已处理但仍超额，完整零和断源未知 |
| FA-02 enterprise 部分 | 原 CreateDraft/Approve/ApplyAllocationRun 正向完整切片、partial/no-run/unit mismatch、费用守恒；同 org 另一项目/成本中心拒绝，绑定伪造/撤销/旧 cursor/旧链接拒绝；不产生新财务 journal、收费或修改原分摊 |
| GX/EXT/恢复 | 默认关闭与旧 consumer 原 bytes；新 profiles 未自动授权；B 保留 floor/前驱/序列/混合 replay；D 目录变化重启及单预算原子阅读/旧 reading 保存；备份恢复 source identity、定义/绑定修订、coverage、pending 原 outbox，认证撤权与 scope_generation |

协议范围包含实际受支持 Chat、Embeddings、Responses、Messages 各自非流/流和 async media；不支持的组合按原合同拒绝，不能为了覆盖虚构 endpoint。认证维度复用 A 的 legacy shared/personal/application、personal/workload grant、Agent 委托核心夹具，在历史身份/金额/权限敏感接缝覆盖差异，原逐项要求不削减。

验证 source 发布失败发生在 dispatch 前时不调用 Provider；dispatch 后故障保持原 hold/recovery，不能通过丢弃失败观察改善结果。并发精确 replay、锁等待/取消、反序 profile、预算变更与 settlement、binding 撤权与读取同时发生均实测。预算 journal 查询需 EXPLAIN 与实际规模；不将缺少索引导致超时误判为 unknown。

性能必须在 A 接受候选上独立测 B/D 默认关闭/启用及组合，保留相同请求/负载/原结果、真实原 outbox 恢复、source pull→独立消费者数据可用时间、读写争用、行数/字节/WAL。A 的约 1ms 结论不能继承为 B/D 通过；不加入 fake Provider sleep 稀释比例，不用混合失败/409 的 p95 冒充全部成功。

候选稳定后完成适用全 Go race/vet、原 CI 选择的本地集成、前端 type/tests/build、API/route/hash/schema/版本/安装及恢复检查；Docker/精确 N1 等缺失仍标待环境，远端 Change Routing/required checks/exact-main 没运行则不报通过。源合同包包含有限 schema、合法/非法页面/快照、身份/hash 向量、真实接口样本和 coverage/迟到/恢复/权限说明。只交协调任务，不派 XI。

本次只验证文档与源码引用，没有 B/D 运行结果。下一步是协调者对本稿精确摘要的独立审阅与差异讨论；共同确认后仍等 A 完整验收才编码。实施完整交付需绑定新的 commit/tree/diff、测试与失败证据，协调者整体独立验收。原 US/FA/GX 及 A—E 状态逐项回填，绝不以文档完成替代产品完成。

## 10. 需要本次独立审阅明确裁定的内容

1. B 在外层事务尾有限合并发布，保留原登记、资格、财务、health 与 I01 顺序；同一事务 A/B 锁与兼容验证是否充分。
2. D 独立 current snapshot/目录合同及其阅读截止、历史保存责任；不提供虚假可回放事件历史。
3. 项目预算直接 journal subtotal + 有证据的 coverage 元数据，既有条件累计只作对照；完整正向周期、迟到和恢复证明是否足够。
4. enterprise ScopeProject 的显式项目绑定是必要权限接缝；最小字段、授权角色、当前撤权和切片泄露边界是否正确。

以上属于具体方案审查，不请求泛化产品改向。发现必须新增产品权限或改变已接受范围时，单独列出精确差异；一般技术修订由 UR 与协调者讨论裁定。A 的 source credential 自动审批阻塞继续单列，设计讨论不能视为其授权。

经验文件无需变化：当前是只读设计，没有新增运行证据达到经验准入门槛。
