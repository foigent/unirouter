# PRD4 Acceptance Matrix

> Date: 2026-07-07
> Status: PRD4 Batch 0 scope baseline accepted per User report; PRD4 Preflight Hardening passed Canary 6/6; Batch 1A Auth UX hardening passed Canary 6/6 and User reported Sentinel completed; Batch 1B Usage And Cost Attribution passed Canary and User reported Sentinel completed; Batch 1C Flow/Event/Trace Foundation passed Canary and User reported Sentinel completed; Batch 1D Runtime Observability Foundation passed Canary and User reported Sentinel completed; Batch 1E Quota Vocabulary And Key Quota Foundation is fixed awaiting Canary.
> Source: `docs/PRD-Phase4.md`, `docs/planning/PRD4-BACKLOG.md`, current
> PRD2/PRD3 evidence boundaries, and User PRD4 scope decisions.

## Status Values

| Status | Meaning |
|--------|---------|
| Planned | Accepted into PRD4 scope but not implemented or verified |
| Fixed awaiting Canary | Codex completed changes and requested independent verification |
| Canary passed | Canary verified the requested evidence |
| Implemented not accepted | Code/docs exist but have not passed required verification |
| Unit tested | Focused tests pass, but no integrated evidence yet |
| E2E accepted | Integrated local, staging, or approved non-production evidence exists |
| Production verified | Target production or explicitly approved production-like evidence exists |
| Deferred to PRD5 | Explicitly outside PRD4 implementation scope |
| Out of scope | Not part of the local-deploy enterprise MaaS direction unless User changes product direction |

Production verified requires target-environment evidence with date, environment,
commit or artifact, credential boundary, command/manual path, expected result,
actual result, and rollback or cleanup note.

## Matrix

| ID | Capability | Status | Acceptance Criteria | Required Evidence | Notes |
|----|------------|--------|---------------------|-------------------|-------|
| P4-F01 | PRD4 Scope And Evidence Baseline | Canary passed | `docs/PRD-Phase4.md`, this matrix, PRD5 backlog, and ADRs exist; current docs route to them; no runtime code changes | Docs grep, `git diff --check`, Canary doc consistency review | Batch 0 accepted per User report after `e24731d` / PR #82; later `VERIFY-RESULT.md` was overwritten by PRD4 Preflight Hardening |
| P4-F02 | Usage And Cost Attribution | Canary passed | API key owner usage is visible to the owner; authorized roles see broader scope; totals reconcile by model/key/project/time range | Backend/frontend tests, seeded two-user/two-key E2E, CSV or API evidence | Batch 1B adds owner/type metadata to API keys, owner/type call-log snapshots, `scope=my`, owner/key-type filters and groupings, frontend analytics controls, and personal-key disablement before hard user delete. Historical logs intentionally keep `owner_user_id=NULL` instead of fabricated ownership. Canary passed and User reported Sentinel completed after `7dd8aa0` / PR #85; Atlas deployment not reported |
| P4-F03 | Quota Vocabulary And Key Quota Foundation | Fixed awaiting Canary | RPM/RPS/TPM/tokens/s terms are stable; key-level quota visibility is present; Scheduler V2 is not implied | Unit/integration tests, docs/UI label checks, migration evidence if schema changes | Batch 1E adds preferred `rps_limit`, structured key `quota` response data, Admin API Keys / Developer Console quota UI, and `docs/tech-ref/21-prd4-quota-vocabulary.md`. Existing `qps_limit` remains compatibility storage/API field. No Scheduler V2, key-level TPM enforcement, budget hard-block, quota audit reason/idempotency fields, or schema migration |
| P4-F04 | Flow/Event/Trace Foundation | Canary passed | One request can be investigated across gateway, billing, routing, provider, call logs, and audit metadata | Request-path E2E by `request_id`, redaction negative checks | Batch 1C adds metadata-only `request_trace_events`, trace sampling config, gateway/billing/routing/provider/call-log events for chat and embeddings, `GET /api/v1/logs/{id}/trace` with project usage RBAC, and Logs detail rendering. User reported Canary/Sentinel completed after `4f62e34` / PR #86. No OpenTelemetry, Runtime Status Center, Prometheus/Grafana integration status, direct global request-id lookup, retention cleanup, or prompt/response plaintext |
| P4-F05 | PlungeIn Port Architecture | Planned | Observe/Act/Enrich/Decide/Mutate ports are documented; PRD4 only allows observation-first ports | ADR review, docs consistency, later port tests if code exists | No arbitrary runtime plugin in PRD4 |
| P4-F06 | Status Center And Runtime Observability | Canary passed | Event Status, Runtime Environment, Capacity Health, and Observability Integrations are separate surfaces; stale/missing telemetry is explicit | Backend/frontend tests, RBAC tests, local runtime smoke | Batch 1D adds `GET /api/v1/runtime/status`, runtime metadata config, app memory, DB/Redis health, recent traffic summary, and same-page Runtime Observability cards while preserving event-derived Internal Status semantics. CPU/disk/network telemetry is explicit unavailable until a source of truth exists. User reported Canary/Sentinel completed after `349ac75` / PR #87; Atlas deployment not reported |
| P4-F07 | External Monitoring Integration | Planned | Prometheus/Grafana or external platform state is shown as connected/stale/unavailable; deep links are configurable | Config tests, local Prom/Grafana smoke, unavailable-state smoke | Batch 1D adds validated `PROMETHEUS_URL` / `GRAFANA_URL` configuration state and deep-link display only. Active probes, stale checks, datasource validation, and embedded dashboards remain future work |
| P4-F08 | Real Upstream Evidence Track | Planned | OpenRouter-compatible and direct Anthropic-native flows have target evidence for non-stream and stream where supported | Approved credentials, guarded smoke scripts, call log/billing evidence | Credential-gated |
| P4-F09 | Real SSO Evidence Track | Planned | Real Logto or approved enterprise OIDC browser login passes with claim mapping and audit evidence | Approved IdP client, browser smoke, audit/log redaction check | Logto remains external/optional in PRD4 |
| P4-F10 | Auth UX Hardening | Canary passed | Password inputs support hidden-by-default visibility toggle with accessible controls | Frontend tests, build, visual smoke if needed | Batch 1A adds shared `PasswordField` for login, change-password, and admin user password fields; no auth semantic changes. Canary PASS 6/6; User reported Sentinel completed after `620794b` / PR #84; Atlas deployment not reported |
| P4-F11 | Production Evidence Promotion | Planned | License, webhook, upstream, SSO, and selected ops rows only move to Production verified with target evidence | Evidence files under `docs/ops/evidence/`, matrix/status updates | Local-only evidence is not enough |
| P4-F12 | Security, Privacy, And Redaction Guardrails | Planned | Usage, trace, runtime, SSO, upstream, and evidence outputs hide restricted fields | RBAC negative tests, evidence grep for secret patterns, docs review | Prompt/response plaintext remains disabled |
| P4-F13 | PRD5 Backlog Consolidation | Planned | Deferred scope is recorded in `docs/planning/PRD5-BACKLOG.md` with reason and prerequisites | Docs review and links from PRD4 | Prevents scope creep |

