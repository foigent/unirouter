# Agent 协作工作流

> 本目录存放 Codex agent 和 Canary agent 的协作流程文件。
> **仅本地使用，不推送 GitHub（已在 .gitignore 中排除）。**

## 核心协议
- [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) — 协作协议完整定义（起始文档）

## 协作文件

### 通道 A：Codex → Canary 验收
| 文件 | 写入方 | 读取方 | 说明 |
|------|--------|--------|------|
| [FIX-SIGNAL.md](FIX-SIGNAL.md) | Codex | Canary | 验收指令（覆盖写入） |
| [VERIFY-RESULT.md](VERIFY-RESULT.md) | Canary | 用户 | 验收结果（覆盖写入） |
| [CHECKLIST.md](CHECKLIST.md) | Canary | Canary | 验收检查清单 |

### 通道 B：问题跟踪
- 问题文件已归档至 `docs/archive/issues/`（全部已解决）

### 状态文件
- [PLAN.md](PLAN.md) — 当前开发计划
- [CONTEXT_HANDOFF.md](CONTEXT_HANDOFF.md) — 上下文切换记录

## 流程概述

```
Codex 开发 → 更新 FIX-SIGNAL.md → 用户传话 → Canary 验收
→ 更新 VERIFY-RESULT.md → 用户传话 → Codex 确认
```

详见 [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md)。
