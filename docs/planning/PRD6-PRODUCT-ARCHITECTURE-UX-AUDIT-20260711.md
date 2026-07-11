# PRD6 产品、架构与 UX 审计（2026-07-11）

> 状态：PRD6 独立审计输入，不修改或重开 PRD5 历史范围。
> 审计基线：`7173c5967a42c112fd0b219938a0901c1a0b9633`（PR #142 合并后的 `main`）。
> 角色视角：平台产品、分布式系统、大规模 2B SaaS、MaaS 网关。
> 本文只评价 UniRouter 自有产品与架构；X-Insight 仅作为边界参照，不把其功能写入 UniRouter 范围。

## 1. 证据口径与结论摘要

本文严格使用三类表述：

- **Confirmed finding（已确认发现）**：可由当前源码、SQL、测试或命令直接复现。每项附绝对路径与行号；若当前工作分支正在修复，则明确写“基线已确认、修复待验收”。
- **Hypothesis（待验证假设）**：对 100 倍吞吐、客户使用方式、目标环境或未来 Agent/多模态负载的推演，不当作当前故障。
- **User new requirement（用户新增需求）**：本轮提出的产品方向或体验目标，需要纳入 PRD6/PRD7 决策，但不等同于已经实现。

视觉验证边界：本轮没有使用任何真实账号或凭证登录管理端。未认证浏览器只能看到登录页；登录后的页面判断来自 React 源码、API 合同和自动化测试，不声称完成了 authenticated visual acceptance。正式 UI 验收必须使用隔离测试租户和脱敏 fixture 重新截图/走查。

### 1.1 回答用户问题的直接结论

1. **审计日志应该分类，但不建议先拆成多张日志表。** 保留一个不可分叉的事实账本，在 UI 增加“身份与登录、账号与权限、路由与模型、财务、通知与集成、系统任务”分类视图；分类应由稳定的事件 taxonomy 驱动。
2. **“全量偏差”不是账单对账差额。** 当前真实公式是“当前筛选范围内、HTTP 状态小于 400 的最终 `total_tokens` 合计，减去 preflight 估算 Token 合计”；“全量”只表示不受当前分页限制，未选择时间时甚至覆盖留存期内全部匹配记录。当前命名会误导。
3. **一个可查询的关联链是必要的，但不能让一个 `request_id` 承担全部语义。** 正常聊天路径已经把同一个 ID 用于 gateway、billing、trace、call log 和上游请求头；但 client 可自定义该值、财务幂等键又以其作为全局主键，且 failover 没有 `attempt_id`，因此“看起来贯穿”不等于“能无歧义追责”。
4. **UI 目前优先要修的是正确性和租户边界，而非配色。** 审计、用量对账、钱包存在基线租户范围问题；组织价格策略页还存在固定组织和前后端字段合同不一致。
5. **`main.go` 偏大，但不是吞吐瓶颈。** 它的主要问题是装配、路由注册和后台任务混在一起，影响维护与升级；真正的复杂度热点是 1,452 行的 `internal/gateway/relay.go`，它同时承担请求解析、计费、路由、failover、流式转发、审计和落账。
6. **2B patch 需要“可验证、可回滚、可离线”的发布链。** 当前已有 build version/commit、迁移 status、备份和 canary 手册基础；尚没有自动生成的版本化制品、SBOM/签名、兼容性清单和客户本地的受控升级包。推荐先做“人工批准的签名离线包”，不做静默自动更新。

## 2. a）审计日志：分类、命名、时间筛选与可读性

### 2.1 Confirmed findings

本节对 `audit_handlers.go` 的行号引用固定指向上述 `7173c59` 审计基线，避免当前 P6-F37 修复分支的增量改行造成“发现不存在过”的误解。

| ID | 发现 | 证据 | 影响 |
|---|---|---|---|
| AUD-01 | 基线 `ListAuditLogs` 只按用户传入的 `org_id` 过滤，没有自动绑定 JWT actor org；路由却允许 `org_admin`、`billing_admin` 访问。P6-F37 已加入 actor-org 约束，通过迁移后双租户 JWT 回归、PR #143 required CI 和 post-merge main CI。 | 基线 `/Users/m4/Desktop/projects/UniRouter/internal/admin/audit_handlers.go:18-42`；路由 `/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go:467`；证据 `/Users/m4/Desktop/projects/UniRouter/docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md` | P0 跨租户审计数据暴露；已在 `a1e0ad6` 修复。
| AUD-02 | 后端已经返回 `before`、`after`、IP、User-Agent、`request_id`，但表格只展示通用 `detail`，这些调查字段没有进入 UI。 | `/Users/m4/Desktop/projects/UniRouter/internal/admin/audit_handlers.go:97-131`；`/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/AuditLogsPage.tsx:62-80` | 出事后仍需查库，UI 不能完成闭环。
| AUD-03 | 操作者显示为 `User #<id>`；组织、项目、目标也主要显示裸 ID；后端查询没有 join 用户名、组织名、项目名或对象快照。 | `/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/AuditLogsPage.tsx:62-74`；`/Users/m4/Desktop/projects/UniRouter/internal/admin/audit_handlers.go:97-130` | 业务用户无法知道 “User #1 是谁”。
| AUD-04 | 开始/结束时间是手填 RFC3339 文本框；每次输入都会改变 `filters` 并触发 `useEffect` 请求，同时页面仍保留“筛选”按钮。 | `/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/AuditLogsPage.tsx:42-55`、`:91-107` | 非技术用户难用；输入中间态会产生无效请求和错误闪烁。
| AUD-05 | 用户名密码登录与 OIDC 成功会写审计；用户名不存在、密码错误、禁用账号、OIDC state/exchange/mapping 失败直接返回，没有失败安全事件。 | `/Users/m4/Desktop/projects/UniRouter/internal/admin/auth_handlers.go:35-71`；`/Users/m4/Desktop/projects/UniRouter/internal/admin/oidc_handlers.go:113-162` | 无法从审计台回答暴力尝试、账号枚举趋势和 SSO 故障。
| AUD-06 | 用户 CRUD 和组织价格策略 CRUD handler 中没有审计调用；现有事件覆盖并非“所有关键后台动作”。 | 基线命令 `rg -n "recordAudit" internal/admin/user_handlers.go internal/admin/model_handlers.go` 无输出；相关实现 `/Users/m4/Desktop/projects/UniRouter/internal/admin/user_handlers.go:69-221`、`/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:418-540` | 审计覆盖不完整，UI 文案容易让人误以为已完备。

