# FIX-SIGNAL.md — PRD2 Batch 15 License Foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 Batch 15

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|----------|------|
| PRD2 Batch 14 | Canary/Sentinel/Atlas completed; PR #45 merged at `30e74a3` per current repo/user report | Batch 15 基于 Batch 14 之后的 `main` 继续开发 |
| P2-F11 Prompt/Response storage policy | 仍不实现 | 用户此前明确下一版再讨论是否需要存储；本轮不抢跑数据留存策略 |
| License enforcement | 不阻断网关请求 | 本轮是 foundation：导入、验签、状态、审计、Admin Console；不做在线激活、硬件指纹、代理商/市场、请求拦截 |
| Production license import | 本地未执行 | 需要真实签发公钥和生产 License，由 Atlas/用户环境完成 |
| Browser smoke against real backend | 本地未执行 | 本轮已做前端单测/构建；真实后端浏览器 smoke 可由 Canary/Atlas 在目标环境补充 |

> Canary 验收时：上表中的未实现项不是 FAIL。FAIL 范围应限于本轮声明的 foundation 能力、测试/构建、文档一致性和安全边界。

---

## Fix #1: Offline license verifier

**文件：** `internal/licensing/license.go:13-240`, `internal/licensing/license_test.go:38-142`

**原问题：** PRD2 P2-F10 需要离线 License 文件、客户/部署/套餐/有效期元数据、签名校验、生命周期状态和临期 warning，但代码库没有统一 verifier。

**当前代码：**

```go
const (
	StatusMissing  = "missing"
	StatusActive   = "active"
	StatusExpiring = "expiring"
	StatusExpired  = "expired"
	StatusInvalid  = "invalid"
)

type Payload struct {
	Customer     string            `json:"customer"`
	DeploymentID string            `json:"deployment_id"`
	Tier         string            `json:"tier"`
	IssuedAt     string            `json:"issued_at,omitempty"`
	ExpiresAt    string            `json:"expires_at"`
	Features     []string          `json:"features,omitempty"`
	Limits       map[string]int64  `json:"limits,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func (v *Verifier) VerifyBytes(raw []byte) Status {
	// empty -> missing; missing public key -> invalid with clear error
	// valid Ed25519 signature + expiry -> active/expiring/expired
}
```

**验证方法：**

```bash
go test ./internal/licensing -run 'TestVerifier' -count=1
```

重点确认：
- `active`、`expiring`、`expired`、`invalid`、`missing public key` 都有覆盖。
- Ed25519 public key 支持 base64/raw-base64/hex 解析。
- 签名是对 canonical payload JSON 校验，非签发方私钥签名会失败。

---

## Fix #2: Config and backend route wiring

**文件：** `internal/config/config.go:32-108`, `internal/config/config_test.go`, `cmd/uniroute/main.go:95-100`, `cmd/uniroute/main.go:319-320`, `cmd/uniroute/route_parity_test.go:69-70`, `.env.example:29-32`, `docker-compose.yml:53-54`, `docker-compose.canary.yml:55-56`

**原问题：** 服务没有 License 公钥配置、临期窗口配置，也没有平台管理员 License API。

**当前代码：**

```go
LicensePublicKey         string
LicenseExpiryWarningDays int

if c.LicenseExpiryWarningDays < 0 {
	return fmt.Errorf("LICENSE_EXPIRY_WARNING_DAYS must be >= 0")
}

LicensePublicKey:         getEnvStr("LICENSE_PUBLIC_KEY", ""),
LicenseExpiryWarningDays: getEnvInt("LICENSE_EXPIRY_WARNING_DAYS", 30),
```

```go
licenseVerifier, err := licensing.NewVerifierFromPublicKey(cfg.LicensePublicKey, cfg.LicenseExpiryWarningDays)
if err != nil {
	log.Fatalf("Invalid license configuration: %v", err)
}
adminHandler.SetLicenseVerifier(licenseVerifier)

