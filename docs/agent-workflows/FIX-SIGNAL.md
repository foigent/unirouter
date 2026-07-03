# FIX-SIGNAL.md — PRD2 Batch 24 OIDC First SSO

**修复者：** main agent
**日期：** 2026-07-04
**修复轮次：** PRD2 Batch 24

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 18 real upstream provider acceptance | 仍 Open/Deferred | User 已明确延后，OIDC 不替代 OpenRouter/Anthropic 真实上游验收 |
| P2-F10 License production/approved-staging evidence | 仍 Open | Batch 24 不处理 License target-environment promotion |
| Project budget hard-block enforcement | 仍 Open | Batch 24 只处理 OIDC-first SSO |
| Production HA/DR | 仍 Open | Batch 24 不处理生产级 HA/DR |
| Real enterprise IdP acceptance | 仍为目标环境证据 | 本轮提供 mock OIDC provider 的本地可重复验证，真实 IdP 需要 operator issuer/client 配置 |

> Canary 验收时：以上未关闭项不算本轮 FAIL；需要确认文档没有把 mock-provider evidence 过度标记为 production/target verified。

---

## Fix #1: OIDC Core Service

**文件：** `internal/oidc/service.go`, `internal/oidc/service_test.go`

**原问题：** PRD2 completion plan 中 Batch 24 要求 OIDC-first SSO，但代码没有 OIDC discovery、login start、callback token exchange、claim validation 或 claim mapping。

**当前代码：**

- 新增 `internal/oidc` package。
- 支持 OIDC discovery：`/.well-known/openid-configuration`。
- 支持 authorization URL 构建：`response_type=code`, `client_id`, `redirect_uri`, `scope`, `state`, `nonce`。
- 支持 token endpoint code exchange。
- 支持 RS256 `id_token` JWKS 验证。
- 校验 issuer、audience、expiry、nonce。
- 映射 `email`、`preferred_username`、`uniroute_role`、`uniroute_org_id` 等 claims。
- `OIDC_AUTO_PROVISION=false` 的预绑定模式不强制 org claim；auto-provision 模式强制 org。

**验证方法：**

```bash
go test ./internal/oidc -count=1
```

---

## Fix #2: Admin OIDC Routes, State Cookie, Provisioning, JWT, Audit

**文件：** `internal/admin/oidc_handlers.go`, `internal/admin/handlers.go`, `cmd/uniroute/main.go`, `cmd/uniroute/route_parity_test.go`, `migrations/000015_prd2_oidc_sso.up.sql`, `migrations/000015_prd2_oidc_sso.down.sql`

**原问题：** Admin Console 只有用户名密码登录，无法通过 OIDC provider 登录，也没有本地用户绑定字段或审计行为。

**当前代码：**

- 新增 public endpoints:
  - `GET /api/v1/auth/oidc/config`
  - `GET /api/v1/auth/oidc/start`
  - `GET /api/v1/auth/oidc/callback`
- `start` 生成随机 `state`/`nonce`，写入 HMAC-signed HttpOnly SameSite=Lax cookie。
- `callback` 校验 state cookie，交换 code，验证 `id_token`，映射本地用户。
- 新增 users OIDC binding columns：`email`, `auth_provider`, `oidc_issuer`, `oidc_subject`。
- Auto-provision 时按 claim/default 创建用户；project-scoped role 会写入 default project membership。
- 登录成功后签发现有 UniRouter admin JWT，复用现有 `AdminAuth`/RBAC。
- callback 默认返回 same-origin HTML 写入 localStorage 后跳转，不把 JWT 放进 URL query。
- 写 `login.oidc` audit，不记录 `id_token`、`access_token`、client secret 或 raw provider response。

**验证方法：**

```bash
go test ./internal/admin ./cmd/uniroute -count=1
```

---

## Fix #3: OIDC Environment Configuration

**文件：** `internal/config/config.go`, `internal/config/config_test.go`, `.env.example`, `docker-compose.yml`

**原问题：** 没有 OIDC provider 配置入口，也没有默认关闭/显式开启的部署边界。

**当前代码/配置：**

- 新增 `OIDC_ENABLED=false` 默认关闭。
- 启用时强校验：
  - `OIDC_ISSUER_URL`
  - `OIDC_CLIENT_ID`
  - `OIDC_CLIENT_SECRET`
  - `OIDC_REDIRECT_URL`
- 支持可选项：
  - `OIDC_PROVIDER_NAME`
  - `OIDC_SCOPES`
  - `OIDC_ALLOWED_EMAIL_DOMAIN`
  - `OIDC_AUTO_PROVISION`
  - `OIDC_DEFAULT_ORG_ID`
  - `OIDC_DEFAULT_ROLE`
  - `OIDC_DEFAULT_PROJECT_SLUG`
  - `OIDC_USERNAME_CLAIM`
  - `OIDC_EMAIL_CLAIM`
  - `OIDC_ROLE_CLAIM`
  - `OIDC_ORG_ID_CLAIM`
- `.env.example` 和 `docker-compose.yml` 均透传这些变量。

