# PRD5 Backlog

> Date: 2026-07-08
> Status: Deferred backlog for capabilities intentionally not implemented in
> PRD4 first scope.
> Source: PRD4 scope confirmation, earlier PRD2/PRD3 deferred boundaries, and
> 2026-07-08 User direction for X-Insight integration scope discovery.

## Purpose

This file records important capabilities that remain valuable but are too large,
high-risk, environment-dependent, strategically premature, or still in
cross-project discovery. Listing an item here prevents PRD4 labels, partial
implementation, or discussion notes from implying that the capability is already
accepted.

## Current PRD5 Discovery Direction

The first PRD5 discovery pass should start from the deferred items below and the
shared UniRouter/X-Insight communication folder:

```text
/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation
```

For the X-Insight-related items, UniRouter should focus on high-fidelity
structured evidence emitted from gateway-native execution points. X-Insight is
expected to own the broader enterprise AI/Agent observability and governance
platform. This boundary remains a discovery assumption until the X-Insight team
answers the open request in the shared folder and the User confirms the final
split.

Current active PRD5 planning, Batch 1A/1B/1C completed implementation, Batch 1D
upstream request correlation, Batch 1E event capture-gap emission, Batch 1F
payload capture mode configuration, Batch 1G trusted evidence context intake,
Batch 1H gateway span hierarchy, Batch 1I payload policy decision evidence,
Batch 1J provider upstream capture-gap evidence, and Batch 1K Gate A snapshot
handoff-note support, and Batch 1L component span refinement are tracked in:

- `docs/PRD-Phase5.md`
- `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
- `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`
- `docs/planning/PRD5-COMPETITOR-COMPARISON.md`
- `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md`
- `docs/tech-ref/26-prd5-evidence-field-contract.md`

This backlog remains the source record for deferred items that are not yet part
of the active PRD5 implementation train.

## Deferred Items

| ID | Capability | Why Not PRD4 | Prerequisites To Reopen |
|----|------------|--------------|--------------------------|
| PRD5-SCHED-001 | Full Scheduler V2 | Rewrites gateway dispatch, capacity reservation, model-family queues, priority fairness, and error semantics | Stable PRD4 usage/cost, quota visibility, request trace data, capacity health, and clear rollout/shadow mode |
| PRD5-PLUGIN-001 | Runtime plugin platform | Requires manifest schema, runtime loading, sandboxing, permissions, signing, lifecycle, version compatibility, and secret isolation | Accepted PlungeIn port ADR, stable core events, security model, plugin API compatibility rules |
| PRD5-MARKET-001 | Plugin marketplace / third-party installation | Adds packaging, trust, distribution, update, license, and support obligations | Runtime plugin platform, signing infrastructure, review policy, install/uninstall rollback |
| PRD5-IAM-001 | Logto/IAM rewrite into UniRouter core | Would make UniRouter responsible for a full identity platform, including SAML/SCIM/MFA/social login/security patching | Clear decision to become an identity platform, license review, migration plan, long-term patch process |
| PRD5-DEPLOY-001 | Kubernetes/Helm/Terraform managed HA product | Target-stack dependent and requires production-grade DB/Redis HA, PITR, off-server backup, and failover testing | Chosen target stack, cloud assumptions, secret management, backup encryption, restore/failover drills |
| PRD5-INFRA-001 | Multi-instance infrastructure observability and topology | PRD4 implements single-node Runtime Observability, external Prometheus/Grafana health, and host summaries only; cluster inventory, topology, collectors, and alert routing require a target architecture | PRD4 Status Center baseline, chosen deployment topology, collector access model, RBAC/redaction policy, stale-data behavior, alert ownership |
| PRD5-BUDGET-001 | Budget hard-block enforcement | Changes admission, wallet, stream settlement, idempotency, reconciliation, and user-facing error behavior | PRD4 policy model, billing trace, concurrency safety tests, refund/cancel/timeout invariants |
| PRD5-CACHE-001 | Runtime cache / prompt-response plaintext storage | High privacy, tenant-isolation, retention, billing, and correctness risk | Explicit opt-in model, encryption, retention, purge/export/delete, audit-on-read, billing rules |
| PRD5-SDK-001 | Full official SDK suite | Freezes API details too early if typed response schemas are still evolving | Stable OpenAPI response schemas, semver policy, generated SDK tests, compatibility commitments |
| PRD5-ROLE-001 | New persistent roles and service accounts | Requires authority surfaces beyond current roles and project membership | Role taxonomy, permission matrix, route guard tests, service-account credential model, audit model |
| PRD5-OPS-001 | Full maintenance/SLA/SLO incident operations | Needs incident lifecycle, planned maintenance, communication workflow, SLO policy, and notification governance | PRD4 Status Center baseline, operator roles, notification reliability, customer-visible policy |
| PRD5-UPSTREAM-001 | Real upstream target-environment E2E | No approved real upstream environment, credentials, and model slugs are available for the current PRD4 closeout; PRD4 keeps only guarded harness assets | Approved OpenRouter and direct Anthropic credentials, target environment owner, cost/risk approval, non-stream/stream run plan, cleanup plan |
| PRD5-SSO-001 | Real SSO target-environment E2E | No approved Logto or enterprise OIDC target, test user, and browser-login environment are available for the current PRD4 closeout; PRD4 keeps only guarded harness assets | Approved IdP tenant/client, test user, callback URL, target console/backend URLs, DB audit access, redaction review |
| PRD5-XINSIGHT-001 | X-Insight integration contract and shared working process | X-Insight integration is new PRD5 discovery scope, not part of the PRD4 closeout; the communication folder exists and X-Insight has reported a first discovery command, but importer mapping and Gate A import evidence are not accepted yet | X-Insight response to shared questions, confirmed ownership split, ingestion mode, schema versioning rule, evidence handoff path, User approval, and Batch 1K DSN-free snapshot handoff note |
| PRD5-EVIDENCE-001 | Structured execution-chain evidence fields | PRD4 trace events are a metadata-only foundation but do not yet model multi-agent/multi-tool/multi-business-system chains with explicit structural fields | Batch 1A/1B/1C accepted the field contract, nullable migration, gateway-known structural writes, read-side exposure, snapshot output, and lightweight trace UI. Batch 1D adds provider-returned upstream request ID capture. Batch 1E adds event-level redacted capture-gap emission for sanitized trace metadata. Batch 1F adds restricted payload evidence mode configuration. Batch 1G adds default-off trusted evidence context intake for internal trace/session/actor/delegation fields. Batch 1H adds per-request gateway root span parenting for UniRouter-emitted trace events. Batch 1I adds explicit payload policy decision events. Batch 1J adds `provider_not_returned` capture gaps for provider response events without upstream request IDs. Batch 1L refines repeated same-component event parenting. Remaining prerequisites: broader capture-gap categories beyond current redaction/provider-ID gaps and any backfill require separate approval |
| PRD5-PAYLOAD-001 | Enterprise-configurable prompt/response payload policy | Raw prompt/response storage is privacy-sensitive and should not be silently introduced through trace, cache, or X-Insight integration work | Batch 1F accepts only `metadata_only` and `disabled` runtime payload evidence modes. Batch 1I emits explicit `payload.policy.applied` decision events while keeping `raw_payload_stored=false` and `payload_ref_present=false`. Remaining prerequisites before redacted/hash/reference/raw modes: User approval, UniRouter/X-Insight ownership split, encryption/key management, retention, deletion/export, audit-on-read, redacted summary, hash, and `evidence_ref` behavior |
| PRD5-COMPARE-001 | NewAPI/LiteLLM/one-api and observability-platform comparison | Functional, technical, and UI comparison is needed before feature-chase planning; older research snapshots and X-Insight prior research are inputs but must be refreshed before PRD commitments | Batch 1A/1B/1C accepted the initial comparison and UniRouter/X-Insight ownership split. Refresh again before public claims, adapter implementation, pricing claims, or choosing a new catch-up workstream |
| PRD5-TRACEUI-001 | Lightweight UniRouter trace UI and X-Insight UI boundary | PRD4 adds request trace display, but X-Insight is expected to own broader observability; UniRouter should not duplicate a full observability product without a confirmed boundary | Batch 1C accepted request-level evidence quality and per-event structural tags. X-Insight deep links, execution search, timeline, node detail, capture-gap panel, policy simulation, and evidence packs remain X-Insight-owned unless User changes boundary |
| PRD5-ONTOLOGY-001 | Ontology-driven governance loop | Requires stable Flow, Trace, Identity, Policy, and Governance entities before ontology can control product behavior | PRD4 trace/event model, identity attribution, policy model, governance evidence model |
| PRD5-SAAS-001 | Public SaaS, reseller, payment, invoice, coupons, campaigns, support CRM | Shifts product direction away from local-deploy enterprise MaaS | Explicit User product-direction change and separate SaaS/commercial PRD |

## PRD4 Relationship

PRD4 may design prerequisites for these items, but must not imply that they are
implemented:

- PRD4 Flow/Event/Trace can feed Scheduler V2 later.
- PRD4 Flow/Event/Trace can provide a foundation for X-Insight integration, but
  PRD5 must still define the cross-project schema, ingestion path, and evidence
  ownership before implementation.
- PRD4 PlungeIn Observe/Act/Enrich ports can prepare plugin boundaries later.
- PRD4 external Logto compatibility can prepare an optional identity deployment
  pattern later.
- PRD4 real upstream and real SSO harnesses remain reusable future evidence
  assets, but real target execution and acceptance are PRD5 backlog items after
  the 2026-07-08 scope decision.
- PRD4 Status Center can prepare SLA/SLO and full incident operations later.
- PRD4 Runtime Observability can prepare multi-instance infrastructure inventory,
  topology, collector integration, and metric-alert routing later.
- PRD4 API key owner usage can prepare budget hard-blocking later.
- PRD4 Logs trace UI can prepare a lightweight UniRouter-side request view, but
  enterprise-wide AI/Agent observability should stay in X-Insight unless the
  User explicitly changes the product boundary.
- Current X-Insight Gate A coordination is recorded in
  `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`: sanitized snapshot
  first, restricted read-only DB/view after local import passes, no shared
  GitHub organization strategy now, no shared ECS staging before local Gate A
  and CI pass, and structural fields before exporter/outbox.

## Reopen Rule

A PRD5 item can move into active planning only when:

- the User explicitly chooses it as current scope;
- current repo docs/code are re-read before planning;
- prerequisites and blast radius are documented;
- acceptance evidence is defined before implementation starts;
- deferred PRD4 boundaries are not silently rewritten.

For X-Insight-related PRD5 items, also require:

- the shared-folder request/response trail is linked;
- any X-Insight preference is separated from confirmed facts;
- UniRouter-owned work and X-Insight-owned work are listed separately;
- payload storage and evidence-reference ownership are decided before raw
  payload capture is implemented.