### 2.2 推荐的信息架构

保持一张 canonical audit ledger，增加稳定字段或由 action prefix 映射以下分类；分类是视图，不改变事实记录：

| UI 分类 | 典型事件 | 面向的用户问题 |
|---|---|---|
| 身份与登录 | `login.success`、`login.failed`、`login.oidc.*`、`password.changed` | 谁在什么时间从哪里登录，为什么失败？ |
| 账号与权限 | 用户增删改、角色变更、项目成员、API Key、上游凭证 | 谁给谁增加了什么权限？ |
| 路由与模型 | 通道、模型目录、alias、model set、价格前的模型配置 | 哪次变更影响了路由结果？ |
| 财务 | top-up、adjust、freeze、settle、grant、价格策略 | 余额为什么变化，使用哪个幂等/请求依据？ |
| 通知与集成 | webhook 创建/验证/禁用、投递、数据交换控制动作 | 数据或告警发到了哪里，谁批准的？ |
| 系统任务 | migration、cleanup、retention、worker lease、license | 系统自动做了什么，在哪个版本发生？ |

推荐事件命名为 `domain.object.verb[.result]`，例如 `identity.login.failed`、`access.user.role_changed`、`billing.wallet.adjusted`。不要把 UI 中文文案写入数据库；数据库保存稳定 code，前端做本地化显示。

### 2.3 推荐列名与展示方式

| 当前名称 | 推荐名称 | 推荐展示 |
|---|---|---|
| ID | 审计事件 ID | `AUD-000001` 或可复制的数字 ID，不作为主语义 |
| 组织 | 组织 | `Acme 华东（#12）`；平台级明确显示“平台” |
| 项目 | 项目 | `客服机器人（#48）`；无项目显示“组织级”而不是 `-` |
| 操作者 | 操作人 | `张三（组织管理员 · #7）`；系统任务显示 `UniRouter System` |
| 动作 | 事件 | 中文标签 + 次级 code，例如“角色变更 / `access.user.role_changed`” |
| 目标类型 / 目标 ID | 操作对象 | `用户：李四（#18）`；保留类型和不可变 ID |
| 详情 | 变更内容 | 默认渲染 before/after diff；原始 JSON 放抽屉，不横向撑满主表 |
| 时间 | 发生时间 | 本地时区显示，hover/copy 提供 UTC RFC3339 |

筛选推荐：`最近 15 分钟 / 1 小时 / 24 小时 / 7 天` 快捷项 + 日期时间范围选择器 + 明确时区 + “应用筛选”按钮。输入只更新 draft filters，点击应用后才发请求。保留精确事件 code/ID 搜索给高级用户。

### 2.4 User new requirement 与边界

- **PRD6 新需求**：分类视图、易懂对象名称、日期范围控件、详情抽屉、成功/失败身份事件、缺失 CRUD 事件。
- **不建议**：为登录日志另建一套不可关联的日志系统。安全视图可以独立导航，但底层必须复用 canonical ledger，避免“账号日志”和“业务日志”互相对不上。
- **隐私边界**：不在审计中保存密码、token、完整凭证、完整 prompt 或 URL query secret；IP/User-Agent 应受租户 RBAC、retention 和导出审计控制。

## 3. b）用量对账：“全量偏差”到底是什么

### 3.1 当前真实计算

单条记录：

```text
estimated_tokens = COALESCE(NULLIF(call_logs.estimated_tokens, 0), prompt_tokens + completion_tokens)
actual_tokens    = call_logs.total_tokens
delta            = actual_tokens - estimated_tokens
status           = status_code < 400 ? reconciled : error
```

证据：`/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:83-144`。

汇总卡片：

```text
preflight_estimated_tokens = SUM(上述 estimated_tokens，仅 status_code < 400)
actual_tokens              = SUM(total_tokens，仅 status_code < 400)
variance_tokens            = actual_tokens - preflight_estimated_tokens
variance_ratio             = variance_tokens / preflight_estimated_tokens（由 metering helper 处理边界）
```

SQL 和赋值证据：`/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:170-194`。

