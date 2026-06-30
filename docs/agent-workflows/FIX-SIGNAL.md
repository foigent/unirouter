# FIX-SIGNAL.md — PRD1 Strict Closeout Security/Ops Fix

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 strict closeout security and ops gate

---

## 背景

用户确认 PRD1 必须全部对齐 PRD 验收矩阵后才能关闭。Codex 在部署后严格检查时发现：

- Alibaba Cloud 当前公开部署接受 `admin/admin123456` 并返回 JWT。
- 这说明 seeded public default admin password 仍可用于生产登录，是 PRD1 关闭阻断。
- `/health/ready` 当前代码返回旧形态 `{"status":"ready"}`，而 PRD1 要求 DB/Redis 就绪形态。
- README/runbook 仍引用不存在或过期的 CLI/部署说明。

本轮修复不运行真实 provider 付费请求，不做生产钱包/渠道/API key 写入，不做压测或故障注入。

---

## Fix #1: 阻断公开默认管理员密码登录

**文件：**

- `internal/admin/auth_handlers.go`
- `internal/admin/admin_mock_test.go`

**修复内容：**

- Web 登录在 bcrypt 校验通过后拒绝 `admin/admin123456`。
- 仅当显式设置 `UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN=true` 时允许该默认口令，用于临时本地开发。
- 默认拒绝发生在 JWT 生成和 audit 写入之前。
- 增加测试覆盖默认口令拒绝和显式 dev override。

---

## Fix #2: 增加管理员运维 CLI

**文件：**

- `cmd/uniroute/cli.go`
- `cmd/uniroute/cli_test.go`
- `cmd/uniroute/main.go`

**修复内容：**

- 增加 `./uniroute admin set-password --username admin --password <strong-password> --org-id 1`。
- 增加 `./uniroute admin create --username <name> --password <strong-password> --role admin --org-id 1`。
- CLI 默认拒绝创建或设置 `admin/admin123456`。
- 增加 `./uniroute migrate up` 和 `./uniroute migrate status`，对齐部署文档。
- 启动时先处理 CLI，未传 CLI 参数时保持正常服务模式。

---

## Fix #3: `/health/ready` 对齐 PRD 响应形态

**文件：**

- `cmd/uniroute/main.go`

**修复内容：**

- PostgreSQL 和 Redis 都健康时返回：

```json
{"status":"ok","db":"ok","redis":"ok"}
```

---

## Fix #4: 增加 PRD1 严格关闭 Runbook 和云端 smoke 脚本

**文件：**

- `scripts/prd1-cloud-smoke.sh`
- `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md`
- `docs/DOC-INVENTORY.md`
- `docs/PROJECT-HANDOFF.md`
- `README.md`

**修复内容：**

- 新增 PRD1 strict closeout gate，明确 S0/S1/S2/S3/S4 安全等级。
- 新增云端 smoke 脚本，验证 health、metrics、docs、Prometheus、Grafana、未授权 401、默认 admin 拒绝。
- 脚本不做真实 provider 调用、不做钱包/渠道/API key 写入、不做压测或故障注入。
- 脚本不使用批量删除目录；临时响应文件保留在 `/tmp`。
- README 增加 strict closeout runbook 链接，并修正 compose 容器数量和管理员初始化说明。

---

## Fix #5: 同步 PRD1 状态、验收矩阵和 open issues

**文件：**

- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/PRD-ACCEPTANCE-MATRIX.md`
- `docs/ops/PRD1-CLOSEOUT-PLAN.md`
- `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`
- `docs/ops/deployment-runbook.md`
- `.env.example`

**修复内容：**

- 明确 PRD1 不能关闭，直到 default admin 修复部署并通过云端 smoke。
- 将 UI redesign 的 Canary 非视觉验证同步为 passed，同时保留用户/cloud 视觉验收门禁。
- 记录云端已确认 `/api/v1/keys`、`/api/v1/logs` 等管理员只读接口返回 200。
- 新增 `SEC-002` Critical issue，跟踪默认管理员安全阻断。
- 修正 deployment runbook 中不存在的旧 `migrate down` / 旧管理员初始化说明。
- `.env.example` 增加 `UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN=false` 安全说明。

---

## Codex 已执行验证

```bash
go test ./cmd/uniroute ./internal/admin
go test ./...
JWT_SECRET=test-secret ENCRYPTION_KEY=0123456789abcdef0123456789abcdef go run ./cmd/uniroute admin set-password --username admin
JWT_SECRET=test-secret ENCRYPTION_KEY=0123456789abcdef0123456789abcdef go run ./cmd/uniroute admin set-password --username admin --password admin123456
JWT_SECRET=test-secret ENCRYPTION_KEY=0123456789abcdef0123456789abcdef go run ./cmd/uniroute migrate status
```

结果：

- `go test ./cmd/uniroute ./internal/admin`: PASS
- `go test ./...`: PASS
- CLI missing-password validation: PASS，返回 `--username and --password are required`
- CLI default-password refusal: PASS，返回 `refusing to set the public default admin password`
- CLI migration status: PASS，列出本地已应用 migrations
- Local `/health/ready` runtime smoke: PASS，返回 `{"status":"ok","db":"ok","redis":"ok"}`

云端事实检查：

- Alibaba Cloud 当前部署 `admin/admin123456` 返回 JWT：FAIL，确认 PRD1 关闭阻断。
- Authenticated admin read smoke for `/api/v1/me`, `/api/v1/keys`, `/api/v1/logs`, `/api/v1/audit-logs`, `/api/v1/model-names`: PASS，均返回 200。

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
```

Source-level checks:

```bash
rg -n "UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN|Default admin password is disabled|admin123456" internal/admin/auth_handlers.go internal/admin/admin_mock_test.go .env.example
rg -n "admin set-password|admin create|migrate up|migrate status" cmd/uniroute/cli.go docs/ops/deployment-runbook.md README.md docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md
rg -n '\"status\":\"ok\",\"db\":\"ok\",\"redis\":\"ok\"' cmd/uniroute/main.go
rg -n "SEC-002|default admin|PRD1-STRICT-CLOSEOUT|prd1-cloud-smoke" docs/issues/OPEN-ISSUES.md docs/ops docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md README.md scripts/prd1-cloud-smoke.sh
bash -n scripts/prd1-cloud-smoke.sh
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 Strict Closeout Security/Ops Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 strict closeout security and ops gate

## 逐条结果

- Fix #1: PASS/FAIL — 默认 admin/admin123456 是否被 Web 登录阻断，测试是否覆盖
- Fix #2: PASS/FAIL — admin set-password/create 和 migrate up/status CLI 是否存在并有基础测试
- Fix #3: PASS/FAIL — /health/ready 是否返回 PRD DB/Redis shape
- Fix #4: PASS/FAIL — strict closeout runbook 和 cloud smoke 脚本是否存在且无批量删除目录行为
- Fix #5: PASS/FAIL — CURRENT-STATUS / OPEN-ISSUES / PRD-ACCEPTANCE-MATRIX / closeout docs 是否同步默认 admin 阻断

## 全局验证

- `git diff --check`: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm run build`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL
- `bash -n scripts/prd1-cloud-smoke.sh`: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
