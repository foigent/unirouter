# PRD7 Cache Service Candidate（方案 B 完整未来能力）

> Date: 2026-07-11
> Status: Future required capability / target architecture accepted; implementation not accepted
> Decision: 用户接受将完整方案 B 作为 UniRouter 后续必须补足的能力，目标形态为
> 可独立部署的 Cache Service。该决定只登记未来产品方向和边界，不授权当前 PRD6
> 实现、部署、回答持久化、数据范围扩大、语义缓存或目标环境变更。

## 1. 结论与边界

方案 B 的完整形态属于 UniRouter 模型推理数据平面，不属于 Tool Gateway、IAM、
业务数据库或导出系统。Cache Service 位于当前身份/权限校验之后、provider 路由
之前；命中结果仍须经过当前输出策略、计费和审计。Tool、数据库写入、文件导出、
发信等副作用由实际 action owner 自己做授权和幂等，不能用模型回答缓存代替。

目标架构同时支持两种交付形态：

- 私有化 2B：一个客户环境部署一个 Cache Service，可使用客户选择的 Redis、对象
  存储和 KMS/密钥设施；
- 托管 SaaS：同一服务合同部署在 tenant-assigned regional cell 中，按租户、地域、
  密钥和容量隔离；不把一个全球共享缓存变成所有租户的共同故障域。

“独立 Cache Service”是最终可拆分边界，不表示仅靠微服务化即可获得 SaaS 能力，
也不表示当前低负载必须立即拆分。服务拆分必须由下文容量或隔离触发器和压测证据
驱动。

## 2. 请求时序

```text
Client / Agent
  -> UniRouter authentication and current authorization
  -> input guardrail and cache eligibility
  -> Cache Service exact lookup
       hit  -> current output guardrail -> cache-aware billing/audit -> response
       miss -> routing/failover -> provider -> output guardrail
            -> settle authoritative usage -> encrypted cache write -> response
```

以下请求首版必须 bypass cache：

- 流式返回；
- 请求包含 `tools`、`tool_choice` 或任何可能触发副作用的 action contract；
- 响应包含 `tool_calls`；
- 图像、音频、视频、大对象或异步 job；
- 当前 IAM、租户、地域、模型、价格或 guardrail 版本无法确定；
- 客户发送 `no-store`，或策略判定内容不允许持久化；
- 无法证明现有调用者仍有权读取创建该缓存条目时对应的数据范围。

语义缓存不是精确缓存的自动扩展。它必须在精确缓存安全闭环后，以独立 decision、
投毒/等价性 red team、误命中损失阈值和可撤回验收重新立项。

## 3. 服务职责

### 3.1 Data Plane

- 对规范化请求生成 keyed HMAC，不保存明文 cache key；
- key 至少绑定 tenant、project/授权域、endpoint、模型与 adapter 配置版本、价格版本、
  guardrail 版本、地域/驻留域和规范化请求指纹；
- 执行 exact lookup、TTL、negative/tombstone check、singleflight/lease 和 stampede
  protection；
- 只返回已完成、未过期、未被 purge、版本仍兼容且当前调用者获授权的条目；
- cache 不可用时允许安全 bypass 到 provider，但不得 bypass IAM、Guardrail、预算、
  admission 或计费真值；
- cache write 失败可以使本次调用以 `cache_store_degraded` 完成，但必须产生有界、
  脱敏的降级证据，不能把未写成功报告为命中。

### 3.2 Control Plane

- tenant/project/endpoint/model 级默认关闭与 opt-in；
- TTL、最大条目、最大租户容量、地域、模型/策略版本和 eligibility policy；
- RBAC、双确认 purge、租户全量 purge、按 key/model/policy version purge；
- cache 命中、绕过、过期、淘汰、写失败、purge 和读取审计；
- 加密密钥版本、轮换、撤销和不可读旧条目处置；
- 容量、命中率、成本节约、错误、延迟和 noisy-neighbor 指标。

### 3.3 Storage Plane

- Redis/兼容 KV：短 TTL 索引、租约、singleflight、tombstone 和 hot metadata；
- 加密对象存储或加密 KV：回答正文；小规模私有化部署可选择单一后端，但接口不得
  假设本地文件系统；
- PostgreSQL：只保存控制配置、purge/audit ledger 和必要元数据，不把大响应正文写入
  当前事务热路径；
- KMS/客户密钥或受支持的本地密钥提供方：envelope encryption 和版本化轮换。

## 4. 安全、计费与一致性不变量

1. 不允许跨 tenant、region/residency、authorization epoch 或 policy version 命中。
2. plaintext prompt/response、Authorization header、provider key 和原始
   `Idempotency-Key` 不进入日志、指标或 cache index。
