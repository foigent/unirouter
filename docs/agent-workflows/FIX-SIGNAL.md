# FIX-SIGNAL.md - PRD5 X-Insight Gate A Coordination

**Date:** 2026-07-08
**Risk level:** R2 acceptance-sensitive documentation change
**Status:** awaiting Canary verification

## Context

PRD4 is complete for the confirmed current scope. This signal must not reopen
PRD4, change PRD4 acceptance counts, or promote any PRD4 row to
`Production verified`.

This round records User-confirmed PRD5 coordination decisions for X-Insight Gate
A:

- X-Insight should use a sanitized controlled snapshot first.
- Restricted read-only DB or read-only views can be considered only after local
  snapshot import works.
- Do not create a shared private GitHub organization strategy now.
- Do not create or update shared ECS staging before local Gate A import works
  and CI is green.
- UniRouter PRD5 should prioritize structural evidence fields before native
  exporter/outbox.

## Intended Changes

UniRouter repo:

- Add `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`.
- Update `docs/planning/PRD5-BACKLOG.md` to link the coordination document.
- Update `docs/DOC-INVENTORY.md` and `docs/README.md` so the coordination
  document is discoverable.

Shared folder:

- Add
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-08-unirouter-gate-a-environment-and-prd5-coordination-response.md`.
- Update
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-08-prd5-structure-fields-before-exporter-proposed.md`
  from proposed to accepted planning decision.

## Non-Goals

This round does not:

- modify Go, frontend, migration, OpenAPI, API route, runtime behavior,
  deployment scripts, Docker, or monitoring config;
- expose or write any secret, DSN, token, credential, raw prompt, raw response,
  or customer data;
- generate the sanitized snapshot yet;
- create read-only database credentials or views;
- create a shared GitHub organization strategy;
- deploy or update shared ECS staging;
- implement X-Insight importer;
- implement UniRouter exporter/outbox;
- change PRD4 acceptance matrix counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Re-read:
   - `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`
   - `docs/planning/PRD5-BACKLOG.md`
   - `docs/DOC-INVENTORY.md`
   - `docs/README.md`
2. Verify PRD4 remains closed for the confirmed current scope and is not
   reopened by this Gate A coordination.
3. Verify `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md` records:
   - sanitized controlled snapshot first;
   - restricted read-only DB/view only after local snapshot import works;
   - no shared GitHub organization strategy now;
   - no shared ECS staging before local Gate A import and CI pass;
   - structural fields before exporter/outbox.
4. Verify `docs/planning/PRD5-BACKLOG.md`, `docs/DOC-INVENTORY.md`, and
   `docs/README.md` link or mention the new coordination document.
5. Verify the shared response file exists and answers the X-Insight request
   without containing secrets or DSNs:
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-08-unirouter-gate-a-environment-and-prd5-coordination-response.md`
6. Verify the shared decision file is now `Decision accepted` and records the
   User-confirmed access/environment conditions:
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-08-prd5-structure-fields-before-exporter-proposed.md`
7. Verify no runtime/code/deployment file changed in this round.
8. Run:

```bash
git diff --check -- docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/planning/PRD5-BACKLOG.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md
```

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered verification instruction;
- any PRD4 boundary regression as a blocking failure;
- any secret/DSN/raw-payload leakage as a blocking failure;
- any shared-folder inconsistency as a documentation/workspace finding;
- a note that no runtime code validation was required for this docs-only R2
  coordination change.
