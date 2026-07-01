# FIX-SIGNAL.md — PRD2 Batch 11 Model Governance Admin UI

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 11

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 1-10 | Canary/Sentinel 已完成 | User 已报告 Batch 10 Canary 和 Sentinel 完成 |
| Atlas 部署 | 未执行 | 本轮是本地管理端 UI 和接口补口，不触发部署 |
| 公开模型市场 | 未实现 | PRD2 决策是企业内部模型目录，不做公开 marketplace |
| Native provider adapter proof | 未实现 | 本轮只做模型治理管理面，不改变 adapter roadmap |
| Playground | 未实现 | 仍是 PRD2 P2，不属于本轮 |
| PRD2 Low hardening follow-ups | 仍 Open | `PRD2-MIG-001/002`, `PRD2-GOV-001`, `PRD2-AUDIT-001` 不混入 Batch 11 |
| Batch 11 验收状态 | 不应直接标 E2E/Production | 本轮只有 Codex 本地测试；Canary 通过前只能是 fixed locally / Unit tested 证据 |

> Canary 验收时：上述未完成项不算 FAIL。若本轮声称完成部署、公开模型市场、Native adapter、Playground 或 Low hardening 全量修复，应标为 FAIL。

---

## Fix #1: Model Set items read endpoint for admin UI

**文件：** `internal/admin/model_governance_handlers.go`, `cmd/uniroute/main.go`, `cmd/uniroute/route_parity_test.go`, `internal/admin/model_governance_handlers_test.go`

**原问题：** 后端已有 `PUT /api/v1/model-sets/{id}/items` 可替换模型集条目，但前端编辑模型集时没有对应的读取接口，只能创建不能安全编辑现有条目。

**当前实现：**

- 新增 `ListModelSetItems` handler。
- 新增 `GET /api/v1/model-sets/{id}/items` 路由。
- 查询通过 `JOIN model_sets ms` 并要求 `ms.org_id = $2`，避免跨组织读取模型集条目。
- 返回 `{ data, total }`，条目字段为 `item_type` 和 `item_name`。
- Route parity test 加入新路由。
- Admin handler test 覆盖 org-scoped join、参数和返回结构。

**建议验证：**

```bash
go test ./internal/admin -run TestListModelSetItems_ReturnsOrgScopedItems -v
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
go test ./internal/admin ./cmd/uniroute
```

---

## Fix #2: Frontend API client, routing, and role-aware navigation

**文件：** `frontend/src/api/client.ts`, `frontend/src/api/__tests__/client.test.ts`, `frontend/src/main.tsx`, `frontend/src/navigation.tsx`, `frontend/src/__tests__/navigation.test.tsx`

**原问题：** PRD2 Batch 3 已有 Model Governance 后端接口，Batch 10 已有 Developer Console 消费 approved catalog，但管理员没有 UI 入口维护 registry、alias、model set 和项目分配。

**当前实现：**

- API client 新增：
  - `getModelRegistry/createModelRegistry/updateModelRegistry`
  - `getModelAliases/createModelAlias/updateModelAlias`
  - `getModelSets/createModelSet/getModelSetItems/replaceModelSetItems`
  - `getProjectModelSets/replaceProjectModelSets`
- 前端新增 `/model-governance` route。
- 组织管理员/平台管理员菜单加入“模型治理”。
- Developer/project roles 不显示 `/model-governance`，仍被前端 route guard 拦截；后端 RBAC 仍是最终边界。
- Client tests 覆盖新 API path、query string 和 PUT body。
- Navigation tests 覆盖 admin 可见、developer 不可见。

**建议验证：**

```bash
npm --prefix frontend test -- client.test.ts navigation.test.tsx
npm --prefix frontend run build
```

---

## Fix #3: Admin Model Governance page

**文件：** `frontend/src/pages/ModelGovernancePage.tsx`, `frontend/src/pages/__tests__/ModelGovernancePage.test.tsx`, `frontend/src/index.css`

**当前实现：**

- 新增管理页 `/model-governance`，采用一个页面四个工作区：
  - `Model Registry`: 列表、搜索、生命周期过滤、approved-only 过滤、新增/编辑模型、维护 context/output/capability/data residency/pricing name/migration recommendation。
  - `Aliases`: 列表、新增/编辑企业稳定 alias，支持 active/disabled。
  - `Model Sets`: 列表、新增模型集、读取并替换模型集条目，条目格式为 `model:gpt-4o` 或 `alias:company-chat`。
  - `Project Assignment`: 选择项目，读取当前绑定模型集，保存 `model_set_ids`。
