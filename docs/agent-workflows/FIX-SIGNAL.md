# FIX-SIGNAL.md — PRD2 planning document baseline

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 planning docs round 1

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 后端/前端代码 | 不应变化 | 本轮只补 PRD2 规划与技术文档 |
| PRD1 历史证据 | 保留历史内容 | 本轮不重写 PRD1 closeout 证据，只标明 PRD1/PRD2 阶段边界 |
| 构建/单测 | 不要求运行 | 本轮没有代码变更，Canary 可做文档存在性和一致性验证 |

> Canary 验收时：以上前置条件中列出的未运行构建/测试不算 FAIL；如发现代码文件变更，应标为 FAIL。

---

## Fix #1: 新增正式 PRD2 草案

**文件：** `docs/PRD-Phase2.md:1`

**原问题：** PRD2 只有 Discovery 共识，没有正式 PRD 草案。

**当前代码/文档：**

```markdown
# UniRouter Product Requirements Document - Phase 2
```

**验证方法：**

1. 确认 `docs/PRD-Phase2.md` 存在。
2. 确认文档包含 PRD2 定位、P0/P1/P2 范围、非目标、功能需求、数据模型影响、阶段治理和首批开发顺序。
3. 确认 PRD2 未把支付、发票、实名、优惠券、Campaign、代理商体系、跨模型 fallback 等后置项写入 PRD2 开发目标。

---

## Fix #2: 新增 PRD2 验收矩阵

**文件：** `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:1`

**原问题：** PRD2 没有独立验收矩阵，容易复用 PRD1 matrix 造成阶段混淆。

**当前代码/文档：**

```markdown
# PRD2 Acceptance Matrix
```

**验证方法：**

1. 确认 `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` 存在。
2. 确认矩阵按 Planning/Governance、P0、P1、P2、Deferred 分组。
3. 确认状态值和 E2E/Production 证据规则存在。
4. 确认 deferred 表中包含代理商、支付/发票/实名/优惠券/Campaign、公有 SaaS 双 Portal、跨模型 fallback 等后置项。

---

## Fix #3: 新增 PRD2 Provider Adapter 技术规格

**文件：** `docs/tech-ref/10-provider-adapter-framework.md:1`

**原问题：** 当前 UniRouter 主要是 OpenAI-compatible HTTP relay，缺少 PRD2 上游 native provider adapter 的架构边界。

**当前代码/文档：**

```markdown
# Provider Adapter Framework
```

**验证方法：**

1. 确认文档明确下游保持 OpenAI-compatible。
2. 确认 `openai_compatible` 被定义为 P0 adapter 类型。
3. 确认文档包含 adapter interface、channel model changes、failure classification、streaming contract、billing/logs boundary、test requirements。
4. 确认文档没有要求下游客户直接使用 Anthropic/Gemini/Baidu/Tongyi/Wenxin SDK。

---

## Fix #4: 新增 PRD2 Project/RBAC 技术规格

**文件：** `docs/tech-ref/11-project-rbac-model.md:1`

**原问题：** PRD2 确认 Project 是核心边界，但缺少数据模型、角色、权限矩阵和迁移规则。

**当前代码/文档：**

```markdown
# Project And RBAC Model
```

**验证方法：**

1. 确认文档定义 `org -> project -> api key -> request` 层级。
2. 确认包含 roles、permission matrix、authorization rules、gateway authorization、model access、audit requirements、migration plan。
3. 确认迁移规则包含每个 org 默认 project 和现有 key 映射。
4. 确认权限矩阵要求 backend checks，而不是只依赖前端隐藏导航。

---

## Fix #5: 新增 PRD2 Developer Console 设计规格

**文件：** `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md:1`

**原问题：** PRD2 需要开发者自助区，但不能误做成公有 SaaS 独立 Portal。

**当前代码/文档：**

```markdown
# PRD2 Developer Console Spec
```

**验证方法：**

1. 确认文档说明 Developer Console 是现有前端应用内的 role-aware area。
2. 确认 MVP 页面包含 Project Overview、API Keys、Approved Models、Usage、Request Lookup、Docs。
3. 确认 Approved Models 默认是企业批准模型，不是上百模型 marketplace。
4. 确认 Request Lookup 不展示 provider key、完整 API key、prompt/response 明文。

---

## Fix #6: 更新状态入口和文档索引

**文件：**

- `docs/ops/CURRENT-STATUS.md:1`
- `docs/DOC-INVENTORY.md:1`
- `docs/tech-ref/README.md:1`

**原问题：** PRD2 文档新增后，当前状态和索引没有指向 PRD2 主线，后续 agent/Sentinel 可能继续把 PRD1 matrix 当成活跃主线。

**当前代码/文档：**

```markdown
| PRD2 | Planning / Draft for Review | `docs/planning/PRD2-DISCOVERY.md`, `docs/PRD-Phase2.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
```

**验证方法：**

1. 确认 `docs/ops/CURRENT-STATUS.md` 有 `Phase Status` 表，且 PRD2 指向 Discovery、正式 PRD 和 PRD2 acceptance matrix。
2. 确认 `docs/DOC-INVENTORY.md` 登记 PRD2 PRD、PRD2 matrix、Provider Adapter、Project/RBAC、Developer Console spec。
3. 确认 `docs/tech-ref/README.md` 登记 10/11 两份 PRD2 技术参考。

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
test -f docs/PRD-Phase2.md
test -f docs/ops/PRD2-ACCEPTANCE-MATRIX.md
test -f docs/tech-ref/10-provider-adapter-framework.md
test -f docs/tech-ref/11-project-rbac-model.md
test -f docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md
rg -n "PRD2" docs/DOC-INVENTORY.md docs/ops/CURRENT-STATUS.md docs/tech-ref/README.md
git diff --check
git status --short
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — 验收结果

**验收者：** canary agent
**日期：** 文件系统时间戳为准
**对应轮次：** PRD2 planning docs round 1

---

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #3: PASS/FAIL — [原因]
- Fix #4: PASS/FAIL — [原因]
- Fix #5: PASS/FAIL — [原因]
- Fix #6: PASS/FAIL — [原因]

## 前置条件核验

- 后端/前端代码：未变化/有变化 → [结论]
- PRD1 历史证据：保留/被误改 → [结论]
- 构建/单测：本轮不要求运行 → [结论]

## 全局验证

- planning files exist: PASS/FAIL
- PRD2 references in index/status: PASS/FAIL
- git diff --check: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
