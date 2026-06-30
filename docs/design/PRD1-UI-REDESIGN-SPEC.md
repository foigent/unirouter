# PRD1 UI Redesign Execution Spec

Status: reference-inspired pass implemented locally, awaiting verification
Owner: Codex
Last updated: 2026-07-01

## Purpose

This document records the PRD1 closeout UI redesign that turns the existing Semi UI pages into a coherent admin console. It is an execution spec, not a concept deck.

The redesign stays inside the PRD1 scope:

- No new product features.
- No backend contract changes.
- No replacement of Semi UI.
- No marketing landing page. The admin console may use restrained gradients, glass surfaces, and dashboard previews where they directly support product context.

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
- `ur-topbar`, `ur-global-search`, `ur-user-chip`
- `ur-page`, `ur-page-wide`, `ur-page-header`
- `ur-surface`, `ur-table-shell`
- `ur-kpi-grid`, `ur-metric`
- `ur-dashboard-hero`, `ur-hero-insight`
- `ur-explorer`, `ur-explorer-with-detail`
- `ur-channel-card`, `ur-wallet-budget`
- `ur-login-page`, `ur-login-shell`, `ur-login-showcase`, `ur-login-panel`

## Reference-Inspired Visual Pass

User-provided references:

- Dribbble AdFlux Marketing Dashboard Web Design
- Pinterest light dashboard reference image

Codex applied the reusable parts of those references to an enterprise AI gateway console:

- Dark, high-contrast left navigation with clear active state.
- Soft light workspace background with elevated white/glass panels.
- Pill-shaped top search and operational status controls.
- Larger first-viewport Dashboard command panel instead of a plain page header.
- Login page with an actual dashboard preview rather than an empty brand card.
- More deliberate KPI cards, empty states, table shells, and channel cards.

The reference pass intentionally avoids changing PRD1 behavior or creating a marketing site. It is a visual system pass over the existing admin console.

## Page Coverage

| Area | Pages | PRD1 redesign result |
|------|-------|----------------------|
| Shell | `App.tsx` | Unified dark sidebar, brand block, topbar search/actions, content workspace, `/docs` nav entry |
| Login | `LoginPage.tsx` | Product console login shell with dashboard preview, no marketing landing page |
| Overview | `DashboardPage.tsx` | Command-center hero, KPI tiles, chart surface, health panel, table surface |
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

- Login: `output/playwright/ui-redesign-login.png`
- Dashboard: `output/playwright/ui-redesign-dashboard.png`
- Mobile Dashboard: `output/playwright/ui-redesign-mobile.png`
- Existing broader evidence remains under `output/playwright/` for Logs, Wallet, API Docs, Channels, and previous mobile checks.

Canary/OpenClaw should verify source-level and test-level evidence only:

- UI primitives exist under `frontend/src/ui/`
- All main pages import and use `PageHeader` and/or `Surface`
- `frontend/src/App.tsx` has a `/docs` nav item
- `DashboardPage.tsx` uses a correct 24h range calculation
- `App.tsx` contains the topbar/search/user chip shell
- `LoginPage.tsx` contains the reference-inspired showcase
- `DashboardPage.tsx` contains the command-center hero and insight cards
- `npm run build` passes
- `npm test -- --run` passes
- Browser visual validation is `NOT_APPLICABLE_TO_CANARY`
