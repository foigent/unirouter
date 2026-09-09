# UR 工作事实本地迭代：独立实现计划

日期：2026-09-09。状态：Proposed（独立审查后修订）；仅完成源码核对与设计，尚未编码或执行产品测试。
执行 owner：01a08339-f189-7703-acd9-5d0a63a2873b。
监督／共同方案审阅：About Me 01a08371-5a0f-7d32-856e-340ccc778825。
范围依据：第42份授权、第41份UR-FIT-01—10及UR-A/B/D/X；第17份原13项、第23份A—E、第24份A-W01/I-01/I-02和第25份I2场景。第36/37份为工程输入；64类型/25接口不原样照搬。

## 1. 精确基线、保护与本轮界限

| 对象 | 当次核对 |
| --- | --- |
| 保存仓库 | `/Users/m4/Desktop/projects/UniRouter`（物理大小写归一为`unirouter`）；`codex/minimax-payg-glm-onboarding`；HEAD `ac53699d29c3791a514a045fe064ab63ff9f99c0`；tree `71d0fcf5a86d609a0f66e90a01def7fd50ea9dca` |
| 已有改动 | 16个tracked修改及4个untracked条目；规则、CI、文档、release checker及研究资料。未重置、移动、清理或吸收 |
| 提供的干净基线 | `/private/tmp/unirouter-main-1.2.77-cd9f354b-20260909`，HEAD `cd9f354b8a104a6f0677345a5694b19c000ab015`；tree `7725ffa81b6f6897f5ef6ff843f308c05d44fd63`；VERSION `1.2.77`；status为空 |
| refs辨别 | 缓存`origin/main`同为`cd9f354b`；本地`main=9e97f536d35fe61ed1d1246d86ab747a429023ec`，它是`cd9f354b`祖先，不是更先进的基线。未fetch；不声明远端或Target当前状态 |
| 本轮隔离工作树 | `/private/tmp/unirouter-workfacts-20260909`；branch `codex/ur-workfacts-20260909`；由上述完整SHA创建，base/tree相同；本轮只新增本文及其独立审查记录 |
| 既有UR执行任务 | “重构Unirouter20260905” `01a06e2e-df62-7892-8fc2-f74121994942`，平台当次状态idle。基线EXECUTION-RECORD首项为compatibility/onboarding closeout；模型数据化、Probe/Approval、Release/Slot、stream counter及交付属于原任务。不以idle证明所有交付已结束 |
| XI协作 | 第40份当次已更新为专用owner `01a08361-94fa-73a3-9a71-f2e2507f08c4`，负责人仍为`01a0822d-9593-7260-87a4-0d3001400de9`；旧XI任务保留。只读识别，不向任一XI任务派发或接管 |

隔离不等于未来无合并冲突。预计共享接缝是`cmd/uniroute/main.go`、gateway、billing、schemaauthority、OpenAPI、版本元数据及接入管理；模型声明/Bundle/Probe/Release执行合同本轮只读取，不重写。共同方案通过后只在本轮工作树实现；整合若基线变化，增量审查实际冲突，不擅自cherry-pick另一任务半成品。

本次授权只到完整独立计划。后续获得负责人共同方案确认后才进行范围内本地实现、重构、迁移、文档、测试及记录成果的本地提交。禁止push/PR/merge、正式签名发布、部署、真实Provider/Feed/客户API、真实凭据/权限、已有服务、云资源、用户数据删除、系统工具安装、付费路径。数据库和密钥均为本轮合成测试专用。第42份不继承旧任务的真实调用、GUI、部署和预算授权。

## 2. 读到的关键源码事实与设计裁定

以下路径均相对本轮精确base；源码事实不是运行验收。

