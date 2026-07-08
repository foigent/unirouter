# FIX-SIGNAL.md - PRD5 Batch 1G Trusted Evidence Context Intake

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway evidence write input and config validation change
plus R2 acceptance-status documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD5 Batch 1F passed Canary 11/11 and Sentinel
merged it as `e9670ad` / PR #109. PRD4 remains complete for the confirmed
current scope and must not be reopened by this round.

This Batch 1G slice implements `P5-F16` trusted evidence context intake:

- keep inbound evidence context disabled by default;
- require `EVIDENCE_CONTEXT_TRUST_SECRET` before reading inbound evidence
  context;
- require `X-UniRouter-Evidence-Auth` to match the configured shared secret;
- sanitize and persist trusted trace/session/actor/delegation context;
- include trusted context in trace and call-log source-event hashes.

## Intended Changes

Code:

- `internal/traceutil/evidence_context.go`
  - extracts trusted evidence context only when the shared secret matches;
  - accepts bounded printable non-whitespace `X-UniRouter-*` evidence headers;
  - accepts trusted W3C `Traceparent` as fallback trace/parent-span source;
  - drops invalid, overlong, whitespace-containing, or unsupported enum values.
- `internal/config/config.go`
  - loads `EVIDENCE_CONTEXT_TRUST_SECRET`;
  - keeps the feature disabled when empty;
  - rejects configured secrets shorter than 16 bytes.
- `cmd/uniroute/main.go`
  - passes the configured trust secret into the gateway relay.
- `internal/gateway/relay.go`
  - injects trusted evidence context into chat and embeddings request contexts;
  - writes trusted trace/session/actor/delegation context into call-log evidence.
- `internal/gateway/trace.go`
  - writes trusted trace ID, parent span ID, session ID, actor fields, and
    `on_behalf_of` into trace events;
  - includes those trusted fields in trace and call-log source-event hashes.
- Tests cover helper extraction, config defaults/overrides/invalid values,
  trusted context injection, and trace/log insert arguments.

Docs/config:

- `.env.example`, `docker-compose.yml`, and
  `docs/ops/deployment-runbook.md` document `EVIDENCE_CONTEXT_TRUST_SECRET`.
- PRD5 status docs mark Batch 1F Canary/Sentinel complete and add P5-F16 as
  Batch 1G fixed awaiting Canary.
- Field contract documents the default-off trust gate and accepted headers.

## Non-Goals

This round does not:

- add a DB migration;
- add a new API route;
- change frontend behavior;
- change routing, provider dispatch, billing, wallet, auth, admission, or
  response payload semantics;
- trust public inbound request headers by default;
- capture raw prompt/response payloads;
- add an encrypted payload object store;
- add exporter/outbox;
- create read-only DB credentials or shared ECS staging;
- add X-Insight as a runtime dependency;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1F PASS
   11/11 and that repo history includes Sentinel merge `e9670ad` / PR #109.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07 and
   P5-F13 through P5-F15 as `Canary passed`, keeps P5-F08 through P5-F11
   deferred, keeps P5-F12 planned, and marks P5-F16 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F16 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1G and lists Batch 1G non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` documents the
   default-off trust gate and accepted evidence context headers.
6. Verify `.env.example`, `docker-compose.yml`, and
   `docs/ops/deployment-runbook.md` document `EVIDENCE_CONTEXT_TRUST_SECRET`
   without instructing users to place secrets in repo/shared docs.
7. Verify `internal/traceutil/evidence_context.go` ignores inbound evidence
   context with no configured secret or mismatched `X-UniRouter-Evidence-Auth`.
8. Verify trusted context accepts sanitized trace/session/actor/delegation fields
   and trusted `Traceparent` fallback.
9. Verify `internal/gateway/relay.go` and `internal/gateway/trace.go` write
   trusted context to existing call-log and trace structural columns and include
   it in source-event hashes.
10. Verify no migration, new route, frontend behavior, exporter/outbox,
    read-only credential, raw payload capture, X-Insight runtime dependency,
    public-header authority by default, or PRD4 matrix change is included.
11. Run:

```bash
go test ./internal/traceutil ./internal/config ./internal/gateway -run 'TestExtractTrustedEvidenceContext|TestEvidenceContextRoundTrip|TestValidate_InvalidRuntimeExposureConfig|TestLoad_RuntimeExposureEnvOverrides|TestLoad_Defaults|TestRecord(Request|TraceEvent)_UsesTrustedEvidenceContext|TestRelayContextWithTrustedEvidence' -count=1
go test ./internal/traceutil ./internal/config ./internal/gateway ./cmd/uniroute -count=1
go test ./... -count=1
docker compose config >/tmp/unirouter-compose-config.yml
rg -n "EVIDENCE_CONTEXT_TRUST_SECRET|X-UniRouter-Evidence-Auth|Trusted Evidence Context|P5-F16|Batch 1G|e9670ad|PR #109" internal/traceutil internal/config internal/gateway cmd/uniroute .env.example docker-compose.yml docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

If Docker is unavailable in the Canary environment, record the
`docker compose config` command as environment SKIP rather than product FAIL,
then still verify the compose YAML diff by source review.

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any public-header evidence trust by default as a blocking failure;
- any new raw payload or restricted metadata capture as a blocking failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- final result for PRD5 Batch 1G.
