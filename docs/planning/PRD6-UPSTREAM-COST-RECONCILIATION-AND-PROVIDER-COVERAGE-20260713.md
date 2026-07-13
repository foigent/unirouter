# UniRouter 上游成本对账方案与厂商覆盖评估

> Date: 2026-07-13
> Status: User-accepted architecture boundary and future product reference
> Evidence level: source/documentation research; not target or production verification
> Repository baseline: `main`/`origin/main` at `234ada685ac26a9baffd5a9d32b82259f3211ec5` before this documentation branch
> Scope guard: this document clarifies P6-F40/P6-F41 and records future candidates. It does not authorize provider credentials, billing polling, bill import, paid requests, deployment, a separate Cost Control Service, or PRD7 implementation.

## 1. 结论先行

用户在 2026-07-13 接受以下方案：

1. UniRouter 使用两本逻辑账：
   - **Customer Revenue Ledger**：由可证明的客户交付事实、权威用量和冻结客户价决定；
   - **Provider COGS Ledger**：记录供应商成本从估算、响应观测、聚合匹配到发票核销的演进。
2. 供应商聚合账单、账户余额变化或运营人员手工输入，**不得直接生成客户扣款**。
3. P6-F40C 的 `collect` 只能收取由既有客户价格合同、权威用量和交付结果确定的金额；不能用供应商 COGS 差额或任意手填金额创造客户欠款。
4. 不设计跨厂商通用的 `exact_request_charged=true`。统一使用可证明的证据等级：
   - `exact_request_usage`；
   - `aggregate_cost_matched`；
   - `invoice_reconciled`。
5. PRD6 只完成当前模块化单体中的安全财务内核和厂商中立证据合同。真实账单连接器、云成本导出、受控账单导入、多单位计量和异步媒体任务是后续产品能力。

推荐继续采用该方案。它比“每次请求都假设已经知道厂商最终扣费”更符合当前供应商公开能力，也比把上游差额直接转嫁客户更安全、可审计。

## 2. 分类口径

### 2.1 Confirmed findings

- 当前默认适配器注册表只有 `openai_compatible` 和 `anthropic_native`：
  [`internal/adapters/registry.go`](../../internal/adapters/registry.go) 与
  [`internal/adapters/types.go`](../../internal/adapters/types.go)。
- 当前运行时端点枚举只有 `/v1/chat/completions` 和 `/v1/embeddings`；不能表达 Seedance 一类提交任务、查询状态、完成交付的异步生命周期。
- 当前统一 `Usage` 结构以 token 及 Prompt Cache token 分区为主；没有字符、音频秒、图片、视频秒/帧或任务计费单位。
- 当前 `billing_provider_attempts` 保存 request/attempt/channel/upstream request ID、计算成本和客户已收成本，但没有供应商确认成本、币种、账单周期、invoice ID 或 import batch：
  [`migrations/000029_prd6_provider_attempt_ledger.up.sql`](../../migrations/000029_prd6_provider_attempt_ledger.up.sql)。
- 当前“用量对账”比较本地估算 token 与响应返回 token，不是供应商账单/发票对账：
  [`internal/admin/model_handlers.go`](../../internal/admin/model_handlers.go)。
- 调研的 12 个供应商账务平台均能提供某种请求或任务用量证据；其中 7 个有公开的结构化聚合成本 API、云账单 API 或机器可导入账单导出。豆包和 Seedance 共用火山方舟这一供应商账务平台。
- 未在这些供应商的公开官方文档中验证到一个可被第三方网关通用使用、按 gateway `request_id` 实时返回最终财务扣费的接口。

### 2.2 Hypotheses requiring later verification

- 企业合同、专属商务后台、地区版产品或私有 API 可能提供比公开文档更细的成本字段。
- 厂商文档标注的典型延迟不等于客户账号的稳定 SLA；正式连接器必须以选定账号、区域和合同重新验证。
- 相同的 OpenAI-compatible 协议不保证错误码、stream usage、cache usage、request ID、取消语义和计费触发点完全一致。

