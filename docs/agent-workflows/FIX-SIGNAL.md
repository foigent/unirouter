# FIX-SIGNAL.md - PRD5 Batch 1E Event Capture Gap Emission

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace evidence write/log persistence change plus R2
acceptance-status documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD4 remains complete for the confirmed current scope
and must not be reopened by this round.

This Batch 1E slice implements `P5-F14` event capture-gap emission:

- keep existing safe trace metadata allowlist behavior;
- detect when metadata is dropped because a key is restricted, a value shape is
  unsupported, or an allowed string is truncated;
- write `capture_gap='redacted'` to the affected `request_trace_events` row;
- include the capture gap in the trace source-event hash;
- keep `capture_gap` null when safe metadata is fully retained.

## Intended Changes

Code:

- `internal/traceutil/metadata.go`
  - keeps `SanitizeMetadata` backward compatible;
  - adds `SanitizeMetadataWithGap`;
  - returns `redacted` when sanitization drops restricted keys, rejects
    unsupported value shapes, or truncates allowed strings.
- `internal/gateway/trace.go`
  - uses `SanitizeMetadataWithGap` for gateway trace writes;
  - writes nullable `capture_gap` to `request_trace_events`;
  - includes `capture_gap` in the trace source-event hash.
- `internal/traceutil/metadata_test.go`
  - adds direct coverage for no-gap safe metadata and redacted-gap metadata.
- `internal/gateway/relay_mock_test.go`
  - updates trace event insert assertions and verifies redacted capture-gap
    persistence when metadata sanitization drops unsafe fields.

Docs/status:

- Mark PRD5 Batch 1D as Canary/Sentinel complete.
- Add `P5-F14` to PRD5 as the explicit event capture-gap emission workstream.
- Mark Batch 1E as fixed awaiting Canary across PRD5 status and routing docs.
- Keep read-only DB/view, exporter/outbox, real upstream E2E, and real SSO E2E
  deferred pending prerequisites.

## Non-Goals

This round does not:

- add a DB migration;
- add a new API route;
- change frontend behavior;
- change routing, provider dispatch, billing, wallet, auth, admission, or
  response payload semantics;
- trust inbound public request headers as evidence authority;
- capture raw prompt/response payloads;
- preserve restricted metadata in trace output;
- add nested metadata capture;
- fabricate session, delegation, parent-span, business-chain, or agent-chain
  facts;
- add exporter/outbox;
- create read-only DB credentials or shared ECS staging;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1D PASS
   11/11 and that `main` includes Sentinel merge `bf435ca` / PR #107.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 as `Canary passed`, keeps P5-F08 through P5-F11 deferred, keeps
   P5-F12 planned, and marks P5-F14 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F14 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently and lists Batch 1E non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents
   event-level `capture_gap='redacted'` for metadata redaction/truncation.
6. Verify `internal/traceutil/metadata.go` keeps `SanitizeMetadata` compatible
   and adds `SanitizeMetadataWithGap`.
7. Verify safe scalar allowlisted metadata produces no capture gap.
8. Verify restricted keys, unsupported value shapes, and allowed-string
   truncation produce `capture_gap='redacted'`.
9. Verify `internal/gateway/trace.go` writes nullable `capture_gap` to
   `request_trace_events` and includes it in the trace source-event hash.
10. Verify no migration, new route, frontend behavior, exporter/outbox,
    read-only credential, raw payload capture, fabricated session/delegation
    fact, or PRD4 matrix change is included.
11. Run:

```bash
go test ./internal/traceutil ./internal/gateway -run 'TestSanitizeMetadata|TestRecordTraceEvent|TestRecordRequest|TestUpstreamRequestIDFromHeader' -count=1
go test ./internal/gateway ./internal/admin ./internal/traceutil -count=1
rg -n "capture_gap|SanitizeMetadataWithGap|redacted|P5-F14|Batch 1E|bf435ca|PR #107" internal/traceutil internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any new raw payload or restricted metadata capture as a blocking failure;
- any fabricated session/delegation/business-chain evidence as a blocking
  failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- final result for PRD5 Batch 1E.
