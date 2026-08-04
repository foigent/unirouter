# Foigent Event Connector 协议 Pin 与 Reference Runtime

## 当前绑定

UniRouter 产品 CI 固定使用以下不可变协议权威：

- repository: `https://github.com/foigent/event-connector-spec`
- tag: `v2.0.2`
- annotated tag object: `1fece056b251239b8a557264c5d95043c4b5bdd2`
- commit: `ad8fe7a38c9d7335657630baa8670dd07556b98c`
- tree: `288fa23b0591b6ed2953e14bc4a9d3571e2839ac`
- content digest:
  `sha256:9dce6081e15be0028441513291808f8d58becb25f00887ae66299031bc79c1c0`
- release digest:
  `sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8`

`contracts/foigent/event-connector-protocol.pin.json` 是产品 CI pin；
`contracts/foigent/connector-registry.pin.v1.json` 是 UniRouter 本地运行时 registry
pin。两者用途不同，不得相互替代。

协议 `v2.0.2` 是共享协议的 PATCH-compatible CI portability release：远程
composite Action 在产品 snapshot 中不需要 `.git`、权威仓历史或产品侧私有仓
凭据。`v2.0.2` 相对 `v2.0.1` 不修改 Event、Schema、vector、mapping、M0、
Gap、provenance、identity、hash、canonicalization、Receipt、Cursor 或
Checkpoint 语义。

Reference Connector 的 manifest、Source Schema、Source projection 和既有
Golden identity/hash 均未变化，因此本地 runtime registry 与
`ReferenceRuntimeBinding` 继续保持已经验证的 `v1.1.0` 身份；产品级 pin
升级不重写运行时描述符。

## 产品所有权边界

UniRouter 只实现并运行
`foigent.unirouter.completed-execution@1.0.0` Reference Connector。它从
UniRouter 已持久化的 completed-execution Source bytes 生成共享
`model.request.completed` Envelope，并保留现有 Feed、Cursor、鉴权、保留期和
确认语义。

协议 v2 的 source-neutral Envelope、Manifest、Lifecycle、Checkpoint、Error、
Receipt、兼容矩阵和跨语言 vectors 只进入产品 pin 与共享 CI 验证。它们证明
UniRouter 的 Reference Connector 与 X-Insight Consumer 使用同一不可变协议
权威，不表示 UniRouter：

- 注册或运行 LiteLLM Connector；
- 连接、部署或管理 LiteLLM；
- 持有 LiteLLM 凭据、Checkpoint、Cursor 或数据；
- 承担 LiteLLM 到 X-Insight 的运行时责任。

UniRouter 本地 registry 必须继续只包含
`unirouter.completedfeed.reference-v1`。LiteLLM 运行时属于对应 Connector
owner 和 Consumer 产品的独立隔离运行时。

## 兼容性不变量

产品 CI authority 更新到 v2.0.2，但 Reference Connector 的下列
内容保持不变：

- Connector manifest；
- completed-execution Source Schema；
- Event Envelope Schema；
- `model.request.completed/v1` Schema；
- UniRouter Source projection、lifecycle 和 identity mapping；
- canonical vectors 与 Reference conformance contract；
- settled/cancelled Golden Source bytes、Envelope bytes、source hash、
  envelope hash 和 idempotency key。

因此 `ReferenceRuntimeBinding`、本地 registry 和 Connector version 均保持
原身份；Connector version 仍为 `1.0.0`。产品 CI pin 的协议身份不进入
Source row、Feed item、Cursor、Checkpoint、billing record 或 Event hash。

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