3. purge 先写不可逆 tombstone，再异步物理清除；物理清除未完成时也不得再次返回。
4. 同一未命中请求只允许一个 owner 调用 provider；等待者必须有超时和有界数量。
5. cache hit 的 provider request count 和 provider cost 为零，但原始 token 数、客户
   cache price/ratio、节省金额和账本处理必须使用版本化真值。
6. 旧回答不能绕过当前 IAM 或 Guardrail。策略/权限 epoch 变化时条目失效或重新检查。
7. tool/data/export 副作用的 idempotency key 由 action owner 管理；Cache Service 不
   声称防止它看不到的副作用。

## 5. 当前容量事实（不是最终产品上限）

截至本文件建立时，仓库证据只支持以下结论：

- Chat、Streaming Chat 和 Embeddings 已由 P6-F21 源码改为 Redis 原子的跨副本
  加权容量：默认平台 100 单位、组织 10 单位，普通/Embedding 权重 1、流式权重
  4，并分别保留 10/2 个短请求单位。增加 gateway 副本不会放大额度；旧
  `AdmissionController` 只保留在 `/v1/models` 等轻量本地保护路径，不能再作为模型
  调用容量证据。源码：`internal/workloadadmission/`、`internal/limitpolicy/service.go`、
  `cmd/uniroute/main.go`；目标容量仍未验证；
- 默认 PostgreSQL pool 为每进程 25，Redis pool 为每进程 10；副本增加会按副本累加
  后端连接。证据：`internal/config/config.go:212-219`、`cmd/uniroute/main.go:53-76`；
- trace event 当前在请求路径同步写 PostgreSQL，一次正常调用会产生多个 event。证据：
  `internal/gateway/trace.go:204-243`；
- 历史 2c4g 目标只验证 HTTP endpoint 在 50 并发以下稳定、80 并发发生 timeout；
  gateway/provider 场景只验证 5 并发、6.4 QPS 稳定，10 并发场景受 10 RPS/key
  限制。该旧目标证据不等于当前源码的最终容量。证据：
  `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md:188-200`；
- Compose 默认是一个 backend、一个 PostgreSQL、一个 Redis；canary overlay 是发布
  验证形态，不是经过证明的 HA/大规模拓扑。

因此目前不存在可诚实声明的“超过 N QPS 必须拆微服务”的 Production verified 数字。
必须在固定模型延迟、流式占比、Agent fan-out、缓存命中率、响应大小、实例资源、
数据库/Redis 拓扑和 SLO 后重新压测。

## 6. 容量推演与拆分触发线

并发近似等于 `每秒新请求数 × 平均请求持续秒数`。以历史已测的 6.4 gateway QPS
作为纯推演基准，100 倍约为 640 RPS：

| 平均调用时间 | 640 RPS 对应的在途请求 | 当前权重下所需共享容量单位 | 能否从该数字推导副本/DB 连接 |
|--------------|------------------------|----------------------------|-------------------------------|
| 5 秒普通调用 | 3,200 | 约 3,200 | 不能，需 CPU、内存、pool wait 和 upstream 延迟压测 |
| 30 秒长流 | 19,200 | 约 76,800（流式权重 4） | 不能，且该形态远超当前已验证证据 |

这不是部署建议，而是说明默认 100/10 是保护阈值，不是容量声明。P6-F21 解决了
“复制进程就复制额度”的正确性问题，但不会凭空增加 PostgreSQL、Redis、CPU、内存
或 upstream 容量。要提高共享阈值，必须先完成 durable finalization/outbox、固定
workload 的多副本压测和故障证据；拆 Cache Service 也不会自动修复这些问题。

只看“客户每秒发多少个根请求”也会低估 Agent 和长任务压力。近似计算为：

```text
有效上游 RPS = 根工作流 RPS × 每工作流平均模型节点数 × 每节点平均 attempt 数
在途请求数   = 有效上游 RPS × 平均 attempt 持续秒数
```

例如 10 个根工作流/秒、每个工作流 20 次模型调用、每次因 retry/failover 平均 1.5 个
attempt，会形成约 300 个上游 attempt/秒；平均 10 秒即约 3,000 个在途 attempt。
当前默认共享阈值会明确拒绝超额流量，而不是通过增加副本绕过。需要多少副本只能由
实测服务时间、pool wait、CPU/内存和 SLO 决定。反过来，视频/长连接即使只有 2 个
请求/秒，只要平均持续 120 秒，也已有约 240 个在途请求；视频仍应使用未来异步 job
plane，而不是直接放宽同步流式阈值。因此拆分评估必须同时看 RPS、持续时间、Agent
fan-out、attempt 放大、流式比例和单租户集中度。

以下数字是未来容量规划触发器，不是已经验证的上限：

