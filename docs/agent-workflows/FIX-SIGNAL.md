# FIX-SIGNAL.md - PRD5 Batch 1Q Gate A Request Approval Manifest

**Date:** File timestamp is authoritative
**Risk level:** R2 snapshot tooling plus PRD5/X-Insight coordination docs
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

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

X-Insight has now requested a small scenario-based list of approved request IDs
for the first real Gate A combined sanitized snapshot. UniRouter should not
fabricate those IDs or discover broad data. Batch 1Q adds an approval-manifest
path so the later snapshot can only use User/UniRouter-owner approved IDs.

## Intended Changes

Snapshot tooling:

- Add `--approval-manifest <path>` to `scripts/prd5_xinsight_snapshot.go`.
- Define manifest schema `unirouter.prd5.xinsight.approval.v1`.
- Allow row statuses `approved`, `pending`, and `unavailable`.
- Export only `approved` rows with concrete request IDs.
- Reject approved placeholder IDs such as `TBD`, `n/a`, or `unavailable`.
- Reject combining `--approval-manifest` with direct `--request-id`,
  `--request-ids`, or `--scenario` flags.
- Use manifest `source_instance` unless `--source-instance` is explicitly set.
- Set snapshot safety `selection_mode=approved_request_id_manifest` for
  manifest-based exports.

Docs and shared folder:

- Reconcile Batch 1P completion as `61091c` / PR #119.
- Add P5-F26 / Batch 1Q planning, matrix, snapshot tech-ref, code-map, status,
  and handoff docs.
- Add UniRouter response to X-Insight:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-approved-request-ids-response.md`

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
- create read-only DB credentials or views;
- create shared ECS staging;
- add exporter/outbox;
- add X-Insight as a runtime dependency;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify repo history includes `61091c` / PR #119 for PRD5 Batch 1P and that
   `docs/agent-workflows/VERIFY-RESULT.md` records Batch 1P PASS 6/6.
2. Verify `scripts/prd5_xinsight_snapshot.go -help` documents
   `--approval-manifest`.
3. Verify manifest error boundaries:
   - pending-only manifests fail before requiring DSN;
   - manifest mode cannot be combined with direct request flags;
   - approved placeholder request IDs are rejected.
4. Verify docs and shared response state that real request IDs must come from
   User/UniRouter-owner approval and are not supplied by Codex.
5. Verify no migration, runtime request-path change, API route, frontend
   behavior, real snapshot artifact, approved request ID invention,
   DSN/credential/token/raw payload, read-only DB/view credential, shared ECS
   staging, exporter/outbox, X-Insight runtime dependency, PRD4 matrix change, or
   `Production verified` claim is included.
6. Run:

```bash
go run scripts/prd5_xinsight_snapshot.go -help 2>&1 | rg -- "-approval-manifest"
go run scripts/prd5_xinsight_snapshot.go --approval-manifest <(printf '%s' '{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"unirouter-dev-gate-a-2026-07-10","requests":[{"scenario_label":"successful_non_stream","request_id":"TBD","approval_status":"pending","required":true}]}') 2>&1 | rg "approval manifest contains no approved request IDs"
go run scripts/prd5_xinsight_snapshot.go --approval-manifest <(printf '%s' '{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"unirouter-dev-gate-a-2026-07-10","requests":[{"scenario_label":"successful_non_stream","request_id":"TBD","approval_status":"approved","required":true}]}') 2>&1 | rg "approved entry must include a concrete request_id"
go run scripts/prd5_xinsight_snapshot.go --approval-manifest <(printf '%s' '{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"unirouter-dev-gate-a-2026-07-10","requests":[{"scenario_label":"successful_non_stream","request_id":"req-approved-1","approval_status":"approved","required":true}]}') --request-id req-extra 2>&1 | rg -- "--approval-manifest cannot be combined"
go test ./...
rg -n "Gate A Request Approval Manifest|P5-F26|Batch 1Q|--approval-manifest|approved_request_id_manifest|unirouter.prd5.xinsight.approval.v1|61091c|PR #119|approved request IDs" scripts/prd5_xinsight_snapshot.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/CODEBASE-MAP.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-approved-request-ids-response.md"
git diff --name-only
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any real snapshot artifact, approved request ID invention, migration, runtime
  request-path change, API route, frontend behavior, secret, read-only DB/view
  credential, shared ECS staging, exporter/outbox, raw payload capture,
  X-Insight runtime dependency, or `Production verified` claim as a scope
  failure;
- final result for PRD5 Batch 1Q.