“全量”的实际含义：汇总使用当前 model/status/usage_source filters，但不受当前页 `LIMIT/OFFSET` 限制；后端标记为 `filtered_query`。页面没有时间筛选，因此不加条件时覆盖 `call_logs` 留存期内所有匹配记录。证据：`/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:36-88`、`:151-167`；UI `/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/ReconciliationPage.tsx:128-197`。

### 3.2 Confirmed findings

| ID | 发现 | 说明 |
|---|---|---|
| REC-01 | `reconciled/正常` 仅等价于 HTTP `<400`，不是“已与上游账单或发票核对”。 | `/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:50-56`、`:124-143`。
| REC-02 | “实际 Token”不总是上游权威值；`usage_source` 可为 estimated、adapter_estimated、counter_v2 等。 | 汇总主动区分来源：`/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:170-188`；UI 却统一叫实际 Token：`/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/ReconciliationPage.tsx:69-80`。
| REC-03 | GET 查询在估算来源比例超阈值时同步 `Emit` 通知事件，读操作会写数据库并可能同步发 webhook。 | `/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:151-163`、`:197-245`；webhook 默认 10 秒 timeout 且逐条发送：`/Users/m4/Desktop/projects/UniRouter/internal/billing/notifications.go:60-78`、`:287-337`、`:401-430`。
| REC-04 | 基线 handler 没有 actor-org 条件，`billing_admin` 路由可查全表 `call_logs` 汇总和明细。 | `/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go:430`；`/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:33-70`、`:170-179`。这是 P0，不是文案问题。
| REC-05 | 正偏差一律红、负偏差一律绿；但少计和多计都可能需要调查，颜色不能直接代表好坏。 | `/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/ReconciliationPage.tsx:82-99`。

### 3.3 推荐改名

- 页面：保留“用量质量核验”或“Token 计量核验”；只有真正接入 provider invoice/billing export 后才叫“账单对账”。
- `估算 Token` → `请求前预估 Token`。
- `实际 Token` → `最终计量 Token`，旁边必须展示来源和置信度。
- `全量偏差` → `当前筛选范围净偏差`。
- `正常` → `请求成功`；增加 `权威来源 / 估算来源 / 缺失来源 / 超阈值` 等计量质量状态。
- 增加绝对偏差、偏差分布/P95，而不是只看正负相互抵消后的净值。

## 4. c）请求关联链：现在实现到哪里，应该怎样设计

### 4.1 Confirmed current behavior

- Gateway auth 接受客户端 `X-Request-ID`；为空才生成，并把值写入 context：`/Users/m4/Desktop/projects/UniRouter/internal/middleware/auth.go:255-268`。
- Chat handler 将它返回给客户端、用于 freeze、路由事件、上游 adapter、settle/cancel、call log：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go:282-348`、`:379-507`、`:1010-1067`。
- Trace 事件有 `trace_id`、`span_id`、`parent_span_id`；没有可信 evidence trace 时 `trace_id=request_id`：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/trace.go:140-189`。
- Provider 返回的请求 ID 单独保存为 `upstream_request_id`：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go:454-490`、`:1035-1066`。

因此，对“正常单次 chat 请求”，答案是：**基本贯穿了**。但对“跨租户、重复 client ID、多次 failover、多 Agent 工作流和跨服务 trace”，答案是：**语义仍不完整**。

### 4.2 Confirmed gaps

| ID | 发现 | 风险 |
|---|---|---|
| COR-01 | client 提供的 `X-Request-ID` 未被转换为独立 `client_request_id`，而 `freeze_idempotency.request_id` 是全局主键。 | 两个组织使用相同 client ID 会发生财务幂等碰撞；恶意/误配置 client 可影响另一租户的 freeze/settle 关联。证据：`/Users/m4/Desktop/projects/UniRouter/migrations/000001_init_schema.up.sql:248-257`、`/Users/m4/Desktop/projects/UniRouter/internal/billing/billing.go:142-148`、`:200-209`。
| COR-02 | failover 只有 route index metadata，没有独立且可查询的 `attempt_id`。 | 无法稳定把一次上游响应、耗时、错误、熔断状态与具体尝试关联。
| COR-03 | 所有渠道失败时，最终 call log 使用 `routes[0]` 的 channel，却搭配最后一次尝试的 status/upstream ID。 | 产生“第一个渠道 + 最后一个上游 ID”的混合记录。证据：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go:394-507`。
| COR-04 | 成功响应元数据中的 `fallback` 固定为 `false`。 | 即使前一个 route 失败、后一个 route 成功，用户仍看不到发生过 failover。证据：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go:599-608`、`:956-966`。
| COR-05 | call log 写失败仅记录日志并返回，不会补偿；trace 写失败同样 best effort。 | 高并发或数据库瞬时故障时，响应可能成功但计量/调查证据不完整。证据：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go:1044-1071`、`/Users/m4/Desktop/projects/UniRouter/internal/gateway/trace.go:167-192`。

### 4.2.1 P6-F17 remediation update

以上表格保留的是审计时基线，不能继续当作当前未修复状态。P6-F17 已由 PR #149
合并并通过 implementation/post-merge required CI，完成以下修复：

- COR-01：`X-Request-ID` 现在由 UniRouter 生成不可覆盖的 `op_` 值；旧调用方传入的
  `X-Request-ID` 只作为兼容 correlation，并持久化为 `client_request_id`。财务冻结键
  改为 tenant composite，Scheme A idempotency 以 tenant/API key/payload/HMAC 绑定；
