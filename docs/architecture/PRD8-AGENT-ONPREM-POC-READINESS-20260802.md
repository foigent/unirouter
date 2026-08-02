# PRD8 Agent 与本地化 POC 就绪性补充合同

> Work item: `UR-PRD8-AGENT-ONPREM-POC-READINESS`
> Date: 2026-08-02
> Status: implementation contract
> Parent product contract: `docs/PRD-Phase8.md`

## 1. 目标与边界

本补充合同服务于第二次真实联调和后续客户现场 POC。它不改变 PRD8
已经冻结的产品边界：UniRouter 仍然只拥有模型请求接入、模型执行策略、
Provider 路由、模型凭证、限额、用量、成本、结算终态和自身 Source 事实。

本轮增加的是长期产品能力，而不是一次性联调脚本：

1. 把调用方从“一个没有业务归属的 API Key”提升为可治理的
   `WorkloadApplication`；
2. 把客户端协议、网关端点和部署网络拆成独立的版本化 Profile；
3. 用同一个后端可用性解析器解释应用、凭证、授权、限额、模型和上游通道；
4. 为 OpenAI SDK、OpenAI Agents SDK、LangChain、AutoGen、OpenClaw 和
   Tencent WorkBuddy 提供可验证的接入配置；
5. 为飞书企业机器人和飞书 Aily 预留隔离 Adapter，而不把消息平台逻辑放入
   Gateway 请求进程；
6. 为 Compose 本地化部署提供显式 Gateway Endpoint/TLS desired-state，架构上
   保留 Helm/Kubernetes、私有 CA、外部负载均衡和离线环境的演进空间。

本轮不实施 X-Insight 调查能力、工具执行、MCP Session/OAuth，也不让外部系统
进入模型请求关键路径。

## 2. 产品对象

| 对象 | 含义 | 权威边界 |
|---|---|---|
| `WorkloadApplication` | 一个企业应用、Agent、工作流或隔离集成 Adapter | UniRouter 组织/项目域 |
| `Credential` | 证明调用身份的 API Key；完整密钥仍只显示一次 | UniRouter IAM；不是应用本身 |
| `ModelAuthorization` | 该身份可以调用哪些模型和模型端点 | 只可缩窄现有权限 |
| `LimitPolicy` | RPM/RPS/TPM/并发与预算类执行约束 | 独立于身份和凭证 |
| `ClientProfile` | 客户端库、版本、API 风格、配置模板与证据等级 | 兼容性合同，不授予权限 |
| `EndpointProfile` | `/v1/chat/completions` 等线协议及运行态 | UniRouter 模型端点合同 |
| `GatewayEndpoint` | 调用方可见的 Base URL、网络区、TLS 和信任引用 | 部署 desired/observed state |
| `IntegrationInstance` | X-Insight Reference Connector 或隔离消息/目录 Adapter | 各自隔离运行时；不是万能插件 |
| `ConformanceResult` | Profile 与指定 UniRouter 版本的离线/在线证据 | 由测试生成，不能人工改成“已认证” |

一个 API Key 最多绑定一个 `WorkloadApplication`。已有 Key 保持
`legacy_unbound` 兼容；新建服务/Agent Key 应在同一创建事务中绑定应用。归档
应用只停止新调用，不删除 Request Receipt、账本、审计或 Source 事实。

## 3. 可用性唯一真相

UI 不再分别用“通道状态”“凭证状态”猜测模型是否可用。后端解析：

```text
ClientProfile
∩ EndpointProfile
∩ WorkloadApplication
∩ ApplicationCredential
∩ ModelAuthorization
∩ LimitPolicy
∩ ModelGovernance
∩ Routing Channel
∩ ProviderCredential
```

每个轴只返回 `ready`、`blocked`、`context_required` 或
`not_applicable`，并携带稳定原因码、修复入口和证据引用。总体状态由后端按
fail-closed 规则计算。没有指定模型时，路由和模型授权显示
`context_required`，不得伪装成可用或不可用。

该解析器必须解释以下已知矛盾：供应商凭证虽然启用，但关联通道被禁用、熔断、
冷却、模型不匹配或没有任何合格 Key 时，最终状态为 blocked，并指出具体轴。

