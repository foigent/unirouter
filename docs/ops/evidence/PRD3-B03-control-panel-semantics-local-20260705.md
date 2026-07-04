# PRD3 Batch 3 Control Panel Semantics Local Evidence

> Date: 2026-07-05
> Scope: P3-F03 Control Panel Semantics
> Status: Codex local validation passed; awaiting Canary.

## Summary

PRD3 Batch 3 closes the remaining control-panel semantics issues identified by the PRD3 consistency audit, without adding new backend capability.

Implemented changes:

- Topbar global search is no longer inert:
  - pressing Enter routes to the existing request-log search route `/logs?search=...`;
  - empty search routes to `/logs`;
  - placeholder now says it searches request logs by Request ID, model, provider, or error.
- Topbar environment label is no longer hard-coded as production:
  - it reads explicit build-time `VITE_APP_ENV` or `VITE_DEPLOY_ENV`;
  - if neither is set, it shows `环境未配置`;
  - supported labels include production, staging, canary, development, and local.
- Batch 1 already fixed role-inappropriate notification access; Batch 2 already clarified Usage Analytics and Internal Status placeholder semantics.
- A page-level scan of active `PageHeader`, `Surface`, `EmptyState`, placeholder, and disabled-control text found no additional PRD3-F03 blocking affordance after the topbar fix. Stale API documentation remains tracked separately as P3-F04.

## Files Changed

- `frontend/src/App.tsx`
- `frontend/src/__tests__/App.test.tsx`

## Verification

```bash
npm --prefix frontend test -- --run App
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- Targeted App shell tests: PASS, 1 file / 2 tests.
- Full frontend tests: PASS, 17 files / 94 tests.
- Frontend build: PASS, with the existing Vite large chunk warning only.
- `git diff --check`: PASS.

## Boundaries

- No new global search backend, index, or cross-resource search product was implemented.
- No deployment runtime detection API was added; the environment label only uses explicit frontend build-time variables.
- No API contract refresh was done in this batch; P3-F04 remains separate.
- No role/navigation changes were made beyond using Batch 1's existing notification access guard.
