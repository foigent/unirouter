# UniRouter Agent 协作协议

**版本：** 1.3
**最后更新：** 本文件修改时间即为最新（见文件系统时间戳）

---

## 概述

UniRouter 项目由 User 协调，Codex、OpenClaw Canary、Sentinel、Atlas 分工推进：

| 角色 | 责任 | 明确禁止/边界 |
|------|------|----------------|
| **User** | 确认 PRD、控制阶段流转、在 agents 间传话 | 不要求手动整理上下文，文件系统应承载上下文 |
| **Codex** | 与 User 确认 PRD，开发实现，做必要本地测试，维护当前状态和 `FIX-SIGNAL.md` | 不在 Canary 通过后擅自继续改代码，除非收到新问题 |
| **OpenClaw Canary** | 独立测试、审查、验收，写 `VERIFY-RESULT.md` | **不做任何代码修改** |
| **Sentinel** | Canary 通过后负责 GitHub PR、合并、CI 等仓库集成动作 | 不替代 Canary 做产品验收 |
| **Atlas** | Sentinel 完成后负责云端部署和部署验证 | 不直接绕过 Codex 修代码 |

**核心约束：** agents 无法直接通信，只能通过项目文件系统和用户传话协调。每个阶段必须把结果写入约定文件，避免口头状态漂移。

---

## 协作流程

### 总流程：PRD → Codex → Canary → Sentinel → Atlas

```text
User 确认 PRD/范围
  ↓
Codex 开发 + 必要本地测试 + 更新 FIX-SIGNAL.md
  ↓
User 通知 OpenClaw Canary
  ↓
OpenClaw Canary 测试/审查 + 写 VERIFY-RESULT.md
  ↓
  ├─ 有 bug: User 通知 Codex 读取 VERIFY-RESULT.md → Codex 修复 → 更新 FIX-SIGNAL.md → 再验
  ↓
  └─ 无问题: User 通知 Sentinel
          ↓
      Sentinel 创建/合并 PR + 跑 CI
          ↓
          ├─ CI/PR 发现代码问题: 回到 Codex 修复
          ↓
          └─ CI/PR 通过: User 通知 Atlas
                  ↓
              Atlas 云端部署 + 部署验证
                  ↓
                  ├─ 部署发现代码问题: 回到 Codex 修复
                  └─ 部署通过: 本轮完成
```

### 通道 A：Codex 修复 → Canary 验收（FIX-SIGNAL / VERIFY-RESULT）

用于 Codex 修改代码后需要验收的场景。

```
┌─────────┐    一句话     ┌──────────┐    自动读取    ┌─────────┐
│  用户   │ ──────────→ │  Canary  │ ──────────→ │ FIX-    │
│         │              │  Agent   │              │ SIGNAL  │
│         │    一句话     │          │    写入      │   .md   │
│         │ ←────────── │          │ ──────────→ │         │
│         │              └──────────┘              └─────────┘
│         │    一句话          ↑
│         │ ──────────→  ┌──────────┐    自动读取
│  用户   │              │  Codex   │ ←────────── VERIFY-
│         │              │  Agent   │              RESULT
│         │    写入      │          │    写入      .md
│         │ ←────────── │          │ ──────────→
└─────────┘              └──────────┘              ┌─────────┐
                                                   │ FIX-    │
                                                   │ SIGNAL  │
                                                   │   .md   │
                                                   └─────────┘
```

### 通道 B：Canary 发现问题 → Codex 评估实现（issues/）

用于 Canary 在测试中发现的非运行时 bug 的结构性/架构性问题（测试壁垒、覆盖率盲区、代码质量问题），需要 Codex 评估和实现。

```
┌──────────────┐                        ┌──────────────┐
│   Canary     │  写 docs/issues/xxx.md      │ docs/issues/ │
│   (测试中    │ ───────────────────→  │   目录       │
│    发现问题) │                        │              │
└──────────────┘                        └──────────────┘
       │                                       │
       │ 同时写入 VERIFY-RESULT.md             │ Codex 读取
       │ "新发现的问题" 引用 issue 文件         │ 评估优先级
       ↓                                       ↓
┌──────────────┐                        ┌──────────────┐
│  VERIFY-     │  用户传话              │   Main       │
│  RESULT.md   │ ←─────────→          │   Agent      │
│  (引用 issue) │                        │   (实现方案) │
└──────────────┘                        └──────────────┘
                                              │
                                              │ 实现后写 FIX-SIGNAL.md
                                              ↓
                                        回到通道 A
```

