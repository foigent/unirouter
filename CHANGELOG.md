# UniRouter Changelog

All product releases follow Semantic Versioning (`MAJOR.MINOR.PATCH`). Deployment
artifacts additionally retain an immutable runtime version and exact Git commit;
those build identities do not replace the product version.

## [1.15.3] - 2026-10-31

### Fixed

- Gate the default-customer baseline and background writers on the existing native runtime lease and ingress admission. Keep health checks available while ordinary requests wait for startup completion; preserve original account data and no-op connector reconciliation during maintenance verification.
- Add M129 to correct the persisted M128 upstream-observation validator's PostgreSQL namespace binding. Verify and restore populated M128 cold backups through the existing isolated restore owner without changing the original dump, source ledger, CHECK constraints, foreign keys or triggers.
- Extend the ordinary full-package upgrade owner to exact M127/M128-to-M129 and M129-to-M129 transitions. Subsequent upgrades bind the completed current release and image override, retain prior installation-state bytes and operation evidence, and verify the full cold recovery pair. Same-schema upgrades preserve the complete ledger without invoking migrations.
- Recreate the selected application containers when an upgrade reuses the same images, preserving mounted data and the existing distinct-container and runtime-identity checks. Resume observes completed startup effects without recreating them again.
- Verify original data before receiver admission, then retain completed-upgrade validity when legitimate business writes occur afterward. Completed resume checks current release/runtime/admission and retained recovery evidence without repeating migration or release recording.
- Let sign-in preview metrics wrap to the available width instead of forcing three columns on narrow screens.
- Show the canonical gateway `X-Request-ID` in the project request debugger after an HTTP refusal, so the displayed failure can be correlated with the retained request evidence.
- Load organization-administrator shared-limit selectors from the existing customer model catalog, preserving platform inventory permissions and valid project/key options.

### Validation boundary

- Normal application patch candidate, separate from the historical `1.15.2-recovery.1` maintenance envelope. Supporting source, PostgreSQL, browser and offline contract checks are local; this version entry does not assert native Linux/Docker qualification, a signed artifact, public release, Target deployment, external Provider/X-Insight integration or Production acceptance. The prepared Linux fixture includes retained-source and same-schema upgrades; its actual execution and existing 45-minute budget remain unverified.

## [1.15.2] - 2026-10-30

### Fixed

- Extend the finite, separately signed maintenance recovery to prove removal of the one original admin session that had already expired before the bound candidate writer started. Retained sessions must remain fully identical; new, modified or missing live sessions refuse recovery. Bind the exact original table and cutoff, derive the expected table only from the unchanged isolated restore, and reject changed observations before any recovery witness or native effect.
- The new recovery envelope uses `1.15.2-recovery.1`; earlier signed packages remain immutable. Original application images, backup and native completion identity remain unchanged. This source candidate does not assert a new signed artifact or Target recovery.

## [1.15.1] - 2026-10-29

### Fixed

- Preserve an X-Insight setup record when its normal reconciliation finds no state or progress change. Real progress, state transitions, authorization failures and audit rollback keep their existing behavior.
- Add a separately signed, bounded maintenance recovery entry for an already stopped M127-to-M128 upgrade whose only protected-data difference is one explicitly bound setup timestamp after the candidate writer started. Reuse the original authenticated release, constructor, checkpoints and remaining native steps; preserve the original backup and completed receipts. Reject other row, field, configuration, writer or restore changes before recording the recovery owner. Read-only checks create no lock or witness.
- The recovery tool has its own `1.15.1-recovery.1` envelope identity and three text payloads. It is not an application install package and does not relabel, rebuild or replace the signed 1.15.0 images. Completion retains the original application release identity.
- Local source candidate only. No CI, signed recovery artifact, Target recovery or Production acceptance is asserted by this entry.

## [1.15.0] - 2026-10-28

### Added
- Reuse exact existing runtime, notices, corresponding-source and system-material byte groups in ordinary full-v2 assembly. Compare the selected artifact inventory before private-key access and retain native streaming rehash. Fixed producer, publisher and consumer programs now accept closed stage records, retain actual predecessor evidence and compare full adjacent-stage identity through the existing signed provenance artifact.
- Add one finite Linux amd64 community upgrade entry for retained M127 installations to M128: read-only check/status, signed full-package and exact predecessor admission, all-writer retirement, cold paired backup with independent restore, preserved configuration and explicit interruption readback. Standard single-writer and the named UR edge/stable-canary layout share the same native owners and host path. The legacy development/test retirement command keeps its existing environment boundary.
- Admit explicitly proved historical M127 terminations into the same signed, atomic maintenance batch. Frozen original process or native replacement observations remain distinct from the current writers' clean-stop proofs; unknown history still refuses before maintenance.
- Record a provider-neutral upstream release declaration with endpoint scope, source, server identity and compare-and-swap revision. Rechecking the same declaration preserves its identity; a changed declaration starts a new evidence epoch. Remote model weights remain unpinned.
- Carry the recorded declaration through native operation/capability Probes, new approvals and immutable release-authoring confirmation. Guard new traffic expansion while preserving historical evidence, existing active execution and safety contraction.
- Show API alias, upstream declaration, UR contract and Probe applicability separately in English and Chinese. M128 requires exact M127 and stopped, retired writers; ordinary startup and rolling expand cannot apply it.

### Fixed
- Read native Compose replacement completion by exact container and status tokens across plain-output spacing, retaining every required event and the original command evidence bytes.
- Parse native PostgreSQL lease timestamps with every microsecond precision on supported Python versions, preserving explicit timezone offsets and exact expiry checks for current and historical writers.
- Give the isolated restored-content verifier a bounded private parent for its original read-only body/token mounts. Preserve separately mounted token files, original recovery metadata and native binding privacy checks.
- Compare complete Docker mount inventories without depending on their returned record order throughout community upgrade and resume. Preserve every mount field, duplicate count and exact container/image guard; real mount replacement or permission changes still refuse maintenance.
- Reset console scroll for new views and restore history positions after lazy content loads within the current session. Keep mobile model drawer controls and long audit identifiers within their containers, with keyboard focus and Escape support.
- Improve English and Chinese status labels, compatibility notes and communication outcomes; compact empty dashboards and prioritize available integrations. Keep settlement definitions, reporting-only budgets and independent model readiness facts visible.
- Add a versioned meaningful-task successor for the current DeepSeek streamed-tools capability Probe. Request construction and strict answer validation share one private immutable task/tool/oracle description; legacy Probe bytes, bindings and evidence retain their original semantics.
- Give native capability Probes their declared 30-second HTTP budget while retaining 15-second operation/discovery checks, parent cancellation, bounded headers and redirect refusal.
- Provide a selector-only capability Bundle for the retained DeepSeek Definition. Publication, native qualification, approval and Gateway activation remain separate owner actions; the Probe declaration remains separate from the M128 schema change.
- Local candidate only. This entry does not assert CI, publication, signed-artifact, Target, Provider or Gateway acceptance. A preceding binary cannot execute the new Probe; rollback requires the existing exact Release/implementation compatibility checks.

## [1.14.2] - 2026-10-25

### Fixed

- Allow first-customer initialization after normal cleanup removes an expired administrator session while retaining its canonical logout audit. Retained session witnesses must still belong to the selected administrator and be revoked; absent witnesses require a canonical positive session ID and a nonblank operation identity. Current-session authorization, exact audit fields, empty business state and release bindings remain required. Authentication and release history are preserved.
- Exercise the native logout, expired-session cleanup and signed-upgrade history in the existing first-customer CI selector, including read-only previews, commit/replay and rejected malformed or contradictory history.
- Local candidate only. This entry does not assert CI, publication, signed-artifact, Target, or Production acceptance.

## [1.14.1] - 2026-10-23

### Fixed

- Revalidate the console HTML entry after deployments while retaining immutable caching for content-hashed assets. Lazy resource failures now explain the loading problem in English or Chinese and offer an explicit page reload; generic rendering errors retain their existing fallback. No automatic reload, retry, form replay, or session change is introduced.
- Allow first-customer initialization after repeated same-admin password changes, logins and logouts, and after a normal signed patch upgrade. Authentication and release history remain intact; the current session, exact latest release binding and empty-business-state checks remain required.
- Support the exact production-security community backend in the existing verified capability-data publisher, with hostname, image and Compose project/service checks. Publication and model qualification remain separate from route activation.
- Local candidate only. This entry does not assert CI, publication, signed-artifact, Target, or Production acceptance.

## [1.14.0] - 2026-10-20

### Added

- Explicit non-expiring upgrade packages: `uniroute-upgrade pack --no-expiry` signs a v2 manifest with `expires_at: "never"`. Existing finite v1 packages retain their original expiry rules and canonical encoding. Old verifiers reject v2 packages; both the standalone verifier and backend must be upgraded for v2 installation and release recording. Database schema remains M127.

## [1.13.0] - 2026-10-19

### Added
- Connection-scoped automatic communication controls, persisted cadence and bounded manual Sync once for the X-Insight connector. New connections start in manual mode; existing connections retain their configured cadence.
- Truthful in-flight and peer-unknown status, durable one-shot receipts and M127 maintenance migration. Automatic pause preserves credentials, grants, cursors, local expiry and completed operation journals.

- Preserve communication intent and run receipts during reset preview; reset apply remains refused even when these tables are empty.

### Upgrade
- M127 requires the existing maintenance owner with stopped, retired writers. Old binaries cannot run on M127. Preserve the pre-M127 backup for rollback; do not discard manual settings or one-shot receipts. Fresh installers require the M127 release schema.

## [1.12.0] - 2026-10-11

- Refresh the console workspaces with model request/cost composition, aligned request and cost trends, persistent model details, and contextual settlement/notification actions. Current deployment availability remains separate from selected-window statistics.
- Default to English while retaining existing explicit Chinese preferences. Add persistent Chinese/English preferences throughout the shell, sign-in, settings, overview, model lifecycle, requests/usage, monitoring, access/people, security/audit, finance, integrations, developer workspace and API reference. Customer names, protocol values and original evidence retain their identity.
- Put request filters above the list, expose request time, model/provider, status, latency and cost beside the evidence inspector, and preserve all exact evidence filters. Combine rapid filter edits and reject stale query responses. Usage analytics puts scoped metrics first and explicitly labels its top-50 grouped rows versus complete summary/CSV scope.
- Group audit and people records into readable columns, prioritize request records in reconciliation, default pricing to the available customer workspace, and make the permitted API endpoint directory searchable. Missing financial summaries remain unavailable rather than appearing as zero.
- Load domain translations with their lazy pages, retaining shared labels and deterministic conflict checks. Language changes preserve drafts, filters, selection, exact pricing and duration/size payloads without repeating reads or invoking providers. Improve narrow-screen forms, date labels and mobile navigation dismissal.
- Include exact per-model `cost_micros` strings in the existing dashboard aggregation. Compositions include all models, preserve model identities, and explicitly handle older backends without this field.
- Local candidate only. No CI, release, deployment or Production acceptance is asserted by this entry.

## [1.11.0] - 2026-10-11

- Add an online Linux amd64 community bootstrap candidate: pin download identities, verify the existing signed package, prepare declared files, then call the established installer.
- Reject unsafe archives, excessive decompression, changed or incomplete preparation, and root reuse of another user's files. Check/resume reuse the original package and current expiry without downloads or credential resets.
- Preserve the existing direct installer and source-level arm64 path. All ten synthetic Linux amd64 installation/recovery checks passed; official public key adoption, artifact publication and public-download acceptance remain pending.

## [1.10.0] - 2026-10-10

- Adopt Apache-2.0 with bilingual project and contributor documentation.
- Retire runtime commercial License admission, imports, refresh and resource-count limits. Authentication, tenant/project policy, financial controls, model qualification and signed upgrade trust remain.
- M126 requires maintenance from the complete M125 ledger. Ordinary startup cannot apply it; old binaries do not support M126. Retain the pre-M126 backup and original Artifact for recovery.
- Deprecated License APIs return `not_required`; imports return `imported=false` without modifying historical records.
- Add a Linux/ECS installer candidate for a verified signed release: independent secrets, private Compose setup, formal fresh initialization and bounded resume. The verifier now exposes the exact signed manifest digest. Public packages remain pending; the native workflow records exact-artifact installation results.
- Pin the customer frontend readiness probe to IPv4 loopback, matching its NGINX listener; native acceptance caught a running console marked unhealthy through `localhost`.
- Unreleased candidate; exact CI and native installation evidence are recorded separately. This entry does not assert public release, Target, Provider or Production acceptance.

