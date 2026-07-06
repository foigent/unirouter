# FIX-SIGNAL.md - PRD3 Batch 16 Navigation Parity Guard

**Date:** 2026-07-06
**Status:** Fixed awaiting Canary

PRD3 Batch 15 frontend paged table cleanup passed Canary 7/7 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #79 at `b9ac4e6`;
User reported Canary/Sentinel/Atlas completed.

Codex has completed PRD3 Batch 16 as a narrow P3-F11 targeted parity-guard
batch. It adds a regression test for current frontend navigation/default-route
semantics without changing role, menu, route, RBAC, or page behavior.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- No new route, menu item, role, permission, route guard, default landing,
  navigation label, page, API client, backend behavior, database migration,
  billing behavior, audit behavior, notification behavior, gateway behavior,
  deployment config, or PRD4 feature was added.
- This batch does not implement new RBAC authority surfaces or persistent roles.
- This batch does not promote any PRD2 external target evidence to Production
  verified.
- No files were deleted.

## Verification Request

Please verify:

1. `frontend/src/__tests__/navigation.test.tsx` includes a new guard named
   `keeps visible navigation routes allowed by the route guard for every current
   role`.
2. The guard covers current role cases:
   - `admin`
   - `platform_admin`
   - `org_admin`
   - `billing_admin`
   - `project_admin`
   - `developer`
   - `viewer`
   - `org_viewer`
   - unknown role `owner`
3. For each role, the guard:
   - flattens `getNavItemsForRole(role)` to visible leaf routes;
   - asserts every visible route is allowed by `isRouteAllowedForRole(route,
     role)`;
   - asserts `getDefaultPathForRole(role)` is allowed;
   - asserts the default path appears in the visible route list.
4. Docs/status are self-consistent:
   - `docs/ops/CURRENT-STATUS.md` shows Batch 16 as the current verification
     request and Batch 15 complete per User report;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` marks P3-F11 as `Fixed awaiting
     Canary` and P3-F10 as `Canary passed`;
   - `docs/issues/OPEN-ISSUES.md` records Batch 16 under `PRD3-PARITY-001`;
   - evidence exists at
     `docs/ops/evidence/PRD3-B16-navigation-parity-guard-local-20260706.md`.
5. Run these commands:

```bash
npm --prefix frontend test -- --run navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
go test ./...
rg -n "keeps visible navigation routes allowed by the route guard|PRD3 Batch 16 Navigation Parity Guard|P3-F11 \\| Test And Parity Guards \\| Fixed awaiting Canary|PRD3-PARITY-001|PRD3-B16-navigation-parity-guard" frontend/src/__tests__/navigation.test.tsx docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/evidence/PRD3-B16-navigation-parity-guard-local-20260706.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

## Expected Result

- Targeted navigation tests pass.
- Full frontend tests pass.
- Frontend build passes, allowing the existing Vite large-chunk warning.
- Full Go tests pass.
- Status/evidence grep returns Batch 16, P3-F11, `PRD3-PARITY-001`, and the
  navigation guard matches.
- `git diff --check` passes.
- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 16 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `frontend/src/__tests__/navigation.test.tsx`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B16-navigation-parity-guard-local-20260706.md`
- `docs/agent-workflows/FIX-SIGNAL.md`
