# X-Insight Connector 控制凭据生命周期（1.9.2 / M125）

适用对象是**已经完成原邀请兑换和正常 setup 的同一个连接**。其原 `installation_id`、`deployment_id`、控制项目、完成事件范围、来源绑定、双向 HTTPS origin、`control_fence`、历史操作及 ACK 保持原值。本功能不创建新连接，不扩大原授权，也不恢复已经撤销的连接。当前源代码中的 [OpenAPI](../tech-ref/05-openapi-spec.yaml) 定义 UR 管理员接口；XI 的控制端点由 XI 自身的合同和鉴权实现负责。此文档描述源代码交付行为，不代表任一 Target 已升级。

## 正常轮换

首次兑换得到的控制 bearer 有效期为 24 小时。剩余不超过 6 小时时，现有单线程 Connector 运行循环将同连接列为待轮换，先在 M125 日志中加密保存新 bearer、准确请求字节和摘要。UR 先向原 XI origin 查询准确 operation；XI 尚未记录时才发送同一 `rotate` 请求，随后用新 bearer `confirm`。得到与 operation、旧/新摘要和原范围匹配的已提交回执后，UR 才以条件更新晋升本地 current bearer。同一循环不执行该连接的普通 Poll/ACK；下一循环重新发现后使用新 bearer。发送或响应丢失时，继续查询并从持久密文重放同一请求；不得另造 bearer、重新兑换邀请或延长旧 bearer。

XI 固定控制端点为 `POST /api/v1/connector-control/installations/{installation_id}/credentials/rotate`、`GET /api/v1/connector-control/installations/{installation_id}/credentials/operations/{operation_id}` 和同一 operation 下的 `POST .../confirm`。这些是服务间调用；管理员不手工构造请求或复制 bearer。普通 Poll/ACK 的有效期和原鉴权要求保持严格。

## 离线跨期恢复

当旧 bearer 已过期且没有可直接晋升的已提交回执时，XI 管理员必须在原连接上发起有时限的恢复票据；UR 当前及原 setup 保存的管理员权限也须重新成立。管理员从 XI 打开 UR 的原连接恢复页；UR 先仅凭全局唯一安装 ID 在本地定位原业务组织并重验权限，**这一步不向 XI 发送票据**，也不把登录用户的技术组织当作业务组织。之后管理员核对预览中的双向 origin、安装、控制项目、完成事件范围、票据期限和本地凭据状态，再确认。UR 只接受原固定 XI origin 返回的票据解析结果，并将其与原 installation、deployment、控制项目、事件范围、来源绑定及双向 origin 逐项核对；请求到 XI 时不转发浏览器 cookie 或人类 Authorization。

UR 管理员 API：

| 动作 | 方法和路径 | 请求 / 结果 |
| --- | --- | --- |
| 定位原组织 | `GET /api/v2/xinsight/setups/{installation_id}/credential-recovery-scope` | 不含票据；200 仅在当前及原 setup 管理员仍有原范围权限时返回原 `org_id`，否则统一 404；不外发 XI 请求。 |
| 预览原范围 | `POST /api/v2/organizations/{org_id}/xinsight/setups/{installation_id}/credential-recovery/preview` | JSON `{"recovery_ticket":"…"}`；200 返回原范围、票据期限和本地 `credential` 状态，不保存恢复操作。 |
| 确认恢复 | `POST /api/v2/organizations/{org_id}/xinsight/setups/{installation_id}/credential-recovery` | 同一 JSON；202 表示恢复请求及新 bearer **已加密持久保存，等待后台接续**，不表示 XI 已确认、业务 Poll 已恢复或数据已加载。 |
| 读取状态 | `GET /api/v2/organizations/{org_id}/xinsight/setups/{installation_id}` | `credential` 区分 `rotation_due`、`rotation_pending`、`committed_unpromoted`、`expired_recovery_required`、`healthy`、`revoked`；XI receiving、正文读取仍另看实际证据。 |

两种断线前驱均保持原连接：XI 已确认轮换而 UR 尚未晋升时，恢复票据绑定 XI 当前的新摘要，UR 从原 pending 密文证明准确持有；若 XI 的 prepared replacement 和旧 current 均过期，票据绑定仍为 XI current 的旧摘要，准确标识待取代的 prepared operation。恢复写入先把被取代操作标为 superseded，再保存新操作；XI 的恢复确认及 UR 晋升必须逐步完成。票据过期、原管理员失权、原 origin 或范围不符、准确旧 secret 缺失，以及撤销状态均拒绝恢复。不要用新安装、清库、SQL 改有效期或通用 service identity 替代。

本地已有回执且其中的 replacement 期限已过时，状态直接为 `expired_recovery_required`，包括 prepared 与 committed-but-unpromoted；原 pending 身份仍保留供准确恢复。若根本未收到回执，即使旧 current 已过期，UR 也不能凭本地事实断言 XI 是否已接收替代凭据，仍显示 `rotation_pending` 和原 operation，等待准确查询或管理员按原连接恢复流程核对，不伪报健康或猜测对端结果。

## 核对与证据边界

先确认 UR 版本/迁移头为 1.9.2/M125、原 setup 与运行循环已启用，再分别读取本地 `credential` 状态、XI 对应 operation/identity 状态和该连接的实际 Poll/ACK 与 Feed 接收。`healthy` 仅说明本地凭据代际；收到完成请求、来源事实与正文仍需分别核验真实结果。Local 单产品测试、双产品合成联合测试、CI、签名 Artifact、Target、Production 是不同证据层，不得相互替代。失败时保留 operation ID、请求/回执摘要及状态；**不记录票据、bearer、请求密文或 DSN**。回退 M125 必须遵守迁移的历史保留与拒绝条件，不删除已产生的代际日志。
