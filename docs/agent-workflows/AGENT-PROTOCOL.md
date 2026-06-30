# UniRouter Agent 协作协议

**版本：** 1.1
**最后更新：** 本文件修改时间即为最新（见文件系统时间戳）

---

## 概述

UniRouter 项目由 Codex agent 负责开发，OpenClaw Canary agent 负责审查和验收：

| Agent | 角色 | 能力特点 |
|-------|------|---------|
| **Codex Agent** | 开发/实现 | 有 `apply_patch`，擅长代码修改，完成后通过 FIX-SIGNAL 通知 Canary 验收 |
| **Canary Agent** | 审查/验收 | OpenClaw agent，擅长深度代码审查和架构分析，结果写入 VERIFY-RESULT |

**核心约束：** 两个 agent 无法直接通信，只能通过项目文件系统协调。用户负责传话（一句话）。

---

## 协作流程

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
│  用户   │              │  Main    │ ←────────── VERIFY-
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

### 第 1 步：Main Agent 修复 + 发信号

Main agent 完成修复后，更新 `FIX-SIGNAL.md`（**覆盖写入**，不留历史内容），内容必须包含：

1. **逐条 fix 清单** — 每条含：文件路径、行号、原问题、当前代码、验证方法
2. **全局构建/测试命令** — Canary 可直接复制执行
3. **期望的验收输出格式** — Canary 写入 `VERIFY-RESULT.md` 的模板

Main agent 同时告诉用户："请 Canary 验收 docs/agent-workflows/FIX-SIGNAL.md"

### 第 2 步：Canary Agent 验收

用户跟 Canary agent 说："去 /Users/m4/Desktop/projects/UniRouter/docs/agent-workflows/FIX-SIGNAL.md，按里面的验收指令逐条验证，结果写入 VERIFY-RESULT.md"

Canary agent 必须：
1. 读取 `FIX-SIGNAL.md` 中的验收指令
2. 逐条验证（代码审查 + 运行命令）
3. 将结果写入 `VERIFY-RESULT.md`（**覆盖写入**）
4. 如发现新问题，在 `VERIFY-RESULT.md` 末尾的"新发现"区域列出

Canary 同时告诉用户："验收完成，看 VERIFY-RESULT.md"

### 第 3 步：用户传递结果

用户跟 Main agent 说："看 VERIFY-RESULT.md"

Main agent 读取结果：
- **全部 PASS** → 告知用户验收通过，本轮结束
- **有 FAIL** → 修复失败项，回到第 1 步，更新 `FIX-SIGNAL.md`

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

- [全部通过 / 需要 Main agent 修复以下问题：...]
```

---

## 重要规则

1. **文件是覆盖写入**，不是追加。每次轮次用新内容覆盖，不留历史。
2. **不要硬编码日期**。日期以文件系统时间戳为准。
3. **FIX-SIGNAL.md 是给 Canary 看的指令**，不是给自己看的笔记。写清楚验证方法。
4. **VERIFY-RESULT.md 是给 Main 看的反馈**，写清楚失败原因和修复建议。
5. **用户每次只需传一句话**，不需要解释上下文——所有上下文都在文件里。
6. **多轮修复是正常的**。FAIL → 修 → 再发信号 → 再验，直到全部 PASS。
7. **前置条件必须写明**。Main agent 在 FIX-SIGNAL.md 中列出本轮预期不变的部分（如旧测试预期仍失败），Canary 在 VERIFY-RESULT.md 中核验前置条件是否符合预期。

---

## 文件清单

| 文件 | 用途 | 谁写 | 模式 |
|------|------|------|------|
| `FIX-SIGNAL.md` | 验收指令 | Codex | 覆盖写入 |
| `VERIFY-RESULT.md` | 验收结果 + 新发现 | Canary | 覆盖写入 |
| `issues/*.md` | 非 bug 问题发现 | Canary | 每发现一个新文件 |
| `docs/testing/archive/BUG-LOG.md` | 运行时 bug | Canary | 追加 |
| `docs/testing/archive/TEST-STATUS.md` | 测试状态 | Canary | 更新 |
| `AGENT-PROTOCOL.md` | 本协议 | 协商 | 更新 |

## 当前状态

**当前轮次：** 第 14 轮
**状态：** 第 14 轮 FIX-SIGNAL 已验收（7 PASS / 1 FAIL — GetModelNames 数据源不一致）
**本轮修复：** Bug #23-#28 + GetModelNames 新端点 + key_prefix 字段
**issues/ 待处理：** GetModelNames 数据源选择（call_logs vs model_prices）