| 阶段 | 规划指标 | 处理方式 |
|------|----------|----------|
| 保持 modular monolith | 持续总流量低于约 100 RPS、在途请求低于约 500、单地域且少于 5 个 gateway 副本；cache 尚未占 gateway 资源的 20% | 先完成 PRD6 分布式正确性；保持明确 Cache interface，不为拆分而拆分 |
| 准备拆分 | 持续 100–500 RPS、500–2,000 在途请求、5–10 个副本、cache hot set 约 10–50 GB，或 cache 消耗 gateway CPU/内存达到 20–30% | 完成独立 service contract、双运行/影子验证、容量和故障注入，不直接切换生产 |
| 建议独立 Cache Service | cacheable lookup 持续超过约 500–1,000 RPS、在途请求超过约 2,000、超过 10 个 gateway 副本、hot set 超过约 50 GB，或需要多地域/独立 tenant cell | 独立伸缩、连接池、存储分片、租户配额、purge ledger、SLO 和故障域 |

无论 RPS 多少，出现以下任一结构性条件也应拆分：

- SaaS 多地域或数据驻留要求不同租户进入不同 cell；
- 一个租户可持续占用 cache 30% 以上资源，需要 dedicated capacity；
- cache 故障、GC、purge 或扩容已影响 gateway 的非缓存请求 SLO；
- cache 的发布、回滚、密钥轮换和容量周期需要与 gateway 独立；
- cache p99、pool wait、eviction、stampede 或 purge lag 连续越过正式 SLO，且单纯
  横向扩 gateway 不能消除。

正式拆分决策必须由同一 workload 在至少 1/2/多副本拓扑下的 load、soak、故障注入
和成本证据支持。上述 planning band 只能用于决定何时开始验证，不能用于对客户作
容量承诺。

## 7. SaaS 复用结论

Cache Service 拆分后可以被 SaaS 版本复用，但“成为微服务”只是必要的可选构件，
不是 SaaS-ready 证明。可复用合同从第一天必须包含：

- mandatory tenant/project/authorization/policy/region identity；
- tenant-scoped encryption key、quota、TTL、purge 和审计；
- 无本地文件状态，存储/KMS/队列均为可替换 adapter；
- bounded timeout、retry、circuit breaker、backpressure 和 fail-safe bypass；
- API/schema/version compatibility、mixed-version read/write 和 rolling rollback；
- per-tenant metrics 采用受控基数，正文和密钥不进入 telemetry。

建议的 SaaS 形态是 cell-based，而不是一个全球共享单体：

```text
Shared SaaS control plane
  -> tenant placement / policy / release metadata

Regional Cell A                    Regional Cell B
  Gateway replicas                  Gateway replicas
  Cache Service replicas            Cache Service replicas
  Redis/KV + encrypted object store Redis/KV + encrypted object store
  billing/outbox data boundary      billing/outbox data boundary
```

每个 tenant 被分配到一个 cell；高价值或强隔离客户可以进入 dedicated cell。这样同一
Cache Service 代码和合同可复用于私有化与 SaaS，但 SaaS 仍须另外完成 control/data
plane 分离、tenant provisioning、HA/DR、autoscaling、region residency、KMS、零停机
migration、SLO/incident、abuse protection 和生命周期删除等能力。

## 8. 未来实施 batches

| Batch | 内容 | 进入门槛 | 最低退出证据 |
|-------|------|----------|--------------|
| D7-CACHE-0 Value/Threat Validation | 无正文 workload 统计、命中率/成本模型、data class、threat model、SLO 与 backend 选择 | PRD6 P0/P1 correctness foundation 完成 | 真实 workload envelope；默认关闭；数据/删除/计费决策；不持久化真实回答 |
| D7-CACHE-1 Exact Cache Service | 独立服务合同、exact key、加密存储、TTL/purge、singleflight、billing/audit | CACHE-0 接受；选择 Redis/KV、object store、KMS | 跨租户/权限/版本负例；purge/tombstone；stampede；故障 bypass；账本守恒；required CI |
| D7-CACHE-2 Private/SaaS Deployment Profiles | 私有化单客户 profile 与 SaaS regional-cell profile，共享同一 contract | CACHE-1 正确性通过；选定首个拓扑 | 1/2/多副本 load/soak；rolling compatibility；cell isolation；backup/restore；无 Production claim 除非目标验证 |
| D7-CACHE-3 Semantic Cache Decision | 语义索引、相似度策略、poisoning/equivalence controls | Exact cache 已运行并证明价值；用户另行接受 | ground-truth fixture、误命中损失、投毒/越权/撤回、abstention、人工可解释性 |

## 9. 明确非授权事项

本文件不授权：

- 当前 PRD6 写入或读取 prompt/response cache；
- 部署 Cache Service、Redis、对象存储、KMS、消息队列或 SaaS cell；
- 扩大数据库、导出、retention、凭证、真实用户数据或付费请求范围；
- 把语义相近解释成同一业务操作；
- 缓存或重放 Tool/数据库写入/导出等副作用；
- 将 planning band 宣称为 Target verified 或 Production verified 容量。