| 当前代码 | 独立核对后的裁定 |
| --- | --- |
| `middleware/auth.go:270,377` legacy SELECT校验app却未返回app ID；grant resolver快照被压成APIKey | 补齐同一次认证返回值并传递非秘密证据；不在终态JOIN今天的owner/app/grant |
| `GatewayAuthAcrossBufferedBody`执行两次认证；第二次取代第一次权威 | 持久化接缝放在第二次认证后的独立wrapper，不把两次认证计成两次执行；buffer前认证不升级为已准入 |
| `agentcap/authority.go:131`已有完整Boundary；`service.go:315`返回APIKey/Claims，丢弃其余Boundary | 在同次Authenticate结果中返回受限证据，不再额外Resolve；说明cached authority/authority version与校验时刻，不能称重新实时查库 |
| `billing/billing.go:579,667,758`两种收费模式共用insertFreezeReservationTx | 增补一次共享事务接缝，冻结费用解释种子；不重复写两份业务账本，也不改原冻结/扣费公式 |
| `billing/outbox.go:19,117`原payload含billing_mode，旧缺值会默认prepaid；journal与TerminalMaterializer同事务 | 在默认之前记录mode来源；CostBasis沿原终态transaction获取金额引用，保留legacy interpretation状态 |
| `completedfeed/service.go:81`原TerminalRecord→BuildSnapshot→项目state锁→序列插入；read为repeatable-read | 复用纯V1构造器与顺序约束，独立新通道；不能直接复用V1 cursor或给V1增加字段 |
| `eventconnector/kernel.go:411`共享幂等身份没有source_revision | 新源每一版的event_id必须包含稳定修订身份；revision只放payload将被旧去重折叠，是明确要避免的错误 |
| `sourceevidence/contract.go`和compatibility-pin只支持冻结活动选集、partial及缺close marker | 保持原包及资格；新在线profile另有UR owned descriptor，不把该包改成完整在线Feed |
| `billing/provider_attempt.go:424`已有reservation锁、owner fence、持久attempt与精确route evidence | B沿writer追加有界观察，不能轮询updated_at冒充事件史；三个候选不等于三个attempt |
| `gateway/public_nonstream_runtime.go:861`是Responses/Messages共有Freeze；`public_nonstream_handler.go:152`绑定Messages流执行器 | 不能只改relay的chat Freeze；覆盖Responses与Messages实际支持的V2 stream执行路径，即便旧构造器名仍叫NonStream |
| `mediajob/postgres_store.go`CreateOrGet/AttachExecutionReservation；`postgres_transitions.go`有独立charge fact | legacy async是另一条持久路径，不经过text billing Freeze/I-01；记录其真实上下文与经济来源，禁止制造I-01金额。canonical preflight明确禁async |
| `enterprisefinance/reporting_store.go`有BudgetPosition、allocation run、exact/partial/not_modeled | D投影原权威；项目预算NULL不可由旧getter的COALESCE(0)读取；不新增预算/分摊引擎 |
| `schemaauthority/compatibility.go`只识别M113/M114/M115完整ledger | M116须显式扩展桥接、fleet/迁移与恢复测试；新表可空并不能让1.2.77接受未来schema |

依赖方向：新增`internal/executioncontext`（纯类型）与`internal/sourcefacts`（UR领域profile、同库捕获/发布/读取）。`middleware/agentcap/accessgrantboundary`传纯快照；billing/媒体通过窄Tx接口提供事实；sourcefacts实现接口并在main组合。sourcefacts可使用billing纯TerminalRecord和completedfeed纯BuildSnapshot，billing不反向import sourcefacts，避免循环依赖。既有sourceevidence只复用可证明合适的纯投影/校验，不把其campaign selection或XI兼容类型变成在线权威。

## 3. UR-A正式候选合同

### 3.1 API和权限：固定字段profile，拒绝按读者重写同一hash

选择现有受限集成面中的一个有版本pull入口：
`GET /api/v1/integrations/xinsight/source-facts?profile=<exact>&cursor=<opaque>&limit=<bounded>`。
同时提供`GET .../source-facts/capabilities`，只披露当前凭据获准的profile/版本/字段和恢复界限。控制面在现有X-Insight接入管理下新增明确的source-profile连接操作，不改变旧连接的默认行为。

A有三个固定字段profile（均为候选1.0.0，未发布）：

| profile | 字段与权限组 |
| --- | --- |
| `unirouter.execution-context.v1` | 来源scope、server request、发生/记录阶段、auth kind、非秘密credential reference、app、operation/protocol与执行引用；`execution.context.read` |
| `unirouter.execution-identity.v1` | 单独的owner账号、grant subject/Sponsor、Agent lineage及验证依据片段；`execution.identity.read`，额外明确授予；仍属ExecutionContext逻辑需求 |
| `unirouter.cost-basis.v1` | 原经济来源/精确引用、freeze模式/价率/版本及缺失状态；`execution.cost_basis.read`；不是新增可求和费用 |

固定profile避免同一event_id/source_hash在人员权限不足时被删字段重算。core不放owner/Sponsor/Agent UUID、邮箱/显示名或身份片段hash。identity片段通过同一request事实关联；查询者能关联不代表能读取。声明`available/unavailable/not_applicable/not_authorized`在能力/响应层有不同含义，不把未授权字段包装成源端未捕获。

使用新的精确purpose `xinsight.source.read`，网络/org/project/允许profile集合由服务端绑定。新purpose只接受source专用认证；旧`xinsight.feed.read`永远不能读新profile，新凭据不能读旧Feed或模型/管理面。复用现有receiver注册、receiver SHA-256 verifier→服务器HMAC及staged/active/revoked两阶段生命周期的机制，不使用admin token采集，不自动迁移/升级旧key。

具体落点为APIKey purpose约束及独立source连接/credential-profile授权行；同一旧token不能仅加scope变成新服务。可共用现有`feed_staged/feed_active/feed_revoked`状态词，但所有unique index、resolver、generic-key排除和控制查询必须精确purpose过滤。M43/M44/M45既有约束需通过新migration增量扩展，不编辑历史migration。新注册schema候选为`unirouter.xinsight.source-credential-registration.v1`，绑定profile集合及source连接；token沿用已实现的`ur-xf1-hex256.v1`格式和`receiver_sha256_v1`机制，但必须独立生成随机bearer，禁止跨purpose复用同一token/verifier。旧control严格拒绝新schema，新resolver同时校验purpose、active状态、连接状态及profile授权。仅本地生成合成receiver bearer；真实生成/注册以后另需对应授权。

