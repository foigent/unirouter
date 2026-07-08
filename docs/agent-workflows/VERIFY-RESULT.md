# VERIFY-RESULT.md - PRD5 Batch 1I Payload Policy Decision Evidence

**Verifier:** Canary
**Date:** 2026-07-09
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Result:** ✅ **PASS — 11/11**

## Verification Summary

| # | Instruction | Result | Evidence |
|---|-------------|--------|----------|
| 1 | VERIFY-RESULT.md records Batch 1H PASS 10/10; repo includes `61f5610` / PR #111 | ✅ PASS | Previous VERIFY-RESULT.md records 10/10; `PRD5-ACCEPTANCE-MATRIX.md` references `61f5610` / PR #111 |
| 2 | Matrix: P5-F01–F07 + F13–F17 Canary passed; F08–F11 deferred; F12 planned; P5-F18 Fixed awaiting Canary | ✅ PASS | `PRD5-ACCEPTANCE-MATRIX.md` — P5-F18 = "Fixed awaiting Canary"; other rows unchanged |
| 3 | PRD-Phase5 defines P5-F18; does not reopen PRD4 | ✅ PASS | `PRD-Phase5.md:405` defines P5-F18; PRD4 matrix unchanged (13 rows) |
| 4 | Implementation plan: UniRouter can proceed independently through Batch 1I; non-goals listed | ✅ PASS | `PRD5-IMPLEMENTATION-PLAN.md` — "no blocking condition through Batch 1I"; Batch 1I non-goals: no raw capture, no hash, no reference, no export |
| 5 | Evidence field contract documents Batch 1I as policy-decision only, no raw payload storage/export | ✅ PASS | `26-prd5-evidence-field-contract.md` — Batch 1I records policy decision only; no raw payload storage/export |
| 6 | Shared snapshot contract exists | ✅ PASS | `contracts/2026-07-09-unirouter-prd5-payload-policy-decision-evidence.md` exists (2818 bytes) |
| 7 | `traceutil/metadata.go` allowlists only safe payload policy keys; no raw prompt/response | ✅ PASS | `metadata.go:16-20` — allowlists `payload_capture_mode`, `payload_policy_reason`, `payload_ref_present`, `raw_payload_stored` only; no prompt/response keys |
| 8 | `relay.go` emits `payload.policy.applied` for chat and embeddings after `gateway.request.accepted` | ✅ PASS | `trace.go:179` — `recordPayloadPolicyDecision` called with `payload.policy.applied` event; relay_mock_test confirms event ordering |
| 9 | `trace.go` maps `payload.policy.applied` to `decision_kind=payload_capture`; `metadata_only`→`allowed`, `disabled`→`skipped` | ✅ PASS | `trace.go:268-272` — `payload.policy.applied` → `payload_capture`; disabled → `skipped`; else → `allowed`; test confirms both modes |
| 10 | No migration, new route, frontend, exporter, read-only credential, raw payload, X-Insight dependency, public-header authority, fabricated chain, or PRD4 change | ✅ PASS | This batch's diff: `metadata.go`, `metadata_test.go`, `trace.go`, `relay.go`, `relay_mock_test.go` + docs only |
| 11 | All commands pass | ✅ PASS | See below |

## Command Results

| Command | Result |
|---------|--------|
| `go test ... -run 'TestSanitizeMetadata_PayloadPolicyFields\|TestRecordPayloadPolicyDecision_...\|TestRecordTraceEvent_ParentsEvents...'` | ✅ 4/4 PASS |
| `go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1` | ✅ 3 packages PASS |
| `go test ./... -count=1` | ✅ All packages PASS |
| `grep` for key terms | ✅ All expected references found |
| `git diff --check` | ✅ Clean |

## PRD4 Boundary

- PRD4 matrix: 13 rows — **unchanged** ✅
- No PRD4 row promoted to `Production verified` ✅

## Security Check

- No raw prompt/response capture ✅
- No payload hash/reference/export implementation ✅
- `raw_payload_stored=false` and `payload_ref_present=false` hardcoded ✅
- Only safe scalar metadata keys allowlisted ✅

## Notes

- `payload.policy.applied` 事件在 `gateway.request.accepted` 之后、billing freeze 之前发出。
- `decision_kind=payload_capture` 映射清晰：`metadata_only` → `allowed`，`disabled` → `skipped`。
- 测试验证了 `raw_payload_stored=false` 和 `payload_ref_present=false` 始终为 false。
- 共享合同文件描述了 UniRouter/X-Insight handoff boundary。

## Conclusion

**PRD5 Batch 1I: PASS 11/11.** PRD4 边界完整，payload policy 决策仅记录策略状态（不存储 raw payload），metadata allowlist 安全，决策映射正确。可以进入 Sentinel commit 流程。
