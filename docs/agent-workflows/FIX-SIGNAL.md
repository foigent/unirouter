# FIX-SIGNAL.md — PRD3 Batch 3 Verification Request

**日期：** 2026-07-05
**状态：** Fixed awaiting Canary

PRD3 Batch 2 passed Canary 5/5 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #64 at `a1baf8f`; User reported Canary, Sentinel, and Atlas completed.

Codex has completed PRD3 Batch 3 for control-panel semantics.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- This batch does not implement a new global search backend, search index, or cross-resource search product.
- This batch does not add a runtime deployment/environment API; the topbar environment label only reflects explicit frontend build-time variables.
- This batch does not refresh backend `/docs`, frontend `/docs`, or OpenAPI; P3-F04 API Contract Refresh remains separate.

## Verification Request

Please verify:

1. `frontend/src/App.tsx` topbar search:
   - no longer uses an inert placeholder;
   - routes non-empty search terms to `/logs?search=<encoded term>`;
   - routes empty search submission to `/logs`;
   - placeholder clearly says it searches request logs, not arbitrary resources.
2. `frontend/src/App.tsx` environment label:
   - no longer hard-codes `生产环境`;
   - reads `VITE_APP_ENV` or `VITE_DEPLOY_ENV`;
   - shows `环境未配置` when neither is set;
   - maps production/staging/canary/development/local to visible labels.
3. `frontend/src/__tests__/App.test.tsx` covers topbar search routing and the unset-environment fallback.
4. Evidence and status docs are updated:
   - `docs/ops/evidence/PRD3-B03-control-panel-semantics-local-20260705.md`
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
   - `docs/issues/OPEN-ISSUES.md`
   - `docs/ops/CURRENT-STATUS.md`
5. Run these commands:

```bash
npm --prefix frontend test -- --run App
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Expected Result

- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3 Batch 3 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `docs/agent-workflows/VERIFY-RESULT.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B03-control-panel-semantics-local-20260705.md`