控制面仅本地产品实现：明确prepare/register、pause、rotate/cancel、resume、revoke；复用现有写入时重授权与审计。旧连接与新连接独立启用、暂停、游标；新profile失效不暂停I-01。

### 3.2 一版事实的身份与hash

逻辑源键：`producer_instance + org + project + fact_kind + server_request_id (+ stage/object discriminator)`；稳定`source_fact_id`不含adapter、发布序号、采集epoch或当前显示名。
同一UR安装在新source与I-01中使用相同的稳定source instance；现有`COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE`是核对接缝，即便I-01关闭也须有显式source身份。启动/注册校验两通道配置一致；不能以新connection ID或epoch替换producer身份。更换真实来源身份须新建来源并报告连续性断口。
一版主键：`logical key + revision`。初版revision为十进制字符串`1`；同键同版同内容为noop，同键同版异内容拒绝并保留受限冲突码。新增观察或更正才增加revision，带前驱revision/hash及reason；不会修改旧页。

event_id选用`uf1_`加SHA256（输入为长度前缀的逻辑源键及revision）；最大长度、编码和域分离固定为UR profile合同并提供向量。event_type稳定区分core/identity/cost，schema或adapter升级不另造经济身份。共享BuildIdentityInput/FinalizeEvent仍按现有2.0.2执行；不改共享协议或pin。

源document先经typed allowlist、严格重复键/未知键校验，再用现有`eventconnector.CanonicalJSON`形成一次不可变bytes，`HashSourceBytes`绑定该bytes；金额/ID采用十进制字符串以规避safe integer差异，时间固定UTC微秒。新profile此约定不追溯改变旧文档字节。
保留三种摘要：旧I-01 snapshot hash、既有execution-route typed Go hash、共享Envelope canonical hash。source hash另绑定源bytes；交付cursor/generated_at不进源事实或Envelope语义hash。adapter版本变化可改变投影hash，但逻辑事实/经济引用不变，消费者以稳定源主键拒绝异内容，不能以adapter UUID重复收费。

### 3.3 费用与I-01关联

CostBasis的经济引用为`source_instance/org/project/request_id/revision=1/snapshot_sha256`以及显式`calculated_cost_micros`与`charged_cost_micros`字段路径。不输出第二份可求和的金额。源处通过原BuildSnapshot从同一TerminalRecord取得完全相同bytes/hash；不以当前model/price构造。

I-01启用：确认原materializer和CostBasis使用同一canonical terminal time/字段。I-01未启用：可以保存`expected_snapshot_ref`，必须标`not_published_by_i01`，不能声称已存在的Feed记录。后启用V1不会主动回填此条；后续只有真实发布的匹配记录才关联。I-02先到时消费者pending，I-01先到时金额仍可读；CostBasis到达只补解释。

persisted basis包含当次billing_mode与来源、pricing authority、pricing version、四类effective token rate、unit rates/meter的有界有效快照、倍率/单位/currency/scale；优先使用freeze既有值，避免每行重复全RateCard。需要保留的rate card经济层/ref/version在quote→freeze时保存，不能事后查current rate card。

`uo1_`仅在UR受绑定的legacy observation生成路径解释为unpriced；合法priced-zero另报priced。旧outbox缺mode保留`legacy_interpretation`和解释版本，绝不把默认值描述成当次已记录。priced-zero、missing、not_applicable与消费者未授权分别处理；计算金额、扣款、供应商出账、现金不互推。

async media明确`economic_source=async_media_charge_fact`、`i01_ref=not_applicable`，费用依据来自其原charge fact；未形成价格/金额就保持unknown，不伪造text billing或旧I-01。A验证此路径身份和“不捏造I-01”边界；B补其过程。原I2-09的R1—R4金额样本属于可关联I-01的text路径。

## 4. writer、提交点与失败语义

| 边界/文件落点 | 持久化与含义 | 失败/重放 |
| --- | --- | --- |
| `auth.go` legacy SELECT、`accessgrantboundary.Snapshot`、`agentcap.Authentication` | 传递同次已验证的不可变小快照；owner/app/grant与Agent分层，不加客户端必填标签 | 不信自报actor/trace；不保存bearer/JWT/hash secret；保留现有授权检查，不能额外JOIN变化后的权威 |
| main中第二次auth之后、license/rate/idempotency之前的capture wrapper | 对已可信org/project的model操作在同库事务发布`authenticated` context；因此后续license/rate/准入失败有上下文，但不称执行成功 | source采集启用时这是必需本地记录；写失败返回该模型协议既有503错误形态，零Provider dispatch。首次buffer前auth和未认证拒绝不进入项目源；其覆盖明确未承诺 |
| `insertFreezeReservationTx` | 在原reservation同一事务保存当次core/identity小快照及费用种子，发布admission观察；同时覆盖prepaid与observation | 任一必需写失败整个freeze回滚，原资金/fence保护不降级；重放必须读取已存当时快照，不用重认证值覆盖旧快照 |
| chat/embeddings `relay.go`、public prepare/Responses与Messages stream、`capbilling` | pure evidence传递到统一Freeze；缓存命中只有已认证上下文和明确no-new-attempt观察，不造freeze/I-01 | 原响应/流/取消/调用和计费不变量保留；idempotency冲突是一个新的HTTP观察，不创建原业务费用/attempt |
| `billing/outbox.go`原settlement→journal | 原settlement事务已有durable outbox；原journal/TerminalRecord事务通过组合materializer直接发布自足终态context/identity/CostBasis片段，V1构造不变 | financial terminal成立与源页可读分开；source materialization失败沿原outbox恢复，不能重扣或重派发。`created && terminalMaterializer`守卫保留；已存在journal的旧outbox重放不重建已清理源事实 |
| `mediajob.CreateOrGet/AttachExecutionReservation` | admission snapshot与job创建同tx；dispatch snapshot与既有重新授权/lease commit同tx；区分首次key版本与dispatch逻辑credential版本 | 返回既有job不改历史；revoked grant禁止dispatch；不增加text Freeze。charge fact与source materializer同tx |

