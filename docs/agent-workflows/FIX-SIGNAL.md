# FIX-SIGNAL.md — PRD1 Remaining Closeout Round

**修复者：** Codex
**日期：** 以文件系统时间戳为准
**修复轮次：** PRD1 remaining closeout

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| OpenClaw Canary | 不修改代码 | 只做测试、审查、记录结果 |
| 本轮范围 | `/me/password` 契约修复、route parity、历史文档纠偏、PRD1 剩余验收包 | PRD1 收尾 |
| 视觉/人工 UI 验收 | 不属于 Canary 能力范围 | 继续作为外部验收项保留 |
| Docker/云部署/真实 provider | 不属于本地 Codex 已完成项 | `docker` 在当前 workspace 不可用，真实 provider/cloud 由 Sentinel/Atlas 或配置环境验证 |

---

## Fix #1: 补齐 `/me/password` 后端契约

**文件：**
- `internal/admin/auth_handlers.go`
- `cmd/uniroute/main.go`

**修复内容：**
- 新增 `ChangePassword` handler
- 注册 `PUT /api/v1/me/password`
- 从 `AdminAuth` context 读取当前 user_id
- 校验旧密码 bcrypt
- 新密码 bcrypt hash 后更新当前用户
- 未认证返回 401
- 旧密码错误返回 401
- 新旧密码相同或新密码过短返回 400

**验证方法：**
```bash
go test ./internal/admin -run 'TestChangePassword|TestLogin'
rg -n "PUT /api/v1/me/password|ChangePassword" cmd/uniroute/main.go internal/admin/auth_handlers.go
```

---

## Fix #2: 前端 Settings 改密校验

**文件：**
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/api/__tests__/client.test.ts`

**修复内容：**
- 前端提交前校验 `new_password` 与 `confirm_password` 一致
- API client 测试覆盖 `PUT /api/v1/me/password`

**验证方法：**
```bash
cd frontend
npm test -- --run src/api/__tests__/client.test.ts
npm run build
```

---

## Fix #3: 增加前端/后端 route parity 守卫

**文件：**
- `cmd/uniroute/route_parity_test.go`

**修复内容：**
- 增加管理端路由静态守卫，覆盖当前 frontend API client 使用的主要 `/api/v1/*` 路由
- 包含 `PUT /api/v1/me/password`

**验证方法：**
```bash
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
```

---

## Fix #4: 历史测试报告纠偏 + OpenAPI 路径快照刷新

**文件：**
- `docs/ops/test-report.md`
- `docs/DOC-INVENTORY.md`
- `docs/tech-ref/05-openapi-spec.yaml`
- `docs/tech-ref/README.md`
- `docs/CODEBASE-MAP.md`

**修复内容：**
- 在 `test-report.md` 顶部加入 historical report/stale banner
- 将原“XOR 加密”结论改为历史更正：当前代码使用 AES-GCM
- `DOC-INVENTORY.md` 更新该历史报告的角色说明
- 重写 `docs/tech-ref/05-openapi-spec.yaml` 为当前 PRD1 API path snapshot
- OpenAPI YAML 已解析通过，包含 38 个 path，且不再包含 `/api/v1/admin/*`
- `docs/tech-ref/README.md`、`CODEBASE-MAP.md`、`DOC-INVENTORY.md` 已同步

**验证方法：**
```bash
python3 - <<'PY'
from pathlib import Path
import yaml
data = yaml.safe_load(Path('docs/tech-ref/05-openapi-spec.yaml').read_text())
assert data['openapi'] == '3.0.3'
paths = data['paths']
assert '/api/v1/me/password' in paths
assert '/metrics' in paths
assert not [p for p in paths if p.startswith('/api/v1/admin/')]
print(f"parsed {len(paths)} paths")
PY
rg -n "XOR|AES-GCM|Historical report|test-report.md|route_parity_test|v1.1" docs/ops/test-report.md docs/DOC-INVENTORY.md docs/tech-ref/05-openapi-spec.yaml docs/tech-ref/README.md docs/CODEBASE-MAP.md internal/auth/auth.go
```

---

## Fix #5: 新增 PRD1 剩余验收包

**文件：**
- `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`
- `docs/README.md`

**修复内容：**
- 记录 Codex 本地已完成证据
- 记录本地不可完成的外部验收项：Docker Compose、真实 provider、流式中断、并发压测、failover/circuit breaker、视觉/人工 UI、Grafana
- 文档索引加入该验收包

**验证方法：**
```bash
rg -n "PRD1-REMAINING-ACCEPTANCE-PACK|External Acceptance Queue|Docker|provider|visual" docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md docs/README.md
```

---

## Fix #6: 同步 PRD1 状态文档

**文件：**
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD1-CLOSEOUT-PLAN.md`
- `docs/ops/PRD-ACCEPTANCE-MATRIX.md`

**修复内容：**
- `ENG-001` 标为 `Fixed awaiting Canary`
- `SEC-001` 保持 `Fixed awaiting Canary`，证据改为 stale-doc correction
- `DOC-002` 标为 `Fixed awaiting Canary`
- 新增 `DOC-005` 记录 OpenAPI Admin spec stale，状态为 `Fixed awaiting Canary`
- `QA-001` 标为 `In progress`，指向剩余验收包
- `PRD1-ENG-001` 标为 `Fixed awaiting Canary`
- `PRD1-DOC-001` 标为 `Fixed awaiting Canary`
- `Frontend API client matches backend routes` 标为 `Unit tested`
- `/metrics` 标为本地当前源码 `E2E accepted`，部署环境仍需复验

**验证方法：**
```bash
rg -n "ENG-001|SEC-001|DOC-002|DOC-005|QA-001|PRD1-ENG-001|PRD1-DOC-001|Frontend API client matches backend routes|Prometheus" docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD1-CLOSEOUT-PLAN.md docs/ops/PRD-ACCEPTANCE-MATRIX.md
```

---

## Codex 已执行验证

```bash
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
go test ./internal/admin -run 'TestChangePassword|TestLogin'
```

临时当前源码服务验证：

- `GET /health/live`: PASS
- `GET /metrics`: PASS，返回 Prometheus metrics
- `POST /api/v1/auth/login`: PASS
- `GET /api/v1/me`: PASS
- `GET /api/v1/settings`: PASS
- `GET /api/v1/dashboard/stats`: PASS
- `PUT /api/v1/me/password` without auth: PASS, returned 401

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check -- cmd/uniroute/main.go cmd/uniroute/route_parity_test.go internal/admin/auth_handlers.go internal/admin/admin_mock_test.go frontend/src/pages/SettingsPage.tsx frontend/src/api/__tests__/client.test.ts docs/ops/test-report.md docs/DOC-INVENTORY.md docs/tech-ref/05-openapi-spec.yaml docs/tech-ref/README.md docs/CODEBASE-MAP.md docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md docs/README.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD1-CLOSEOUT-PLAN.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/agent-workflows/FIX-SIGNAL.md
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 Remaining Closeout 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 remaining closeout

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #3: PASS/FAIL — [原因]
- Fix #4: PASS/FAIL — [原因]
- Fix #5: PASS/FAIL — [原因]
- Fix #6: PASS/FAIL — [原因]

## 全局验证

- `git diff --check`: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL
- `npm run build`: PASS/FAIL
- 视觉/人工 UI 验收: NOT_APPLICABLE_TO_CANARY
- Docker/云部署/真实 provider: NOT_APPLICABLE_TO_CANARY

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
