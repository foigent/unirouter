# UR／XI 正式接入 P1：UniRouter 源码边界与最小合同

日期：2026-09-29。性质：**本地只读源码核对与方案**，不是实现、Target 检查或生产验收。产品决定以《61-UR-XI正式接入与观测工作台-决定及实施-2026-09-29.md》D02、D07–D16、P1 为准。

## 精确基线

- 本任务工作树 `codex/content-policy-defaults-20260928`：HEAD `da347b73c533aa4a861f19699d7fbd63e5a195b8`，tree `217766eda9d906385680a3b39081ffad9a1cf14b`，`VERSION=1.6.1`。审阅时已有四个未跟踪文件 `DELIVERY-M121-HOST-DESIGN.md`、`DELIVERY-REVIEW-INPUT.md`、`DELIVERY-REVIEW-MANIFEST.json`、`STATUS.md`；本次没有修改它们。
- 本地 `origin/main` 为 `2bad61e92a45ae5ce8e4205fdaaa9a6f580327f5`，`VERSION=1.6.2`。相对本任务 HEAD，仅 `CHANGELOG.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`、`internal/sourcefacts/dispatch_outbox_integration_test.go`、`internal/sourcefacts/observation_dispatch.go` 不同。下述其余路径与该本地主线字节相同；实施时仍须以新鲜主线及工作树重核。

## 必留结构化事实与可选正文

“必留”表示不能因关闭**未来正文保存**而丢失其运行、结算、审计或恢复依赖；并非所有表列均永远保留，期限另定。“XI 输出”仅列正式合同能提供的字段，不等于整表复制。

| 事实／实际结构 | UR 依赖与正文边界 | XI 正式供数与缺口 |
| --- | --- | --- |
| 服务器 `request_id`、org/project、API key/credential、operation/protocol、身份/授权快照（`internal/executioncontext/context.go:20-56`；`internal/sourcefacts/contract.go:62-82`） | 认证、归属、执行链与后续源事实定位必留；`Identity.ActualHuman` 可表示未知，不能从 key owner 推造实际人。 | Source facts 的 context/identity profile 有正式边界；XI 要分列凭据、费用归属、实际发起人与执行身份，不以共享 key 代人。 |
| 每次 Provider 尝试、route、状态、确定性、重试授权、上游请求号（`migrations/000029_prd6_provider_attempt_ledger.up.sql:3-38`） | 财务风险与恢复状态机必留；表明不存 prompt、响应正文、凭据、完整 URL。 | 已有 Source observation/attempt profiles；只输经合同投影的模型与尝试事实，不给 XI 直接查账务表。 |
| 用量、成本、计费权威/终态、交易与 pricing version（`migrations/000027_prd6_durable_internal_outbox.up.sql:107-149`；`migrations/000031_prd6_usage_authority.up.sql:3-24`） | 账务核对及恢复必留。Token 数不是 prompt 字符串；估算、缺失与最终值必须区分。 | completed-execution snapshot 的 `usage`/`money`（`internal/completedfeed/contract.go:47-91`）与 cost Source fact 的 economic reference（`internal/sourcefacts/contract.go:37-60`）是正式边界。 |
| 请求跟踪、决策与审计的有界字段（`migrations/000017_prd4_request_trace_events.up.sql:3-23`；`migrations/000001_init_schema.up.sql:209-220`；`internal/gateway/trace.go:241-307`） | 阶段、结果、时间、身份、引用保留；`metadata`、`detail`、`upstream_error` 是需逐 writer 核对的自由文本/JSON，**不能仅因字段名是日志或元数据就豁免正文禁存**。 | 现有 Source facts `payload_capture_mode=metadata_only, raw_payload_present=false`（`internal/sourcefacts/envelope.go:91`、`observation_envelope.go:93`）；但这不证明所有本地诊断 writer 都无正文。D14 的资源/动作/结果由工具或资源来源提供，UR 没看到时保持未知。 |
| 内部 outbox、usage journal、completed feed 快照、Source facts（`migrations/000027_prd6_durable_internal_outbox.up.sql:7-28,107-149`；`migrations/000043_v1_completed_execution_feed.up.sql:44-73`；`migrations/000116_execution_source_facts.up.sql:66-91`） | 幂等分发、财务恢复、前进游标及独立事实保留。outbox `payload`/feed `snapshot`/fact `document` 虽为 JSON/bytes，须由类型边界和 writer 检查而非扩展名认定安全。 | completed feed、Source v2 profile 是正式出口；禁止“复制私有 DB”。Source capability 为 `fixed_profiles`、`forward_only`（`internal/sourcefacts/handler.go:168-199`）。 |
| 四个可选正文观察点：`client_request`、`provider_request`、`provider_response`、`client_response`（`internal/contentcapture/policy.go:102-110`） | `internal/middleware/content_capture.go:11-42` 与 `internal/gateway/content_capture.go:17-57` 捕获网关边界字节。`metadata_only`/`disabled` 形成 `not_captured` manifest；完整模式写入有界文件，不改变执行或财务结果（`internal/sourcefacts/content_capture_service.go:93-185,187-203,239-279`）。四点并不等于用户原始意图、模型内部关注内容或 OpenClaw 工具 I/O。 | `unirouter.interaction-content.v1` manifest 携带请求、观察点、policy 版本、完整性、缺口、引用与 digest（`internal/sourcefacts/content_contract.go:65-101`）；正式内容能力、manifest、body 读取 API（`internal/sourcefacts/content_read_contract.go:13-55`），需 XI 独立保存才满足 D10/正式验收。 |
| 内容策略/对象/流状态（`migrations/000119_execution_content_capture.up.sql:94-187`） | `execution_content_policies` 存期望/生效版本与配额；`execution_content_manifests` 存来源描述；`execution_content_objects` 映射正文文件；stream、scope、binding、operation、health 承担恢复和授权。关闭未来正文不等于删除历史对象、manifest 或 read grant。 | 内容读取由独立 source-content scope、credential、coverage 和 purpose 授权，capture policy **不等于**读取授权（`internal/sourcefacts/content_body_authority.go:48-62`）。 |

