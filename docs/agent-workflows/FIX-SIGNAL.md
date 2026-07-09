# FIX-SIGNAL.md - PRD5 Batch 1R Gate A Request ID Approval Runbook

**Date:** File timestamp is authoritative
**Risk level:** R1 PRD5/X-Insight coordination docs and shared worksheet only
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD5 Batch 1F passed Canary 11/11 and Sentinel
merged it as `e9670ad` / PR #109. PRD5 Batch 1G passed Canary 11/11 and
Sentinel merged it as `c701843` / PR #110. PRD5 Batch 1H passed Canary 10/10
and Sentinel merged it as `61f5610` / PR #111. PRD5 Batch 1I passed Canary
11/11 and Sentinel merged it as `1dc20b5` / PR #112. PRD5 Batch 1J passed
Canary and Sentinel merged it as `f5223df` / PR #113. PRD5 Batch 1K passed
Canary 10/10 and Sentinel merged it as `2db830a` / PR #114. PRD5 Batch 1L
passed Canary 10/10 and Sentinel merged it as `ffebf83` / PR #115. PRD5 Batch
1M passed Canary 10/10 and Sentinel merged it as `4a4e254` / PR #116. PRD5
Batch 1N passed Canary 10/10 and Sentinel merged it as `92cb8ce` / PR #117.
PRD5 Batch 1O passed Canary 8/8 and Sentinel merged it as `b2eea2e` / PR #118.
PRD5 Batch 1P passed Canary 6/6 and Sentinel merged it as `61091c` / PR #119.
PRD5 Batch 1Q passed Canary 6/6 and Sentinel merged it as `ed8c69d` / PR #120.

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

The User asked how to approve concrete request IDs because the JSON approval
manifest is technical. Batch 1R turns the Gate A request ID approval boundary
into a non-technical runbook plus a shared approval worksheet. This round does
not change the snapshot script and does not generate a real snapshot artifact.

## Intended Changes

- Reconcile Batch 1Q completion as `ed8c69d` / PR #120.
- Add `P5-F27` / Batch 1R to PRD5 scope, matrix, implementation plan, backlog,
  Gate A coordination, current status, handoff, and docs indexes.
- Add `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md`.
- Add shared approval worksheet:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`
- Explain in plain language:
  - a `request_id` is one request receipt;
  - approving a row exports only sanitized metadata for that exact request;
  - `approved`, `pending`, and `unavailable` have different export behavior;
  - approval manifest is safer than auto-discovery, read-only DB/view, or
    exporter/outbox for the first Gate A exchange.

## Non-Goals

This round does not:

- generate a real snapshot artifact;
- invent, approve, or write real request IDs;
- write a DSN, password, token, credential, raw prompt, raw response, raw IP, or
  raw upstream error into the repo or shared folder;
- add a DB migration;
- change runtime request handling;
- add a new API route;
- change frontend behavior;
- change `scripts/prd5_xinsight_snapshot.go`;
- create read-only DB credentials or views;
- create shared ECS staging;
- add exporter/outbox;
- add X-Insight as a runtime dependency;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify repo history includes `ed8c69d` / PR #120 for PRD5 Batch 1Q and that
   `docs/agent-workflows/VERIFY-RESULT.md` records Batch 1Q PASS 6/6.
2. Verify `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md` explains:
   - `request_id` as a single request receipt;
   - what the User approves and does not approve;
   - `approved`, `pending`, and `unavailable`;
   - scenario list requested by X-Insight;
   - tradeoffs versus auto-discovery, read-only DB/view, and exporter/outbox;
   - an example non-technical approval sentence.
3. Verify the shared approval worksheet exists and contains only placeholder
   rows, allowed statuses, and warnings not to paste secrets or raw payloads.
4. Verify PRD5 status, matrix, implementation plan, backlog, Gate A
   coordination, handoff, and docs indexes route Batch 1R correctly.
5. Verify no code/runtime change, real snapshot artifact, approved request ID
   invention, DSN/credential/token/raw payload, read-only DB/view credential,
   shared ECS staging, exporter/outbox, X-Insight runtime dependency, PRD4
   matrix change, or `Production verified` claim is included.
6. Run:

```bash
git log --oneline -3 origin/main | rg "ed8c69d.*PRD5 Batch 1Q|#120"
rg -n "Gate A Request ID Approval Runbook|P5-F27|Batch 1R|receipt number for one UniRouter|approval manifest is a whitelist|Auto-discover request IDs|Read-only DB/view|Exporter/outbox|Example User Approval Sentence" docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/CODEBASE-MAP.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md"
rg -n 'approved|pending|unavailable|Do not paste raw prompts|Only rows marked `approved`' "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md"
git diff --name-only
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git diff --check
```

No Go/frontend test run is required for Batch 1R because it is docs and
shared-folder coordination only.

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any code/runtime change, real snapshot artifact, approved request ID
  invention, migration, runtime request-path change, API route, frontend
  behavior, secret, read-only DB/view credential, shared ECS staging,
  exporter/outbox, raw payload capture, X-Insight runtime dependency, or
  `Production verified` claim as a scope failure;
- final result for PRD5 Batch 1R.