新增字段和发布数据只在明确启用capture后产生。默认升级不启用新采集；connection bootstrap也不是把历史行JOIN成当时事实。capture允许的固定字段组由UR配置决定，独立于某消费者的启用时间和读权限；授权只控制读取，不能改变已存源bytes。可先显式启用capture，再启用各消费者；capture之前的请求若较晚终态，明确context当时未捕获，仅使用确实持久化的freeze依据。

建议同库两类数据结构：
1. `execution_source_facts`：immutable key/revision/predecessor、typed bytes/hash、occurred/recorded、request/scope/profile、capture generation；publication位置/时间在同一次插入时确定，作为独立交付元数据不进入源hash。无未发布中间状态，SQL禁止修改事实内容。该行同时是可重放来源，不另复制一套ledger。
2. `execution_source_streams`：按source/org/project/profile固定排序锁、published high/floor、source epoch、capture coverage边界；连接授权/消费ack使用source专用关联行，不能写旧Feed状态。
直接在原writer事务内锁project/profile state、分配位置并插入完整源行，复用V1排序锁的提交次序原则。每个事务先取得原业务锁，涉及V1时先执行既有V1 materializer，再按profile ID字节升序取得全部新source state锁；不能持新source锁后再取得reservation/job/V1锁。retention只取得对应source state锁，不反向读取并锁业务行。禁止网络调用和不受控扫描占用这些事务；测试并发和锁等待。保留原financial outbox，不新增source publisher、队列、死信状态或积压准入系统。

A text每请求至多authenticated两片段、admission两片段、terminal三片段，共7片段；字段均typed allowlist，单片段≤64KiB，测试最坏编码上界。媒体submit/dispatch/terminal采用相同的有界阶段数；幂等重放不追加阶段。无payload、prompt、response或逐token事件。consumer离线不影响本库发布；已发布数据按7天窗口维护。新增写入有存储和事务延迟成本，capture失败可令请求503；终态事务失败依赖已存在的financial outbox恢复并报告源不可用，不承诺数据库耗尽时仍能无条件完成。使用原DB/outbox健康与资源告警，A测新增字节、WAL、事务延迟及资源故障后的可恢复性，不虚构未实现的容量预留保障。B追加过程阶段须分别声明其现有最大attempt/阶段上界，不继承A的7片段数字。

跨保留窗终态的依据由reservation/job上的当次小快照和费用种子负责，不依赖早期source页。未决/hold期间不得被source retention清理；终态writer在同一事务重新形成terminal阶段的core、identity、CostBasis自足片段，并统一取publication时刻。core/identity保留原captured_at/authority与auth/admission值，明确这是终态所携当时快照，不是重新认证或新执行。terminal片段的stage进入源键；按同一server request聚合，不能因早期和终态两个观察重复计执行/费用。必要小快照至少保留至相应终态成功发布，其后遵循原业务保留合同；页面只依赖自身终态片段，7天内不再要求业务行或早期页存在。未发生freeze/job的认证拒绝或cache观察按自身7天窗口处理。

额外的网络/可选传输观测允许失败，但不能让其失败抹去必需账务或context。B只承诺实际提交的观察；持久化失败通过原可靠终态/恢复路径附gap；无法保存gap时stream coverage为unknown，禁止complete claim。XI离线不改变模型执行；UR本库故障仍服从原财务fail-closed合同。

## 5. 分页、启用、保留、迟到与恢复

