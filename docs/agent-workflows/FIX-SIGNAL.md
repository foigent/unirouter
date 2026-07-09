# FIX-SIGNAL.md - PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization

**Date:** File timestamp is authoritative
**Risk level:** R2 overall; R1 read-only snapshot tooling/tests plus PRD/status and human authorization contracts
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1S passed Canary 6/6 and Sentinel merged it as `c6ae2a7` / PR
#122. PRD4 remains complete for the confirmed current scope and must not be
reopened by this round.

X-Insight accepted the Batch 1S product boundary and implemented
`xinsight.import-receipt.v1`. It requested two source-side envelope fields and
a less technical approval path for fresh controlled requests:

- `exchange_id` for one outbound exchange;
- `approval_manifest_sha256` for the exact manifest used;
- prior authorization of scenario set, data classes, source instance, safety,
  retention, and operator before a narrow run creates request IDs.

X-Insight currently has no durable `import_job_id` or deep link. This batch does
not invent either field and does not implement the future UI.

## Intended Changes

- Add `exchange_id=uxchg_<32-lowercase-hex>` to generated snapshots and handoff
  notes using 128 bits of local cryptographic randomness.
- Add `approval_manifest_sha256=sha256:<64-lowercase-hex>` for manifest-selected
  snapshots, calculated from the exact manifest file bytes.
- Keep the snapshot artifact's own SHA-256 outside the snapshot; X-Insight
  computes it and reconciles `(exchange_id, artifact.sha256)`.
- Preserve `--approval-manifest` in generated handoff commands.
- Require manifest `source_instance` and reject a conflicting CLI override
  before DSN access.
- Reject approval manifests containing a second JSON value.
- Add focused explicit-file Go tests for exchange ID format/uniqueness,
  exact-byte manifest hashing, trailing JSON rejection, and handoff output.
- Add `docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md` and shared-folder
  exchange contract, authorization worksheet, and UniRouter response.
- Reconcile Batch 1S completion and route `P5-F29` / Batch 1T through current
  PRD5 status, matrix, plan, backlog, handoff, tech ref, and indexes.

## Non-Goals

This round does not:

- send a real controlled request;
- generate a real snapshot, approved-ID manifest, handoff artifact, or receipt;
- invent or approve request IDs;
- write a DSN, password, token, API key, credential, raw prompt, raw response,
  raw IP, or raw upstream error into the repo or shared folder;
- implement UI code, API routes, DB migrations, or runtime request-path changes;
- create read-only DB credentials/views or shared ECS staging;
- add exporter/outbox or an X-Insight runtime dependency;
- claim that Gate A, readback, production, `import_job_id`, or a deep link is
  complete;
- change PRD4 acceptance counts or create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `origin/main` contains Batch 1S as `c6ae2a7` / PR #122 and the prior
   `VERIFY-RESULT.md` records Batch 1S PASS 6/6.
2. Verify snapshot tooling:
   - generates a non-empty `exchange_id` matching
     `uxchg_<32-lowercase-hex>`;
   - hashes exact approval-manifest file bytes with the `sha256:` prefix;
   - emits the manifest hash only in manifest mode;
   - requires manifest source instance and rejects conflicting CLI override;
   - rejects a second JSON value in the manifest;
   - keeps `--approval-manifest` in the generated handoff command;
   - does not put the snapshot artifact's own hash inside the snapshot.
3. Verify the focused tests cover ID format/uniqueness, exact-byte hash, trailing
   JSON, and manifest-mode handoff behavior.
4. Verify the controlled-run runbook and shared worksheet require prior approval
   of environment, operator, and retention; use only the two approved synthetic
   scenarios and unique `X-Request-ID` values; and do not authorize broad
   discovery or secrets/raw payload handoff.
5. Verify the shared exchange contract and UniRouter response match X-Insight's
   current receipt contract: `validated` / `persisted` / `rejected`, no current
   durable `import_job_id`, no current deep link, and reconciliation by
   `(exchange_id, artifact.sha256)`.
6. Verify PRD5 status/matrix/plan/backlog/handoff/tech-ref/index docs route
   `P5-F29` / Batch 1T and mark Batch 1S complete as `c6ae2a7` / PR #122.
7. Verify no non-goal or PRD4 boundary regression is present, then run:

```bash
git log --oneline -3 origin/main | rg "c6ae2a7.*PRD5 Batch 1S|#122"
go test scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go -count=1
go run scripts/prd5_xinsight_snapshot.go -help
rg -n "P5-F29|Batch 1T|exchange_id|approval_manifest_sha256|xinsight.import-receipt.v1|controlled run|Controlled Run|X-Request-ID|artifact.sha256|seven days|7 天" scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-gate-a-exchange-envelope.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-controlled-run-authorization.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-exchange-envelope-and-controlled-run-response.md"
go test ./... -count=1
git diff -- frontend internal cmd migrations
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git status --short
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- exact command evidence for the focused and full Go tests;
- any real request/artifact, approved ID invention, secret/raw payload, UI/API/
  migration/runtime change, read-only DB/view, shared ECS, exporter/outbox,
  X-Insight runtime dependency, PRD4 change, or overclaim as a blocking scope
  failure;
- final result for PRD5 Batch 1T.