## [1.9.8] - 2026-10-09

- 修复正式 DeepSeek 1.1.0/v6 能力探测的 admin 响应分派，复用已发布 Probe v2 的严格解析与终态校验。
- 响应 validator 在网络请求前从实际分派解析，响应处理复用同一 validator；不支持的合同不会触发供应商调用。
- 正式 Bundle HTTP/PG 验收覆盖旧 Definition 保留、新 candidate、单次组合调用的六条事实、审批和 Release 绑定；历史合同与 Bundle 不变。本条不代表现网或供应商验收完成。

## [1.9.7] - 2026-10-09

- 增加显式 DeepSeek Chat v6 与未发布的 `deepseek.v4.1-flash.api@1.1.0` 后继 Bundle 候选，复用已有 provider-final 输出机制。冻结额度 B 原样发为 `max_tokens`，deviation 为 0；解码字节采用资源保护上限，避免将 UTF-8 字节当作供应商 completion tokens 截断。
- Runtime 与新 Probe v2 共用严格终态校验：最终空 choice 带有效 finish 与 usage、完整工具参数及 `[DONE]`。新 Probe 保持 thinking enabled/high 和 `max_tokens=2048`；旧 DeepSeek v5、Qwen、历史 Bundle、Release、部分流估算结算及成本观察规则保持原合同。
- 本条仅为本地候选；不表示 CI、正式 Publication、目标升级、真实供应商调用或生产验收已完成。

## [1.9.6] - 2026-10-08

### Added
- Independent, immutable `deepseek.v4.1-flash.api@1.0.0` text Chat Bundle for the current floating `deepseek-flash` upstream identity; historical V4 Flash definitions and Releases remain unchanged.
- Version 3 of the default-effort streamed-tool Probe uses explicit protocol/reasoning/cache composition and exact upstream/data binding. It keeps thinking enabled/high, strict reported-model/usage/DONE validation, and safe persisted diagnostics; historical Probe v1/v2 semantics remain unchanged.

### Validation boundary
- This candidate requires local exact-data preflight/runtime and PG16 HTTP publication, qualification, freeze and initial-activation verification. Bundle data grants no Target publication, Provider conformance, pricing, client access or Production readiness.

## [1.9.5] - 2026-10-08

- 共享流式 Function Tools 能力探测在失败时保留固定的阶段与原因分类，真实记录已组装 data 事件及已解析 delta 数量；POST、持久回执、GET 与管理员页面一致展示安全诊断。原有合同错误码、请求形状、严格模型校验和资格规则保持兼容。

## [1.9.4] - 2026-10-08

- 修复数据化 Chat Profile 的默认推理能力准入：网关按实际默认推理合同要求相应 `reasoning` 或 `reasoning_effort` 资格；新 data-Chat Release 在签发时逐路由核对精确能力批准，历史 Release 的读取与验证合同不变。
- 为 DeepSeek V4 Flash 1.6.0 增加独立 `reasoning_effort` Probe v2 与 Capability Bundle；保留原 Probe v1 及既有五项能力绑定。该修正和数据仅提供后续正式发布、Probe 与新路由激活的源码条件，不表示目标环境已完成。

## [1.9.3] - 2026-10-06

- 修复 development/test 已启用来源采集的 M124→M125 升级：沿既有严格桥接保留 stable/canary 原采集、生产者、正文存储及静态对端绑定，拒绝配置漂移；兼容扩展 CLI 从受支持迁移头顺序确定准确前驱，允许 M124→M125 且拒绝跳级。
- 必需数据库测试须显式指定测试 DSN，并在创建或清空数据库前核对 PostgreSQL 16，避免默默回退到其他本机实例。本条为本地修复候选，尚未发布或部署。

## [1.9.2] - 2026-10-06

- 为既有 X-Insight Connector 增加控制凭据到期前轮换与双端管理员确认的过期恢复；按原 installation、identity、origin、scope 和来源绑定接续，并保存加密操作日志和回执。撤销的连接不进入恢复路径。
- 增加 M125 扩展、运行时资格与预算恢复证明支持，安装状态展示凭据代际；原业务 ACK、control fence 与历史不重置。本条记录本地候选，尚未发布或部署。

## [1.9.1] - 2026-10-05

- 修复 X-Insight 邀请在密码登录、过期会话或登录状态暂不可读时丢失的问题：在当前页面内存保留固定接入目的，重新核对当前账号及项目权限后继续原邀请预览和最终确认；刷新或企业登录后需从 XI 重新打开邀请。
- 补齐接入状态说明：区分等待 XI 加载正文保管设置与需管理员处理的条件，提供受限诊断编号；保留双端实际状态，重新读取不会重复确认或授权。

## [1.9.0] - 2026-10-03

- 增加 M124 与显式 Connector control v3：管理员可接管现有活动连接，保留原凭据、出口约束、同步进度和历史；邀请兑换 v2 冻结原接收端公开身份，旧邀请与新安装 v2 合同保持独立。
- 接管分为原凭据绑定和读取确认。确认分别核对 UR 认证记录及 XI 读取 receipt，不比较两机时钟；两种机器用途独立交付公开候选，保留管理员分别登记、激活和授予权限的维护入口。
- 原 v2 新安装完成后，可沿原 feed 凭据只读核验与安装 consent 绑定的来源身份及出口约束，供 XI 准备机器候选；该 descriptor 不改变同步进度、不创建凭据或授权，也不表示机器已经可用。
- 详情页仅读回已校验的公开候选与接管摘要，按当前安装与 fence 限定；供数授权沿正式已启用字段集合保留原八项，避免自动扩展预算字段或静默缩小原范围。
- 正式安装、M115–M124 兼容扩展、runtime 支持门禁、恢复 helper 和受保护降级同步支持 M124；历史 M118 证明及旧 v2 生命周期保持。本条记录本地候选，尚未发布或部署。

- 增加普通管理员接入向导：组织管理员可访问准确的正常安装入口与集成页，使用真实项目目录与独立 setup 接口；旧平台维护与历史账本保持独立。一次配置静态受信对端，之后沿 XI 自身登录、原邀请链接及 UR 最终确认完成两端同意。后台按真实确认者当前资格推进原机器注册、激活、独立委托和供数；关闭页面或同请求重放不重复授权。
- 预览和保存绑定原来源及策略 CAS；唯一匹配原来源沿原 bind-existing 纳入，保留凭据与进度。企业默认和项目管理与供数排除分别说明；实际接收、正文副本和受保护读取分别显示，空来源等待首条数据。
- 前端交付统一使用精确 Node 24.19.0/npm 11.17.0，包元数据、CI、构建容器与本地校验一致；共享后端资格提供默认八类，其他合资格类别仍须明确选择。

## [1.8.2] - 2026-10-01

- 修复 M121→M123 日常升级的 canary Compose 配置：与 stable 一样显式提供异步媒体关闭时的默认请求期限 24 小时，使两实例通过既有严格桥接校验；保留采集、生产者、正文绑定及 Connector Agent 单写者边界。

## [1.8.1] - 2026-09-30

- development/test 日常 M121→M123 正式升级载体保留原 A/B/C 采集、生产者与正文存储绑定；M115–M120 仍要求采集关闭。仅在旧媒体服务已关闭、旧配置无该项时，允许新 Compose 增加默认 24 小时的媒体请求期限设置；其他环境、挂载、身份及迁移字节漂移继续拒绝。
- 本修正只扩展受支持升级前态，不自动迁移数据库、退役历史运行代、绑定 X-Insight 凭据或证明 Target 已升级。启用异步媒体的 M121 前态仍被拒绝。

## [1.8.0] - 2026-09-29

- 增加按精确 X-Insight Installation 绑定的策略管理与 A/C 自动供数独立机器凭据、人类委托、当前及未来项目发现、事务性策略 CAS/审计/回执，以及新项目 A/C 项目凭据注册与读时撤权检查。
- A/C 注册的幂等 200 回读保留首次 201 的 `registration_hash`，供 XI 按同一凭据和注册摘要核对；本地 UR↔XI 隔离 HTTPS/PG 联合用例覆盖首次接入、前向起点后的事实与正文消费。
- 审计 V2 区分人类、服务和系统身份，V1 保留兼容投影；管理页提供委托、供数授权与凭据生命周期入口，OpenAPI 记录机器合同。既有人工 A/C 连接需由 UR 管理员按精确前态显式纳入，旧 token 与游标不变，纳入后仍须 XI 实际读取验收；本条仅为本地候选，未发布或部署。
- Exact Response Cache 仅在有效策略允许 `openai.chat_completions.v1` 协议及 `provider_response`、`client_response` 两个响应观测点时读取命中或提交新条目；提供方调用返回后的提交会重新读取策略，任一条件被移除即释放预留并跳过写入。

## [1.7.0] - 2026-09-29

- 增加企业默认、项目显式覆盖和个人密钥例外的内容策略 V2 管理与运行解析，按已认证主体冻结作用域、策略身份和版本向量，并共用项目正文额度；旧项目策略保留显式覆盖语义。
- Exact Response Cache 在正文禁存时跳过命中与新提交；新异步媒体任务按有效执行暂存策略准入，固定请求密文截止时间，过期未派发任务在 worker 恢复时清空，并回读实际清空时间。旧异步任务不回填截止时间。
- 新增 M122 非滚动扩展及回滚拒绝条件，管理界面展示三层解析和执行暂存状态；本地候选尚未发布或部署。

## [1.6.3] - 2026-09-29

- Adapter 解析、上游响应读取及全部路由失败时，应用日志保留失败阶段，不再附带可能含有 Provider 控制正文的错误文本；覆盖非流响应与流式 chunk 路径。网关响应、Provider 调用及财务终态不变。
- 当前为本地修复候选；定向日志负向测试与包级回归不代表所有 writer、CI、Target 或正式制品已验收。

## [1.6.2] - 2026-09-28

- 修复带数据 Release 的模型请求在来源观测开启时可能因 Profile definition digest 格式不符而被拒绝：仅在写入观测引用时补齐 `sha256:` 前缀，保留 Release 原始 digest、严格校验及原有调度事务回滚边界。

## [1.6.1] - 2026-09-28

- 项目正文表单预填推荐的 30 天、5 GiB、32 MiB，并允许逐项留空回退；保存前展示实际值。现有策略值优先且保持秒/字节精度，非法、超限或不整除的单位转换会明确报错。
- 说明当前正文不脱敏、策略只影响新请求、UniRouter 与 X-Insight 的留存边界；将数据交换页的 metadata-only 描述限定为 I-01 完成请求摘要通道。

## [1.6.0] - 2026-09-28

- 在现有 X-Insight 集成页加入项目级正文策略与正文连接管理。策略读取不写入，保存使用期望版本并回读期望/生效状态；完整正文的保留期限和配额必须显式填写，脱敏仍不受支持，页面打开不会启用正文。
- 正文连接沿现有非秘密 registration 和 C 控制 API 管理准备、激活、暂停、恢复及凭据生命周期；所有控制按回读的状态版本提交并核对结果 ID/hash，不修改服务端权限、协议或数据库。
- 区分 Connector 自动配对状态与手工 Feed 实际状态，避免手工 Feed 已启用时仍显示笼统的“未连接”。当前仅为本地 UI 候选，不代表正文已在任何目标环境启用。

## [1.5.2] - 2026-09-27

- 增加 development/test 历史运行实例的正式单实例停止检查与应用：在精确 rollout 退出、撤流和当前进程证据下，核对原支持清单、Target、版本与财务终态，只将旧实例状态置为 `stopped` 并递增修订号。旧镜像及历史证据继续保留为受支持的兼容恢复制品；租约过期不自动退役。
- 宿主命令复用现有生命周期和 ingress 锁；证据缺失、旧容器仍可重启、活跃租约、未决财务、陈旧修订或变化的 ingress 均拒绝。当前为本地候选，不代表 Target 已执行停止或 M117 扩展。

## [1.5.1] - 2026-09-26

