# PRD4 Acceptance Matrix

> Date: 2026-07-07
> Status: Proposed matrix for PRD4; no PRD4 runtime implementation accepted yet.
> Source: `docs/PRD-Phase4.md`, `docs/planning/PRD4-BACKLOG.md`, current
> PRD2/PRD3 evidence boundaries, and User PRD4 scope decisions.

## Status Values

| Status | Meaning |
|--------|---------|
| Planned | Accepted into PRD4 scope but not implemented or verified |
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
| P4-F01 | PRD4 Scope And Evidence Baseline | Planned | `docs/PRD-Phase4.md`, this matrix, PRD5 backlog, and ADRs exist; current docs route to them; no runtime code changes | Docs grep, `git diff --check`, Canary doc consistency review | Batch 0 only |
| P4-F02 | Usage And Cost Attribution | Planned | API key owner usage is visible to the owner; authorized roles see broader scope; totals reconcile by model/key/project/time range | Backend/frontend tests, seeded two-user/two-key E2E, CSV or API evidence | Shared/service key handling must be explicit |
| P4-F03 | Quota Vocabulary And Key Quota Foundation | Planned | RPM/RPS/TPM/tokens/s terms are stable; key-level quota visibility is present; Scheduler V2 is not implied | Unit/integration tests, docs/UI label checks, migration evidence if schema changes | Avoid ambiguous `TPS` in UI |
| P4-F04 | Flow/Event/Trace Foundation | Planned | One request can be investigated across gateway, billing, routing, provider, call logs, and audit metadata | Request-path E2E by `request_id`, redaction negative checks | Metadata only; no prompt/response plaintext |
| P4-F05 | PlungeIn Port Architecture | Planned | Observe/Act/Enrich/Decide/Mutate ports are documented; PRD4 only allows observation-first ports | ADR review, docs consistency, later port tests if code exists | No arbitrary runtime plugin in PRD4 |
| P4-F06 | Status Center And Runtime Observability | Planned | Event Status, Runtime Environment, Capacity Health, and Observability Integrations are separate surfaces; stale/missing telemetry is explicit | Backend/frontend tests, RBAC tests, local runtime smoke | Current Internal Status semantics remain event-derived |
| P4-F07 | External Monitoring Integration | Planned | Prometheus/Grafana or external platform state is shown as connected/stale/unavailable; deep links are configurable | Config tests, local Prom/Grafana smoke, unavailable-state smoke | Do not duplicate full Grafana UI |
| P4-F08 | Real Upstream Evidence Track | Planned | OpenRouter-compatible and direct Anthropic-native flows have target evidence for non-stream and stream where supported | Approved credentials, guarded smoke scripts, call log/billing evidence | Credential-gated |
| P4-F09 | Real SSO Evidence Track | Planned | Real Logto or approved enterprise OIDC browser login passes with claim mapping and audit evidence | Approved IdP client, browser smoke, audit/log redaction check | Logto remains external/optional in PRD4 |
| P4-F10 | Auth UX Hardening | Planned | Password inputs support hidden-by-default visibility toggle with accessible controls | Frontend tests, build, visual smoke if needed | No auth semantic changes |
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