**规则：**
1. Canary **只写问题发现**，不写具体代码方案。内容包括：问题描述、影响、严重性、建议方向（不写实现）
2. 文件名：`docs/issues/{简短描述}.md`（如 `billing-pure-function-barrier.md`）
3. Canary 同时在 VERIFY-RESULT.md 的「新发现的问题」中引用该 issue 文件路径
4. Codex 读取 issues/，评估优先级，选择实现
5. 实现完成后 Codex 写 FIX-SIGNAL.md，走通道 A 验收
6. 验收通过后，issue 文件标注「已解决」

### 通道 C：当前状态与文档治理

用于同步项目真实进度、PRD 验收状态、未关闭问题和历史文档归档边界。

| 文件 | 用途 | 规则 |
|------|------|------|
| `docs/ops/CURRENT-STATUS.md` | 当前项目状态入口 | 只写当前事实，不写历史流水账 |
| `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | PRD 验收矩阵 | 每个验收项必须有状态和证据 |
| `docs/issues/OPEN-ISSUES.md` | 当前未关闭问题 | Issue ID 不复用 |
| `docs/DOCS-GOVERNANCE.md` | 文档治理规则 | 定义哪些文件是当前事实源 |

文档整理或状态变更如果会影响项目判断，也应走通道 A：Codex 更新 `FIX-SIGNAL.md`，OpenClaw Canary 验证文档状态是否自洽。

### 通道 D：Canary 通过 → Sentinel PR/CI

用于 OpenClaw Canary 验收通过后的 GitHub 集成阶段。

**进入条件：**
1. `VERIFY-RESULT.md` 结论为全部通过，或只剩用户明确接受的非阻断项。
2. `FIX-SIGNAL.md` 和 `VERIFY-RESULT.md` 对应同一轮。
3. `OPEN-ISSUES.md` 中没有本轮阻断项处于 `Open`。

**Sentinel 职责：**
1. 创建或更新 GitHub PR。
2. 确认变更范围和提交内容。
3. 跑 CI 并记录结果。
4. 合并或按用户要求等待合并。

**失败回流：**
- 如果 CI 或 PR 检查发现代码/测试问题，User 通知 Codex，并将 Sentinel 的失败信息作为输入。
- Codex 修复后重新走通道 A，由 OpenClaw Canary 再验。

### 通道 E：Sentinel 通过 → Atlas 云端部署

用于 GitHub PR/CI 通过后的部署阶段。

**进入条件：**
1. Sentinel 确认 PR/merge/CI 通过。
2. 部署版本和提交 SHA 明确。

**Atlas 职责：**
1. 在云端部署指定版本。
2. 验证健康检查、关键 API、前端访问和必要运行时配置。
3. 反馈部署结果。

**失败回流：**
- 如果部署失败是环境/配置问题，Atlas 记录并处理。
- 如果部署失败暴露代码 bug，User 通知 Codex，Codex 修复后重新走通道 A。

### 第 1 步：Codex 修复 + 发信号

Codex 完成修复后，更新 `FIX-SIGNAL.md`（**覆盖写入**，不留历史内容），内容必须包含：

1. **逐条 fix 清单** — 每条含：文件路径、行号、原问题、当前代码、验证方法
2. **全局构建/测试命令** — Canary 可直接复制执行
3. **期望的验收输出格式** — Canary 写入 `VERIFY-RESULT.md` 的模板

Codex 同时告诉用户："请 Canary 验收 docs/agent-workflows/FIX-SIGNAL.md"

### 第 2 步：Canary Agent 验收

用户跟 Canary agent 说："去 /Users/m4/Desktop/projects/UniRouter/docs/agent-workflows/FIX-SIGNAL.md，按里面的验收指令逐条验证，结果写入 VERIFY-RESULT.md"

Canary agent 必须：
1. 读取 `FIX-SIGNAL.md` 中的验收指令
2. 逐条验证（代码审查 + 运行命令）
3. 将结果写入 `VERIFY-RESULT.md`（**覆盖写入**）
4. 如发现新问题，在 `VERIFY-RESULT.md` 末尾的"新发现"区域列出

Canary 同时告诉用户："验收完成，看 VERIFY-RESULT.md"

### 第 3 步：用户传递结果

用户跟 Codex 说："看 VERIFY-RESULT.md"

Codex 读取结果：
- **全部 PASS** → 告知用户验收通过，本轮结束
- **有 FAIL** → Codex 修复失败项，回到第 1 步，更新 `FIX-SIGNAL.md`

---

## 信号文件格式

### FIX-SIGNAL.md 模板

```markdown
# FIX-SIGNAL.md — [简要描述本轮修复内容]

