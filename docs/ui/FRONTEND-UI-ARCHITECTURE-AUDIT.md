# Frontend UI and Architecture Audit

Status: PRD1 closeout guidance
Owner: Codex
Last updated: 2026-07-01

## Executive Summary

The current frontend is a usable React/Semi UI admin console, but it is still a page-level prototype rather than a durable product console architecture. The main PRD1 closeout risk is not visual polish alone. The larger risk is that page code, API contracts, and UI patterns are not centralized, so new PRD2 features will amplify duplication and contract drift.

This audit is based on source-code review, local route/API comparison, and existing test coverage. It does not claim browser visual verification. Canary/OpenClaw does not provide visual acceptance in the current workflow, so visual/manual UI acceptance must be handled by a separate capable step.

## Confirmed Strengths

- React, Vite, TypeScript, Semi UI, Recharts, and Zustand are already in place.
- Navigation grouping in `frontend/src/App.tsx` broadly follows `docs/design/spec-v1.md`.
- Dashboard and Logs have stronger product intent than generic CRUD pages.
- Existing frontend test/build scripts provide a baseline for regression checks.
- `docs/design/spec-v1.md`, `docs/design/pages/dashboard-v2.md`, and `docs/design/pages/logs-explorer-v2.md` provide useful design direction.

## Confirmed Gaps

### API Contract

- `frontend/src/api/client.ts` previously treated non-2xx JSON responses as successful data, except for 401 redirect handling.
- `frontend/src/pages/KeysPage.tsx` called `/dashboard/models`, but the backend exposes `/api/v1/model-names`.
- `frontend/src/pages/WalletPage.tsx` called `/wallet/recharge`, but the backend exposes `/api/v1/wallet/topup` and `/api/v1/wallet/adjust`.
- Several pages still use `any`, so backend drift is hard to detect at compile time.
- The OpenAPI document exists at `docs/tech-ref/05-openapi-spec.yaml`, but the frontend does not yet consume it as a typed client source.

### UI Architecture

- CRUD pages repeat the same state and lifecycle logic: list loading, pagination, drawer form, editing record, submit, delete/disable, and toast handling.
- There is no shared resource-page template such as `ResourcePage`, `ResourceTable`, `ResourceDrawerForm`, or `ConfirmAction`.
- There are no shared formatting primitives for money, status, dates, model/provider labels, or latency.
- Page-level inline styles are common, so the visual system is not encoded as reusable components or tokens.
- `DashboardPage.tsx` and `LogsPage.tsx` are large feature modules living as single page files.

### UX and Visual Consistency

- Loading, empty, error, and permission states are inconsistent across pages.
- Tables lack a consistent density, action-column pattern, width strategy, and filter placement.
- Drawer forms are common but not standardized.
- Admin money operations use a modal, while other management workflows use SideSheet/Drawer.
- Semi Button props were used incorrectly in Dashboard segmented controls, causing warning noise.

### Testing

- Current frontend tests mainly cover login, dashboard, logs, formatting, and the API client.
- Resource CRUD pages have little coverage.
- There is no browser-level PRD acceptance suite yet.
- There is no visual regression or accessibility smoke test.

## Fixes Applied In This Round

- Added `ApiError` and robust response parsing in `frontend/src/api/client.ts`.
- Non-2xx responses now throw an error with status and backend message.
- 401 responses still clear token and redirect to `/login`.
- `KeysPage` now loads available models from `api.getModelNames()` instead of missing `/dashboard/models`.
- `WalletPage` now uses `api.getWallet()` and `api.topUp()` instead of raw `/wallet` and missing `/wallet/recharge`.
- `DashboardPage` now uses correct Semi Button `theme` and `type` props.
- Removed the misleading total-request delta formula from Dashboard because the backend does not return a request-count delta.
- Updated API client tests to cover successful calls, non-2xx errors, and 401 redirect behavior.

## PRD1 UI Redesign Applied After This Audit

