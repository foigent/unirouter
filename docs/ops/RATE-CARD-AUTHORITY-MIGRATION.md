# Rate Card 请求定价权威迁移

`REQUEST_PRICING_AUTHORITY` 是升级闸门，不是隐式回退开关：

- `legacy`（客户部署默认）：继续使用 `model_prices × org_model_prices.multiplier`，避免旧部署升级后立即停流。这是临时兼容阶段，不是最终权威。
- `rate_card`：每次请求只读取已激活的版本化 Rate Card；合同价优先于目录价，缺失、不完整或同层 provider 歧义一律失败关闭，绝不读取旧价格表兜底。

Rate Card 可选择全天同价，或提交 UTC 日内价带。日内价带必须完整覆盖 `00:00-24:00`、互不重叠、不得跨午夜，每段都显式给出输入、输出、缓存读取、缓存写入四项 CNY 微元价格。UniRouter 不推断本地时区、峰谷折扣或缺失时段；请求按 `priced_at` 的 UTC 分钟选择唯一价带，并把完整 schedule、选中价带、Rate Card ref/version、有效期和组件哈希共同冻结为 `rcq2_...` 计价版本。旧的无价带 Rate Card 继续按全天同价处理。

供应商价格必须按当前 Rate Card 选择维度判断是否可精确表达：

- 固定 Token 价格（例如当前 Kimi 目录价）使用全天同价。
- 按每日时段变化的价格（例如 DeepSeek 北京时间峰谷价）必须先换算成完整 UTC 日内价带，不能保存成一个平均价或“当前价”。
- 依赖请求长度、服务等级、套餐余额或其他请求维度的价格（例如 MiniMax M3 的上下文长度分档与 priority 倍率）目前不能由仅按 `model_ref + priced_at` 选择的 Rate Card 精确表达。此类模型在补齐对应选择维度前，不得把某一档按量价标成通用官方价格。
- Token Plan/订阅 Key 与按量 API Key 是不同的经济合同。客户选择零收费可以创建显式零价合同草稿，但 `0` 不得标记为供应商官方按量目录价，也不代表订阅配额没有成本或上限。

模型执行 Profile 与价格权威相互独立。执行 Profile 冻结 tokenizer、上下文、输出、推理、缓存用量和协议语义；价格变化只产生新的 Rate Card，不要求升级执行 Profile。创建已知模型时可以展示仓库已确认的官方价格建议，但模型治理写权限不得代替财务写权限：任何自动化最多在 Billing Admin/Platform Admin 明确操作后生成带来源证据的 `provider_list` 草稿，不能自动批准、激活或覆盖客户合同价。用户修改价格应创建新的合同价/目录价草稿并走同一 maker-checker 与 supersede 流程。

缓存 token 分类同样不能由价格表或 provider 名称推断。Kimi 当前 Chat API 在
`usage.cached_tokens` 顶层报告缓存命中；只有模型执行计划选择了冻结的 Kimi
cache-usage profile 时，该字段才映射为 `cache_read_tokens`，其余
`prompt_tokens - cached_tokens` 映射为普通输入。未选择该 profile、字段缺失、跨
schema 重复报告或分区不一致时，不得应用缓存折扣。这样 Rate Card 只负责给已经
证明的 token 类定价，不负责解释供应商响应格式。

安全切换顺序：

1. 保持 `REQUEST_PRICING_AUTHORITY=legacy`。
2. 调用 `POST /api/v1/enterprise-finance/rate-cards/bootstrap-legacy-drafts` 将旧价格及当前客户倍率转换为带哈希来源证据的 `provider_list` 草稿；缺少稳定 provider 的模型会被跳过。
3. 对草稿完成 maker-checker 复核与激活。必须在来源证据 `stale_after` 之前完成激活；若过期，重新生成草稿以记录新的受控读取时间和证据。也可通过 `import-official-draft` 导入 HTTPS 官方来源证据；导入永远只生成草稿。
4. 将全部实际路由模型的 `model_ref` 提交给 `POST /api/v1/enterprise-finance/rate-cards/preflight`。只有响应 `ready=true` 才进入下一步。
5. 设置 `REQUEST_PRICING_AUTHORITY=rate_card`，运行客户部署 preflight 并重启服务。
6. 在“费率与计价”确认“Rate Card 严格模式”。

官方来源的 `stale_after` 是激活闸门：`stale_after <= activated_at` 时拒绝激活，但过期或后续导入失败都不会改写当前 active Rate Card。

持续换价仍使用新 ref 的 `draft → 独立 approve`。若它与唯一的同模型、同经济层 active 费率冲突，激活请求必须同时提交旧费率的精确 `supersedes_rate_card_ref` 和 `expected_superseded_version`。服务端在同一事务中为旧 ref 追加 `retired` 版本、再激活新 ref；错 ref、错版本、多个冲突、未生效候选、过期来源或任意写入失败都会全部回滚。旧 active 版本保留，用于验证已冻结请求的不可变快照。
