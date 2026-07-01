# FIX-SIGNAL.md — PRD2 Batch 14 API Key Hash Secret and Deployment Env Hardening

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 14

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 13 | Canary/Sentinel/Atlas completed per User report; PR #44 deployed | User reported all three roles completed and Anthropic stream feature is online |
| PR #44 deployment incident | Existing 31 API Keys remain invalid unless old hash secret is recovered | Old `JWT_SECRET` was not recoverable; API Key hashes are one-way HMACs |
| `ENCRYPTION_KEY` | Recovered in production per User report | Upstream channel keys are not expected to be affected |
| Admin sessions | Must re-login after JWT secret change | JWT signing secret changed |
| Real Anthropic upstream E2E | Still not claimed by this batch | This batch hardens deployment secrets, not provider credential coverage |

> Canary 验收时：已失效的 31 个 API Key 不算本轮 FAIL；这是 PR #44 事故影响，不能在没有旧 secret 的情况下反推恢复。本轮验收重点是未来 `JWT_SECRET` 轮换不再牵连 API Key hash，以及部署流程不再删除生产 `.env`。

---

## Fix #1: Config separates API Key hash secret from JWT signing secret

**文件：** `internal/config/config.go:27-98`, `internal/config/config_test.go`

**原问题：** API Key hash 依赖 `JWT_SECRET`，导致 JWT secret 丢失或轮换会让既有 API Key 全部失效。

**当前代码：**

```go
JWTSecret        string
APIKeyHashSecret string
EncryptionKey    string

if c.APIKeyHashSecret == "" {
	return fmt.Errorf("API_KEY_HASH_SECRET environment variable is required and cannot be empty")
}

jwtSecret := getEnvStr("JWT_SECRET", "")
APIKeyHashSecret: getEnvStr("API_KEY_HASH_SECRET", jwtSecret),
```

**验证方法：**

```bash
go test ./internal/config -run 'TestValidate|TestLoad' -count=1
```

重点确认：
- `API_KEY_HASH_SECRET` 单独设置时覆盖 `JWT_SECRET`。
- 未设置 `API_KEY_HASH_SECRET` 时回落到 `JWT_SECRET`，不破坏现有部署升级路径。
- 直接构造空 `APIKeyHashSecret` 的配置会被 `Validate()` 拒绝。

---

## Fix #2: Admin API Key creation uses the stable hash secret

**文件：** `internal/admin/handlers.go:18-37`, `internal/admin/key_handlers.go:154-160`, `internal/admin/admin_test.go`

**原问题：** Admin 创建 API Key 时使用 `JWT_SECRET` 计算 `key_hash`。

**当前代码：**

```go
type Handler struct {
	jwtSecret        string
	apiKeyHashSecret string
}

func NewHandlerWithAPIKeyHashSecret(db db.DBQuerier, jwtSecret, apiKeyHashSecret string, encKey []byte, sf *filter.DFAFilter) *Handler {
	if apiKeyHashSecret == "" {
		apiKeyHashSecret = jwtSecret
	}
	return &Handler{db: db, jwtSecret: jwtSecret, apiKeyHashSecret: apiKeyHashSecret, encKey: encKey, sensitiveFilter: sf}
}

keyHash := auth.HashAPIKey(rawKey, h.apiKeyHashSecret)
```

**验证方法：**

```bash
go test ./internal/admin -run 'TestNewHandler|TestCreateAPIKey' -count=1
```

---

## Fix #3: Gateway API Key authentication uses the stable hash secret

**文件：** `internal/middleware/auth.go:204-222`, `cmd/uniroute/main.go:94-149`, `internal/middleware/auth_test_mock_test.go`

**原问题：** Gateway 校验 API Key 时同样用 `JWT_SECRET` 计算 HMAC，导致 Admin JWT secret 轮换会影响 `/v1/*` 客户端调用。

**当前代码：**

```go
func GatewayAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler {
	return GatewayAuthWithAPIKeyHashSecret(q, jwtSecret)
}

func GatewayAuthWithAPIKeyHashSecret(q db.DBQuerier, apiKeyHashSecret string) func(http.Handler) http.Handler {
	keyHash := hashKey(rawKey, apiKeyHashSecret)
	// ...
}

adminHandler := admin.NewHandlerWithAPIKeyHashSecret(postgres.Pool, cfg.JWTSecret, cfg.APIKeyHashSecret, encKey, sensitiveFilter)
gatewayAuth := middleware.GatewayAuthWithAPIKeyHashSecret(postgres.Pool, cfg.APIKeyHashSecret)
```

