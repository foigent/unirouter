# PRD3 Batch 16 Navigation Parity Guard Local Evidence

Date: 2026-07-06

## Scope

PRD3 Batch 16 is a P3-F11 targeted parity-guard batch. It adds regression
coverage for existing frontend navigation and route-guard semantics without
changing product behavior.

Inputs:

- User reported PRD3 Batch 15 Canary/Sentinel/Atlas completed.
- `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 15 PASS 7/7.
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` keeps P3-F11 open for focused
  route/nav/RBAC/default-landing parity guards before PRD3 final closeout.

## Code Changes

Test coverage added:

- `frontend/src/__tests__/navigation.test.tsx`
  - Adds a guard for current roles:
    - `admin`
    - `platform_admin`
    - `org_admin`
    - `billing_admin`
    - `project_admin`
    - `developer`
    - `viewer`
    - `org_viewer`
    - unknown role `owner`
  - For each role, flattens the visible navigation leaf routes from
    `getNavItemsForRole(role)`.
  - Asserts each visible route is allowed by `isRouteAllowedForRole(route,
    role)`.
  - Asserts `getDefaultPathForRole(role)` is allowed by the same guard.
  - Asserts the default path is present in that role's visible navigation
    routes.

## Boundaries

- No route, menu item, role, permission, route guard, default landing,
  navigation label, page, API client, backend behavior, database migration,
  billing behavior, audit behavior, notification behavior, gateway behavior, or
  deployment config changed.
- This batch does not implement new RBAC authority surfaces or persistent roles.
- This batch does not promote any PRD2 external target evidence to Production
  verified.

## Local Validation

Commands run by Codex:

```bash
npm --prefix frontend test -- --run navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
go test ./...
rg -n "keeps visible navigation routes allowed by the route guard|PRD3 Batch 16 Navigation Parity Guard|P3-F11 \\| Test And Parity Guards \\| Fixed awaiting Canary|PRD3-PARITY-001|PRD3-B16-navigation-parity-guard" frontend/src/__tests__/navigation.test.tsx docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/evidence/PRD3-B16-navigation-parity-guard-local-20260706.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

Observed local result:

- Navigation targeted tests pass: 1 file, 9 tests.
- Full frontend tests pass: 21 files, 105 tests.
- Frontend build passes. Vite still reports the pre-existing large chunk
  warning.
- Full Go tests pass.
- Status/evidence grep returns Batch 16, P3-F11, `PRD3-PARITY-001`, evidence,
  and navigation guard matches.
- `git diff --check` passes.