- 复用`eventconnector/pullv1.EventBatch`（50条）；新source document≤64KiB；JSON整页<8MiB；limit最多50。保持shared pin 2.0.2。不复制draft500条页。
- 一次GET绑定一个精确profile及权限，不跨授权集合混页。repeatable-read取得授权状态、retained floor、high watermark及行；页裁剪按最后实际发出的序号推进，不按最高读到/最高授权外序号推进。共位置多片段各有不同位置，不能截断后跳过。
- cursor独立HMAC域，绑定source instance/epoch/org/project/profile/connection scope generation/position；不复用V1 codec字节。轮换凭据且scope不变可延续连接cursor；scope缩减或身份变化拒绝旧cursor，必须显式新epoch/前向握手并记coverage断口。
- 空cursor为只返回空页和当时high的bootstrap；首次向前启动，不自动回填。读未装/不支持profile、未授权、暂停、过期、同键异内容分别有typed错误；沿UR现有source API错误与shared ConnectorError映射，不改模型协议或旧Feed错误。
- pause停止pull而不停止capture/publication；resume可在retention内追赶。staged key只允许证明paused，不得读active源。revoke不删除原事实。profile独立启用、与I-01顺序无关。
- late arrival按published位置递增，即便occurred_at早于启用仍可被后续页收到。high watermark只说明该发布序列的一次读界限，不证明任何业务时间段完整。closed request、source collection close和read truncation分开。
- 7天从published_at算；retention锁同一state且只删除连续已发布前缀并原子推进floor。终态core/identity/basis同事务发布、同一publication时刻，分别在固定权限profile可重放；CostBasis不夹带未授权人员字段，身份读取被拒不改源内容。早期context已过期也不影响获准读取终态自足片段；不通过延长早期前缀让一个hold阻塞整条流清理。引用前驱已过期时明确history gap。
- profile不同启用/授权时间仍遵守向前bootstrap：终态发生在该profile的bootstrap之后则可拉取自足片段；若之前已经发布且被bootstrap跳过，不承诺追溯补发，明确late-enable coverage gap。消费者刚获identity权限也不能用旧scope cursor越过新授权边界；其可用窗口不等于UR未捕获。连接应在开始联合消费前完成所需profile握手，不能以CostBasis可读推定身份一定已落地。
- cursor低于floor永久410（携受权gap范围）；cursor高于当前high返回409 regressed，恢复数据库后也不能自动reset到最新。source重建需新epoch明确记录；不重新发布旧事实创造经济身份。
- 每次GET携带前次耐久cursor仅作为消费者已确认位置；不等于XI查询或UI就绪。独立合成consumer使用`事件耐久落地→整页收敛→checkpoint CAS`，在页中断、checkpoint失败和乱序到达后可恢复。

## 6. 协议 × 认证 × 执行矩阵

每格的fixture绑定实际handler/typed executor，PG用真实本次隔离实例；本表是计划，不是通过记录。legacy/grant指凭据；route authority另有legacy/canonical，不混为一维。

| 操作/协议 | 认证覆盖 | 执行路径及关键反例 |
| --- | --- | --- |
| Chat非流/流 | legacy personal/shared/app、grant personal/workload、合法Agent capability | legacy/canonical实际支持路由；成功、freeze失败、取消、fallback、provider模糊终态、流中断；功能工具流用现成typed假provider |
| Embeddings | legacy、grant、endpoint-scoped Agent | 正常及准入拒绝、cache配置实际支持性、单次freeze与I-01；不能漏掉relay第二处Freeze |
| Responses | Bearer legacy/grant/允许该endpoint的Agent | 实际非流子集，以及exact Responses v2 stream executor；不根据旧docs/函数名标stream N/A；cache bypass；hold/终态/取消 |
| Messages | x-api-key legacy/grant/允许该endpoint的Agent + exact anthropic-version | 非流typed子集及exact Messages V2 stream executor正向；freeze→provider→checkpoint→persist_usage→settle，协议特有usage/terminal reason、取消/中断与费用关联。保留历史V1流拒绝、Authorization冲突/版本缺失反例及Anthropic错误形状 |
| Async media | 项目root legacy/grant；Agent按mediaAuth实际拒绝 | legacy route受支持：job submit/replay、dispatch时重授权、取消、charge fact。canonical=true时preflight强拒绝async，验证拒绝而非开启不支持路径 |
| Metadata/preflight与未认证 | 根据原公开合同 | /models和chat preflight不得变成收费或真实执行；无可信project拒绝不泄入项目源。新service purpose在所有模型/媒体/管理面拒绝 |

所有允许组合先验证现有supported schema/profile，未支持的组合提供配置、validator与call path证据并测试拒绝。无需按模型数重复运行；不能用普通API无业务标签当采集失败。cache命中、billing admission失败、请求HTTP重放和真正的新server request分别验收。

## 7. UR-A01—12 / I2验收绑定

