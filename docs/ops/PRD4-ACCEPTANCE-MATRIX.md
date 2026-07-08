# PRD4 Acceptance Matrix

> Date: 2026-07-07
> Status: PRD4 Batch 0 scope baseline accepted per User report; PRD4 Preflight Hardening passed Canary 6/6; Batch 1A Auth UX hardening passed Canary 6/6 and User reported Sentinel completed; Batch 1B Usage And Cost Attribution passed Canary and User reported Sentinel completed; Batch 1C Flow/Event/Trace Foundation passed Canary and User reported Sentinel completed; Batch 1D Runtime Observability Foundation passed Canary and User reported Sentinel completed; Batch 1E Quota Vocabulary And Key Quota Foundation passed Canary and User reported Sentinel completed; Batch 1F PlungeIn Port Architecture passed Canary and Sentinel merge is reflected by `87bfc17` / PR #89; Batch 1G External Monitoring Integration passed Canary 9/9 and Sentinel merge is reflected by `bb291a4` / PR #90; Batch 1H Real Upstream Evidence Harness passed Canary 11/11 and Sentinel merge is reflected by `d3acfa6` / PR #91; Batch 1I Real SSO Evidence Harness passed Canary 11/11 and Sentinel merge is reflected by `2c02430` / PR #92; Batch 1J Runtime Host Telemetry passed Canary 8/8 and Sentinel merge is reflected by `8707346` / PR #94; Batch 1K Trace Retention And Cleanup passed Canary 10/10 and Sentinel merge is reflected by `1d9ea50` / PR #95; Batch 1L Runtime Monitoring URL Redaction passed Canary 8/8 and Sentinel merge is reflected by `4426efd` / PR #96; Batch 1M Trace Metadata Read-Side Redaction passed Canary 8/8 and Sentinel merge is reflected by `2f43fca` / PR #97; Batch 1N Developer Console Self Usage And Model Cost passed Canary 9/9 and Sentinel merge is reflected by `6194062` / PR #98; Batch 1O API Key Input And Security Header Hardening passed Canary 8/8 and Sentinel merge is reflected by `59721d7` / PR #99. Batch 1P Scope Deferral And Closeout Alignment passed Canary 6/6 and Sentinel merge is reflected by `9e2927f` / PR #100: User decision on 2026-07-08 moves real target execution for `P4-F08` / `P4-F09` out of the current PRD4 closeout; Batch 1H/1I harnesses remain accepted assets and real target E2E is deferred to PRD5. Batch 1Q Production Evidence Promotion passed Canary 8/8 and Sentinel commit is reflected by `f0d34e1`: current target evidence is classified without promoting any row to `Production verified`. Batch 1R Runtime Build Chain Follow-up is fixed awaiting Canary for repo-controllable `PRD4-RUNTIME-002` gaps.
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
| P4-F02 | Usage And Cost Attribution | Canary passed | API key owner usage is visible to the owner; authorized roles see broader scope; totals reconcile by model/key/project/time range | Backend/frontend tests, seeded two-user/two-key E2E, CSV or API evidence | Batch 1B adds owner/type metadata to API keys, owner/type call-log snapshots, `scope=my`, owner/key-type filters and groupings, frontend analytics controls, and personal-key disablement before hard user delete. Historical logs intentionally keep `owner_user_id=NULL` instead of fabricated ownership. Canary passed and User reported Sentinel completed after `7dd8aa0` / PR #85. Batch 1N passed Canary 9/9 and Sentinel merge is reflected by `6194062` / PR #98 as `PRD4-USAGE-002`: Developer Console uses `scope=my` + selected project + model alias grouping to show the current user's own usage, cost, and model breakdown instead of relying only on recent project logs |
| P4-F03 | Quota Vocabulary And Key Quota Foundation | Canary passed | RPM/RPS/TPM/tokens/s terms are stable; key-level quota visibility is present; Scheduler V2 is not implied | Unit/integration tests, docs/UI label checks, migration evidence if schema changes | Batch 1E adds preferred `rps_limit`, structured key `quota` response data, Admin API Keys / Developer Console quota UI, and `docs/tech-ref/21-prd4-quota-vocabulary.md`. Existing `qps_limit` remains compatibility storage/API field. No Scheduler V2, key-level TPM enforcement, budget hard-block, quota audit reason/idempotency fields, or schema migration. User reported Canary/Sentinel completed after `a1acdb6` / PR #88; Atlas deployment not reported |
| P4-F04 | Flow/Event/Trace Foundation | Canary passed | One request can be investigated across gateway, billing, routing, provider, call logs, and audit metadata | Request-path E2E by `request_id`, redaction negative checks | Batch 1C adds metadata-only `request_trace_events`, trace sampling config, gateway/billing/routing/provider/call-log events for chat and embeddings, `GET /api/v1/logs/{id}/trace` with project usage RBAC, and Logs detail rendering. User reported Canary/Sentinel completed after `4f62e34` / PR #86. Batch 1K passed Canary 10/10 and Sentinel merge is reflected by `1d9ea50` / PR #95 for trace-event retention config and cleanup wiring. No OpenTelemetry, Runtime Status Center, Prometheus/Grafana integration status, direct global request-id lookup, partitioning, archival, or prompt/response plaintext |
| P4-F05 | PlungeIn Port Architecture | Canary passed | Observe/Act/Enrich/Decide/Mutate ports are documented; PRD4 only allows observation-first ports | ADR review, docs consistency, later port tests if code exists | Batch 1F accepts ADR 0001 for PRD4 scope and adds `docs/tech-ref/22-prd4-plungein-port-contract.md`. It is docs-only: no runtime plugin loading, manifest, sandbox, marketplace, third-party installation, or mutation of auth/billing/routing/wallet/settlement/provider dispatch. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `87bfc17` / PR #89 |
| P4-F06 | Status Center And Runtime Observability | Canary passed | Event Status, Runtime Environment, Capacity Health, and Observability Integrations are separate surfaces; stale/missing telemetry is explicit | Backend/frontend tests, RBAC tests, local runtime smoke | Batch 1D adds `GET /api/v1/runtime/status`, runtime metadata config, app memory, DB/Redis health, recent traffic summary, and same-page Runtime Observability cards while preserving event-derived Internal Status semantics. Batch 1J replaces the earlier CPU/disk/network placeholders with host CPU/disk/network summaries: CPU logical cores and Linux sampled usage when available, disk total/used/free/root mount usage, and network interface counts plus Linux rx/tx counters when available. Canary PASS 8/8 was recorded before Batch 1K overwrote the current `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `8707346` / PR #94. No cluster inventory, raw topology, private IP/hostname/cloud identifiers, OpenTelemetry, metric alerts, or Production verified target evidence is claimed |
| P4-F07 | External Monitoring Integration | Canary passed | Prometheus/Grafana state is shown as connected/stale/misconfigured/unavailable/not_configured; deep links remain configurable; monitoring failures do not break the main app | Backend httptest probes, frontend status rendering tests, config/docs grep, broad Go/frontend regression | Batch 1G actively probes configured `PROMETHEUS_URL` and `GRAFANA_URL` from `GET /api/v1/runtime/status`: Prometheus `/-/ready` plus `up{job="unirouter"}`, Grafana `/api/health`, latency, reason, scrape status, version, and external links. Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `bb291a4` / PR #90. It does not embed dashboards, validate Grafana datasources, add metrics auth, or implement a generic monitoring connector framework |
| P4-F08 | Real Upstream Evidence Harness | Deferred to PRD5 | PRD4 keeps the guarded harness only; real OpenRouter-compatible and direct Anthropic-native non-stream/stream target execution is not part of current closeout | Batch 1H harness evidence; PRD5 requires approved credentials, guarded smoke scripts, call log/billing/trace evidence | Batch 1H adds the guarded evidence harness, preflight, seed, runner, and tech reference. Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `d3acfa6` / PR #91. User decision on 2026-07-08: no approved real upstream environment exists for current PRD4, so real-provider E2E is unimplemented in this phase and tracked as `PRD5-UPSTREAM-001` |
| P4-F09 | Real SSO Evidence Harness | Deferred to PRD5 | PRD4 keeps the guarded harness only; real Logto or enterprise OIDC browser-login target execution is not part of current closeout | Batch 1I harness evidence; PRD5 requires approved IdP client, browser smoke, audit/log redaction evidence | Batch 1I adds the guarded real SSO evidence harness, browser runner, read-only audit checker, and tech reference. Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `2c02430` / PR #92. User decision on 2026-07-08: no approved real SSO environment exists for current PRD4, so real browser-login E2E is unimplemented in this phase and tracked as `PRD5-SSO-001` |
| P4-F10 | Auth UX Hardening | Canary passed | Password inputs support hidden-by-default visibility toggle with accessible controls | Frontend tests, build, visual smoke if needed | Batch 1A adds shared `PasswordField` for login, change-password, and admin user password fields; no auth semantic changes. Canary PASS 6/6; User reported Sentinel completed after `620794b` / PR #84; Atlas deployment not reported |
| P4-F11 | Production Evidence Promotion | Canary passed | License, webhook, runtime/build-chain, UI confirmation, and selected ops rows only move to Production verified with target evidence; `P4-F08` / `P4-F09` are excluded from current PRD4 promotion | `docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md`, matrix/status updates | Batch 1Q classifies the final target report as useful target-smoke evidence for selected PRD4 areas, but promotes no PRD4 row to `Production verified`: the final report covers the PRD4 Batch 1N target state, later security fixes landed in Batch 1O, and runtime/build-chain gaps remained open as `PRD4-RUNTIME-002` at that point. Batch 1R subsequently fixes repo-controllable runtime/build-chain gaps awaiting Canary; target DB cleanup is split to `PRD4-DEPLOY-002`. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel commit is reflected by `f0d34e1`. Real upstream and real SSO target evidence are PRD5 backlog items after the 2026-07-08 User decision |
| P4-F12 | Security, Privacy, And Redaction Guardrails | Canary passed | Usage, trace, runtime, SSO, upstream, and evidence outputs hide restricted fields | RBAC negative tests, evidence grep for secret patterns, docs review | Batch 1L implements the accepted runtime redaction slice: `GET /api/v1/runtime/status` strips URL userinfo, query strings, and fragments from Prometheus/Grafana `url` and `health_url` response fields while probes continue using the raw configured URL. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `4426efd` / PR #96. Batch 1M passed Canary 8/8 and Sentinel merge is reflected by `2f43fca` / PR #97: gateway trace writes and `GET /api/v1/logs/{id}/trace` responses share one metadata sanitizer. Batch 1O passed Canary 8/8 and Sentinel merge is reflected by `59721d7` / PR #99 as `PRD4-SECURITY-001`: API key display-name validation, negative `project_id` rejection, baseline security headers, and standard 404 root catch-all behavior. It does not change billing, routing, RBAC, migrations, trace storage, SSO, upstream, wallet, provider dispatch, frontend behavior, or Production verified claims |
| P4-F13 | PRD5 Backlog Consolidation | Canary passed | Deferred scope is recorded in `docs/planning/PRD5-BACKLOG.md` with reason and prerequisites | Docs review and links from PRD4 | Batch 1P adds `PRD5-UPSTREAM-001` and `PRD5-SSO-001`, moves `P4-F08` / `P4-F09` real target execution out of current PRD4, and keeps Batch 1H/1I harnesses as accepted reusable assets. Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `9e2927f` / PR #100 |

## PRD5 Deferred Capabilities

| Capability | Status | Reason |
|------------|--------|--------|
| Full Scheduler V2 | Deferred to PRD5 | Requires stable usage, quota, trace, and capacity data before gateway scheduling rewrite |
| Runtime plugin platform and marketplace | Deferred to PRD5 | Requires manifest, sandbox, signing, lifecycle, permissions, compatibility, and security model |
| Real upstream target-environment E2E | Deferred to PRD5 | No approved target credentials/environment exist for current PRD4; Batch 1H keeps only reusable guarded harness assets |
| Real SSO target-environment E2E | Deferred to PRD5 | No approved Logto or enterprise OIDC target/test user exists for current PRD4; Batch 1I keeps only reusable guarded harness assets |
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

## Batch 1F Verification

PRD4 Batch 1F verifies the PlungeIn Port Architecture contract:

```bash
rg -n "PlungeIn|Observe|Act|Enrich|Decide|Mutate|PRD4 Batch 1F|PRD4-PLUNGEIN-001|P4-F05" docs/tech-ref/22-prd4-plungein-port-contract.md docs/governance/adr/ADR-0001-prd4-plungein-ports.md docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/tech-ref/README.md
rg -n "runtime plugin platform|manifest|dynamic loading|sandbox|marketplace|third-party|Mutate|auth|billing|routing|wallet|settlement|provider dispatch" docs/tech-ref/22-prd4-plungein-port-contract.md docs/governance/adr/ADR-0001-prd4-plungein-ports.md docs/planning/PRD5-BACKLOG.md docs/issues/OPEN-ISSUES.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md
git status --short
git diff --check
```

No Go, frontend, migration, API route, deployment config, runtime plugin loader,
manifest, sandbox, marketplace, third-party install path, or core auth/billing/
routing/wallet/settlement/provider-dispatch mutation is expected for Batch 1F.

## Batch 1G Verification

PRD4 Batch 1G verifies External Monitoring Integration:

```bash
rg -n "runtimePrometheusIntegration|runtimeGrafanaIntegration|prometheusUniRouterTargetQuery|scrape_status|health_url|auth_required|integrationMeta|integrationTone" internal/admin frontend/src/pages frontend/src/ui
rg -n 'PRD4-MONITORING-001|P4-F07|Batch 1G|External Monitoring|PROMETHEUS_URL|GRAFANA_URL|up\{job="unirouter"\}|connected|stale|misconfigured|unavailable' docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md docs/ops/deployment-runbook.md .env.example
go test ./internal/admin -run 'TestRuntimeStatus|TestRuntimeOverallStatus' -count=1
go test ./internal/config ./internal/admin ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run InternalStatusPage client ApiDocsPage navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

