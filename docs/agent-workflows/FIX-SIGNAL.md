# FIX-SIGNAL.md - PRD3 Batch 15 Frontend Paged Table Cleanup

**Date:** 2026-07-06
**Status:** Fixed awaiting Canary

PRD3 Batch 14 admin helper boundary cleanup passed Canary 6/6 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #78 at `73bc34e`;
User reported Canary/Sentinel completed.

Codex has completed PRD3 Batch 15 as a narrow P3-F10 frontend architecture
cleanup. This introduces a shared paged-table wrapper for existing pages and
does not add new user-facing capabilities.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- No new API route, response field, role, permission, menu item, page, workflow,
  backend behavior, database migration, billing behavior, audit behavior,
  notification behavior, gateway behavior, deployment config, or PRD4 feature
  was added.
- Existing page titles, forms, columns, actions, and API client methods remain in
  their current product scope.
- This batch does not claim a full design-system rebuild; it only centralizes the
  repeated table shell, pagination, loading, and empty-state pattern for the
  touched pages.
- No files were deleted.

## Verification Request

Please verify:

1. `frontend/src/ui/data/PagedTable.tsx` owns the shared `ur-table-shell`, empty
   state, loading, pagination, row key, and data-source wiring for Semi `Table`.
2. `frontend/src/ui/index.ts` exports `PagedTable`.
3. These existing pages now use `PagedTable` instead of local Semi `Table` shells:
   - `frontend/src/pages/ModelPricesPage.tsx`
   - `frontend/src/pages/OrgModelPricesPage.tsx`
   - `frontend/src/pages/ChannelKeysPage.tsx`
   - `frontend/src/pages/GrantsPage.tsx`
4. Existing page-size drift is fixed:
   - `OrgModelPricesPage` passes the selected page size to
     `api.getOrgModelPrices(1, page, pageSize)`;
   - `GrantsPage` passes the selected page size to `api.getGrants(page,
     pageSize)`.
5. Regression coverage exists:
   - `frontend/src/ui/data/__tests__/PagedTable.test.tsx` covers pagination
     values, callbacks, empty state, loading, and row key passthrough.
6. Docs/status are self-consistent:
   - `docs/ops/CURRENT-STATUS.md` shows Batch 15 as the current verification
     request and Batch 14 complete per User report;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` marks P3-F10 as `Fixed awaiting
     Canary` and P3-F09 as `Canary passed`;
   - `docs/issues/OPEN-ISSUES.md` records Batch 15 under `PRD3-ARCH-001`;
   - `docs/CODEBASE-MAP.md` documents `frontend/src/ui/` and `PagedTable`.
7. Evidence exists at
   `docs/ops/evidence/PRD3-B15-frontend-paged-table-local-20260706.md`.
8. Run these commands:

```bash
npm --prefix frontend test -- --run PagedTable ModelPricesPage
npm --prefix frontend test -- --run
npm --prefix frontend run build
go test ./...
rg -n "PagedTable|PRD3 Batch 15 Frontend Paged Table|P3-F10 \\| Frontend Architecture Cleanup \\| Fixed awaiting Canary|PRD3-B15-frontend-paged-table|frontend/src/ui/" frontend/src/ui/index.ts frontend/src/ui/data/PagedTable.tsx frontend/src/ui/data/__tests__/PagedTable.test.tsx docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/CODEBASE-MAP.md docs/ops/evidence/PRD3-B15-frontend-paged-table-local-20260706.md docs/agent-workflows/FIX-SIGNAL.md
! rg -n "from '@douyinfe/semi-ui'.*Table|<Table|ur-table-shell" frontend/src/pages/ModelPricesPage.tsx frontend/src/pages/OrgModelPricesPage.tsx frontend/src/pages/ChannelKeysPage.tsx frontend/src/pages/GrantsPage.tsx
git diff --check
```

## Expected Result

- Targeted `PagedTable` / `ModelPricesPage` tests pass.
- Full frontend tests pass.
- Frontend build passes, allowing the existing Vite large-chunk warning.
- Full Go tests pass.
- Status/evidence grep returns Batch 15, P3-F10, `PagedTable`, evidence, and
  codebase-map matches.
- The page grep does not find local Semi `Table` imports, local `<Table`
  elements, or local `ur-table-shell` wrappers in the four touched pages.
- `git diff --check` passes.
- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 15 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `frontend/src/ui/data/PagedTable.tsx`
- `frontend/src/ui/data/__tests__/PagedTable.test.tsx`
- `frontend/src/ui/index.ts`
- `frontend/src/pages/ModelPricesPage.tsx`
- `frontend/src/pages/OrgModelPricesPage.tsx`
- `frontend/src/pages/ChannelKeysPage.tsx`
- `frontend/src/pages/GrantsPage.tsx`
- `docs/CODEBASE-MAP.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B15-frontend-paged-table-local-20260706.md`
- `docs/agent-workflows/FIX-SIGNAL.md`
