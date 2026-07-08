# FIX-SIGNAL.md - PRD4 Batch 1S Open Issue Boundary Closeout

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R2 documentation/status-boundary change
**Scope:** PRD4/PRD5 issue classification only

## Verification Request

Please verify PRD4 Batch 1S: Open Issue Boundary Closeout.

This batch does not implement product code. It closes PRD4 closeout ambiguity by
moving remaining non-current PRD4 implementation gaps to explicit PRD5 backlog
items and leaving the target database cleanup task with Atlas.

## Scope To Verify

Files:

- `docs/issues/OPEN-ISSUES.md`
- `docs/planning/PRD5-BACKLOG.md`
- `docs/ops/evidence/PRD4-B1S-open-issue-boundary-closeout-20260708.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PRD-Phase4.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Expected Behavior

- `PRD4-PLUGIN-001` is no longer an active PRD4 implementation gap; it is
  `Deferred to PRD5` and maps to `PRD5-PLUGIN-001` / `PRD5-MARKET-001`.
- `PRD4-DEPLOY-001` is no longer an active PRD4 implementation gap; it is
  `Deferred to PRD5` and maps to `PRD5-DEPLOY-001`.
- `PRD4-INFRA-001` is no longer an active PRD4 implementation gap; it is
  `Deferred to PRD5` and maps to new `PRD5-INFRA-001`.
- `PRD4-DEPLOY-002` remains `Open` because target test database cleanup is an
  Atlas target-environment task.
- PRD4 matrix totals stay stable: 13 rows, 11 `Canary passed`, 2 `Deferred to
  PRD5`.
- Current status and handoff docs show Batch 1S as fixed awaiting Canary while
  keeping Batch 1R as the latest completed verification.

## Non-Scope

Do not expect this batch to:

- change Go source, frontend source, migrations, API routes, runtime behavior,
  deployment scripts, or evidence harness code;
- delete target databases or mutate target infrastructure;
- claim `Production verified`;
- reopen PRD5 real upstream, real SSO, runtime plugin platform, managed HA, or
  multi-instance infrastructure observability as current PRD4 implementation.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "PRD4-PLUGIN-001|PRD4-DEPLOY-001|PRD4-INFRA-001|PRD4-DEPLOY-002|PRD5-PLUGIN-001|PRD5-MARKET-001|PRD5-DEPLOY-001|PRD5-INFRA-001|Batch 1S|PRD4-B1S-open-issue-boundary-closeout-20260708" docs/issues/OPEN-ISSUES.md docs/planning/PRD5-BACKLOG.md docs/ops/evidence/PRD4-B1S-open-issue-boundary-closeout-20260708.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md docs/agent-workflows/FIX-SIGNAL.md

awk -F'|' '/^\| P4-F[0-9]+ / {gsub(/^ +| +$/, "", $4); count[$4]++; total++} END {print "total=" total; for (s in count) print s "=" count[s]}' docs/ops/PRD4-ACCEPTANCE-MATRIX.md

git diff --check
```

Optional consistency check:

```bash
rg -n "PRD4-PLUGIN-001|PRD4-DEPLOY-001|PRD4-INFRA-001|PRD4-DEPLOY-002" docs/issues/OPEN-ISSUES.md
```

## Acceptance Criteria

1. `OPEN-ISSUES.md` allows the `Deferred to PRD5` status and uses it for
   `PRD4-PLUGIN-001`, `PRD4-DEPLOY-001`, and `PRD4-INFRA-001`.
2. `OPEN-ISSUES.md` keeps `PRD4-DEPLOY-002` open for Atlas target test database
   cleanup.
3. `PRD5-BACKLOG.md` includes `PRD5-INFRA-001` and already includes the mapped
   plugin/deploy backlog items.
4. PRD4 status, matrix, PRD, handoff, inventory, README, evidence, and this
   signal all reference Batch 1S consistently.
5. The PRD4 acceptance matrix count remains `total=13`, `Canary passed=11`,
   `Deferred to PRD5=2`.
6. `git diff --check` passes.
7. No code/runtime/deployment/target-infrastructure file changed for Batch 1S.
