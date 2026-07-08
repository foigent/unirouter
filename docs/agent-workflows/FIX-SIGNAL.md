# FIX-SIGNAL.md - PRD5 Batch 1D Upstream Request Correlation

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway evidence write/log persistence change plus R2
acceptance-status documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD4 remains complete for the confirmed current scope and
must not be reopened by this round.

This Batch 1D slice implements `P5-F13` upstream request correlation:

- capture provider-returned upstream request IDs from a bounded response-header
  allowlist;
- write the sanitized value to provider response trace events;
- write the sanitized value to `call_logs.upstream_request_id`;
- keep missing provider IDs null.

## Intended Changes

Code:

- `internal/gateway/relay.go`
  - adds `upstreamRequestIDFromHeader`;
  - extracts upstream request IDs from provider response headers on chat and
    embeddings paths;
  - passes the value into non-stream, stream, failed-upstream, and embeddings
    request logging paths;
  - adds `upstream_request_id` to the `call_logs` insert.
- `internal/gateway/trace.go`
  - accepts optional upstream request ID on trace writes;
  - writes it to `request_trace_events.upstream_request_id`;
  - includes the value in trace/log source-event hashes;
  - sanitizes the value before persistence.
- `internal/gateway/relay_mock_test.go`
  - updates structural evidence arg assertions;
  - adds upstream request ID header extraction coverage.

Docs/status:

- Update PRD5 docs/matrix/backlog/status to mark Batch 1A/1B/1C as
  Canary/Sentinel complete and Batch 1D as fixed awaiting Canary.
- Add `P5-F13` to PRD5 as the explicit upstream request correlation workstream.
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
- add exporter/outbox;
- create read-only DB credentials or shared ECS staging;
- store raw prompt/response payloads;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1A/1B/1C
   PASS 17/17 and that `main` includes Sentinel merge `e1e76e3` / PR #106.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 as
   `Canary passed`, keeps P5-F08 through P5-F11 deferred, keeps P5-F12 planned,
   and marks P5-F13 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F13 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently and lists Batch 1D non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents the
   upstream request ID response-header allowlist and sanitizer boundary.
6. Verify `internal/gateway/relay.go` extracts upstream request IDs only from
   provider response headers, not inbound public request headers.
7. Verify `internal/gateway/trace.go` rejects empty, non-printable-ASCII,
   whitespace-containing, or control-character-containing upstream request IDs
   before persistence.
8. Verify provider response trace events can receive `upstream_request_id`.
9. Verify call-log inserts can receive `upstream_request_id`.
10. Verify no migration, new route, frontend behavior, exporter/outbox,
    read-only credential, raw payload capture, or PRD4 matrix change is included.
11. Run:

```bash
go test ./internal/gateway -run 'TestRecord(Request|TraceEvent)|TestUpstreamRequestIDFromHeader|TestRelayNonStreamRequest|TestRelayStreamRequest' -count=1
go test ./internal/gateway ./internal/admin -count=1
rg -n "upstream_request_id|upstreamRequestID|upstreamRequestIDFromHeader|OpenAI-Request-ID|Anthropic-Request-Id|printable ASCII|P5-F13|Batch 1D" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any new inbound public-header trust as a blocking failure;
- any gateway request semantic change outside evidence writes as a blocking
  failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- final result for PRD5 Batch 1D.