No embedded Prometheus/Grafana dashboard, Grafana datasource validation, new
metrics authentication strategy, OpenTelemetry export, node exporter/cloud host
telemetry collector, cluster inventory, generic monitoring connector framework,
or PRD5 runtime plugin platform is expected for Batch 1G.

## Batch 1H Verification

PRD4 Batch 1H verifies the Real Upstream Evidence Harness and its guardrails:

```bash
rg -n "prd4-real-upstream-smoke|prd4_real_upstream_seed|prd4_real_upstream_smoke|OPENROUTER_MODEL|ANTHROPIC_MODEL|approved-target-real-upstream|PRD4 Batch 1H|P4-F08|PRD4-UPSTREAM-001" scripts docs/tech-ref/23-prd4-real-upstream-evidence.md docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/tech-ref/README.md docs/CODEBASE-MAP.md
scripts/prd4-real-upstream-smoke.sh --preflight
node --check scripts/prd4_real_upstream_smoke.mjs
go test ./internal/adapters ./internal/gateway ./internal/admin ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Canary should not run real-provider mode unless the target environment supplies
approved `OPENROUTER_API_KEY`, `OPENROUTER_MODEL`, `ANTHROPIC_API_KEY`,
`ANTHROPIC_MODEL`, matching backend encryption/API-key-hash secrets, and
`UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`.

After the 2026-07-08 User decision, real-provider mode is not part of current
PRD4 closeout. Batch 1H remains accepted as reusable harness evidence only;
actual target execution is tracked as `PRD5-UPSTREAM-001`.

Batch 1H acceptance is limited to harness correctness, explicit mutation
guardrails, safe preflight behavior, evidence redaction, and documentation
consistency. `P4-F08` real target execution is not part of current PRD4
closeout after the 2026-07-08 User decision; it is tracked as
`PRD5-UPSTREAM-001`.

## Batch 1I Verification

PRD4 Batch 1I verifies the Real SSO Evidence Harness and its guardrails:

```bash
rg -n "prd4-real-sso-smoke|prd4_real_sso_smoke|prd4_real_sso_audit_check|approved-target-real-sso|PRD4 Batch 1I|P4-F09|PRD4-SSO-001|Logto|real SSO" scripts docs/tech-ref/24-prd4-real-sso-evidence.md docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/tech-ref/README.md docs/CODEBASE-MAP.md
scripts/prd4-real-sso-smoke.sh --preflight
node --check scripts/prd4_real_sso_smoke.mjs
go run ./scripts/prd4_real_sso_audit_check.go
go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run LoginPage client navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