| 验收 | 新测试落点及预期 |
| --- | --- |
| A01 / I2-01 | `cmd/uniroute/source_facts_http_integration_test.go`：同client correlation两次调用→两个server request，正式源页重放不增事实/费用 |
| A02 / I2-02/03/04 | 同文件+middleware/agentcap：调用后变更owner/app/grant、Redis过期；回读原owner/app及Agent，actual human unknown不撤销已知账号归属；Sponsor不进入个人使用排名 |
| A03 / I2-05/06 | billing integration：freeze后改mode/price、旧缺mode、uo1 unpriced、合法priced zero；cost basis引用原金额不变，不能以默认为现场事实 |
| A04 / I2-09 | 独立consumer：I-01/I-02两个启用顺序、不同到达顺序/adapter版本；R1—R4计算100CNY扣0，R5迟到105；不再加CostBasis一次金额 |
| A05 / I2-08 | sourcefacts PG：同版冲突、相同内容重复、两tx反向commit、页条数/字节边界、同项目并发publication；无跳记录或越权 |
| A06 / I2-08 | Tx故障注入于context/freeze/settlement/journal含source publication的commit前后与commit ACK丢失；重启恢复、不双扣/派发；source写入失败沿原financial outbox可见并恢复，已有journal重放不重建源。XI离线仍正常模型响应 |
| A07 / I2-10 | 真middleware+PG：旧key新profile、新key模型/旧Feed、跨org/project、自报权限、人字段泄露、日志脱敏、staged/暂停/撤权/轮换 |
| A08 | bootstrap后迟到、pause/resume、7天floor、hold跨7天终态、原context清理后终态自足、各profile不同启用/权限；同刻终态片段保留及晚启用coverage gap。已签cursor高于恢复后high、scope/source epoch变化不能静默跳最新 |
| A09 | 原V1golden bytes及strict decoder/hash向量逐字不变；共享identity/canonical/source hash、revision/adapter升级向量；partial source-evidence仍partial |
| A10 / I2-03/10 | 上述协议矩阵全部真实接缝；Messages V2流成功/取消/中断及V1流拒绝、无标签调用、cache、准入拒绝、async非I-01边界。成功HTTP不能代替源事实成功 |
| A11 | `internal/migrate/source_facts_migration_integration_test.go`：M115→M116、空新装、重复/中断、bridge旧schema运行、旧binary拒新schema、unsafe down拒绝、恢复；M116上的lease/manifest、counter v3和capability publication原能力保留，拒未知head；各实际支持安装路径check/dry-run前后digest/行数一致 |
| A12 | 同假provider、实际handler和相同PG/Redis条件，base/candidate交错批次对比；量p50/p95及差异/误差、DB新增字节/索引/WAL、跨profile并发锁等待、原journal/outbox至源页延迟、独立consumer落地。试验建议p95≤5%且≤5ms，样本/波动不足不称通过；组合页面≤60秒仍待XI/E |

复用原I2-07（候选与attempt）由B完整验收；A不能声称已完成全部I2运行史。原R1—R5样本保留金额/身份，测试转换器仅映射正式候选schema；新增异常样本不混入原汇总。独立检查器用公开page/schema/冻结向量和原V1预期，不依赖sourcefacts写入实现自行证明自己。

## 8. 迁移、升级与恢复方案

拟议新增`000116_execution_source_facts`（编码前再确认编号未占用），只expand新表/列/索引/约束及source purpose，历史行无人员回填。默认capture关闭，新writer在M115不触碰不存在的表。

新binary增加明确M116完整ledger支持并保留受支持M113/114/115分支；不是允许任意future head。必须一起更新和验证以下有限谓词，不能仅扩schemaauthority：

| 接缝 | M116必需行为 |
| --- | --- |
| `internal/schemaauthority/{version,compatibility}.go` | 完整ledger及精确受支持heads；capture关闭时bridge仍可在旧合法schema运行 |
| `internal/runtimegeneration/lease.go:133`、`cmd/uniroute/runtime_generation_cli.go:74` | 在已验证M116 ledger上允许现有lease/状态读取；旧heads的实际差异保持，拒任意未来head |
| `cmd/uniroute/runtime_generation.go:131` | 原counter v3不能因head从M115升为M116消失；按有限兼容集合验证实现ref、manifest与schema，不放宽计数器资格 |
| `internal/modelcapabilitypublication/plan_v2.go:131` | M116复用现有capability authoring及静态投影合同；M114/M115支持与未满足条件的拒绝保留，不更改Publication/Release内容 |
| `cmd/uniroute/compatible_expand_cli.go`、`scripts/compatible-expand.py` | 明确M116 target、M115前驱和幂等已达目标；继续精确fleet、lease、recovery process、manifest/implementation refs、migration digest及迁移锁保护 |

现有compatible-expand仅支持development/test/integration，固定`uniroute-api`/`uniroute-api-canary`两实例。此路径按原合同先换两实例bridge且capture关闭，确认完整fleet可接受M116，再apply并显式启用；本地模拟验证精确次序，不泛化为任意客户拓扑。客户Compose安装/升级另沿其实际受支持的installer/preflight/签名upgrade检查入口，核对原维护窗口、停机/迁移责任及恢复合同，在本地临时部署资料上验证；没有依据就不能声称在线无停机升级或已通过Docker运行验收。X保留这两类入口的独立证据，当前不执行真实rollout或正式签名。

1.2.77在M116不能直接重启；升级失败首先由新bridge binary继续在合法schema运行或前滚修复。空未使用M116允许受guard的down；已有source数据/凭据/运行依赖则down明确拒绝，禁止删证据换回滚成功。恢复用本次隔离备份恢复副本验schema、原经济事实、cursor/floor、pending outbox、credential状态；恢复后消费cursor领先须显式处理gap。

同步受影响schema ceiling、runtime contract清单、安装/升级validator、preflight/dry-run及版本元数据。功能是新增兼容能力，拟用产品minor `1.3.0`（实施稳定后按实际交付基线核对），不是仅修改VERSION的patch豁免。正式artifact/tag/release均未授权。

