# Agent 协作工作流

> 本目录存放 Codex agent 和 Canary agent 的协作流程文件。
> **仅本地使用，不推送 GitHub（已在 .gitignore 中排除）。**

## 核心协议
- [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md) — 协作协议完整定义（起始文档）

## 当前项目状态入口
- [../PROJECT-HANDOFF.md](../PROJECT-HANDOFF.md) — Agent 接手项目第一入口
- [../CODEBASE-MAP.md](../CODEBASE-MAP.md) — 代码结构地图
- [../DOC-INVENTORY.md](../DOC-INVENTORY.md) — 文档清单和当前/历史边界
- [../ops/CURRENT-STATUS.md](../ops/CURRENT-STATUS.md) — 当前项目状态
- [../ops/PRD1-CLOSEOUT-PLAN.md](../ops/PRD1-CLOSEOUT-PLAN.md) — PRD1 收尾计划
- [../ops/PRD-ACCEPTANCE-MATRIX.md](../ops/PRD-ACCEPTANCE-MATRIX.md) — PRD 验收矩阵
- [../issues/OPEN-ISSUES.md](../issues/OPEN-ISSUES.md) — 当前未关闭问题

## 协作文件

### 通道 A：Codex → Canary 验收

仅在 `AGENT-PROTOCOL.md` 定义的风险门触发时使用：R2/R3/R4 必须使用，R1 可合并成一个 gate，R0 本地整理不强制使用。

| 文件 | 写入方 | 读取方 | 说明 |
|------|--------|--------|------|
| [FIX-SIGNAL.md](FIX-SIGNAL.md) | Codex | Canary | 风险门验收指令（触发时覆盖写入） |
| [VERIFY-RESULT.md](VERIFY-RESULT.md) | Canary | 用户 | 风险门验收结果（触发时覆盖写入） |
| [CHECKLIST.md](CHECKLIST.md) | Canary | Canary | 验收检查清单 |

### 通道 B：Canary 发现问题 → Codex 评估实现
- 当前未关闭问题统一记录在 `docs/issues/OPEN-ISSUES.md`
- 结构性问题可以另建 `docs/issues/{short-name}.md`，但必须同步登记到 `OPEN-ISSUES.md`

### 通道 C：当前状态与文档治理
- 当前状态入口：`docs/ops/CURRENT-STATUS.md`
- PRD 验收矩阵：`docs/ops/PRD-ACCEPTANCE-MATRIX.md`
- 当前问题队列：`docs/issues/OPEN-ISSUES.md`

### 通道 D：Canary 通过 → Sentinel PR/CI
- Canary 验收通过后，用户通知 Sentinel。
- Sentinel 负责 GitHub PR、merge、CI。
- CI/PR 发现代码问题时，回流给 Codex，再走通道 A。

### 通道 E：Sentinel 通过 → Atlas 云端部署
- Sentinel 确认 PR/CI 通过后，用户通知 Atlas。
- Atlas 负责云端部署和部署验证。
- 部署发现代码问题时，回流给 Codex，再走通道 A。

### 状态文件
- [PLAN.md](PLAN.md) — 当前开发计划
- [CONTEXT_HANDOFF.md](CONTEXT_HANDOFF.md) — 上下文切换记录

## 流程概述

```
用户确认 PRD/范围
→ Codex 开发 + 必要测试 + 判定 R0/R1/R2/R3/R4
→ R0 本地验证结束；R1 可合并 gate；R2/R3/R4 更新 FIX-SIGNAL.md
→ 风险门触发时，用户通知 OpenClaw Canary 验收
→ Canary 写 VERIFY-RESULT.md
→ 有 bug 回 Codex 修复；无问题通知 Sentinel
→ Sentinel 创建/合并 PR + 跑 CI
→ 需要部署/目标环境证据时通知 Atlas 云端部署
```

详见 [AGENT-PROTOCOL.md](AGENT-PROTOCOL.md)。