The `go run ./scripts/prd4_real_sso_audit_check.go` command is expected to fail
in an unconfigured workspace before reading the database, because
`UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso` is absent.

Canary should not run real browser mode unless the target environment supplies
approved IdP/test-user credentials, console/backend URLs, issuer URL, DB DSN,
and `UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`.

After the 2026-07-08 User decision, real browser mode is not part of current
PRD4 closeout. Batch 1I remains accepted as reusable harness evidence only;
actual target execution is tracked as `PRD5-SSO-001`.

Batch 1I acceptance is limited to harness correctness, explicit browser/IdP/DB
guardrails, safe preflight behavior, evidence redaction, and documentation
consistency. `P4-F09` real target execution is not part of current PRD4 closeout
after the 2026-07-08 User decision; it is tracked as `PRD5-SSO-001`.
Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `2c02430` / PR #92.

## Batch 1J Verification

PRD4 Batch 1J verifies Runtime Host Telemetry under `P4-F06`:

```bash
rg -n "runtimeHostTelemetry|runtimeCPUStatus|runtimeDiskStatus|runtimeNetworkStatus|usage_ratio|logical_cores|active_interface_count|rx_bytes|tx_bytes" internal/admin frontend/src/pages/InternalStatusPage.tsx frontend/src/pages/__tests__/InternalStatusPage.test.tsx
go test ./internal/admin -run 'TestRuntimeStatus|TestLinuxCPUUsageRatio' -count=1
go test ./internal/admin -count=1
go test ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run InternalStatusPage client navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Acceptance is limited to structured host CPU/disk/network summaries in
`GET /api/v1/runtime/status` and separate `/status` cards. It does not include
node exporter, cloud metadata, Kubernetes/cluster inventory, raw private
topology, OpenTelemetry, metric-alert notification events, or Production
verified target-environment evidence. Canary PASS 8/8 is recorded in
`docs/agent-workflows/VERIFY-RESULT.md`, and Sentinel merge is reflected by
`8707346` / PR #94.

## Batch 1K Verification

PRD4 Batch 1K verifies Trace Retention And Cleanup under `PRD4-TRACE-002`:

```bash
rg -n "TRACE_EVENT_RETENTION_DAYS|CleanupOldDataWithTraceRetention|request_trace_events|LOG_RETENTION_DAYS" internal cmd .env.example docker-compose.yml docs/ops/deployment-runbook.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md
go test ./internal/config -count=1
go test ./internal/admin -run 'TestCleanupOldData|TestCleanupOldDataWithTraceRetention' -count=1
go test ./internal/admin -count=1
go test ./cmd/uniroute -count=1
go test ./... -count=1
git diff --check
```

Acceptance is limited to trace-event retention configuration and cleanup wiring:
`TRACE_EVENT_RETENTION_DAYS` defaults to `LOG_RETENTION_DAYS`, validates
non-negative values, is exposed in env/compose/runbook, and the existing cleanup
loop deletes expired `request_trace_events`. It does not include trace
partitioning, archival, OpenTelemetry ingestion/export, prompt/response
plaintext storage, runtime cache, or Production verified target-environment
evidence.

Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `1d9ea50` / PR #95.

## Batch 1L Verification

PRD4 Batch 1L verifies Runtime Monitoring URL Redaction under
`PRD4-REDACTION-001` / `P4-F12`:

```bash
rg -n "monitoringDisplayURL|RedactsMonitoringURLs|PRD4-REDACTION-001|P4-F12|Batch 1L" internal/admin docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md
go test ./internal/admin -run 'TestRuntimeStatus_RedactsMonitoringURLsButUsesRawProbeURL|TestRuntimeStatus_ReportsExternalMonitoringProblems|TestRuntimeStatus_ReturnsRuntimeHealthAndHostTelemetry' -count=1
go test ./internal/admin -count=1
go test ./... -count=1
git diff --check
```

Acceptance is limited to runtime monitoring display URL redaction:
Prometheus/Grafana `url` and `health_url` response fields strip URL userinfo,
query strings, and fragments, while monitoring probes continue to use the raw
configured URL. It does not add monitoring auth config, embedded dashboards,
OpenTelemetry export, node exporter/cloud metadata, cluster inventory, metric
alerts, frontend layout changes, route/RBAC changes, or Production verified
target-environment evidence.

Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `4426efd` / PR #96.

## Batch 1M Verification

PRD4 Batch 1M verifies Trace Metadata Read-Side Redaction under
`PRD4-REDACTION-002` / `P4-F12`:

```bash
rg -n "traceutil|SanitizeMetadata|sanitizeTraceResponseMetadata|TestSanitizeTraceResponseMetadata|PRD4-REDACTION-002|P4-F12|Batch 1M" internal/admin internal/gateway internal/traceutil docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md
go test ./internal/traceutil -count=1
go test ./internal/admin -run 'TestGetRequestTrace|TestSanitizeTraceResponseMetadata' -count=1
go test ./internal/gateway -run 'TestRecordTraceEvent_SanitizesMetadataAndSnapshotsOwner' -count=1
go test ./internal/admin -count=1
go test ./... -count=1
git diff --check
```

Acceptance is limited to shared trace metadata sanitization and the trace API
response boundary: gateway write-side trace metadata and admin read-side trace
responses now use the same internal allowlist; historical or manually inserted
trace metadata is allowlisted after JSON unmarshalling, safe scalar fields
remain available for request investigation, restricted keys such as `api_key`,
`authorization`, `access_token`, `request_body`, and `prompt` are dropped,
unsupported nested values are not returned, and safe strings are
trimmed/truncated. It does not change trace sampling/storage semantics, schema,
frontend layout, OpenTelemetry, prompt/response plaintext storage, billing,
routing, auth, SSO, upstream, wallet, provider dispatch, or Production verified
target-environment evidence.

Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `2f43fca` / PR #97.

## Batch 1N Verification

PRD4 Batch 1N verifies Developer Console Self Usage And Model Cost under
`PRD4-USAGE-002` / `P4-F02`:

```bash
rg -n "我的模型用量|scope: 'my'|group_by: 'alias'|PRD4-USAGE-002|P4-F02|Batch 1N" frontend/src/pages/DeveloperConsolePage.tsx frontend/src/pages/__tests__/DeveloperConsolePage.test.tsx docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md
go test ./internal/admin -run 'TestAnalyticsConditionsMyScope|TestAnalyticsConditionsRejectsConflictingOwnerScope' -count=1
npm --prefix frontend test -- --run DeveloperConsolePage
npm --prefix frontend test -- --run UsageAnalyticsPage client
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Acceptance is limited to Developer Console self-service usage visibility:
Developer Console uses existing Usage Analytics with `scope=my`, the selected
project, and `group_by=alias`; KPI tiles use the owner-attributed summary when
available; a model usage table shows the current user's request, token, cost,
success/failure, and latency breakdown by model/alias; and self-usage analytics
failure does not block project keys, approved models, recent calls, playground,
or lookup workflows. It does not change backend usage SQL, billing, wallet
settlement, API key ownership, RBAC, routes, migrations, prompt/response
storage, trace, runtime status, SSO, upstream, or provider dispatch.

Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `6194062` / PR #98. Target-environment final
test evidence for the deployed PRD4 Batch 1N state is archived at
`docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`.

