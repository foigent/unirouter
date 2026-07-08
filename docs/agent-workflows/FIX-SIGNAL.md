# FIX-SIGNAL.md - PRD4 Batch 1P Scope Deferral And Closeout Alignment

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R2 acceptance-boundary documentation
**Scope:** P4-F08 / P4-F09 deferral, PRD5 backlog alignment, PRD4 closeout planning

## Verification Request

Please verify PRD4 Batch 1P: Scope Deferral And Closeout Alignment.

This is a documentation/status-boundary change only. It implements the User's
2026-07-08 decision:

- no approved real upstream target environment exists now;
- no approved real SSO target environment exists now;
- `P4-F08` and `P4-F09` real target execution must not remain current PRD4
  implementation gaps;
- PRD4 keeps Batch 1H/1I guarded harness assets only;
- real upstream target E2E and real SSO target E2E move to PRD5/unimplemented
  scope.

## Scope To Verify

Docs/status files:

- `docs/agent-workflows/FIX-SIGNAL.md`
- `docs/PRD-Phase4.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/planning/PRD5-BACKLOG.md`
- `docs/planning/PRD4-BACKLOG.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/CODEBASE-MAP.md`

## Expected Behavior

- `P4-F08` and `P4-F09` are no longer represented as current PRD4
  implementation gaps requiring real target execution.
- `P4-F08` and `P4-F09` rows in the PRD4 matrix are `Deferred to PRD5`.
- Batch 1H and Batch 1I remain accurately described as accepted guarded harness
  assets.
- `docs/planning/PRD5-BACKLOG.md` has explicit reopenable items for real
  upstream target E2E and real SSO target E2E.
- `P4-F11` Production Evidence Promotion excludes `P4-F08` / `P4-F09` real
  target execution from current PRD4 closeout.
- Index and handoff docs route future real upstream/SSO target runs to PRD5.

## Non-Scope

Do not expect this batch to:

- modify Go, frontend, migrations, API routes, runtime behavior, deployment
  scripts, or evidence harness code;
- delete Batch 1H/1I harnesses or historical evidence;
- claim real upstream or real SSO E2E acceptance;
- claim Production verified status for upstream, SSO, license, webhook, HA/DR,
  runtime, or UI rows;
- resolve `PRD4-RUNTIME-002`, `PRD4-INFRA-001`, `PRD4-DEPLOY-001`, or
  `PRD4-PLUGIN-001`.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "PRD5-UPSTREAM-001|PRD5-SSO-001|P4-F08|P4-F09|Deferred to PRD5|real target execution|real upstream/SSO harness|2026-07-08" docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD4-BACKLOG.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md docs/agent-workflows/FIX-SIGNAL.md

awk -F'|' '/^\| P4-F[0-9]+ / {gsub(/^ +| +$/, "", $4); count[$4]++; total++} END {print "total=" total; for (s in count) print s "=" count[s]}' docs/ops/PRD4-ACCEPTANCE-MATRIX.md

git diff --check
```

## Acceptance Criteria

1. `P4-F08` is `Deferred to PRD5` in the PRD4 matrix and points to
   `PRD5-UPSTREAM-001`.
2. `P4-F09` is `Deferred to PRD5` in the PRD4 matrix and points to
   `PRD5-SSO-001`.
3. Batch 1H/1I harnesses remain recorded as accepted Canary/Sentinel work, not
   deleted or reclassified as failed.
4. `docs/planning/PRD5-BACKLOG.md` contains reopen prerequisites for real
   upstream and real SSO target E2E.
5. `P4-F11` no longer treats upstream/SSO real target execution as current PRD4
   promotion work.
6. Current-status and handoff docs state that no approved real environments are
   currently available and that real target execution is out of PRD4 closeout.
7. README, DOC-INVENTORY, and CODEBASE-MAP do not imply current PRD4 real
   target execution.
8. The PRD4 matrix count is expected to be 13 total rows with 9 `Canary passed`,
   2 `Deferred to PRD5`, 1 `Fixed awaiting Canary`, and 1 `Planned`.
9. `git diff --check` passes.
