# FIX-SIGNAL.md - PRD5 Batch 1N Snapshot Capture Gap Candidates

**Date:** File timestamp is authoritative
**Risk level:** R2 explicit-request snapshot tooling/output contract change plus
R2 acceptance-status documentation update
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
1M passed Canary 10/10 and Sentinel merged it as `4a4e254` / PR #116.

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

X-Insight Gate A importer evidence is still absent from the shared folder, so
read-only DB/view access, shared ECS staging, and exporter/outbox design remain
gated. Batch 1N continues UniRouter-owned evidence readiness by making Gate A
snapshot-generated missing-row gaps more structured for X-Insight import review.
It does not require X-Insight at runtime.

## Intended Changes

Code:

- `scripts/prd5_xinsight_snapshot.go`
  - adds optional `candidate_capture_gaps` to generated `capture_gaps` rows;
  - keeps generated missing-row `capture_gap` conservative as
    `not_available_from_source`;
  - adds `sampled_out`, `retention_expired`, and `not_instrumented` candidates
    when a `call_logs` row exists but no `request_trace_events` row is present;
  - adds a generated capture-gaps table to the optional Markdown handoff note.

Docs and shared folder:

- PRD5 status docs mark Batch 1M Canary/Sentinel complete and add P5-F23 as
  Batch 1N fixed awaiting Canary.
- `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md` documents the additive
  `candidate_capture_gaps` output field and handoff-note table.
- `docs/tech-ref/26-prd5-evidence-field-contract.md` documents
  `candidate_capture_gaps` as a snapshot/import aid, not a new runtime column.
- Shared-folder contract:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-snapshot-capture-gap-candidates.md`

## Non-Goals

This round does not:

- add a DB migration;
- add a new API route;
- change runtime request handling;
- change frontend behavior;
- change routing, provider dispatch, billing, wallet, auth, admission, or
  response payload semantics;
- trust public inbound request headers by default;
- capture raw prompt/response payloads;
- hash prompt/response payloads;
- create payload object references;
- add exporter/outbox;
- create read-only DB credentials, read-only views, or shared ECS staging;
- add X-Insight as a runtime dependency;
- assert one definitive sampling/retention/non-instrumented cause;
- infer cross-system agent/tool/business-system graph nodes;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1M PASS
   10/10 and that repo history includes Sentinel merge `4a4e254` / PR #116.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F22 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F23 as
   `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F23 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1N and lists Batch 1N non-goals.
5. Verify `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md` documents
   `candidate_capture_gaps` and the generated handoff-note capture-gap table.
6. Verify the shared contract file exists and describes the UniRouter/X-Insight
   snapshot capture-gap candidate boundary.
7. Verify `scripts/prd5_xinsight_snapshot.go` adds
   `candidate_capture_gaps` to generated gap records.
8. Verify missing `request_trace_events` rows with existing `call_logs` rows
   list `sampled_out`, `retention_expired`, and `not_instrumented` only as
   candidates, while the primary generated `capture_gap` remains
   `not_available_from_source`.
9. Verify no migration, runtime request-path change, new route, frontend
   behavior, exporter/outbox, read-only credential/view, raw payload capture,
   X-Insight runtime dependency, public-header authority by default, definitive
   sampling/retention cause assertion, or PRD4 matrix change is included.
10. Run:

```bash
go run scripts/prd5_xinsight_snapshot.go -help
go run scripts/prd5_xinsight_snapshot.go --request-id req-example --handoff-note - 2>&1 | rg -- "--handoff-note must be a Markdown file path"
go test ./... -count=1
rg -n "Snapshot Capture Gap Candidates|P5-F23|Batch 1N|candidate_capture_gaps|CandidateGaps|sampled_out|retention_expired|not_instrumented|Capture Gaps|4a4e254|PR #116" scripts/prd5_xinsight_snapshot.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-snapshot-capture-gap-candidates.md"
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any public-header evidence trust by default as a blocking failure;
- any new raw payload capture as a blocking failure;
- any payload hash/reference/export implementation as a scope failure;
- any fabricated cross-system agent/tool/business-chain fact as a blocking
  failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- any definitive sampling/retention/non-instrumented cause assertion as a scope
  failure;
- final result for PRD5 Batch 1N.
