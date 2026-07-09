# UniRouter Agent Handoff

> Purpose: fast onboarding for Codex, OpenClaw Canary, or any other agent taking over this repository.
> Read this file first, then follow the linked current-state documents.

## First Read Order

1. `docs/ops/CURRENT-STATUS.md` - current verified project status.
2. `docs/PRD-Phase5.md` - active PRD5 scope.
3. `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` - PRD5 acceptance rows and evidence boundaries.
4. `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` - PRD5 batch plan and UniRouter/X-Insight ownership split.
5. `docs/planning/PRD5-COMPETITOR-COMPARISON.md` - competitor comparison and UniRouter-owned PRD5 catch-up items.
6. `docs/planning/PRD5-BACKLOG.md` - capabilities intentionally deferred beyond PRD4 and PRD5 active scope.
7. `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md` - X-Insight Gate A sequencing decisions.
8. `docs/PRD-Phase4.md` - completed PRD4 scope.
9. `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` - PRD4 accepted/deferred rows and evidence boundaries.
10. `docs/planning/PRD4-BACKLOG.md` - PRD4 candidate backlog source record.
11. `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` - completed PRD3 cleanup/optimization acceptance state.
12. `docs/PRD-Phase3.md` - completed PRD3 scope.
13. `docs/planning/PRD3-CONSISTENCY-AUDIT.md` - PRD1 -> PRD2 consistency findings behind PRD3.
14. `docs/issues/OPEN-ISSUES.md` - active unresolved issues and recently verified fixes.
15. `docs/agent-workflows/AGENT-PROTOCOL.md` - risk-gated Codex/OpenClaw Canary/Sentinel/Atlas workflow.
16. `docs/agent-workflows/FIX-SIGNAL.md` and `docs/agent-workflows/VERIFY-RESULT.md` - current signal state and latest verification result.
17. `docs/CODEBASE-MAP.md` - code, route, data, test, and deployment map.
18. `docs/DOC-INVENTORY.md` and `docs/DOCS-GOVERNANCE.md` - which docs are current, frozen, historical, candidate, or reference-only.

## Current Project Shape

UniRouter is a local-deployable enterprise AI gateway/MaaS platform. PRD1 delivered the core gateway, admin API, admin frontend, billing, logs, wallet, pricing, admission controls, health checks, and monitoring foundation. PRD2 is frozen for new feature work after accepted product expansions such as project/RBAC, model governance, notification events, license foundation, OIDC, usage analytics, internal status, and error docs.

PRD3 is complete for the confirmed scope. It was cleanup/optimization only: consistency, semantics, route/API contract, documentation lifecycle, module boundaries, frontend architecture, parity tests, and engineering governance.

PRD4 is complete for the confirmed commercial observability and control foundation scope. Its formal scope is `docs/PRD-Phase4.md`: API key owner usage/cost, quota vocabulary, Flow/Event/Trace foundation, PlungeIn observation-first ports, Status Center runtime observability, external monitoring integration, real upstream/SSO harness assets only, auth UX hardening, production evidence promotion for currently available evidence, and PRD5 backlog consolidation. User decision on 2026-07-08 moved real upstream target E2E and real SSO target E2E out of current PRD4 because no approved real environments are available. `docs/planning/PRD4-BACKLOG.md` remains the candidate-source record, not the formal implementation contract.

PRD5 is now active for evidence-readiness work. Its current scope is
`docs/PRD-Phase5.md`: X-Insight Gate A sanitized snapshot, structural evidence
field contract, payload policy/evidence references, lightweight UniRouter trace
UI boundary, competitor comparison and UniRouter-owned catch-up items, plus later
nullable field implementation. X-Insight remains a separate project and owns
broader cross-system AI/Agent observability and governance surfaces.

## What Is Current Truth

