# FIX-SIGNAL.md — PRD2 Full Completion Plan Update

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 completion planning after Batch 15

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 Batch 15 code | 已在 `main`/`origin/main` 的 `c997341` | 本轮只更新 PRD2 后续完整实现计划，不改业务代码 |
| `VERIFY-RESULT.md` | 仍是 Batch 15 Canary 结果 | Canary 新验收前不应伪造新的 VERIFY 结果 |
| 业务测试 | 不要求全量重跑 | 本轮是 docs-only 计划/状态更新；用文档一致性命令验收 |

---

## Fix #1: 新增 PRD2 完整收尾执行计划

**文件：** `docs/ops/PRD2-COMPLETION-PLAN.md:1`

**原问题：** User 明确要求 PRD2 必须按完整规划实现并完全可用后才收尾；聊天中的后续计划尚未写入文件系统事实源。

**当前代码/文档：**

- 新增 `docs/ops/PRD2-COMPLETION-PLAN.md`。
- 记录完整收尾标准、用户已确认的产品决策、cache research-only 原因、prompt/response 默认不存原因。
- 拆分 Batch 16 到 Batch 25：
  - real-backend Developer Console/RBAC E2E；
  - Model Governance 与 metering 完整化；
  - OpenRouter Claude 与 direct Anthropic native 分离验收；
  - notification/event 与 budget；
  - audit/hardening；
  - HA/deployment；
  - License production acceptance；
  - P2-F13/F14/F15；
  - OIDC first；
  - final closeout。

**验证方法：**

```bash
test -f docs/ops/PRD2-COMPLETION-PLAN.md
rg -n "Batch 16|Batch 25|OIDC|OpenRouter|anthropic_native|Cache Research|Prompt/response" docs/ops/PRD2-COMPLETION-PLAN.md
```

---

## Fix #2: 更新 PRD2 验收矩阵中的未完成/未完全实现项

**文件：** `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** 矩阵还没有反映 User 最新确认的完整 PRD2 范围：P2-F13/F14/F15 必做、OIDC first 后期实现、budget hard-block 后期实现、生产 HA 未完全实现、OpenRouter 只能覆盖 `openai_compatible` Claude E2E。

**当前代码/文档：**

- 新增 `PRD2 full-completion plan exists` 行，状态为 `Implemented not accepted`。
- Developer Console 行记录 developer 默认可创建 permitted project key。
- Project budget 行记录 alert-only first，并新增 hard-block enforcement planned 行。
- HA 行记录 staging/docker-compose first，production HA 仍需 target evidence。
- Prompt/response storage 行记录默认不存 plaintext、正式策略设计先行。
- Native adapter proof 行区分 OpenRouter Claude 与 direct Anthropic native E2E。
- SSO 行改为 `OIDC first SSO integration`。
- P2-F13/F14/F15 行标记 User confirmed mandatory，并保持 `Planned`。

**验证方法：**

```bash
rg -n "full-completion|hard-block|OIDC first|OpenRouter|P2-F13|P2-F14|P2-F15|Cache research" docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #3: 更新当前状态入口

**文件：** `docs/ops/CURRENT-STATUS.md`

**原问题：** Current Status 只记录到 Batch 15 完成，没有明确 PRD2 仍需继续按完整范围实现，不应误判为可收尾。

**当前代码/文档：**

- Last verified 行保留 Batch 15 已完成事实，同时说明本轮 full-completion plan update fixed locally awaiting Canary。
- Summary 记录 User 最新范围决策。
- Phase Status 指向 `docs/ops/PRD2-COMPLETION-PLAN.md`。
- Current Verification 新增 `PRD2 full-completion plan` docs-only 待验行。

**验证方法：**

```bash
rg -n "full-completion|PRD2-COMPLETION-PLAN|P2-F13|OIDC-first|OpenRouter|hard-block|production HA" docs/ops/CURRENT-STATUS.md
```

---

## Fix #4: 更新未完成事项和文档清单

**文件：** `docs/issues/OPEN-ISSUES.md`, `docs/DOC-INVENTORY.md`

**原问题：** OIDC first、budget hard-block、production HA 这些 User 明确要求“记入未实现项目/未完全实现”的项目没有进入 active issue queue；新计划文档也未进入文档清单。

**当前代码/文档：**

- 新增 `PRD2-AUTH-001`：OIDC first SSO integration is not implemented。
- 新增 `PRD2-BUDGET-001`：Project budget hard-block enforcement is not implemented。
- 新增 `PRD2-HA-001`：Production HA is not fully implemented or verified。
- `DOC-INVENTORY.md` Current Truth 增加 `docs/ops/PRD2-COMPLETION-PLAN.md`。

**验证方法：**

```bash
rg -n "PRD2-AUTH-001|PRD2-BUDGET-001|PRD2-HA-001|PRD2-COMPLETION-PLAN" docs/issues/OPEN-ISSUES.md docs/DOC-INVENTORY.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
test -f docs/ops/PRD2-COMPLETION-PLAN.md
rg -n "Batch 16|Batch 25|OIDC|OpenRouter|anthropic_native|Cache Research|Prompt/response" docs/ops/PRD2-COMPLETION-PLAN.md
rg -n "full-completion|hard-block|OIDC first|OpenRouter|P2-F13|P2-F14|P2-F15|Cache research" docs/ops/PRD2-ACCEPTANCE-MATRIX.md
rg -n "full-completion|PRD2-COMPLETION-PLAN|P2-F13|OpenRouter|hard-block|production HA" docs/ops/CURRENT-STATUS.md
rg -n "PRD2-AUTH-001|PRD2-BUDGET-001|PRD2-HA-001|PRD2-COMPLETION-PLAN" docs/issues/OPEN-ISSUES.md docs/DOC-INVENTORY.md
git diff --check
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`，建议格式：

```markdown
# VERIFY-RESULT.md — PRD2 Full Completion Plan Update 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 completion planning after Batch 15

## 逐条结果

- Fix #1: PASS/FAIL — PRD2-COMPLETION-PLAN 是否存在且覆盖完整范围、批次、边界。
- Fix #2: PASS/FAIL — PRD2-ACCEPTANCE-MATRIX 是否准确记录未完成/未完全实现项，没有 overclaim。
- Fix #3: PASS/FAIL — CURRENT-STATUS 是否把 PRD2 标为继续完整实现而非可收尾。
- Fix #4: PASS/FAIL — OPEN-ISSUES 和 DOC-INVENTORY 是否记录 OIDC/budget hard-block/production HA/新计划文档。

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `test -f docs/ops/PRD2-COMPLETION-PLAN.md` | PASS/FAIL | |
| completion plan grep | PASS/FAIL | |
| acceptance matrix grep | PASS/FAIL | |
| current status grep | PASS/FAIL | |
| open issues/doc inventory grep | PASS/FAIL | |
| `git diff --check` | PASS/FAIL | |

## 新发现

- [如有，列出]

## 结论

PASS/FAIL. 明确说明本轮 docs-only full-completion plan 是否可交给 Sentinel，以及下一轮是否应进入 Batch 16。
```
