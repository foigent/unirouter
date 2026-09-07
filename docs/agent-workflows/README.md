# Agent 执行与复核工作流

本目录保存 UniRouter 当前执行记录、协议和独立复核入口。当前流程以一个执行负责人闭环计划、实现、测试、仓库集成和已授权环境操作。

## 当前入口

| 文件 | 用途 |
|------|------|
| [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) | Agent Protocol v2.3，权限、风险、执行和复核规则 |
| [EXECUTION-RECORD.md](EXECUTION-RECORD.md) | 当前批次的执行、测试、CI、部署、批准和缺口记录 |
| [reviews/README.md](reviews/README.md) | 独立复核格式；明确前置触发项见项目 AGENTS.md |
| [../ops/CURRENT-STATUS.md](../ops/CURRENT-STATUS.md) | 当前项目事实入口 |
| [../PROJECT-HANDOFF.md](../PROJECT-HANDOFF.md) | 按任务导航；历史检查点不替代当前状态 |

## 兼容文件

| 文件 | 当前语义 |
|------|----------|
| [FIX-SIGNAL.md](FIX-SIGNAL.md) | 协议 v1.x 旧链接兼容入口，指向当前执行记录 |
| [VERIFY-RESULT.md](VERIFY-RESULT.md) | 最近一次协议 v1.x 独立验收历史，不再覆盖 |
| [CHECKLIST.md](CHECKLIST.md) | 历史 Canary 检查参考，可供复核者选择使用 |
| [PLAN.md](PLAN.md) | 历史/临时计划，不是当前事实源 |
| [CONTEXT_HANDOFF.md](CONTEXT_HANDOFF.md) | 历史上下文转移参考 |

## 当前流程

```text
明确当前目标与已有授权
-> 命中项目 AGENTS.md 触发项时，先独立只读审查
-> 按风险实现、验证；需要恢复或正式证据时使用已有记录
-> 仅在授权覆盖时提交/PR；适用 required CI 通过且获准后合并
-> 仅在目标需要且获准时部署并取得对应证据
-> 继续当前请求剩余的已授权必要工作；全部完成后停止
```

步骤按任务适用，不因流程图自动新增 PR、部署、付费调用或记录要求。
明确前置审查不能用事后复核替代；新非阻塞建议不自动扩大当前范围。

Canary、Sentinel、Atlas 可以作为复核画像继续参与，但不再是默认串行关卡。详见 [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md)。
