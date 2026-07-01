# FIX-SIGNAL.md — PRD2 implementation batch 2 RBAC backend foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 2

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 1 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮只同步状态并推进 batch 2 |
| Frontend | 预期无功能变化 | 本轮只做后端 RBAC gate，不做 Developer Console UI |
| 完整项目成员管理 UI/API | 仍未完成 | 本轮使用 batch 1 的 `project_members` 表做读取校验，成员管理 API 后续实现 |
| Model Registry / Provider Adapter | 仍未实现 | 本轮只推进 RBAC 后端底座 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成完整 Developer Console、Model Registry 或 Provider Adapter，则应标为 FAIL。

---

## Fix #1: Batch 1 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md:4`

**原问题：** batch 1 已由 Canary/Sentinel 完成，但状态文档仍停留在 awaiting Canary。

**当前代码/文档：**

```markdown
| PRD2 implementation batch 1 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
```

**验证方法：**

1. 读取 `docs/agent-workflows/VERIFY-RESULT.md`，确认对应 PRD2 implementation batch 1 且 5/5 PASS。
2. 确认 `docs/ops/CURRENT-STATUS.md` 记录 batch 1 Canary passed 和 User reported Sentinel completed。
3. 确认 `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` 没把缺少 E2E 证据的 batch 1 条目误标为 `E2E accepted`。

---

## Fix #2: PRD2 role constants, compatibility mapping, and route RBAC middleware

**文件：** `internal/auth/auth.go:17`

**原问题：** PRD1 只有松散字符串角色，`admin`/`org_viewer` 与 PRD2 `platform_admin`/`viewer` 没有统一映射，后端路由也缺少复用式 role gate。

**当前代码：**

```go
const (
    RoleLegacyAdmin   = "admin"
    RolePlatformAdmin = "platform_admin"
    RoleOrgAdmin      = "org_admin"
    RoleBillingAdmin  = "billing_admin"
    RoleProjectAdmin  = "project_admin"
    RoleDeveloper     = "developer"
    RoleViewer        = "viewer"
    RoleOrgViewer     = "org_viewer"
)
```

`internal/auth/auth.go:149` 的 `NormalizeRole` 将 `admin -> platform_admin`、`org_viewer -> viewer`。`internal/middleware/rbac.go:13` 新增 `RequireRoles`，按归一化角色返回 403。

**验证方法：**

1. 运行 `go test ./internal/auth ./internal/middleware`。
2. 确认 `TestNormalizeRole` 覆盖 legacy role 映射。
3. 确认 `TestRequireRoles_DeniesBillingAdminForPlatformOnlyRoute` 覆盖 billing admin 被 platform-only route 拒绝。

---

## Fix #3: Admin routes grouped by PRD2 role boundaries

**文件：** `cmd/uniroute/main.go:210`

**原问题：** 大多数 `/api/v1/*` admin route 只有 JWT 登录校验，没有区分 platform/org/billing/project/developer/viewer。

**当前代码：**

```go
platformRoute := func(h http.HandlerFunc) http.Handler { ... }
orgAdminRoute := func(h http.HandlerFunc) http.Handler { ... }
billingRoute := func(h http.HandlerFunc) http.Handler { ... }
usageRoute := func(h http.HandlerFunc) http.Handler { ... }
projectReadRoute := func(h http.HandlerFunc) http.Handler { ... }
projectWriteRoute := func(h http.HandlerFunc) http.Handler { ... }
```

关键路由：

- `cmd/uniroute/main.go:281` channel/provider routes 只允许 `platform_admin`。
- `cmd/uniroute/main.go:303` wallet/grants/reconciliation routes 走 `billingRoute`。
- `cmd/uniroute/main.go:287` project key reads/creates 走 project read/write gates。
- `cmd/uniroute/main.go:332` global audit logs 只允许 platform/org/billing roles，不允许 developer/viewer。

**验证方法：**

1. 运行 `go test ./cmd/uniroute`。
2. 审查 `cmd/uniroute/main.go`，确认 channel/channel-key/channel-test 不是普通 `adminAuth`。
3. 审查 audit route，确认 developer/viewer 不在 allowed roles。

---

## Fix #4: Project-scoped handlers enforce project membership

**文件：** `internal/admin/rbac_helpers.go:24`

**原问题：** batch 1 已有 `project_id`，但 project-scoped roles 仍可能读取无项目过滤的数据或访问非成员项目。

**当前代码：**

```go
func (h *Handler) canReadProject(ctx context.Context, projectID int64) bool
func (h *Handler) canWriteProject(ctx context.Context, projectID int64) bool
func (h *Handler) canCreateProjectKey(ctx context.Context, projectID int64) bool
func (h *Handler) canReadProjectUsage(ctx context.Context, projectID int64) bool
```

落点：

- `internal/admin/project_handlers.go` 对项目列表、详情、更新做 membership 过滤/校验。
- `internal/admin/key_handlers.go:19` project-scoped role 只能看到有 `project_members` 的 key；`:121` developer/project role 创建 key 必须显式传 `project_id`。
- `internal/admin/log_handlers.go:69` logs API 对项目级角色要求 `project_id`，并校验 membership。

**验证方法：**

1. 运行 `go test ./internal/admin -run 'TestListLogs_ProjectScopedRole|TestCreateAPIKey_DeveloperRequiresProjectID' -v`。
2. 确认 `TestListLogs_ProjectScopedRoleRequiresProjectID` 返回 403。
3. 确认 `TestListLogs_ProjectScopedRoleWithMembership` 在 membership 存在时返回 200。

---

## Fix #5: User roles are validated before write

**文件：** `internal/admin/user_handlers.go:73`

**原问题：** 用户创建/更新可写入任意 role 字符串，后续 RBAC 可能被脏角色绕过或产生不可预期状态。

**当前代码：**

```go
if !auth.IsValidRole(req.Role) {
    writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid role"})
    return
}
```

`internal/admin/rbac_handlers_test.go:109` 和 `:123` 分别覆盖 CreateUser/UpdateUser 的 invalid role 400。

**验证方法：**

1. 运行 `go test ./internal/admin -run 'Test(CreateUser_InvalidRole|UpdateUser_InvalidRole)' -v`。
2. 确认默认角色仍为兼容角色 `org_viewer`，不会破坏 PRD1 用户创建测试。

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/auth ./internal/middleware ./internal/admin ./cmd/uniroute
go test ./...
git diff --check
git status --short
sed -n '34,44p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — 验收结果

**验收者：** canary agent
**日期：** 文件系统时间戳为准
**对应轮次：** PRD2 implementation batch 2

---

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #3: PASS/FAIL — [原因]
- Fix #4: PASS/FAIL — [原因]
- Fix #5: PASS/FAIL — [原因]

## 前置条件核验

- Frontend 无功能变化: PASS/FAIL
- 完整项目成员管理 UI/API 未声称完成: PASS/FAIL
- Model Registry / Provider Adapter 未声称完成: PASS/FAIL

## 全局验证

- go test ./internal/auth ./internal/middleware ./internal/admin ./cmd/uniroute: PASS/FAIL
- go test ./...: PASS/FAIL
- git diff --check: PASS/FAIL
- PRD2 acceptance matrix 状态未过度标注: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