**关闭正文的核对范围。** 未来新请求要覆盖客户端请求/响应、每次 Provider 请求/响应、媒体路径、成功/失败/流中断、pending/partial/limit gap；核对文件存储、`execution_content_objects`、manifests、维护过期/回收，以及 diagnostic `upstream_error`、trace `metadata`、audit `detail`、outbox `payload`、feed/source document、媒体记录与应用日志的真实 writer。保留 manifest 的 `not_captured` 与必要运行/计费事实。XI 自有库和受管采集器另由其所有者做对应负向检查；不能从 UR 源码宣称它们已符合 D12。现有过期回收见 `internal/sourcefacts/content_expiry.go:32-117`，文件储存见 `internal/contentcapture/store.go:145-214`。

## 正式配置、授权与 XI 主入口的最小协作

现有 `GET/PUT /api/v1/projects/{id}/payload-policy`（`cmd/uniroute/main.go:1137-1138`）是项目级正式 API。`PolicyConfig` 有 `disabled`、`metadata_only`、`full_content`，协议/观察点、留存秒数、总额/单项上限和 purpose；`redaction_ref` 当前拒绝，`full_content` 当前只许 `human_review`（`internal/contentcapture/policy.go:19-46,90-135`）。GET 返回 source instance、org/project、policy ref、desired/effective 各自版本、effective_at、`applied/pending/failed`、原因、存储状态和 limits（同文件 73-86,194-242）。PUT 带 `expected_policy_version`，不同版本为 409；持锁事务校验、更新并写 audit，pending 可返回 202（`internal/contentcapture/policy_store.go:140-239`；`internal/admin/content_policy.go:37-123`）。因此 XI 可做“主动保存→正式写入→回读→显示生效/待处理/失败”；后来 UR 单独修改时比较已确认 XI 期望版本与 UR 当前 desired/effective，显示 drift，**不自动覆盖**。恢复 XI 或接受 UR 是下一次显式动作，须用新鲜版本再次 CAS。

但上述 API 只允许已登录 UR 用户的组织/平台管理员或该项目管理员，经同一事务重验（`internal/admin/content_policy.go:43-90`）；它不是 XI 机器服务可直接使用的下发授权。现有部署 `DefaultMode` 仅 `disabled` 或 `metadata_only`，且全局 disabled 是硬停，完整正文另有部署开关/存储就绪要求（`internal/contentcapture/policy_store.go:23-67,121-137,157-170`；`cmd/uniroute/content_runtime.go:35-75`）。目前策略表仅 `(org_id, project_id)` 一层，没有企业默认自动覆盖未来项目、用户例外的正式 policy scope。D08/D11 因此是**明确实施差距**，不能把项目默认虚称企业策略。需在既有策略体系中增加企业默认、项目/用户例外的最小持久化与有效策略解析，并设计有授权的 XI 服务写入合同；不让 XI 直接写 UR 私表或借 source-read key 变更策略。