- 修正客户升级说明中 M121 恢复辅助脚本的精确 SHA-256 与字节数，并明确 M119、M120、M121 恢复时同一事务重置 C recovery epoch；脚本字节及运行行为不变。

## [1.5.0] - 2026-09-25

- 为认证后的 Chat `routing.no_route` 503 新增独立 B v2 网关执行前终态；Provider、delivery 与财务均保持未开始/未创建，捕获失败保留原 503，不改 v1 财务终态与 completed-execution Feed。
- M121 仅扩展 source stream/connection/credential 的有限 profile 集，运行代次必须声明 M121 与 v2 观测实现；旧读取者继续按显式 v1 profile 订阅。含数据或活跃代次时拒绝回退。
- development/test 现存 M115–M120 数据库使用一次精确迁移后缀 carrier 切换到兼容二进制，再由原 `compatible-expand` 按头逐段扩展到 M121；每对 up/down 字节、旧账本和禁用的 source 配置均须吻合。旧二进制不因新 schema 自动取得回退资格。
- 当前为本地候选；合成 PostgreSQL 全链与源码检查不代表日常备份/运行 fleet、XI v2 消费、CI、Target 或正式制品已验收。

## [1.4.8] - 2026-09-23

- 付费准入在同一数据库快照内区分普通 delivery_pending 与未决风险：精确绑定的持久用量已被预留现金和赠金完整覆盖，或属于成本观察模式时，允许独立请求继续；未知用量、补款不足、交付失败及其他明确风险继续阻断。
- 原请求的 attempt fencing、交付凭据、收费和恢复合同不变；不引入活性租约或调整钱包、池大小及负载阈值。
- 当前为本地候选；离线编译与纯测不代替真实 PostgreSQL、200 RPS、CI、制品或部署验证。

## [1.4.7] - 2026-09-23

- Source Facts A/B 发布在原事务内按唯一 profile 有序建流与加锁，完成重放和前驱校验后按 profile 批量分配序号并写入原始字节，减少重复 SQL；身份、hash、排序、原子回滚及财务边界不变。
- 内容初始未捕获路径复用已存在 stream 的只读 epoch 快照，追加仍锁内核对精确 epoch；首次建流保留独立提交与 health 恢复锚点。策略、manifest、失联提交及恢复 fencing 合同不变。
- 当前为本地内部优化候选；真实 PostgreSQL、原 200 RPS、CI、正式制品与部署验证独立，不以离线测试代替。

## [1.4.6] - 2026-09-22

- 加权准入保留一次 Reserve 的首次 Redis 排队起点；队列项被当前请求或其他请求清理后，原调用不能重置最长等待时间、重新推进公平序列或超期获得租约，等待证据保持累计值。
- 新排队期限语义纳入现有 workload 配置指纹；新旧实例有在途租约或排队时沿用 mixed-config 拒绝与先 drain 切换，旧租约释放及清理兼容能力保留。
- 活动租约幂等、恰好期限时的准入边界、容量与公平策略、终态释放及 Redis 不确定时 fail-closed 不变。本地修复不代表 200 RPS 验收、真实 Redis 或部署已经通过。

## [1.4.5] - 2026-09-22

- Trace 落库时，无 Provider attempt 或 response-cache 投影的 route_channel_id 写为 NULL，保留普通 routing 上下文；共享 call-log 投影、数据库约束和真实 attempt 资格不变。
- 性能诊断纠正通用 context dial 标签，并在原 60 秒收口期限内、轮询前保留有界且精确作用域的 trace/outbox 白名单快照；辅助采样单独报告，不改变原成功判据；不修改费用 hold、池大小、重试或业务超时。
- 当前为本地修复候选；定向纯测/编译与 PG、性能、正式制品和部署证据分别记录。本修复不代表此前 8.223 秒前上游延迟已定位或解决。

## [1.4.4] - 2026-09-22

- 独立 measurement、admission-rejection 和 committed observation 写事务使用 READ COMMITTED；跨来源的 dispatch 交接和已提交 attempt/reservation/delivery 读取各用单条 SQL 快照，保留冻结身份、hash、状态推进拒绝及原失败语义。
- 原财务 writer、连续发布序列、提交原子性、准入未决保护、捕获预算及重试不变。
- 当前仅为临时候选补丁；纯测/编译与 PG/ECS、性能、正式制品和部署证据分别记录。

## [1.4.3] - 2026-09-22

- 修正 durable outbox 有可领取工作时每批固定等待的问题：成功非空批次连续排空，空队列或错误仍沿用原轮询；worker、batch、lease、优先级及重试合同不变。
- Source Facts 捕获失败保留有限 SQL 阶段及原错误链供安全诊断；不改变 SQL、事务隔离、预算、重试或原失败策略。
- 当前为本地源码候选；PG/ECS、性能、签名发布及部署另行核验。

## [1.4.2] - 2026-09-21

- 新增显式离线 `content-store reclaim-unpublished check|apply`：核精确数据库/source/项目/对象绑定及完整未发布清单链，只回收持久映射的 final/temp 普通文件；check 不写入。
- M120 增加 nullable `reclaimed_at`，保留原 reservation 去重行、原预留量、策略和 epoch；文件耐久缺失后一次释放 reserved，stored 与历史清单不变。来源不明 orphan、历史缺失、重复映射和已发布引用均拒绝。
- M119 迁移字节不变；有限 runtime/fresh/restore 路径识别 M120，预算证明仍绑定原 M118 前缀。全部共享 DB/root 的进程停止且禁止重启是强制运维前提，无在线 GC 或目录扫描。
- 当前为本地源码候选；PG/ECS 实际验收、签名发布及部署另行核验。

## [1.4.1] - 2026-09-12

- 新增同一后端制品的 `fresh-install plan/check/apply/resume`，显式接管真实空库，沿用 M1–M118 迁移、独立 Publication Plan 和原 session migration lock。
- 安装中断保留原 Plan；普通迁移前缀已提交时登记后继 Plan，Publication 提交与原 Plan 引用原子绑定。M118 原预算认证、完整账本和安装完成标记在同一事务提交。
- 普通启动与正式管理写入拒绝进入活动安装；已完成安装的历史来源不绑定恢复后的数据库身份或新制品，仍校验当前账本、静态投影和有效预算覆盖证明。
- 当前仅为待原生验证的源码候选；不代表构建、安装、恢复、发布或父平台验收已经通过。

## [1.4.0] - 2026-09-10

- 新增默认关闭的执行、attempt、能力、策略和交付观察，以及原执行租约的当前运行快照；派发前必需来源写入与派发后附加观察分别处理，不改变原金额和 hold 权威。
- 新增默认关闭的项目预算定义、周期摘要及原提醒快照。网关金额只引用原已扣费终态；覆盖缺口和未决经济事实单独显示，企业预算只读取明确绑定的项目分摊切片。
- 企业项目分摊切片为空时仍校验原分摊运行的币种和精度；单位不符不输出可比较零金额或剩余预算。
- 预算字段和范围使用独立访问版本及目录游标，支持当前管理员权限重验和原审计 outbox 原子提交；旧凭据不会自动增加字段，预算撤权保留 A/B 执行进度。
- 新增 M117/M118 显式有限扩展、原 writer 支持证明和预算覆盖认证；正式数据库恢复在读取启动前重建覆盖边界，保留旧历史并拒绝已使用数据的回退删除。
- 修正兼容升级预检对已过期、无入口且完整支持目标版本的历史实例误判；仍拒绝额外活跃实例、不明入口和不完整或损坏的恢复支持证明，不修改历史实例或迁移记录。
- M118 暂无受支持的成功 reset 生命周期：保留预算元数据及原 journal；只读预览报告范围不完整，加载为执行计划和应用均拒绝。关闭 D 采集不解除此限制；A/M116 的历史成功重置证据不代表本版本支持。
- 同步固定字段合同、独立快照校验器、OpenAPI、接入管理、默认配置和客户恢复说明。本地候选版本不代表签名发布、Target 部署或真实 X-Insight 验收。

## [1.3.0] - 2026-09-09

- 新增默认关闭的执行上下文、身份与费用依据源事实 profile，在认证、原冻结及终态事务中保存当时的事实；费用依据引用原 I-01 金额，不另建金额账本。
- 异步媒体保留提交与首次调度快照，终态引用其独立 charge fact；取消、失败及过期恢复与源事实在同一事务提交。
- 新增 M116 显式扩展及 M113—M116 有限兼容支持，保留旧 Feed 字节合同和 counter v2/v3 恢复能力；源数据、凭据或运行依赖存在时拒绝降级删除。
- 已记录的微金额账本安全标记不再随每次财务写入重复更新单例行，保留首次标记、事务回滚和原降级保护；减少这一已确认的并发冲突来源。
- 健康结果在原行锁、版本校验和 attempt fence 保护下读取当前状态，投影统一记录取得锁后的数据库时刻，避免普通并发成功因旧事务快照进入异常 hold；派发授权、金额及未决责任保护保持原合同。
- 新源按固定字段 profile 授权，提供独立的前向游标和七日保留边界。启用采集需要显式配置稳定来源身份；采集写入失败会阻止受影响请求继续分发。
- 新增来源连接管理：注册、激活、暂停、轮换、取消、撤销与字段授权；管理写入重新核对当前管理员权限，权限变更使旧凭据及游标失效，接收端 Bearer 不进入注册接口或管理响应。

## [1.2.77] - 2026-09-09

- 在正式 development/test 构建前检查本地 Docker 28/29、Compose 合并语法支持和 Python 时间戳/缺失对象行为；构建与检查固定同一本地 daemon，失败不创建镜像。
- 将轻量发布兼容检查前置到 Change Routing，保留原有必需回归和构建证据；只有明确预检失败才阻止昂贵后续任务。
- 模型集中验证分别展示最新 Probe 与有效 Approval，精确读取每条 Probe 的批准记录；刷新替换事实及批准响应不明时停止继续提交，不自动调用模型或切换 Release。
- 已配置模型可从批量接入直接进入精确 Deployment；OpenClaw 配置生成使用与同步 CLI 一致的 Provider 名称约束。

## [1.2.76] - 2026-09-08

### Fixed
- Parse Docker RFC3339Nano carrier timestamps on the development Target's
  Python 3.10, preserving the exact identity and remaining-lifetime gates.
  Reject malformed timestamps and invalid timezone offsets; retain failed
  1.2.75 evidence and existing model, schema and financial behavior.
- Run the existing real Docker carrier and rollout/ingress regressions on
  Python 3.10 in CI and record the actual Python/Docker versions.

## [1.2.75] - 2026-09-08

### Fixed

- Recognize the exact Docker 29 lower-case missing-object diagnostic in the formal
  development/test rollout. Exit status, empty JSON, requested identity and all
  non-absence failures remain strictly checked. Preserve earlier refused74
  artifacts and plans; no schema, model, financial or Provider behavior changes.
- Cover old/new diagnostics and wrong-name, permission, daemon and mixed-error
  negatives; record the real Docker version in the existing carrier CI evidence.

## [1.2.74] - 2026-09-08

### Fixed
- Keep the formal runtime-generation CLI usable on both exact M114 and M115 ledgers. Unknown, incomplete and legacy-only ledgers still reject; control commands have a bounded execution deadline.
- Prevent compatible-expand checks from creating Python import caches in immutable source worktrees.

### Changed
- Add explicit `artifact_cli` control-command carriage to the existing development/test rollout. It uses the exact candidate backend image and the same typed ingress owner, with operation locks, a short-lived private container, verified identity/configuration, bounded stop and ambiguity readback. This allows a healthy M115 service with the old M114-only CLI to roll forward without raw database repair or stopping both backends.
- In this explicit mode, plan/inspect create and remove a temporary CLI container; they do not mutate database state, ingress membership or serving processes. Default read-only behavior and the existing model, financial and schema authorities are unchanged.

## [1.2.73] - 2026-09-08

### Fixed
- Add an opt-in cumulative stream estimate (counter v3): legal partitioning of the same delivered text no longer changes estimated tokens. Supplier final usage remains independent; historical v2 amounts and checkpoints are not recalculated.
- Persist one immutable estimator version per stream and carry it through checkpoints, both recovery owners, replay and settlement. Reject unknown or mismatched versions before financial effects; retain provider-only output holds.

