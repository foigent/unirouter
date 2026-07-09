# FIX-SIGNAL.md - PRD5 Batch 1S X-Insight Data Exchange Control Surface

**Date:** File timestamp is authoritative
**Risk level:** R1 PRD5/X-Insight planning docs and shared-folder coordination only
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
PRD5 Batch 1R passed Canary 6/6 and Sentinel merged it as `c15cd7d` / PR #121.

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

The User asked whether the approval process should become a separate UniRouter
UI entry and whether X-Insight data-flow audits should be placed inside that
entry. Batch 1S records the product/architecture answer: a future UI entry is
reasonable, but it should be a UniRouter-owned Data Exchange Control surface for
outbound approval, export ledger, handoff artifacts, import receipts, and deep
links, not a full X-Insight audit or investigation console.

The current X-Insight shared-folder responses support this boundary: X-Insight
asks for one small approved-request combined snapshot first, says not to create
read-only DB/view, shared ECS staging, exporter/outbox, or runtime dependency
before that snapshot loop is proven, and owns normalization, persistence/read
APIs, capture-gap interpretation, UI/policy simulation, and cross-source
compatibility.

## Intended Changes

- Reconcile Batch 1R completion as `c15cd7d` / PR #121.
- Add `P5-F28` / Batch 1S to PRD5 scope, matrix, implementation plan, backlog,
  Gate A coordination, current status, handoff, codebase map, and docs indexes.
- Add `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`.
- Add shared X-Insight review request:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md`
- Record the product boundary:
  - UniRouter owns outbound approval, export ledger, handoff artifacts, import
    receipts, safety summary, and X-Insight deep links when available.
  - X-Insight owns importer internals, normalized evidence, read-side audit,
    investigation timeline/graph, node detail, policy simulation, and evidence
    packs.
- Record staged implementation: current Markdown/manifest first, then UI
  readiness spec, then minimal approval/export-ledger UI, then receipt
  reconciliation, then exporter/outbox only after Gate A proves the need.

## Non-Goals

This round does not:

- implement UI code;
- add API routes;
- add DB migrations;
- change runtime request handling;
- change snapshot script behavior;
- generate a real snapshot artifact;
- invent, approve, or write real request IDs;
- write a DSN, password, token, credential, raw prompt, raw response, raw IP, or
  raw upstream error into the repo or shared folder;
- create read-only DB credentials or views;
- create shared ECS staging;
- add exporter/outbox;
- add X-Insight as a runtime dependency;
- duplicate X-Insight timeline, graph, policy simulation, evidence packs, or
  read-side audit in UniRouter;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify repo history includes `c15cd7d` / PR #121 for PRD5 Batch 1R and that
   `docs/agent-workflows/VERIFY-RESULT.md` records Batch 1R PASS 6/6.
2. Verify `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md` states:
   - a future UI entry is reasonable;
   - the entry should be Data Exchange Control, not a full X-Insight audit
     console;
   - UniRouter owns outbound approval/export ledger/import receipts/deep links;
   - X-Insight owns downstream observability, read-side audit, investigation
     timeline/graph, policy simulation, and evidence packs;
   - staged implementation keeps exporter/outbox deferred.
3. Verify the shared review request exists and asks X-Insight for receipt/deep
   link boundary feedback without requesting secrets, raw payloads, or importer
   internals.
4. Verify PRD5 status, matrix, implementation plan, backlog, Gate A
   coordination, handoff, codebase map, and docs indexes route Batch 1S and
   `P5-F28` correctly.
5. Verify no UI code, API route, migration, runtime request-path change, snapshot
   script change, real snapshot artifact, approved request ID invention,
   DSN/credential/token/raw payload, read-only DB/view credential, shared ECS
   staging, exporter/outbox, X-Insight runtime dependency, PRD4 matrix change, or
   `Production verified` claim is included.
6. Run:

```bash
git log --oneline -3 origin/main | rg "c15cd7d.*PRD5 Batch 1R|#121"
rg -n "X-Insight Data Exchange Control Surface|P5-F28|Batch 1S|Data Exchange Control|outbound ledger|read-side audit|Investigation|Receipt Reconciliation|Stage 2: Minimal UniRouter UI|Put all X-Insight audits in UniRouter|Do not do this" docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md"
rg -n "import_job_id|exchange_id|deep link|read-side audit|X-Insight owns" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md"
git status --short
git diff --name-only
git diff -- frontend internal cmd scripts
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git diff --check
```

No Go/frontend test run is required for Batch 1S because it is docs and
shared-folder coordination only.

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any UI code, API route, migration, runtime request-path change, snapshot script
  change, real snapshot artifact, approved request ID invention, secret,
  read-only DB/view credential, shared ECS staging, exporter/outbox, raw payload
  capture, X-Insight runtime dependency, or `Production verified` claim as a
  scope failure;
- final result for PRD5 Batch 1S.
