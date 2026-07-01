# FIX-SIGNAL.md — PRD2 implementation batch 1 Project/Workspace foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 1

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 planning docs round 1 | 已由 Canary/Sentinel 通过 | 本轮不重写 PRD2 规划正文，只推进第一批后端实现 |
| Frontend | 预期无功能变化 | 本轮只做 Project/Workspace 后端底座，不做 Developer Console UI |
| RBAC 完整权限矩阵 | 仍未完成 | 本轮只承载 `project_id` 数据边界，完整 role enforcement 在后续批次 |
| Model Registry / Provider Adapter | 仍未实现 | 本轮按计划先打 Project/RBAC 数据底座 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成完整 RBAC、Developer Console 或 Model Registry，则应标为 FAIL。

---

## Fix #1: PRD2 Project/Workspace schema and compatibility backfill

**文件：** `migrations/000009_prd2_project_foundation.up.sql:6`

**原问题：** PRD1 schema 只有 `org_id -> api_key_id -> request_id`，没有 PRD2 所需的企业内部 Project/Workspace 边界。

**当前代码/文档：**

```sql
CREATE TABLE IF NOT EXISTS projects (...);
CREATE TABLE IF NOT EXISTS project_members (...);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
```

`migrations/000009_prd2_project_foundation.up.sql:43` 为每个现有 org 创建 `Default Project`，并在 `:58` 到 `:84` 回填既有 `api_keys`、`call_logs`、`transactions`。

**验证方法：**

1. 审查 `migrations/000009_prd2_project_foundation.up.sql` 和 `.down.sql`，确认 up/down 对称。
2. 确认 `projects` 有 `(org_id, slug)` 唯一约束，`project_members` 有 `(project_id, user_id)` 主键。
3. 确认 `api_keys`、`call_logs`、`transactions`、`audit_logs` 均增加 nullable `project_id`，避免破坏旧数据。

---

## Fix #2: Project admin API routes and handlers

**文件：** `internal/admin/project_handlers.go:14`

**原问题：** PRD2 Developer Console 和项目级 key/logs 需要后端项目对象，但当前没有 Project API。

**当前代码/文档：**

```go
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request)
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request)
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request)
```

路由在 `cmd/uniroute/main.go:268` 注册：

```go
mux.Handle("GET /api/v1/projects", adminAuth(http.HandlerFunc(adminHandler.ListProjects)))
mux.Handle("POST /api/v1/projects", adminAuth(http.HandlerFunc(adminHandler.CreateProject)))
```

**验证方法：**

1. 运行 `go test ./internal/admin -run 'Test(ListProjects|CreateProject|GetProject|UpdateProject)' -v`。
2. 确认 `/api/v1/projects`、`/api/v1/projects/{id}` 出现在 `cmd/uniroute/main.go` 和 `docs/tech-ref/05-openapi-spec.yaml`。
3. 确认本轮没有新增删除项目的 API；批量/危险删除不属于本轮。

---

## Fix #3: API keys are project-scoped while keeping default-project compatibility

**文件：** `internal/admin/key_handlers.go:56`

**原问题：** API key 只能绑定 org，无法表示 PRD2 的 project-scoped key。

**当前代码/文档：**

```go
ProjectID int64 `json:"project_id"`
projectID, err := h.resolveAPIKeyProjectID(r.Context(), req.OrgID, req.ProjectID)
```

`internal/admin/key_handlers.go:318` 的 `resolveAPIKeyProjectID` 在未传 `project_id` 时通过 `ON CONFLICT (org_id, slug)` 获取或创建 org 的 default project。

**验证方法：**

1. 审查 `CreateAPIKey`，确认新建 key 插入 `api_keys.project_id`。
2. 审查 `ListAPIKeys` 和 `GetAPIKey`，确认响应包含 `project_id` 和 `project_name`。
3. 运行 `go test ./internal/admin -run 'Test(CreateAPIKey|ListAPIKeys|GetAPIKey|UpdateAPIKey)' -v`。

---

## Fix #4: Gateway auth context and call logs carry project_id

**文件：** `internal/middleware/auth.go:62`

**原问题：** GatewayAuth 只注入 org/key/request，没有 project context，relay 无法把请求日志落到 project。

**当前代码/文档：**

```go
CtxProjectID contextKey = "project_id"
func GetProjectIDFromContext(ctx context.Context) int64
```

`internal/middleware/auth.go:175` 查询 `COALESCE(ak.project_id, 0)`，`internal/middleware/auth.go:242` 将 project 注入 context。`internal/gateway/relay.go:617` 从 context 读取 project 并写入 `call_logs.project_id`。

**验证方法：**

1. 运行 `go test ./internal/middleware ./internal/gateway`。
2. 审查 `internal/gateway/relay_mock_test.go:286`，确认 `recordRequest` 单测验证了 `project_id` insert arg。
3. 确认没有改变 OpenAI-compatible 下游协议；`project_id` 只进入 UniRouter 内部 context/log。

---

## Fix #5: Logs, audit, test utilities, and docs track project scope

**文件：** `internal/admin/log_handlers.go:28`

**原问题：** Project 数据即使写入，也无法通过日志 API 过滤/返回，审计也无法承载 project context。

**当前代码/文档：**

```go
projectIDFilter := r.URL.Query().Get("project_id")
conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
```

`internal/admin/audit_handlers.go:61` 新增 `recordAuditForProject`，`docs/ops/CURRENT-STATUS.md:23` 和 `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:32` 记录本轮状态。

**验证方法：**

1. 运行 `go test ./internal/admin -run 'TestListLogs|TestRecordAudit|TestCreateAPIKey' -v`。
2. 审查 `internal/testutil/testdb.go` 和 `internal/testutil/db.go`，确认测试清理顺序包含 `project_members` 和 `projects`。
3. 确认 `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` 未把本轮标为 `E2E accepted` 或 `Production verified`。

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/middleware ./internal/gateway ./internal/admin
go test ./...
git diff --check
git status --short
sed -n '30,60p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — 验收结果

**验收者：** canary agent
**日期：** 文件系统时间戳为准
**对应轮次：** PRD2 implementation batch 1

---

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #3: PASS/FAIL — [原因]
- Fix #4: PASS/FAIL — [原因]
- Fix #5: PASS/FAIL — [原因]

## 前置条件核验

- Frontend 无功能变化: PASS/FAIL
- 完整 RBAC 未声称完成: PASS/FAIL
- Model Registry / Provider Adapter 未声称完成: PASS/FAIL

## 全局验证

- go test ./internal/middleware ./internal/gateway ./internal/admin: PASS/FAIL
- go test ./...: PASS/FAIL
- git diff --check: PASS/FAIL
- PRD2 acceptance matrix 状态未过度标注: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
