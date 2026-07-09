# Agent 执行与复核工作流

本目录保存 UniRouter 当前执行记录、协议和独立复核入口。当前流程以一个执行负责人闭环计划、实现、测试、仓库集成和已授权环境操作。

## 当前入口

| 文件 | 用途 |
|------|------|
| [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) | Agent Protocol v2.0，权限、风险、执行和复核规则 |
| [EXECUTION-RECORD.md](EXECUTION-RECORD.md) | 当前批次的执行、测试、CI、部署、批准和缺口记录 |
| [reviews/README.md](reviews/README.md) | 可选独立复核文件格式和严重级别规则 |
| [../ops/CURRENT-STATUS.md](../ops/CURRENT-STATUS.md) | 当前项目事实入口 |
| [../PROJECT-HANDOFF.md](../PROJECT-HANDOFF.md) | 接手项目第一阅读入口 |

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
User 确认范围和高影响授权
-> Codex 实现、测试并记录 EXECUTION-RECORD.md / evidence
-> Codex 创建 PR，等待强制 CI
-> CI 通过后 Codex 合并
-> 已授权非生产目标由 Codex 部署和验证
-> 生产或高影响动作经 User 单次批准后执行
-> 独立复核者按需写入 reviews/，P0/P1 阻断或重新打开批次
```

Canary、Sentinel、Atlas 可以作为复核画像继续参与，但不再是默认串行关卡。详见 [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md)。