- 页面是企业内部运维管理面，不做模型市场、公开 SaaS portal 或 campaign/支付/发票能力。
- CSS 增加 tab、筛选、统计、表单网格、项目分配布局和移动端响应式约束。
- 页面测试覆盖：
  - 默认加载 Model Registry。
  - 渲染 migration recommendation fixture。
  - 切换 Alias tab 后加载 aliases。
  - Model Set tab 能读取当前条目。
  - Project Assignment 能读取并保存项目模型集。
  - `parseModelSetItems` 去重和格式化。

**建议验证：**

```bash
npm --prefix frontend test -- ModelGovernancePage.test.tsx
npm --prefix frontend test -- ModelGovernancePage client navigation
npm --prefix frontend run build
```

---

## Fix #4: PRD2 status documents updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/agent-workflows/FIX-SIGNAL.md`

**当前实现：**

- `CURRENT-STATUS.md` 标记 Batch 11 为 Codex fixed locally awaiting Canary。
- `PRD2-ACCEPTANCE-MATRIX.md` 只补充 Batch 11 本地证据，不把 Batch 11 UI 提前标成 E2E accepted 或 Production verified。
- `P2-F04 Model Registry`、Approved Catalog、Model Set assignment、Deprecated migration recommendation rows 增加 Batch 11 本地 UI/API/test evidence，并保留 Canary/browser/real-env 缺口。
- `FIX-SIGNAL.md` 覆盖为 Batch 11 验收信号。

**建议验证：**

```bash
rg -n "Batch 11|Model Governance|model-governance|P2-F04|Deprecated model" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/agent-workflows/FIX-SIGNAL.md
```

---

## 本地已执行并通过

```bash
go test ./internal/admin ./cmd/uniroute
npm --prefix frontend test -- ModelGovernancePage client navigation
npm --prefix frontend test
npm --prefix frontend run build
go test ./... -count=1
git diff --check
```

`npm --prefix frontend test` 仍显示既有 React Router/Semi UI jsdom 警告；测试退出码为 0。`npm --prefix frontend run build` 仍显示既有 Vite large chunk warning；构建退出码为 0。

---

## Canary 全局验证建议

请从仓库根目录执行：

```bash
go test ./internal/admin -run TestListModelSetItems_ReturnsOrgScopedItems -v
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
go test ./internal/admin ./cmd/uniroute
go test ./... -count=1
npm --prefix frontend test -- ModelGovernancePage client navigation
npm --prefix frontend test
npm --prefix frontend run build
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

如具备浏览器自动化能力，可补充：

```bash
npm --prefix frontend run dev
# Browser smoke:
# 1. org_admin/platform_admin can open /model-governance.
# 2. Model Registry tab shows models and can open create/edit sheets.
# 3. Aliases tab shows alias rows and can open create/edit sheets.
# 4. Model Sets tab can open item editor and see model:/alias: lines.
# 5. Project Assignment tab loads active projects, current model sets, and save button calls PUT /projects/{id}/model-sets.
# 6. developer direct /model-governance redirects away from admin UI.
```

---

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 11 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 11 Model Governance Admin UI

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/admin -run TestListModelSetItems_ReturnsOrgScopedItems -v` | PASS/FAIL | |
| `go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v` | PASS/FAIL | |
| `go test ./internal/admin ./cmd/uniroute` | PASS/FAIL | |
| `go test ./... -count=1` | PASS/FAIL | |
| `npm --prefix frontend test -- ModelGovernancePage client navigation` | PASS/FAIL | |
| `npm --prefix frontend test` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | note large chunk warning if present |
| `git diff --check` | PASS/FAIL | |
| `git diff --check -- $(git ls-files --others --exclude-standard)` | PASS/FAIL | |

## 浏览器烟测（如执行）

- PASS/FAIL/SKIP — evidence

## 新发现

- None / list issue paths.

## 结论

PASS/FAIL. 明确说明 Batch 11 是否可交给 Sentinel，以及是否存在 Batch 12 blocker。
```
