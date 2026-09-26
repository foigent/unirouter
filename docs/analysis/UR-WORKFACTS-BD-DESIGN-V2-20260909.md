# UR-B/D 独立设计与 X 依赖 v2（修订后待共同确认）

2026-09-09。作者：UR owner `01a08339-f189-7703-acd9-5d0a63a2873b`。本稿只交付设计；没有实施 B/D 产品代码。

本稿是对协调者四项规划层 `validated_required` 的集中修订，替代原稿作为本次审阅对象。原稿 [UR-WORKFACTS-BD-DESIGN-20260909.md](UR-WORKFACTS-BD-DESIGN-20260909.md) 保持逐字不变，SHA-256 `c22f9895803edc777030e83bb429f0798219e6ff6875fe7c6a12439482d94f27`。这四项不是声称运行时已复现的新故障。共同确认尚待协调者回读本稿。

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

A 的三个 profiles、严格 V1 completed feed、route evidence 和各自 hash 域均保留。B events 使用新版本入口 `/xinsight/source/v2/events`，D 使用 `/xinsight/source/v2/budgets` 与 `/xinsight/source/v2/budgets/current`。这些是待评审的相对路径；最终按现有 router 前缀冻结 OpenAPI。v2 capabilities 按 profile 声明 `incremental_events`、`current_snapshot`，runtime profile 可同时支持历史增量与按需当前租约读取；现有 v1 capabilities/events 响应与 cursor 维持 A 合同，不能把全体 bootstrap 标成同一种模式。

B 继续使用已有 source tables、7 天连续保留 floor、每页最多 50 项/8MiB、5 秒读取期限、原共享读取槽。新增 B schema、descriptor 和 typed codec；存储层接受有限的已验证 publication record，A 仍由原 A codec 生成。B 必有原 request_id；attempt_id 只在真实 attempt 存在时填写。source_fact_id 的 length-prefixed hash 域含新 schema、instance、org/project、profile、request、subject kind/id、observation kind；revision 与前驱 hash 规则明确。原 request/stage 不能碰撞多个 attempts。回放同一原 writer 身份不产生新事件，内容冲突拒绝。

B cursor 用独立版本域并绑定 instance、epoch、org/project、profile、scope_generation；不复用 A token。分页、过期 410、领先 409、撤权、恢复负向向量均保留。终态必须含独立理解所需的 request/attempt/context/route 引用与 coverage；不能要求读到已过保留窗的开始记录才能解释。

## 4. B1：持久责任、失败边界和当前运行

运行状态拆为 admission、provider、local_delivery、financial、capture 五个维度。request 从可信认证后归属项目的 UR request 身份开始；无可信 project 的认证失败不进入项目 feed。candidate、登记 attempt、观察到 Do、收到响应分别计数。cache 保留原 Freeze/I01，无新 Provider attempt。已扣费、HTTP 200 或 guardrail pass 都不能推出技术 completed。

### 4.1 必需记录和附加观察

B 不新增“测量失败”金融状态。必需原子记录是原执行权威/结果/金融/交付记录，以及本稿明确的派发前 B 最小登记；附加单调测量、策略内存汇总和直接 source 投影不成为原收费成立条件。实施接口必须把这两种错误返回分开，不能一个 error 冒充两种责任。