### Changed
- Add the finite M115 schema bridge. Roll the bridge with the v2 writer first, explicitly expand through the existing schema owner after verifying all serving/recovery actors, then select v3 through the same-image formal rollout. Busy control/financial locks reject the migration without waiting for a deadlock victim. Default writer remains v2; M114 plus v3 is rejected before serving.
- Formal development/test rollout supports only the exact M115 migration-pair carrier and same-image v2/v3 writer transitions. Existing ingress, backup, predecessor, drain and recovery checks remain. M115/v3 history prevents rollback to a pre-bridge binary; no historical bills or prices change.

## [1.2.72] - 2026-09-08

### Added
- Concentrated model qualification with one credential selection, exact server-planned shared probes, individual results and explicit independent approvals. One equivalent streamed-tools request can produce several capability facts; non-equivalent contracts remain separate.
- Exact shared-observation readback after an unconfirmed result, without repeating Provider I/O. Group persistence is atomic and current safety/revision drift invalidates the complete group.
- Saved-model next actions and client setup using stable names, application-key placeholders and the existing OpenClaw dry-run sync.

### Unchanged
- No schema, Profile/Bundle, Release/Slot, runtime parser, pricing or historical evidence changes. Probe success does not approve or activate a model.

## [1.2.71] - 2026-09-08

### Fixed

- Explain rejected Chat output-parameter spelling through the existing exact
  validator. Preflight adds safe field migration details; normal Chat retains
  its status/code and identifies the accepted field. Admission, model contracts,
  limits, routing and billing remain unchanged.

## [1.2.70] - 2026-09-08

### Added

- Add an opt-in Qwen complete-output contract and immutable Flash/Max data
  candidates. New Releases accept only `max_completion_tokens > 10`, reserve
  the accepted total and send ten fewer tokens under the supplier's documented
  allowance. Old protocols and active Releases keep their existing behavior.
- Expose accepted/provider limits and stream resource bounds. The new stream
  contract uses supplier final usage with separate finite transport limits,
  instead of comparing UTF-8 bytes with the requested token count.

### Fixed

- Retain final supplier output-limit violations as provider evidence pending
  explicit reconciliation. Atomically record actual streamed delivery, the exact
  lease fence and the terminal hold, including commit-ambiguity readback. No
  automatic charge or Provider retry; interrupted streams retain existing marked
  estimates and frozen pricing. Neither a token limit nor Freeze is a bill cap.

## [1.2.69] - 2026-09-08

### Fixed

- Run development/test rollout observation with the existing pinned ingress
  container's qualified curl, disabling curlrc and retaining strict HTTPS,
  HTTP/1.1 and HTTP/2 response checks. Record client identity, bounded complete
  authentication-error response evidence and connection counts. Historical
  failed or incomplete observer windows remain failed or unproven after resume;
  no Provider retry, fallback, schema or model execution change is introduced.

## [1.2.68] - 2026-09-07

### Fixed

- Persist streamed delivery checkpoints with the same existing customer token
  estimate used by live interruption settlement. Recovery and checkpoint cadence
  no longer interpret the UTF-8 admission bound as customer tokens. Preserve
  Provider-final charge requirements, partial-write semantics, frozen prices and
  historical rows; the separate conservative output guard remains unchanged.

## [1.2.67] - 2026-09-07

### Added

- Describe exact model output and thinking controls separately in model catalogs
  and onboarding: native effort, explicit aliases, provider defaults, independent
  budgets and mutual exclusions. Historical Profile/Definition/Release bytes and
  public limits remain unchanged.

### Fixed

- Project only verified thinking menus and wire controls for OpenClaw's selected
  Chat transport. Preserve provider defaults when controls cannot be represented;
  report those limits and reject stale or conflicting explicit configuration before
  writes. Native max and aliases targeting it are withheld because this client can
  introduce ultra or silently downgrade the wire value.
- Keep client defaults distinct from provider defaults and expose the current
  conservative UTF8 stream guard without claiming a provider token count.

## [1.2.66] - 2026-09-07

### Fixed

- Recover complete ingress database transactions after PostgreSQL serialization
  aborts, with exact command identity and bounded revalidation. Connection loss,
  ambiguous commit results and external operations are never blindly replayed.
- Retain a safe SQLSTATE in ingress owner failures for precise diagnosis.

### Added

- A fixed development/test rollout tool with read-only planning and inspection,
  exact runtime configuration checks, shared ingress locking through backend
  replacement, and durable recovery evidence. Existing ingress, compatibility,
  schema, model activation and settlement authorities remain unchanged.
- Preserve short-request failures and observation gaps through recovery. Healthy
  deployment readback does not establish long-stream or cancellation acceptance.

## [1.2.65] - 2026-09-07

### Added

- Create a previously uncompiled PublicModel from an exact formally published
  Profile Definition through the existing transactional onboarding owner. Reuse
  a connection and key, choose an explicit contract revision in detailed or batch
  onboarding, and retain independent qualification, Release and activation steps.
- Read immutable data bootstrap identities throughout candidate attachment,
  contract browsing and Release authoring. Compiled bootstrap and historical
  idempotency behavior remain stable; missing or mismatched definitions fail
  closed. No new Catalog entry, schema migration or provider parser is required.

## [1.2.64] - 2026-09-07

### Fixed

- Generate bounded data Operation Probes from the installed boolean reasoning
  contract. Qwen candidates now explicitly disable thinking and preserve the
  protocol-owned total output limit; immutable summaries retain the false value.
  Legacy probes and published Definitions are unchanged. Candidate creation,
  actual probe dispatch and persisted evidence are covered together.

## [1.2.63] - 2026-09-07

### Added

- Group model onboarding by platform, expose QianwenAI explicitly, and reuse a
  saved connection and exact credential binding for multiple model configurations.
  Beijing workspace model discovery is optional metadata, with administrator
  scope, bounded responses and no inference, qualification or activation effect.
- Add Qwen3.7-Flash's exact streamed-tools protocol and data Bundle, sharing
  tool assembly with finite Qwen envelope preparation. Probe and Runtime preserve
  tool history, reasoning, terminal usage and failed-delivery accounting boundaries.
  Existing contracts remain unchanged; qualification and Agent completion require
  separate live validation after deployment.

### Fixed

- Retain one configuration idempotency key across uncertain retries within a
  bounded actor-scoped window. Fresh creation returns the exact selected binding;
  replay cannot guess it from current credential rows.

## [1.2.62] - 2026-09-06

### Fixed

- Add exact MiniMax Highspeed data Profile and Probe revisions that allow absent
  stream cache counters without granting cache authority. Nonstream cache
  qualification remains strict; historical contracts and Releases stay unchanged.
- Honor exact data Profile tool-index normalization without falling back to
  legacy model-name dispatch; legacy APIs and streaming indexes remain unchanged.
- Raise the Backend CI deadline from 30 to 35 minutes after a confirmed timeout,
  preserving all tests and required checks.
- Correct reasoning-effort Probe confirmation text to describe the selected exact
  contract, including streamed requests and provider-default effort, instead of
  claiming all probes use nonstreaming low effort. Probe behavior is unchanged.

## [1.2.61] - 2026-09-06

### Fixed
- Add exact Kimi K3 default-effort qualification for streamed tool requests. A new Probe implementation revision and independent one-binding Bundle reuse the retained data Profile and shared wire validator; prior bindings, Releases and evidence remain unchanged.

## [1.2.60] - 2026-09-06

### Fixed

- Add explicit MiniMax Highspeed and M3 reasoning revisions that accept
  self-consistent tool-result history without requiring assistant content replay.
  Historical contracts remain unchanged; no content or tool identity is invented.
- Deliver immutable data Profile successors through the existing Bundle channel,
  with exact Probe composition and runtime implementation requirements. Old
  runtimes fail compatibility checks; qualification and Agent completion remain
  separate evidence, with no schema, pricing or public model identity change.

## [1.2.59] - 2026-09-06

### Fixed

- Add an exact MiniMax M3 streamed-tools implementation that validates and
  normalizes supplier `service_tier` response metadata through the shared Probe
  and Runtime path. Older protocol revisions retain their accepted responses;
  the metadata cannot change request tier, pricing or settlement authority.
- Deliver M3 Profile 1.4 as a separate data Bundle requiring that implementation,
  with no schema migration, Catalog entry or new public model identity. Real
  qualification and Release activation remain separate Target actions.

## [1.2.58] - 2026-09-06

### Fixed

- Keep data Release public context/output ceilings distinct from Deployment
  request defaults. Authoring exposes explicit limits, prefills only a stable
  Release's existing bounds, and preserves idempotency across unchanged retries.
  Legacy and initial baselines require explicit data limits; the existing server
  remains the final Profile and baseline validator.
- Support frontend-only development builds with the existing exact-source,
  immutable-image and storage gates, without rebuilding or relabeling a backend.

## [1.2.57] - 2026-09-06

### Fixed

- Preserve bounded, safe diagnostics when a nonstream capability Probe cannot
  normalize a successful HTTP response: closed failure stage, captured byte
  count, completeness and SHA-256. Failed normalization no longer leaves an
  unexplained default zero byte count. Response text, credentials and arbitrary
  parser errors are excluded; usage and qualification authority stay unchanged.
- Keep adapter error messages and unwrap semantics compatible. Existing Probe
  summaries, contracts, approvals, schema and sealed Releases remain unchanged.

## [1.2.56] - 2026-09-05

### Fixed

- Admit the existing immutable Profile digest, public limits and capability
  Approval IDs at the Release HTTP boundary. Valid data Release requests now
  reach the unchanged atomic authoring owner; strict unknown/duplicate fields,
  qualification freshness and immutable evidence checks remain enforced.
- Exercise HTTP authoring with a stored data revision and a real Probe/Approval
  decision, including sealed pins, idempotent replay and stale rejection with
  no new Release or receipt. No schema or Provider protocol change.

## [1.2.55] - 2026-09-05

### Changed

- Added exact typed streamed Function Tools implementations for existing
  DeepSeek, Kimi, MiniMax M3 and M2 Highspeed data revisions. Historical Profile,
  Protocol and Probe addresses remain unchanged; installing these implementations
  creates no model definition, qualification, Release activation or schema change.
- Reused shared SSE/tool assembly with finite DeepSeek/Kimi envelope preparation.
  Kimi retains terminal-choice usage and one consistent usage-only repeat while
  rejecting counter conflicts; DeepSeek/Kimi require DONE and MiniMax retains its
  approved EOF policy. Invalid delivery preserves safe supplier usage evidence.
- Data Probe construction, summaries and response normalization use immutable
  definitions and installed typed implementations, including MiniMax cache
  evidence and Highspeed child-index normalization. OpenAPI implementation enums
  include installed data implementations without compiling model revisions.

## [1.2.54] - 2026-09-05

### Fixed

- Capability qualification now reads the latest Probe against the candidate
  Operation's immutable data Profile, so successful or failed evidence remains
  visible when the stable model retains its legacy Profile. Existing revision,
  selector and credential freshness checks and independent Approval remain intact.
- Probe/Approval confirmation uses the same exact execution Profile and Definition
  identity as the Operation. Missing data identity blocks confirmation; a changed
  digest invalidates pending confirmation. No new schema, evidence replay or
  Provider call is needed to recover a previously persisted Probe.

## [1.2.53] - 2026-09-05

### Changed

- Added the explicit M113/M114 compatibility bridge and independent append-only
  capability publication ledger. Startup never applies M114 implicitly. Reviewed
  CI Bundle files use one delta materializer with exact Target/Artifact identity,
  real database projection digests and read-only ambiguous-commit recovery.
- Added immutable data Profile revisions, candidate Deployments on the same
  stable model and Connection, and exact capability/evidence pins on sealed
  Releases. Request derivation, planning, dispatch and response mechanics retain
  the selected definition; mutable safety state can still reject dispatch.
- Added registry-derived runtime support, self-fencing leases and coordinated
  ingress intent. Slot activation and generation join check compatible potential
  receivers under one domain boundary; ambiguous reload retains old receivers.
  Provider attempts identify their server-owned runtime generation. Existing
  billing Freeze owns customer pricing and terminal settlement.
- Exposed published execution revisions and explicit qualification selections in
  the existing model/Release administration flow. Daily data delivery creates
  neither schema migration nor backend image. Target continuity, actual model
  qualification and OpenClaw outcomes remain separate deployment evidence.