## Batch 1O Verification

PRD4 Batch 1O verifies API Key Input And Security Header Hardening under
`PRD4-SECURITY-001` / `P4-F12`:

```bash
rg -n "normalizeAPIKeyName|SecurityHeaders|TestSecurityHeaders|TestRootHandlerDoesNotExposeHardcodedVersion|PRD4-SECURITY-001|Batch 1O|UNIROUTERS-TEST-REPORT-FINAL-20260708" internal/admin internal/middleware cmd/uniroute docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md docs/CODEBASE-MAP.md
go test ./internal/admin -run 'Test(CreateAPIKey_RejectsUnsafeNameAndOversizedName|CreateAPIKey_RejectsNegativeProjectID|UpdateAPIKey_RejectsUnsafeName|UpdateAPIKey_RejectsNegativeProjectID)$' -count=1
go test ./internal/middleware -run TestSecurityHeaders -count=1
go test ./cmd/uniroute -run 'TestRootHandler(DoesNotExposeHardcodedVersion|ReturnsStandardNotFoundForUnknownPath)' -count=1
go test ./internal/admin ./internal/middleware ./cmd/uniroute -count=1
go test ./... -count=1
git diff --check
```

Acceptance is limited to the application-code issues from
`docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`: API key create
and update reject unsafe display names, overlong names, and negative
`project_id`; all HTTP responses include baseline security headers; and unknown
GET paths return a standard 404 without root service/version data. It does not
change billing, routing, RBAC, migrations, trace storage, SSO, upstream, wallet,
provider dispatch, frontend behavior, Go toolchain version, Grafana target
environment config, runtime `ldflags`, container healthcheck wiring, target test
database cleanup, or Production verified claims.

Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `59721d7` / PR #99.