## 4. 客户端兼容等级

兼容等级是测试证据，不是营销开关：

| 等级 | 含义 |
|---|---|
| `certified` | 固定客户端版本通过官方或双方签署的完整 POC 套件 |
| `verified` | 固定客户端版本通过仓库零网络套件和目标环境在线验证 |
| `protocol_compatible` | 配置和线协议兼容，尚未完成目标环境验证 |
| `unsupported` | 当前 Endpoint/Profile 不支持或明确冲突 |

OpenAI Agents SDK 在本轮使用 Chat Completions 适配路径；其默认 Responses 路径
保持 `contract_only`，直到 `/v1/responses` 有独立运行时、计量、终态和兼容证据。
不得为了联调伪装完整 Responses 支持。

## 5. UI 信息架构

保留六个一级工作区，全部二级菜单常驻展开。系统与集成最终为：

- 部署与网络
- 集成管理
- API 与兼容性
- 商业授权

访问与安全中的“访问凭证”更名为“应用接入”，页面标题为“应用与 Agent 接入”。
它是一个任务工作区而不是把所有领域混成一张表：左侧/主区选择应用，页面展示
Owner、项目、环境、Client Profile、绑定凭证、模型授权、限额、调用地址、配置
片段和自检；右侧固定 Inspector 始终显示最终可用性和全部阻塞原因。

“集成管理”展示 X-Insight Reference Connector、飞书企业机器人 Adapter、飞书
Aily Adapter 及未来目录/ERP Adapter。未实现项必须标记规划中或需要环境，不得
生成虚假开关或假健康状态。

## 6. 部署与 TLS

首个交付形态仍为 Compose appliance，但数据模型和操作合同不绑定 Compose。

- `GatewayEndpoint` 保存 Base URL、Audience、网络区和非敏感证书引用；
- TLS 私钥不进入 UI、数据库或审计明文；
- UI 只管理 desired state 并读取 observed state；
- 应用配置由 Operator/CLI/Compose/Helm 执行，必须支持 plan、验证和回滚；
- ECS 开发环境可以使用公网 HTTPS，客户 POC 支持内部 DNS、私有 CA、外部 LB、
  代理或 air-gap；
- Developer Console 必须读取默认 Gateway Endpoint，不能假定
  `window.location.origin` 就是模型调用地址。

## 7. 数据重置边界

第二次联调开始前的清空是显式 Operator/CLI 工作流，不提供日常 UI 按钮。工作流
必须先 dry-run，生成备份和 SHA-256，列出精确作用域，再执行受控清空、迁移、
bootstrap 与 readback。Provider 通道/凭证也在届时重建；部署密钥、TLS 私钥和
其他 deployment secrets 不因业务数据重置自动删除。

如果数据库/Outbox/Cursor epoch 被重建，必须生成新的显式 Source Instance；
禁止默认值、空值回退或复用旧 Source 身份。

## 8. 兼容性不变量

实现和发布必须证明以下行为不变：

- `/v1/chat/completions`、`/v1/embeddings`、`/v1/models` 和现有媒体任务合同；
- 请求记录、请求身份、调用计量、成本和结算终态；
- API Key 原有验证、租户和项目边界；
- Outbox 身份、completed-execution DTO/Source bytes/hash；
- Reference Connector Feed API、凭据、排序和 Cursor；
- X-Insight、Tool Gateway 和 MCP Gateway 不进入模型请求关键路径。

## 9. 验收顺序

```text
APP-1 应用领域与迁移
→ APP-2 管理 API、审计、Key 绑定和可用性解析
→ UI-6 导航与应用任务工作区
→ DEPLOY-1 Gateway Endpoint/TLS desired-observed state
→ COMPAT-1 客户端配置、零网络 Conformance 和自检
→ INTEGRATION-1 隔离 Adapter 清单与真实非声明状态
→ RESET-1 仅 dry-run 的受控重置工具
→ 全量 Go/前端/迁移/浏览器/Compose/CI
→ 现有 ECS 非数据清空发布与 HTTPS 验证
```

真实业务数据清空、新 Provider 凭据和第二次联调流量仍等待用户明确的“开始联调”
指令。
