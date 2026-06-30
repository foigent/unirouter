# PRD1 UI Findings — 2026-07-01

Purpose: classify the user-reported UI/product issues against PRD1 and record what Codex fixed in this round.

## Classification

| # | Finding | PRD1 Status | Classification | Codex Action |
|---|---------|-------------|----------------|--------------|
| 1 | 调用日志筛选器的模型下拉没有内容 | PRD1 planned: F06 call logs + F10 admin frontend | Bug | Fixed: model options now load from `/api/v1/model-names`; provider options load from channels instead of Dashboard period data |
| 2 | Dashboard 24h/7天/30天切换后 AI 活动趋势图不变 | PRD1 planned: F07 Dashboard + F10 admin frontend | Bug / acceptance gap | Fixed: backend trend buckets are hourly for short ranges and daily for longer ranges; frontend labels support hourly buckets |
| 3 | Dashboard 渠道健康状态互相遮挡 | PRD1 planned: F10 admin frontend usability | UI bug | Fixed: channel cards now use tighter grid layout, truncation, and warning status coloring |
| 4 | 钱包管理没有任何可编辑入口 | PRD1 planned: F08 wallet management + F10 admin frontend | Backend/frontend contract bug | Fixed: backend returns `is_admin` and frontend field aliases; frontend adjustment now sends `target_balance` matching backend |
| 5 | 审计日志没有登录审计 | PRD1 planned: audit/security requirements | Backend bug + frontend mapping bug | Fixed: login audit writes synchronously with actor context; audit page maps `user_id/resource/resource_id` correctly |
| 6 | 系统设置文档链接 `/docs` 为空 | PRD1 planned: admin/API documentation surface | Deployment/frontend route bug + incomplete backend placeholder | Fixed: frontend `/docs` route added; backend direct `/docs` page expanded from placeholder to API index |
| 7 | 整体 UI 很丑、没有品味 | PRD1 includes usable admin frontend; high-end visual system is not fully specified | UI quality debt / PRD1 acceptance risk | Fixed locally for PRD1 closeout: added `frontend/src/ui/` design layer, global tokens, refreshed shell, Dashboard, Logs, Wallet, Docs, resource pages, settings/test/reconciliation, mobile spacing, and a reference-inspired shell/login/Dashboard visual pass |

## Screenshot Status

Codex captured local Playwright screenshots with read-only mocked API data because Canary/OpenClaw cannot perform visual acceptance and Codex did not mutate cloud data.

Current local visual evidence:

- `output/playwright/login.png`
- `output/playwright/dashboard-final.png`
- `output/playwright/logs-detail-final-v4.png`
- `output/playwright/wallet-final.png`
- `output/playwright/api-docs.png`
- `output/playwright/channels.png`
- `output/playwright/dashboard-mobile-final.png`
- `output/playwright/logs-mobile-final.png`
- `output/playwright/ui-redesign-login.png`
- `output/playwright/ui-redesign-dashboard.png`
- `output/playwright/ui-redesign-mobile.png`

The screenshots are ignored by Git and retained as local QA evidence.

Unauthenticated `/docs` behavior was verified with curl:

- `http://8.130.18.85:3000/docs` returned the frontend SPA shell instead of API docs.
- `http://8.130.18.85:8080/docs` returned a backend placeholder page.

## Verification Commands

```bash
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
```
