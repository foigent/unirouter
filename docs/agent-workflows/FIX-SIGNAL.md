# FIX-SIGNAL.md - PRD4 Batch 1A Auth UX Hardening

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary

## Canary Follow-Up

Canary's first Batch 1A verification failed only because
`LoginPage > toggles password visibility without changing the submitted value`
timed out at the default 5000ms. The component/page behavior was accepted by
source inspection. Codex simplified that test to use synchronous
`fireEvent.change` / `fireEvent.click` instead of slow character-by-character
`userEvent.type` interactions. No production code changed in this follow-up.

Local follow-up verification now passes:

- `npm --prefix frontend run test -- LoginPage --testNamePattern "toggles password visibility"`
- `npm --prefix frontend run test -- PasswordField LoginPage`
- `npm --prefix frontend test`
- `npm --prefix frontend run build`
- `git diff --check`

## Context

User approved continuing into PRD4 after Batch 0 scope baseline and Preflight
Hardening completed. This batch implements the narrow P4-F10 Auth UX hardening
slice only:

1. login password input has an explicit hidden-by-default show/hide control;
2. current/new/confirm password inputs in Settings share the same control;
3. admin user create/edit password input shares the same control and resets to
   hidden when the drawer/user context changes.

This batch does not change authentication semantics, password policy, session
handling, JWT/API key behavior, OIDC/SSO, RBAC, backend APIs, migrations,
pricing/usage, trace/event data, runtime observability, deployment config, or
any PRD5 deferred capability.

Status reconciliation included:

- PRD4 Batch 0 scope baseline is recorded as complete per User report after
  `e24731d` / PR #82.
- PRD4 Preflight Hardening is recorded as Canary PASS 6/6 in
  `docs/agent-workflows/VERIFY-RESULT.md`; User reported Sentinel/Atlas
  completed after `e335a9e` / PR #83.
- Current active verification request is now this Batch 1A Auth UX hardening.

## Files Changed

- `frontend/src/ui/form/PasswordField.tsx`
- `frontend/src/ui/form/__tests__/PasswordField.test.tsx`
- `frontend/src/ui/index.ts`
- `frontend/src/pages/LoginPage.tsx`
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/pages/UsersPage.tsx`
- `frontend/src/pages/__tests__/LoginPage.test.tsx`
- `docs/agent-workflows/FIX-SIGNAL.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`

## Required Verification

1. Confirm the shared component and target pages are wired:

   ```bash
   rg -n "PasswordField|显示密码|隐藏密码|visibilityResetKey" frontend/src/ui/form frontend/src/pages/LoginPage.tsx frontend/src/pages/SettingsPage.tsx frontend/src/pages/UsersPage.tsx frontend/src/pages/__tests__/LoginPage.test.tsx
   ```

   Expected:

   - `PasswordField` renders `显示密码` / `隐藏密码` accessible controls.
   - `LoginPage`, `SettingsPage`, and `UsersPage` use `PasswordField`.
   - `UsersPage` passes `visibilityResetKey` so the drawer field resets to
     hidden across drawer/user context changes.

2. Run focused frontend tests:

   ```bash
   npm --prefix frontend run test -- PasswordField LoginPage
   ```

   Expected: all PasswordField and LoginPage tests pass. The tests must verify
   default hidden state, show/hide toggling, reset-on-key-change behavior, and
   that login submission keeps the original password value.

3. Run broad frontend regression:

   ```bash
   npm --prefix frontend test
   ```

   Expected: all frontend tests pass. Existing React Router future-flag,
   Semi `findDOMNode`, and intentional mocked-error console warnings are
   non-blocking unless a test fails.

4. Run production frontend build:

   ```bash
   npm --prefix frontend run build
   ```

   Expected: TypeScript and Vite build pass. Existing Vite large chunk warning
   is non-blocking unless a new hard error appears.

5. Confirm PRD4 status/docs are aligned:

   ```bash
   rg -n "PRD4-AUTHUX-001|P4-F10|Batch 1A|Auth UX" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md
   ```

   Expected:

   - `PRD4-AUTHUX-001` is `Fixed awaiting Canary`.
   - P4-F10 is `Fixed awaiting Canary`.
   - first-read docs point to Batch 1A as the current verification signal.
   - prior Batch 0 and Preflight statuses are not left as awaiting Canary.

6. Run diff hygiene:

   ```bash
   git diff --check
   git diff --name-only
   ```

## Expected Result

Canary should mark this batch PASS only if:

- password inputs remain hidden by default;
- visible/hidden state changes only after explicit user action;
- icon buttons are accessible by role/name as `显示密码` and `隐藏密码`;
- login, change-password, and admin user password fields use the shared control;
- admin user password visibility resets when drawer/user context changes;
- login submission sends the typed value unchanged after toggling visibility;
- frontend focused tests, broad tests, and build pass;
- docs/status accurately mark Batch 0 and Preflight as completed and Batch 1A as
  fixed awaiting Canary;
- no backend API, migration, auth policy, SSO/OIDC, RBAC, pricing/usage,
  trace/event, runtime observability, deployment, PRD2 freeze, or PRD3
  completion semantics changed.