| Question | Current Source |
|----------|----------------|
| What is the PRD5 formal scope? | `docs/PRD-Phase5.md` |
| What is the PRD5 acceptance plan? | `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` |
| What is the PRD5 implementation order? | `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` |
| What competitor gaps feed PRD5? | `docs/planning/PRD5-COMPETITOR-COMPARISON.md` |
| What is the PRD4 formal scope? | `docs/PRD-Phase4.md` |
| What is the PRD4 acceptance plan? | `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` |
| Which items are explicitly deferred to PRD5? | `docs/planning/PRD5-BACKLOG.md` |
| What is actually verified now? | `docs/ops/CURRENT-STATUS.md` |
| Which PRD3 items are accepted? | `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` |
| Which PRD2 items are frozen/accepted/deferred? | `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
| Which capabilities were PRD4 candidates before formal scoping? | `docs/planning/PRD4-BACKLOG.md` |
| What should be fixed next? | `docs/issues/OPEN-ISSUES.md` |
| How should Codex and OpenClaw coordinate? | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Which old docs may be stale? | `docs/DOC-INVENTORY.md` |

PRD1 and PRD2 documents remain useful requirements/evidence references, but they are not the active planning surface for new work. Do not treat files under `docs/testing/archive/` as current status. They are historical evidence unless reconciled into the current-state files above.

## Risk-Gated Agent Workflow

Use `docs/agent-workflows/AGENT-PROTOCOL.md` to classify each change before deciding the handoff path:

1. Codex implements or edits and classifies the change as R0/R1/R2/R3/R4.
2. R0 local cleanup does not require `FIX-SIGNAL.md` / `VERIFY-RESULT.md`.
3. R1 tooling/evidence changes may be grouped into one Canary gate.
4. R2/R3/R4 changes require Codex to update `docs/agent-workflows/FIX-SIGNAL.md`.
5. User asks OpenClaw Canary to verify that file when a risk gate is triggered.
6. OpenClaw writes `docs/agent-workflows/VERIFY-RESULT.md`.
7. Codex reads the result and either closes the loop or fixes failures.

User handoff sentence for OpenClaw when a risk gate is triggered:

```text
去 /Users/m4/Desktop/projects/UniRouter/docs/agent-workflows/FIX-SIGNAL.md，按里面的验收指令逐条验证，结果写入 VERIFY-RESULT.md。
```

## Current Verification Baseline

Current active verification signal: PRD5 Batch 1N Snapshot Capture Gap Candidates
fixed awaiting Canary in `docs/agent-workflows/FIX-SIGNAL.md`.

PRD4 is complete for the confirmed current scope. PRD5 must preserve the PRD4
deferred-boundary decisions recorded here.

PRD5 Batch 1A/1B/1C:

- Scope: planning closure, first snapshot artifact, and nullable structural
  evidence fields plus lightweight trace UI exposure.
- Status: Canary PASS 17/17 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `e1e76e3` / PR #106.
- Adds: PRD5 formal scope, PRD5 matrix, implementation plan, competitor
  comparison, sanitized snapshot runbook, structural evidence field contract,
  `scripts/prd5_xinsight_snapshot.go`, a shared-folder snapshot contract,
  migration `000018`, gateway trace/log structural writes, and existing trace
  API read-side structural fields, and Logs detail evidence quality / structural
  evidence tags.
- Does not add: new API routes, deployment changes, read-only credentials,
  exporter/outbox, trusted inbound correlation headers, raw prompt/response
  capture, X-Insight timeline/graph UI, or PRD4 matrix changes.

PRD5 Batch 1D:

- Scope: provider-returned upstream request ID correlation for gateway evidence.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `bf435ca` / PR #107.
- Adds: bounded response-header allowlist extraction, sanitized
  `upstream_request_id` persistence on provider trace events and call logs, and
  focused gateway tests.
- Does not add: inbound public-header trust, migration, new route, frontend
  behavior, exporter/outbox, read-only credential, raw payload capture, or PRD4
  matrix changes.

PRD5 Batch 1E:

- Scope: event-level capture-gap emission for trace metadata safety.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `a19830f` / PR #108.
- Adds: `capture_gap='redacted'` when UniRouter drops restricted metadata,
  rejects unsupported metadata shapes, or truncates allowed metadata strings;
  focused `traceutil` and gateway tests.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw payload capture, fabricated session/delegation
  facts, or PRD4 matrix changes.

PRD5 Batch 1F:

- Scope: restricted payload capture mode configuration.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `e9670ad` / PR #109.
- Adds: `PAYLOAD_CAPTURE_MODE` defaulting to `metadata_only`; active support for
  `metadata_only` and `disabled` only; config validation that rejects unsupported
  payload modes; gateway trace/log writes that persist the configured
  `payload_capture_mode`; source-event hashes that include the configured mode;
  `.env.example`, `docker-compose.yml`, and deployment-runbook docs.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw prompt/response capture, encrypted payload object
  store, redacted/hash/reference active modes, fabricated session/delegation
  facts, or PRD4 matrix changes.

PRD5 Batch 1G:

- Scope: default-off trusted evidence context intake for internal
  trace/session/actor/delegation propagation.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `c701843` / PR #110.
- Adds: `EVIDENCE_CONTEXT_TRUST_SECRET`, `X-UniRouter-Evidence-Auth` trust gate,
  bounded `X-UniRouter-*` evidence header parsing, trusted `Traceparent`
  fallback, trace/log structural writes for trusted `trace_id`,
  `parent_span_id`, `session_id`, `actor_*`, and `on_behalf_of`, plus
  source-event hashes that include those fields.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, public inbound header trust by default, raw payload
  capture, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1H:

- Scope: gateway span hierarchy for UniRouter-emitted request trace events.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `61f5610` / PR #111.
- Adds: per-request in-memory span state, gateway root span assignment on the
  first UniRouter trace event, trusted inbound parent preservation for the root
  span, later UniRouter trace events parented to the gateway root span, and
  source-event hashes that include the resulting `parent_span_id`.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, public inbound header trust by default, raw payload
  capture, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1I:

- Scope: explicit payload capture policy decision evidence for chat and
  embeddings.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `1dc20b5` / PR #112.
- Adds: `payload.policy.applied` trace events after gateway request acceptance;
  sanitized metadata for `payload_capture_mode`, `payload_policy_reason`,
  `raw_payload_stored=false`, and `payload_ref_present=false`; and
  `decision_kind=payload_capture` with `allowed` for metadata-only mode or
  `skipped` for disabled mode.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw prompt/response capture, payload hashing, payload
  object references, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1J:

- Scope: provider upstream request ID capture-gap evidence.
- Status: Canary/Sentinel complete; Sentinel merge reflected by `f5223df` /
  PR #113.
- Adds: `capture_gap=provider_not_returned` on
  `provider.response_accepted` and `provider.response_rejected` trace events
  when UniRouter has no sanitized provider-returned `upstream_request_id`.
  Existing returned upstream IDs and redaction gaps are preserved.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw prompt/response capture, X-Insight runtime
  dependency, public inbound-header trust, provider request/response semantic
  changes, or PRD4 matrix changes.

PRD5 Batch 1K:

- Scope: Gate A snapshot Markdown handoff-note support for X-Insight discovery.
- Status: Canary/Sentinel complete; Sentinel merge reflected by `2db830a` /
  PR #114.
- Adds: optional `--handoff-note <path>` support in
  `scripts/prd5_xinsight_snapshot.go`; a generated DSN-free Markdown note with
  generated date, source instance, schema version, snapshot output path,
  selected request IDs, scenario labels, counts, safety confirmation, and
  command shape without DSN; and a shared-folder contract for X-Insight.
- Does not add: real snapshot generation without approved request IDs and a
  read-only DSN, read-only DB/view credentials, shared ECS staging,
  exporter/outbox, migration, API route, frontend behavior, raw prompt/response
  capture, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1L:

- Scope: request-local component span refinement for UniRouter trace events.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `ffebf83` / PR #115.
- Adds: per-request component span tracking in `internal/gateway/trace.go` so
  first component events parent to the gateway root and repeated same-component
  events parent to that component's first span; focused gateway coverage in
  `internal/gateway/relay_mock_test.go`; shared X-Insight contract for the
  request-local hierarchy boundary.
- Does not add: migration, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, raw prompt/response capture,
  X-Insight runtime dependency, cross-system agent/tool/business graph
  inference, or PRD4 matrix changes.

PRD5 Batch 1M:

- Scope: provider failure capture-gap evidence.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `4a4e254` / PR #116.
- Adds: `capture_gap=not_available_from_source` for provider-stage failure
  events that occur before an upstream provider response source exists; preserves
  `provider_not_returned` for provider response events that lack a returned
  upstream request ID; preserves `redacted` priority for sanitized metadata
  gaps; passes available embeddings upstream request IDs into
  `provider.response_parse_failed`; and shares the gap contract with X-Insight.
- Does not add: migration, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, raw prompt/response capture,
  X-Insight runtime dependency, provider dispatch semantic change, or PRD4
  matrix changes.

PRD5 Batch 1N:

- Scope: snapshot capture-gap candidate categories for Gate A handoff artifacts.
- Status: fixed awaiting Canary in `docs/agent-workflows/FIX-SIGNAL.md`.
- Adds: optional `candidate_capture_gaps` in generated snapshot `capture_gaps`;
  candidates `sampled_out`, `retention_expired`, and `not_instrumented` when a
  call log exists but no trace rows are exported; and a capture-gap table in the
  Markdown handoff note.
- Does not add: migration, runtime request-path change, API route, frontend
  behavior, read-only DB/view credential, shared ECS staging, exporter/outbox,
  raw prompt/response capture, X-Insight runtime dependency, definitive
  sampling/retention cause assertion, or PRD4 matrix changes.

Latest completed PRD4 verification result is PRD4 Batch 1S Open Issue Boundary
Closeout:

- Status: Canary PASS 7/7 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `5b41b87` / PR #103.
- Scope: documentation/status-boundary only.
- Mapping: `PRD4-PLUGIN-001` -> `PRD5-PLUGIN-001` / `PRD5-MARKET-001`,
  `PRD4-DEPLOY-001` -> `PRD5-DEPLOY-001`, and `PRD4-INFRA-001` ->
  `PRD5-INFRA-001`.
- Target cleanup: User reported Atlas deleted `uniroute_test`, closing
  `PRD4-DEPLOY-002`.
- Evidence: `docs/ops/evidence/PRD4-final-closeout-20260708.md`.
- Boundary: no Go, frontend, migration, API route, runtime behavior, deployment
  script, or `Production verified` claim changed.

Previous completed verification result is PRD4 Batch 1R Runtime Build Chain
Follow-up:

- Status: Canary PASS 9/9; Sentinel merge reflected by `819846c` / PR #102.
- Scope: repo-controllable `PRD4-RUNTIME-002` runtime/build-chain gaps.
- Evidence: `docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md`.
- Changes: Go toolchain pin to `go1.25.12`, Docker builder patch version,
  version/commit `ldflags`, runtime metadata fallback, compose
  `RUNTIME_ENVIRONMENT=local`, compose `GRAFANA_URL=http://grafana:3000`,
  backend `/health/ready` healthcheck, env/runbook docs.