## [1.2.52] - 2026-09-05

### Changed

- Added per-process HTTP admission/drain accounting and runtime/schema identity
  in readiness. Accepted streaming handlers retain their contexts and terminal
  dependencies during planned shutdown; timeout cancellation is reported as an
  incomplete drain. Effective drain covers supported request lifetimes.
- Added a directory-mounted Nginx membership carrier and explicit same-schema
  withdrawal helper with exact predecessor/config checks, old-worker observation
  and fail-closed ambiguity handling. It never stops a backend automatically.
  Slow old proxy connections abort rollout rather than being forcibly closed.
- Aligned default Compose stop grace with application drain. Initial carrier
  installation remains a bootstrap boundary. No schema or Publication change;
  source tests do not by themselves establish Target continuity or Agent success.

## [1.2.51] - 2026-09-04

### Changed

- Appended exact streamed Function Tools contracts for GLM-5.3/5.2/5.1/5-Turbo/
  5/5V-Turbo, preserving model, endpoint, parameter and media boundaries.
  M113 supplies one compatible static declaration with 30 independent bindings,
  following M111 publication and ordinary M112 schema migration.
- Shared bounded stream/tool assembly is selected by the exact contract and
  frozen upstream identity. GLM reasoning_content, terminal choice/usage and
  DONE remain separate from M111 embedded thinking and complete clean EOF.
  Frozen request tool constraints apply before identity disclosure.
- Constructed cross-client/Probe/Runtime regressions do not imply Provider
  qualification, Target apply, rollout, useful-task success or Production.

## [1.2.50] - 2026-09-04

### Changed

- Integrated the previously reviewed M111 MiniMax M2.7 PAYG Profile 1.3
  streaming Function Tools candidate: bounded shared tool-delta assembly,
  exact auto/thinking/history/usage rules, and a static append-only Publication.
  Existing identities and their non-stream acceptance remain unchanged.
- M112 extends the existing successor command with an exact primary-Alias
  before-state. Qualified revisions of the same verified model product may
  advance repeatedly; Slot activation, registry, Alias, model-set delta and
  immutable receipts commit together. Ordinary Alias writers remain fenced.
  Historical replay does not revert the current target; down refuses new
  durable receipts. No new table, parallel lifecycle or Agent SDK is introduced.
- Source and local test evidence do not imply Publication apply, Target rollout,
  real Agent task success, Provider charges or Production readiness. The GLM
  stream-tools path remains a separate next composition, not a claimed result.

## [1.2.49] - 2026-09-04

### Changed

- Added optional caller-authorized Chat preflight using canonical Profile,
  capability and route preparation plus the same credential-free outbound
  shaping and context checks as execution. It never dispatches, decrypts,
  reserves execution quota/funds, claims a lease or creates execution evidence.
  Its safe snapshot explicitly separates unchecked live/response/Agent behavior.
- Client metadata projects sealed stable Release limits and the existing
  Profile request default separately. OpenClaw sync preserves valid user
  overrides and experimental ceilings, rejects conflicting or unbound defaults,
  and reports non-secret parameter provenance before an optional config write.
- No Profile, Probe, Publication, schema ceiling, credential, Pricing or
  Production change. Fourteen-client local request regressions and no-network
  installed-client checks do not claim completed real Agent tasks. The separate
  unpublished M111 stream-tools candidate is not included in this source batch.

## [1.2.48] - 2026-09-04

### Fixed

- Developer Console recent calls display the actual requested model, falling
  back to the retained model only for historical records without that field.
  Request lookup reserves the separately labeled canonical route identity for
  existing organization/platform administrator views; audit records are unchanged.
- Extended the existing reuse regression through exact Profile admission and
  outbound preparation for current MiniMax/GLM Function Tools Probe and Console
  requests. Omitted controls remain omitted, explicit auto is not silently
  rewritten, and existing role-specific response validators remain independent.
- No parser extraction, Profile/Probe identity, Publication, migration, routing,
  credential, customer pricing or Production change. Prior UI successes retain
  their original artifact identities; this source version is not deployment or
  supplier-budget evidence.

## [1.2.47] - 2026-09-04

### Fixed

- Reused the shared non-stream child tool-call index normalizer for the exact
  MiniMax M2.7 Highspeed PAYG Profile 1.2 Chat tuple. Only non-null lexical
  non-negative integer indexes are removed; malformed values retain valid
  Provider usage but remain unavailable for delivery or qualification.
- Closed the typed Highspeed Probe's residual-index validation gap. Runtime
  still rejects unknown siblings, and duplicate JSON fails before usage parsing.
  Historical Profiles, exported legacy scopes and streaming indexes are unchanged.
- No new Profile, Probe identity, Publication, migration, route, credential or
  Production authority is introduced. Local fixtures do not establish live UI
  success; the existing failed Highspeed request remains retained evidence.

## [1.2.46] - 2026-09-04

### Fixed

- Added an explicit Provider-default Function Tool scenario to Developer
  Console. It sends the existing fixed function definition without `tool_choice`,
  so contracts that express automatic choice by omission are usable without
  changing explicit auto/required/named requests or guessing from model names.
- The server's exact Profile remains the request-validation authority. No
  Profile, Probe, Publication, migration, routing or credential contract changed;
  this metadata update does not claim a Release or Production deployment.
- Corrected an existing stream-health test's timing assumption: half-open lease
  renewal is valid while waiting for response headers. The regression now
  exercises that ordering explicitly while retaining terminal health checks;
  production execution code is unchanged.

## [1.2.45] - 2026-09-04

### Fixed

- Exposed the already-published `cache_read_usage` and `prompt_cache` typed
  qualification workflows in the model execution target UI. Probe and approval
  remain separate server-owned actions; no provider/model allowlist or new
  qualification authority is introduced in the frontend.
- Displayed observed cache-read counters without converting absent evidence to
  zero or interpreting a zero counter as a cache hit.
- Removed only the documented non-authoritative GLM `request_id` response
  metadata through shared adapter preparation for six exact current Chat
  tuples. Malformed metadata still withholds delivery while preserving valid
  Provider usage; unknown siblings and historical/generic tuples are unchanged.

### Not included

- No new Profile, protocol, Probe identity, Publication, migration, route,
  credential, administrator-account or Production authority is introduced.

## [1.2.44] - 2026-09-04

### Fixed

- Bound startup-only pending capability approval to the selected sealed Release,
  its exact revisions and Profile/Protocol identity, and the canonical operation
  catalog path. Established Chat Releases may keep their intentionally empty
  public-execution path while the immutable capability Probe binding remains
  exact for `/v1/chat/completions`.
- Treated a predecessor projection's inherited `production_approved` text as
  pending when no current exact capability qualification exists. It cannot grant
  ordinary routing authority; the request planner remains fail-closed until the
  current Probe and approval are recorded.

### Not included

- No migration, Profile, Probe, Publication, Release, credential, endpoint,
  fallback, administrator-account or Production contract changes are introduced.

## [1.2.43] - 2026-09-03

### Fixed

- Kept the startup route-authority gate fail-closed while allowing a sealed
  release to start when its exact published capability contract and enabled
  deployment projection are intact and only the immutable routing approval is
  still pending. This lets the control plane perform the formal Probe and
  approval steps without making the unqualified route customer-routable.
- Continued to reject missing or disabled projections, contract/product
  mismatches and malformed production evidence as structural startup failures.
  The ordinary request planner still returns no route until the exact
  capability qualification has been approved.

### Not included

- No migration, Profile, Probe, Publication, Release, credential, fallback,
  administrator-account or Production contract changes are introduced.

## [1.2.42] - 2026-09-03

### Changed

- Added M110 as one append-only Capability Publication for the exact MiniMax
  PAYG M3/M2.7/M2.7-highspeed and GLM 5.3/5.2/5.1/5-Turbo/5/5V-Turbo Chat
  runtime capability compositions required by canonical Function Tools calls.
- Advanced the existing provider-family Function Tools Probe contracts to v2
  for the current exact Profiles. The shared validator proves the tool call,
  provider-specific reasoning evidence and usage while retaining separate
  provider/model identities.
- Added MiniMax cache qualification that distinguishes an explicitly reported
  zero cache-read counter from a missing counter and requires a real positive
  cache hit only for `prompt_cache`. Sanitized probe evidence records counter
  presence and value without storing provider response content.
- Preserved fail-closed routing: M110 creates static contracts only; every exact
  capability still requires its own current Probe and routing approval before a
  route can satisfy the composed invocation.

### Not included

- No Token Plan, multimodal, pricing, Production, Alias, credential, fallback
  or administrator-account changes are introduced.

## [1.2.41] - 2026-09-03

### Changed

- Added the missing Canonical Release Workspace action for the existing M109
  never-activated staging successor contract. The UI restricts predecessor
  choices to the same exact upstream model and operation, reads the complete
  model-set before-state immediately before submission and uses the formal
  idempotent atomic cutover endpoint.
- Preserved the M109 Release, registry, Alias, model-set and receipt contracts;
  this change adds no Profile, Probe, Publication, migration or routing
  authority and does not modify administrator identities or credentials.

## [1.2.40] - 2026-09-03

### Changed

- Added the append-only MiniMax PAYG cache-usage contract v2 and Profile 1.2
  successors. Missing `cached_tokens` is accepted only for provider inputs below
  the documented 512-token cache threshold; explicit null, invalid counters and
  missing counters at or above the threshold remain fail-closed.
- Added M108 as one compatible Publication for the three exact MiniMax PAYG
  Function Tools Profile successors while preserving M104/M106 bindings and
  all prior qualification evidence.
- Added an independently versioned, atomic cutover for never-activated staging
  canonical models. It requires exact zero Slot, authoritative Release, active
  Alias and prior successor-receipt evidence before replacing the staging
  identity with its first immutable successor.
- Preserved the existing published-predecessor successor receipt, idempotency
  scope and response shape; the staging command uses lifecycle receipt v3,
  child receipt v2 and a separate operation scope.

### Not included

- No Token Plan, multimodal, pricing, Production, global Alias precedence or
  provider fallback changes are introduced.

## [1.2.39] - 2026-09-03

### Changed

- Added a fail-closed first-successor cutover for stable public model request
  identities. The exact command activates the successor's sealed initial
  Release, retires and de-lists the predecessor, creates one immutable primary
  Alias, rewrites affected model-set entries and records complete child
  evidence in one PostgreSQL transaction.
- Added authorization-aware request-identity projection for model Catalog and
  `/v1/models`. Ordinary users receive the stable request name while
  administrators retain the immutable canonical successor identity for audit.
  Direct ordinary execution of a canonical successor protected by a primary
  Alias is rejected.
- Added database guards for primary Alias and completed predecessor/successor
  immutability, deterministic replay and ambiguous-commit recovery, plus a
  guarded M107 downgrade that refuses to discard durable successor authority.
- Updated Developer Console, API Docs and readiness selectors to consume the
  server-owned request identity without exposing the dated canonical successor
  as a second ordinary model.

### Scope

- M107 is a model-governance and Runtime lifecycle schema change, not a new
  Capability Publication. It reuses the existing GLM-5.2 Profile, Probe and
  applied M105 binding. This source batch does not create the GLM successor,
  activate a Release or route, call a Provider, deploy to Target or claim
  Production readiness.

## [1.2.38] - 2026-09-02

### Changed

- Bound the GLM-5-Turbo canonical connectivity Probe to its exact
  `glm.5-turbo.api@1.0.0` reasoning contract and explicitly disables the
  provider-default thinking mode. A Target differential request proved the
  otherwise identical bounded request succeeds with this control after two
  default-thinking requests reached the existing 15-second timeout.
- Added a safe canonical Probe request summary containing the exact execution
  Profile plus fixed `thinking`, output-limit and streaming controls, without
  persisting prompts, response bodies or credential material.

### Scope

- This change does not alter model, protocol, Profile, Publication, Release,
  Pricing or route identity and does not relax the canonical success criteria
  or timeout. It records a source candidate only; Target rollout and re-Probe
  remain separate evidence.

## [1.2.37] - 2026-09-02

### Changed

- Added an append-only MiniMax China PAYG endpoint identity for the current
  official `https://api.minimax.cn/v1` OpenAI-compatible Chat root while
  preserving the historical `api.minimaxi.com/v1` class and all M104 evidence.
