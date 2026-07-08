# FIX-SIGNAL.md - PRD5 Batch 1H Gateway Span Hierarchy

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace evidence write input and source-event hash
change plus R2 acceptance-status documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD5 Batch 1F passed Canary 11/11 and Sentinel
merged it as `e9670ad` / PR #109. PRD5 Batch 1G passed Canary 11/11 and
Sentinel merged it as `c701843` / PR #110. PRD4 remains complete for the
confirmed current scope and must not be reopened by this round.

This Batch 1H slice implements `P5-F17` gateway span hierarchy:

- create per-request in-memory span state for UniRouter-emitted trace events;
- treat the first UniRouter trace event in a request as the gateway root span;
- preserve trusted inbound `parent_span_id` as the gateway root span parent when
  present;
- parent later UniRouter trace events in the same request to the gateway root
  span;
- preserve existing trusted/default `trace_id` behavior;
- include the resulting `parent_span_id` in trace source-event hashes.

## Intended Changes

Code:

- `internal/gateway/trace.go`
  - adds request-scoped span state helpers;
  - derives `parent_span_id` from trusted inbound parent for the first emitted
    UniRouter event;
  - parents later UniRouter events to the gateway root span.
- `internal/gateway/relay.go`
  - attaches span state to gateway request contexts together with trusted
    evidence context.
- `internal/gateway/relay_mock_test.go`
  - covers trusted context preservation;
  - covers gateway root span parenting across two trace events;
  - verifies request contexts carry span state.

Docs:

- PRD5 status docs mark Batch 1G Canary/Sentinel complete and add P5-F17 as
  Batch 1H fixed awaiting Canary.
- Field contract documents that Batch 1H is a UniRouter-local request hierarchy,
  not a full cross-system agent graph.

## Non-Goals

This round does not:

- add a DB migration;
- add a new API route;
- change frontend behavior;
- change routing, provider dispatch, billing, wallet, auth, admission, or
  response payload semantics;
- trust public inbound request headers by default;
- capture raw prompt/response payloads;
- add exporter/outbox;
- create read-only DB credentials or shared ECS staging;
- add X-Insight as a runtime dependency;
- fabricate cross-system agent/tool/business-chain facts;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1G PASS
   11/11 and that repo history includes Sentinel merge `c701843` / PR #110.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F16 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F17 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F17 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1H and lists Batch 1H non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents Batch 1H
   as UniRouter-local request span hierarchy, not a full cross-system graph.
6. Verify `internal/gateway/relay.go` attaches evidence span state to request
   contexts.
7. Verify `internal/gateway/trace.go` uses the first UniRouter trace event as
   gateway root, preserves a trusted inbound parent for that root, and parents
   later UniRouter trace events to the gateway root.
8. Verify trace source-event hashes include the resulting `parent_span_id`.
9. Verify no migration, new route, frontend behavior, exporter/outbox,
   read-only credential, raw payload capture, X-Insight runtime dependency,
   public-header authority by default, fabricated cross-system chain, or PRD4
   matrix change is included.
10. Run:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(SanitizesMetadataAndSnapshotsOwner|UsesTrustedEvidenceContext|ParentsEventsToGatewayRootSpan)|TestRelayContextWithTrustedEvidence' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Gateway Span Hierarchy|P5-F17|Batch 1H|parent_span_id|contextWithEvidenceSpanState|parentSpanForEvent|c701843|PR #110" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

## Expected Output

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any public-header evidence trust by default as a blocking failure;
- any new raw payload capture as a blocking failure;
- any fabricated cross-system agent/tool/business-chain fact as a blocking
  failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- final result for PRD5 Batch 1H.