**验证方法：**

```bash
go test ./internal/config -count=1
```

---

## Fix #4: Frontend Login Entry

**文件：** `frontend/src/api/client.ts`, `frontend/src/pages/LoginPage.tsx`, `frontend/src/pages/__tests__/LoginPage.test.tsx`, `frontend/src/api/__tests__/client.test.ts`, `frontend/src/index.css`

**原问题：** 即使后端实现 OIDC，登录页也没有 SSO entry，前端 client 也没有 public config/start route contract。

**当前代码：**

- API client 新增 `getOIDCConfig()` 和 `getOIDCStartURL()`.
- LoginPage mount 后读取 `/auth/oidc/config`。
- 只有后端返回 `enabled=true` 时显示 SSO 按钮。
- SSO 按钮跳转到 `/api/v1/auth/oidc/start?redirect=/`。
- 现有用户名密码登录和角色跳转保持不变。

**验证方法：**

```bash
npm --prefix frontend test -- --run LoginPage client navigation
```

---

## Fix #5: Operator Docs And Local Smoke Evidence

**文件：** `docs/tech-ref/18-oidc-sso.md`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`, `docs/ops/deployment-runbook.md`, `scripts/prd2-batch24-oidc-smoke.sh`, `docs/ops/evidence/PRD2-B24-oidc-local-smoke-20260704.md`

**原问题：** OIDC 需要明确部署配置、claim mapping、first-login provisioning、security boundary 和 repeatable verification，否则容易把 mock evidence 误写成真实企业 IdP 验收。

**当前文档/脚本：**

- 新增 OIDC tech-ref，记录 flow、env vars、claim mapping、auto-provision、audit 和安全边界。
- 部署 runbook 增加 OIDC provider setup 和 mock-provider smoke。
- 新增 Batch 24 smoke wrapper：

```bash
scripts/prd2-batch24-oidc-smoke.sh
```

- 新增本地 mock-provider evidence：

```text
docs/ops/evidence/PRD2-B24-oidc-local-smoke-20260704.md
```

**验证方法：**

```bash
bash -n scripts/prd2-batch24-oidc-smoke.sh
scripts/prd2-batch24-oidc-smoke.sh
rg -n "OIDC|Batch 24|18-oidc-sso" docs/tech-ref/18-oidc-sso.md docs/tech-ref/README.md docs/DOC-INVENTORY.md docs/ops/deployment-runbook.md docs/ops/evidence/PRD2-B24-oidc-local-smoke-20260704.md
```

---

## Fix #6: Status, Matrix, Issues, Completion Plan Discipline

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/issues/OPEN-ISSUES.md`, `docs/ops/PRD2-COMPLETION-PLAN.md`

**原问题：** Batch 24 本地实现完成后，当前状态需要反映“fixed locally awaiting Canary”，但不能提前写成 Canary/Sentinel/Atlas complete。

**当前文档预期：**

- `PRD2-AUTH-001` 标为 `Fixed awaiting Canary`。
- OIDC row 标为 `Implemented not accepted`。
- Current status 记录 Batch 24 local implementation and smoke PASS。
- Completion plan 记录 Batch 24 本地完成，真实企业 IdP 仍需目标环境证据。
- Batch 18 provider、P2-F10 License target evidence、budget hard-block、production HA/DR 仍保持开放。

**验证方法：**

```bash
rg -n "Batch 24|OIDC|PRD2-AUTH-001|Implemented not accepted|Fixed awaiting Canary" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/PRD2-COMPLETION-PLAN.md
git diff --check
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1
npm --prefix frontend test -- --run LoginPage client navigation
bash -n scripts/prd2-batch24-oidc-smoke.sh
scripts/prd2-batch24-oidc-smoke.sh
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## 期望输出格式

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 24 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 24 OIDC First SSO

## 逐条结果

- Fix #1: PASS/FAIL — [OIDC core service/discovery/JWKS/id_token validation]
- Fix #2: PASS/FAIL — [admin OIDC routes/state/callback/provision/JWT/audit]
- Fix #3: PASS/FAIL — [config/env/compose]
- Fix #4: PASS/FAIL — [frontend login entry and client route contract]
- Fix #5: PASS/FAIL — [docs/smoke/evidence boundary]
- Fix #6: PASS/FAIL — [status/matrix/issues/completion discipline]

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1` | PASS/FAIL | |
| `npm --prefix frontend test -- --run LoginPage client navigation` | PASS/FAIL | |
| `bash -n scripts/prd2-batch24-oidc-smoke.sh` | PASS/FAIL | |
| `scripts/prd2-batch24-oidc-smoke.sh` | PASS/FAIL | |
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | large chunk warning is existing/non-blocking unless new errors appear |
| `git diff --check` | PASS/FAIL | |
| Real enterprise IdP browser login | PASS/FAIL/SKIP | 如果 SKIP，写明缺少 approved issuer/client credentials 或 target environment |

## 新发现

- [如无，写“无新发现”]

## 结论

PASS/FAIL。
```
