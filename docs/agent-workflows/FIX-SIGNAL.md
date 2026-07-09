# FIX-SIGNAL.md - PRD5 Batch 1O Gate A Controlled Snapshot Handoff Prep

**Date:** File timestamp is authoritative
**Risk level:** R2 acceptance-status documentation plus shared-folder
coordination update
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

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

X-Insight currently has UniRouter discovery/import/persistent import/minimal
read API surfaces, but Gate A is not complete until X-Insight imports a real
controlled UniRouter snapshot and reads it back through those APIs. Batch 1O
prepares that handoff without generating a real snapshot or handing off
credentials.

## Intended Changes

Docs and shared folder:

- Reconcile PRD5 status docs so Batch 1N is Canary/Sentinel complete with
  `92cb8ce` / PR #117.
- Add P5-F24 / Batch 1O as Gate A controlled snapshot handoff prep.
- Document that the next real snapshot requires approved request IDs, scenario
  labels, a non-secret source instance label, and approved data source access.
- Add shared-folder request:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-prd5-controlled-snapshot-handoff-prep.md`

## Non-Goals

This round does not:

- generate a real snapshot artifact;
- write a DSN, password, token, credential, raw prompt, raw response, raw IP, or
  raw upstream error into the repo or shared folder;
- add a DB migration;
- add a new API route;
- change runtime request handling;
- change frontend behavior;
- create read-only DB credentials or views;
- create shared ECS staging;
- add exporter/outbox;
- add X-Insight as a runtime dependency;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify repo history includes `92cb8ce` / PR #117 for PRD5 Batch 1N.
2. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1N PASS
   10/10.
3. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F23 as `Canary passed`
   and adds P5-F24 as `Fixed awaiting Canary`.
4. Verify `docs/PRD-Phase5.md` defines P5-F24 and does not reopen PRD4.
5. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says there is no blocking
   condition for UniRouter Batch 1A through Batch 1O, while controlled snapshot
   artifact generation remains blocked until approved request IDs, scenario
   labels, and source data access are available.
6. Verify the shared request file exists and does not contain DSNs, credentials,
   raw prompts, raw responses, raw IP addresses, or raw upstream error text.
7. Verify no runtime code, migration, API route, frontend behavior, real
   snapshot artifact, read-only DB/view credential, shared ECS staging,
   exporter/outbox, X-Insight runtime dependency, or PRD4 matrix change is
   included.
8. Run:

```bash
git log --oneline -3 origin/main
rg -n "Gate A Controlled Snapshot Handoff Prep|P5-F24|Batch 1O|92cb8ce|PR #117|controlled sanitized snapshot|approved request IDs|no DSN|no raw prompt/response|read-only DB/view remains gated" docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-prd5-controlled-snapshot-handoff-prep.md"
rg -n "P5-F23 \\| Snapshot Capture Gap Candidates \\| Canary passed|92cb8ce|PR #117" docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/ops/CURRENT-STATUS.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/PROJECT-HANDOFF.md
git diff --name-only
git diff --check
```

No Go/frontend test run is required for Batch 1O because it changes only docs
and shared-folder coordination files.

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any code, migration, API route, frontend, real snapshot artifact, secret,
  read-only DB/view credential, shared ECS staging, exporter/outbox, raw payload
  capture, or X-Insight runtime dependency as a scope failure;
- final result for PRD5 Batch 1O.
