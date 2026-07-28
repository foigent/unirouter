# Foigent Event Connector 协议 Pin 与 Reference Runtime

## 当前绑定

UniRouter 产品侧固定使用以下不可变协议权威：

- repository: `https://github.com/foigent/event-connector-spec`
- tag: `v1.1.0`
- commit: `434feebdf4865a876756c6b2faddd92e7a9854f9`
- tree: `93f942d8e6d4b809b4bef1878fe28bf7410c3506`
- content digest:
  `sha256:3067581f2012c1239dbee1959facbc77ab1b18dc3bfc82172f57c3be894d09c5`
- release digest:
  `sha256:3bb10a544ace17d3b6c4a44e19048c45c557eba19ad79d9cea81728336658c4d`

`contracts/foigent/event-connector-protocol.pin.json` 是产品 CI pin；
`contracts/foigent/connector-registry.pin.v1.json` 是 UniRouter 本地运行时 registry
pin。两者用途不同，不得相互替代。

## 产品所有权边界

UniRouter 只实现并运行
`foigent.unirouter.completed-execution@1.0.0` Reference Connector。它从
UniRouter 已持久化的 completed-execution Source bytes 生成共享
`model.request.completed` Envelope，并保留现有 Feed、Cursor、鉴权、保留期和
确认语义。

协议 v1.1.0 新增的 LiteLLM manifest、upstream mapping、compatibility
matrix、conformance contract 和跨语言 vectors 只进入产品 pin 与共享 CI
验证。它们证明 UniRouter 所依赖的共享协议仍可被第二种 Source Connector
实现，不表示 UniRouter：

- 注册或运行 LiteLLM Connector；
- 连接、部署或管理 LiteLLM；
- 持有 LiteLLM 凭据、Checkpoint、Cursor 或数据；
- 承担 LiteLLM 到 X-Insight 的运行时责任。

UniRouter 本地 registry 必须继续只包含
`unirouter.completedfeed.reference-v1`。LiteLLM 运行时属于对应 Connector
owner 和 Consumer 产品的独立隔离运行时。

## 兼容性不变量

协议 authority 从 v1.0.0 更新到 v1.1.0，但 Reference Connector 的下列内容
保持不变：

- Connector manifest；
- completed-execution Source Schema；
- Event Envelope Schema；
- `model.request.completed/v1` Schema；
- UniRouter Source projection、lifecycle 和 identity mapping；
- canonical vectors 与 Reference conformance contract；
- settled/cancelled Golden Source bytes、Envelope bytes、source hash、
  envelope hash 和 idempotency key。

因此 `ReferenceRuntimeBinding` 只更新协议 version/commit/release identity；
Connector version 仍为 `1.0.0`。协议身份不进入 Source row、Feed item、Cursor、
Checkpoint、billing record 或 Event hash。

## CI 与变更门

`.github/workflows/foigent-protocol-conformance.yml` 必须：

1. 以完整 commit SHA 固定共享 Action；
2. 校验协议 release、三方批准证据和 UniRouter 产品 pin；
3. 运行共享 Reference 与 LiteLLM Go/TypeScript Conformance Kit；
4. 运行 UniRouter 三路径 Reference differential、Golden identity、delivery
   state exclusion、registry 和 runtime binding 回归。

任何未来协议升级若改变 Reference Connector 的 manifest、Source/Event
Schema、mapping、canonical vectors 或 hash/idempotency identity，必须作为
新的兼容性决策处理，不能只更新 pin。

## 非运行时变更

本次 pin 更新不修改数据库、迁移、凭据、Feed wire、Cursor、Checkpoint、
部署配置、流量或 Development/Production 状态。
