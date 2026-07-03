# PRD2 Batch 27 Copy And Navigation Polish Local Evidence

**Date:** 2026-07-04
**Owner:** main agent
**Scope:** post-closeout UI/copy polish after User review. This batch does not reopen PRD2 feature scope and does not change backend billing, metering, reconciliation, or provider-routing behavior.

## User Findings Covered

1. API 文档和退出登录在侧栏底部重合。
2. 渠道测试延迟高，需要确认是否是 ECS/上游问题，以及是否消耗 token。
3. 调用日志详情面板和原日志记录底色都为白色，层级不清。
4. 菜单命名偏消费级或不够企业级，例如 `钱包管理` / `Token对账`。
5. 左侧 UniRouter logo 下方 `AI Gateway Console` 不够专业。
6. 登录页 `OpenAI 中转服务管理平台`、模型状态预览和平台描述需要企业化。
7. 登录页 `管理渠道、API keys、钱包、日志和运营监控` 需要更专业。

## Implementation Evidence

### Sidebar footer separation

- `frontend/src/App.tsx:56` keeps the product name as `UniRouter`.
- `frontend/src/App.tsx:57` changes the sidebar caption to `Enterprise Intelligence Foundation`.
- `frontend/src/App.tsx:60` removes the Semi `Nav` footer prop and renders `退出登录` as a sibling `.ur-sider-footer` at `frontend/src/App.tsx:67`.
- `frontend/src/index.css:197` styles `.ur-sider-footer` with its own border, background, and padding so it no longer competes with `API 文档` inside the scrollable navigation.

### Request log detail contrast

- `frontend/src/index.css:1156` changes `.ur-explorer-detail` to a blue-tinted gradient panel with a 3px left rail and inset shadow.
- `frontend/src/index.css:1224` updates the sticky detail header background and border.
- `frontend/src/index.css:1257` keeps cards white inside the tinted detail panel, improving panel/card separation.
- `frontend/src/pages/LogsPage.tsx` renames the page to `请求日志`, localizes `计量来源`, and changes `Request inspector` to `请求检查器`.

### Enterprise naming and login copy

- `frontend/src/navigation.tsx` renames visible menu groups and items:
  - `AI 流量` -> `流量治理`
  - `渠道管理` -> `上游通道`
  - `上游密钥` -> `上游凭证`
  - `API 密钥` -> `访问凭证`
  - `调用日志` -> `请求日志`
  - `渠道测试` -> `连通性检测`
  - `账单` -> `财务治理`
  - `钱包管理` -> `资金账户`
  - `模型价格` -> `模型定价`
  - `组织倍率` -> `价格策略`
  - `Token对账` -> `用量对账`
  - `赠送管理` -> `额度调整`
  - `权限` -> `访问控制`
  - `系统` -> `系统治理`
  - `Developer Console` -> `开发者工作台`
- Page titles and descriptions were aligned in the touched pages: `ChannelsPage`, `ChannelKeysPage`, `KeysPage`, `WalletPage`, `ReconciliationPage`, `ModelPricesPage`, `OrgModelPricesPage`, `GrantsPage`, `TestPage`, `LogsPage`, `DeveloperConsolePage`, and `ApiDocsPage`.
- `frontend/src/pages/LoginPage.tsx` now shows `Enterprise Intelligence Foundation`, Chinese preview metrics/statuses, and the description `统一治理模型接入、访问凭证、用量成本、审计日志与运行状态。`

### Channel test behavior clarification

- No backend code was changed for channel testing in this batch.
- `internal/admin/test_handlers.go:25` reads the selected channel and adapter config.
- `internal/admin/test_handlers.go:57` reads the first active upstream channel key.
- `internal/admin/test_handlers.go:74` decrypts the upstream key.
- `internal/admin/test_handlers.go:100` calls `adapter.Test(...)` with the selected model and prompt.
- `internal/admin/test_handlers.go:139` returns `latency_ms`, `status_code`, `model`, and `tokens_used`.

Conclusion: channel testing is a real non-stream upstream probe when a channel has a valid active key. High latency is therefore normally dominated by ECS-to-provider network path, provider queue/model response time, DNS/TLS/connect overhead, or the selected upstream endpoint. A successful upstream probe can consume a small number of tokens because it sends an actual prompt. Failures before reaching upstream, such as missing key or local validation errors, should not consume upstream tokens.

### API docs duplicate key warning

- Browser smoke surfaced a duplicate React key warning for `GET /api/v1/license` and `PUT /api/v1/license`.
- `frontend/src/pages/ApiDocsPage.tsx:96` now creates `key: ${method}:${path}` and the table uses `rowKey="key"` at `frontend/src/pages/ApiDocsPage.tsx:103`.
- A targeted Playwright API docs check returned `duplicateKeyWarnings: 0` and `consoleErrorCount: 0`.

## Local Verification

Commands run by Codex:

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- `npm --prefix frontend test -- --run`: PASS, 16 test files / 88 tests.
- `npm --prefix frontend run build`: PASS, existing Vite large chunk warning only.
- `git diff --check`: PASS.

Browser verification with Playwright against local frontend `http://localhost:3000` and mocked API responses:

- Login page includes `UniRouter`, `Enterprise Intelligence Foundation`, Chinese preview labels, and the professional platform description.
- Old login subtitle `OpenAI 中转服务管理平台` is absent.
- Old preview model rows `deepseek-chat`, `gpt-4o-mini`, and `qwen-plus` are absent.
- Sidebar includes the new foundation caption and renamed enterprise menu labels.
- `API 文档` and `退出登录` do not overlap. Observed `navBottom=660.90625` and `footerTop=660.90625`.
- Logs page includes `请求日志` and `请求检查器`.
- Detail panel has `detailBorderLeft=3px solid rgba(53, 104, 232, 0.62)` and a non-white gradient background.
- API docs targeted check reports `duplicateKeyWarnings: 0` and `consoleErrorCount: 0`.
- Full Logs smoke still emits existing Semi UI StrictMode `findDOMNode` warnings; no duplicate-key warning was observed.

Screenshots:

- `output/playwright/prd2-batch27-copy-login.png`
- `output/playwright/prd2-batch27-copy-sidebar.png`
- `output/playwright/prd2-batch27-copy-logs-detail.png`

## Boundary

- No Go backend files were changed.
- No billing, metering, reconciliation, settlement, or `usage_source` logic was changed.
- Channel test latency/token behavior was inspected and documented from current backend code; the implementation remains a real upstream probe.
- This batch is post-closeout UI/copy polish and should be verified independently from the already accepted PRD2 Batch 25 feature closeout and the user-reported completed Batch 26 UI/UX hardening.