| 接缝 | 谁持久化，何时原子 | 失败/进程退出及重放依据 |
| --- | --- | --- |
| 认证后 request_start/原 Freeze、明确准入拒绝 | 沿 A 原可信认证/准入接缝；B 最小 request/拒绝事实在向调用方结束该分支前写入。request=1/attempt=0 的拒绝不造 I01 | 此前尚未派发，所需最小记录失败可返回 source unavailable，不能调用 Provider；未持久则分母 coverage 有缺口，不把拒绝当已供数 |
| coordinator 成功 revalidateAndClaim 后、原 commit 前 | 在原 tx 尾原子发布 B attempt_registered、选定 capability/destination 最小快照；同 request/attempt/原 route evidence，仍由 coordinator 决定派发 | 失败一起回滚，无 Provider 调用；原 commit ambiguity/fence 决定能否继续。不能在 BeginOwned 之后、权威锁之前发布 |
| CompleteProviderAttempt、原 hold/financial/delivery writer | 原 billing/provider/media 状态及 receipt 仍按原合同持久并 commit；其必需失败仍走原处理。B 的即时结果投影在原 commit 成功后用独立、有界 source-only 事务追加，错误不返回为原业务失败 | 原持久结果是可靠依据；独立投影失败记录结构化诊断/计数，已知结果不倒改，不造新 hold/重试/扣费。不能重放不存在的中间版本；原表只有最终态时，之后只能提供“按当前持久态核对”的事实，不能补造此前事件史 |
| 原 financial outbox 的终态 materializer | 已提交的原金融 outbox payload、冻结 context/basis、原持久 receipt 支持金融终态与可核对的最终结果。I01+A+B 必需终态在原 journal transaction 内发布，保留 original created guard | 失败只让原 outbox 重试 journal/feed 投影，不能回滚已经成立的客户收费或新增 hold。原事件重放、过保留窗 created=false 不重建旧页。不存在于可靠输入中的测量/策略细节必须缺失 |
| Do 起止的单调测量 | gateway 执行器捕获；原结果 commit 后由 source-only 事务追加 measurement，绝不把单调时间序列放入费用判定 | 内存尚未持久就退出，没有可靠重放依据，值为 not_observed；不能由两个墙上时间、updated_at 或旧 latency 反推。若 source-only commit 成功而回包丢失，同身份/bytes 重放幂等 |
| Write/Flush 之后的首输出/完整输出测量 | 原 RecordDelivery/stream checkpoint 先履行原必需 receipt。测量在相应有限输出边界追加，非逐帧写入。原 receipt 的成功本地交付与 duration 分开 | 仅附加测量失败：已完成本地交付保持 completed、duration not_observed、金融照原 receipt 继续。原 receipt 自身失败：原恢复/不确定性合同处理，不能把内存成功冒充 durable delivery。之后重放 receipt 不恢复丢失单调 duration |
| 当次 guardrail Result 的策略汇总 | 原 guardrail 仍先决定真实 allow/replace/block；在有限 input/nonstream/stream-end 边界用 source-only 事务保留结果及已观察作用，source 失败不改变原策略动作 | trace 不是可靠全量重放源。崩溃/写源失败且原 durable receipt 没有相应细节时，policy observation 为 not_observed/gap；不重跑 detector、不从当前配置复原历史。真实 block 必须继续 block，不能因附加记录失败放行 |
| 媒体 submit/dispatch/transition/terminal | 原 job/lease/reservation/charge writer 继续拥有原子事实；派发前最小 B 登记随其原 tx；后续附加测量遵守上面的 source-only 边界 | 原 Claim 对派发后过期 lease 产生 provider_outcome unknown，保留原规则；不得凭无 B 结果再派发或造文本 I01 |

附加观察接口不启动新 publisher/outbox 类型或长期重试器。其 source-only 事务只取 source 锁，原业务 tx 已结束；遵循原请求剩余期限/既有 bounded context，不延长业务恢复或强加新 hold。新 API 返回的 `capture_expected` 列出已覆盖的有限 observation kind；只有相应成功 source 记录才能标 observed。缺种类、进程退出或最后记录太旧都显示 not_observed/gap，不能从最后一个阶段推导 capture 完整。原金融 terminal 可以自足携带可核对的结果及缺失说明，但不伪造丢失的中间事件、计时或策略决定。诊断日志丢失也不影响这一保守解释。

正常正向仍须实际 handler→原 writer→source page 提供准入/attempt/结果/测量/策略事实；“附加可失败”不是允许在正常路径不实现，或用全 unknown 通过验收。必需投影与附加观察不同的故障注入必须分别证明上述边界。

### 4.2 汇合发布和升级共存：保留原 outbox 尾部顺序

修正原稿“所有业务先做完再 I01/source”的总括规则：**不移动 `billing/outbox.go:240–324` 的预算尾部**。选择最小结构，既有与新增 writer 都使用：

`INSERT journal / 原 terminal hook → I01 → 一次汇合 A+B 必需 source → 原 budget read / usage increment / budget-evaluate enqueue → commit`。

`main.go:471` 的现有 V1+A composition 改为有限 V1+统一 A/B materializer；保留一次 source profile 全集排序，不能 A 先取锁后再单独为 B 局部排序。A 文档仍用原 `TerminalRecord.OccurredAt`、冻结 snapshot/basis 和原 BuildSnapshot 输入，不改为后移的 DB clock；I01 先于其 reference 校验。`created` 的分支、budget 未配置/项目不存在/cancelled 的原 commit 分支，以及 source/预算失败导致原 journal tx 回滚都保留。