### 2.3 User-accepted decisions

- 接受双账边界和分级证据模型。
- 接受把本方案未覆盖部分记录为后续产品待实现功能。
- PRD7 与目标环境证据仍按既有 D6-09 决策后置到 PRD6 闭环之后；本次登记不是当前实现授权。

## 3. 为什么不能“实时确认所有上游已经收费”

一次模型调用通常产生三种不同事实：

1. **技术事实**：供应商收到了请求，返回 response/task ID 和 usage；
2. **计价事实**：按当时可见价格、折扣、缓存类型和单位计算的成本估算；
3. **财务事实**：供应商最终账单经过折扣、赠金、税费、汇率、阶梯价、补账、退款或承诺消费处理后的应付金额。

第一种事实可以接近实时；第二种事实可以由 UniRouter 实时计算；第三种事实通常只在分钟、小时、日或月度账单层出现。把第一或第二种事实命名为“供应商已经最终扣费”会制造错误的财务权威。

因此，正确状态演进是：

```text
request/task evidence
  -> response_observed / usage_observed
  -> cost_estimated
  -> aggregate_cost_matched
  -> invoice_reconciled
```

状态必须单调追加证据，不覆盖原始记录。后到的供应商账单只调整 Provider COGS Ledger 和毛利差异，不改写已依据客户合同形成的 Customer Revenue Ledger。

## 4. 推荐架构

### 4.1 Customer Revenue Ledger

客户账只接受以下权威输入：

- 客户和租户身份；
- server operation/request/attempt identity；
- 模型、价格版本和组织倍率的冻结快照；
- `usage_authority`、`usage_purpose`、`usage_finality`；
- 本地单调 delivery outcome；
- 已授权的退款或合同内调整。

禁止输入：供应商日账单总额、账号余额差、未知归因的成本差额、任意运营手填扣款。

### 4.2 Provider COGS Ledger

供应商成本账至少需要：

| 字段组 | 最小字段 |
|--------|----------|
| 身份 | `provider_account_id`、`channel_key_id`、`request_id`、`attempt_id`、`upstream_request_id`、可选 `provider_task_id` |
| 计量 | `usage_unit`、`quantity`、`usage_source`、`usage_finality` |
| 计价 | `currency`、`price_contract_version`、`estimated_cost`、`provider_observed_cost` |
| 对账 | `evidence_level`、`billing_bucket`、`import_batch_id`、`invoice_id`、`matched_amount`、`variance_amount` |
| 调整 | append-only adjustment、原因、操作者、授权、前后证据引用 |

建议证据状态：

- `estimated`：响应 usage × 冻结供应商价格；
- `response_observed`：供应商响应或任务查询提供 usage/cost 信号；
- `aggregate_matched`：已与小时/日/项目/SKU/账号账单桶匹配；
- `invoice_reconciled`：已与结算账单或发票核销；
- `unattributed_variance`：存在供应商差额但不能安全归因到请求或客户。

### 4.3 Key block 的正确边界

- 只有在 dispatch outcome 未知、重试可能造成重复供应商责任，或已知客户费用无法安全完成资金归属时，才阻止受影响 key 的进一步付费 admission。
- 不能仅因为月度发票尚未到达就长期封锁 key。
- P6-F40C 的人工操作必须从既有有限动作中选择，并写入 append-only 审计；不能提供自由输入的客户扣款金额。
- `collect` 依据权威 usage + delivery + frozen customer price；`refund` 撤销客户账中的既有收取；`bounded subsidy` 是单独授权的平台 COGS 吸收，不伪装成客户债务。

## 5. 方案利弊

### 5.1 优点

