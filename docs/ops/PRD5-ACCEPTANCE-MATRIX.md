# PRD5 Acceptance Matrix

> Date: 2026-07-08
> Status: PRD5 Batch 1A/1B/1C, Batch 1D, Batch 1E, Batch 1F, and Batch 1G
> Canary/Sentinel complete; Batch 1H gateway span hierarchy fixed awaiting
> Canary. PRD4 remains complete for the confirmed current scope and is not
> reopened.
> Source: `docs/PRD-Phase5.md`, `docs/planning/PRD5-BACKLOG.md`,
> `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`, current repo/code review,
> X-Insight shared-folder responses, PRD5 Batch 1A/1B/1C implementation
> artifacts, PRD5 Batch 1D upstream request correlation changes, PRD5 Batch 1E
> event capture-gap emission changes, PRD5 Batch 1F payload capture mode
> configuration changes, PRD5 Batch 1G trusted evidence context intake changes,
> and PRD5 Batch 1H gateway span hierarchy changes.

## Status Values

| Status | Meaning |
|--------|---------|
| Planned | Accepted into PRD5 plan but not yet implemented |
| Fixed awaiting Canary | Codex completed scoped changes and requested independent verification |
| Canary passed | Canary verified the requested evidence |
| E2E accepted | Integrated local, staging, or approved non-production evidence exists |
| Production verified | Target production or explicitly approved production-like evidence exists |
| Deferred pending prerequisite | Explicitly not actionable until a named prerequisite exists |
| Out of scope | Not part of UniRouter's local-deploy enterprise gateway direction unless User changes direction |

Production verified requires target-environment evidence with date, environment,
commit or artifact, credential boundary, command/manual path, expected result,
actual result, and rollback or cleanup note.

## Matrix