D 为 snapshot 投影，其预算定义/绑定/coverage writer 不进入 A/B source 发布，也不从预算锁反向索取 stream 锁。B 派发前是原 reservation/authority locks→source；派发后的附加写是 source-only tx。由此无须为本次汇合新增“budget→source”顺序。不是靠“同一事务”证明安全，实施仍以这些具体路径验证锁图和 PG 并发。

允许的共存对象明确如下：

| 真实前后态 | 本次允许条件 |
| --- | --- |
| 当前 A/M116 writer 与新 B/D bridge 尚在 M116 | B/D 关闭，新 bridge 继续完全相同的 I01→A→budget 顺序；只能读取/写入 A 的原 schema。原 A API/source bytes 的兼容测试包括此共存 |
| 扩展至 B/D schema 前 | 沿已有支持的 fleet/manifest/lease/compatible-expand 或客户维护入口确认所有 serving 与 recovery writers 已具备精确新 schema/合同支持；旧 A binary 不认识新 head，不能留作恢复 writer。受支持维护流程无法证明这一点就不 apply，不新增滚动升级承诺 |
| 扩展后多个新版本进程、B 开关不同 | 都保留同一 I01→统一 source→budget 顺序；关闭 B 的进程不冒称 B writer coverage。开启范围的完整覆盖须显式证明所有相关入口启用。source grants 不因 binary/schema 更新自动扩权 |
| 历史 v1/v2 financial outbox 交给新 worker | 原 decoder 和金额/事件语义继续；只对原 created 分支发布，不以升级为理由重建旧事件。A 接受候选及实际新候选双版本 fixtures 证明 compatible/busy/recovery/negative |

没有改变 Authorize RR、attempt-first、health fence/replay、retry、model qualification 或原 runtime terminal hooks。未受支持的在线 reconcile/health 重叠仍沿原裁定。

### 4.3 当前运行：复用活跃租约的按需阅读

增加 runtime profile 下的只读 `GET /xinsight/source/v2/runtime/current`，与增量历史 events 明确区别。按当前 credential 的项目范围在 RR 中读取原 lease 和结果，不依赖请求开始日窗口；最多 50 项 keyset 页面、原 read slot/期限/字节限制。指定 request 查询可返回该请求的最后持久状态与 freshness；列表只枚举有新鲜依据的当前存量，并明确未覆盖的同步请求类别。分页是各页 observed_at 的当前读，不冒充静止全集，不能相加成无条件准确的全企业并发数。

- stream：原 `stream_delivery_leases` 的 `state=active`、`lease_expires_at>本次DB读取时刻`、当前 owner/generation 与原 reservation/attempt binding 一致，且没有已经持久的 completed/interrupted/write_failed 交付或 terminal stream 状态；结合真实最近 checkpoint/lease 更新引用输出 `lease_valid_execution`、observed_at、valid_until。半开模型 Probe 租约不是 request 活跃租约，不能代用。
- media：原 job 的 queue/lease 均 leased、execution_state=running、未过期 lease/fence，且没有 terminal 状态；未开始的 leased/pending 单列 queued/leased_pending，不称 Provider 正在运行。
- 仅登记未见终态的普通 nonstream 没有相应持久活跃租约时，返回 last_known/unknown freshness，不制造新 heartbeat 或用 updated_at 设置任意“仍在运行”宽限。
- 租约有效仅证明在该截止下仍有执行租约，不能证明 CPU/网络正在前进。进程刚崩溃但 lease 未到期仍带这个有限含义；到期后即使旧行尚未被回收，也不能继续显示新鲜运行。原 media Claim 的未知终态/stream recovery 结果随后作为独立事实到达。
- XI 断源后必须停止“当前”宣称，保留 last confirmed/valid_until；不把 source 页面 ack 或存在租约历史当新鲜证据。正常 stream renew 不新增 source heartbeat 事件。

正向使用真实 stream lease/checkpoint 与媒体 MarkRunning，负向包含跨日仍活跃、completed 但 lease 行未清、generation 不符、只有 Probe lease、过期未回收、进程退出、断源及 nonstream 无租约。历史 events 和当前 snapshot 各自给出范围，不用一个替代另一个。

不写逐 token、逐 heartbeat 或逐 trace 事件。每 request 阶段和每 attempt observation kind 有限，策略按真实 policy/version/stage 汇总；原 retry/candidate/媒体边界保持。实施前列出有限种类及最坏字节/行数，64KiB 超限不能静默截掉必需事实或调低原业务能力；必要时按固定语义分条。

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

