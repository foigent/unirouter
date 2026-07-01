# FIX-SIGNAL.md — PRD2 Batch 10 Developer Console MVP

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 10

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 1-9.5 | Canary/Sentinel 已完成 | User 已报告 Canary 和 Sentinel 完成，`PRD2-ERR-001` 为 Canary passed |
| Atlas 部署 | 未执行 | 本轮是本地 Developer Console 实现，不触发部署 |
| Playground | 未实现 | `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md` 明确 Playground 是 P2，不属于本轮 P0 |
| 独立公开 Portal | 未实现 | PRD2 决策是一个登录、一个前端应用、角色感知导航 |
| Developer 停用 Key | 未放开 | 当前后端仅允许 `platform_admin/org_admin/project_admin` 停用 Key；Developer Console 对 `developer` 隐藏停用按钮，不在本轮扩大权限 |
| PRD2 Low hardening follow-ups | 仍 Open | `PRD2-MIG-001/002`, `PRD2-GOV-001`, `PRD2-AUDIT-001` 不混入 Batch 10 |

> Canary 验收时：上述未完成项不算 FAIL。若本轮声称完成部署、Playground、公开 Portal、Developer 停用 Key 权限扩大或 Low hardening 全量修复，应标为 FAIL。

---

## Fix #1: Project-filtered API key listing

**文件：** `internal/admin/key_handlers.go`, `internal/admin/rbac_handlers_test.go`

**原问题：** Developer Console 有项目选择器，但现有 `GET /api/v1/keys` 只能按项目角色做成员隔离，不能按选中的 `project_id` 过滤。多项目成员会看到混合 Key 列表。

**当前实现：**

- `GET /api/v1/keys?project_id=<id>` 解析并校验 `project_id`。
- 先调用 `canReadProject`，无权限返回 403。
- SQL 追加 `ak.project_id = $n`，项目角色仍保留 membership EXISTS 过滤。
- 新增测试覆盖允许路径和无权限路径。

**建议验证：**

```bash
go test ./internal/admin -run 'TestListAPIKeys_ProjectIDFilter|TestListAPIKeys_Success|TestCreateAPIKey_DeveloperRequiresProjectID' -v
```

---

## Fix #2: Auth profile refresh and role-aware routing

**文件：** `frontend/src/store/auth.ts`, `frontend/src/main.tsx`, `frontend/src/App.tsx`, `frontend/src/navigation.tsx`, `frontend/src/pages/LoginPage.tsx`

**原问题：** 前端只用 token 判断是否登录，刷新后没有 `/me` profile，无法知道角色；侧边栏也对所有用户显示管理员菜单。

**当前实现：**

- Auth store 增加 `UserProfile`、`profileLoading`、`loadProfile()`。
- `ProtectedRoute` 在有 token 但无角色时读取 `/api/v1/me`。
- 登录成功后按角色跳转：项目角色进入 `/developer`，管理员/账单角色进入 `/`。
- `navigation.tsx` 集中定义角色标签、默认落点、菜单和前端路由允许列表。
- `developer/viewer/project_admin` 只显示 Developer Console 和 Docs。
- Direct URL 前端 guard 会把项目角色访问 `/channels` 等管理员路径重定向回 `/developer`；后端 RBAC 仍是最终权限边界。

**建议验证：**

```bash
npm --prefix frontend test -- navigation.test.tsx LoginPage.test.tsx
```

---

## Fix #3: Developer Console MVP page

**文件：** `frontend/src/pages/DeveloperConsolePage.tsx`, `frontend/src/pages/ApiDocsPage.tsx`, `frontend/src/index.css`

**当前实现：**

- 在既有前端应用内新增 `/developer`。
- 项目选择器来自 `GET /api/v1/projects?status=active`。
- 项目 API Key 列表使用 `GET /api/v1/keys?project_id=<id>`。
- 创建项目 Key 时发送 `org_id`、`project_id`、name、allowed models、IP allowlist、RPM/QPS，并只显示一次完整 Key。
- Approved Models 使用 `GET /api/v1/model-catalog?project_id=<id>`，展示 alias、lifecycle、provider family、context length、capability tags、data residency。
- 最近调用使用 `GET /api/v1/logs?project_id=<id>`，展示 request_id、status、model、tokens、cost、latency。
- Request Lookup 使用 `GET /api/v1/logs?project_id=<id>&request_id=<id>` 后再查 `/logs/{id}`，UI 不展示 provider key、完整 API key、prompt、response。
- Quickstart 使用当前 origin `/v1` 和当前 approved model/alias 生成 curl 示例。
- `/docs` 静态接口索引从 PRD1 文案更新为 PRD2，并加入 Developer Console 相关项目 API。
- CSS 增加固定网格、代码块换行、lookup 摘要网格和移动端响应式约束。

