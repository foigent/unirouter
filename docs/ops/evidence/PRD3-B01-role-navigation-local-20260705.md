# PRD3 Batch 1 Role Navigation Local Evidence

> Date: 2026-07-05
> Scope: P3-F02 Role And Navigation Parity, P3-F07 User Management Role Workflow
> Status: Codex local validation passed; awaiting Canary.

## Summary

PRD3 Batch 1 aligns current frontend role/navigation behavior with existing backend authorization and PRD3 scope decisions.

Implemented changes:

- `org_admin` no longer inherits platform-only or billing-only navigation entries:
  - removed `channels`, `channel-keys`, `test`, `wallet`, `reconciliation`, `grants`, and `license` from org-admin navigation;
  - route guard now blocks those routes for `org_admin`.
- `org_viewer` and unknown roles no longer default to API Docs:
  - new `/access` access-state page is the default landing for unsupported/no-workbench roles;
  - API Docs remains available as a secondary reference, not the default workbench.
- Project-scoped roles still land in Developer Console.
- Topbar notification button is only shown when `/notifications` is allowed for the current role.
- User Management now exposes canonical non-legacy roles supported by the current backend role model:
  - `platform_admin`
  - `org_admin`
  - `billing_admin`
  - `project_admin`
  - `developer`
  - `viewer`
  - `org_viewer`
- Legacy `admin` remains display-compatible but is not offered as a new create option.

## Files Changed

- `frontend/src/navigation.tsx`
- `frontend/src/main.tsx`
- `frontend/src/App.tsx`
- `frontend/src/pages/AccessStatePage.tsx`
- `frontend/src/pages/UsersPage.tsx`
- `frontend/src/__tests__/navigation.test.tsx`
- `frontend/src/pages/__tests__/LoginPage.test.tsx`

## Verification

```bash
npm --prefix frontend test -- --run navigation LoginPage
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- Targeted navigation/Login tests: PASS, 2 files / 17 tests.
- Full frontend tests: PASS, 16 files / 92 tests.
- Frontend build: PASS, with the existing Vite large chunk warning only.
- `git diff --check`: PASS.

## Boundaries

- No backend authorization, database, billing, gateway, or route wiring was changed.
- This batch does not implement new persistent roles.
- This batch does not complete all P3-F03 control-panel semantics work; remaining inert controls such as global search and environment label stay open for later PRD3 batches.
