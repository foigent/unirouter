# FIX-SIGNAL.md - PRD5 Batch 1I Payload Policy Decision Evidence

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace evidence write plus R2 acceptance-status
documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD5 Batch 1F passed Canary 11/11 and Sentinel
merged it as `e9670ad` / PR #109. PRD5 Batch 1G passed Canary 11/11 and
Sentinel merged it as `c701843` / PR #110. PRD5 Batch 1H passed Canary 10/10
and Sentinel merged it as `61f5610` / PR #111. PRD4 remains complete for the
confirmed current scope and must not be reopened by this round.

This Batch 1I slice implements `P5-F18` payload policy decision evidence:

- emit `payload.policy.applied` trace events for chat and embeddings requests;
- place the event after `gateway.request.accepted` and before billing freeze;
- write only sanitized scalar policy metadata:
  `payload_capture_mode`, `payload_policy_reason`,
  `raw_payload_stored=false`, and `payload_ref_present=false`;
- map the event to `decision_kind=payload_capture`;
- map `metadata_only` mode to `decision_result=allowed` and
  `decision_reason=payload.metadata_only`;
- map `disabled` mode to `decision_result=skipped` and
  `decision_reason=payload.disabled`.

## Intended Changes

Code:

- `internal/traceutil/metadata.go`
  - allows sanitized payload policy metadata keys.
- `internal/gateway/trace.go`
  - adds a helper to record payload policy decision events;
  - maps `payload.policy.applied` events to payload-capture decision fields.
- `internal/gateway/relay.go`
  - records payload policy decision events for chat and embeddings requests.
- `internal/gateway/relay_mock_test.go`
  - covers disabled-mode payload policy decisions and verifies no payload
    reference or raw payload storage claim is emitted.
- `internal/traceutil/metadata_test.go`
  - covers payload policy metadata sanitization.

Docs:

- PRD5 status docs mark Batch 1H Canary/Sentinel complete and add P5-F18 as
  Batch 1I fixed awaiting Canary.
- Field contract and shared-folder contract document that Batch 1I records a
  policy decision only and does not implement payload storage or export.

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
- export payload content or payload references;
- add exporter/outbox;
- create read-only DB credentials or shared ECS staging;
- add X-Insight as a runtime dependency;
- fabricate cross-system agent/tool/business-chain facts;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1H PASS
   10/10 and that repo history includes Sentinel merge `61f5610` / PR #111.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F17 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F18 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F18 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1I and lists Batch 1I non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents Batch 1I
   as policy-decision evidence only, with no raw payload storage/export.
6. Verify
   `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-payload-policy-decision-evidence.md`
   exists and describes the UniRouter/X-Insight handoff boundary.
7. Verify `internal/traceutil/metadata.go` allowlists only safe payload policy
   metadata keys and does not allow raw prompt/response fields.
8. Verify `internal/gateway/relay.go` emits `payload.policy.applied` for chat
   and embeddings after `gateway.request.accepted`.
9. Verify `internal/gateway/trace.go` maps `payload.policy.applied` to
   `decision_kind=payload_capture`; metadata-only mode should be `allowed`,
   disabled mode should be `skipped`.
10. Verify no migration, new route, frontend behavior, exporter/outbox,
   read-only credential, raw payload capture, X-Insight runtime dependency,
   public-header authority by default, fabricated cross-system chain, or PRD4
   matrix change is included.
11. Run:

```bash
go test ./internal/traceutil ./internal/gateway -run 'TestSanitizeMetadata_PayloadPolicyFields|TestRecordPayloadPolicyDecision_(DisabledMode|MetadataOnlyMode)|TestRecordTraceEvent_ParentsEventsToGatewayRootSpan' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Payload Policy Decision Evidence|P5-F18|Batch 1I|payload.policy.applied|payload_capture|payload_capture_mode|payload_policy_reason|raw_payload_stored|payload_ref_present|61f5610|PR #111" internal/traceutil internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-payload-policy-decision-evidence.md"
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
- final result for PRD5 Batch 1I.