**修复者：** main agent
**日期：** [文件系统时间戳为准，不要硬编码日期]
**修复轮次：** 第 N 轮

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| [例] 集成测试  | 预期仍失败 | 本轮未修改测试代码，旧测试本身有问题 |
| [例]  警告 | 预期无新增 | 本轮只改业务逻辑，未改签名 |

> Canary 验收时：前置条件中列出的预期失败不算 FAIL，只需在结论中注明。

---

## Fix #N: [标题]

**文件：** `path/to/file.go:行号`
**原问题：** [一句话]
**当前代码：** [关键代码片段]
**验证方法：** [具体步骤]

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go build ./...
go vet ./...
go test ./...
```

## 期望输出格式

验收结果写入 VERIFY-RESULT.md，格式见下方模板。
```

### VERIFY-RESULT.md 模板

```markdown
# VERIFY-RESULT.md — 验收结果

**验收者：** canary agent
**日期：** [文件系统时间戳为准]
**对应轮次：** 第 N 轮（对应 FIX-SIGNAL.md）

---

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #N: PASS/FAIL — [原因]

## 前置条件核验

- [例] 集成测试：预期失败，实际失败 → 符合预期 ✓
- [例] go vet：无新增警告 → 符合预期 ✓

## 全局构建

- `go build ./...`: PASS/FAIL
- `go vet ./...`: PASS/FAIL
- `go test ./...`: PASS/FAIL（附失败用例）

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```

---

## 重要规则

1. **文件是覆盖写入**，不是追加。每次轮次用新内容覆盖，不留历史。
2. **不要硬编码日期**。日期以文件系统时间戳为准。
3. **FIX-SIGNAL.md 是给 Canary 看的指令**，不是给自己看的笔记。写清楚验证方法。
4. **VERIFY-RESULT.md 是给 Main 看的反馈**，写清楚失败原因和修复建议。
5. **用户每次只需传一句话**，不需要解释上下文——所有上下文都在文件里。
6. **多轮修复是正常的**。FAIL → 修 → 再发信号 → 再验，直到全部 PASS。
7. **前置条件必须写明**。Codex 在 FIX-SIGNAL.md 中列出本轮预期不变的部分（如旧测试预期仍失败），Canary 在 VERIFY-RESULT.md 中核验前置条件是否符合预期。
8. **Canary 不改代码**。OpenClaw Canary 只测试、审查、写结果；任何修复都回到 Codex。
9. **Canary 通过后不继续扩范围**。进入 Sentinel 前，Codex 不应再做新的非必要代码修改。
10. **UI 是 PRD1 收尾范围**。PRD1 关闭前必须评估 admin UI 的架构、复用、视觉一致性和核心浏览器流程。

---

## 文件清单

| 文件 | 用途 | 谁写 | 模式 |
|------|------|------|------|
| `docs/PROJECT-HANDOFF.md` | Agent 接手第一入口 | Codex/Canary | 更新 |
| `docs/CODEBASE-MAP.md` | 代码结构地图 | Codex/Canary | 更新 |
| `docs/DOC-INVENTORY.md` | 文档清单与当前/历史边界 | Codex/Canary | 更新 |
| `FIX-SIGNAL.md` | 验收指令 | Codex | 覆盖写入 |
| `VERIFY-RESULT.md` | 验收结果 + 新发现 | Canary | 覆盖写入 |
| `docs/issues/OPEN-ISSUES.md` | 当前未关闭问题 | Codex/Canary | 更新 |
| `issues/*.md` | 问题补充说明 | Codex/Canary | 每个问题一个文件 |
| `docs/ops/CURRENT-STATUS.md` | 当前项目真实状态 | Codex/Canary | 更新 |
| `docs/ops/PRD1-CLOSEOUT-PLAN.md` | PRD1 收尾计划 | Codex/User | 更新 |
| `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | PRD 验收状态 | Codex/Canary | 更新 |
| `docs/testing/archive/BUG-LOG.md` | 历史运行时 bug 记录 | Canary | 归档参考 |
| `docs/testing/archive/TEST-STATUS.md` | 历史测试状态 | Canary | 归档参考 |
| `AGENT-PROTOCOL.md` | 本协议 | 协商 | 更新 |

## 当前状态

当前状态以 `docs/ops/CURRENT-STATUS.md` 为准。

当前未关闭问题以 `docs/issues/OPEN-ISSUES.md` 为准。
