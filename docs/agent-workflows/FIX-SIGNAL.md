# FIX-SIGNAL.md - PRD5 Batch 1J Provider Upstream Capture Gap

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace evidence write/source-event hash change plus
R2 acceptance-status documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD5 Batch 1F passed Canary 11/11 and Sentinel
merged it as `e9670ad` / PR #109. PRD5 Batch 1G passed Canary 11/11 and
Sentinel merged it as `c701843` / PR #110. PRD5 Batch 1H passed Canary 10/10
and Sentinel merged it as `61f5610` / PR #111. PRD5 Batch 1I passed Canary
11/11 and Sentinel merged it as `1dc20b5` / PR #112. PRD4 remains complete for
the confirmed current scope and must not be reopened by this round.

X-Insight Gate A importer evidence is still absent from the shared folder, so
read-only DB/view and exporter/outbox remain gated. This Batch 1J slice
continues UniRouter-owned evidence readiness by implementing `P5-F19` provider
upstream capture-gap evidence.

## Intended Changes

Code:

- `internal/gateway/trace.go`
  - marks provider response trace events with
    `capture_gap=provider_not_returned` when no sanitized
    `upstream_request_id` is available;
  - applies the gap only to `provider.response_accepted` and
    `provider.response_rejected`;
  - preserves returned upstream request IDs and existing redaction gaps.
- `internal/gateway/relay_mock_test.go`
  - verifies provider response events without upstream request IDs write the new
    capture gap and keep `upstream_request_id` null.

Docs and shared folder:

- PRD5 status docs mark Batch 1I Canary/Sentinel complete and add P5-F19 as
  Batch 1J fixed awaiting Canary.
- `docs/tech-ref/26-prd5-evidence-field-contract.md` records the
  `provider_not_returned` gap semantics.
- Shared-folder contract:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-provider-upstream-capture-gap.md`

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
- create read-only DB credentials or shared ECS staging;
- add X-Insight as a runtime dependency;
- fabricate cross-system agent/tool/business-chain facts;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1I PASS
   11/11 and that repo history includes Sentinel merge `1dc20b5` / PR #112.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F18 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F19 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F19 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1J and lists Batch 1J non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents
   `provider_not_returned` as a provider response capture gap.
6. Verify the shared contract file exists and describes the UniRouter/X-Insight
   import boundary for this gap.
7. Verify `internal/gateway/trace.go` applies `provider_not_returned` only to
   `provider.response_accepted` and `provider.response_rejected` when
   `upstream_request_id` is empty.
8. Verify returned upstream request IDs still populate `upstream_request_id`.
9. Verify existing metadata redaction gaps still keep `capture_gap=redacted`
   instead of being overwritten.
10. Verify no migration, new route, frontend behavior, exporter/outbox,
    read-only credential, raw payload capture, X-Insight runtime dependency,
    public-header authority by default, provider semantic change, fabricated
    cross-system chain, or PRD4 matrix change is included.
11. Run:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority|SanitizesMetadataAndSnapshotsOwner)|TestUpstreamRequestIDFromHeader' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Provider Upstream Capture Gap|P5-F19|Batch 1J|provider_not_returned|isProviderResponseEvent|ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority|1dc20b5|PR #112" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-provider-upstream-capture-gap.md"
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
- final result for PRD5 Batch 1J.
