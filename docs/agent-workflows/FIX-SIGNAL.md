# FIX-SIGNAL.md - PRD5 Batch 1F Payload Capture Mode Configuration

**Date:** File timestamp is authoritative
**Risk level:** R3 gateway trace/log evidence write configuration plus R2
acceptance-status documentation update
**Status:** awaiting Canary verification

## Context

PRD5 Batch 1A/1B/1C passed Canary 17/17 and Sentinel merged it as
`e1e76e3` / PR #106. PRD5 Batch 1D passed Canary 11/11 and Sentinel merged it
as `bf435ca` / PR #107. PRD5 Batch 1E passed Canary 11/11 and Sentinel merged
it as `a19830f` / PR #108. PRD4 remains complete for the confirmed current
scope and must not be reopened by this round.

This Batch 1F slice implements `P5-F15` payload capture mode configuration:

- add `PAYLOAD_CAPTURE_MODE` runtime config;
- default to `metadata_only`;
- allow only `metadata_only` and `disabled` in the active implementation;
- reject unsupported payload modes during config validation;
- write the configured mode to trace and call-log evidence;
- include the configured mode in source-event hashes.

## Intended Changes

Code:

- `internal/traceutil/payload.go`
  - defines `metadata_only` and `disabled`;
  - normalizes blank/mixed-case config values;
  - rejects unsupported modes such as `raw`, `redacted`, `hash_only`, and
    `reference_only`.
- `internal/config/config.go`
  - loads `PAYLOAD_CAPTURE_MODE`;
  - defaults to `metadata_only`;
  - validates the value before runtime use.
- `cmd/uniroute/main.go`
  - passes the configured payload capture mode into the gateway relay.
- `internal/gateway/relay.go`
  - stores the configured payload mode with a safe metadata-only fallback;
  - writes the configured value into `call_logs.payload_capture_mode`;
  - includes the configured value in call-log source-event hashes.
- `internal/gateway/trace.go`
  - writes the configured value into
    `request_trace_events.payload_capture_mode`;
  - includes the configured value in trace source-event hashes.
- Tests cover config defaults/overrides/invalid values, helper behavior, and
  trace/log insert arguments.

Docs/config:

- `.env.example`, `docker-compose.yml`, and
  `docs/ops/deployment-runbook.md` document `PAYLOAD_CAPTURE_MODE`.
- PRD5 status docs mark Batch 1E Canary/Sentinel complete and add P5-F15 as
  Batch 1F fixed awaiting Canary.
- Field contract now states that only `metadata_only` and `disabled` are active
  runtime modes; redacted/hash/reference/raw remain later gated decisions.

## Non-Goals

This round does not:

- add a DB migration;
- add a new API route;
- change frontend behavior;
- change routing, provider dispatch, billing, wallet, auth, admission, or
  response payload semantics;
- trust inbound public request headers as evidence authority;
- capture raw prompt/response payloads;
- add an encrypted payload object store;
- activate `redacted`, `hash_only`, or `reference_only` payload modes;
- fabricate session, delegation, parent-span, business-chain, or agent-chain
  facts;
- add exporter/outbox;
- create read-only DB credentials or shared ECS staging;
- change PRD4 acceptance counts;
- create a `Production verified` claim.

## Canary Verification Instructions

1. Verify `docs/agent-workflows/VERIFY-RESULT.md` records PRD5 Batch 1E PASS
   11/11 and that repo history includes Sentinel merge `a19830f` / PR #108.
2. Verify `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` marks P5-F01 through P5-F07,
   P5-F13, and P5-F14 as `Canary passed`, keeps P5-F08 through P5-F11 deferred,
   keeps P5-F12 planned, and marks P5-F15 as `Fixed awaiting Canary`.
3. Verify `docs/PRD-Phase5.md` defines P5-F15 and does not reopen PRD4.
4. Verify `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` says UniRouter can still
   proceed independently through Batch 1F and lists Batch 1F non-goals.
5. Verify `docs/tech-ref/26-prd5-evidence-field-contract.md` states active
   runtime payload modes are only `metadata_only` and `disabled`.
6. Verify `.env.example`, `docker-compose.yml`, and
   `docs/ops/deployment-runbook.md` document `PAYLOAD_CAPTURE_MODE` without
   implying raw prompt/response capture.
7. Verify `internal/config/config.go` loads and validates
   `PAYLOAD_CAPTURE_MODE`, defaults blank/missing values to `metadata_only`,
   and rejects unsupported modes.
8. Verify `internal/gateway/relay.go` and `internal/gateway/trace.go` write the
   configured `payload_capture_mode` to call-log and trace evidence.
9. Verify trace and call-log source-event hashes include
   `payload_capture_mode`.
10. Verify no migration, new route, frontend behavior, exporter/outbox,
    read-only credential, raw payload capture, payload object store,
    fabricated session/delegation fact, or PRD4 matrix change is included.
11. Run:

```bash
go test ./internal/traceutil ./internal/config ./internal/gateway -run 'TestPayloadCaptureMode|TestValidate_InvalidRuntimeExposureConfig|TestLoad_RuntimeExposureEnvOverrides|TestLoad_Defaults|TestRecord(Request|TraceEvent)|TestSetTraceEvents' -count=1
go test ./internal/traceutil ./internal/config ./internal/gateway ./cmd/uniroute -count=1
go test ./... -count=1
docker compose config >/tmp/unirouter-compose-config.yml
rg -n "PAYLOAD_CAPTURE_MODE|payload_capture_mode|P5-F15|Batch 1F|a19830f|PR #108" internal/traceutil internal/config internal/gateway cmd/uniroute .env.example docker-compose.yml docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

If Docker is unavailable in the Canary environment, record the
`docker compose config` command as environment SKIP rather than product FAIL,
then still verify the compose YAML diff by source review.

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered instruction;
- any PRD4 boundary regression as a blocking failure;
- any new raw payload or restricted metadata capture as a blocking failure;
- any fabricated session/delegation/business-chain evidence as a blocking
  failure;
- any migration/new-route/frontend/exporter/raw-payload change as a scope
  failure;
- final result for PRD5 Batch 1F.
