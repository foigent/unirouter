# FIX-SIGNAL.md - PRD3 Batch 7 Document Lifecycle Cleanup

**Date:** 2026-07-05
**Status:** Fixed awaiting Canary

PRD3 Batch 6 API contract refresh passed Canary 6/6 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #69 at `1ae60b0`;
User reported Canary, Sentinel, and Atlas completed.

Codex has completed PRD3 Batch 7 as a documentation lifecycle cleanup for
P3-F08. This aligns first-read docs with the current lifecycle: PRD3 active,
PRD2 frozen, PRD1 historical/reference, and PRD4 candidate backlog.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- This batch changes documentation only.
- No runtime code, API route, handler, RBAC, frontend behavior, test code, build
  config, deployment config, or archive file changed.
- No files were deleted.
- This does not close PRD1/PRD2 evidence gaps; it only routes readers to the
  correct current source of truth.

## Verification Request

Please verify:

1. Batch 6 status is advanced from awaiting verification to complete:
   - `docs/ops/CURRENT-STATUS.md`;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`;
   - `docs/issues/OPEN-ISSUES.md`.
2. Root `README.md` routes readers to current PRD3 documents first and no longer
   presents PRD1 closeout as the active first-read path.
3. `docs/README.md` routes current entry docs to PRD3/current status first, then
   frozen or historical PRD1/PRD2 material.
4. `docs/PROJECT-HANDOFF.md` first-read order starts from current status, PRD3
   matrix, PRD3 scope, PRD3 audit, open issues, agent protocol, current
   verification request/result, codebase map, and inventory/governance.
5. `docs/DOCS-GOVERNANCE.md` defines PRD1 historical/frozen, PRD2 frozen, PRD3
   active, and PRD4 candidate lifecycle states.
6. `docs/DOC-INVENTORY.md` classifies PRD2 as frozen, PRD3 as active, and PRD4
   as candidate backlog.
7. `docs/CODEBASE-MAP.md` includes current backend packages and frontend route
   page groups.
8. Status/evidence docs are updated:
   - `docs/ops/evidence/PRD3-B07-doc-lifecycle-cleanup-local-20260705.md`;
   - `docs/ops/CURRENT-STATUS.md`;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`;
   - `docs/issues/OPEN-ISSUES.md`;
   - `docs/agent-workflows/FIX-SIGNAL.md`;
   - `README.md`;
   - `docs/README.md`;
   - `docs/PROJECT-HANDOFF.md`;
   - `docs/DOCS-GOVERNANCE.md`;
   - `docs/DOC-INVENTORY.md`;
   - `docs/CODEBASE-MAP.md`.
9. Run these commands:

```bash
rg -n "PRD3 Batch 6 API contract refresh fixed awaiting Canary|PRD3-CONTRACT-001 \\| High \\| Fixed awaiting Canary" docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md
rg -n "Frontend tests: PASS, 33 tests|pre-existing uncommitted changes|PRD2 产品需求草案|当前 PRD1 API 路径快照|ratelimit/|hooks/" README.md docs/README.md docs/PROJECT-HANDOFF.md docs/DOCS-GOVERNANCE.md docs/DOC-INVENTORY.md docs/CODEBASE-MAP.md
rg -n "PRD3|PRD2.*frozen|PRD1.*historical|PRD4.*candidate|PRD4.*候选" README.md docs/README.md docs/PROJECT-HANDOFF.md docs/DOCS-GOVERNANCE.md docs/DOC-INVENTORY.md
git diff --check
```

## Expected Result

- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 7 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `README.md`
- `docs/README.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOCS-GOVERNANCE.md`
- `docs/DOC-INVENTORY.md`
- `docs/CODEBASE-MAP.md`
- `docs/ops/evidence/PRD3-B07-doc-lifecycle-cleanup-local-20260705.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
