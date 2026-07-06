# PRD3 Batch 15 Frontend Paged Table Local Evidence

Date: 2026-07-06

## Scope

PRD3 Batch 15 is a P3-F10 frontend architecture cleanup batch. It reduces
duplicate table shell, pagination, loading, and empty-state wiring for existing
admin pages without adding product capability or changing user workflows.

Inputs:

- User reported PRD3 Batch 14 Canary/Sentinel completed.
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` tracks P3-F10 as frontend architecture
  cleanup for already implemented functionality.
- Several pages repeated the same Semi `Table` + `ur-table-shell` +
  pagination/empty-state pattern.

## Code Changes

Shared component introduced:

- `frontend/src/ui/data/PagedTable.tsx`
  - owns the standard `ur-table-shell` wrapper;
  - wires shared empty state through `EmptyState`;
  - passes pagination, page-size, loading, row key, and table data through to
    Semi `Table`;
  - constrains table records to object rows for TypeScript compatibility.

Export updated:

- `frontend/src/ui/index.ts` exports `PagedTable`.

Existing pages moved to the shared component:

- `frontend/src/pages/ModelPricesPage.tsx`
- `frontend/src/pages/OrgModelPricesPage.tsx`
- `frontend/src/pages/ChannelKeysPage.tsx`
- `frontend/src/pages/GrantsPage.tsx`

Existing pagination drift fixed while migrating:

- `OrgModelPricesPage` now passes the current page size to
  `api.getOrgModelPrices(1, page, pageSize)` instead of hard-coding `20`.
- `GrantsPage` now passes the current page size to `api.getGrants(page,
  pageSize)` instead of hard-coding `20`.

Test coverage:

- Added `frontend/src/ui/data/__tests__/PagedTable.test.tsx` to assert the
  shared table receives loading, row key, pagination values, empty state, and
  pagination callbacks.

Docs:

- `docs/CODEBASE-MAP.md` now records `frontend/src/ui/` as the shared UI
  primitive location, including `PagedTable`.

## Boundaries

- No new route, API endpoint, response field, role, permission, page, menu item,
  workflow, or product capability was added.
- No backend code, database schema, migrations, billing behavior, audit behavior,
  notification behavior, gateway behavior, deployment config, or PRD4 feature
  changed.
- Page titles, copy, forms, actions, columns, and existing API client methods
  remain in their current product scope.
- This batch does not claim a full design-system rebuild; it is a narrow shared
  table/pagination/empty-state cleanup.

## Local Validation

Commands run by Codex:

```bash
npm --prefix frontend run build
npm --prefix frontend test -- --run PagedTable ModelPricesPage
npm --prefix frontend test -- --run
go test ./...
git diff --check
```

Observed local result:

- Frontend build passes. Vite still reports the pre-existing large chunk
  warning.
- Targeted frontend tests pass: `PagedTable` and `ModelPricesPage` 2/2 tests.
- Full frontend tests pass: 21 files, 104 tests.
- Full Go tests pass.
- `git diff --check` passes.