## 9. 后续全部未阻塞UR-B/D/X顺序

A共同方案→A实现/自测/完整交付→监督独立验收→B→D→X综合。X的配置/迁移兼容从A同步建设，最后集中复验。若某真实来源阻塞，只暂停依赖分支；不把A通过当整轮完成。

| 批次 | 有界实现与文件接缝 | 原目标/依赖 |
| --- | --- | --- |
| B1：Runtime/Attempt | sourcefacts追加runtime/attempt profile；原provider_attempt start/complete/hold/delivery writer及public/relay/media接缝。同tx保留已承诺stage修订，request终态与capture close独立；每请求阶段/attempt有上限，不记逐token | CT-02、GX-01；CT2/R/T、I2-07/08。3候选1执行、cache无新attempt、入站拒绝、fallback/取消/流断、Provider接收未知及Messages V2流特有终态都实测 |
| B2：能力/Policy/Destination | 从当次route evidence/Release pin/protocol/profile/Probe/Approval产生受限snapshot；复用已实际执行guardrail决定/模式/版本/效果；endpoint只输出规范化允许的origin/Provider/配置地域，排除userinfo/query/secret | CT-01/SE-01/02；COMP/S/DATA；静态能力≠当次成功，配置地域≠驻留，body写完≠上游接收，目的地≠敏感数据已外发。无content binding保持未知 |
| D：预算及财务解释 | sourcefacts预算profile；project budget更新原tx保留setting/revision，直接读取nullable列；复用`events`预算提醒、project spend和enterprise BudgetPlan/Position/allocation run。按原scope限制投影，当前scope之外的org预算不泄露给project key | FA-02/03、GX；B01—15/FA3；项目gateway charged UTC周期、enterprise report-only、源摘要/明细覆盖、币种scale守恒。预算摘要不加到源费用；无供应商账单则FA-04待独立源 |
| X：UR可组合交付 | 现有接入管理、source capability、配置说明/安全默认、OpenAPI/前端docs入口、客户Compose preflight、签名upgrade检验及兼容包。产品端显示未安装/未配置/暂停/可拉取，不把ack当XI可查询 | GX/EXT/COMP及E的UR部分；无XI照常运行，旧消费者继续可用，独立profile拒未知，恢复保持身份。共同安装/SSO/真实XI/部署/E归监督后续，不擅自实现或声称完成 |

D不新建计算费用预算权威；若使用已有enterprise定义的合法经济层，按原版本读取；旧项目预算不改义。未来新增calculated预算或超范围预算控制属于独立产品决定。必要目录只输出此字段profile最小对象引用，不导出整个人员目录。

## 10. 文件范围与相关技术债

计划文件落点：
- 新：`internal/executioncontext/`、`internal/sourcefacts/`、`contracts/source-facts/v1/`（schema/manifest/向量/有效无效页）、A/B/D对应migration及集成测试、`docs/tech-ref/55-source-facts-v1.md`（编号实施前核对）。
- 修改：`middleware/auth.go`、`agentcap/{types,service,authority}.go`、`accessgrantboundary`必要返回值、`billing/{billing,outbox,terminal_materializer}.go`、gateway共有准备/Freeze/guardrail/attempt接缝、`mediajob`原写入事务、`cmd/uniroute/main.go`及route/OpenAPI parity。
- 接入：`internal/xinsightconnector`原可复用control primitives及source专用binding；现有接入管理handler/UI仅新增profile配置/能力及错误呈现，不整页重写。
- 配套：`internal/schemaauthority`、runtimegeneration/compatible-expand合同、`deploy/customer/preflight.sh`及相应测试、`.env.example`、OpenAPI、backend/frontend docs、VERSION/CHANGELOG/package metadata；必要ADR使用现有模板登记本计划裁定，不再建另一套台账。

| 技术债 | 本轮处置/验证 | 保留与退出 |
| --- | --- | --- |
| legacy app遗漏、grant/Agent snapshot被丢弃 | 同次权威输出与历史持久化；改归属后回读旧请求 | 不改原auth含义/authority version；不引入第二resolver |
| 默认mode、NULL预算、unpriced零的语义损失 | 新源显式provenance/setting/pricing状态；原getter/wire保兼容 | 旧消费依赖未核验，不删除旧API |
| terminal materializer单槽及重复V1快照构造 | 用有限composition按固定顺序运行V1与source hook，共用纯BuildSnapshot；原错误/重试语义回归 | 不改原V1 bytes/序列；没有新的财务ledger |
| trace/updated_at被当事件史风险 | B在原writer补有界观察，scope明确；重复/恢复负向测试 | 旧trace仍用于原诊断；不因新profile删除现有日志/历史包 |
| schema/安装/前端docs分离漂移 | 同一版本适用检查与exact full-ledger bridge测试 | 不复用旧Target/CI记录，不放宽required gate |
| 旧profile/旧writer退役 | 本轮为并列补充，不替代V1、campaign包或model data路径 | 无删除/退役授权；新路径、兼容、退役分别报告，保留有真实消费者合同的旧路径 |

## 11. 必需本地检查与环境就绪度

