# FIX-SIGNAL.md - PRD5 Batch 1M Provider Failure Capture Gaps

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace evidence capture-gap/source-event hash change
plus R2 acceptance-status documentation update
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
passed Canary 10/10 and Sentinel merged it as `ffebf83` / PR #115.

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

X-Insight Gate A importer evidence is still absent from the shared folder, so
read-only DB/view access, shared ECS staging, and exporter/outbox design remain
gated. Batch 1M continues UniRouter-owned evidence readiness by refining
provider failure `capture_gap` semantics. It does not require X-Insight at
runtime.

## Intended Changes

Code:

- `internal/gateway/trace.go`
  - keeps `capture_gap=provider_not_returned` for provider response accepted or
    rejected events when a response exists but no sanitized upstream request ID
    is available;
  - writes `capture_gap=not_available_from_source` for provider-stage failure
    events that occur before an upstream provider response source exists;
  - preserves `capture_gap=redacted` priority when metadata sanitization already
    recorded a redaction gap.
- `internal/gateway/relay.go`
  - passes available embeddings upstream request IDs into
    `provider.response_parse_failed` trace evidence.
- `internal/gateway/relay_mock_test.go`
  - adds focused coverage for provider failure capture gaps and redaction
    priority.

Docs and shared folder:

- PRD5 status docs mark Batch 1L Canary/Sentinel complete and add P5-F22 as
  Batch 1M fixed awaiting Canary.
- `docs/tech-ref/26-prd5-evidence-field-contract.md` documents the distinction
  between `provider_not_returned` and `not_available_from_source`.
- Shared-folder contract:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-provider-failure-capture-gaps.md`

## Non-Goals

This round does not:

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
- infer cross-system agent/tool/business-system graph nodes;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1L PASS
   10/10 and that repo history includes Sentinel merge `ffebf83` / PR #115.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F21 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F22 as
   `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F22 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1M and lists Batch 1M non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents
   provider failure capture gaps and the `provider_not_returned` /
   `not_available_from_source` distinction.
6. Verify the shared contract file exists and describes the UniRouter/X-Insight
   provider failure capture-gap boundary.
7. Verify `internal/gateway/trace.go` maps provider response missing IDs to
   `provider_not_returned` and provider pre-response failure missing IDs to
   `not_available_from_source`.
8. Verify redaction gaps keep priority over provider failure missing-source
   gaps, and available upstream request IDs are preserved for response parse
   failures.
9. Verify no migration, new route, frontend behavior, exporter/outbox,
   read-only credential/view, raw payload capture, X-Insight runtime dependency,
   public-header authority by default, provider dispatch semantic change, or
   PRD4 matrix change is included.
10. Run:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(ProviderFailureMissingUpstreamRequestIDCaptureGap|ProviderFailureRedactionGapTakesPriority|ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority)' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Provider Failure Capture Gaps|P5-F22|Batch 1M|not_available_from_source|providerMissingUpstreamCaptureGap|ProviderFailureMissingUpstreamRequestIDCaptureGap|ProviderFailureRedactionGapTakesPriority|ffebf83|PR #115" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-provider-failure-capture-gaps.md"
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
- any provider dispatch semantic change as a scope failure;
- final result for PRD5 Batch 1M.