mux.Handle("GET /api/v1/license", platformRoute(adminHandler.GetLicense))
mux.Handle("PUT /api/v1/license", platformRoute(adminHandler.UpdateLicense))
```

**验证方法：**

```bash
go test ./internal/config ./cmd/uniroute -run 'TestValidate|TestLoad|TestAdminRoutesRegisteredForFrontendClient' -count=1
rg -n 'LICENSE_PUBLIC_KEY|LICENSE_EXPIRY_WARNING_DAYS|/api/v1/license' .env.example docker-compose.yml docker-compose.canary.yml cmd internal
```

重点确认：
- `LICENSE_EXPIRY_WARNING_DAYS < 0` 会被拒绝。
- `LICENSE_PUBLIC_KEY` 缺失时服务可启动，但导入 License 会失败并返回清晰错误。
- License API 走 `platformRoute`，仅 `admin` / `platform_admin`。

---

## Fix #3: Settings-backed license storage and audit

**文件：** `internal/admin/handlers.go:18-44`, `internal/admin/license_handlers.go:13-137`, `internal/admin/license_handlers_test.go:51-161`

**原问题：** PRD2 要求 License 元数据对平台管理员可见，License 变更需要审计，非法 License 要给清晰错误；代码库没有存储和审计入口。

**当前代码：**

```go
const licenseSettingKey = "license.offline"

