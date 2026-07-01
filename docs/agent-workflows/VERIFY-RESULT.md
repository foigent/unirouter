# VERIFY-RESULT.md — PRD2 Batch 14 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 14 API Key Hash Secret and Deployment Env Hardening

## 逐条结果

- Fix #1: **PASS** — `API_KEY_HASH_SECRET` 已添加到 config，`Validate()` 要求非空。`TestLoad_APIKeyHashSecretOverride` 确认单独设置时覆盖 `JWT_SECRET`。`TestValidate_MissingAPIKeyHashSecret` 确认空值被拒绝。未设置时回落到 `JWT_SECRET`，不破坏现有部署升级路径。
- Fix #2: **PASS** — `NewHandlerWithAPIKeyHashSecret` 接受独立 `apiKeyHashSecret` 参数，为空时回落到 `jwtSecret`。`auth.HashAPIKey(rawKey, h.apiKeyHashSecret)` 使用独立 secret 计算 hash。`TestNewHandler` 和 `TestNewHandlerWithAPIKeyHashSecret` 通过。
- Fix #3: **PASS** — `GatewayAuthWithAPIKeyHashSecret` 使用独立 secret 计算 key hash。`cmd/uniroute/main.go` 传入 `cfg.APIKeyHashSecret` 给 gateway auth 和 admin handler。`TestGatewayAuthWithAPIKeyHashSecret_UsesSeparateSecret` 确认 SQL hash 使用 `api-key-secret` 而非 `jwt-secret`。
- Fix #4: **PASS** — `.env.example` 新增 `API_KEY_HASH_SECRET` 及说明。`docker-compose.yml` 和 `docker-compose.canary.yml` 新增 `API_KEY_HASH_SECRET: ${API_KEY_HASH_SECRET:-}`。`README.md` 首次配置提示包含三类密钥。`canary-testing.md` 要求 stable/canary 使用相同 `API_KEY_HASH_SECRET`。
- Fix #5: **PASS** — `deployment-runbook.md` v1.4 明确三类密钥职责（JWT/API Key Hash/Encryption），升级步骤要求先设置 `API_KEY_HASH_SECRET` 为当前 `JWT_SECRET`。rsync 示例加入 `--exclude='.env'`/`.env.production`/`backups/`/`data/`。`.env.production` 备份建议。PR #44 事故完整复盘。
- Fix #6: **PASS** — `CURRENT-STATUS.md` 记录 PR #44 部署事故 + Batch 14 本地修复。`PRD2-ACCEPTANCE-MATRIX.md` 新增 "Deployment secret persistence" 行（Implemented not accepted）。`OPEN-ISSUES.md` 新增 `PRD2-DEPLOY-001`（High, Fixed awaiting Canary）。`CODEBASE-MAP.md` 更新 API Key hash 使用 `API_KEY_HASH_SECRET`。

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/config -run 'TestValidate\|TestLoad' -count=1` | ✅ PASS | 8 tests, 0.793s |
| `go test ./internal/admin -run 'TestNewHandler\|TestCreateAPIKey' -count=1` | ✅ PASS | 9 tests, 0.439s |
| `go test ./internal/middleware ./cmd/uniroute -run 'TestGatewayAuth\|TestHashKey\|TestAdminRoutesRegisteredForFrontendClient' -count=1` | ✅ PASS | 13 tests |
| combined targeted test | ✅ PASS | 4 packages |
| `go test ./... -count=1` | ✅ PASS | 19 packages, 0 FAIL |
| `npm --prefix frontend test` | ✅ PASS | 9 files, 64 tests |
| `npm --prefix frontend run build` | ✅ PASS | large chunk warning（既有） |
| docs grep command | ✅ PASS | Batch 14/PRD2-DEPLOY-001 引用一致 |
| `git diff --check` | ✅ PASS | exit 0 |
| `git diff --check -- $(git ls-files --others --exclude-standard)` | ✅ PASS | exit 0 |

## 部署事故复核

- **PASS** — `PRD2-DEPLOY-001` 准确描述 PR #44 事故：rsync --delete 删除 .env → JWT_SECRET 丢失 → 31 API Key 失效。影响和后续动作清晰。
- **PASS** — `deployment-runbook.md` v1.4 足够防止 rsync --delete 再次删除 .env：`--exclude='.env'` + `.env.production` 备份建议。
- **SKIP** — 无 Atlas 环境权限，无法确认服务器 .env 和 .env.production 是否存在。

## 新发现

- 无新发现。所有测试通过，代码/文档实现与 FIX-SIGNAL 描述一致。
- 代码当前为 uncommitted 状态，需 Sentinel commit/push。

## 结论

**PASS (6/6)**。Batch 14 可交给 Sentinel 进行 commit/push/PR 操作。无 Batch 15 blocker。

`API_KEY_HASH_SECRET` 实现正确：独立于 `JWT_SECRET`，有向后兼容回落，admin 和 gateway 均已接入。部署 runbook 防护措施充分（rsync exclude + .env 备份 + 三类密钥职责文档）。文档一致且准确记录了 PR #44 事故。

**运营提醒**：既有部署升级时，需先将 `API_KEY_HASH_SECRET` 设置为当前 `JWT_SECRET` 值，否则已有 API Key 会失效。