- Advanced the three MiniMax PAYG Profiles to current version 1.1.0 without
  changing model, protocol, function-tools, reasoning, cache or usage
  semantics; the 1.0.0 Profiles remain addressable but cannot receive new
  attachments.
- Added deterministic M106 Publication V1 artifacts after M105 for the three
  exact 1.1.0 Function Tools bindings. Existing M104 and M105 declarations are
  not rewritten or replayed.
- Updated the MiniMax onboarding preset and exact validation to use the new
  endpoint identity, with control-plane, Probe and Gateway regression coverage.

### Scope

- This metadata records a Local source candidate only. M104, M105 and M106 are
  not applied by this change, and no Provider call, Target rollout, capability
  approval, model Release, tag, pricing, traffic or Production state is created
  or changed. Token Plan remains historical and outside this batch.

## [1.2.36] - 2026-09-02

### Changed

- Synchronized the architecture and codebase fact maps with the executable
  `000105_glm_toggle_function_tools_qualification` schema ceiling and the
  existing M105 GLM toggle-family source candidate.
- Synchronized the current-status and model-capability status projections with
  version 1.2.36/M105, together with the release-gate-required frontend version
  mirrors. The M105 code, migration and Publication artifacts are unchanged.

### Scope

- This is a repository fact-source and release-metadata correction only. The
  development/test Target remains exact 1.2.33/M103. No M104 or M105 apply,
  Provider call, capability qualification, artifact, rollout, Release object,
  tag, pricing, traffic or Production state is created or changed.

## [1.2.35] - 2026-09-02

### Changed

- Added append-only exact official-open-platform Chat Profiles for GLM-5.1,
  GLM-5-Turbo and GLM-5, and advanced GLM-5.2 and GLM-5V-Turbo without
  reinterpreting their historical Profile versions.
- Added one immutable toggle-family Function Tools Probe identity over the
  existing shared OpenAI-compatible request-builder and normalized validator.
  Each of the five model tuples retains exact reported-model checks, Profile,
  protocol, reasoning, evidence and approval identity.
- Added immutable GLM protocol successors that explicitly model Function Tools
  for the new bindings while leaving the historical GLM-5.3 and GLM-5V-Turbo
  protocol identities unchanged.
- Added deterministic M105 Publication V1 declaration and generated migration
  artifacts with M104 as the exact predecessor and five guarded bindings.
  GLM-5V-Turbo's binding qualifies only pure-text Function Tools; vision,
  video, files and media-plus-tools require independent capability evidence.
- Kept GLM-5.3-Flash outside M105 because its multimodal operation and
  Function Tools combination is not yet frozen as an exact executable
  UniRouter contract.

### Scope

- This metadata records a Local source candidate only. M104 and M105 are not
  applied and no Provider call, Target rollout, capability approval, Release,
  pricing activation, tag, traffic or Production state is created or changed.

## [1.2.34] - 2026-09-02

### Changed

- Added append-only MiniMax PAYG Profiles for M3, M2.7 and M2.7 Highspeed on
  the exact official `api.minimaxi.com/v1` endpoint class. Each Profile freezes
  its upstream model, limits, reasoning/cache semantics and explicit Chat
  operation identity; historical Token Plan Profiles remain addressable but
  are parked for new attachments.
- Added PAYG-specific Function Tools Probe identities while retaining one
  shared validator/mechanics path. M3 has its own exact contract; M2.7 and
  M2.7 Highspeed share a parameterized contract with separate exact bindings,
  reported-model checks, evidence and approvals.
- Added deterministic M104 Publication V1 artifacts with M103 as the exact
  predecessor and three guarded PAYG bindings. M103 and its historical
  MiniMax/GLM evidence are not rewritten.
- Corrected the GLM onboarding preset to select the existing exact
  `glm_official_openai_v4` endpoint class instead of the generic HTTPS class.

### Scope

- This metadata records a source candidate only. M104 is not applied and no
  Provider call, Target rollout, capability approval, Release, pricing
  activation, tag, traffic or Production state is created or changed.

## [1.2.33] - 2026-09-01

### Changed

- Added append-only `glm.5.3.api@1.8.0` for the ordinary GLM open platform at
  `https://open.bigmodel.cn/api/paas/v4`, using `base_relative` paths. Coding
  Plan and generic HTTPS endpoints remain outside this exact identity.
- Added separate MiniMax M3 and GLM 5.3 Function Tools Probe contracts over one
  shared request-builder and adapter-normalized validator path. Exact output
  limit, reasoning, tool-choice and MiniMax response-replay semantics remain
  provider-specific and fail closed.
- Added deterministic M103 Publication V1 artifacts for one compatible logical
  plan containing the two exact bindings. The plan shares an M102 predecessor
  and rollback boundary without collapsing provider, Profile, protocol or
  Probe identities.

### Scope

- This metadata records a source batch only. M103 is not applied and no live
  Probe, Approval, qualification, Release object, tag, artifact, Provider call,
  Target deployment, route, traffic or Production state is created or changed.

## [1.2.32] - 2026-09-01

### Changed

- Converged the exact DeepSeek V4 Flash and Pro OpenAI-compatible Chat v4
  operation paths on one binding-selected lifecycle engine shared by Runtime
  and capability Probe. Runtime retains its existing single-data-event,
  first-`[DONE]` and clean-EOF compatibility policy; immutable Probe v1/v2
  contracts retain their separate usage shapes, model/content assertions and
  whole-body diagnostics.
- Made the production DeepSeek streaming Probe consume the complete compiled
  capability binding, while preserving Runtime-owned cache hit/miss usage,
  guardrail, billing and delivery authority. The immutable generic DeepSeek
  Flash attachment, historical DeepSeek paths and current Kimi identities and
  behavior remain unchanged.

### Scope

- This metadata records a source batch only. It creates or updates no actual
  Release object, tag, artifact, Publication, current pointer, Profile,
  protocol, Probe contract, migration, route, Target deployment, Provider call
  or Production state.

## [1.2.31] - 2026-09-01

### Changed

- Converged Kimi K2.6, K2.7 Code and K3 OpenAI-compatible Chat streaming on
  bounded SSE framing, current-Kimi usage comparison, terminal `[DONE]`
  handling and exact operation-binding selection. Runtime remains delivery,
  guardrail, backpressure, billing and receipt authority; capability Probes
  retain their existing typed-model, sentinel, reasoning and diagnostic
  authority.
- Preserved historical Profile and non-Kimi streaming behavior. Kimi streaming
  tools remain unsupported, and malformed framing plus incompatible usage or
  finish-reason shapes continue to fail closed. Runtime stops at the first
  valid `[DONE]` without disclosing later provider bytes, while Probe retains
  its bounded whole-body trailing-data diagnostics.

### Scope

- This change creates or updates no actual Release object, external release
  status, tag, artifact, current pointer, Release binding, promotion or
  lifecycle mechanism. It changes no Profile, Protocol, Probe, schema,
  migration, pricing, Alias or canonical-route identity and provides no
  Target, deployment, Provider-integration or Production evidence.

## [1.2.30] - 2026-09-01

### Changed

- Converged the exact Kimi K2.6, K2.7 Code and K3 non-stream Chat runtime and
  capability-Probe paths on one bounded document-ambiguity and child
  `tool_calls[].index` preparation seam. Exact Profile, operation, protocol ID
  and immutable operation-binding version select the existing mechanics; no
  provider-family inheritance or new policy/version authority was added.
- Preserved published Probe-contract acceptance sets and customer-delivery
  fail-closed behavior. Duplicate or malformed JSON still fails before usage;
  a syntactically valid but structurally undeliverable tool tree retains
  validated provider token usage for the existing evidence hold and remains
  rejected by the canonical delivery guardrail.
- Carried the exact operation protocol ID/version through canonical routing and
  reused a `UseNumber` object decoded from the final normalized response body,
  while retaining the existing fallback decoder for all unaffected adapters.

### Scope

- This release changes no schema, migration, Profile, protocol, Probe contract,
  qualification, Release, Alias, canonical route, pricing or billing authority.
  Streaming, provider characterization and MiniMax/GLM promotion remain outside
  this batch.

## [1.2.29] - 2026-08-31

### Documentation

- Recorded the exact Kimi K3 Chat Core development/test outcome: M102
  Publication V1, the later-created exact Deployment, eight current capability
  qualifications, the development/test-only stable test route, bounded
  fixed-SDK non-stream/tools/JSON/cache evidence and temporary access cleanup.
- Kept capability qualification separate from public SDK delivery. The typed
  SSE Probe is qualified, while two separately counted public SDK streaming
  requests returned provider HTTP 503 and remain an explicit external-delivery
  gap rather than being reported as success.
- Recorded all 20 authorized supplier calls, `max_retries=0`, the zero customer
  wallet/transaction result and the absence of supplier invoice or
  `SupplierCostObservation`. CNY 5 remains only the approved spend ceiling.

### Evidence boundary

- This is a documentation and release-metadata correction only. It changes no
  runtime contract, M102 data, Profile, Probe, Approval, Release, route,
  pricing, billing, credential or Target deployment. Development/test remains
  exact 1.2.28/M102; its stable-slot activation is not `released`, Production,
  Alias or global canonical authority.

## [1.2.28] - 2026-08-31

### Fixed

- Routed the Kimi K3 `reasoning_effort` Probe v1 through the existing bounded
  detailed-usage decoder after live Probe receipt 67 returned the already
  modeled `completion_tokens_details` member. The contract continues to accept
  only the explicit cache/reasoning counters and rejects unknown siblings,
  non-integer counters and inconsistent totals.
- Preserved Kimi reasoning Probe v1 historical semantics and all K3 Profile,
  protocol, M102 and qualification identities. Failed Probe receipt 67 remains
  immutable; this source change does not retry or approve any capability.

## [1.2.27] - 2026-08-31

### Fixed

- Extended the existing strict non-streaming `tool_calls[].index` normalizer to
  the exact Kimi K3 Chat v5 tuple after live Probe receipt 63 showed that K3
  emits the same optional, non-authoritative child index as K2.6/K2.7. The
  normalizer still rejects duplicate members and invalid index types, removes
  only that reviewed path, preserves `choices[].index`, and leaves unknown
  siblings for the canonical delivery guardrail.
- Kept Profile 1.9, protocol K3 v5, Probe Contract v4, M102, routing and billing
  identities unchanged. Failed Probe receipts 62/63 remain immutable; no
  capability is approved or retried by this source change.

## [1.2.26] - 2026-08-31

### Fixed

- Bound Kimi K3 typed streaming, tool and JSON probes to the already modeled
  `reasoning_effort=low` control and raised their bounded completion budget to
  512 tokens, preventing always-on default-max reasoning from exhausting the
  earlier 64/256-token probe budget before the asserted response contract.
- Raised only the provider-default K3 reasoning probe budget to 4,096 tokens;
  it still omits `reasoning_effort`, while the independent effort probe remains
  the sole representative `low` evidence. K2.6 and K2.7 probe requests are
  unchanged, and no Probe contract identity, migration, Release or pricing
  authority is advanced.

## [1.2.25] - 2026-08-31

### Added

- Added append-only `kimi.k3.api@1.9.0`, preserving Profile 1.8 semantics while
  adding the exact `reasoning_effort` execution contract and immutable Kimi K3
  Chat v5 operation binding. Profile 1.8 remains historically addressable but
  is no longer eligible for new attachment.
- Generated M102 through Strict Capability Contract Publication V1. It appends
  only eight exact K3 Profile 1.9 Chat Core bindings and selectors: streaming,
  reasoning, reasoning effort, tools, function tools, automatic/required tool
  choice and JSON Object.
- Added server-owned K3 probes that preserve always-on reasoning by omitting
  the K2.x `thinking` toggle, use `reasoning_effort=low` for the effort probe
  and require exact `tool_choice=required` for that capability.

### Evidence boundary

- M102 creates no Probe, Approval, qualification, Release, Alias, pricing,
  billing or canonical route. Dynamic Tools, JSON Schema, search and media are
  not projected by this Core publication and remain fail closed without their
  own route evidence.
- This source candidate exists only in local candidate commits on the K3
  branch; it has not been pushed, deployed or exercised against Kimi.
  Development/test remains exact 1.2.23/M101; Target publication, deployment
  and bounded provider validation require separate authorization.