## Batch 1P Verification

PRD4 Batch 1P verifies Scope Deferral And Closeout Alignment under `P4-F08`,
`P4-F09`, and `P4-F13`:

```bash
rg -n "PRD5-UPSTREAM-001|PRD5-SSO-001|P4-F08|P4-F09|Deferred to PRD5|real target execution|real upstream/SSO harness|2026-07-08" docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/planning/PRD5-BACKLOG.md docs/planning/PRD4-BACKLOG.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md docs/agent-workflows/FIX-SIGNAL.md
awk -F'|' '/^\| P4-F[0-9]+ / {gsub(/^ +| +$/, "", $4); count[$4]++; total++} END {print "total=" total; for (s in count) print s "=" count[s]}' docs/ops/PRD4-ACCEPTANCE-MATRIX.md
git diff --check
```

Acceptance is limited to documentation/status alignment after the User's
2026-07-08 scope decision. `P4-F08` and `P4-F09` real target execution are
deferred to PRD5 because no approved real environments are currently available;
Batch 1H/1I harnesses remain accepted reusable assets. `P4-F13` passed Canary
because the deferred real-upstream and real-SSO target E2E items are explicitly
recorded in `docs/planning/PRD5-BACKLOG.md`.

No Go, frontend, migration, API route, runtime behavior, deployment script,
evidence harness code, or Production verified claim changes are expected for
Batch 1P.

Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`, and
Sentinel merge is reflected by `9e2927f` / PR #100.

## Batch 1Q Verification

PRD4 Batch 1Q verifies Production Evidence Promotion under `P4-F11`:

```bash
rg -n "P4-F11|Production Evidence Promotion|PRD4-B1Q-production-evidence-promotion-20260708|No PRD4 matrix row is promoted|Target smoke evidence only|PRD4-RUNTIME-002|PRD5-UPSTREAM-001|PRD5-SSO-001" docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/CURRENT-STATUS.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/agent-workflows/FIX-SIGNAL.md
awk -F'|' '/^\| P4-F[0-9]+ / {gsub(/^ +| +$/, "", $4); count[$4]++; total++} END {print "total=" total; for (s in count) print s "=" count[s]}' docs/ops/PRD4-ACCEPTANCE-MATRIX.md
git diff --check
```

Acceptance is limited to classifying existing target evidence and enforcing the
`Production verified` boundary. Batch 1Q intentionally promotes no PRD4 matrix
row to `Production verified` because the final target report is for the Batch 1N
target state, Batch 1O fixed later application-code findings, and
`PRD4-RUNTIME-002` remains open.

No Go, frontend, migration, API route, runtime behavior, deployment script,
evidence harness code, or production deployment change is expected for Batch 1Q.

## Batch 1R Verification

PRD4 Batch 1R verifies repo-controllable Runtime Build Chain Follow-up under
`PRD4-RUNTIME-002`:

```bash
rg -n "go1.25.12|toolchain go1.25.12|buildVersion|buildCommit|RUNTIME_VERSION|RUNTIME_COMMIT|http://grafana:3000|healthcheck|PRD4-RUNTIME-002|PRD4-DEPLOY-002|PRD4-B1R-runtime-build-chain-local-20260708" go.mod Dockerfile docker-compose.yml .env.example cmd/uniroute/main.go cmd/uniroute/main_test.go docs/ops/deployment-runbook.md docs/issues/OPEN-ISSUES.md docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md docs/CODEBASE-MAP.md docs/agent-workflows/FIX-SIGNAL.md
go test ./cmd/uniroute ./internal/config ./internal/admin -run 'TestApplyBuildRuntimeMetadata|TestLoad_RuntimeExposureEnvOverrides|TestRuntimeStatus' -count=1
go test ./... -count=1
go build -ldflags='-X main.buildVersion=20260708.test -X main.buildCommit=abc1234' -o /tmp/uniroute-prd4-b1r ./cmd/uniroute
rm /tmp/uniroute-prd4-b1r
go env GOVERSION GOTOOLCHAIN
ruby -e 'require "yaml"; YAML.load_file("docker-compose.yml"); YAML.load_file("docker-compose.canary.yml") if File.exist?("docker-compose.canary.yml"); puts "yaml ok"'
git diff --check
```

If Docker is available, Canary should additionally run:

```bash
docker compose config
```

Acceptance is limited to the repo-controllable runtime/build-chain items from
`PRD4-RUNTIME-002`: Go patch toolchain pin, Docker builder patch version,
version/commit `ldflags`, runtime metadata fallback, compose Grafana service URL,
compose `RUNTIME_ENVIRONMENT`, backend container healthcheck, env/runbook docs,
and local evidence. It does not delete target databases, deploy production, or
claim `Production verified`.
