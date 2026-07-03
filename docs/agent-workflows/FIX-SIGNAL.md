# FIX-SIGNAL.md — PRD2 Batch 25 Final Closeout

**修复者：** main agent
**日期：** 2026-07-04
**修复轮次：** PRD2 Batch 25

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 18 real upstream provider acceptance | 仍 Open / later-plan | User 已明确延后；当前环境仍没有 OpenRouter/Anthropic 凭据 |
| P2-F10 License target evidence | 不是 Production verified | Batch 22 提供 local real-backend/browser E2E；Production verified 仍需生产或 approved-staging import/status evidence |
| Project budget hard-block enforcement | Deferred | User 已确认 PRD2 first release 是 alert-only；hard-block 是 later unfinished work |
| Production HA/DR | 仍 Open / later target evidence | Batch 21 接受 self-hosted/staging readiness 和备份恢复 drill；未声明 production HA/DR 完整闭环 |
| Real enterprise IdP browser login | 仍 target-environment evidence | Batch 24 core OIDC 已通过 mock-provider；真实 IdP 需要 approved issuer/client credentials |
| Runtime plugins / marketplace / enterprise IaC / full engineering handbook | PRD3/later | PRD2 不实现 runtime plugin platform、marketplace、Helm/Terraform 或完整工程治理手册 |

> Canary 验收时：以上项目不应作为 Batch 25 FAIL。需要确认它们被明确记录为 deferred、later-plan 或 target-evidence pending，而不是被误标为 Production verified。

---

## Fix #1: P2-F11 Prompt/Response Storage Policy

**文件：** `docs/tech-ref/19-prompt-response-storage-policy.md`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`

**原问题：** PRD2 P2-F11 仍是 `Planned`，只在 completion plan 中有原则描述，缺少正式 policy/design 文档。

**当前文档：**

- 新增 `docs/tech-ref/19-prompt-response-storage-policy.md`。
- 明确 PRD2 默认不存储 prompt/response plaintext。
- Request troubleshooting 保持 metadata-only。
- Optional plaintext storage 必须 future explicit approval 后才能实现。
- 覆盖 enablement、purpose、retention、encryption、redaction、RBAC、audit-on-read、customer controls、export、deletion、incident response。
- 说明 cache/analytics 不得绕过该 policy 暗中启用 plaintext retention。
- 已在 tech-ref index 和 DOC-INVENTORY 中索引。

**验证方法：**

```bash
test -f docs/tech-ref/19-prompt-response-storage-policy.md
rg -n "disabled by default|audit-on-read|explicit future approval|metadata-only" docs/tech-ref/19-prompt-response-storage-policy.md
rg -n "19-prompt-response-storage-policy|Prompt And Response Storage Policy" docs/tech-ref/README.md docs/DOC-INVENTORY.md
```

---

## Fix #2: PRD2 Acceptance Matrix Final Reconciliation

**文件：** `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** Batch 25 要求所有 PRD2 matrix rows 进入终态，但当前仍有 `Planned`、`Implemented not accepted`、`Unit tested` 行。

**当前文档：**

- P2-F05 Provider Adapter Framework 相关行改为 `E2E accepted`，但 real-upstream OpenRouter/Anthropic evidence 保持 deferred under `PRD2-B18-001`。
- P2-F06 Token Metering V2 相关行改为 `E2E accepted`，broader real-upstream usage evidence 保持 deferred under Batch 18。
- P2-F12 Acceptance discipline 改为 `E2E accepted`，记录不 overclaim Production verified 的边界。
- Project budget hard-block enforcement 改为 `Deferred`。
- P2-F10 License Foundation 改为 `E2E accepted`，但明确 not `Production verified`。
- P2-F11 Prompt/Response storage policy 改为 `E2E accepted`，指向新增 policy doc。
- Native adapter proof 改为 `E2E accepted`，direct Anthropic upstream evidence 保持 credential-gated deferred。

**验证方法：**

```bash
rg -n '\| (Planned|Implemented not accepted|Unit tested) \|' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
rg -n "P2-F10 License foundation|P2-F11 Prompt/Response storage policy|Project budget hard-block enforcement|PRD2-B18-001|Production verified" docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

期望：第一条 `rg` 无输出并返回 1；第二条能看到 P2-F10/P2-F11/budget/provider boundary。

---

## Fix #3: Open Issues Boundary Discipline

**文件：** `docs/issues/OPEN-ISSUES.md`

**原问题：** Batch 25 需要确认 PRD2 open items 都有明确用户决策或 target-evidence 边界，不能留下未分类 blockers。

**当前文档：**

- `PRD2-BUDGET-001` 说明 hard-block 是 user-approved later-phase item，matrix row 为 `Deferred`。
- `PRD2-HA-001` 说明 production HA/DR 是 later target evidence，不阻断 PRD2 E2E closeout。
- `PRD2-B18-001` 说明 real upstream provider E2E 是 credential-gated later-plan item。
- `PRD2-B22-001` 说明 License is E2E accepted locally, while target-environment Production verified evidence remains open。

**验证方法：**

```bash
rg -n "PRD2-BUDGET-001|PRD2-HA-001|PRD2-B18-001|PRD2-B22-001" docs/issues/OPEN-ISSUES.md
rg -n "later-phase|later target evidence|credential-gated|not `Production verified`|E2E accepted locally" docs/issues/OPEN-ISSUES.md
```

---

## Fix #4: Current Status And Completion Plan Closeout State

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-COMPLETION-PLAN.md`, `docs/ops/evidence/PRD2-B25-final-closeout-local-20260704.md`

