# FIX-SIGNAL.md — PRD1 Cloud API Smoke Follow-up Round

**修复者：** Codex
**日期：** 以文件系统时间戳为准
**修复轮次：** PRD1 cloud API smoke follow-up

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| OpenClaw Canary | 不修改代码 | 只做测试、审查、记录结果 |
| 本轮范围 | 非破坏性云端 API smoke 证据、admin list scan/NULL 修复、PRD1 状态同步 | PRD1 收尾 |
| 视觉/人工 UI 验收 | 不属于 Canary 能力范围 | 继续作为外部验收项保留 |
| Docker/云部署/真实 provider | 云端非破坏性 API smoke 已完成；真实 provider 不在本轮范围 | 真实 provider 付费调用、压测、断流、故障注入需用户确认凭据和安全条件 |

---

## Fix #1: 云端非破坏性 API smoke 证据入档

**文件：**
- `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD-ACCEPTANCE-MATRIX.md`
- `docs/ops/PRD1-CLOSEOUT-PLAN.md`
- `docs/issues/OPEN-ISSUES.md`

**修复内容：**
- 记录 Alibaba Cloud 只读/认证类 smoke 结果
- 覆盖 frontend、backend health、backend metrics、Prometheus、Grafana login
- 覆盖 gateway missing-key 401、admin missing-token 401
- 覆盖 authenticated admin GET/read-only API：dashboard、channels、keys、channel keys、wallet、transactions、logs、model prices、model names、org model prices、reconciliation、users、grants、audit logs、settings、sensitive words
- 明确排除真实 provider 付费调用、写入 mutation、压测、断流和故障注入

**验证方法：**
```bash
rg -n "non-destructive|8080/metrics|/api/v1/logs|missing-key|missing-token|PRD1-ENG-003" docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md docs/ops/CURRENT-STATUS.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/ops/PRD1-CLOSEOUT-PLAN.md docs/issues/OPEN-ISSUES.md
```

---

## Fix #2: admin list scan/NULL 修复

**文件：**
- `internal/admin/key_handlers.go`
- `internal/admin/log_handlers.go`
- `internal/admin/admin_mock_test.go`

**修复内容：**
- `ListAPIKeys` 对 nullable `api_keys.budget_limit` 使用 `COALESCE(ak.budget_limit, 0)`
- `ListLogs` 对 nullable `call_logs.upstream_error` 使用 `COALESCE(upstream_error, '')`
- 两个列表 handler 均检查 `rows.Scan` 和 `rows.Err`
- 新增 scan-error 回归测试，避免后续吞错并返回零值字段

**验证方法：**
```bash
go test ./internal/admin -run 'TestList(APIKeys|Logs)_(Success|ScanError|QueryError)$'
go test ./...
```

---

## Prior Canary-passed context from previous closeout round

The following items were already verified by OpenClaw Canary in the previous `PRD1 remaining closeout` round and are kept here only as context.

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
rg -n "non-destructive|8080/metrics|/api/v1/logs|missing-key|missing-token|PRD1-ENG-003" docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md docs/ops/CURRENT-STATUS.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/ops/PRD1-CLOSEOUT-PLAN.md docs/issues/OPEN-ISSUES.md docs/agent-workflows/FIX-SIGNAL.md internal/admin/key_handlers.go internal/admin/log_handlers.go
go test ./internal/admin -run 'TestList(APIKeys|Logs)_(Success|ScanError|QueryError)$'
go test ./...
git diff --check
```

本轮云端非破坏性 API smoke：

- `GET http://8.130.18.85:3000/`: PASS
- `GET http://8.130.18.85:8080/health/live`: PASS
- `GET http://8.130.18.85:8080/health/ready`: PASS
- `GET http://8.130.18.85:8080/metrics`: PASS
- `GET http://8.130.18.85:3000/health/ready`: PASS
- `GET http://8.130.18.85:9090/-/healthy`: PASS
- `GET http://8.130.18.85:3001/login`: PASS
- `GET /v1/models` without API key: PASS, expected 401
- `GET /api/v1/channels` without admin token: PASS, expected 401
- Authenticated admin read-only GET APIs: PASS

上一轮已执行验证保留为背景：

```bash
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
git diff --check -- internal/admin/key_handlers.go internal/admin/log_handlers.go internal/admin/admin_mock_test.go docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD1-CLOSEOUT-PLAN.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/agent-workflows/FIX-SIGNAL.md
rg -n "COALESCE\\(ak.budget_limit, 0\\)|COALESCE\\(upstream_error, ''\\)|Scan failed|Rows failed" internal/admin/key_handlers.go internal/admin/log_handlers.go
rg -n "non-destructive|8080/metrics|/api/v1/logs|missing-key|missing-token|PRD1-ENG-003" docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md docs/ops/CURRENT-STATUS.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/ops/PRD1-CLOSEOUT-PLAN.md docs/issues/OPEN-ISSUES.md docs/agent-workflows/FIX-SIGNAL.md
go test ./internal/admin -run 'TestList(APIKeys|Logs)_(Success|ScanError|QueryError)$'
go test ./...
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 Cloud API Smoke Follow-up 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 cloud API smoke follow-up

## 逐条结果

- Fix #1: PASS/FAIL — 云端非破坏性 API smoke 证据是否入档且边界清楚
- Fix #2: PASS/FAIL — admin list scan/NULL 修复是否正确

## 全局验证

- `git diff --check`: PASS/FAIL
- `rg` evidence checks: PASS/FAIL
- targeted admin tests: PASS/FAIL
- `go test ./...`: PASS/FAIL
- 视觉/人工 UI 验收: NOT_APPLICABLE_TO_CANARY
- 真实 provider/压测/断流/故障注入: NOT_APPLICABLE_TO_CANARY

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