## PRD5 Deferred Capabilities

| Capability | Status | Reason |
|------------|--------|--------|
| Full Scheduler V2 | Deferred to PRD5 | Requires stable usage, quota, trace, and capacity data before gateway scheduling rewrite |
| Runtime plugin platform and marketplace | Deferred to PRD5 | Requires manifest, sandbox, signing, lifecycle, permissions, compatibility, and security model |
| Logto or full IAM rewrite into UniRouter core | Deferred to PRD5 | Would make UniRouter responsible for a full identity platform and long-term security patching |
| Kubernetes/Helm/Terraform managed HA product | Deferred to PRD5 | Requires chosen target stack, cloud/VPC assumptions, managed DB/Redis, PITR, and failover drills |
| Budget hard-block enforcement | Deferred to PRD5 | Changes admission, wallet, billing, stream settlement, idempotency, and user error semantics |
| Runtime cache or prompt/response plaintext storage | Deferred to PRD5 | Requires tenant isolation, opt-in, encryption, retention, audit-on-read, purge, and billing correctness |
| Full SDK suite | Deferred to PRD5 | Should wait until typed API response contracts are stable |
| New persistent role system | Deferred to PRD5 | Requires new authority surfaces, route guards, service-account model, and audit behavior |
| Public SaaS, reseller, payment, invoice, coupon, campaign, support CRM | Out of scope | Not part of PRD4 local-deploy enterprise MaaS scope |

## Batch 0 Verification

PRD4 Batch 0 is documentation-only and should verify:

```bash
rg -n "P4-F01|P4-F13|PRD5|PlungeIn|API key owner|Status Center|Production verified" docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/planning/PRD5-BACKLOG.md docs/governance/adr
rg -n "docs/PRD-Phase4.md|PRD4-ACCEPTANCE-MATRIX|PRD5-BACKLOG" docs/README.md docs/DOC-INVENTORY.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md
git diff --check
```

No Go, frontend, migration, API route, deployment config, or runtime behavior
change is expected for Batch 0.

## Batch 1A Verification

PRD4 Batch 1A verifies Auth UX hardening only:

```bash
rg -n "PasswordField|显示密码|隐藏密码|visibilityResetKey" frontend/src/ui/form frontend/src/pages/LoginPage.tsx frontend/src/pages/SettingsPage.tsx frontend/src/pages/UsersPage.tsx frontend/src/pages/__tests__/LoginPage.test.tsx
npm --prefix frontend run test -- PasswordField LoginPage
npm --prefix frontend test
npm --prefix frontend run build
rg -n "PRD4-AUTHUX-001|P4-F10|Batch 1A|Auth UX" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md
git diff --check
```