func (h *Handler) UpdateLicense(w http.ResponseWriter, r *http.Request) {
	status := verifier.VerifyBytes(raw)
	if status.Status == licensing.StatusInvalid {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": status.Error, "status": status})
		return
	}

	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ($1, $2::jsonb, 'Offline UniRouter license document', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2::jsonb, description = EXCLUDED.description, updated_at = NOW()`,
		licenseSettingKey, string(normalized))

	h.recordAuditFromRequest(r, "license.update", "license", licenseResourceID(status), map[string]interface{}{
		"before": licenseAuditSummary(beforeStatus),
		"after":  licenseAuditSummary(status),
	})
}
```

**验证方法：**

```bash
go test ./internal/admin -run 'TestGetLicense|TestUpdateLicense' -count=1
```

重点确认：
- `GET /api/v1/license` 无存储时返回 `missing`。
- 有效 License 会保存到 `settings` 的 `license.offline`。
- 非法签名返回 400，且不会写数据库。
- `license.update` 审计只写 before/after summary，不写原始 `signature`。

---

## Fix #4: Admin Console License page and platform-only frontend access

**文件：** `frontend/src/api/client.ts:201-204`, `frontend/src/navigation.tsx:55-200`, `frontend/src/main.tsx:16-98`, `frontend/src/pages/LicensePage.tsx:74-218`, `frontend/src/pages/__tests__/LicensePage.test.tsx`, `frontend/src/__tests__/navigation.test.tsx`, `frontend/src/api/__tests__/client.test.ts`

**原问题：** 平台管理员没有 License 自助查看/导入页面；前端导航和 route guard 没有 License 权限边界。

**当前代码：**

```ts
// License
getLicense: () => request('/license'),
updateLicense: (license: any) =>
  request('/license', { method: 'PUT', body: JSON.stringify({ license }) }),
```

```ts
{ itemKey: '/license', text: 'License', icon: <IconShield /> }

if (route === '/license') return isPlatformAdminRole(role);
```

```tsx
<Route path="license" element={<RoleRoute><LicensePage /></RoleRoute>} />
```

**验证方法：**

```bash
npm --prefix frontend test -- LicensePage client navigation
npm --prefix frontend test
npm --prefix frontend run build
```

重点确认：
- License page 能展示状态/元数据/限制。
- 粘贴 JSON 后调用 `api.updateLicense()`。
- `org_admin`、billing、project roles 看不到/进不了 `/license`。
- `admin`、`platform_admin` 可访问 `/license`。

---

## Fix #5: Docs, tech-ref, and acceptance matrix

**文件：** `docs/tech-ref/16-license-foundation.md:1-86`, `docs/tech-ref/README.md`, `docs/CODEBASE-MAP.md`, `docs/ops/deployment-runbook.md`, `docs/ops/canary-testing.md`, `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `frontend/src/pages/ApiDocsPage.tsx:44-45`, `cmd/uniroute/main.go:232-233`

**原问题：** 新的 License foundation 如果只改代码，后续 agent/部署人员无法知道格式、配置、公钥缺失行为、非目标范围和验收缺口。

**当前实现：**
- `docs/tech-ref/16-license-foundation.md` 记录 License JSON 格式、Ed25519 签名规则、API、状态、审计字段、非目标。
- `.env.example`、compose、runbook、canary-testing 记录 `LICENSE_PUBLIC_KEY` 和 `LICENSE_EXPIRY_WARNING_DAYS`。
- `CURRENT-STATUS.md` 和 `PRD2-ACCEPTANCE-MATRIX.md` 只标记本地验证通过，未 overclaim Canary/Sentinel/Atlas。
- API docs 列出 `GET /api/v1/license` 和 `PUT /api/v1/license`。

**验证方法：**

```bash
rg -n 'Batch 15|License foundation|LICENSE_PUBLIC_KEY|LICENSE_EXPIRY_WARNING_DAYS|/api/v1/license|license.update|internal/licensing|P2-F10' docs README.md .env.example docker-compose.yml docker-compose.canary.yml internal cmd frontend/src
```

---

## 本地已执行并通过

```bash
go test ./internal/licensing ./internal/config ./internal/admin ./cmd/uniroute -run 'TestVerifier|TestValidate|TestLoad|TestGetLicense|TestUpdateLicense|TestAdminRoutesRegisteredForFrontendClient' -count=1
npm --prefix frontend test -- LicensePage client navigation
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
rg -n 'Batch 15|License foundation|LICENSE_PUBLIC_KEY|LICENSE_EXPIRY_WARNING_DAYS|/api/v1/license|license.update|internal/licensing|P2-F10' docs README.md .env.example docker-compose.yml docker-compose.canary.yml internal cmd frontend/src
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

`npm --prefix frontend test` 仍显示既有 React Router/Semi UI/jsdom warning；测试退出码为 0。`npm --prefix frontend run build` 仍显示既有 Vite large chunk warning；构建退出码为 0。

---

## Canary 全局验证建议

请从仓库根目录执行：

```bash
go test ./internal/licensing -run 'TestVerifier' -count=1
go test ./internal/config ./cmd/uniroute -run 'TestValidate|TestLoad|TestAdminRoutesRegisteredForFrontendClient' -count=1
go test ./internal/admin -run 'TestGetLicense|TestUpdateLicense' -count=1
go test ./internal/licensing ./internal/config ./internal/admin ./cmd/uniroute -run 'TestVerifier|TestValidate|TestLoad|TestGetLicense|TestUpdateLicense|TestAdminRoutesRegisteredForFrontendClient' -count=1
npm --prefix frontend test -- LicensePage client navigation
go test ./... -count=1
npm --prefix frontend test
npm --prefix frontend run build
rg -n 'Batch 15|License foundation|LICENSE_PUBLIC_KEY|LICENSE_EXPIRY_WARNING_DAYS|/api/v1/license|license.update|internal/licensing|P2-F10' docs README.md .env.example docker-compose.yml docker-compose.canary.yml internal cmd frontend/src
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

Optional manual/browser checks if Canary has a running backend/frontend:
- `admin` / `platform_admin` can open `/license`; `org_admin` redirects away.
- Missing `LICENSE_PUBLIC_KEY` returns a clear import error and does not save.
- With a valid Ed25519 public key/license pair, import succeeds, status shows `active` or `expiring`, and audit contains `license.update` without raw `signature`.

---

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 15 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 15 License Foundation

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./internal/licensing -run 'TestVerifier' -count=1` | PASS/FAIL | |
| `go test ./internal/config ./cmd/uniroute -run 'TestValidate|TestLoad|TestAdminRoutesRegisteredForFrontendClient' -count=1` | PASS/FAIL | |
| `go test ./internal/admin -run 'TestGetLicense|TestUpdateLicense' -count=1` | PASS/FAIL | |
| targeted combined backend command | PASS/FAIL | |
| `npm --prefix frontend test -- LicensePage client navigation` | PASS/FAIL | |
| `go test ./... -count=1` | PASS/FAIL | |
| `npm --prefix frontend test` | PASS/FAIL | note existing warnings if present |
| `npm --prefix frontend run build` | PASS/FAIL | note large chunk warning if present |
| docs grep command | PASS/FAIL | |
| `git diff --check` | PASS/FAIL | |
| untracked diff check | PASS/FAIL | |

## 边界复核

- P2-F11 prompt/response storage 是否仍未实现: PASS/FAIL
- License online activation/hardware fingerprint/reseller/marketplace 是否未被引入: PASS/FAIL
- Gateway request blocking 是否未被提前引入: PASS/FAIL
- License audit 是否未写 raw signature: PASS/FAIL

## 结论

PASS/FAIL. 明确说明 Batch 15 是否可交给 Sentinel，以及是否存在 Batch 16 blocker。
```