- Verification: focused Go tests, `go test ./... -count=1`, `go build`
  with `-ldflags`, `go env`
  showing `go1.25.12` / `auto`, YAML parse, and `git diff --check`.
- Boundary: Docker CLI was unavailable to Canary, so live container health still
  needs CI/Atlas before any Production verified runtime claim; target test
  database cleanup was later closed by User-reported Atlas deletion of
  `uniroute_test`.

Previous completed verification result is PRD4 Batch 1Q Production Evidence
Promotion:

- Status: Canary PASS 8/8; Sentinel commit reflected by `f0d34e1`.
- Scope: documentation/evidence-boundary only.
- Evidence: `docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md`.
- Decision: current target evidence is target-smoke evidence for selected PRD4
  areas, but no PRD4 matrix row is promoted to `Production verified`.
- Reason: the final report covers the PRD4 Batch 1N target state, Batch 1O
  fixed later findings, and `PRD4-RUNTIME-002` was still open at that point.
- No Go, frontend, migration, API route, runtime behavior, deployment script,
  evidence harness code, or production deployment change was made in Batch 1Q.

Latest accepted PRD4 documentation/status-boundary batch is PRD4 Batch 1P Scope
Deferral And Closeout Alignment:

- Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `9e2927f` / PR #100.
- Scope: documentation/status-boundary only. User decision on 2026-07-08 moves
  `P4-F08` / `P4-F09` real target execution out of current PRD4 because no
  approved real upstream or real SSO environments are available.
