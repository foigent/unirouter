# P3 控制凭据生命周期：UR 侧编码前交叉审查

- 审查者：`/root`（UR 执行会话）。独立于共享合同作者，仅只读检查设计和源码；若后续由本会话实现 UR 代码，本记录不冒充对该实现 diff 的独立复核。
- Active Milestone：原 UR–XI 连接的控制凭据生命周期补全；四项目标为同连接跨期、离线跨期正式恢复、撤销不可恢复、历史与范围不变。
- 精确对象：`/Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/20260929-formal-ingress/p3-control-credential-lifecycle-20261006.md`，SHA-256 `165dedb4f6311c3bbc0c90c91153910160546e8f51f8a20bd703cf3c6bc54a95`（84 行）。UR 基线 `a831e6bc33ca25edd7ccb07f8a036d84f5bae591`，本分支尚无产品代码 diff。
- 不变量：原 installation/deployment/origin/control project/scope/source binding、初始兑换摘要、control_fence、ACK/历史/权限范围不变；旧凭据不延长；已撤销权威不复活。
- 非目标：新安装、重授权已撤销委托、新调度器、凭据丢失重建、现网写入。
- 实际检查：`shasum -a 256` 匹配派发摘要；读合同全文、UR `redeem.go`、`journal.go`、`runtime_store.go`、`redeemed_origin.go`、`http_counterpart.go`、`xinsight_connector_runtime.go`、setup/状态路径及既有集成测试；只读查看 XI 当前本地 checkout 的 HTTP access、鉴权 store、身份 guard 和路由。没有运行测试、数据库或现网调用。XI 本地 checkout 是 `e393e258...`，不据此声称已核对 XI 远端 `fda71f56...` 的每一行。

## 必须在冻结或实现时解决的发现

1. **`validated_blocker`（P1）：XI 的共享鉴权会因旧 token 到期将同一 identity 标为 `expired`，使已准备的新 token 也失效。** `internal/db/agent_access_store.go:109-128` 在 token 或 identity 任一到期时更新整个 `service_identities.status`；新的 prepared token 即使有效，同 identity 的后续 query/confirm 仍会被 `status != active` 拒绝。`migrations/000021_create_connector_installations.up.sql:527-565` 又把 expired/revoked 状态变更作为终态保护。合同要求同一 identity 跨旧 token 到期和过期后恢复，必须明确 XI 限定 connector identity 的代际鉴权语义：过期旧 token 永远 401，但不得仅因它到期毒化有效 replacement；若 identity 已因历史鉴权变为 expired，恢复只能由严格核验 ticket、准确前驱与未撤销状态的受控事务恢复可用性，revoked 永不恢复。不能靠延长旧 token 或通用放开终态 guard。该点需 XI 负责人在精确 main 上再核实。
2. **`validated_required`：UR 的过期恢复不能走现有 active bearer/origin 读取器。** `internal/xinsightconnector/journal.go:153-177`、`redeemed_origin.go:23-30` 和 `runtime_store.go:45-54` 都要求旧 token 尚未过期；过期时既不能从普通 `controlToken` 取出旧 secret 作持有证明，也不能通过普通 `ReadCompletedRedeemOriginTx` 取原 origin。新增路径必须在原 installation 与 binding 锁下，分别读取目的隔离加密的准确旧 secret（仅作 proof）和不可变 redeemed origin（即使过期），仍验证 deployment、project、scope、source binding 和未撤销状态；普通 Poll/ACK 与 setup-status 的 active 检查不能因此放宽。
3. **`validated_required`：轮换 pending 必须在现有循环中优先接续，并与普通 Poll/ACK 隔离。** `cmd/uniroute/xinsight_connector_runtime.go:290-373` 先快照 runnable，再兑换，最后 Poll；`ListRunnableInstallations` 到期即过滤安装。必须新增有界 pending/due 发现，先处理已持久 replacement，待本端 committed CAS 晋升后再让下一轮正常 Poll；旧到期但新已 prepared、XI confirm 已提交但 UR 崩溃的安装不能消失。普通业务 `control_fence` 不变；已持久终态但尚未 ACK 的操作应继续用现有 journal 重放和新凭据 ACK（`agent.go:225-235`），不能重新执行产品效果。跨代际 ACK/confirm 并发及断电测试是当前验收所需。
4. **`validated_required`：XI 新 HTTP 路由必须接入现有精确鉴权边界。** 当前 `internal/api/access.go:309-326` 仅准许原控制 Poll/ACK 的 service bearer，`363-377` 仅将邀请兑换列为无 cookie 的公开控制入口；`internal/api/server.go:237-239` 未注册合同的新端点。rotate/query/confirm 要有同一 identity 的精确 route permission 和新 token 状态校验；resolve/recover 才能有狭窄公开例外，并在 handler 执行 HTTPS、无 ambient cookie/Authorization、ticket/旧 secret 的服务端核验。否则合同端点即使写成 handler 仍会在外层被拒绝，或者错误落到人类 session 路径。
5. **`validated_required`：状态诊断要从本端持久凭据代际先得出确定结论。** `internal/admin/xinsight_setup_intent.go:324-358` 在远端 setup 状态无法读取时只附 `xi_status_unavailable`；`http_counterpart.go:163-191` 的读路径会在旧 token 到期前置失败；`readXInsightFeedConnectionStatus` 又主要投影安装/Feed 健康。必须把本地到期、pending、已提交未晋升、撤销明确投影并优先于一般 custody/network 错误；XI 有新 token 也不能单独证明 UR 已晋升或 Poll/ACK 已恢复。
6. **`validated_required`：消除两个摘要和历史兼容的歧义。** 合同中的 `source_binding_sha256` 应明确是 `xinsight_connector_installations.source_binding_sha256`，它在首次兑换 wire 中叫 `consent_sha256`（`redeem.go:331`）；它不是 `xInsightSavedSetup.XIConsent.ConsentSHA256`（`xinsight_setup_contract.go:31-41`）。正常 setup 持久保存了原 UR/XI origin；旧的 originless 或缺失 setup consent 安装不能从当前配置猜测历史双向 origin，应 fail closed 并单列可验证退出条件。
7. **`validated_blocker`（P1）：合同的恢复前驱不能固定为 UR 尚未晋升的 active binding。** 第一种真实中断是 XI 已 confirm 并撤销旧 token，UR 未晋升就离线超过 replacement TTL；XI 权威 current 为已过期的新 digest，UR 的准确 secret 只在原 rotation pending 密文中。ticket 必须绑定 XI 权威 current digest（可包含其来源 operation），UR 只用匹配该 digest 的 pending secret 作旧持有证明，不得误用已撤销的旧 active secret。第二种是真实 XI prepared replacement 和旧 current 均已过期；ticket 绑定 XI 仍为 current 的旧 digest，且准确标出需终结的 prepared operation/replacement digest。恢复兑换必须在 XI 同一事务内把该 prepared 操作标为被本次 recovery 取代并拒绝其后续 confirm，再接受新恢复 replacement；UR 保存旧 pending 的终态与关联 recovery，不以“每安装仅一个未完成操作”永久阻止本次恢复。两种情况都保持同一 installation/identity/fence/history，且只允许当前未撤销 secret、有效 ticket 和双端服务端管理员事实共同通过。合同 §2 的 pending 上限和 §3 的 `previous_token` 来源须作此最小补正；不能用清库或新安装代替。

