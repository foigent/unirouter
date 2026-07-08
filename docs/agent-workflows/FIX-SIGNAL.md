# FIX-SIGNAL.md - PRD5 Batch 1K Gate A Snapshot Handoff Note

**Date:** File timestamp is authoritative
**Risk level:** R2 PRD5 evidence tooling and acceptance-status documentation
update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD5 Batch 1F passed Canary 11/11 and Sentinel
merged it as `e9670ad` / PR #109. PRD5 Batch 1G passed Canary 11/11 and
Sentinel merged it as `c701843` / PR #110. PRD5 Batch 1H passed Canary 10/10
and Sentinel merged it as `61f5610` / PR #111. PRD5 Batch 1I passed Canary
11/11 and Sentinel merged it as `1dc20b5` / PR #112. PRD5 Batch 1J provider
upstream capture-gap evidence passed Canary and Sentinel merged it as `f5223df`
/ PR #113. PRD4 remains complete for the confirmed current scope and must not
be reopened by this round.

This Batch 1K slice is independent of 1J runtime evidence writes: it only
improves the X-Insight Gate A sanitized snapshot handoff process after
X-Insight reported that its discovery command is ready for a controlled
snapshot.

X-Insight Gate A importer evidence is still absent from the shared folder.
Read-only DB/view access, shared ECS staging, and exporter/outbox design remain
gated. Batch 1K lets UniRouter generate a DSN-free Markdown handoff note beside
an explicit-request snapshot when approved request IDs and a read-only DSN are
available.

## Intended Changes

Code:

- `scripts/prd5_xinsight_snapshot.go`
  - adds `--handoff-note <path>`;
  - writes a Markdown handoff note only after the selected snapshot loads and
    JSON output succeeds;
  - includes generated date, source instance, schema version, selected request
    IDs, scenario labels, counts, safety confirmation, snapshot output path, and
    command shape without DSN;
  - rejects `--handoff-note -` so JSON and Markdown are not interleaved on
    stdout;
  - reuses `--overwrite` semantics for the note path.

Docs and shared folder:

- PRD5 status docs add P5-F20 / Batch 1K as fixed awaiting Canary.
- `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md` documents
  `--handoff-note` and the no-DSN/no-secret note rule.
- Shared-folder contract:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-gate-a-snapshot-handoff-note.md`

## Non-Goals

This round does not:

- generate a real snapshot artifact without approved request IDs and a
  read-only DSN;
- write DSNs, credentials, tokens, raw prompts, raw responses, raw IP addresses,
  or raw upstream error text to any generated note;
- add a DB migration;
- add a new API route;
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
- fabricate cross-system agent/tool/business-chain facts;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1I PASS
   11/11 and that repo history includes Sentinel merges `1dc20b5` / PR #112 and
   `f5223df` / PR #113.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F18 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, marks P5-F19 as `Canary passed`, and marks
   P5-F20 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F20 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1K and lists Batch 1K non-goals.
5. Verify `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md` documents
   `--handoff-note`, handoff-note contents, and the no-DSN/no-secret rule.
6. Verify the shared contract file exists and describes the UniRouter/X-Insight
   handoff-note boundary.
7. Verify `scripts/prd5_xinsight_snapshot.go` writes the handoff note only after
   snapshot JSON write succeeds and does not include the DSN value.
8. Verify `--handoff-note -` is rejected before any DSN is required.
9. Verify no real snapshot artifact, DSN, credential, token, raw payload, raw IP,
   raw upstream error, migration, new route, frontend behavior, exporter/outbox,
   read-only credential/view, shared ECS staging, X-Insight runtime dependency,
   or PRD4 matrix change is included.
10. Run:

```bash
go run scripts/prd5_xinsight_snapshot.go -help
go run scripts/prd5_xinsight_snapshot.go --request-id req-example --handoff-note - 2>&1 | rg -- "--handoff-note must be a Markdown file path"
go test ./... -count=1
rg -n "Gate A Snapshot Handoff Note|P5-F20|Batch 1K|--handoff-note|writeHandoffNote|formatHandoffNote|Command Shape Without DSN|DSN, credentials, tokens|snapshot handoff-note" scripts/prd5_xinsight_snapshot.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-gate-a-snapshot-handoff-note.md"
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any public-header evidence trust by default as a blocking failure;
- any new raw payload capture as a blocking failure;
- any payload hash/reference/export implementation as a scope failure;
- any DSN/credential/token/raw prompt/raw response/raw IP/raw upstream error
  written into handoff docs as a blocking failure;
- any fabricated cross-system agent/tool/business-chain fact as a blocking
  failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- final result for PRD5 Batch 1K.