The original audit identified the missing UI architecture layer. Codex subsequently implemented the PRD1 closeout redesign recorded in `docs/design/PRD1-UI-REDESIGN-SPEC.md`.

Implemented locally:

- Added `frontend/src/ui/` with `PageHeader`, `Surface`, `MetricTile`, `MoneyText`, `LatencyText`, `StatusTag`, and `EmptyState`.
- Added global console design tokens and layout classes in `frontend/src/index.css`.
- Refreshed the app shell, login page, Dashboard, Logs Explorer, Wallet, API Docs, and major resource pages.
- Fixed Dashboard 24h range calculation from 24 days to 24 hours.
- Disabled Recharts animation for stable browser screenshots.
- Added local Playwright screenshot evidence under ignored `output/playwright/`.

Residual PRD2 architecture work remains:

- Split large page modules into feature folders.
- Introduce typed OpenAPI-backed API contracts.
- Convert resource pages to a declarative `ResourcePage`/`ResourceTable`/`ResourceDrawerForm` abstraction.

## Recommended PRD1 Closeout Scope

PRD1 should not attempt a full frontend rewrite. It should close the highest-risk contract and UX defects while preparing the codebase for PRD2.

Required before PRD1 closeout:

1. Verify all page API calls against `cmd/uniroute/main.go` and `docs/tech-ref/05-openapi-spec.yaml`.
2. Add typed request/response interfaces for the highest-risk flows: channels, keys, channel keys, wallet, logs, dashboard, model prices, org model prices.
3. Introduce shared UI primitives:
   - `PageHeader`
   - `SectionCard`
   - `ResourceActions`
   - `ConfirmAction`
   - `MoneyText`
   - `DateText`
   - `StatusTag`
   - `LatencyText`
4. Convert two core resource pages first: Channels and API Keys.
5. Split Dashboard and Logs into feature components after the contract fixes are stable.
6. Add tests for the converted resource-page template.

## PRD2 Target Architecture

Recommended target structure:

```text
frontend/src/
  api/
    client.ts
    generated/
    types.ts
  app/
    routes.tsx
    shell/
  features/
    dashboard/
    logs/
    channels/
    keys/
    billing/
    pricing/
    users/
    settings/
  ui/
    actions/
    data-display/
    feedback/
    forms/
    layout/
    formatting/
  utils/
```

Recommended server-state direction:

- PRD1: keep the existing fetch layer but enforce typed API functions and `ApiError`.
- PRD2: adopt a dedicated server-state layer such as TanStack Query, or an equivalent internal query abstraction if dependency additions are intentionally minimized.

Recommended contract direction:

- Treat `docs/tech-ref/05-openapi-spec.yaml` as the source of truth.
- Generate or validate frontend API types from OpenAPI in CI.
- Add route parity checks for critical admin endpoints.

Recommended UI direction:

- Semi UI remains the base component library.
- UniRouter should add a thin domain design layer above Semi UI, not replace Semi UI.
- Resource pages should be declarative configurations over shared templates.
- Dashboard and Logs should remain bespoke feature pages, but split into components and hooks.

## Canary Verification Request

Canary/OpenClaw should verify without modifying code:

1. Frontend tests pass.
2. Frontend build passes.
3. API client test confirms non-2xx responses throw errors.
4. API key page no longer calls `/dashboard/models`.
5. Wallet recharge now calls `/wallet/topup`.
6. Dashboard no longer emits Semi Button theme warnings from `primary/tertiary` misuse.
7. If backend/runtime logs are available, request paths should show `/api/v1/model-names` and `/api/v1/wallet/topup`.
8. Canary should explicitly mark visual/manual UI acceptance as not applicable to its capability set.

## Residual Risks

- Browser rendering has not been verified in this round and is not covered by Canary/OpenClaw.
- Some page payloads remain weakly typed.
- The frontend still lacks reusable resource-page architecture.
- Org model pricing payload semantics should be verified against backend handlers and PRD before visual redesign.
- Settings page may expose password-change UI while backend route parity remains unclear.