**建议验证：**

```bash
npm --prefix frontend test -- DeveloperConsolePage.test.tsx
npm --prefix frontend run build
```

---

## Fix #4: Frontend API client supports project Developer Console contracts

**文件：** `frontend/src/api/client.ts`, `frontend/src/api/__tests__/client.test.ts`

**当前实现：**

- 新增 `appendParams()`，避免继续手拼 query string。
- `getKeys(page, pageSize, { project_id })`。
- `getProjects(page, pageSize, { status, org_id })`。
- `getProject(id)`。
- `getModelCatalog(page, pageSize, { project_id, org_id })`。
- `getLogs()` filters 增加 `project_id` 和 `usage_source`。
- API client tests 覆盖 keys/projects/model-catalog/logs 的 project query。

**建议验证：**

```bash
npm --prefix frontend test -- client.test.ts
```

---

## Fix #5: PRD2 status documents updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/agent-workflows/FIX-SIGNAL.md`

**当前实现：**

- `CURRENT-STATUS.md` 标记 Batch 10 fixed locally awaiting Canary。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 `P2-F03 Developer Console MVP` 从 `Planned` 改为 `Implemented not accepted`，并保留 Canary/browser/Sentinel 缺口。
- 不把 Batch 10 提前标成 `E2E accepted` 或 `Production verified`。

**建议验证：**

```bash
rg -n "Batch 10|Developer Console MVP|P2-F03|project_id" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/agent-workflows/FIX-SIGNAL.md
```

---

## 本地已执行并通过

```bash
go test ./internal/admin ./cmd/uniroute
go test ./...
npm --prefix frontend test
npm --prefix frontend run build
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

`npm --prefix frontend run build` 仍显示既有 Vite large chunk warning；构建退出码为 0。

Codex 还执行了本地 Playwright mock-browser smoke：

- `/login` 渲染通过，无 console error。
- 未登录访问 `/developer` 重定向到 `/login`。
- 使用 Playwright route mock `/api/v1/me`, `/projects`, `/keys`, `/model-catalog`, `/logs` 后，`/developer` 桌面和 390px 移动端视口渲染通过，无 console error。
- 截图保存到被 git ignore 的本地证据：`output/playwright/prd2-batch10-login.png`, `output/playwright/prd2-batch10-developer-console.png`, `output/playwright/prd2-batch10-developer-console-mobile.png`。

---

## Canary 全局验证建议

请从仓库根目录执行：

```bash
go test ./internal/admin -run 'TestListAPIKeys_ProjectIDFilter|TestListAPIKeys_Success|TestCreateAPIKey_DeveloperRequiresProjectID' -v
go test ./internal/admin ./cmd/uniroute
go test ./...
npm --prefix frontend test
npm --prefix frontend run build
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

如果具备浏览器自动化能力，可补充：

```bash
npm --prefix frontend run dev
# Browser smoke:
# 1. developer/project_admin login lands on /developer.
# 2. /developer shows project selector, project key table, approved models, recent calls, request lookup, quickstart.
# 3. developer direct /channels redirects to /developer.
# 4. project_admin can open create-key drawer and plaintext key modal appears after create.
```

---

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 10 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 10 Developer Console MVP

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/admin -run 'TestListAPIKeys_ProjectIDFilter|TestListAPIKeys_Success|TestCreateAPIKey_DeveloperRequiresProjectID' -v` | PASS/FAIL | |
| `go test ./internal/admin ./cmd/uniroute` | PASS/FAIL | |
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | note large chunk warning if present |
| `git diff --check` | PASS/FAIL | |

## 浏览器烟测（如执行）

- PASS/FAIL/SKIP — evidence

## 新发现

- None / list issue paths.

## 结论

PASS/FAIL. 明确说明 Batch 10 是否可交给 Sentinel，以及是否存在 Batch 11 blocker。
```