## 结论与边界

合同总体可实施，复用现有加密 journal、固定 origin HTTP transport、单循环和双端管理员边界即可；没有发现需要新平台或改动原业务 control_fence 的理由。冻结前先把第 1、6、7 项精确写清；实施时第 2–5 项及对应跨期/中断/撤销回归是必需工作。当前仅有源码级结论，尚无测试、CI、Artifact、Target 或现网证据；XI 精确 main 的当前实现仍由 XI 负责人复核。本次没有改产品代码、执行迁移或访问现网。

## 精确 XI 基线复核与裁定补正

- 已只读核对 XI 独立 checkout `/private/tmp/xi-fpv15-controller-boundary-20261005` 的 HEAD `fda71f56cc1ff57a9e966a3552c51bc20703ab6b`，未修改该 checkout。其 `internal/db/agent_access_store.go:115-123` 明确仅在 identity 自身到期时把 identity 标为 `expired`；旧 token 单独到期不会毒化同一 identity 的有效 replacement。因此上文第 1 项“旧 token 到期毒化 replacement”的前半判断基于旧 checkout，裁定 **`rejected`**，不再作为冻结阻塞；上文结论中“冻结前第 1 项”相应失效。
- 精确基线仍有需 XI 实现者处理的交错风险：鉴权先读到 identity 的旧 `expires_at`，随后 `UPDATE service_identities ... WHERE id = $1 AND status = 'active'`（同文件 `:118-123`）未在写入条件中重查 `expires_at`，可能与延长期限事务交错。此为源码推断，尚未经并发测试证实；不把它冒充已复现的失败。已过期 identity 的受控恢复如何满足 `migrations/000021_create_connector_installations.up.sql:534-542` 的终态保护，仍须由 XI 在精确身份、ticket、未撤销前驱约束下设计和验证。第 2–7 项裁定维持；冻结前的实质合同阻塞为第 7 项，另须明确第 6 项映射。

## 再核对

- XI `queryServiceIdentityByDigestLock` 已对同一 identity/token 行加锁；上段推测的 stale `UPDATE` 交错缺少成立前提，撤回该风险判断，裁定 `rejected`。本审查保留原判断与补正轨迹，后续以最新共享合同 SHA-256 `60073ab48c36599bd21193c76f0daf9f7090fd88e8044f6c888648f5acf2518c` 为准。
