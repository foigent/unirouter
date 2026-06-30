# PRD1 UI Redesign Execution Spec

Status: implemented locally, awaiting verification
Owner: Codex
Last updated: 2026-07-01

## Purpose

This document records the PRD1 closeout UI redesign that turns the existing Semi UI pages into a coherent admin console. It is an execution spec, not a concept deck.

The redesign stays inside the PRD1 scope:

- No new product features.
- No backend contract changes.
- No replacement of Semi UI.
- No decorative hero, gradients, blobs, or marketing layout.

## Confirmed Root Cause

The original SPE documents were directionally sound, but the frontend did not encode them as reusable architecture.

Confirmed pre-redesign gaps:

- Page-level inline styling was the default implementation pattern.
- There was no domain UI layer above Semi UI.
- Dashboard, Logs, Wallet, API Docs, and resource pages used different page shells.
- Status, money, latency, empty states, and table surfaces were repeated ad hoc.
- Canary/OpenClaw cannot perform visual/browser validation, so PRD1 needs a separate Codex browser screenshot pass.

## Implemented PRD1 UI Foundation

New frontend UI layer:

```text
frontend/src/ui/
  data/
    LatencyText.tsx
    MoneyText.tsx
    StatusTag.tsx
  feedback/
    EmptyState.tsx
  layout/
    MetricTile.tsx
    PageHeader.tsx
    Surface.tsx
  index.ts
```

New global console tokens and layout classes live in:

```text
frontend/src/index.css
```

Key classes:

- `ur-app-shell`, `ur-sider`, `ur-content`
- `ur-page`, `ur-page-wide`, `ur-page-header`
- `ur-surface`, `ur-table-shell`
- `ur-kpi-grid`, `ur-metric`
- `ur-explorer`, `ur-explorer-with-detail`
- `ur-channel-card`, `ur-wallet-budget`
- `ur-login-page`, `ur-login-panel`

## Page Coverage

| Area | Pages | PRD1 redesign result |
|------|-------|----------------------|
| Shell | `App.tsx` | Unified sidebar, brand block, content surface, `/docs` nav entry |
| Login | `LoginPage.tsx` | Product console login shell, no marketing layout |
| Overview | `DashboardPage.tsx` | Analysis-view template, KPI tiles, chart surface, health panel, table surface |
| Traffic | `LogsPage.tsx` | Explorer template with filter/result/detail panels |
| Traffic | `ChannelsPage.tsx`, `KeysPage.tsx`, `ChannelKeysPage.tsx`, `TestPage.tsx` | Shared page header and surface/table treatment |
| Billing | `WalletPage.tsx`, `ModelPricesPage.tsx`, `OrgModelPricesPage.tsx`, `GrantsPage.tsx`, `ReconciliationPage.tsx` | Shared billing visual system, money formatting, action panels |
| Access | `UsersPage.tsx`, `AuditLogsPage.tsx` | Shared access page shells and table treatment |
| System | `SettingsPage.tsx`, `ApiDocsPage.tsx` | Unified settings/docs entrypoints |

## Behavior Fix Included

Dashboard `24h` range previously calculated a 24-day window:

```ts
24 * 86400000
```

It now uses the correct 24-hour window:

```ts
86400000
```

This is included because it directly affected the visual trustworthiness of the time-range controls.

## PRD2 Frontend Architecture Direction

PRD1 now has a minimum viable UI foundation. PRD2 should not keep expanding page files directly.

Recommended PRD2 next steps:

1. Move large pages into feature folders:
   - `features/dashboard`
   - `features/logs`
   - `features/billing`
   - `features/traffic`
2. Introduce a typed API layer generated or validated from OpenAPI.
3. Add reusable resource-page primitives:
   - `ResourcePage`
   - `ResourceTable`
   - `ResourceDrawerForm`
   - `ConfirmAction`
4. Add browser-level smoke coverage for core workflows.
5. Add visual regression screenshots for Dashboard, Logs, Wallet, and resource pages.

## Verification Requirements

Codex should verify:

```bash
cd frontend && npm run build
cd frontend && npm test -- --run
git diff --check
```

Codex should also run a browser screenshot pass because Canary cannot visually inspect UI:

- Login
- Dashboard
- Logs
- Wallet
- API Docs
- At least one resource page, such as Channels

Canary/OpenClaw should verify source-level and test-level evidence only:

- UI primitives exist under `frontend/src/ui/`
- All main pages import and use `PageHeader` and/or `Surface`
- `frontend/src/App.tsx` has a `/docs` nav item
- `DashboardPage.tsx` uses a correct 24h range calculation
- `npm run build` passes
- `npm test -- --run` passes
- Browser visual validation is `NOT_APPLICABLE_TO_CANARY`