已核对工具：Go1.25.12 darwin/arm64匹配go.mod toolchain；专用PG16.14在`/opt/homebrew/opt/postgresql@16/bin`，PATH默认PG18.3不能冒充PG16；本机Redis8.8.0、Node23.11.0/npm10.9.2。没有启动任何服务，没有读取DSN/凭据。docker不在PATH，常见Docker.app路径不存在；Docker build本地执行能力尚未证明，不因此删gate或自动安装。

实施测试将在本轮唯一临时根创建PG16 cluster、Unix socket/独占127.0.0.1端口及合成DB角色；Redis专用进程/端口，严禁复用现有开发Redis。实际环境变量逐项显式赋值，`REQUIRE_TEST_DATABASE=1`，禁止继承TEST_DATABASE_URL默认localhost:5432。现有SetupTestDB会TRUNCATE，只有本次专用实例允许；优先SetupMigratedTestDB临时库与精确artifact installer，允许工具清理自身生成数据库。不触碰任何用户现有库。

验证顺序：
1. new source单元/严格schema/hash/profile/冲突/权限；billing/middleware/agentcap/媒体接缝定向integration（真实PG、fake provider）。
2. A01—12及协议矩阵、V1golden/旧sourceevidence和共享`eventconnector/...`回归；原billing/provider fence、mode/pricing、恢复测试。
3. 候选稳定后`go vet ./...`、全Go race回归；按当前`.github/workflows/ci.yml:316-387`保留原consolidated integration选择与admin integration全部必需覆盖，并追加新sourcefacts的非空测试选择；避免把未运行或skip称通过。不得悄悄缩小原选择集。
4. frontend type-check/tests/build与route/OpenAPI/doc parity；必要接入管理浏览器验证用本次临时实例，遵循实际工具可用性。产品没有新UI改动的切片不能因此取消最终适用Frontend gate。
5. `git diff --check`、release metadata、现有schema/upgrade/customer preflight及无副作用校验；Docker Build/Compose/config/secrets-context gate在有授权且可用的本地runtime上执行。工具缺失准确标待环境，其他本地检查继续。
6. shared protocol 2.0.2本地向量及原sourceevidence资格回归；其中exact Neutral N1测试需`UNIROUTE_INTEGRATION_CONTROL_N1_ROOT`绑定经校验的本地不可变release资料，当前未验证该路径。资料缺失只阻塞该资格检查，不能标pass。原远端Change Routing/Backend/Frontend/Docker/Browser E2E及exact-main协议工作流全部待独立CI授权，不声明已通过。

现阶段没有产品测试结果；上列全部是将执行的检查。因仅交付计划，本次只做基线/代码/依赖定位和计划一致性回读，不运行整套基线测试制造无关成本。

## 12. 交付、待决定与停止点

负责人应重点审阅：auth成功边界记录的可用性代价、字段profile拆分、I-01未启用时的expected reference、source revision/event_id、同事务发布与跨保留窗自足终态、完整M115/M116兼容谓词及两类安装入口、Messages V2流与async独立经济合同；这些均为常规技术方案，现无需要用户补答的产品范围问题。

本次交付此精确计划和独立只读审查结论，待负责人回传共同方案再编码。适用`AGENTS.md §4`原文：“变更共享架构、身份、迁移、财务、权限、路由、Release，或启动与恢复合同前，取得一次独立审查。”作者自查不满足；审查只裁定validated_blocker/validated_required/valid_deferred/rejected/product_decision_required。负责人审查若已覆盖同一对象可以满足要求，不为重复仪式新增审查层。

本轮独立审查对象为原稿SHA-256 `ce68898bbaf929e7af60e762c1b25ce3d67eb3b368182dfa2964883d35e22794`；4项validated_required全部采纳：Messages V2流、跨profile自足保留、M116有限兼容谓词、取消新增publisher。旧journal重放会重建源事实的疑虑依据`created &&`守卫判rejected。精确审查、修订及收尾结果见`docs/agent-workflows/reviews/UR-WORKFACTS-PLAN-REVIEW-20260909.md`，不作为实现已通过证据。

最终本地迭代必须交付A/B/D/X全部未阻塞实现、精确commit/tree/diff、真实测试/负向证据和UR owned版本化候选合同包。给XI负责人的包包含profile/字段授权、I-01关联、金额性质、identity/hash向量、分页/retention/late arrival/gap、支持矩阵、有效无效页、典型请求/过程/预算样本、准确下游适配清单。仅交监督任务，不派XI改造。

原13项回填：CT-01→B；CT-02→B；US-01/02→A；US-03→稳定引用（XI用途/复核）；SE-01→A/B；SE-02→B；SE-03→XI处理；FA-01→A；FA-02→A/D；FA-03→D；FA-04→独立账单条件；GX-01→A/B/D/X。未接入资源/活动、真实XI、真实模型、组合安装/部署、父级A—E/整版仍逐项待证，不能由本轮UR局部结果代替。

经验文件无需变化：本次仅只读设计，没有新的可执行约束及运行证据达到admission threshold。已有MAR-005/006/008/010等按身份/赞助/精确迁移/既有权威边界应用；不为收录经验新建实现。
