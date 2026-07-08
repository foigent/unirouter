# FIX-SIGNAL.md - PRD5 Batch 1L Component Span Refinement

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace evidence parent-span/source-event hash change
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
Canary 10/10 and Sentinel merged it as `2db830a` / PR #114.

PRD4 remains complete for the confirmed current scope and must not be reopened
by this round.

X-Insight Gate A importer evidence is still absent from the shared folder, so
read-only DB/view access, shared ECS staging, and exporter/outbox design remain
gated. Batch 1L continues UniRouter-owned evidence readiness by improving the
request-local trace hierarchy. It does not require X-Insight at runtime.

## Intended Changes

Code:

- `internal/gateway/trace.go`
  - keeps the first UniRouter-emitted trace event as the request gateway root;
  - preserves trusted inbound `parent_span_id` only as the gateway root parent;
  - tracks the first span for each `source_component` in the request;
  - parents repeated same-component events to that component's first span;
  - keeps first events for new components parented to the gateway root.
- `internal/gateway/relay_mock_test.go`
  - extends gateway span hierarchy coverage to prove repeated routing events
    parent to the routing component span instead of flattening under the
    gateway root.

Docs and shared folder:

- PRD5 status docs mark Batch 1K Canary/Sentinel complete and add P5-F21 as
  Batch 1L fixed awaiting Canary.
- `docs/tech-ref/26-prd5-evidence-field-contract.md` documents the request-local
  component span semantics.
- Shared-folder contract:
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-component-span-refinement.md`

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

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1K PASS
   10/10 and that repo history includes Sentinel merge `2db830a` / PR #114.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F20 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F21 as
   `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F21 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1L and lists Batch 1L non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents
   component span refinement as request-local UniRouter evidence only.
6. Verify the shared contract file exists and describes the UniRouter/X-Insight
   component span boundary.
7. Verify `internal/gateway/trace.go` tracks first spans by
   `source_component`, keeps first component events parented to the gateway
   root, and parents repeated same-component events to the component span.
8. Verify trusted inbound parent spans still apply only to the gateway root.
9. Verify no migration, new route, frontend behavior, exporter/outbox,
   read-only credential/view, raw payload capture, X-Insight runtime dependency,
   public-header authority by default, cross-system graph inference, or PRD4
   matrix change is included.
10. Run:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(ParentsEventsToGatewayRootSpan|UsesTrustedEvidenceContext|ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority)' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Component Span Refinement|P5-F21|Batch 1L|componentSpanIDs|parentSpanForEvent|ParentsEventsToGatewayRootSpan|2db830a|PR #114" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-component-span-refinement.md"
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
- final result for PRD5 Batch 1L.
