# PRD3 Batch 7 Document Lifecycle Cleanup - Local Evidence

Date: 2026-07-05
Owner: Codex
Scope: PRD3 P3-F08 documentation lifecycle cleanup

## Summary

This batch updates first-read and inventory documents so new readers land on the
active PRD3 state first, then frozen PRD2 references, historical PRD1 references,
and PRD4 candidate backlog.

## Files Changed

- `README.md`
- `docs/README.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOCS-GOVERNANCE.md`
- `docs/DOC-INVENTORY.md`
- `docs/CODEBASE-MAP.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Decisions

- Root and docs README files route readers to active PRD3 scope before PRD1
  historical closeout material.
- `PROJECT-HANDOFF.md` first-read order starts with current status, PRD3 matrix,
  PRD3 scope, PRD3 consistency audit, open issues, agent protocol, and current
  verification request/result.
- `DOCS-GOVERNANCE.md` defines PRD1 historical/frozen, PRD2 frozen, PRD3 active,
  and PRD4 candidate lifecycle states.
- `DOC-INVENTORY.md` classifies PRD2 as frozen, PRD3 as active, and PRD4 as
  candidate backlog.
- `CODEBASE-MAP.md` now includes current backend packages and current frontend
  route page groups.

## Non-Goals

- No historical/archive files were rewritten.
- No files were deleted.
- No runtime code, API, route, UI behavior, or build/deploy configuration changed.
- This batch does not close PRD1/PRD2 evidence gaps; it only routes current
  readers to the correct source of truth.

## Local Verification

Commands run after the documentation update:

```bash
rg -n "PRD3 Batch 6 API contract refresh fixed awaiting Canary|PRD3-CONTRACT-001 \\| High \\| Fixed awaiting Canary" docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md
rg -n "Frontend tests: PASS, 33 tests|pre-existing uncommitted changes|PRD2 产品需求草案|当前 PRD1 API 路径快照|ratelimit/|hooks/" README.md docs/README.md docs/PROJECT-HANDOFF.md docs/DOCS-GOVERNANCE.md docs/DOC-INVENTORY.md docs/CODEBASE-MAP.md
rg -n "PRD3|PRD2.*frozen|PRD1.*historical|PRD4.*candidate|PRD4.*候选" README.md docs/README.md docs/PROJECT-HANDOFF.md docs/DOCS-GOVERNANCE.md docs/DOC-INVENTORY.md
git diff --check
```

Result:

- No stale Batch 6 awaiting status remains in current status/matrix/issue docs.
- No stale first-read wording remains in the checked entry docs.
- Current/frozen/historical/candidate lifecycle labels are present.
- `git diff --check` exits successfully.