| ID | Capability | Status | Acceptance Criteria | Required Evidence | Notes |
|----|------------|--------|---------------------|-------------------|-------|
| P5-F01 | PRD5 Scope And Acceptance Baseline | Canary passed | Formal PRD5 scope, implementation plan, matrix, docs routing, and PRD4 boundary discipline exist | Canary PASS 17/17 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `e1e76e3` / PR #106 | Batch 1A docs-only planning surface; no PRD4 row/count changes |
| P5-F02 | X-Insight Gate A Sanitized Snapshot | Canary passed | Explicit-request snapshot tool exports sanitized `call_logs` and `request_trace_events` with source refs, structural fields, and capture gaps | Canary PASS 17/17; source/runbook review | No raw prompt/response, API key, credential, raw IP, raw upstream error, or broad table export |
| P5-F03 | Structural Evidence Field Contract | Canary passed | Field dictionary, enums, trust rules, nullable migration plan, and writer behavior plan exist | Canary PASS 17/17; `docs/tech-ref/26-prd5-evidence-field-contract.md` review | Batch 1D extends the same contract with upstream request ID response-header allowlist |
| P5-F04 | Payload Policy And Evidence References | Canary passed | Payload capture is default-off; `payload_capture_mode`, `payload_ref`, `evidence_ref`, hashes, and ownership split are documented and visible in request trace detail | Canary PASS 17/17; PRD5 PRD, field contract, trace API/frontend review | Raw payload storage remains a later explicit enterprise opt-in decision |
| P5-F05 | Nullable Structural Field Implementation | Canary passed | New nullable/default structural fields are added and populated where UniRouter knows facts directly | Canary PASS 17/17; migration review, gateway/admin focused tests | Batch 1B adds migration `000018`, gateway trace/log structural writes, and trace API read-side exposure. It does not trust new inbound headers, collect raw payloads, or add exporter/outbox |
| P5-F06 | Lightweight Trace UI Boundary | Canary passed | UniRouter keeps request-level trace UI; X-Insight owns cross-system investigation; deep links are later | Canary PASS 17/17; `frontend/src/pages/LogsPage.tsx` review and LogsPage test | Batch 1C adds evidence quality summary and per-event structural evidence tags without adding timeline/graph UI |
| P5-F07 | Competitor-Informed Feature Catch-Up | Canary passed | Current source-grounded comparison exists and catch-up items are split by UniRouter vs X-Insight ownership | Canary PASS 17/17; `docs/planning/PRD5-COMPETITOR-COMPARISON.md` review | Official source refresh was performed on 2026-07-08; refresh again before public claims |
| P5-F08 | Read-Only DB/View Gate | Deferred pending prerequisite | Restricted read-only DB/view is considered only after X-Insight local snapshot import passes | X-Insight Gate A import evidence, User approval | No credentials or DSNs in repo/shared folder |
| P5-F09 | Cursor Export API Or Outbox | Deferred pending prerequisite | Exporter/outbox design references Gate A gaps, field quality, cursor, retry, dedupe, redaction, and backpressure | Gate A evidence plus design doc | Structural fields first; no synchronous X-Insight dependency |
| P5-F10 | Real Upstream Target E2E | Deferred pending prerequisite | PRD4 real-upstream harness runs against approved target credentials and model slugs | Target run evidence, cleanup note | Tracked from `PRD5-UPSTREAM-001` |
| P5-F11 | Real SSO Target E2E | Deferred pending prerequisite | PRD4 real-SSO harness runs against approved IdP tenant/client/test user | Target browser/audit evidence, cleanup note | Tracked from `PRD5-SSO-001` |
| P5-F12 | Larger Deferred Backlog Screening | Planned | Scheduler, plugins, managed HA/IaC, runtime cache, SDK, roles, and hard budget blocking are separately screened before implementation | Future scope docs and User approval | Not automatically active merely because PRD5 started |
| P5-F13 | Upstream Request Correlation | Canary passed | Provider-returned upstream request IDs are captured from a bounded response-header allowlist and written to trace/log evidence without trusting public inbound headers | Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `bf435ca` / PR #107 | Batch 1D code-only evidence enrichment; no migration, new route, exporter/outbox, raw payload capture, request-path semantic change, or PRD4 matrix change |
| P5-F14 | Event Capture Gap Emission | Canary passed | Trace events record `capture_gap='redacted'` when UniRouter intentionally drops, shape-rejects, or truncates trace metadata during safe evidence capture | Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `a19830f` / PR #108 | Batch 1E code-only evidence enrichment; no migration, new route, frontend behavior, exporter/outbox, raw payload capture, fabricated session/delegation facts, or PRD4 matrix change |
| P5-F15 | Payload Capture Mode Configuration | Canary passed | `PAYLOAD_CAPTURE_MODE` defaults to `metadata_only`, allows only `metadata_only` and `disabled`, rejects unsupported modes, and writes the configured mode to trace/log evidence plus source-event hashes | Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `e9670ad` / PR #109 | Batch 1F restricted runtime config; no migration, new route, frontend behavior, exporter/outbox, raw prompt/response capture, new payload object store, or PRD4 matrix change |
| P5-F16 | Trusted Evidence Context Intake | Canary passed | Inbound trace/session/actor/delegation context is ignored by default, accepted only when `EVIDENCE_CONTEXT_TRUST_SECRET` is configured and `X-UniRouter-Evidence-Auth` matches, sanitized before persistence, and written to existing trace/log structural fields plus source-event hashes | Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `c701843` / PR #110 | Batch 1G trusted internal context only; no migration, new route, frontend behavior, exporter/outbox, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F17 | Gateway Span Hierarchy | Fixed awaiting Canary | UniRouter-emitted trace events in one request share a gateway root span; the first span preserves a trusted inbound parent when present, and later spans parent to the gateway root | Focused gateway tests, source review, docs grep, `git diff --check` | Batch 1H code-only evidence hierarchy; no migration, new route, frontend behavior, exporter/outbox, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |

## Batch 1H Verification

PRD5 Batch 1H verifies gateway span hierarchy:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(SanitizesMetadataAndSnapshotsOwner|UsesTrustedEvidenceContext|ParentsEventsToGatewayRootSpan)|TestRelayContextWithTrustedEvidence' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Gateway Span Hierarchy|P5-F17|Batch 1H|parent_span_id|contextWithEvidenceSpanState|parentSpanForEvent|c701843|PR #110" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, public-header evidence authority, PRD4 matrix count, or
`Production verified` claim should change in Batch 1H.

## Batch 1G Verification

PRD5 Batch 1G verifies trusted evidence context intake:

```bash
go test ./internal/traceutil ./internal/config ./internal/gateway -run 'TestExtractTrustedEvidenceContext|TestEvidenceContextRoundTrip|TestValidate_InvalidRuntimeExposureConfig|TestLoad_RuntimeExposureEnvOverrides|TestLoad_Defaults|TestRecord(Request|TraceEvent)_UsesTrustedEvidenceContext|TestRelayContextWithTrustedEvidence' -count=1
go test ./internal/traceutil ./internal/config ./internal/gateway ./cmd/uniroute -count=1
go test ./... -count=1
docker compose config >/tmp/unirouter-compose-config.yml
rg -n "EVIDENCE_CONTEXT_TRUST_SECRET|X-UniRouter-Evidence-Auth|Trusted Evidence Context|P5-F16|Batch 1G|c701843|PR #110" internal/traceutil internal/config internal/gateway cmd/uniroute .env.example docker-compose.yml docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, public-header evidence authority, PRD4 matrix count, or
`Production verified` claim should change in Batch 1G.

## Batch 1F Verification

PRD5 Batch 1F verifies restricted payload capture mode configuration:

```bash
go test ./internal/traceutil ./internal/config ./internal/gateway -run 'TestPayloadCaptureMode|TestValidate_InvalidRuntimeExposureConfig|TestLoad_RuntimeExposureEnvOverrides|TestLoad_Defaults|TestRecord(Request|TraceEvent)|TestSetTraceEvents' -count=1
go test ./internal/traceutil ./internal/config ./internal/gateway ./cmd/uniroute -count=1
go test ./... -count=1
docker compose config >/tmp/unirouter-compose-config.yml
rg -n "PAYLOAD_CAPTURE_MODE|payload_capture_mode|P5-F15|Batch 1F|e9670ad|PR #109" internal/traceutil internal/config internal/gateway cmd/uniroute .env.example docker-compose.yml docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, fabricated business-chain/session/delegation fact, PRD4 matrix
count, or `Production verified` claim should change in Batch 1F.

## Batch 1E Verification

PRD5 Batch 1E verifies event capture-gap emission:

```bash
go test ./internal/traceutil ./internal/gateway -run 'TestSanitizeMetadata|TestRecordTraceEvent|TestRecordRequest|TestUpstreamRequestIDFromHeader' -count=1
go test ./internal/gateway ./internal/admin ./internal/traceutil -count=1
rg -n "capture_gap|SanitizeMetadataWithGap|redacted|P5-F14|Batch 1E|a19830f|PR #108" internal/traceutil internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw payload capture, fabricated business-chain/session/delegation
fact, PRD4 matrix count, or `Production verified` claim should change in Batch
1E.

## Batch 1D Verification

PRD5 Batch 1D verifies upstream request correlation:

```bash
go test ./internal/gateway -run 'TestRecord(Request|TraceEvent)|TestUpstreamRequestIDFromHeader|TestRelayNonStreamRequest|TestRelayStreamRequest' -count=1
go test ./internal/gateway ./internal/admin -count=1
rg -n "upstream_request_id|upstreamRequestID|upstreamRequestIDFromHeader|OpenAI-Request-ID|Anthropic-Request-Id|printable ASCII|P5-F13|Batch 1D" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw payload capture, PRD4 matrix count, or `Production verified`
claim should change in Batch 1D.

## Batch 1A/1B/1C Historical Verification

PRD5 Batch 1A/1B/1C verifies planning closure, snapshot tooling, nullable
structural evidence fields, and lightweight trace UI exposure:

```bash
go run scripts/prd5_xinsight_snapshot.go -help
go test ./internal/gateway -run 'TestRecord(Request|TraceEvent)' -count=1
go test ./internal/admin -run 'TestGetRequestTrace|TestSanitizeTraceResponseMetadata' -count=1
npm --prefix frontend test -- LogsPage.test.tsx
npm --prefix frontend run build
rg -n "P5-F01|P5-F12|PRD5|X-Insight|sanitized snapshot|trace_id|capture_gap|decision_kind|LiteLLM|New API|one-api|Helicone|Langfuse" docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-COMPETITOR-COMPARISON.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/planning/PRD5-BACKLOG.md
git diff --check
```

No new API route, deployment configuration, exporter/outbox,
read-only credential, raw payload capture, PRD4 matrix count, or
`Production verified` claim should change in Batch 1A/1B/1C.
