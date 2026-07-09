# FIX-SIGNAL.md - PRD5 Batch 1V Gate A Authorized Controlled Execution

**Date:** File timestamp is authoritative
**Risk level:** R2 authorization/status documentation; Atlas execution is R4
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1U passed Canary 7/7, and Sentinel merged it as `b2ba892` / PR #124.
PRD4 remains complete for the confirmed current scope.

The User accepted the complete Gate A controlled-run recommendation on
2026-07-10. Authorization `UR-XI-GATE-A-20260710-01` now permits Atlas, and no
other operator, to execute exactly `successful_non_stream` and
`model_permission_denied` in the current UniRouter Alibaba Cloud ECS
development/test environment after Atlas confirms it is not production.

This batch formalizes that decision and the Atlas handoff. It does not claim
that Atlas has deployed, sent requests, generated a snapshot, obtained an
X-Insight receipt, or deleted the temporary snapshot.

## Intended Changes

- Record Batch 1U Canary/Sentinel completion as `b2ba892` / PR #124.
- Promote the shared recommendation to accepted decision
  `UR-XI-GATE-A-20260710-01` and close the authorization request as answered.
- Bind Atlas-only execution to a non-production environment attestation, exact
  merged version, health/rollback evidence, and stop conditions.
- Authorize exactly two synthetic scenarios with response bodies discarded.
- Delegate exact-ID approval to Atlas only after expected status, returned
  `X-Request-ID`, source label, and environment checks pass.
- Keep all other worksheet scenarios `unavailable` for this authorization.
- Add a shared Atlas execution request and pending result template without
  inventing IDs, artifacts, receipts, or cleanup results.
- Route `P5-F31` / Batch 1V through PRD5 scope, matrix, plan, backlog,
  coordination, status, handoff, and document indexes.

## Non-Goals

This documentation round does not:

- execute deployment or requests from Codex;
- invent or approve placeholder request IDs;
- claim a snapshot, X-Insight receipt, cleanup result, Gate A import acceptance,
  or `Production verified` state;
- add code, API routes, migrations, frontend behavior, or runtime dependencies;
- expose credentials, DSNs, tokens, raw prompt/response, raw IP, raw upstream
  error, or broad query output;
- create X-Insight read-only DB/view access, shared cross-project ECS staging,
  or exporter/outbox;
- reassign Atlas authorization automatically;
- reopen or modify PRD4 acceptance.

## Canary Verification Instructions

1. Verify `origin/main` contains Batch 1U as `b2ba892` / PR #124 and the prior
   `VERIFY-RESULT.md` records Batch 1U PASS 7/7.
2. Verify the decision and answered request use the same authorization ID,
   approved environment, Atlas-only operator, two scenarios, safety profile,
   and seven-day retention rule.
3. Verify the Atlas request requires non-production attestation, required merged
   code, health/rollback evidence, exactly two returned request IDs, and stop on
   access/target/precondition mismatch.
4. Verify the worksheet keeps the two authorized IDs pending until actual
   checks pass and marks every other scenario unavailable.
5. Verify the result template remains `Awaiting Atlas execution` with required
   values `Pending`; no fabricated target result, snapshot, receipt, or cleanup
   evidence exists.
6. Verify PRD5 docs route `P5-F31` / Batch 1V as `Authorized awaiting Atlas`,
   while Batch 1U is Canary/Sentinel complete and PRD4 remains closed.
7. Verify no non-goal or PRD4 boundary regression, then run:

```bash
git log --oneline -3 origin/main | rg "b2ba892.*PRD5 Batch 1U|#124"
rg -n "PASS.*7/7|PRD5 Batch 1U" docs/agent-workflows/VERIFY-RESULT.md
rg -n "UR-XI-GATE-A-20260710-01|Authorized awaiting Atlas|Atlas only|not yet executed|exactly two|successful_non_stream|model_permission_denied|seven days|not production" docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-10-gate-a-controlled-run-authorization-recommendation.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-controlled-run-authorization.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-atlas-execution.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-10-unirouter-gate-a-atlas-run-result.md"
rg -n "Awaiting Atlas execution|Pending" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-10-unirouter-gate-a-atlas-run-result.md"
git diff -- frontend internal cmd migrations scripts
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git status --short
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- evidence that the authorization, Atlas request, worksheet, pending result
  template, and PRD5 routing agree;
- any fabricated result, secret/raw data, scope widening, operator reassignment,
  PRD4 regression, or other non-goal as a blocking failure;
- final documentation-gate result for PRD5 Batch 1V, without overwriting the
  separate Atlas target outcome.