1. **防误扣、防内部滥用**：不能用无法归因的聚合账单向某个客户补扣。
2. **客户账稳定**：供应商赠金、折扣、税费、补账或发票延迟不会反复改写客户消费。
3. **支持 BYOK 与托管模式共存**：BYOK 以客户合同账和供应商聚合对账并行；未来托管模式可以增加平台统一 credits/bill。
4. **审计可解释**：request → attempt → delivery → customer transaction 与 provider estimate → aggregate match → invoice adjustment 分开追踪。
5. **供应商中立**：允许不同厂商提供不同证据等级，不强迫伪造一个共同的实时扣费能力。
6. **支持多模态演进**：可扩展 token、字符、音频秒、图片、视频秒/帧和任务成功等单位。
7. **毛利可观察**：能够区分客户收入、供应商采购成本、不可归因差额和平台补贴。

### 5.2 缺点和风险

1. **短期没有最终财务真值**：供应商成本可能数分钟、数小时或次日才稳定。
2. **连接器维护成本高**：认证、分页、币种、税费、折扣、赠金、回填、撤销和账期均有厂商差异。
3. **存在不可归因差额**：聚合账单无法安全拆到请求时，差额必须由平台承担或进入财务调查。
4. **过度保守会伤可用性**：如果把“等待发票”误当成“未知 dispatch outcome”，可能不必要地封锁正常流量。
5. **异步媒体状态复杂**：任务提交、排队、生成、失败、取消、交付和收费触发点可能彼此分离。
6. **托管统一计费带来资金风险**：UniRouter 成为 merchant/reseller 后，要承担透支、坏账、供应商预充值、汇率和信用控制。

## 6. 友商处理方式

| 产品 | 客户消费记录 | 上游对账方式 | 财务权威等级 | 对 UniRouter 的启示 |
|------|--------------|--------------|--------------|---------------------|
| LiteLLM | response usage × 模型/自定义价格；逐请求 SpendLogs | 按同一时间窗将请求数、input/output/cache token 与 provider dashboard 比较；可接 Lago 做客户账单 | 内部账/估算，不是厂商逐请求发票 | 最适合作为 BYOK 双账参照 |
| NewAPI | 预扣估算额度，响应后按实际 token 和倍率补扣/退回 | 渠道余额刷新、价格同步、用量日志 | 内部倍率账 | 余额是风险信号，不是单请求采购真值 |
| One-API | token quota 和倍率计价 | 周期性渠道余额观察 | 内部账 + 余额信号 | 适合基础运营，不足以单独支撑企业发票核销 |
| Portkey | usage × 模型价格；支持自定义价格 | 公开能力以请求成本观测为主 | 请求级估算 | 缺 usage 时必须标记 unknown，不能把 0 当免费 |
| Kong AI Gateway | 配置输入/输出 token 成本并输出日志/指标 | telemetry/Prometheus 为主 | 请求级估算/监控 | 路由观测不等于财务对账 |
| Cloudflare AI Gateway BYOK | best-effort model cost estimate；可配置 custom cost | 官方明确供应商 dashboard 更权威 | 估算 | BYOK 网关不能普遍实时知道原厂最终扣费 |
| Cloudflare Unified Billing | Cloudflare 统一 credits/bill 并代付 | Cloudflare 自身是客户交易主体 | 平台账权威 | 权威性来自商业/资金角色，不来自通用原厂接口 |
| Helicone | 价格库 × usage；direct integration 是 best-effort | 公开能力以请求成本观测为主 | 请求级估算 | “计算准确”不等于供应商财务确认 |
| OpenRouter | 托管调用返回 `usage.cost`/`upstream_inference_cost`，可按 generation ID 查询 | OpenRouter 掌握其平台交易；BYOK 仍可能估算 | 托管交易接近平台逐请求权威 | 若 UniRouter 未来做统一托管计费，必须显式承担 merchant/reseller 责任 |

官方参考：

