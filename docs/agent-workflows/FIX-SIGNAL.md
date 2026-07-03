# FIX-SIGNAL.md — PRD2 Batch 22 License Production Acceptance

**修复者：** main agent
**日期：** 2026-07-03
**修复轮次：** PRD2 Batch 22

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 18 real upstream provider acceptance | 仍 Open/Deferred | User 已明确延后，Batch 22 不处理 OpenRouter/Anthropic 凭据验收 |
| Production/approved-staging License import/status | 仍需目标环境证据 | 本轮 Codex 完成本地真实后端/浏览器 smoke 和可复用脚本；是否升级为 production verified 取决于 Canary/Atlas 或用户批准的目标环境运行 |
| `UNIROUTE_BATCH22_ALLOW_LICENSE_RESET` | 必须显式设置 | Batch 22 smoke 会清空或覆盖全局 `settings.license.offline`，只能在本地、预发或用户批准环境运行 |

> Canary 验收时：前置条件中列出的未关闭目标环境证据不算本轮 FAIL；需要确认文档没有把本地 smoke 过度标记为 `Production verified`。

---

## Fix #1: Batch 22 License Seed And Test License Generator

**文件：** `scripts/prd2_batch22_license_seed.go:49`

**原问题：** Batch 22 缺少可重复生成测试离线 License 和匹配 `LICENSE_PUBLIC_KEY` 的入口，无法在真实后端上覆盖 missing/invalid/active/expiring/expired 状态。

**当前代码：**

- `print-public-key` 输出 smoke 测试公钥：`scripts/prd2_batch22_license_seed.go:49-54`
- `seed` 创建专用 platform admin、清理 fixture org 的 audit/notification 证据，并清空全局 `license.offline`：`scripts/prd2_batch22_license_seed.go:87-153`
- `store-invalid` 直接写入无效签名 fixture，用于验证真实后端的 `invalid` 当前状态：`scripts/prd2_batch22_license_seed.go:68-81`
- 生成 active/expiring/expired/invalid 四类测试 License：`scripts/prd2_batch22_license_seed.go:184-249`
- 清空/覆盖全局 license 前必须设置 `UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset`：`scripts/prd2_batch22_license_seed.go:258-263`

**验证方法：**

```bash
cd /Users/m4/Desktop/projects/UniRouter
go run ./scripts/prd2_batch22_license_seed.go -action print-public-key
```

预期输出：一行 base64 Ed25519 public key。

---

## Fix #2: Batch 22 Real Backend Browser Smoke

**文件：** `scripts/prd2_batch22_license_smoke.mjs:17`

**原问题：** P2-F10 只有 Batch 15 单元/组件证据，缺少真实后端和真实浏览器的 `/license` 导入、状态、审计、通知验收。

**当前代码：**

- 后端 readiness、seed、JWT login、missing/invalid API 状态验证：`scripts/prd2_batch22_license_smoke.mjs:17-36`
- Playwright 登录 Admin Console 并打开 `/license`：`scripts/prd2_batch22_license_smoke.mjs:38-50`
- 浏览器导入 active、expiring、expired License，并用 API 复核状态：`scripts/prd2_batch22_license_smoke.mjs:52-63`
- 验证 `license.warning` notification event：`scripts/prd2_batch22_license_smoke.mjs:249-266`
- 验证 `license.update` audit rows 且不泄漏 raw signature：`scripts/prd2_batch22_license_smoke.mjs:268-290`
- 截图输出：`scripts/prd2_batch22_license_smoke.mjs:70-86`
- 默认清理本轮测试写入的 `license.offline` fixture，除非设置 `UNIROUTE_BATCH22_KEEP_LICENSE=true`：`scripts/prd2_batch22_license_smoke.mjs:98-109`

**验证方法：**

先用 Fix #3 的 wrapper 命令运行。若 Canary 使用自己的 backend/frontend 地址，设置 `UNIROUTE_SMOKE_BACKEND_URL` 和 `UNIROUTE_SMOKE_FRONTEND_URL`。

---

## Fix #3: Batch 22 Smoke Wrapper

**文件：** `scripts/prd2-batch22-license-smoke.sh:1`

**原问题：** Canary/Atlas 没有单一入口来生成测试公钥、安装 Playwright 依赖和运行 Batch 22 smoke。

**当前代码：**

- `--print-public-key` 输出 backend 需要配置的 smoke 测试公钥：`scripts/prd2-batch22-license-smoke.sh:7-10`
- 校验 Go/npm/npx 依赖：`scripts/prd2-batch22-license-smoke.sh:12-25`
- 支持自定义 backend/frontend URL：`scripts/prd2-batch22-license-smoke.sh:27-31`
- 安装/跳过 Playwright Chromium 后运行 Node smoke：`scripts/prd2-batch22-license-smoke.sh:33-41`

**验证方法：**

```bash
cd /Users/m4/Desktop/projects/UniRouter
bash -n scripts/prd2-batch22-license-smoke.sh
scripts/prd2-batch22-license-smoke.sh --print-public-key
```

---

## Fix #4: Batch 22 Evidence And Runbook Updates

**文件：** `docs/ops/evidence/PRD2-B22-license-local-smoke-20260703.md:1`, `docs/ops/deployment-runbook.md:57`, `docs/tech-ref/16-license-foundation.md:88`

**原问题：** Batch 22 执行后缺少证据归档和可复用操作说明，后续 agent 容易把本地 smoke、Canary 验收和生产验收混为一谈。

**当前代码/文档：**