## [1.2.24] - 2026-08-31

### Documentation

- Recorded the exact Kimi K2.7 Code Chat Core development/test closeout:
  Publication V1 M101 apply, later-created Deployment projections, six
  independent capability qualifications, fixed-SDK text/stream/tools/
  multi-turn/JSON/cache evidence, the 17-of-20 bounded-call total and temporary
  access cleanup.
- Distinguished the `project_default` Grant from its K2.7-only credential and
  kept the CNY 5 authorization ceiling separate from absent supplier invoice or
  SupplierCostObservation authority.

### Evidence boundary

- This is a documentation and release-metadata correction only. It changes no
  runtime contract, schema, Profile, Probe, Approval, Release, Alias, pricing,
  billing, credential, route, provider traffic or Target deployment. The
  development/test Target remains exact 1.2.23/M101.

## [1.2.23] - 2026-08-31

### Added

- Published the exact `kimi.k2.7-code.api@1.3.0 ×
  protocol.kimi-openai-chat.v5@1.0.0` Chat Core declaration through Strict
  Capability Contract Publication V1. M101 appends six Profile-scoped bindings
  and selectors without a generic tuple, Profile change, Release, Alias,
  pricing activation or routing qualification.
- Added K2.7 required-thinking capability probes, the exact K2.7 non-stream
  tool-call index normalization scope, strict SSE v4 reasoning/usage
  validation and an all-or-nothing local migration artifact generator.

### Evidence boundary

- M101 is static publication authority only. It creates no Probe, Approval,
  Release, price, customer charge or canonical route. Target deployment and
  bounded Kimi provider validation remain separate post-CI evidence.
- K2.7 Dynamic Tools, built-in search, media, highspeed/Coding subscription
  identity, required/named/parallel tool choice and function-tool streaming
  remain outside this Chat Core batch.

## [1.2.22] - 2026-08-31

### Added

- Added Strict Capability Contract Publication V1: deterministic declaration-
  owned migration generation, exact Target planning, explicit install/apply/
  guarded-recovery commands and read-only startup compatibility verification.
- Bound every publication to the exact artifact, migration bytes, ledger
  predecessor, compiled declaration, immutable Plan and declared deployment-
  projection transitions. Publication up/down now rejects incomplete writer
  hooks, forged projection predecessors and declaration/migration inventory
  drift before any authorized write.

### Evidence boundary

- This source candidate adds no schema migration and performs no Target apply,
  deployment, K2.7/K3 operation, provider call, credential, billing, Release,
  Alias, canonical or Production change. The development/test Target remains
  the exact 1.2.20/M100 closeout recorded in the current-status documents.
- Local Go/vet checks and exact-commit independent reviews passed; required
  PostgreSQL, frontend, Docker and full routed evidence are delegated to PR CI.

## [1.2.21] - 2026-08-30

### Documentation

- Recorded the exact Kimi K2.6 Chat Core development/test closeout: deployed
  1.2.20/M100 artifact identity, six independent capability qualifications,
  fixed OpenAI SDK delivery and cache-read evidence, the 18-of-20 bounded-call
  total, zero customer wallet movement, and supported test-access cleanup.
- Removed the duplicated stale Kimi active-batch snapshot so the current-status,
  capability-status and system-fact entrypoints agree.

### Evidence boundary

- This is a documentation and release-metadata correction only. It changes no
  runtime contract, schema, Profile, Probe, Approval, Release, Alias, pricing,
  billing, credential, route, provider traffic or Target deployment. The
  development/test Target remains the exact 1.2.20/M100 artifact recorded in
  the status documents.

## [1.2.20] - 2026-08-30

### Fixed

- Normalized the optional non-stream
  `choices[].message.tool_calls[].index` wire member for the exact Kimi K2.6
  Chat v5 tuple using the same bounded normalizer as public runtime delivery.
  Only a lexical non-negative JSON integer is accepted; streaming indexes and
  every unknown sibling remain unchanged and fail closed under their existing
  contracts. Duplicate JSON members, including escaped-equivalent names, are
  rejected before usage or delivery maps can apply last-wins semantics.
- Published Kimi Function Tools Probe Contract v4 for only tools, function
  tools and automatic tool choice, while retaining the failed v3 Probe as
  immutable historical evidence.

### Evidence boundary

- M100 is a control-data publication plus shared decoder correction. It does
  not change the Execution Profile, Chat protocol, Release, alias, pricing,
  Planner, UI, customer billing or canonical traffic and creates no live Probe
  or Approval evidence by itself.

## [1.2.19] - 2026-08-30

### Fixed

- Kept Kimi capability Probe `contract_error_code` values stable and moved a
  syntax-constrained unknown JSON member identifier into the separately bounded
  `diagnostic_member` evidence field. Provider-controlled member names no
  longer create dynamic error-code identities.
- Added the repository-level model-adaptation learning closeout pointer so an
  independently shipped Delivery Batch cannot rely on long-chat memory for its
  required learning-file inspection and report.

### Evidence boundary

- The diagnostic member remains provider-controlled, bounded to 64 ASCII
  identifier characters and non-authoritative for capability, usage, billing,
  routing or release. The member value and complete raw response body are not
  returned.
- This patch does not change the Kimi Probe acceptance set, migration ceiling,
  Profile, protocol, Probe contract, Release, alias, pricing, routing, billing
  or canonical traffic and creates no live evidence by itself.

## [1.2.18] - 2026-08-30

### Fixed

- Added a bounded, schema-only diagnostic projection for failed Kimi capability
  Probes. It can identify a syntax-constrained unknown member name or the decode
  category, but never persists the member value or complete raw response body.
- Kept the Probe acceptance set and qualification decision unchanged: the
  diagnostic makes an immutable failure actionable and cannot turn it into
  successful evidence.

### Evidence boundary

- Development/test M99 Probe 50 remains immutable, failed and unapproved. This
  patch changes no migration, Profile, protocol, Probe contract, Release,
  alias, pricing, routing, billing, UI or canonical traffic, and creates no
  live or release evidence by itself.

## [1.2.17] - 2026-08-30

### Fixed

- Aligned the exact Kimi K2.6 Function Tools capability Probe with the public
  Chat delivery boundary for bounded numeric provider usage extensions and the
  already modeled nullable response members. Only modeled core, cache and
  reasoning counters remain qualification authority; unknown numeric counters
  are never promoted into usage or billing authority.
- Preserved the immutable v1/v2 acceptance sets, strict tool identity and
  arguments, exact finish semantics, duplicate-member rejection and fail-closed
  handling of every response member outside the existing delivery boundary.

### Evidence boundary

- M99 advances only the exact Kimi K2.6 tools, function-tools and automatic
  tool-choice bindings to Function Tools Probe Contract v3. It does not change
  the Execution Profile, Chat protocol, Release, alias, pricing, Planner, UI or
  canonical traffic, and it creates no live Probe or Approval evidence by
  itself. Failed Probe 49 remains immutable and unapproved.

## [1.2.16] - 2026-08-30

### Fixed

- Accepted Kimi K2.6's bounded numeric non-stream usage details for prompt
  cache and completion reasoning while preserving top-level cache authority,
  exact token totals and strict rejection of every other unknown usage member.
- Published non-stream Probe Contract v2 for reasoning, tools, function tools,
  automatic tool choice and JSON Object so the live failed reasoning v1 Probe
  remains immutable and stale rather than being reinterpreted.

### Evidence boundary

- M98 advances only the five exact Kimi K2.6 non-streaming Probe contracts. It
  does not change streaming v3, the Execution Profile, Chat protocol, Release,
  alias, pricing, Planner, UI or canonical traffic, and it creates no live
  Probe or Approval evidence by itself.

## [1.2.15] - 2026-08-30

### Fixed

- Accepted Kimi K2.6's bounded choice-level terminal stream usage, including
  numeric cached/reasoning detail counters, while still requiring the standard
  top-level usage frame and identical repeated token totals.
- Published streaming Probe Contract v3 so the live failed v2 Probe remains
  immutable and stale rather than being reinterpreted by the repaired parser.

### Evidence boundary

- M97 advances only the exact Kimi K2.6 streaming Probe contract. It does not
  change the Execution Profile, Chat protocol, Release, alias, pricing,
  routing framework, UI or the other five K2.6 capability contracts, and it
  creates no live Probe or Approval evidence by itself.

## [1.2.14] - 2026-08-29

### Fixed

- Normalized only an empty-string `choices[].delta.reasoning_content` member
  from the exact Kimi K2.6 OpenAI-compatible Chat v5 SSE surface. Runtime
  delivery and the streaming capability Probe now consume the same
  exact-scoped normalizer; non-empty reasoning remains visible, invalid types
  fail closed and unknown siblings remain subject to the existing guardrail.
- Published streaming Probe Contract v2 so the failed live v1 sample remains
  immutable and stale rather than being silently reinterpreted by the new
  software artifact.

### Evidence boundary

- M96 changes only the Kimi K2.6 streaming Probe interpretation contract. It
  does not change the Execution Profile, Chat protocol, Release, alias, pricing
  or the other five K2.6 capability contracts, and it creates no live Probe or
  Approval evidence by itself.

## [1.2.13] - 2026-08-29

### Added

- Added an exact Kimi K2.6 OpenAI-compatible Chat operation identity and six
  typed capability qualification contracts for streaming, provider-default
  reasoning, tools, function tools, automatic tool choice and JSON Object.
- Added strict Kimi probe response validation and cross-contract fixtures that
  show accepted fixtures pass the current Gateway delivery-shape and guard
  parsers. Public UI and fixed-SDK delivery remain separate Target checkpoints.

### Changed

- Scoped immutable Chat operation bindings by exact Execution Profile address,
  preventing the shared Kimi v5 validation contract from implicitly granting
  K2.7 Code onboarding authority during the K2.6 milestone.

### Evidence boundary

- M95 publishes only declared Kimi K2.6 contracts. It does not create live
  Probe or Approval evidence, qualify a route, activate pricing, publish a
  Release, change an alias, or call the provider. K2.7 Code remains outside
  this release's execution authority.

## [1.2.12] - 2026-08-29

### Fixed

- Preserved the backend's `active_workload_sponsorship` diagnosis when an
  administrator attempts to offboard a business Sponsor that still owns an
  active workload authorization. The UI now directs the operator to archive
  the related application or revoke the old authorization; retaining access
  requires a new independently sponsored authorization followed by revocation
  of the old authorization. This replaces the generic delete failure.

### Evidence boundary

- The backend sponsorship invariant and atomic offboarding transaction are
  unchanged. This frontend correction does not reassign a Sponsor, revoke a
  Grant, change routing or create provider traffic.

## [1.2.11] - 2026-08-29

### Fixed

- Required a platform administrator to select an active member of the exact
  customer project as the business Sponsor for a workload-application access
  request; operator identity no longer substitutes for customer authority.
- Exposed the existing grant-bounded `allowed_models` input when issuing an
  application Key. An exact model list narrows the Key while an empty value
  continues to inherit the project's dynamic model scope.

### Evidence boundary

- The backend still revalidates Sponsor, project, Grant and credential scope.
  This UI repair does not promote a model lifecycle, create pricing or aliases,
  widen a Grant, alter routing, or issue provider traffic.

## [1.2.10] - 2026-08-29

### Fixed

- Made exact reviewed Chat operation probes consume the same immutable
  Execution Profile operation-protocol identity used by onboarding, so a
  DeepSeek V4 Pro deployment can produce the operation evidence required by
  Release authoring without weakening fail-closed routing.
- Replaced the frontend DeepSeek Profile/protocol qualification allowlist with
  a server-owned exact capability-probe contract projection. The frontend now
  renders only presentation contracts it understands, while each capability's
  Probe, Approval and qualification remains independently authoritative.
- Stopped labelling every Release authoring conflict as baseline drift; only
  the server's explicit general authoring-conflict code triggers the neutral
  refresh message, while idempotency and other errors preserve their exact
  reason.

### Evidence boundary

- These changes repair administration reachability and evidence projection;
  they do not create capability evidence, qualify a route, publish a Release,
  change pricing or authorize provider traffic. Target proof remains separate.

## [1.2.9] - 2026-08-29

### Fixed