`GET /v2/budgets` 是当前授权项目的预算引用目录：原项目 gateway 预算固定一个引用，enterprise 只列显式绑定的 project scope。有限 keyset 页，最多 50 项，不读取整个 org 再内存过滤。目录 cursor 绑定独立 D 域的 instance/org/project/key ID、d_access_generation、目录修订及最后引用；预算定义或绑定增删会改变目录修订，旧 cursor 返回明确重启信号。日常交易支出变化不改变目录修订，避免活跃项目无法翻页。目录分页保证固定目录修订下引用集合；不声称其中每个预算金额来自同一个全局数据库快照。

`GET /v2/budgets/current?budget_ref=...` 每次只读取一个明确预算，在同一 RR 事务中完成当前权限、定义、期间、摘要和有界提醒读取，返回同一阅读快照的对象。提醒超出页面上限时返回独立的有界继续读取信息，不能宣称第一页完整；提醒读取 cursor 绑定独立 D 域、该预算/绑定修订及当前 d_access_generation，说明状态可能在后续读取变化。禁止客户端传 org/project 覆盖 credential 范围。返回 Cache-Control:no-store；暂停/撤销/字段 profile 缺权行为与 A 保持对应边界。

D 不是可回放的 append-only events feed。返回 `reading_id`、`observed_at`、原定义/期间/数据依据引用、canonical source hash；快照中的每个 typed document 绑定 reading_id 与各自 profile，预算摘要绝无 request_id。reading_id 是本次只读观察身份，不是预算版本或费用交易；同一响应重新处理必须幂等。XI 若需保持旧阅读，须保存原始响应及 hash；后续 GET 可产生新阅读，不能覆盖旧阅读，也不能把新 reading 计成新费用。断源时保留旧阅读截止和缺口，不用旧 reading 冒充当前。

不在 A source 表中伪造 D 请求/阶段，不给 A 开历史 bootstrap，不因为 D 消费者首次读取当前预算而回放历史模型经济事件。D 当前摘要可以使用原权威现存的周期事实，明确其覆盖；这是当前状态解释，不是重建旧 I01/source 历史。新资料包提供 D 独立 snapshot schema/manifest/向量及读取合同，不能给 XI 一个假的 event cursor。

## 7. D 定义、项目范围与支出覆盖

### 7.1 原项目预算

原 `budget_cents/budget_period` 不改义。新定义显式给出 setting_state：unset、inactive_zero、inactive_period_none、active、unsupported；读取错误返回 unavailable 错误，不生成“预算为零”。金额 cents 转 micros 乘 10000，checked integer；使用原 charged/project/UTC daily-or-monthly 口径。单独保留预算 revision、changed_at、变更权威引用。项目名/描述修改不增加预算修订，no-op 不增；所有原预算 writer 在原授权事务中原子维护此元数据/历史值。首次 D 迁移保存 `observed_baseline`，不把未知历史修改时间赋为 project.updated_at。

预算中途改额形成新定义，同一周期的支出仍从周期开始计算；旧阅读保留旧预算/差额。差额为预算减同口径已确认支出，不是钱包余额、可用额度或执行许可。reserved、calculated、charged 不混合。

摘要最低充分实现：直接从现有 durable financial journal 按项目/原发生期间计算同口径 settled charged subtotal，增加必要的窄索引，不建立第二个金额累计器。异步媒体金额若不是该旧 gateway 预算权威的一部分，明确 excluded，不偷偷加进旧口径。原条件式 project_budget_usage 可作为 `legacy_counter` 对照依据，不能与 journal 相加。M27 历史 call_logs 基线只有存在可证明互斥边界时才可单独引用；本轮不自动补做基线。

#### 周期、materialization 和 coverage 的具体有效性合同

金额聚合固定为 `usage_finalization_journal.event_type='settled'` 的 `charged_cost_micros`，org/project 精确匹配，`occurred_at >= period_start AND occurred_at < min(period_end, reading_cutoff)`。该 occurred_at 来自 `billing/outbox.go:65` 的原 usageJournalPayload.OccurredAt，经原财务 writer `time.Now().UTC()` 写入；它是原财务事件时刻，不是 request_start、journal.created_at、消费者接收时间或 worker 执行时间。沿现有时钟质量边界，不新增“绝对准确发生时刻”承诺。午夜边界及 equal-to-cutoff 均按半开区间测试。

