# PRD5 Acceptance Matrix

> Date: 2026-07-10
> Status: PRD5 Batch 1A/1B/1C, Batch 1D, Batch 1E, Batch 1F, Batch 1G,
> Batch 1H, Batch 1I, Batch 1J, Batch 1K, Batch 1L, Batch 1M, Batch 1N,
> Batch 1O, Batch 1P, Batch 1Q, Batch 1R, Batch 1S, Batch 1T, and Batch 1U
> Historical Canary/Sentinel evidence is complete through Batch 1V (`cc38094` / PR #125). Post-merge
> target execution produced an immutable
> sanitized snapshot with one denied-scenario correlation/evidence gap and now
> awaits X-Insight import. Batch 1W Pre-Relay Model Permission Evidence Parity
> is CI verified at `931400a` / PR #126 under Agent Protocol v2.0; target
> deployment/rerun remains pending.
> PRD4 remains complete and is not reopened.
> Source: `docs/PRD-Phase5.md`, `docs/planning/PRD5-BACKLOG.md`,
> `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`, current repo/code review,
> X-Insight shared-folder responses, PRD5 Batch 1A/1B/1C implementation
> artifacts, PRD5 Batch 1D upstream request correlation changes, PRD5 Batch 1E
> event capture-gap emission changes, PRD5 Batch 1F payload capture mode
> configuration changes, PRD5 Batch 1G trusted evidence context intake changes,
> PRD5 Batch 1H gateway span hierarchy changes, PRD5 Batch 1I payload policy
> decision evidence changes, PRD5 Batch 1J provider upstream capture-gap
> changes, PRD5 Batch 1K snapshot handoff-note tooling changes, PRD5 Batch 1L
> component span refinement changes, PRD5 Batch 1M provider failure
> capture-gap changes, PRD5 Batch 1N snapshot capture-gap candidate changes,
> PRD5 Batch 1O Gate A controlled snapshot handoff preparation, PRD5 Batch 1P
> model permission decision evidence changes, PRD5 Batch 1Q request approval
> manifest changes, PRD5 Batch 1R approval runbook/worksheet changes, PRD5
> Batch 1S data exchange control surface decision changes, PRD5 Batch 1T
> exchange envelope/controlled-run authorization changes, PRD5 Batch 1U
> Embeddings model-permission parity changes, Batch 1V Atlas artifact evidence,
> and Batch 1W pre-relay permission ownership changes.

## Status Values

| Status | Meaning |
|--------|---------|
| Planned | Accepted into PRD5 plan but not yet implemented |
| Fixed awaiting Canary | Codex completed scoped changes and requested independent verification |
| Canary passed | Canary verified the requested evidence |
| Local verified | The execution owner completed the recorded local checks; this is not an independent review or target claim |
| CI verified | The corresponding commit passed the repository's required CI checks |
| Authorized awaiting Atlas | User-approved target action is bounded and handed off, but no target execution or result is claimed yet |
| Target executed with known gap | Target action produced real evidence, but a recorded gap prevents full target acceptance |
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
| P5-F17 | Gateway Span Hierarchy | Canary passed | UniRouter-emitted trace events in one request share a gateway root span; the first span preserves a trusted inbound parent when present, and later spans parent to the gateway root | Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `61f5610` / PR #111 | Batch 1H code-only evidence hierarchy; no migration, new route, frontend behavior, exporter/outbox, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F18 | Payload Policy Decision Evidence | Canary passed | Chat and embeddings traces emit explicit `payload.policy.applied` events with sanitized payload policy metadata and `decision_kind=payload_capture`, while raw prompt/response capture remains off | Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `1dc20b5` / PR #112 | Batch 1I code-only evidence enrichment; no migration, new route, frontend behavior, exporter/outbox, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F19 | Provider Upstream Capture Gap | Canary passed | Provider response trace events write `capture_gap=provider_not_returned` when no sanitized `upstream_request_id` is available, while preserving returned IDs and redaction gaps | Canary PASS recorded before Batch 1K; Sentinel merge `f5223df` / PR #113 | Batch 1J code-only evidence enrichment; no migration, new route, frontend behavior, exporter/outbox, read-only credential, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F20 | Gate A Snapshot Handoff Note | Canary passed | Explicit-request X-Insight snapshot runs can write a companion Markdown handoff note with selected IDs, scenario labels, counts, safety confirmation, and DSN-free command shape | Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `2db830a` / PR #114 | Batch 1K tooling/docs enrichment; no runtime behavior change, migration, new route, frontend behavior, exporter/outbox, read-only credential handoff, shared ECS staging, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F21 | Component Span Refinement | Canary passed | Repeated trace events from the same `source_component` parent to that component's first span while first component events still parent to the request gateway root | Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `ffebf83` / PR #115 | Batch 1L code-only evidence hierarchy refinement; no migration, new route, frontend behavior, exporter/outbox, read-only credential, raw payload capture, X-Insight runtime dependency, cross-system graph inference, or PRD4 matrix change |
| P5-F22 | Provider Failure Capture Gaps | Canary passed | Provider failure trace events that occur before a provider response exists write `capture_gap=not_available_from_source` when no sanitized `upstream_request_id` can exist, while response events keep `provider_not_returned` and redaction gaps keep priority | Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `4a4e254` / PR #116 | Batch 1M code-only evidence gap refinement; no migration, new route, frontend behavior, exporter/outbox, read-only credential, raw payload capture, X-Insight runtime dependency, provider dispatch semantic change, or PRD4 matrix change |
| P5-F23 | Snapshot Capture Gap Candidates | Canary passed | Gate A snapshots keep primary missing-row gaps factual while adding `candidate_capture_gaps` for missing trace rows, so X-Insight can distinguish possible sampling, retention, and non-instrumented causes without UniRouter fabricating evidence | Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `92cb8ce` / PR #117 | Batch 1N snapshot-tooling evidence enrichment; no migration, runtime request-path change, new route, frontend behavior, exporter/outbox, read-only credential, raw payload capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F24 | Gate A Controlled Snapshot Handoff Prep | Canary passed | UniRouter status docs reflect Batch 1N completion and the next Gate A controlled snapshot handoff preconditions are explicit before any real snapshot, DSN, credential, read-only DB/view, shared ECS staging, or exporter/outbox work starts | Canary PASS 8/8 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `b2eea2e` / PR #118 | Batch 1O docs/shared-folder coordination only; no snapshot artifact, migration, runtime request-path change, new route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F25 | Model Permission Decision Evidence | Canary passed | Existing chat `allowed_models` rejects emit sanitized `auth.model_not_allowed` trace evidence with `source_component=auth`, `capture_gap=permission_denied`, `decision_kind=auth`, `decision_result=rejected`, and `decision_reason=auth_error.model_not_allowed` before the existing 403 response | Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `61091c` / PR #119 | Batch 1P auth/trace evidence enrichment only; no migration, new route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, Embeddings authorization behavior change, or PRD4 matrix change |
| P5-F26 | Gate A Request Approval Manifest | Canary passed | Snapshot tooling accepts a JSON approval manifest and exports only rows explicitly marked `approved` with concrete request IDs, while `pending`/`unavailable` rows remain non-exported and direct request flags cannot be mixed with manifest mode | Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `ed8c69d` / PR #120 | Batch 1Q tooling/docs/shared-folder coordination only; no real snapshot artifact, approved request ID invention, migration, runtime request-path change, new route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F27 | Gate A Request ID Approval Runbook | Canary passed | User-facing runbook and shared worksheet explain how to approve concrete request IDs without requiring the User to edit JSON, touch DB access, or approve broad export | Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `c15cd7d` / PR #121 | Batch 1R docs/shared-folder coordination only; no real snapshot artifact, approved request ID invention, migration, runtime request-path change, new route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, or PRD4 matrix change |
| P5-F28 | X-Insight Data Exchange Control Surface | Canary passed | Future UI/control-plane boundary is defined as UniRouter-owned outbound approval/export ledger/import receipt control, while downstream observability, read audit, investigation timeline, graph, policy simulation, and evidence packs remain X-Insight-owned | Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `c6ae2a7` / PR #122 | Batch 1S docs/shared-folder coordination only; no UI code, API route, migration, real snapshot artifact, approved request ID invention, read-only DB/view, shared ECS staging, exporter/outbox, X-Insight runtime dependency, raw payload capture, or PRD4 matrix change |
| P5-F29 | Gate A Exchange Envelope And Controlled Run Authorization | Canary passed | Snapshots carry exchange identity and exact manifest hash, manifest source cannot be overridden, handoff commands preserve manifest selection, and fresh Gate A requests require prior scenario/data/environment/operator/retention authorization | Canary PASS 7/7 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `fc62e65` / PR #123 | Batch 1T tooling/tests/docs/shared-folder coordination only; no real request, snapshot artifact, approved IDs, secrets, UI, API, migration, runtime path change, read-only DB/view, shared ECS, exporter/outbox, X-Insight runtime dependency, or PRD4 change |
| P5-F30 | Embeddings Model Permission Parity | Canary passed | Embeddings applies the same API-key `allowed_models` rule as Chat and emits the same auth rejection evidence before billing, routing, or provider work | Canary PASS 7/7 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge `b2ba892` / PR #124 | Batch 1U isolated R3 authorization fix; no route, migration, frontend, raw payload, exporter/outbox, X-Insight runtime dependency, real Gate A execution, target claim, or PRD4 change |
| P5-F31 | Gate A Authorized Controlled Execution | Target executed with known gap | Atlas executes exactly two approved synthetic scenarios in a confirmed non-production UniRouter ECS environment, hands off sanitized metadata, and obtains X-Insight persistent import/readback receipt evidence | Canary/Sentinel `cc38094` / PR #125; authorization `UR-XI-GATE-A-20260710-01`; deployed app `b2ba892`; exchange `uxchg_f7de31e541516893a0d9b31cc0f3892a`; artifact SHA-256 `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`; one call log, seven trace events, two gaps; X-Insight receipt pending | Success scenario passed. Denied scenario returned 403 but lacked returned-ID correlation and source trace because the live wrapper rejected before Relay. Snapshot is diagnostic evidence, not complete Batch 1P target evidence |
| P5-F32 | Pre-Relay Model Permission Evidence Parity | CI verified; target deployment/rerun pending | Project-allowed requests reach Relay for API-key model restriction so live 403 responses include `X-Request-ID` and the accepted auth decision trace before billing | `931400a` / PR #126 with required Backend/Frontend/Docker checks PASS; `docs/ops/evidence/PRD5-B1W-pre-relay-model-permission-parity-local-20260710.md`; full handler integration test for header/trace/no-call-log/no-freeze; wrapper/gateway tests; full Go suite | Batch 1W removes one duplicate pre-relay API-key check; no project model-governance, route, migration, frontend, billing amount, provider, immutable snapshot, X-Insight runtime, or PRD4 change |

## Batch 1W Verification

PRD5 Batch 1W verifies that live API-key model denials reach Relay and preserve
the accepted correlation/evidence contract:

```bash
go test ./cmd/uniroute ./internal/gateway -count=1
go test -tags=integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -count=1 -v
go test ./... -count=1
rg -n "P5-F32|Batch 1W|PRD5-EVIDENCE-002|Pre-Relay Model Permission Evidence Parity|company-other|auth.model_not_allowed|permission_denied|X-Request-ID" cmd/uniroute docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-COMPETITOR-COMPARISON.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md
git diff -- frontend internal/admin internal/billing internal/db internal/migrate migrations
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git diff --check
```

Any remaining pre-relay API-key `allowed_models` denial, missing correlation
header/decision trace, billing freeze/call-log write, project model-governance
semantic change, target-history rewrite, raw payload, X-Insight runtime
dependency, PRD4 change, or `Production verified` claim is a blocking failure.

## Batch 1V Verification

PRD5 Batch 1V verifies the actual immutable Atlas artifact and keeps its gap
explicit while X-Insight import remains pending:

```bash
shasum -a 256 "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-snapshot-20260710.json" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/gatea-approved-request-ids.json"
jq '.counts, .capture_gaps, .safety' "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-snapshot-20260710.json"
rg -n "PARTIAL|correlation/evidence exception|not_available_from_source|awaiting X-Insight|test-key disable" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-10-unirouter-gate-a-atlas-run-result.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-persistent-import-and-receipt.md"
```

Any claim that the denied scenario is fully correlated, already imported, or
complete Batch 1P target evidence is a blocking failure.

## Batch 1U Verification

PRD5 Batch 1U verifies Embeddings model-permission parity:

```bash
git log --oneline -3 origin/main | rg "b2ba892.*PRD5 Batch 1U|#124"
go test ./internal/gateway -run 'TestModelAllowedForAPIKey|TestHandleChatCompletion_ModelNotAllowed|TestHandleEmbeddings_Model|TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision' -count=1 -v
go test ./internal/gateway ./internal/middleware ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "P5-F30|Batch 1U|Embeddings Model Permission Parity|modelAllowedForAPIKey|rejectModelNotAllowed|TestHandleEmbeddings_ModelNotAllowed|auth.model_not_allowed|permission_denied|b2ba892|PR #124" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-COMPETITOR-COMPARISON.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-embeddings-model-permission-parity.md"
git diff -- frontend internal/admin internal/billing internal/db internal/migrate
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git status --short
git diff --check
```

Any billing amount/freeze semantic change beyond pre-billing denial, routing or
provider success-path change, route/migration/frontend change, raw payload,
exporter/outbox, X-Insight runtime dependency, real Gate A execution, target
environment claim, PRD4 change, or `Production verified` claim is a scope
failure.

## Batch 1T Verification

PRD5 Batch 1T verifies exchange identity and controlled-run authorization:

```bash
git log --oneline -3 origin/main | rg "c6ae2a7.*PRD5 Batch 1S|#122"
go test scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go -count=1
go run scripts/prd5_xinsight_snapshot.go -help
rg -n "P5-F29|Batch 1T|exchange_id|approval_manifest_sha256|xinsight.import-receipt.v1|controlled run|Controlled Run|X-Request-ID|artifact.sha256|seven days|7 天" scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-gate-a-exchange-envelope.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-controlled-run-authorization.md" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-exchange-envelope-and-controlled-run-response.md"
go test ./... -count=1
git diff -- frontend internal cmd migrations
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git status --short
git diff --check
```

Any real controlled request, real snapshot/manifest/receipt artifact, approved
request ID invention, DSN/credential/token/API key/raw payload, UI/API/migration/
runtime request-path change, read-only DB/view credential, shared ECS staging,
exporter/outbox, X-Insight runtime dependency, PRD4 matrix change, or
`Production verified` claim is a scope failure.

## Batch 1S Verification

PRD5 Batch 1S verifies the X-Insight data exchange control surface decision:

```bash
git log --oneline -3 origin/main | rg "c15cd7d.*PRD5 Batch 1R|#121"
rg -n "X-Insight Data Exchange Control Surface|P5-F28|Batch 1S|Data Exchange Control|outbound ledger|read-side audit|Investigation|Receipt Reconciliation|Stage 2: Minimal UniRouter UI|Put all X-Insight audits in UniRouter|Do not do this" docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/CODEBASE-MAP.md docs/DOC-INVENTORY.md docs/README.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md"
rg -n "import_job_id|exchange_id|deep link|read-side audit|X-Insight owns" "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md"
git status --short
git diff --name-only
git diff -- frontend internal cmd scripts
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git diff --check
```

No Go/frontend test run is required for Batch 1S because it changes only
documentation and shared-folder coordination files. Any UI code, API route,
migration, real snapshot artifact, approved request ID invention, DSN/credential
handoff, raw prompt/response capture, read-only DB/view credential, shared ECS
staging, exporter/outbox, X-Insight runtime dependency, PRD4 matrix change, or
`Production verified` claim is a scope failure.

## Batch 1R Verification

PRD5 Batch 1R verifies the human request ID approval process:

```bash
git log --oneline -3 origin/main | rg "ed8c69d.*PRD5 Batch 1Q|#120"
rg -n "Gate A Request ID Approval Runbook|P5-F27|Batch 1R|receipt number for one UniRouter|approval manifest is a whitelist|Auto-discover request IDs|Read-only DB/view|Exporter/outbox|Example User Approval Sentence" docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/CODEBASE-MAP.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md"
rg -n 'approved|pending|unavailable|Do not paste raw prompts|Only rows marked `approved`' "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md"
git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md
git diff --check
```

No Go/frontend test run is required for Batch 1R because it changes only
documentation and shared-folder coordination files. Any real snapshot artifact,
approved request ID invention, DSN/credential/token, raw prompt/response, raw
IP, raw upstream error, migration, runtime request-path change, API route,
frontend behavior, read-only DB/view credential, shared ECS staging,
exporter/outbox, X-Insight runtime dependency, PRD4 matrix change, or
`Production verified` claim is a scope failure.

## Batch 1Q Verification

PRD5 Batch 1Q verifies Gate A request approval manifest handling:

```bash
go run scripts/prd5_xinsight_snapshot.go -help 2>&1 | rg -- "-approval-manifest"
go run scripts/prd5_xinsight_snapshot.go --approval-manifest <(printf '%s' '{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"unirouter-dev-gate-a-2026-07-10","requests":[{"scenario_label":"successful_non_stream","request_id":"TBD","approval_status":"pending","required":true}]}') 2>&1 | rg "approval manifest contains no approved request IDs"
go run scripts/prd5_xinsight_snapshot.go --approval-manifest <(printf '%s' '{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"unirouter-dev-gate-a-2026-07-10","requests":[{"scenario_label":"successful_non_stream","request_id":"TBD","approval_status":"approved","required":true}]}') 2>&1 | rg "approved entry must include a concrete request_id"
go run scripts/prd5_xinsight_snapshot.go --approval-manifest <(printf '%s' '{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"unirouter-dev-gate-a-2026-07-10","requests":[{"scenario_label":"successful_non_stream","request_id":"req-approved-1","approval_status":"approved","required":true}]}') --request-id req-extra 2>&1 | rg -- "--approval-manifest cannot be combined"
go test ./...
rg -n "Gate A Request Approval Manifest|P5-F26|Batch 1Q|--approval-manifest|approved_request_id_manifest|unirouter.prd5.xinsight.approval.v1|61091c|PR #119|approved request IDs" scripts/prd5_xinsight_snapshot.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/CODEBASE-MAP.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-approved-request-ids-response.md"
git diff --check
```

Any real snapshot artifact, approved request ID invention, DSN/credential/token,
raw prompt/response, raw IP, raw upstream error, migration, runtime request-path
change, API route, frontend behavior, read-only DB/view credential, shared ECS
staging, exporter/outbox, X-Insight runtime dependency, PRD4 matrix change, or
`Production verified` claim is a scope failure.

## Batch 1P Verification

PRD5 Batch 1P verifies runtime model-permission decision evidence:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_ModelNotAllowedCaptureGapAndDecision|TestHandleChatCompletion_ModelNotAllowed|TestRecordTraceEvent_ProviderResponseMissingUpstreamRequestIDCaptureGap|TestRecordTraceEvent_ProviderFailureMissingUpstreamRequestIDCaptureGap|TestRecordPayloadPolicyDecision'
go test ./internal/traceutil
go test ./...
rg -n "Model Permission Decision Evidence|P5-F25|Batch 1P|auth.model_not_allowed|permission_denied|decision_kind=auth|auth_error.model_not_allowed|Embeddings authorization behavior change|b2eea2e|PR #118" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/CODEBASE-MAP.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-model-permission-decision-evidence.md"
git diff --check
```

Any migration, new API route, frontend behavior, read-only DB/view credential,
shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight
runtime dependency, Embeddings authorization behavior change, PRD4 matrix change,
or `Production verified` claim is a scope failure.

## Batch 1O Verification

PRD5 Batch 1O verifies status reconciliation and Gate A controlled snapshot
handoff preparation:

```bash
git log --oneline -3 origin/main
rg -n "Gate A Controlled Snapshot Handoff Prep|P5-F24|Batch 1O|92cb8ce|PR #117|controlled sanitized snapshot|approved request IDs|no DSN|no raw prompt/response|read-only DB/view remains gated" docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/planning/PRD5-BACKLOG.md docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-prd5-controlled-snapshot-handoff-prep.md"
rg -n "P5-F23 \\| Snapshot Capture Gap Candidates \\| Canary passed|92cb8ce|PR #117" docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/ops/CURRENT-STATUS.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/PROJECT-HANDOFF.md
git diff --check
```

No Go/frontend test run is required for Batch 1O because it changes only
documentation and shared-folder coordination files. Any code, migration, API
route, frontend behavior, real snapshot artifact, DSN, credential, read-only
DB/view, shared ECS staging, exporter/outbox, raw prompt/response capture,
X-Insight runtime dependency, PRD4 matrix count, or `Production verified` claim
is a scope failure.

## Batch 1N Verification

PRD5 Batch 1N verifies Gate A snapshot capture-gap candidates:

```bash
go run scripts/prd5_xinsight_snapshot.go -help
go run scripts/prd5_xinsight_snapshot.go --request-id req-example --handoff-note - 2>&1 | rg -- "--handoff-note must be a Markdown file path"
go test ./... -count=1
rg -n "Snapshot Capture Gap Candidates|P5-F23|Batch 1N|candidate_capture_gaps|CandidateGaps|sampled_out|retention_expired|not_instrumented|Capture Gaps|4a4e254|PR #116" scripts/prd5_xinsight_snapshot.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-snapshot-capture-gap-candidates.md"
git diff --check
```

No new migration, runtime request-path change, API route, frontend behavior,
exporter/outbox, read-only credential, raw prompt/response capture, payload
object store, X-Insight runtime dependency, PRD4 matrix count, or
`Production verified` claim should change in Batch 1N.

## Batch 1M Verification

PRD5 Batch 1M verifies provider failure capture-gap evidence:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(ProviderFailureMissingUpstreamRequestIDCaptureGap|ProviderFailureRedactionGapTakesPriority|ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority)' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Provider Failure Capture Gaps|P5-F22|Batch 1M|not_available_from_source|providerMissingUpstreamCaptureGap|ProviderFailureMissingUpstreamRequestIDCaptureGap|ProviderFailureRedactionGapTakesPriority|4a4e254|PR #116" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-provider-failure-capture-gaps.md"
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, provider dispatch semantic change, PRD4 matrix count, or
`Production verified` claim should change in Batch 1M.

## Batch 1L Verification

PRD5 Batch 1L verifies component span refinement:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(ParentsEventsToGatewayRootSpan|UsesTrustedEvidenceContext|ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority)' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Component Span Refinement|P5-F21|Batch 1L|componentSpanIDs|parentSpanForEvent|ParentsEventsToGatewayRootSpan|ffebf83|PR #115" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-component-span-refinement.md"
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, cross-system graph inference, PRD4 matrix count, or
`Production verified` claim should change in Batch 1L.

## Batch 1K Verification

PRD5 Batch 1K verifies Gate A snapshot handoff-note support:

```bash
go run scripts/prd5_xinsight_snapshot.go -help
go run scripts/prd5_xinsight_snapshot.go --request-id req-example --handoff-note - 2>&1 | rg -- "--handoff-note must be a Markdown file path"
go test ./... -count=1
rg -n "Gate A Snapshot Handoff Note|P5-F20|Batch 1K|--handoff-note|writeHandoffNote|formatHandoffNote|Command Shape Without DSN|DSN, credentials, tokens|snapshot handoff-note" scripts/prd5_xinsight_snapshot.go docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-gate-a-snapshot-handoff-note.md"
git diff --check
```

No real snapshot artifact, DSN, credential, token, raw prompt/response, raw IP,
raw upstream error, migration, API route, frontend behavior, exporter/outbox,
read-only credential handoff, shared ECS staging, X-Insight runtime dependency,
PRD4 matrix count, or `Production verified` claim should change in Batch 1K.

## Batch 1J Verification

PRD5 Batch 1J verifies provider upstream capture-gap evidence:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority|SanitizesMetadataAndSnapshotsOwner)|TestUpstreamRequestIDFromHeader' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Provider Upstream Capture Gap|P5-F19|Batch 1J|provider_not_returned|providerMissingUpstreamCaptureGap|ProviderResponseMissingUpstreamRequestIDCaptureGap|ProviderResponseRedactionGapTakesPriority|f5223df|PR #113" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-provider-upstream-capture-gap.md"
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, public-header evidence authority, provider request/response semantic
change, PRD4 matrix count, or `Production verified` claim should change in
Batch 1J.

## Batch 1I Verification

PRD5 Batch 1I verifies payload policy decision evidence:

```bash
go test ./internal/traceutil ./internal/gateway -run 'TestSanitizeMetadata_PayloadPolicyFields|TestRecordPayloadPolicyDecision_(DisabledMode|MetadataOnlyMode)|TestRecordTraceEvent_ParentsEventsToGatewayRootSpan' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Payload Policy Decision Evidence|P5-F18|Batch 1I|payload.policy.applied|payload_capture|payload_capture_mode|payload_policy_reason|raw_payload_stored|payload_ref_present|61f5610|PR #111" internal/traceutil internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-09-unirouter-prd5-payload-policy-decision-evidence.md"
git diff --check
```

No new migration, API route, frontend behavior, exporter/outbox, read-only
credential, raw prompt/response capture, payload object store, X-Insight runtime
dependency, public-header evidence authority, PRD4 matrix count, or
`Production verified` claim should change in Batch 1I.

## Batch 1H Verification

PRD5 Batch 1H verifies gateway span hierarchy:

```bash
go test ./internal/gateway -run 'TestRecordTraceEvent_(SanitizesMetadataAndSnapshotsOwner|UsesTrustedEvidenceContext|ParentsEventsToGatewayRootSpan)|TestRelayContextWithTrustedEvidence' -count=1
go test ./internal/gateway ./internal/traceutil ./cmd/uniroute -count=1
go test ./... -count=1
rg -n "Gateway Span Hierarchy|P5-F17|Batch 1H|parent_span_id|contextWithEvidenceSpanState|parentSpanForEvent|61f5610|PR #111" internal/gateway docs/PRD-Phase5.md docs/ops/PRD5-ACCEPTANCE-MATRIX.md docs/planning/PRD5-IMPLEMENTATION-PLAN.md docs/tech-ref/26-prd5-evidence-field-contract.md docs/agent-workflows/FIX-SIGNAL.md
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
