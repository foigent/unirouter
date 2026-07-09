# VERIFY-RESULT.md - PRD5 Batch 1U Embeddings Model Permission Parity

**Verifier:** Canary
**Date:** 2026-07-10
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Result:** ✅ **PASS — 7/7**

## Verification Summary

| # | Instruction | Result | Evidence |
|---|-------------|--------|----------|
| 1 | `origin/main` contains Batch 1T as `fc62e65` / PR #123; prior VERIFY-RESULT records Batch 1T PASS 7/7 | ✅ PASS | `git log --oneline -5 origin/main` → HEAD `fc62e65 feat: PRD5 Batch 1T — Gate A exchange envelope and controlled run authorization (P5-F29) (#123)`; VERIFY-RESULT.md title: "PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization", result: "PASS — 7/7" |
| 2 | `modelAllowedForAPIKey` preserves prior Chat behavior: unrestricted, wildcard, resolved model, requested alias, rejection | ✅ PASS | `relay.go:261`: nil/empty → true; `*` → true; match resolved → true; match requested → true; no match → false. `TestModelAllowedForAPIKey`: 6 subtests (missing_key_context, empty_restriction, wildcard, resolved_model, requested_alias, not_allowed) all PASS |
| 3 | Chat and Embeddings both use shared predicate; Embeddings checks after model parsing but before token estimation/billing freeze/route lookup/provider key decryption/adapter selection/dispatch | ✅ PASS | `relay.go:324` Chat: `if !modelAllowedForAPIKey(callerAPIKey, req.Model, requestedModel)` before `estimateTokens`/`Freeze`; `relay.go:1150` Embeddings: `if !modelAllowedForAPIKey(callerAPIKey, req.Model, requestedModel)` before `tokenutil.EstimateTokens`/`Freeze`/route/provider. Both call `rejectModelNotAllowed` on failure |
| 4 | Unauthorized Embeddings returns 403 + `auth_error.model_not_allowed` + `X-Request-ID`; no billing Freeze; expected auth rejection trace fields | ✅ PASS | `TestHandleEmbeddings_ModelNotAllowed`: asserts `rec.Code == 403`, body contains `"code":"auth_error.model_not_allowed"`, `X-Request-ID == "req-embeddings-denied"`, `freezeCalled == false`, trace args: event_type=`auth.model_not_allowed`, stage=`auth`, source_component=`auth`, capture_gap=`permission_denied`, decision_kind=`auth`, decision_result=`rejected`, decision_reason=`auth_error.model_not_allowed` |
| 5 | Existing Chat model-denial and trace-decision tests still pass | ✅ PASS | `TestHandleChatCompletion_ModelNotAllowed` PASS; `TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision` PASS (verifies event_type, stage, capture_gap, decision_kind, decision_result, decision_reason, source_event_hash) |
| 6 | PRD5 docs route P5-F30 / Batch 1U, mark Batch 1T complete as `fc62e65` / PR #123, real Gate A execution explicitly awaiting User approval | ✅ PASS | All 12 docs + shared contract contain P5-F30 / Batch 1U references (see grep results); CURRENT-STATUS.md line 4 lists `fc62e65` / PR #123 for Batch 1T; CURRENT-STATUS.md line 49: "real controlled Gate A execution awaits explicit User approval"; shared contract: "requires explicit User approval"; Gate A recommendation: "awaiting explicit User approval", "Real controlled requests: not yet authorized or executed", "This file is a recommendation, not authorization" |
| 7 | No non-goal or PRD4 boundary regression | ✅ PASS | See Non-Goal and PRD4 Boundary Evidence below |

## Non-Goal and PRD4 Boundary Evidence

| Check | Evidence |
|-------|----------|
| No frontend/admin/billing/db/migrate changes | `git diff -- frontend internal/admin internal/billing internal/db internal/migrate` → empty (exit 0) |
| No PRD4 doc changes | `git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md` → empty (exit 0) |
| No whitespace errors | `git diff --check` → clean (exit 0) |
| Working tree: docs + gateway relay only | `git status --short` → 15 modified docs + 2 modified Go files (`relay.go`, `relay_mock_test.go`); no new routes, migrations, or frontend files |
| No route/migration/frontend/raw-payload/exporter/X-Insight-runtime/PRD4 change | Code changes limited to `internal/gateway/relay.go` (extract shared predicate + add Embeddings check + X-Request-ID header) and `relay_mock_test.go` (new tests); no billing amount/routing/provider success-path changes |

## Command Results

| Command | Result |
|---------|--------|
| `git log --oneline -5 origin/main` | ✅ `fc62e65` at HEAD for PRD5 Batch 1T / #123 |
| `go test ./internal/gateway -run 'TestModelAllowedForAPIKey|TestHandleChatCompletion_ModelNotAllowed|TestHandleEmbeddings_Model|TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision' -count=1 -v` | ✅ **8/8 PASS** (0.714s): TestModelAllowedForAPIKey (6 subtests), TestHandleEmbeddings_ModelNotAllowed, TestHandleEmbeddings_ModelAllowedByRequestedAlias, TestHandleChatCompletion_ModelNotAllowed, TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision |
| `go test ./internal/gateway ./internal/middleware ./cmd/uniroute -count=1` | ✅ All 3 packages PASS |
| `go test ./... -count=1` | ✅ All 23 packages PASS, 0 FAIL |
| `git diff -- frontend internal/admin internal/billing internal/db internal/migrate` | ✅ Empty (no non-goal code changes) |
| `git diff -- docs/PRD-Phase4.md ...` | ✅ Empty (no PRD4 changes) |
| `git status --short` | ✅ 15 modified docs + 2 modified Go files |
| `git diff --check` | ✅ Clean |

## New Discoveries

No new bugs or scope violations found.

## Final Result

**PRD5 Batch 1U Embeddings Model Permission Parity: ✅ PASS — 7/7**

All seven Canary verification instructions pass. The batch extracts `modelAllowedForAPIKey` as a shared Chat/Embeddings predicate, enforces `allowed_models` on Embeddings before billing freeze/route/provider dispatch, returns 403 + `X-Request-ID` + auth trace evidence on denial, and adds focused tests (helper, Chat regression, Embeddings pre-billing rejection, trace fields). No non-goal, PRD4 boundary regression, or scope violation is present.