必核对的 writer 是原 Settle/Cancel、delivery prepare/cache/stream-output/guardrail hold、provider reconciliation、settlement recovery；都通过原 `enqueueUsageJournalTx` 发布，正式类型为 `billing.usage.prepared.v2`、`billing.settled.v2`、`billing.cancelled.v2`，加三个原兼容 v1 类型。usage_prepared/cancelled 不加进已扣支出，但其未完成 materialization 和未决 reservation 不能被藏起来。`runtime.terminal.v1/v2` 是下游原 hook、budget-evaluate/notification/call_log/trace 是其他作用，不把它们的 pending 当作 journal 金额缺失，也不由其完成证明金额齐全。media 独立 charge fact 非原 gateway budget 的 writer，显式 excluded。

单预算读取的 `reading_cutoff` 固定取本次 RR 事务开始时的 `transaction_timestamp()`；所有定义、coverage epoch、journal subtotal、原 outbox 和未决 reservation 都读该事务的 MVCC 快照，响应另列实际 `observed_at`。等待锁不能把 cutoff 改成等锁后的时刻却仍使用旧 snapshot。它是带可见性边界的阅读截止，不声称所有发生在该墙上时刻之前的在途事务都已提交：

1. 对上述六类 outbox 以 org/project（payload.project_id 与原 reservation 可验证一致）和原 payload.OccurredAt 区间归属，按 event_key LEFT JOIN journal。缺 journal 的行，无论已 claim、available_at 在未来、正在 retry 或 dead_lettered_at 非空，都为 materialization pending/gap；不只看可领取队列。重复 v1/v2 同业务身份按原 event_key 防双算。payload 无法解码/归属/与 envelope 核对时，受影响 scope 为 unknown，不能跳过后报 complete。
2. journal 已存在但 worker 尚未删除 outbox 的行，是已 materialized 待确认，不再加金额也不误报金额缺口。`outbox.process` 的现行顺序是 handler commit 后按 owner DELETE；财务 PriorityBilling 正常不触发 bounded dead letter，仍覆盖人为/损坏前态的非空 dead_lettered_at 负例。
3. `usage_prepared`、financial hold/settlement_pending 及进行中 reservation 分别给 `unresolved_economics`，不把未知待定支出加到 settled subtotal。`settled_as_of` 只说明本次可见、已成立金融终态的 journal 已齐，不说明所有请求已经完成或周期永不变化。complete 金额覆盖和 unresolved economics 是独立维度，页面措辞不得把 settled subtotal 称最终账单。
4. 迟到 commit、后续恢复确认的 settlement/合法修订不在当前 RR 快照中，就进入后续 reading；旧 reading、预算定义和金额均不被回写。完整仅指已认证连续区间中、截至该次阅读可见的原已提交终态，不是关闭期承诺。

coverage 元数据不记金额，只记 `coverage_epoch`、`certified_from`、`writer_contract`、精确 schema/runtime 支持依据、state/reason 和最近受支持 restore boundary。生成/验证/失效限定在下列入口：

| 入口 | 具体职责 |
| --- | --- |
| D 首次迁移 + 原 schema/运行准入 | 在原允许的维护升级步骤中确认旧 writer/recovery 全部退出、精确新 binary/support manifest 与 journal outbox 启用合同成立；D 迁移在同一 tx 写 initial epoch 和 DB `clock_timestamp()` 的 certified_from，不追认任何旧期间。前置不成立则迁移/认证不通过。不是把 source profile 打开时间当财务起点 |
| 新 binary 的 `sourcefacts.CheckReady` 及每次 D current 读 | 只读验证精确 ledger、instance、writer contract/epoch、原 financial outbox 必需配置与状态。缺失/不匹配返回 uncertified/unknown，不能补写一个 true。普通重启同一受支持数据库不重置起点；不能容许旧 schema writer 绕过有限 runtime head 检查 |
| `scripts/prd2-backup-restore-rehearsal.sh` 的原 restore 之后、任何被恢复 backend/source reader 启动之前 | 在既有 `run_restore_psql` 步骤执行受版本管理的固定 coverage rebase SQL：校验存在的 D schema，留存旧 epoch 为历史，生成新 epoch，记录 restore boundary，certified_from 重置为本次恢复完成/重新认证的 DB 时刻；不能沿用 dump 中旧证书。该步骤失败则整个恢复验证失败，不报告 D 可启用 |
| 客户/运维 `docs/ops/deployment-runbook.md §4.2` 的实际 SQL restore | 同一辅助 SQL 由原恢复流程在维护窗口、所有读写进程仍停止时执行；配置/运行 preflight 验证新 epoch 后才允许 D 读取。将这一步及精确 helper 路径/hash 放入对应客户包/恢复说明。现有裸 psql 恢复段需要补这项实际合同；不是声称当前已实现 |
| 原 resetplan | 保留已存在的 source 保护/拒绝规则，D 元数据同列保护；不执行 reset，不扩大擦除范围。不能通过保留一个 coverage 行来允许丢弃其权威 journal |

