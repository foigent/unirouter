# FIX-SIGNAL.md - PRD5 Batch 1P Model Permission Decision Evidence

**Date:** File timestamp is authoritative
**Risk level:** R3 auth-denial request path plus gateway trace evidence
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

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

X-Insight Gate A remains gated on approved request IDs, scenario labels, source
data access, and import/read evidence. Batch 1P does not depend on X-Insight
runtime work; it closes a UniRouter-owned evidence gap on an existing auth-denial
path.

## Intended Changes

Runtime evidence:

- Existing chat `allowed_models` rejection now records an `auth.model_not_allowed`
  trace event before returning the existing 403 response.
- The event writes `source_component=auth`, `capture_gap=permission_denied`,
  `decision_kind=auth`, `decision_result=rejected`, and
  `decision_reason=auth_error.model_not_allowed`.
- The denial still returns before billing freeze, routing, or provider dispatch.

Tests and docs:

- Add focused gateway coverage for direct trace insertion and the chat handler
  denial path.
- Add a shared-folder contract note for X-Insight:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-model-permission-decision-evidence.md`
- Reconcile Batch 1O completion as `b2eea2e` / PR #118.
- Add P5-F25 / Batch 1P planning, matrix, field-contract, code-map, status, and
  handoff docs.

## Non-Goals

This round does not:

- add a DB migration;
- add a new API route;
- change frontend behavior;
- create read-only DB credentials or views;
- create shared ECS staging;
- add exporter/outbox;
- store, hash, reference, or export raw prompt/response payloads;
- add X-Insight as a runtime dependency;
- change Embeddings authorization behavior;
- change successful request handling, billing, routing, or provider dispatch;
- add `decision_not_emitted` backfill or override trace sampling behavior;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify repo history includes `b2eea2e` / PR #118 for PRD5 Batch 1O and that
   `docs/agent-workflows/VERIFY-RESULT.md` records Batch 1O PASS 8/8.
2. Verify the only runtime behavior change is the existing chat model-permission
   denial trace event. The HTTP response remains 403
   `auth_error.model_not_allowed`, and denial still happens before billing,
   routing, or provider dispatch.
3. Verify trace evidence for the denial uses:
   `event_type=auth.model_not_allowed`, `source_component=auth`,
   `capture_gap=permission_denied`, `decision_kind=auth`,
   `decision_result=rejected`, and
   `decision_reason=auth_error.model_not_allowed`.
4. Verify Embeddings authorization behavior is unchanged in this batch.
5. Verify no migration, new route, frontend behavior, real snapshot artifact,
   read-only DB/view credential, shared ECS staging, exporter/outbox, raw payload
   capture, X-Insight runtime dependency, PRD4 matrix change, or
   `Production verified` claim is included.
6. Run:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision|TestHandleChatCompletion_ModelNotAllowed|TestRecordTraceEvent_ProviderResponseMissingUpstreamRequestIDCaptureGap|TestRecordTraceEvent_ProviderFailureMissingUpstreamRequestIDCaptureGap|TestRecordPayloadPolicyDecision'
go test ./internal/traceutil
go test ./...
rg -n "auth.model_not_allowed|permission_denied|decision_kind=auth|auth_error.model_not_allowed|Model Permission Decision Evidence|P5-F25|Batch 1P|b2eea2e|PR #118|Embeddings authorization behavior change" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/CODEBASE-MAP.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-model-permission-decision-evidence.md"
git diff --name-only
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any migration, API route, frontend, real snapshot artifact, secret, read-only
  DB/view credential, shared ECS staging, exporter/outbox, raw payload capture,
  X-Insight runtime dependency, Embeddings authorization behavior change, or
  `Production verified` claim as a scope failure;
- final result for PRD5 Batch 1P.