**验证方法：**

```bash
go test ./internal/middleware ./cmd/uniroute -run 'TestGatewayAuth|TestHashKey|TestAdminRoutesRegisteredForFrontendClient' -count=1
```

重点确认 `TestGatewayAuthWithAPIKeyHashSecret_UsesSeparateSecret` 捕获到的 SQL hash 使用 `api-key-secret`，而不是 `jwt-secret`。

---

## Fix #4: Env, compose, and canary docs expose `API_KEY_HASH_SECRET`

**文件：** `.env.example`, `docker-compose.yml`, `docker-compose.canary.yml`, `README.md`, `docs/ops/canary-testing.md`

**原问题：** 生产和 canary 环境只显式要求 `JWT_SECRET`/`ENCRYPTION_KEY`，部署人员容易遗漏 API Key hash 的稳定密钥。

**当前实现：**
- `.env.example` 新增 `API_KEY_HASH_SECRET`，并说明既有部署应先设置为当前 `JWT_SECRET`。
- backend compose env 新增 `API_KEY_HASH_SECRET: ${API_KEY_HASH_SECRET:-}`。
- canary 文档要求 stable/canary 使用相同 `API_KEY_HASH_SECRET`，否则 stable 创建的 API Key 在 canary 会失败。
- README 首次配置提示包含 `JWT_SECRET`、`API_KEY_HASH_SECRET`、`ENCRYPTION_KEY`。

**验证方法：**

```bash
rg -n 'API_KEY_HASH_SECRET|JWT_SECRET|ENCRYPTION_KEY' .env.example docker-compose.yml docker-compose.canary.yml README.md docs/ops/canary-testing.md
```

---

## Fix #5: Deployment runbook protects `.env` and documents PR #44 incident handling

**文件：** `docs/ops/deployment-runbook.md:24-66`, `docs/ops/deployment-runbook.md:117-146`

**原问题：** `rsync --delete` 可删除不在仓库中的生产 `.env`，本次 PR #44 部署已实际触发，并导致旧 `JWT_SECRET` 无法恢复。

**当前实现：**
- Runbook v1.4 明确三类密钥职责：`JWT_SECRET`、`API_KEY_HASH_SECRET`、`ENCRYPTION_KEY`。
- 现有部署升级步骤要求先把 `API_KEY_HASH_SECRET` 设置为当前 `JWT_SECRET`，验证既有 API Key 后再考虑轮换 JWT。
- `.env` 被定义为生产状态文件，并建议保留 `.env.production` 备份。
- `rsync --delete` 示例加入 `--exclude='.env'`、`--exclude='.env.production'`、`--exclude='backups/'`、`--exclude='data/'`。
- `.env` 丢失应优先从备份恢复；只有 `API_KEY_HASH_SECRET` 丢失才需要重新生成 API Key；仅 `JWT_SECRET` 丢失只影响管理员登录。

**验证方法：**

```bash
rg -n 'PR #44|rsync|\\.env.production|API_KEY_HASH_SECRET|JWT_SECRET|ENCRYPTION_KEY' docs/ops/deployment-runbook.md
```

---

## Fix #6: Status, matrix, and issue tracker reflect the incident without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/issues/OPEN-ISSUES.md`, `docs/CODEBASE-MAP.md`

**原问题：** 部署事故如果只口头记录，后续 agent 可能继续把 Batch 13 当作无风险部署完成，或不知道 `API_KEY_HASH_SECRET` 是新边界。

**当前实现：**
- `CURRENT-STATUS.md` 记录 PR #44 部署成功、Anthropic stream feature online、`.env` 删除事故和 Batch 14 本地修复状态。
- `PRD2-ACCEPTANCE-MATRIX.md` 更新 Batch 13 外部流程状态，并新增“Deployment secret persistence and API key hash secret separation”验收行，状态为 `Implemented not accepted`。
- `OPEN-ISSUES.md` 新增 `PRD2-DEPLOY-001`，Severity `High`，Status `Fixed awaiting Canary`。
- `CODEBASE-MAP.md` 记录 API Key hash 当前由 `API_KEY_HASH_SECRET` 负责，未设置时才回落到 `JWT_SECRET`。

