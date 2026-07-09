# FIX-SIGNAL.md - PRD5 Batch 1U Embeddings Model Permission Parity

**Date:** File timestamp is authoritative
**Risk level:** R3 authorization path
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1T passed Canary 7/7 and Sentinel merged it as `fc62e65` / PR
#123. PRD4 remains complete for the confirmed current scope.

Current code enforced `api_keys.allowed_models` on Chat but not on Embeddings.
An API key restricted to specific models could therefore call an unlisted
Embeddings model and reach billing, routing, and provider dispatch. This is an
authorization consistency defect, not only an observability gap.

The separate real Gate A controlled run is still not authorized. The shared
recommendation names ECS/Atlas/seven-day retention, but its status explicitly
remains awaiting User approval.

## Intended Changes

- Extract the existing Chat model permission semantics into
  `modelAllowedForAPIKey`.
- Preserve unrestricted, `*` wildcard, resolved-model, and requested-alias
  matching.
- Use the same predicate in Chat and Embeddings.
- Reject unauthorized Embeddings models with the existing HTTP 403
  `auth_error.model_not_allowed` contract before billing freeze, routing, or
  provider dispatch.
- Return `X-Request-ID` on Embeddings responses.
- Reuse the existing `auth.model_not_allowed` trace path so denial evidence has
  `source_component=auth`, `capture_gap=permission_denied`,
  `decision_kind=auth`, `decision_result=rejected`, and
  `decision_reason=auth_error.model_not_allowed`.
- Add focused helper, Chat regression, Embeddings response/pre-billing, and
  trace-field tests.
- Reconcile Batch 1T completion and route `P5-F30` / Batch 1U through PRD5
  status, matrix, plan, backlog, competitor comparison, field contract, handoff,
  indexes, and the shared X-Insight contract.

## Non-Goals

This round does not:

- add or change API routes;
- add a DB migration or frontend behavior;
- change billing amounts, freeze/settle semantics, routing selection, provider
  adapters, or allowed request success paths;
- create a call-log row for a pre-admission auth denial;
- store raw prompt/response payloads;
- add exporter/outbox, read-only DB/view, or shared ECS changes;
- add X-Insight as a runtime dependency;
- authorize or execute the real Gate A controlled run;
- deploy to ECS or create target-environment evidence;
- change PRD4 acceptance counts or create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `origin/main` contains Batch 1T as `fc62e65` / PR #123 and the prior
   `VERIFY-RESULT.md` records Batch 1T PASS 7/7.
2. Verify `modelAllowedForAPIKey` preserves the prior Chat behavior for:
   unrestricted keys, wildcard, resolved model, requested alias, and rejection.
3. Verify Chat and Embeddings both use the shared predicate, and Embeddings
   performs the check after model parsing but before token estimation/billing
   freeze, route lookup, provider key decryption, adapter selection, or dispatch.
4. Verify an unauthorized Embeddings request returns 403 with
   `auth_error.model_not_allowed` and `X-Request-ID`, does not call billing
   Freeze, and writes the expected auth rejection trace fields.
5. Verify existing Chat model-denial and trace-decision tests still pass.
6. Verify PRD5 docs and the shared contract route `P5-F30` / Batch 1U, mark
   Batch 1T complete as `fc62e65` / PR #123, and keep real Gate A execution
   explicitly awaiting User approval.
7. Verify no non-goal or PRD4 boundary regression, then run:

```bash
git log --oneline -3 origin/main | rg "fc62e65.*PRD5 Batch 1T|#123"
go test ./internal/gateway -run 'TestModelAllowedForAPIKey|TestHandleChatCompletion_ModelNotAllowed|TestHandleEmbeddings_Model|TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision' -count=1 -v
go test ./internal/gateway ./internal/middleware ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "P5-F30|Batch 1U|Embeddings Model Permission Parity|modelAllowedForAPIKey|rejectModelNotAllowed|TestHandleEmbeddings_ModelNotAllowed|auth.model_not_allowed|permission_denied|fc62e65|PR #123" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-COMPETITOR-COMPARISON.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-embeddings-model-permission-parity.md"
rg -n "awaiting explicit User approval|Real controlled requests: not yet authorized or executed|This file is a recommendation, not authorization" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-10-gate-a-controlled-run-authorization-recommendation.md"
git diff -- frontend internal/admin internal/billing internal/db internal/migrate
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git status --short
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- focused test evidence proving pre-billing rejection and exact trace fields;
- any billing/routing/provider success-path change, route/migration/frontend
  change, real Gate A execution, target claim, PRD4 regression, or other
  non-goal as a blocking failure;
- final result for PRD5 Batch 1U.