- Batch 1H/1I harnesses remain accepted reusable assets.
- `docs/planning/PRD5-BACKLOG.md` now tracks `PRD5-UPSTREAM-001` and
  `PRD5-SSO-001`.
- `P4-F13` PRD5 Backlog Consolidation is accepted.
- No Go, frontend, migration, API route, runtime behavior, deployment script,
  evidence harness code, or Production verified claim changed.

Latest accepted PRD4 implementation batch remains PRD4 Batch 1O API Key Input And
Security Header Hardening:

- Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `59721d7` / PR #99.
- Issue/matrix: `PRD4-SECURITY-001` / `P4-F12`.
- Scope: API key create/update rejects unsafe or overlong display names and
  negative `project_id`; the HTTP chain adds baseline browser security headers;
  and unknown GET paths no longer fall through to the root service/version
  response.
- This batch does not change billing, routing, RBAC, migrations, trace storage,
  SSO, upstream, wallet, provider dispatch, frontend behavior, Go toolchain
  version, Grafana target config, runtime `ldflags`, container healthcheck
  wiring, target test database cleanup, or Production verified claims.
- Runtime/build-chain findings from the 2026-07-08 target report passed Batch 1R
  Canary verification as `PRD4-RUNTIME-002`; target test database cleanup was
  later closed by User-reported Atlas deletion of `uniroute_test`.

