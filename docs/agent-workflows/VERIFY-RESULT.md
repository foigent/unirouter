# VERIFY-RESULT.md - PRD5 Batch 1M Provider Failure Capture Gaps

**Verifier:** Canary
**Date:** 2026-07-09
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Result:** ✅ **PASS — 10/10**

## Verification Summary

| # | Instruction | Result | Evidence |
|---|-------------|--------|----------|
| 1 | VERIFY-RESULT.md records Batch 1L PASS 10/10; repo includes `ffebf83` / PR #115 | ✅ PASS | Previous VERIFY-RESULT.md records Batch 1L PASS 10/10; `git log` shows `ffebf83 feat: PRD5 Batch 1L — component span refinement (P5-F21) (#115)` |
| 2 | Matrix: P5-F01–F07 + F13–F21 Canary passed; F08–F11 deferred; F12 planned; P5-F22 Fixed awaiting Canary | ✅ PASS | `PRD5-ACCEPTANCE-MATRIX.md:61` — P5-F22 = "Fixed awaiting Canary"; P5-F01–F07, F13–F21 = "Canary passed"; F08–F11 = "Deferred pending prerequisite"; F12 = "Planned" |
| 3 | PRD-Phase5 defines P5-F22; does not reopen PRD4 | ✅ PASS | `PRD-Phase5.md:514` defines P5-F22 "Provider Failure Capture Gaps"; PRD4 matrix unchanged (13 rows: 11 Canary passed, 2 Deferred to PRD5) |
| 4 | Implementation plan: UniRouter can proceed independently through Batch 1M; non-goals listed | ✅ PASS | `PRD5-IMPLEMENTATION-PLAN.md:487` — "no blocking condition for UniRouter Batch 1A through Batch 1M"; Batch 1M non-goals listed at `:395` |
| 5 | Evidence field contract documents provider failure capture gaps and `provider_not_returned` / `not_available_from_source` distinction | ✅ PASS | `26-prd5-evidence-field-contract.md:207` — "Batch 1M provider failure capture gaps"; `:213` — `not_available_from_source` for pre-response failures; `:86` — enum value listed |
| 6 | Shared contract file exists and describes UniRouter/X-Insight provider failure capture-gap boundary | ✅ PASS | `contracts/2026-07-09-unirouter-prd5-provider-failure-capture-gaps.md` exists (2296 bytes); describes `provider_not_returned` vs `not_available_from_source` writer rules and X-Insight import guidance |
| 7 | `trace.go` maps provider response missing IDs to `provider_not_returned` and provider pre-response failure missing IDs to `not_available_from_source` | ✅ PASS | `trace.go:326` — `providerMissingUpstreamCaptureGap` returns `evidenceCaptureGapProvider` for `response_accepted`/`response_rejected` and `evidenceCaptureGapSource` for `key_decrypt_failed`/`adapter_unknown`/`request_build_failed`/`request_failed`/`response_parse_failed` |
| 8 | Redaction gaps keep priority; available upstream request IDs preserved for response parse failures | ✅ PASS | `trace.go:154-158` — gap assigned only when `captureGap == ""` (redacted takes priority); `relay.go:1288` — embeddings `response_parse_failed` passes `upstreamRequestID` |
| 9 | No migration, new route, frontend, exporter/outbox, read-only credential/view, raw payload, X-Insight dependency, public-header authority, provider dispatch semantic change, or PRD4 matrix change | ✅ PASS | Diff: `trace.go` (constant + function rename/expand), `relay.go` (+1 arg to existing call), `relay_mock_test.go` (+2 tests) + docs only. No PRD4 row changes, no new migration, no new route |
| 10 | All commands pass | ✅ PASS | See below |

## Command Results

| Command | Result |
|---------|--------|
| `go test ./internal/gateway -run 'TestRecordTraceEvent_(ProviderFailureMissingUpstreamRequestIDCaptureGap\|ProviderFailureRedactionGapTakesPriority\|ProviderResponseMissingUpstreamRequestIDCaptureGap\|ProviderResponseRedactionGapTakesPriority)' -count=1` | ✅ 4/4 PASS |
| `go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1` | ✅ 3 packages PASS |
| `go test ./... -count=1` | ✅ All 22 packages PASS (0 FAIL) |
| `grep` for key terms | ✅ All expected references found across code, docs, and shared contract |
| `git diff --check` | ✅ Clean (exit 0) |

## PRD4 Boundary

- PRD4 matrix: 13 rows (11 Canary passed, 2 Deferred to PRD5) — **unchanged** ✅
- No PRD4 row promoted to `Production verified` ✅

## Security Check

- No cross-system graph inference ✅
- No public-header authority by default ✅
- No raw payload capture ✅
- No X-Insight runtime dependency ✅
- No provider dispatch semantic change ✅

## Code Diff Summary

| File | Change |
|------|--------|
| `trace.go` | Added `evidenceCaptureGapSource` constant; replaced `isProviderResponseEvent` with `providerMissingUpstreamCaptureGap` (returns `provider_not_returned` for response events, `not_available_from_source` for pre-response failures, empty for non-provider events); broadened gap assignment from response-only to all provider events |
| `relay.go` | Embeddings `provider.response_parse_failed` now passes `upstreamRequestID` so parse failures with a known upstream ID do not falsely get `not_available_from_source` |
| `relay_mock_test.go` | +2 tests: `ProviderFailureMissingUpstreamRequestIDCaptureGap` (verifies `not_available_from_source` on `request_failed`) and `ProviderFailureRedactionGapTakesPriority` (verifies `redacted` wins over `not_available_from_source` on `request_build_failed` with sensitive metadata) |

## Notes

- `providerMissingUpstreamCaptureGap` 函数统一处理所有 provider 阶段的 capture gap 逻辑，比之前的 `isProviderResponseEvent` 更具扩展性。
- 响应解析失败路径（embeddings）现在正确传递 `upstreamRequestID`，避免在 provider 已返回 ID 时错误标记 `not_available_from_source`。
- 红色化优先级通过 `captureGap == ""` 条件保证：只有在 sanitizer 未标记 `redacted` 时才分配 provider 缺失 ID 的 gap。
- Batch 1M 代码变更尚未 commit（工作树中），等待 Sentinel 执行 commit/push/PR。

## Conclusion

**PRD5 Batch 1M: PASS 10/10.** PRD4 边界完整，provider failure capture gap 语义正确（response 事件用 `provider_not_returned`，pre-response 失败用 `not_available_from_source`，redacted 保持优先），embeddings parse failure 路径正确传递 upstream ID，无越界变更。可以进入 Sentinel commit 流程。