No backend API, migration, password policy, RBAC, SSO/OIDC, session, token,
pricing, trace, runtime observability, or deployment behavior change is expected
for Batch 1A.

## Batch 1B Verification

PRD4 Batch 1B verifies Usage And Cost Attribution foundation:

```bash
rg -n "owner_user_id|key_type|scope=my|group_by=owner|group_by=key_type" migrations internal frontend/src
rg -n "PRD4-USAGE-001|P4-F02|Batch 1B|Usage And Cost Attribution" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md
go test ./internal/admin ./internal/middleware ./internal/gateway
go test ./...
npm --prefix frontend test -- --run src/api/__tests__/client.test.ts src/pages/__tests__/UsageAnalyticsPage.test.tsx
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

No Trace UI, Runtime Status Center CPU/memory/network panels, Scheduler V2,
hard quota enforcement, real upstream target evidence, real SSO evidence,
OpenTelemetry ingestion, Logto/IAM rewrite, prompt/response plaintext storage,
or PRD5 runtime plugin marketplace is expected for Batch 1B.

## Batch 1C Verification

PRD4 Batch 1C verifies the Flow/Event/Trace foundation first slice:

```bash
rg -n "request_trace_events|TRACE_EVENTS_ENABLED|TRACE_EVENT_SAMPLE_RATE|GetRequestTrace|getLogTrace|gateway.request.accepted|logging.call_log.recorded" migrations internal cmd frontend/src docs/tech-ref/05-openapi-spec.yaml docs/ops/deployment-runbook.md .env.example docker-compose.yml
rg -n "PRD4-TRACE-001|P4-F04|Batch 1C|Flow/Event/Trace" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md
go test ./internal/config ./internal/gateway ./internal/admin ./cmd/uniroute -count=1
go test ./...
npm --prefix frontend test -- --run LogsPage client ApiDocsPage
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

No OpenTelemetry ingestion/export, Runtime Status Center CPU/memory/network
panels, Prometheus/Grafana integration status, direct global request-id lookup,
full PlungeIn runtime plugin framework, Scheduler V2, trace retention cleanup,
or prompt/response plaintext storage is expected for Batch 1C.

## Batch 1D Verification

PRD4 Batch 1D verifies the Runtime Observability foundation first slice:

```bash
rg -n "RuntimeStatus|runtime/status|RUNTIME_ENVIRONMENT|RUNTIME_DEPLOYMENT|PROMETHEUS_URL|GRAFANA_URL|telemetry_source_not_configured" internal cmd frontend/src docs/tech-ref/05-openapi-spec.yaml docs/ops/deployment-runbook.md .env.example docker-compose.yml
rg -n "PRD4-RUNTIME-001|PRD4-INFRA-001|P4-F06|Batch 1D|Runtime Observability|runtime/status" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md
go test ./internal/config ./internal/admin ./cmd/uniroute -count=1
go test ./...
npm --prefix frontend test -- --run InternalStatusPage client ApiDocsPage navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

No host CPU utilization, disk usage, network throughput, node exporter/cloud
collector, cluster inventory, Prometheus/Grafana active probe, embedded
dashboard, OpenTelemetry export, metric-alert notification routing, or
capacity-aware scheduler behavior is expected for Batch 1D.

## Batch 1E Verification

PRD4 Batch 1E verifies the Quota Vocabulary and Key Quota foundation:

```bash
rg -n "rps_limit|qps_compat_limit|apiKeyQuotaPayload|createRPSLimit|updateRPSLimit|keyQuotaText|RPM / RPS / TPM|RPS 限速" internal frontend/src docs/tech-ref/21-prd4-quota-vocabulary.md
rg -n "DEFAULT_KEY_QPS|QPS|qps_limit|TPS" .env.example docker-compose.yml docs/ops/deployment-runbook.md docs/tech-ref docs/PRD-Phase4.md frontend/src
rg -n "PRD4-QUOTA-001|P4-F03|Batch 1E|Quota Vocabulary|rps_limit|PRD4-RUNTIME-001|Batch 1D" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md docs/PRD-Phase4.md
go test ./internal/admin -run 'Test(CreateRPSLimit|UpdateRPSLimit|APIKeyQuotaPayload)' -count=1
go test ./internal/admin ./cmd/uniroute -count=1
go test ./...
npm --prefix frontend test -- src/utils/__tests__/quota.test.ts src/pages/__tests__/KeysPage.test.tsx src/pages/__tests__/DeveloperConsolePage.test.tsx
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

No Scheduler V2, token reservation, capacity-aware dispatch, key-level TPM
enforcement, budget hard-blocking, quota audit reason/idempotency fields,
`qps_limit` removal, schema migration, real upstream target evidence, or real
SSO target evidence is expected for Batch 1E.