选择保守的恢复合同：**即使是较新/看似完整的备份，只要走数据库 restore，就重新认证从恢复边界起的区间，不自动续接旧 certified_from。** 恢复日/月跨过该边界的预算只供 known_subtotal + restore_gap；从下一个完整 UTC 周期开始才可重新满足 complete。旧行保留为带截止的历史依据，不能推定恢复空档没有支出。无需在数据库外新建永续 witness、检测任意手工擦除或实现 WAL 补账；绕过正式恢复步骤的 raw restore 不在新增 D complete 支持承诺内，不能通过源码搜索证明它可被自动识别。

固定 SQL helper 只处理 coverage epoch/历史，不改账、凭据、source instance、A/B cursor 或业务配置；其执行属于既有获批恢复动作内的必要步骤，当前仅设计，无实际恢复/执行。已有无 D schema 的旧备份演练保持旧验证；当目标拟启用 D 时，必须先完成正常迁移与新认证，不能静默跳过。

`period_start >= certified_from`、epoch 合法且恢复连续性成立、原 writer 合同成立、适用 materialization 无缺口，才能标 `complete_settled_as_of`。complete 零还必须说明这是“截至阅读已确认扣费为零”；有未决经济事实时同时展示，不说最终支出零。中途开启/恢复、断源、旧元数据、不解码的 pending、损坏输入分别有反例。全周期跨边界不合格不能伪造 remaining；600/1000、月中已知200+缺口、UTC/上海08:00和999.999999精度案例沿原 B01–15。

### 7.2 enterprise 预算和项目 scope 绑定

BudgetPlan/版本/审批、FiscalPeriod、AllocationRun、分摊结果仍由 `enterprisefinance` 原 writer 拥有。源投影继续 report_only，引用原 run/source revision，保留 exact/partial/no_allocation_run/unit_mismatch/not_modeled。不从未建模的 committed/forecast 产生零，不把 allocation actual 重命名为计算费用或新增扣费。

本稿建议新增窄的显式绑定：`(org_id, enterprise_project_scope_ref) -> UR project_id`，带 revision、当前有效状态、批准 actor/audit 引用和双向组织一致性约束。仅允许 ScopeProject；部门/成本中心/org 预算不能借此映射后暴露给单项目。绑定写入明确沿 `main.go:845/1217` 的 enterprise `billingRoute`：仅当前 `platform_admin`、`billing_admin`；`org_admin` 继续只在原 pricingReadRoute 读，project_admin/org_viewer/source key 均无绑定写权。handler 之外还在实际写入 tx 通过 `reauthorizeCommittingActorTx` 核对当前用户、session、auth_epoch、组织角色、精确目标 org/project 活跃与同 org，角色撤销和绑定 commit 按原 principal 锁序裁决。平台管理员跨 org 必须明确目标且通过原 customer context 约束；billing_admin 不能跨所属 org。绑定新增/修订/撤销与 audit outbox 及 D 目录版本在同一 tx，expected_revision 防覆盖；不顺便修改既有 enterprise 审批角色或 maker/checker。source credential 只读，不能建立/修改绑定。旧任意 scope_ref 不自动迁移、不按显示名或数字猜测。绑定是当前可见性依据，需独立权限审查；其实现不由本稿自动获得超出已有范围的批准。

读取 D 必须先 SQL 限定当前项目绑定，再取对应 project budget 及 target_kind/ref 匹配的分摊切片；不输出 org 总金额、其他 target、全 org source/result 数量、人员目录或原始行。可返回经 scope 安全审查的原覆盖结论及 run 引用，不能透传整个 Position 结构后再删字段。绑定撤销/变更使对应目录/current/提醒读取失效，**仅更新 D 自有版本，不更新 A/B 共用 connection.scope_generation**；旧缓存/链接不能借旧绑定访问当前接口。已有 XI 副本的撤权仍由 XI 当前阅读权限负责，本地源端拒绝不证明远端已删除。

D 自有授权状态落在窄的 `(source_instance, org_id, project_id)` scope 行：`d_access_generation`（正整数）、`catalog_revision`；binding 自带 revision/status。绑定新增/撤销/重指向时，在原 tx 中按稳定 project ID 顺序锁定受影响旧/新 D scope 行，增加各自 D 版本并写 audit。目录 cursor 使用 D 域并绑定 key ID/D generation/catalog revision；单预算 current 每次核对当前有效 binding；提醒 continuation 再绑定预算与 binding revision。D-only profile grant 调整也增加 D generation，不能通过复用 A `update connection` 的总 generation 路径实现。