- Local evidence 文件记录目标、环境、命令、PASS 输出、覆盖范围和边界：`docs/ops/evidence/PRD2-B22-license-local-smoke-20260703.md:1-88`
- deployment runbook 增加非生产 License smoke 操作入口和 reset 护栏说明：`docs/ops/deployment-runbook.md:57-71`
- License 技术参考记录 Batch 22 smoke 覆盖范围和非生产边界：`docs/tech-ref/16-license-foundation.md:88-106`
- 文档索引列出 License 技术参考：`docs/DOC-INVENTORY.md`

**验证方法：**

```bash
cd /Users/m4/Desktop/projects/UniRouter
rg -n "PRD2-B22|prd2-batch22-license|UNIROUTE_BATCH22_ALLOW_LICENSE_RESET" docs/ops/evidence docs/ops/deployment-runbook.md docs/tech-ref/16-license-foundation.md docs/DOC-INVENTORY.md
```

---

## Fix #5: Status, Matrix, Open Issues, And Completion Plan Updated Without Overclaim

**文件：** `docs/ops/CURRENT-STATUS.md:4`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:72`, `docs/issues/OPEN-ISSUES.md`, `docs/ops/PRD2-COMPLETION-PLAN.md`

**原问题：** Batch 22 需要推进 P2-F10，但不能把本地 smoke 直接标为 production verified；同时 open issues 和 completion plan 必须反映当前下一步。

**当前文档：**

- `CURRENT-STATUS.md` 标记 Batch 22 fixed locally awaiting Canary，并引用本地 evidence。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 P2-F10 从 `Unit tested` 推进到 `Implemented not accepted`，保留 Canary/目标环境 gap。
- `OPEN-ISSUES.md` 新增 `PRD2-B22-001`，状态 `Fixed awaiting Canary`。
- `PRD2-COMPLETION-PLAN.md` 的 Batch 22 段记录实现文件、命令、实际 PASS 输出和剩余边界。

**验证方法：**

```bash
cd /Users/m4/Desktop/projects/UniRouter
rg -n "Batch 22|PRD2-B22|P2-F10 License foundation|Implemented not accepted|Production verified" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/PRD2-COMPLETION-PLAN.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go run ./scripts/prd2_batch22_license_seed.go -action print-public-key
bash -n scripts/prd2-batch22-license-smoke.sh
node --check scripts/prd2_batch22_license_smoke.mjs
go test ./internal/licensing ./internal/admin -run 'TestVerifier|Test(GetLicense|UpdateLicense)' -count=1
npm --prefix frontend test -- --run LicensePage client navigation
go test ./...
npm --prefix frontend run build
git diff --check
```

### 可选真实后端/浏览器 smoke

在本地或用户批准的预发/目标环境启动 backend 时，先配置 `LICENSE_PUBLIC_KEY` 为：

```text
0NBjZfaRzoseRasshluB1vGr+kwzBj9qISlCOXQvmyM=
```

本地示例：

```bash
LISTEN_PORT=18082 \
JWT_SECRET=batch22-jwt-secret \
API_KEY_HASH_SECRET=batch22-api-key-secret \
ENCRYPTION_KEY=12345678901234567890123456789012 \
LICENSE_PUBLIC_KEY=0NBjZfaRzoseRasshluB1vGr+kwzBj9qISlCOXQvmyM= \
LICENSE_EXPIRY_WARNING_DAYS=30 \
go run ./cmd/uniroute
```

另一个 shell 运行：

```bash
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18082 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch22-license-smoke.sh
```

Codex 本地实际输出：

```json
{
  "status": "PASS",
  "batch": 22,
  "backend_url": "http://localhost:18082",
  "frontend_url": "http://localhost:3000",
  "org_id": 41,
  "user_id": 27,
  "public_key": "0NBjZfaRzoseRasshluB1vGr+kwzBj9qISlCOXQvmyM=",
  "active_deployment_id": "prd2-b22-active",
  "expiring_deployment_id": "prd2-b22-expiring",
  "expired_deployment_id": "prd2-b22-expired",
  "warning_event_id": 12,
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch22-license.png"
}
```

---

## 期望输出格式

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 22 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 22 License Production Acceptance

## 逐条结果

- Fix #1: PASS/FAIL — [seed/generator 结果]
- Fix #2: PASS/FAIL/SKIP — [真实后端/浏览器 smoke 结果；无运行环境时说明 SKIP 原因]
- Fix #3: PASS/FAIL — [wrapper 结果]
- Fix #4: PASS/FAIL — [evidence/runbook/tech-ref 结果]
- Fix #5: PASS/FAIL — [status/matrix/issues/completion plan 是否无 overclaim]

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go run ./scripts/prd2_batch22_license_seed.go -action print-public-key` | PASS/FAIL | |
| `bash -n scripts/prd2-batch22-license-smoke.sh` | PASS/FAIL | |
| `node --check scripts/prd2_batch22_license_smoke.mjs` | PASS/FAIL | |
| `go test ./internal/licensing ./internal/admin -run 'TestVerifier\|Test(GetLicense\|UpdateLicense)' -count=1` | PASS/FAIL | |
| `npm --prefix frontend test -- --run LicensePage client navigation` | PASS/FAIL | |
| `go test ./...` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | large chunk warning is existing/non-blocking unless new errors appear |
| `git diff --check` | PASS/FAIL | |
| Batch 22 real backend/browser smoke | PASS/FAIL/SKIP | 如果 SKIP，写明缺少 backend/frontend/DB/Redis/Playwright 哪一项 |

## 新发现

- [如无，写“无新发现”]

## 结论

PASS/FAIL。若 PASS 但未运行目标环境 smoke，请明确说明：本轮可交给 Sentinel；production/approved-staging License import/status evidence 仍需 Atlas 或目标环境执行后才能标记 `Production verified`。
```