- [LiteLLM Cost Tracking](https://docs.litellm.ai/docs/proxy/cost_tracking)、[Cost Discrepancy](https://docs.litellm.ai/docs/troubleshoot/cost-discrepancy)
- [NewAPI Rate Settings](https://docs.newapi.pro/en/docs/guide/console/settings/rate-settings)
- [Portkey Cost Management](https://portkey.ai/docs/product/observability/cost-management)
- [Kong AI Proxy](https://developer.konghq.com/plugins/ai-proxy/)
- [Cloudflare AI Gateway Costs](https://developers.cloudflare.com/ai-gateway/observability/costs/)、[Unified Billing](https://developers.cloudflare.com/ai-gateway/features/unified-billing/)
- [Helicone Cost Tracking](https://docs.helicone.ai/guides/cookbooks/cost-tracking)
- [OpenRouter Usage Accounting](https://openrouter.ai/docs/cookbook/administration/usage-accounting)

## 7. 主要模型厂商覆盖矩阵

“当前 UniRouter”只描述源码能力，不把 OpenAI-compatible 推断为厂商生产验收。

| 厂商/产品 | 请求/任务证据 | 成本/账单证据 | 公开逐请求最终扣费 | 当前 UniRouter | 后续路线 |
|-----------|---------------|---------------|----------------------|-------------------|----------|
| OpenAI | response ID、usage、request ID headers | Usage API、日粒度 Costs API、CSV、月度账单 | 未验证 | OpenAI-compatible runtime；无成本连接器 | Wave 1：usage + daily cost + invoice |
| Anthropic | message ID、usage、request-id | Usage/Cost Admin API、账单 | 未验证 | Anthropic-native runtime；无成本连接器 | Wave 1：Admin API + invoice |
| Azure OpenAI | response ID、usage、`apim-request-id` | Cost Details/Exports、发票 | 未验证 | 未验证原生 API version/认证/资源语义 | Wave 2：resource/meter/project 匹配 |
| AWS Bedrock | requestId；可选 invocation logging | CUR、Cost Explorer、发票 | 未验证 | 无 SigV4 原生 adapter | Wave 2：invocation log + CUR |
| Google Vertex AI | responseId、modelVersion、usageMetadata | Cloud Billing BigQuery Export、发票 | 未验证 | 无 Vertex OAuth/project/location adapter | Wave 2：Billing Export 匹配 |
| 智谱 GLM | OpenAI/Claude 兼容 ID 和 token usage | 财务控制台、账单/导出 | 未验证 | 协议可能兼容；厂商 conformance 未验证 | Wave 3：usage + 受控账单导入 |
| MiniMax | token、characters、task/trace ID | 余额、账单、套餐用量 | 未验证 | 文本可能兼容；语音/视频单位不支持 | Wave 3 文本；Wave 4 媒体 |
| Kimi | response ID、usage/cache usage | 账户级 balance API、账单 | 未验证 | 协议可能兼容；账务未验证 | Wave 3：usage + balance drift + bill import |
| DeepSeek | input/output/cache/reasoning token、response ID | balance API、月度 key CSV | 未验证 | 协议可能兼容；账务未验证 | Wave 3：usage + balance + CSV |
| Qwen/阿里云百炼 | OpenAI-compatible/DashScope usage 和 ID | 用量、分钟聚合账单、导出 | 未验证 | 协议可能兼容；DashScope conformance 未验证 | Wave 2：中国云账单优先 |
| 小米 MiMo | OpenAI/Anthropic 兼容 ID、token、部分秒数 | 控制台用量/计费/导出 | 未验证 | 文本可能兼容；多单位不支持 | Wave 3：usage + bill import |
| 豆包/火山方舟 | response ID、final/cache usage | GetInferenceUsage、火山账单/导出 | 未验证 | 协议可能兼容；方舟 conformance 未验证 | Wave 2：小时/日桶匹配 |
| Seedance/火山方舟 | async task ID、状态、时长/帧/用量 | Ark 聚合用量和火山账单 | 未验证 | 当前同步 chat/embeddings 架构不支持 | Wave 4：async media adapter + task ledger |

供应商官方参考：

- [OpenAI Usage](https://platform.openai.com/docs/api-reference/usage)
- [Anthropic Usage and Cost API](https://platform.claude.com/docs/en/manage-claude/usage-cost-api)
- [Azure Cost Details Automation](https://learn.microsoft.com/en-us/azure/cost-management-billing/automate/automation-ingest-usage-details-overview)
- [AWS Bedrock Model Invocation Logging](https://docs.aws.amazon.com/bedrock/latest/userguide/model-invocation-logging.html)、[Cost Management](https://docs.aws.amazon.com/bedrock/latest/userguide/cost-management.html)
- [Google Cloud Billing Export](https://cloud.google.com/billing/docs/how-to/export-data-bigquery)
- [智谱 OpenAI SDK 兼容](https://docs.bigmodel.cn/cn/guide/develop/openai/introduction)
- [MiniMax OpenAI-compatible Text API](https://platform.minimaxi.com/docs/api-reference/text-openai-api)
- [Kimi Balance API](https://platform.kimi.com/docs/api/balance)
- [DeepSeek Balance API](https://api-docs.deepseek.com/zh-cn/api/get-user-balance/)
- [阿里云百炼账单查询和成本管理](https://help.aliyun.com/zh/model-studio/bill-query-and-cost-management)
- [MiMo OpenAI-compatible API](https://mimo.mi.com/docs/api/chat/openai-api)
- [火山方舟 GetInferenceUsage](https://api.volcengine.com/api-docs/view?action=GetInferenceUsage&serviceCode=ark&version=2024-01-01)
- [Seedance/Ark 异步任务查询](https://api.volcengine.com/api-docs/view?action=GetContentsGenerationsTask&serviceCode=ark&version=2024-01-01)

## 8. Four Quadrants

| 象限 | 当前内容 | 控制方式 |
|------|----------|----------|
| Known Knowns | 当前只有两类 adapter；usage 主要是 token；供应商聚合账通常无 gateway request ID；主流 BYOK 网关使用本地估算 | 双账、分级证据、append-only adjustment、禁止聚合账单直接扣客户 |
| Known Unknowns | 企业合同字段、真实账单延迟、折扣/赠金/税费规则、各区域导出稳定性、兼容 API 的错误/取消差异 | connector conformance suite、fixture、正式合同核验、可配置 lag/SLA、默认 unknown |
| Unknown Knowns | 客户财务或运营团队可能已有控制台导出、账号标签和月结匹配经验，但尚未编码为产品合同 | 在正式设计前访谈财务/FinOps/运营，提取现有账期、归因和审批流程 |
| Unknown Unknowns | 新计费单位、供应商补账/撤销、价格结构突变、媒体任务收费点变化、转售角色变化 | 版本化 capability profile、不可变 price contract、可回放 import、kill switch、异常差额隔离 |

## 9. 后续产品待实现功能

以下是本方案明确未覆盖、但用户已要求保留的未来产品能力。它们进入 PRD7 候选池，不进入当前 PRD6 Accepted Scope：

| Candidate ID | 功能 | 建议优先级 | 最低验收证据 |
|--------------|------|------------|--------------|
| D7-ADAPTER-001 | Provider Native Runtime And Conformance Packs：Azure API version/custom auth、AWS Bedrock SigV4、Google Vertex OAuth/project/location，以及中国兼容接口的错误/stream/cache/request-ID 差异 | P1/P2；future required | 每厂商 Chat/Embeddings/stream/error/cancel/request-ID/cache usage fixture；认证轮换；协议漂移；同一 adapter 不能伪装未验证厂商 |
| D7-COST-001 | Provider Cost Reconciliation Control Plane：Provider COGS Ledger、证据状态、聚合匹配、invoice reconciliation、差额调查和 append-only adjustment | P1；future required | 两账守恒；客户账不受 COGS 差额直接影响；重复/乱序 import 幂等；币种/账期/撤销测试；RBAC/audit |
| D7-COST-002 | Provider Billing Connector Packs：OpenAI/Anthropic Admin API；Azure/AWS/GCP/Qwen/Volcengine 云账单；智谱/MiniMax/Kimi/DeepSeek/MiMo 受控导入/余额漂移 | P1/P2 | 每厂商 conformance fixture；分页/回填/重复/缺口/限流/认证失败；账单桶与 invoice readback；无真实凭证进入日志 |
| D7-METER-001 | Generalized Usage Units And Charge Triggers：token、character、audio second、image、video second/frame、request/task；submitted/completed/delivered trigger | P1/P2；媒体前置 | 单位维度和价格版本不可混淆；零值/缺失值；取消/失败/部分交付；多币种舍入；账本守恒 |
| D7-MEDIA-001（承接 P6-F22） | Async Image/Video Job Plane：object reference、job/status/cancel、queue lease、task ledger、delivery proof、最终结算 | P2；future required，实施未接受 | 幂等 completion；取消/退款；进程恢复；对象权限和删除；负载隔离；不发送真实付费任务 |
| D7-BILLING-001 | Managed/Unified Billing：UniRouter 成为托管交易/转售主体，维护统一 credits/bill | P3；需要新的产品、资金和运营决定 | 信用/透支/坏账/预充值/汇率/退款/发票闭环；与 BYOK 明确隔离；不得由本次决定自动接受 |

`D7-COST-001` 可以先作为模块化单体内的独立 domain/module 实现。是否拆为独立 Cost Control Service，必须由吞吐、账单导入负载、故障隔离、团队所有权和独立扩缩容证据触发；本次决定不提前接受微服务拆分。

## 10. 建议分波顺序

1. **PRD6 当前闭环**：P6-F40C key block/operator reconciliation；P6-F41 usage authority 和 metadata-only delivery outcome。
2. **D7 Adapter/Cost Wave 0**：正式 provider capability profile、原生 runtime/conformance contract、Provider COGS schema、import contract、威胁模型和 FinOps 访谈；仍不接真实凭证。
3. **Wave 1**：OpenAI、Anthropic 结构化 Usage/Cost API 与 invoice 对账。
4. **Wave 2**：Azure、AWS、Google 原生 runtime，加上 Qwen/百炼、豆包/火山方舟的资源/项目/SKU/时间桶匹配。
5. **Wave 3**：智谱、MiniMax、Kimi、DeepSeek、MiMo 的 provider conformance、usage、余额漂移和受控账单导入。
6. **Wave 4**：Seedance 和其他视频/语音的多单位计量、async task ledger 与 delivery proof。
7. **Managed Billing**：只有用户另行接受 UniRouter 的 merchant/reseller 角色后才立项。

既有 D7-UPGRADE-001/002 仍是已选定的 PRD7 第一方向；本报告不改变其顺序，也不提前执行 PRD7 或目标环境证据。

## 11. PRD6 验收边界

P6-F40C/P6-F41 可以声称完成的只有：

- 客户收取与供应商成本证据分离；
- 未决 attempt 对受影响 key 的安全 admission 控制；
- 平台授权、租户安全、有限动作的 collect/refund/bounded subsidy；
- `collect` 只能使用权威 usage/delivery 与冻结客户价；
- 显式 zero 与 missing usage 不混淆；
- metadata-only delivery receipt；
- 无原始 prompt/response retention；
- 本地测试、迁移、分支、PR 和 required CI 证据。

P6-F40C/P6-F41 不能声称：

- 已实时知道所有供应商最终扣费；
- 已完成任一真实供应商账单连接器；
- 已完成云账单、余额或发票导入；
- 已支持 Seedance/视频异步任务；
- 已支持通用多单位计量；
- 已完成独立 Cost Control Service；
- 已完成托管统一计费或 merchant/reseller 能力；
- 已获得 target 或 Production verified 证据。

## 12. 复核触发条件

出现以下任一情况时，应更新本报告和 PRD7 候选，而不是静默修改现有结论：

1. 某供应商公开按 request/task ID 返回最终财务扣费的正式 API；
2. UniRouter 获得包含专属账单字段的企业合同或私有接口；
3. UniRouter 决定成为托管交易/转售主体；
4. 选定第一个正式视频/语音供应商和计费单位；
5. provider reconciliation 的导入吞吐、故障域或组织所有权证明需要独立服务；
6. 正式 PRD7 启动 D7-ADAPTER-001、D7-COST-001、D7-COST-002、
   D7-METER-001、D7-MEDIA-001 或 managed billing 的实施批次。