D 读取与撤权的顺序：D current/目录/提醒先在 bounded RR 事务用原 authenticateTx 解析合法凭据的 org/project（此时不返回数据），再对对应 D scope 行 `FOR SHARE`，核对 D profile 授权与绑定；绑定撤销取得同一行排他锁。撤权已提交后开始的读取必拒绝旧 D cursor/binding；先取得读锁的已授权请求可在撤权 commit 前完成这一阅读，不能追回已传输字节。RR 遇并发更新的 serialization failure 作为 source read unavailable 返回，不放行旧权限。没有 D 权限的旧 cursor 先拒权再核对内容，避免对象枚举。

原连接全局暂停/撤销、credential rotation、org/project 停用依然按原 A 控制链作用于全部 profiles；这里不更改它们的既有语义。只变更 D scope/binding/profile 时，A/B grants、epoch/high/floor、HMAC 的原 connection generation 都不变。验证必须先保存未消费完 A/B 页的旧 cursor，再撤销/重绑定 D：旧 D 目录/current/提醒被拒，原 A/B 从原 cursor 继续读到未消费的事实且 hash/顺序不变；不允许以重新 bootstrap 跳过积压来“修复”。测试还包括旧项目撤权、新项目明确授权、跨 org 拒绝、并发 reader/撤权、org_admin 降权与旧 session 失效。

无绑定的 enterprise 预算为“该来源范围未配置”，不能作为有零个 enterprise 预算的组织级断言。完整 D Local 正向测试必须通过原管理权限建立合成明确绑定、创建/批准原 BudgetPlan、应用原 AllocationRun，再用受限 source credential 实读；不能全用 unknown 通过。绑定如果审查发现不应纳入，必须重新确定安全的正式 scope 方案；不能跳过该既有 enterprise 目标称 D 完成。

### 7.3 提醒

读取原 notification event 的触发时金额/周期/阈值、事件引用、当前 ack/resolve、投递状态及各自截止。原事件没有预算版本时返回 version_unknown，不事后按当前 budget 补历史值；新 writer 可在原事件创建时附带当时定义引用，旧 dedupe 与投递仍原样。ack/resolve/投递成功不说明人已读，也不说明现在仍超额；当前 comparison 在 summary 中独立计算。不新增收件人、webhook、提醒频率、重发或 hard block。

## 8. X 配套、迁移与依赖顺序

| 可先确定、不依赖 A 待批控制链的设计 | 必须等待 A 验收/后续批准与证据 |
| --- | --- |
| B 的原 writer、状态/计时定义、最小披露字段、原 R/T/CT2 用例；D 的金额口径、coverage、snapshot 及绑定设计 | B/D 产品编码、最终迁移前驱和 digest、接入权限及新 profile 生效 |
| X 的新增 profile/transport 支持矩阵、默认关闭、候选包结构及恢复场景 | source credential 正向 HTTP/cursor/消费者、管理 UI、绑定的权限实施及浏览器实测 |
| 原合同与新合同分别列字段/未知/单位/截止、独立消费者测试计划 | 用真实已验收 A 对齐新 codec/Envelope/profile grants；不能用当前 A 候选当正式 pin |

B 增量迁移扩展 profile allowlist、连接授权上限及新 source schema 有限集合；D 迁移只增加定义/绑定/D scope version/coverage 的必要元数据、历史及查询索引，不重建原财务账本。迁移编号在 A 真正交付后按当前 ledger 冻结，当前不创建不可变编号或签名候选。新增 profiles 默认不授予已有 source key；新字段权限由明确 profile grant 决定，不能因升级自动扩大。

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

验证必需派发前 source 失败不调用 Provider；原业务记录失败按原 hold/recovery 处理，而派发后的附加 source/measurement 失败不新造 hold 或改变已成立交付/金融。须分别注入原 receipt 失败、附加计时失败、policy 汇总退出与 financial outbox materialization 失败，不能混成一类错误。并发精确 replay、锁等待/取消、反序 profile、预算变更与 settlement、binding 撤权与读取同时发生均实测。预算 journal 查询需 EXPLAIN 与实际规模；不将缺少索引导致超时误判为 unknown。