- COR-02：每次 route/failover 都生成独立 `attempt_id`，并可按 operation、client、
  workflow、Agent、attempt 或 upstream ID 在 Logs/Audit 中回查；
- COR-03/COR-04：最终 call log 使用真实最终 attempt 的 channel/upstream identity，
  response metadata 返回真实 `fallback`、`attempt_count`、`route_count` 和 `attempt_id`；
- COR-05 不由 P6-F17 冒充完成：call log/trace/settlement 的 durable finalization、
  crash recovery 和 bounded internal outbox 仍是 P6-F20。

为兼容现有 API 和数据库，当前实现把 server-owned operation ID 暴露在旧字段名
`request_id` 中，不再额外制造第二个 server HTTP ID。`client_request_id`、
`idempotency_key_hash`、`trace/span`、`workflow_id`、`agent_id`、`attempt_id` 和
`upstream_request_id` 各自保持独立语义。证据见
`docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md`。

### 4.3 推荐 ID 模型（PRD6）

| 字段 | 一句话含义 | 生成者 / 作用域 |
|---|---|---|
| `operation_id` / legacy `request_id` | UniRouter 实际接受的一次服务端操作；当前为同一 server-owned `op_` 值 | UniRouter 强制生成，全局唯一，不直接信任 client 值 |
| `client_request_id` | 客户端传来的原始关联值 | 客户端提供；只可查询，不作为全局财务主键 |
| `idempotency_key` | 明确用于有副作用动作去重 | 按 `(org_id, operation_type, idempotency_key)` 唯一 |
| `trace_id` / `span_id` | 跨组件的分布式 trace 与单个工作单元 | 遵循 [W3C Trace Context](https://www.w3.org/TR/trace-context/) / [OpenTelemetry Trace API](https://opentelemetry.io/docs/specs/otel/trace/api/) |
| `attempt_id` | 一次具体 route/upstream 尝试 | 每次 retry/failover 新生成，关联 channel/key/status/latency |
| `upstream_request_id` | Provider 返回的支持工单 ID | 上游生成；绑定 `attempt_id` |
| `workflow_id` / `agent_id` / `parent_agent_id` | 多 Agent 图中的业务关联 | 上层 Agent runtime 传入；只做受信传播和审计，不用于网关授权 |

传播规则：`traceparent` 走标准 header；业务关联可以使用受限 baggage，但敏感或高基数字段不能盲目传播。OpenTelemetry 对 baggage 的风险说明见 [Baggage 文档](https://opentelemetry.io/docs/concepts/signals/baggage/)。

## 5. d）UI 可改进项：设计、命名、已有但未展示

### 5.1 P0：先修正确性和租户边界

| ID | Confirmed finding | 证据 / 建议 |
|---|---|---|
| UI-01 | 钱包查询、入账、余额调整、交易列表和导出都按“第一钱包”或全交易表工作，没有 actor org 绑定。 | `/Users/m4/Desktop/projects/UniRouter/internal/admin/wallet_handlers.go:13-56`、`:78-145`、`:174-233`、`:235-293`；路由允许 billing admin：`/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go:415-418`、`:489`。必须先修，再展示更多钱包能力。
| UI-02 | Freeze 同时从 balance/grant 扣除并增加 `frozen`，下一次可用额又计算 `balance+grant-frozen`；UI 也用已扣后的 balance/grant 再减 frozen。 | `/Users/m4/Desktop/projects/UniRouter/internal/billing/billing.go:151-186`；`/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/WalletPage.tsx:93-95`、`:138-149`。无论最终选择“余额含冻结”还是“余额不含冻结”，当前读写定义不一致，需建立单一账本不变量并迁移验证。
| UI-03 | 价格策略页固定查询 `org_id=1`，创建/更新发送 input/output price；后端 API 实际只读写 `multiplier`，且 list 返回 `items`。 | `/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/OrgModelPricesPage.tsx:19-25`、`:45-70`、`:106-158`；`/Users/m4/Desktop/projects/UniRouter/frontend/src/api/client.ts:309-317`；后端 `/Users/m4/Desktop/projects/UniRouter/internal/admin/model_handlers.go:418-520`。这是可见功能的合同断裂。
| UI-04 | 平台管理员新增用户的 UI 没有组织选择；后端在平台调用未传 `org_id` 时取数据库任意第一组织。 | `/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/UsersPage.tsx:122-142`；`/Users/m4/Desktop/projects/UniRouter/internal/admin/user_handlers.go:69-100`。平台管理员必须显式选择组织，组织管理员则锁定 JWT 组织。

### 5.2 已有后端能力但 UI 未完整展示

| 能力 | 当前证据 | PRD6 建议 |
|---|---|---|
| 钱包资金流水 | API client 已有 `getWalletTransactions`，后端有 list；导出 route/OpenAPI 也存在。`total_spent_cents` 已由 wallet API 返回。 | 在租户隔离修复后增加流水分页、按类型/时间过滤、CSV 导出和累计支出卡。证据：`/Users/m4/Desktop/projects/UniRouter/frontend/src/api/client.ts:116-121`；`/Users/m4/Desktop/projects/UniRouter/internal/admin/wallet_handlers.go:37-52`、`:235-293`。
| 项目管理 | 后端有项目 list/create/get/update 与 model-set 绑定；目前只被 Developer Console/Model Governance 当选择器使用，完整管理员导航没有项目管理页。 | 增加“组织与项目”页，展示组织、项目成员、模型集、预算和状态。证据：`/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go:403-408`；`/Users/m4/Desktop/projects/UniRouter/frontend/src/navigation.tsx:73-130`。
| 审计调查字段 | 后端返回 before/after/IP/UA/request ID。 | 详情抽屉、可复制关联 ID、字段级 diff、敏感字段遮罩。
| 运行版本 | runtime response interface 含 version/commit，build 也注入；状态页没有渲染它们。 | 在系统状态与页脚显示版本、commit、schema version，便于工单与 patch。证据：`/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/InternalStatusPage.tsx:62-77`、`:314-391`；`/Users/m4/Desktop/projects/UniRouter/Dockerfile:1-10`。

### 5.3 体验与可信度

- 登录页的 `2.24M / 99.92% / 18` 是硬编码展示，不是当前实例数据：`/Users/m4/Desktop/projects/UniRouter/frontend/src/pages/LoginPage.tsx:72-103`。2B 产品应改为无数字的产品能力说明，或明确标记“示意数据”。
- 多个页面对失败只 `console.error` 或静默 catch，例如用户、审计、价格策略页。应统一 error state、重试、support code 和 `request_id`，不能让空表看起来像“没有数据”。
- 术语中英文混排（Billing、Request ID、Open 严重事件）应建立 glossary；技术 code 保留在二级信息层，主视图使用业务中文。
- 桌面宽表应改为“关键列 + 详情抽屉”；组织、项目、用户等关系用名称为主、ID 为辅。
- Authenticated visual acceptance 必须覆盖 6 种角色、空/错/加载状态、窄屏、长中文/英文名称、时区与大数字，不以 source review 代替。

## 6. e）架构与代码：`main.go`、100 倍吞吐和 Agent 爆炸

### 6.1 Confirmed findings

命令证据：

```text
$ wc -l cmd/uniroute/main.go internal/gateway/relay.go
734  cmd/uniroute/main.go
1452 internal/gateway/relay.go
```

1. `main.go` 同时负责配置、DB/Redis、迁移、组件装配、全部 route、HTTP server 和四类后台 loop。证据：`/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go:39-158`、`:490-527`、`:575-595`、`:673-734`。
2. `relay.go` 同时承担交互主链；单个 chat handler 从解析、过滤、freeze、RouteAll、300 秒 upstream client、failover、stream/non-stream、settle、trace 到 call log。证据：`/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go:282-514`。
3. 每个应用副本都会启动 cleanup、budget alert、notification retry 和 sensitive-word reload；retry 查询没有 lease/claim、`FOR UPDATE SKIP LOCKED` 或 owner，多个副本会选中同一 delivery。证据：`/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go:147-157`、`:673-734`；`/Users/m4/Desktop/projects/UniRouter/internal/billing/notifications.go:152-215`。
4. migration runner 没有 advisory lock、checksum、dirty state；SQL 执行与 `schema_migrations` 记账是两个独立 Exec。证据：`/Users/m4/Desktop/projects/UniRouter/internal/migrate/migrate.go:16-89`。多实例误配时存在并发/半记账风险。
5. 审计基线的 Frontend required CI 只做 type-check/build，没有执行现有 Vitest tests。P6-F38 当前分支已把 126 项测试加入该 required job，并把经实测会在并行 jsdom/Semi UI 负载下触发的默认 5 秒超时调整为有界 10 秒；断言未放宽。最终状态仍以 exact-head CI 为准。基线证据：`/Users/m4/Desktop/projects/UniRouter/.github/workflows/ci.yml:74-100`；本地证据：`/Users/m4/Desktop/projects/UniRouter/docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md`。

### 6.2 不应误判的点

- **Confirmed**：文件大是可维护性信号，不是性能测量。拆 `main.go` 本身不会提高吞吐。
- **Confirmed**：同步 HTTP/Streaming 是交互式 LLM 请求的合理主路径，不能为了“架构先进”强行把每个 token 经过 MQ。
- **Hypothesis**：100 倍吞吐时瓶颈更可能来自数据库写放大、长连接占用、上游限流、计费/trace 强一致路径、worker 重复处理和 tenant hot spot。必须用 workload model 和压测确认排序。

### 6.3 推荐的模块化单体改造（PRD6）

先保持一个可部署单体，按 ownership 抽 seam，不启动大规模微服务重写：

```text
cmd/uniroute
  bootstrap.go          配置、依赖和生命周期
  routes_gateway.go     /v1 网关路由
  routes_admin.go       /api/v1 管理路由
  workers.go            worker 注册、leader/lease、shutdown

internal/gateway
  admission/            鉴权、模型许可、限流、预算预检
  orchestration/        route plan、attempt、retry/failover policy
  transport/            non-stream、SSE、未来长任务 adapter
  accounting/           freeze/settle/cancel state machine
  evidence/             trace/audit/call-log outbox
```

要求：先加 characterization tests，再搬代码；每次 PR 只拆一个 seam，不同时改变产品行为、schema 和路由策略。

### 6.4 100 倍 / 大量 Agent 的三类扩展性盲区

| 类型 | 状态 | 未来崩溃方式 | 推荐改造 |
|---|---|---|---|
| 身份与策略放大 | Hypothesis（当前 API Key/JWT 是事实，Agent fan-out 规模未知） | 每个 subagent 重新走远端鉴权/策略读取；token 同时过期造成重试风暴和“鉴权雪崩”。 | 短寿命 capability token，绑定 org/project/agent/tool/audience/预算/过期时间；本地验签；父 Agent 只能衰减权限；控制面异步撤销版本。不要把长期平台凭证下发给 subagent。
| 同步 evidence 写放大 | Confirmed 设计 + 规模假设 | 每个请求/attempt 产生多次 trace、call log、billing 写；DB 短暂不可用时 best-effort 证据丢失，强制同步又会拖垮响应。 | 财务账本保持事务内最小事实；非财务 trace/audit 使用 transactional outbox + 有界队列 + backpressure + DLQ；消费者幂等。MQ 是否引入由压测阈值触发。
| 长连接与长任务混用 | Hypothesis | 视频生成、工具执行、百 Agent 串联超过 120/300 秒 timeout；连接断开后状态、计费与取消无统一归属。 | 定义 capability/adapter contract：`interactive_stream` 继续同步；`async_job` 返回 operation ID，使用持久化状态机、poll/webhook、cancel、lease 与阶段性计费；不让 gateway 内存成为任务状态源。

额外盲区：单一 `request_id` 造成高基数与语义碰撞；租户热分区使全局表/index 失衡；多个副本无 lease 的周期任务重复执行；failover 状态与最终聚合记录不一致。它们都应先有指标和故障注入，再决定分区、MQ 或服务拆分。

## 7. f）2B patch、升级和兼容性

### 7.1 当前已有基础（Confirmed)

- Docker build 支持注入 version/commit：`/Users/m4/Desktop/projects/UniRouter/Dockerfile:1-10`。
- runtime API/类型包含 version/commit；迁移 CLI 支持 `migrate up/status`：`/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/cli.go:77-100`、`:154-201`。
- Runbook 已有备份、拉镜像、自动迁移、ready check 和 canary/stable 手册：`/Users/m4/Desktop/projects/UniRouter/docs/ops/deployment-runbook.md:541-555`、`:595-609`、`:678-713`。
- required CI 有 Go race/vet、frontend type-check/build、Docker build：`/Users/m4/Desktop/projects/UniRouter/.github/workflows/ci.yml:9-115`。

### 7.2 当前缺口（Confirmed)

- `.github/workflows` 只有 `ci.yml`；没有 release workflow、版本 tag 制品、SBOM、签名/校验、release notes 或兼容矩阵。命令证据：`rg --files .github/workflows` 只输出 `.github/workflows/ci.yml`。
- 迁移没有 checksum/dirty/全局锁，不能证明客户数据库与发布包完全匹配。
- Runbook 是工程操作手册，不是客户可验证的离线 patch 包；也没有一键 preflight/postflight/support bundle。
- 当前 canary 共享数据库，遇到非 backward-compatible migration 时不能把“停 canary”当完整数据库回滚。

### 7.3 推荐分层

**PRD6：发布基础（必须先做）**

1. 采用 [Semantic Versioning](https://semver.org/)；版本、commit、schema range、build time 可读回。
2. 每个 tag 生成 immutable image/digest、离线 OCI bundle、migration bundle、前端静态制品、SBOM、SHA-256 manifest 和签名。
3. `uniroute preflight`：检查 CPU/磁盘、DB/Redis 版本、schema、备份可读、配置缺项、端口和 N/N-1 兼容性，不改状态。
4. migration 增加 advisory lock、checksum、dirty state、事务边界和 expand/contract 规则。
5. `postflight`：health、version、schema、关键只读 smoke、租户负向 RBAC；失败生成脱敏 support bundle。
6. 发布策略明确安全 patch、bugfix、minor 的支持周期；required CI 加 frontend tests、migration up/down/roundtrip 和 upgrade-from-N-1 fixture。

**PRD7：客户可消费的升级产品**

- 首选：客户本地、人工批准、签名验证的离线升级包；预览变更、备份、维护窗口、执行、readback、可回滚边界都可见。
- 后续才考虑 release channel、fleet rollout、自动暂停和多节点编排。
- 不推荐在 PRD6 做云端静默自动升级；2B 客户通常要求变更审批、离线环境、固定维护窗口和可审计责任边界。

## 8. g）额外优化建议

### 8.1 Confirmed-derived recommendations

1. **建立 tenant-scope query contract**：每个 org-scoped handler 必须先解析 actor scope；平台管理员跨组织操作必须显式选择 org，不能使用“第一条记录”。用一张 route × role × org matrix 做负向集成测试。
2. **建立资金账本不变量**：明确 `balance` 是否包含冻结额；推荐用 append-only ledger + derived balance，freeze/settle/cancel 使用复合幂等键和数据库约束。UI 只展示后端返回的 `available_cents`，不自行推导第二套公式。
3. **GET 必须无副作用**：阈值评估交给独立 evaluator/outbox；打开对账页不应创建通知或触发外发。
4. **把 failover 变成一等数据模型**：route plan、attempt、decision reason、breaker state transition、upstream ID 全部可查询；最终摘要由 attempts 聚合，不覆盖历史。
5. **worker 单一所有权**：使用 DB lease/`SKIP LOCKED` 或明确 leader election；每项任务有 owner、lease_until、attempt、heartbeat 和幂等写。
6. **让 support 可自助**：所有错误页显示可复制的 support code/request ID；系统状态显示版本/schema；导出前遮罩并写审计。
7. **契约优先 UI**：OpenAPI schema 生成/校验 TypeScript 类型，杜绝组织价格页这种 `multiplier` 与 input/output price 漂移。

### 8.2 Hypotheses that require measurement

- PostgreSQL 是否已成为吞吐瓶颈；需按 org/project/request/attempt 写入量测 P95/P99、lock wait、WAL、index bloat。
- Redis 锁竞争是否严重；当前不能仅凭“用了 Redis”判断，需采集 key cardinality、wait、timeout、hot-key 分布。
- Agent token 是否会集中过期；需明确 Agent runtime、token TTL、fan-out、refresh owner 和失败重试曲线。
- streaming 断线后的 token/成本偏差率；需注入 client disconnect、upstream half-close、DB outage、process kill。
- 客户是否需要在线升级中心；需按 air-gapped、单机/多机、维护窗口、合规审批分群。

### 8.3 h）当前两个/多个分支怎样处理

当前 GitHub 远端只存在 `main`；只读命令 `git ls-remote --heads origin`
返回 `7173c596... refs/heads/main`。PR #139、#140、#141、#142 均已合并，
不需要把它们再次 merge。用户界面若显示“两个分支”，更可能是本地历史分支加
当前工作分支；本机当前有 66 个本地分支，其中大部分是已完成批次的历史工作分支。

推荐默认处理：

1. P6-F37/P6-F38 已由 PR #143 合并，不需要再次 merge；当前 closeout 分支仅在本次文档 PR 完成前保留；
2. 历史分支不参与运行时，也不会自动进入 `main`，因此不构成功能风险；
3. 不用 `git branch --merged` 单独判断 squash-merge 是否完成，因为 squash 后原
   分支 commit 不是 `main` 的祖先；以 GitHub PR `MERGED` 状态和文件树差异为准；
4. 若用户授权清理，先 `git fetch --prune origin`，再逐个核对明确分支名、PR 状态
   和树差异，逐个删除本地分支；不批量删除，不重新 merge，不删除当前分支。

本轮未获得删除授权，因此没有删除任何分支。这是仓库维护事项，不属于 PRD6 或
PRD7 产品功能。

## 9. 四象限审计

| 象限 | 本轮内容 | 行动 |
|---|---|---|
| Known Knowns | audit/reconciliation/wallet 基线租户约束缺口；wallet freeze 读写不变量冲突；org price 前后端合同断裂；基础 request chain 已存在；main/relay/worker/migration 结构可直接读到。 | 进入 P0/P1，写集成测试、迁移和 UI 契约证据。
| Known Unknowns | 100 倍 workload、Agent fan-out、真实 streaming 断线率、上游 token 权威度、客户升级环境、目标实例数与 HA 模式。 | 先定义指标和实验，不以架构偏好提前购买 MQ/微服务复杂度。
| Unknown Knowns | 后端已有但产品没暴露的交易流水、CSV、total spent、项目 APIs、runtime version；audit action prefix 和 trace span 已形成隐性 taxonomy。 | 把现有能力产品化、统一命名，优先复用而非重写。
| Unknown Unknowns | 新 Provider 计费语义变化、视频/音频长任务、百 Agent 循环/级联取消、租户热分区、法规要求的数据驻留与删除证明、供应链攻击。 | 通过 capability contract、版本化事件、配额/深度上限、故障注入、签名制品和可撤销 feature flag 限制爆炸半径。

## 10. P0-P3 优先级矩阵

| 优先级 | 项目 | 产品价值 | 主要风险 | 推荐阶段 |
|---|---|---|---|---|
| P0 | Audit、reconciliation、wallet、org price 的 tenant isolation；禁止“第一组织/第一钱包”隐式默认 | 2B 数据隔离与财务正确性底线 | 跨租户读写/误入账 | PRD6 立即 |
| P0 | Wallet freeze/settle/cancel 不变量与复合幂等键 | 防止误拒绝、余额错算和跨租户 ID 碰撞 | 财务损失 | PRD6 立即 |
| P0 | Server-owned request ID + client correlation 分离；failover attempt 模型 | 可追责、可支持、财务去重可靠 | schema/兼容迁移 | PRD6 |
| P0 | Org price UI/API 合同修复、平台新增用户显式 org | 恢复已展示功能的基本可用性 | 错组织配置 | PRD6 |
| P1 | 审计 taxonomy、名称快照、日期控件、diff 抽屉、失败登录事件 | 非技术管理员可独立调查 | PII/retention | PRD6 |
| P1 | 用量核验术语、时间范围、来源质量、无副作用 evaluator | 防止误读“账单已对平” | 指标迁移/事件风暴 | PRD6 |
| P1 | 钱包流水/导出/total spent、项目页、版本/schema UI | 降低支持成本 | 导出权限/脱敏 | PRD6 |
| P1 | Worker lease、migration lock/checksum/dirty、frontend tests required CI | 多实例与升级可靠性 | 发布流程变更 | PRD6 |
| P2 | `main.go`/`relay.go` 渐进模块化、evidence outbox、有界 backpressure | 支持团队并行和 100 倍演进 | 大改回归 | PRD6 后半；以压测门槛决定 MQ |
| P2 | 签名离线 release bundle、pre/postflight、N-1 upgrade test | 2B patch 可消费 | 签名密钥与回滚边界 | 基础 PRD6，产品化 PRD7 |
| P2 | async job contract for video/long tools | 多模态/长任务 | 状态机与计费复杂度 | PRD7，须有真实需求 |
| P3 | 微服务全面拆分、插件市场、全托管 fleet 自动升级、完整 SDK | 潜在生态价值 | 运维/供应链/兼容成本 | PRD7+ 决策，不自动恢复历史 defer |

## 11. 可执行 batches 与验收证据

### Batch 6-A：Tenant and Money Safety（P0）

- 所有 org-scoped list/get/write 强制 actor org；platform cross-org 需要显式 org。
- 修 audit、reconciliation、wallet、org price；删除“first org/wallet”产品逻辑。
- 定义 wallet invariant；修 freeze/settle/cancel 和 UI；幂等唯一键变为 org-scoped composite。
- 修 org price API/UI contract 和 platform user org selector。

验收证据：

- PostgreSQL integration：Org A 的 billing/org admin 对 Org B 的 list/detail/write 全部 403 或 non-enumerating 404；platform explicit org 正常。
- 并发 freeze/settle/cancel、重复 client ID（同 org/跨 org）、失败回滚、余额守恒 property tests。
- migration up/down/roundtrip；旧数据 preflight 报告；`go test -race ./... -count=1`。
- 不做目标部署，不使用真实余额/凭证。

### Batch 6-B：Operator UX and Semantics（P1）

- 审计分类、名称快照、日期范围、详情 diff、失败身份事件和 CRUD coverage。
- 对账重命名，增加时间/来源质量/绝对偏差；GET 去副作用。
- 钱包流水/导出、项目管理、版本/schema、统一 error/support ID。
- 删除或明确标注登录页示意数字。

验收证据：

- Vitest/API contract tests；角色矩阵；长名称/空错加载/时区测试。
- 隔离 fixture 下 authenticated browser screenshots，覆盖 platform/org/billing/project roles；不得使用生产数据。
- 导出权限、脱敏、审计 readback；文案与公式 golden tests。

### Batch 6-C：Correlation and Attempt Ledger（P0/P1）

- expand schema 增加 operation/client/attempt/idempotency fields；保持旧 `request_id` 查询兼容。
- server-owned request ID；W3C trace context；每次 retry/failover 独立 attempt。
- 最终 call log 从 attempt 聚合，准确返回 `fallback=true/attempt_count`。
- 财务与 evidence 使用 durable outbox 边界；先证明无丢失，再异步化非财务事件。

验收证据：

- 两租户相同 client ID、三次 failover、stream disconnect、DB 瞬断、process kill 故障注入。
- 从任一 ID 可查回 operation → request → attempts → upstream → billing/audit；无混合 channel/upstream ID。
- trace header conformance、cardinality/redaction tests、load test 与数据完整率。

### Batch 6-D：Release and Multi-instance Operability（P1/P2）

- migration lock/checksum/dirty；worker lease；graceful worker shutdown。
- required CI 执行 frontend tests、upgrade N-1、migration roundtrip。
- SemVer release manifest、SBOM、digest/signature、offline bundle、pre/postflight、support bundle。

验收证据：

- 两实例同时启动只有一个 migration owner；worker delivery 恰好一次业务效果/至少一次处理可幂等。
- 篡改 bundle/manifest 必须拒绝；离线环境可验证签名；版本/schema readback 一致。
- 备份恢复、failed migration、canary rollback boundary 演练；目标执行仍需单独授权。

### Batch 6-E：Modular Monolith Seams（P2）

- 按 bootstrap/routes/workers、admission/orchestration/transport/accounting/evidence 渐进拆分。
- 不改变外部 API、计费公式、route policy 或 evidence level。

验收证据：characterization tests、route/OpenAPI/docs parity、Go race/vet、frontend full suite、Docker build；diff review 证明行为未改变。

## 12. 安全边界与最少用户决定

### 12.1 本轮默认安全边界

- 不部署，不修改 ECS/安全组，不发真实或付费上游请求，不更改凭证。
- 不访问额外数据库范围，不扩大数据导出，不启用 exporter/outbox。
- 任何财务 schema/migration 必须先备份、fixture 迁移、守恒验证和显式 target authorization。
- X-Insight 不参与 UniRouter runtime 实现；UniRouter 只提供版本化、最小化、受权的事实/控制合同。
- PRD5 历史证据保持不变；本文的新发现和批次只进入 PRD6/PRD7。

### 12.2 推荐默认值（若用户不反对即可按此推进）

1. 接受一个 canonical audit ledger + 六类 UI 视图，不拆双账本。
2. `request_id` 改为 UniRouter 生成；客户端值保存为 `client_request_id`；财务幂等键按组织和操作类型限定。
3. 升级先做“客户本地、人工批准、签名离线包”，不做静默自动更新。
4. `main.go`/`relay.go` 采用渐进模块化单体，不进行 PRD6 微服务重写。

只有以下事项需要后续产品决定，而不是阻塞 P0 修复：

- 客户界面把页面命名为“用量核验”还是“Token 计量核验”；推荐前者，副标题说明 Token 来源。
- 审计 IP/User-Agent 的默认 retention 与导出权限；需结合客户合规要求。
- PRD7 是否需要在线 update center；应先完成客户部署形态调研。