- Exposed the existing DeepSeek V4 Pro Chat Core capability qualification
  workflow in the deployment details UI, matching the M94 exact Profile
  bindings without changing protocol, Profile or qualification authority.

## [1.2.8] - 2026-08-29

### Added

- Added M94's exact DeepSeek V4 Pro Chat Core qualification bindings for the
  current Profile `1.5.0` and protocol v4 tuple. The nine routing capabilities
  retain independent Probe, Approval and qualification evidence; no generic
  tuple, grouped approval authority or additional surface is introduced.

### Fixed

- Moved reviewed Chat attachment protocol selection out of capability
  qualification and into an immutable, model-profile-owned operation protocol
  identity resolver. Invalid or ambiguous typed declarations now fail closed;
  a legitimate absence of typed authority preserves only the independently
  validated server-owned base identity.
- Reused DeepSeek V4's exact response normalizer and Probe grammars for Pro
  without changing the existing canonical Chat response contract or creating a
  model-identity upgrade for an SDK-only tool-call index.

### Evidence boundary

- The resolver and M94 migration are locally conformant after focused unit and
  PostgreSQL integration tests. DeepSeek V4 Pro remains unreleased and is not
  live verified until the exact artifact is deployed, probed, approved and
  exercised through development/test.

## [1.2.7] - 2026-08-28

### Fixed

- Normalized DeepSeek V4 Flash's optional SDK-only function-tool `index` at the
  exact provider/model/Chat boundary, preserving the existing Profile `1.5.0`,
  protocol v4, Release and Alias. Runtime delivery and function-tool Probe v2
  reuse one normalizer; valid lexical non-negative integer indexes are removed
  from the canonical response while unrelated fields remain fail-closed.
- Added M93's ten immutable Probe v2 bindings for the five existing function-tool
  capabilities under Profile `1.5.0`'s two exact Chat protocol identities. Old
  v1 receipts remain historical but no longer qualify current routing.
- Made the Developer Console surface a terminal SSE error and its request and
  billing status after an HTTP 200 stream, instead of displaying the failed
  stream as an empty successful response.

### Evidence boundary

- This UI contract does not create, qualify, release or route a model by itself.
  Those control-plane records and any supplier calls remain separately verified
  on development/test.
- M93's Probe v2/normalizer correction and the terminal-stream UI correction are
  locally conformant only until their exact artifact is deployed, probed,
  approved and exercised through the development/test UI. They do not reinterpret
  v1 evidence or authorize Production.

## [1.2.6] - 2026-08-28

### Fixed

- Kept reviewed onboarding strict about the exact Execution Profile and upstream
  provider model while allowing the administrator to choose a unique, stable
  public model ID. This lets a new immutable deployment replace an older public
  identity through an audited Alias cutover without treating that caller-facing
  ID as a provider model alias.

### Evidence boundary

- This onboarding contract does not create, qualify, release or route a model by
  itself. Those control-plane records and any supplier calls remain separately
  verified on development/test.

## [1.2.5] - 2026-08-28

### Fixed

- Added immutable DeepSeek V4 Flash and Pro Profile `1.5.0` identities that
  preserve the provider-default thinking mode while rejecting explicit
  `tool_choice` values that the active reasoning surface cannot honor. A
  redundant explicit `auto` is normalized to the provider default; named and
  required choices remain available only when thinking is explicitly disabled.
- Added M92 exact capability-contract bindings for DeepSeek V4 Flash Profile
  `1.5.0`, taught canonical onboarding to materialize every migration-approved
  capability for an exact Profile tuple, and advanced startup schema authority
  to M92. Historical Profile, Release, probe and approval evidence remains
  immutable and version scoped.
- Updated the Admin capability workflow and OpenAPI projection for Profile
  `1.5.0`; clean rollback is supported only before any Profile `1.5.0`
  attachment or qualification evidence exists. A retained Flash/Pro model
  attachment, probe or approval prevents destructive downgrade.

### Evidence boundary

- Local request, migration, bootstrap and UI tests do not transfer the existing
  Target Profile `1.4.0` live evidence to `1.5.0`. Deployment, new exact probes,
  routing approvals, ordinary supplier calls and any public-model traffic switch
  remain separately bounded and must be verified on development/test.

## [1.2.4] - 2026-08-28

### Added

- Added exact DeepSeek V4 Flash Chat qualification contracts for reasoning,
  reasoning effort, fixed function tools, auto/named/required tool choice and
  JSON Object. Migrations M90/M91 add only declared support projections;
  immutable typed probe and approval receipts remain required before routing.
- Added one guided Admin capability workflow for the new exact contracts and
  bounded Developer Console scenarios for reasoning, fixed no-side-effect tools
  and JSON Object. Arbitrary provider payloads and private reasoning text are
  not retained by the qualification evidence.

### Fixed

- Preserved terminal usage and finish reason in the streaming Developer Console
  result, and prevented `thinking=disabled` from being sent together with
  `reasoning_effort`.
- Advanced the binary schema authority to M91, extended current-schema rollback
  fixtures, made M90/M91 downgrade fail closed on stable identity drift, and
  updated fresh-customer bootstrap to validate all 20 exact probe bindings and
  18 current contracts.
- Updated the OpenAPI capability qualification contract and negative tests so
  schema, bootstrap, UI and runtime consumers agree on the same finite set.

### Evidence boundary

- Local typed, migration, bootstrap, rollback and UI evidence does not by itself
  prove provider-live qualification, routing approval, customer billing,
  Release or canonical promotion. Paid tests remain separately bounded.

## [1.2.3] - 2026-08-24

### Added

- Added exact versioned onboarding Profiles and official Provider presets for
  the retained catalog: 30 upstream model entries and 33 API-surface identities,
  with exact upstream IDs, explicit operations, provider limits, request defaults,
  bounded protocol subsets and separately recorded documentation evidence.
- Added dated CNY or USD official-price candidates. Flat, reviewable schedules
  can create provider-list drafts; context-, tier-, cache-TTL-, storage- or
  tool-dependent prices remain visible as `manual_review_required` instead of
  being flattened into an inaccurate rate.
- Added migration `000084_official_rate_source_currency_drafts`, allowing USD
  only on official review-source evidence while retaining CNY-only request
  pricing approval, activation, wallet and settlement contracts.
- Added M85 Plan-to-Freeze/attempt snapshot fences, M86 coexisting exact public
  Responses/Messages v1/v2 bindings and the bounded M87 Messages function-tool
  binding. Historical Profile and sealed Release identities remain addressable;
  referenced evidence prevents destructive rollback. These schema changes
  require an explicitly approved non-rolling deployment, not a live promotion.
- Added the bounded M88 capability-qualification authority: append-only typed
  probe and approval receipts, deterministic preferred approval for new Plans
  and cutover, and exact bound-receipt revalidation for in-flight Plans. This
  release exposes only DeepSeek V4 Flash Chat streaming qualification. The
  migration and canonical deployment creator materialize its exact
  `enabled/declared` support projection only when the compiled and database
  contracts agree; they never call the provider or create live, approval,
  route, Release or pricing authority, and never overwrite an existing support
  decision.

### Fixed

- Separated documented provider maximum output from the default output used
  when a request omits an explicit limit, including the DeepSeek V4 384K
  maximum and the reviewed defaults for Kimi, MiniMax and the new catalog.
- Rejected mixed-currency official drafts and guarded M84 rollback when retained
  non-CNY evidence would be lost; no implicit FX conversion was introduced.
- Derived invocation capability requirements from the exact typed Profile in
  the same route snapshot, rather than requiring every supported capability.
  Normalized capability evidence through one timestamp/hash authority and kept
  historical strict public-v1 execution isolated from newer capabilities.
- Rejected public output-limit violations before credentials/Freeze, serialized
  M85 rollback against evidence writers and migration discovery, and corrected
  onboarding/status claims to distinguish local subsets from full adaptation.
- Kept public stream response, terminal-health and delivery ordering consistent
  with the durable attempt ledger; preserved actual rejected/transport status
  and prevented checkpoint estimates from charging provider-final-only contracts.
  Updated M86 attempt/call-log/trace constraints to consume exact public bindings.
- Required explicit GPT surface selection for ambiguous governance aliases;
  populated new public limits from executable maxima without changing existing
  lower limits or mistaking request defaults for provider maxima.
- Kept fresh-install bootstrap compatible with the exact M86/M87 protocol seed
  additions while retaining dirty-state, provenance, schema and Release guards.
- Prevented mutable capability projection rows from authorizing M88-typed
  routes without a current receipt-aware qualification. Equivalent newer
  approvals do not invalidate an already frozen Plan; stable contract drift
  still fails closed, while transient credential cooldown remains a separate
  route-health gate. M88 rollback removes only its untouched declaration
  marker; retained probe, approval or execution evidence blocks destructive
  rollback, and a down/up replay restores only the exact supported tuple.

### Evidence boundary

- B1 retains only the explicitly approved bounded implementation batch; it is
  not completion of every model's provider-documented capabilities. Local
  conformance is capability-specific. Provider-live, canary and release evidence
  remain separate; no paid traffic, deployment or canonical switch is authorized
  by this changelog. USD prices remain review-only drafts, and nonstandard tiers
  cannot debit prepaid customer wallets.

## [1.2.2] - 2026-08-24

### Fixed

- Removed all React `act(...)` and Semi Form validation-race warnings from the
  frontend unit suite by waiting for observable state, real user interactions,
  and file-owned Semi portal cleanup.
- Kept the custom provider template selected after an execution-Profile catalog
  failure by binding initialization to the SideSheet open transition and
  preserving unrelated fields when applying partial Semi Form updates.
- Normalized ANSI control sequences before warning fingerprinting so local and
  CI runs classify the same diagnostics and stack continuations.

### Added

- Added a Vitest stderr fingerprint ratchet: an unknown diagnostic or an
  increase in an approved fingerprint now fails the test command even when all
  assertions pass. Reporter output remains visible.
- Kept expected validation-boundary and explicit failure-path diagnostics in
  exact, independently capped fingerprints so one warning family cannot
  silently replace another.

### Compatibility boundary

- The exact React 18 / Semi UI 2.100.0 `findDOMNode` waiver remains visible and
  capped at ten occurrences. UniRouter-owned calls remain forbidden and React
  19 remains blocked until that third-party warning reaches zero.

## [1.2.1] - 2026-08-24

### Changed

- Enabled the reviewed React Router v7 transition and relative-splat behavior
  in production and shared test routers before the eventual router upgrade.
- Added a React compatibility guard that retains Strict Mode, rejects
  UniRouter-owned `findDOMNode` calls, and blocks React 19 while the exact Semi
  UI dependency waiver remains active.

### Fixed

- Covered absolute deep links, query/hash restoration and catch-all replace
  redirects under the future router behavior, while making affected async
  component tests wait for observable UI state instead of render counts.

### Compatibility boundary

- UniRouter source has no `findDOMNode` call. The visible React 18 warning from
  Semi UI 2.100.0 remains an exact, non-suppressed temporary waiver and must be
  removed before React 19; this release does not claim the upstream warning is
  fixed.

## [1.2.0] - 2026-08-24

### Added

- Reviewed provider/model onboarding templates that compose provider presets
  with an exact versioned Model Execution Profile.
- Safe defaults and constrained choices for public model identity, upstream
  model identity, context/output limits, and operation.
- A visible product version in the administration shell plus a CI guard that
  keeps `VERSION`, frontend package metadata, and this changelog aligned.

### Changed

- Renamed the primary model onboarding UI from the implementation-oriented
  “Canonical Deployment” wording to the user-facing “模型接入”; canonical
  deployment and operation identities remain visible as advanced details.
- Canonical model creation now pins the selected reviewed execution profile;
  omitted profile selection remains explicitly unverified rather than inferred.
- Updated the system fact map and codebase map for the current M83/Profile
  onboarding boundary.

### Fixed

- Kept model-onboarding SideSheets and nested page action groups inside narrow
  mobile viewports without changing their desktop widths.

### Verification scope

- Local backend/frontend contract tests, frontend type/build gates, and a
  bounded browser UI audit are required before this release entry is marked
  externally delivered.

> This is the first repository-governed product SemVer entry. It advances the
> existing deployed `1.1.62-*` runtime line without rewriting that historical
> build identity.