性能必须在 A 接受候选上独立测 B/D 默认关闭/启用及组合，保留相同请求/负载/原结果、真实原 outbox 恢复、source pull→独立消费者数据可用时间、读写争用、行数/字节/WAL。A 的约 1ms 结论不能继承为 B/D 通过；不加入 fake Provider sleep 稀释比例，不用混合失败/409 的 p95 冒充全部成功。

候选稳定后完成适用全 Go race/vet、原 CI 选择的本地集成、前端 type/tests/build、API/route/hash/schema/版本/安装及恢复检查；Docker/精确 N1 等缺失仍标待环境，远端 Change Routing/required checks/exact-main 没运行则不报通过。源合同包包含有限 schema、合法/非法页面/快照、身份/hash 向量、真实接口样本和 coverage/迟到/恢复/权限说明。只交协调任务，不派 XI。

本次只验证文档与源码引用，没有 B/D 运行结果。下一步是协调者对本稿精确摘要的独立审阅与差异讨论；共同确认后仍等 A 完整验收才编码。实施完整交付需绑定新的 commit/tree/diff、测试与失败证据，协调者整体独立验收。原 US/FA/GX 及 A—E 状态逐项回填，绝不以文档完成替代产品完成。

## 10. 需要本次独立审阅明确裁定的内容

1. B 必需派发前记录/原 durable 结果与可失败附加观察的具体责任，以及原 outbox I01→统一A/B→budget尾部的顺序与共存证明。
2. D 独立 current snapshot/目录合同及其阅读截止、历史保存责任；不提供虚假可回放事件历史。
3. 项目预算原 occurred_at/subtotal、六类 outbox/已物化待删除/未决经济事实，以及正式 restore 一律 rebase coverage 的具体正反向证明。
4. enterprise 绑定仅 platform_admin/billing_admin、tx reauthorization/audit，同 org 切片和独立 D generation/读撤权顺序，不破坏未变 A/B 追赶。

以上属于具体方案审查，不请求泛化产品改向。发现必须新增产品权限或改变已接受范围时，单独列出精确差异；一般技术修订由 UR 与协调者讨论裁定。A 的 source credential 自动审批阻塞继续单列，设计讨论不能视为其授权。

经验文件无需变化：当前是只读设计，没有新增运行证据达到经验准入门槛。


## 11. 本次四项裁定回填及新增验证

| 规划层裁定 | 本稿采取的具体修订 | 必须提供的实施证据 |
| --- | --- | --- |
| VR-1：必需/附加持久责任、当前运行 | §4.1 分离原业务 commit/派发前必需 source 与 commit 后附加写；仅原金融 outbox 支持其可核对终态的可靠 materialization；丢失单调值/策略不复原。§4.3 独立按需 lease snapshot | 真实完整交付/计费后让附加写失败，业务结果/钱/hold不变、样本缺失可见；原 receipt 失败走旧恢复；lease正向与崩溃/过期/错generation/跨日/无lease负向 |
| VR-2：原 outbox 尾部及共存 | §4.2 保留原 I01→A/B→budget，撤回先搬预算尾部的意图；列明 M116 bridge、扩展后及历史outbox交接 | 新旧writer并发锁等待、相同项目budget更新、source反序、预算tail失败回滚、cancel/no-budget分支、A exact bytes/time、created=false不重建；旧不兼容recovery拒绝 |
| VR-3：coverage有效性 | §7.1 精确 journal period、六种outbox、已存在journal的待ack、pending/unknown/未决经济，迁移认证与正式restore保守rebase | 备份前有coverage→备份后原库有已成立费用→恢复旧备份且pending为空：不能完整；rebase后当周期gap、下一UTC完整周期正向零/有支出；helper失败不宣告恢复可用、旧schema备份路径；晚到新reading不改旧reading |
| VR-4：授权和独立D失效 | §7.2 冻结原billingRoute两角色及事务重新授权，D-only generation与目录/binding版本，读写同scope锁序 | org_admin/project角色/source key写拒绝，actor撤权/session注销竞态，跨org拒绝；旧D目录/current/提醒拒绝，同时旧A/B cursor完整追赶、零跳页/零hash漂移 |

原 R/T/CT2/DATA/B/FA3、规模/性能、真实 handler→受限独立消费者、X/原 required checks 不删减。新增 snapshot 及失效/恢复合同必须进入正式 schema/OpenAPI/包和实际验证，不能用此规划文档代替。

本次仅修改设计及执行记录；A 产品代码、原设计/审查/health 计划和 source-control 待批补丁均未应用新变更。共同确认和 A 完整验收门槛保持。
