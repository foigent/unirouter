# PRD2 Batch 26 UI/UX Hardening Local Evidence

**Date:** 2026-07-04
**Owner:** Codex main agent
**Scope:** Post-PRD2-closeout UI/UX hardening requested by User after Batch 25 Canary/Sentinel completion.

## Scope Covered

- Sidebar primary/secondary menu distinction and visible submenu collapse indicators.
- Sidebar vertical scrolling so lower entries such as API docs remain reachable.
- Navigation icon differentiation for previously duplicated items.
- Audit logs table horizontal scrolling for wide 50/100-row review workflows.
- Channel test model selection based on each channel's configured model list instead of hardcoded `gpt-3.5-turbo`.
- Visible Chinese labels for PRD2 surfaces that still used English UI terms such as Model Registry, Aliases, Provider, and Catalog.

## Local Verification

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Result:

- `npm --prefix frontend test -- --run`: PASS, 16 files / 88 tests.
- `npm --prefix frontend run build`: PASS; existing Vite large chunk warning only.
- `git diff --check`: PASS.

## Browser Smoke

Browser smoke used Playwright Chromium against the local Vite frontend at `http://localhost:3000`, with API responses mocked in-browser for deterministic UI validation.

Artifacts:

- `/Users/m4/Desktop/projects/UniRouter/output/playwright/prd2-batch26-ux-audit.png`
- `/Users/m4/Desktop/projects/UniRouter/output/playwright/prd2-batch26-ux-channel-test.png`

Observed assertions:

- 1280x560 viewport sidebar: `siderChildrenClientHeight=560`, `navClientHeight=485`, `navScrollHeight=1014`, `navOverflowY=auto`.
- Sidebar text includes `API 文档`.
- Visible submenu toggle count: 25.
- Visible secondary menu item count: 20.
- Audit table horizontal scroll container detected: `.semi-table-body`.
- Channel test POST payload: `{"model":"gpt-4o-mini","prompt":"Hello, this is a test message."}`.
- Channel test page rendered configured model `gpt-4o-mini`, showed `测试成功`, and did not render `gpt-3.5-turbo`.
- Browser console errors: none.

## Boundaries

- No backend billing, reconciliation, metering, or database code was changed in this round.
- Existing P2-F06 compatibility remains as previously implemented: canonical `usage_source` normalization, legacy alias handling, and reconciliation/logs/analytics filtering are preserved.
- This is fixed locally and awaiting Canary; it is not Sentinel/Atlas complete until the normal workflow runs.