**验证方法：**

```bash
rg -n 'Batch 14|PRD2-DEPLOY-001|API_KEY_HASH_SECRET|rsync|PR #44|\\.env.production' docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/CODEBASE-MAP.md
```

---

## 本地已执行并通过

```bash
go test ./internal/config ./internal/admin ./internal/middleware ./cmd/uniroute -run 'TestValidate|TestLoad|TestNewHandler|TestCreateAPIKey|TestGatewayAuth|TestHashKey|TestAdminRoutesRegisteredForFrontendClient' -count=1
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
```

`npm --prefix frontend test` 仍显示既有 React Router/Semi UI/jsdom warning；测试退出码为 0。`npm --prefix frontend run build` 仍显示既有 Vite large chunk warning；构建退出码为 0。

---

## Canary 全局验证建议

请从仓库根目录执行：

```bash
go test ./internal/config -run 'TestValidate|TestLoad' -count=1
go test ./internal/admin -run 'TestNewHandler|TestCreateAPIKey' -count=1
go test ./internal/middleware ./cmd/uniroute -run 'TestGatewayAuth|TestHashKey|TestAdminRoutesRegisteredForFrontendClient' -count=1
go test ./internal/config ./internal/admin ./internal/middleware ./cmd/uniroute -run 'TestValidate|TestLoad|TestNewHandler|TestCreateAPIKey|TestGatewayAuth|TestHashKey|TestAdminRoutesRegisteredForFrontendClient' -count=1
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
rg -n 'Batch 14|PRD2-DEPLOY-001|API_KEY_HASH_SECRET|rsync|PR #44|\\.env.production' docs README.md .env.example docker-compose.yml docker-compose.canary.yml internal cmd
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

Deployment smoke recommended for Atlas after Canary/Sentinel:

```bash
test -s .env
test -s .env.production
grep -E '^(JWT_SECRET|API_KEY_HASH_SECRET|ENCRYPTION_KEY)=' .env
docker compose config >/tmp/uniroute-compose-check.yml
```

Operational note: for an existing deployment whose API Keys were created before Batch 14, set `API_KEY_HASH_SECRET` to the old API-key hash secret value. If the old value is unrecoverable, already-created API Keys cannot be verified and must be recreated.

---

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 14 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 14 API Key Hash Secret and Deployment Env Hardening

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence
- Fix #6: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/config -run 'TestValidate|TestLoad' -count=1` | PASS/FAIL | |
| `go test ./internal/admin -run 'TestNewHandler|TestCreateAPIKey' -count=1` | PASS/FAIL | |
| `go test ./internal/middleware ./cmd/uniroute -run 'TestGatewayAuth|TestHashKey|TestAdminRoutesRegisteredForFrontendClient' -count=1` | PASS/FAIL | |
| `go test ./internal/config ./internal/admin ./internal/middleware ./cmd/uniroute -run 'TestValidate|TestLoad|TestNewHandler|TestCreateAPIKey|TestGatewayAuth|TestHashKey|TestAdminRoutesRegisteredForFrontendClient' -count=1` | PASS/FAIL | |
| `go test ./... -count=1` | PASS/FAIL | |
| `npm --prefix frontend test` | PASS/FAIL | note existing warnings if present |
| `npm --prefix frontend run build` | PASS/FAIL | note large chunk warning if present |
| docs grep command | PASS/FAIL | |
| `git diff --check` | PASS/FAIL | |
| `git diff --check -- $(git ls-files --others --exclude-standard)` | PASS/FAIL | |

## 部署事故复核

- PASS/FAIL — `PRD2-DEPLOY-001` 是否准确描述 PR #44 事故、影响和后续动作。
- PASS/FAIL — `deployment-runbook.md` 是否足够防止 `rsync --delete` 再次删除 `.env`。
- PASS/FAIL/SKIP — 如有 Atlas 环境权限，确认 `.env` 和 `.env.production` 均存在且三类密钥可被 compose 读取。

## 新发现

- None / list issue paths.

## 结论

PASS/FAIL. 明确说明 Batch 14 是否可交给 Sentinel，以及是否存在 Batch 15 blocker。
```