**原问题：** Current status 和 completion plan 还停留在 Batch 24/初始 Batch 25 计划，没有记录 Batch 25 local closeout handoff。

**当前文档：**

- CURRENT-STATUS 记录 Batch 25 fixed locally awaiting Canary。
- Phase Status 指向新增 P2-F11 policy doc。
- Current Verification 增加 Batch 25 Final Closeout row。
- Completion plan Batch 25 增加 2026-07-04 status update。
- External inputs 更新为 License Production verified promotion 所需 target evidence。
- Local evidence file 记录 P2-F11/index/matrix checks、全量 Go/前端测试/build、`git diff --check`，以及 current-code browser smoke PASS。

**验证方法：**

```bash
rg -n "Batch 25|19-prompt-response-storage-policy|P2-F11|Production verified|credential" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-COMPLETION-PLAN.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
test -f docs/tech-ref/19-prompt-response-storage-policy.md
rg -n "disabled by default|audit-on-read|explicit future approval|metadata-only" docs/tech-ref/19-prompt-response-storage-policy.md
rg -n "19-prompt-response-storage-policy|Prompt And Response Storage Policy" docs/tech-ref/README.md docs/DOC-INVENTORY.md
! rg -n '\| (Planned|Implemented not accepted|Unit tested) \|' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
rg -n "P2-F10 License foundation|P2-F11 Prompt/Response storage policy|Project budget hard-block enforcement|PRD2-B18-001|Production verified" docs/ops/PRD2-ACCEPTANCE-MATRIX.md
rg -n "PRD2-BUDGET-001|PRD2-HA-001|PRD2-B18-001|PRD2-B22-001" docs/issues/OPEN-ISSUES.md
rg -n "Batch 25|19-prompt-response-storage-policy|P2-F11|Production verified|credential" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-COMPLETION-PLAN.md
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

环境具备时，Canary 也可以复跑 current-code browser smoke。该命令需要本地 PostgreSQL/Redis、一个当前源码 backend、以及可访问的 frontend：

```bash
set -a; source .env.production; set +a; LISTEN_PORT=18084 go run ./cmd/uniroute
# 另一个 shell:
set -a; source .env.production; set +a; \
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18084 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch23-product-smoke.sh
```

若 Canary 没有本地运行环境，可将 browser smoke 标为 SKIP，但需核验 `docs/ops/evidence/PRD2-B25-final-closeout-local-20260704.md` 已记录 Codex current-code smoke PASS，且没有把该本地证据写成 production/customer evidence。

## 期望输出格式

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 25 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 25 Final Closeout

## 逐条结果

- Fix #1: PASS/FAIL — [P2-F11 policy exists, indexed, and keeps plaintext disabled by default]
- Fix #2: PASS/FAIL — [matrix rows reconciled to final states without overclaiming Production verified]
- Fix #3: PASS/FAIL — [open PRD2 issues have explicit user decision / target-evidence boundary]
- Fix #4: PASS/FAIL — [current status and completion plan reflect Batch 25 local closeout handoff]

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `test -f docs/tech-ref/19-prompt-response-storage-policy.md` | PASS/FAIL | |
| `rg -n "disabled by default|audit-on-read|explicit future approval|metadata-only" docs/tech-ref/19-prompt-response-storage-policy.md` | PASS/FAIL | |
| `rg -n "19-prompt-response-storage-policy|Prompt And Response Storage Policy" docs/tech-ref/README.md docs/DOC-INVENTORY.md` | PASS/FAIL | |
| `! rg -n '\| (Planned|Implemented not accepted|Unit tested) \|' docs/ops/PRD2-ACCEPTANCE-MATRIX.md` | PASS/FAIL | PASS means no non-final matrix status remains |
| `rg -n "P2-F10 License foundation|P2-F11 Prompt/Response storage policy|Project budget hard-block enforcement|PRD2-B18-001|Production verified" docs/ops/PRD2-ACCEPTANCE-MATRIX.md` | PASS/FAIL | |
| `rg -n "PRD2-BUDGET-001|PRD2-HA-001|PRD2-B18-001|PRD2-B22-001" docs/issues/OPEN-ISSUES.md` | PASS/FAIL | |
| `rg -n "Batch 25|19-prompt-response-storage-policy|P2-F11|Production verified|credential" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-COMPLETION-PLAN.md` | PASS/FAIL | |
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | large chunk warning is existing/non-blocking unless new errors appear |
| `git diff --check` | PASS/FAIL | |
| Current-code browser smoke | PASS/FAIL/SKIP | If SKIP, state that no local backend/frontend environment was available and verify the Batch 25 evidence boundary instead |

## 新发现

- [如无，写“无新发现”]

## 结论

PASS/FAIL。
```