Previous accepted PRD4 implementation batch is PRD4 Batch 1N Developer Console
Self Usage And Model Cost:

- Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `6194062` / PR #98.
- Batch 1N uses existing Usage Analytics with `scope=my`, selected `project_id`,
  and `group_by=alias` to show the current user's own usage, cost, and
  model/alias breakdown in Developer Console.
- Target-environment final test evidence for the deployed PRD4 Batch 1N state is
  archived at `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`.
- The report also records follow-up security/runtime issues; application-code
  issues are in Batch 1O, repo-controllable runtime/build-chain issues passed
  Batch 1R Canary verification as `PRD4-RUNTIME-002`, and target DB cleanup was
  later closed by User-reported Atlas deletion of `uniroute_test`.

Previous accepted trace redaction batch is PRD4 Batch 1M:

- Canary PASS 8/8 was recorded before Batch 1N overwrote the current
  `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `2f43fca` / PR #97.
- Batch 1M shares trace metadata sanitization across gateway trace writes and
  `GET /api/v1/logs/{id}/trace` responses.
- Restricted trace metadata keys such as `api_key`, `authorization`,
  `access_token`, `request_body`, and `prompt` are not returned by the trace
  API even if they exist in historical or manually inserted rows.
- Batch 1M does not change route registration, RBAC, DB schema, trace
  sampling/storage semantics, frontend layout, billing, routing, auth, SSO,
  upstream, wallet, provider dispatch, or Production verified target evidence.

Previous accepted runtime redaction batch is PRD4 Batch 1L:

- Canary PASS 8/8 was recorded before Batch 1M overwrote the current
  `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `4426efd` / PR #96.
- Batch 1L redacts Prometheus/Grafana `url` and `health_url` response fields in
  `GET /api/v1/runtime/status` by stripping URL userinfo, query strings, and
  fragments while raw configured monitoring URLs are still used for probes.

Previous accepted trace retention batch is PRD4 Batch 1K:

- Canary PASS 10/10 was recorded before Batch 1L overwrote the current
  `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `1d9ea50` / PR #95.
- Batch 1K adds `TRACE_EVENT_RETENTION_DAYS`, defaults it to
  `LOG_RETENTION_DAYS`, exposes both retention knobs in env/compose/runbook, and
  wires existing cleanup to delete expired `request_trace_events`.
- Batch 1K does not add trace partitioning, archival, OpenTelemetry
  ingestion/export, prompt/response plaintext storage, runtime cache, or
  Production verified target-environment evidence.

Previous accepted runtime telemetry batch is PRD4 Batch 1J:

- Canary PASS 8/8 is recorded in earlier verification history.
- Sentinel merge is reflected by `8707346` / PR #94.
- Batch 1J adds host CPU/disk/network summaries to
  `GET /api/v1/runtime/status` and `/status` Runtime Observability cards without
  cluster inventory, raw topology, private IP/hostname or cloud identifiers,
  OpenTelemetry, metric alerts, or Production verified target evidence.

Accepted real SSO evidence-harness batch is PRD4 Batch 1I, with real target
execution deferred to PRD5:

- Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `2c02430` / PR #92.
- Batch 1I adds guarded tooling for `P4-F09` real SSO evidence:
  `scripts/prd4-real-sso-smoke.sh`, `scripts/prd4_real_sso_smoke.mjs`,
  `scripts/prd4_real_sso_audit_check.go`, and
  `docs/tech-ref/24-prd4-real-sso-evidence.md`.
- Preflight mode is safe without target credentials: it reports `SKIP`, does
  not drive a browser, does not call an IdP, and does not read the database.
- Real mode requires approved Logto or enterprise OIDC target access, a test
  user, console/backend/issuer URLs, DB DSN, and
  `UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`.
- User decision on 2026-07-08: because no approved real SSO environment exists,
  current PRD4 keeps this harness only and defers real target execution to
  `PRD5-SSO-001`.

Latest workflow verification gate is Protocol Risk-Gated Workflow Update And
Status Reconciliation:

- Canary PASS 10/10 was recorded in `docs/agent-workflows/VERIFY-RESULT.md`
  before later PRD4 product batches overwrote the current result file.
- No runtime code, migrations, PRD4 product behavior, or target-environment
  evidence changed.

Accepted real upstream evidence-harness batch is PRD4 Batch 1H, with real
target execution deferred to PRD5:

- Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `d3acfa6` / PR #91.
- Atlas deployment has not been reported for Batch 1H.
- Batch 1H adds guarded tooling for `P4-F08` real upstream evidence:
  `scripts/prd4-real-upstream-smoke.sh`, `scripts/prd4_real_upstream_seed.go`,
  `scripts/prd4_real_upstream_smoke.mjs`, and
  `docs/tech-ref/23-prd4-real-upstream-evidence.md`.
- Preflight mode is safe without credentials: it reports `SKIP`, does not
  mutate the database, and does not call providers.
- Real mode requires approved OpenRouter and direct Anthropic credentials/model
  slugs, matching backend secrets, and
  `UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`.
- User decision on 2026-07-08: because no approved real upstream environment
  exists, current PRD4 keeps this harness only and defers real target execution
  to `PRD5-UPSTREAM-001`.

The latest accepted route-contract baseline remains PRD3 Batch 6 on 2026-07-05:

```bash
go test ./cmd/uniroute -run 'Test(OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage|AdminRoutesRegisteredForFrontendClient)' -count=1 -v
npm --prefix frontend test -- --run ApiDocsPage client
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- Route parity target tests: PASS.
- Go tests: PASS.
- Frontend tests: PASS, 18 files / 97 tests.
- Frontend build: PASS with Vite large chunk warning.

Always check `docs/ops/CURRENT-STATUS.md` and `docs/agent-workflows/VERIFY-RESULT.md` for the latest accepted batch before making new claims.

## Worktree Warning

Always run `git status --short` before editing. The worktree may contain user or agent changes from another round. Do not revert them unless the user explicitly requests it. Avoid mixing unrelated cleanup with the current batch.

## No Bulk Deletion

Project instruction forbids bulk deletion of files or directories. If cleanup requires removing files, stop and ask the user. Only one explicit file path may be deleted at a time after approval.