`GET /api/v1/data-exchange/xinsight/source-connections/{project_id}/content` 及 prepare/rotate/activate/resume/pause/cancel/revoke 是**内容来源连接/读凭据生命周期**（`cmd/uniroute/xinsight_content_control_routes.go:10-18`），不是正文保存策略。内容源读取能力通过 `GET /api/v1/integrations/xinsight/source/v2/content-capabilities` 宣告协议、观察点、capture modes、purpose、coverage、retention floor、limits 与 endpoints（`internal/sourcefacts/content_read_contract.go:13-40`；`cmd/uniroute/content_runtime.go:116-140`）。策略 UI 的项目/用户发现和选择也尚须正式列表/授权边界设计，不应让管理员手抄底层 ID。

## OpenClaw—UR—XI 关联边界

`X-Request-ID` 是 UR 在身份中间件生成、不可由调用方覆写的 `op_<32 hex>` 执行 ID，沿 legacy DB/API `request_id` 使用；调用方传 `X-UniRouter-Client-Request-ID`，旧入站 `X-Request-ID` 仅兼作客户端关联，冲突会 400；`Idempotency-Key` 是单独的去重身份（`internal/middleware/request_identity.go:15-32,58-143`）。响应暴露服务器 ID 与客户端关联头（同文件 129-135；`internal/middleware/cors.go:27-28`）。上游 Provider 的 request ID 又是另一值（`internal/gateway/relay.go:317-336`）。尝试、trace、Source fact/event 以 UR server request ID 关联（`internal/gateway/trace.go:247-307`；`internal/sourcefacts/envelope.go:91`）。现有 execution context 有 workflow/agent/parent/lineage/actual-human 的**有界已知字段**（`internal/executioncontext/context.go:20-50`），不能推出工具动作本身。

OpenClaw 适配器若可从模型调用响应取得 UR server ID，应把它和具体 agent/tool 事件关联依据一起报告 XI；若只能设置客户端关联号，则由 UR 正式事实和精确调用回执再绑定，不能将客户端/Provider ID 冒充 UR ID。先离线验证当前受支持 OpenClaw 版本实际可取得的响应头、流式和失败路径，再决定是否有真正缺口；不预设增加 UR 框架私有端点。

## 改动与验收边界

最小 UR 实施候选：在现有 project policy/control/store 上补企业默认与用户例外的**正式作用域及优先级**、未来项目继承、有效策略和版本回读；为 XI 提供独立、最小权限的策略写入/发现合同及 actor audit；保持 UR 自身项目配置能力、版本 CAS、部署硬停/存储就绪与 read grant 独立；确认现有 correlation 和 Source 输出是否已够。若扩展公开 API、DB/迁移或交付运行合同，依项目规则处理产品版本/CHANGELOG、迁移前态/负向验证和编码前独立审查。当前设计不创建 UR 第二账本、XI 私表写口、框架私有 UR 路由或新调度系统。

本地验收应覆盖：企业默认对新/已有项目、项目与用户例外优先级；管理员与 XI 服务授权的允许/拒绝；旧版本 409、XI 保存成功但 UR pending/failed、UR 单独改后 drift 且无后台覆盖；`disabled` 下上述所有受管正文持久化负向路径，同时账务/审计/恢复链仍工作；缺失实际人/Provider request ID/工具事实不虚构；Source manifest gap、read grant 与正文保留独立。真实 XI 独立保管、Target、授权模型调用、文件变化核验及容量/时延不在本份源码核对中宣称通过。

尚需共同合同落点：XI 以什么**经授权的服务身份**调用 UR 策略写入，以及企业/项目/用户例外的主体标识与优先级应由两端固定；这是 P1 接口设计问题，D08/D11/D15/D16 的产品方向已确定。若 XI 方案已有合适的正式身份与主体合同，优先复用并核对它能否与 UR 事务重验、审计及原项目管理权限共存。
