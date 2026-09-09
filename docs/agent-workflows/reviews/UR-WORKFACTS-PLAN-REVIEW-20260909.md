# UR工作事实计划：独立只读审查与裁定

日期：2026-09-09。范围仅为编码前计划，不是产品实现或运行验收。

- 计划：[UR-WORKFACTS-LOCAL-PLAN-20260909.md](../../analysis/UR-WORKFACTS-LOCAL-PLAN-20260909.md)。
- base：`cd9f354b8a104a6f0677345a5694b19c000ab015`；tree：`7725ffa81b6f6897f5ef6ff843f308c05d44fd63`。
- 原稿SHA-256：`ce68898bbaf929e7af60e762c1b25ce3d67eb3b368182dfa2964883d35e22794`。本任务临时副本：`/private/tmp/ur-workfacts-plan-original-ce68898b.md`。
- 修订稿SHA-256：`0e504bffc251c815680485f1b2dab8b69b335e682ec4d66e586b9b7923a07cbd`。
- 作者：UR执行owner `01a08339-f189-7703-acd9-5d0a63a2873b`。
- 独立审查者：`/root/precode_review`（Hubble）；独立读取原计划、第41/25份要求及实际源码，仅只读，不写文件、不测试、不操作服务、不派发XI。
- 触发：适用AGENTS.md §4规定共享架构、身份、迁移、财务、权限、路由及启动恢复合同变更须编码前独立审查。

## 原稿结论与作者裁定

原审查结论：方向可行，4项`validated_required`应修订后进入共同方案；无新增`product_decision_required`。作者逐项核对源码后全部采纳。

| 项 | 独立审查证据 | 作者修订及后续实现验证 |
| --- | --- | --- |
| R1：Messages实际流支持 | `public_nonstream_handler.go:152`绑定流执行器；`public_responses_stream_runtime.go:65`及其测试137行起有Messages协议与终态 | 计划§2/6/7/9覆盖Messages V2 stream成功、usage/checkpoint/结算、取消/中断及费用关联；保留历史V1流拒绝，不因旧函数名漏测 |
| R2：跨profile保留窗 | `billing/provider_attempt.go:750`未决hold可超过7天；各profile仅按早期published_at清理会先丢context | §4/5/7明确原reservation/job保留当时快照直到终态发布；同一终态事务发布自足core/identity/basis，共用publication时刻与7天窗口；权限仍分离。测试早期context过期、跨7天hold、不同启用时间/权限；晚启用仍明确前向coverage gap |
| R3：M116兼容谓词遗漏 | `runtimegeneration/lease.go:133`、`runtime_generation_cli.go:74`、`runtime_generation.go:131`、`modelcapabilitypublication/plan_v2.go:131`均有精确head条件 | §8逐项纳入有限M116支持、完整ledger/lease/counter v3/authoring/manifest回归及未知head拒绝；compatible-expand只在现有双实例development/test/integration合同内扩展。客户Compose另核对其实际安装/upgrade入口，不能拿双实例证据代替 |
| R4：新增publisher容量责任未落定 | `outbox/outbox.go:255,493`没有计划所称新增容量预留；重试/死信不能自行证明已冻结终态容量 | §4取消source publisher、额外队列和积压准入；原writer同库事务直接发布，原financial outbox保留。固定业务锁→原V1→按profile排序的新source锁；有界片段，无网络调用；测并发、锁等待、字节/WAL/延迟和故障恢复，明确新增503及数据库耗尽风险 |

`rejected`：已有journal时旧outbox重放仍调用terminal materializer、重建已清理源事实。实际`billing/outbox.go:238`有`created && b.terminalMaterializer != nil`守卫；计划保留该幂等条件。

其余独立核对：两次认证后捕获、同次Agent authority快照、revision进入event ID、固定字段profile和独立purpose、I-01关闭时明确expected reference，与已读源码边界相容。async当前独立charge writer的金额/价格未决不能伪造成text I-01。D沿既有预算/分摊权威，X保留实际安装/恢复依赖。

## 一次收尾核对

收尾限定原稿、上述修订diff及新diff引入的阻塞；不重开架构审查。同一独立审查者已核实修订稿SHA-256，并逐项确认R1—R4全部解决。新增verifier、独立token和source instance澄清未发现新阻塞；无未解决的`validated_required`、新增`validated_blocker`或`product_decision_required`。

独立收尾结论：计划层四项修正完成，可交负责人确定共同方案。仅进行了本次唯一收尾核对，未修改文件、运行测试或验证实现；不能把计划修订当产品已验收。

## 当前交付边界

本轮只新增计划与本记录。隔离工作树由精确base创建，未写产品代码、修改原工作树、启动服务或执行外部写操作。按本次派发要求，交负责人形成共同方案后再实施；不要求用户重复批准已授权的本地技术动作。

结束时再核对：提供的base工作树仍干净且HEAD/tree未变；保存工作树仍为原16个tracked修改及4个untracked条目、原HEAD/tree。第41份保护清单中18个有文件hash的目标均与原证据一致；研究目录只有目录路径记录，未据此宣称完成目录内容hash比较。本任务计划/审查记录经回读和空白检查，未运行产品测试。

环境事实：本地Go和独立PG16二进制可用，但未启动；Docker执行能力与Neutral N1本地资料路径尚未证明。后续必需gate保留，不能将缺工具或skip记为通过。当前没有CI、Artifact、Target、Provider、External Integration或Production验收结果。

经验文件无需变化：只读设计尚未产生满足准入门槛的新运行经验；审查也未提出新增经验要求。
