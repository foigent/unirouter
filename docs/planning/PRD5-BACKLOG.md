# PRD5 Backlog

> Date: 2026-07-10
> Status: Deferred backlog for capabilities intentionally not implemented in
> PRD4 first scope. P5-F12 screening is Local verified in
> `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`; this does not
> activate the full deferred items automatically.
> Batch 1Y implements the accepted scoped follow-ups. Batch 1Z target evidence is
> `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`;
> the X-Insight runtime response is reconciled in
> `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md`;
> full deferred items remain inactive.
> **2026-07-20 `IR-IP-POLICY-ADAPTATION` checkpoint:** the SHA-bound
> [request](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-20-x-insight-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-request.md>)
> (`7517496d...`) authorizes one UniRouter-only strict-producer change. The
> designated source field is reduced to bounded presence accounting; only
> `false/false` and `true/true` are accepted for
> `ip_address_was_present` / `ip_address_redacted`, with exact aggregate/detail
> reconciliation. Cross-project binding state remains `Not binding`.
> The 2026-07-19 UR-E decision is accepted/binding at SHA-256 `c674b04f...`.
> UniRouter Stage A generated and exclusively transferred one new manifest-
> bound metadata-only synthetic v2 artifact, SHA-256 `412c9b641c...`. The
> sanitized handoff is `producer_complete_receiver_pending`; X-Insight Stage
> A.5 read-only preflight and all Target work remain gated. This does not
> activate a live feed or prove External integration/Production.
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
structured evidence emitted from gateway-native execution points. X-Insight
owns the broader enterprise AI/Agent observability and governance platform. Its
answered runtime request confirms the current file-sidecar-only boundary;
durable jobs/deep links, automated receipts, retention/deletion/audit, public
adapters, and continuous ingestion remain unimplemented and disabled.

Current active PRD5 planning, Batch 1A/1B/1C completed implementation, Batch 1D
upstream request correlation, Batch 1E event capture-gap emission, Batch 1F
payload capture mode configuration, Batch 1G trusted evidence context intake,
Batch 1H gateway span hierarchy, Batch 1I payload policy decision evidence,
Batch 1J provider upstream capture-gap evidence, Batch 1K Gate A snapshot
handoff-note support, Batch 1L component span refinement, Batch 1M provider
failure capture gaps, Batch 1N snapshot capture-gap candidates, Batch 1O Gate A
controlled snapshot handoff prep, Batch 1P model permission decision evidence,
Batch 1Q Gate A request approval manifest, Batch 1R request ID approval
runbook, Batch 1S data exchange control surface decision, Batch 1T exchange
envelope/controlled-run authorization, and Batch 1U Embeddings model-permission
parity, Batch 1V Gate A target execution, Batch 1W pre-relay permission evidence
parity, Batch 1Y repository-owned controls, and Batch 1Z target deployment/security
closeout are tracked in:

- `docs/PRD-Phase5.md`
- `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
- `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`
- `docs/planning/PRD5-COMPETITOR-COMPARISON.md`
- `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`
- `docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md`
- `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md`
- `docs/tech-ref/26-prd5-evidence-field-contract.md`
- `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`
- `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`
- `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md`

This backlog remains the source record for deferred items that are not yet part
of the active PRD5 implementation train. P5-F12 screening has now selected only
scoped follow-ups; full Scheduler V2, runtime plugins/marketplace, IAM rewrite,
managed HA/IaC product, hard budget blocking implementation, runtime
prompt/response cache, full SDK suite, new service-account authority, and full
SLA operations remain separate decisions.

## Strict Raw-IP Policy Adaptation (2026-07-20)

`IR-IP-POLICY-ADAPTATION` changes only the strict internal-real producer. Its
frozen declaration is
`raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained`.
The database may evaluate designated `public.call_logs.ip_address` presence,
but the raw string is never returned, scanned, logged, hashed or serialized;
raw error and every other blocked family remain fail-closed. This repository
checkpoint is implementation evidence only, not source qualification, X-Insight
acceptance or a binding decision. It does not start B2a/B2b, create or transfer
a manifest/artifact, or authorize Target, Production, live-feed or B3 work.

## UR-E Bounded Synthetic Sidecar Execution (2026-07-19)

The exact one-artifact, metadata-only, synthetic sidecar decision is now
accepted and binding, as documented in its corrected owner-authored
[response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-19-unirouter-prd2-v2-sidecar-external-integration-artifact-response.md>).
The linked
[joint decision](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md>)
is accepted/binding at SHA-256 `c674b04f...`.
The later
[P1 correction request](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-19-x-insight-ur-e-u0-exclusive-create-correction.md>)
identified a TOCTOU/overwrite defect in ordinary `install -m 0600`. The owner
response now replaces it with versioned `ur-e-exclusive-copy-v1`: the exact
receiver is opened atomically with `O_WRONLY|O_CREAT|O_EXCL`, pre-existing
regular or symlink destinations remain unchanged, success re-verifies regular
file / `0600` / length / SHA-256, and post-create failure is confined to the
same exact created file.

Stage A is complete for the exact artifact SHA-256 `412c9b641c...`: one new
approved request, `cost_micros="314159"`, one call log, no trace row and one
missing-trace gap. Atomic exclusive transfer produced bytewise-equal mode-
`0600` non-Target receivers. The sanitized
[handoff](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md>)
is `producer_complete_receiver_pending`. X-Insight Stage A.5 must independently
rehash the receiver and pass read-only Target/release/database/path and source-
qualified pre-existence checks before Stage B can begin. External integration,
customer data, Production and the completed-execution live feed remain open or
deferred as previously classified. Evidence:
`docs/ops/evidence/V1-XI-LEDGER-V2-SIDECAR-UR-E-STAGE-A-20260719.md`.

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
| PRD5-XINSIGHT-001 | X-Insight integration contract and shared working process | Both controlled v1 snapshots passed artifact reconciliation, persistence, idempotent rerun and readback; Batch 1W is Target verified and keys 36/37 are disabled. X-Insight PR #52 / `0462e698` published the v2 consumer and monotonic source-version correction; PR #54 / `0ca502f` published deployable rc.6; PR #55 / `6b1ea900` records the historical synthetic v2 fixture on the named non-production Target as execution-owner verified, not independent acceptance. External integration for a fresh producer handoff remains pending. The accepted sidecar-only boundary remains binding; the completed-execution live feed is deferred | The corrected UR-E decision is accepted/binding. UniRouter Stage A generated one new manifest-bound synthetic v2 artifact `412c9b641c...` and transferred it through atomic exclusive-create to bytewise-equal `0600` operator-local receivers; the handoff is `producer_complete_receiver_pending`. X-Insight Stage A.5 and Stage B remain required. Durable job/deep-link/automated receipt/authenticity/public-adapter contracts, real upstream E2E, read-only views/exporter/outbox and broader data remain unimplemented or unapproved. UniRouter ECS `INFRA-SEC-001` remediation is closed in Batch 1Z |
| PRD5-EVIDENCE-001 | Structured execution-chain evidence fields | PRD4 trace events are a metadata-only foundation but do not yet model multi-agent/multi-tool/multi-business-system chains with explicit structural fields | Batch 1A/1B/1C accepted the field contract, nullable migration, gateway-known structural writes, read-side exposure, snapshot output, and lightweight trace UI. Batch 1D adds provider-returned upstream request ID capture. Batch 1E adds event-level redacted capture-gap emission for sanitized trace metadata. Batch 1F adds restricted payload evidence mode configuration. Batch 1G adds default-off trusted evidence context intake for internal trace/session/actor/delegation fields. Batch 1H adds per-request gateway root span parenting for UniRouter-emitted trace events. Batch 1I adds explicit payload policy decision events. Batch 1J adds `provider_not_returned` capture gaps for provider response events without upstream request IDs. Batch 1L refines repeated same-component event parenting. Batch 1M adds `not_available_from_source` capture gaps for provider-stage failures that occur before any upstream response source exists. Batch 1N adds snapshot `candidate_capture_gaps` for missing trace rows. Batch 1P adds `permission_denied` auth decision evidence for Chat model rejects. Batch 1U closes the equivalent Embeddings `allowed_models` bypass. Batch 1V proved the live wrapper still bypassed Chat rejection evidence; Batch 1W removes that duplicate pre-relay check. `decision_not_emitted` remains an observed import/gap condition rather than a fabricated runtime event |
| PRD5-PAYLOAD-001 | Enterprise-configurable prompt/response payload policy | Raw prompt/response storage is privacy-sensitive and should not be silently introduced through trace, cache, or X-Insight integration work | Batch 1F accepts only `metadata_only` and `disabled` runtime payload evidence modes. Batch 1I emits explicit `payload.policy.applied` decision events while keeping `raw_payload_stored=false` and `payload_ref_present=false`. Remaining prerequisites before redacted/hash/reference/raw modes: User approval, UniRouter/X-Insight ownership split, encryption/key management, retention, deletion/export, audit-on-read, redacted summary, hash, and `evidence_ref` behavior |
| PRD5-COMPARE-001 | NewAPI/LiteLLM/one-api and observability-platform comparison | Functional, technical, and UI comparison is needed before feature-chase planning; older research snapshots and X-Insight prior research are inputs but must be refreshed before PRD commitments | Batch 1A/1B/1C accepted the initial comparison and UniRouter/X-Insight ownership split. Refresh again before public claims, adapter implementation, pricing claims, or choosing a new catch-up workstream |
| PRD5-TRACEUI-001 | Lightweight UniRouter trace UI and X-Insight UI boundary | PRD4 adds request trace display, but X-Insight is expected to own broader observability; UniRouter should not duplicate a full observability product without a confirmed boundary | Batch 1Z target-verifies structural search and UniRouter Data Exchange Control; route/failover summary is CI verified and still needs a concrete target detail check. X-Insight execution search, timeline, node detail, capture-gap panel, policy simulation, evidence packs, and read-side audit remain X-Insight-owned unless User changes boundary |
| PRD5-ONTOLOGY-001 | Ontology-driven governance loop | Requires stable Flow, Trace, Identity, Policy, and Governance entities before ontology can control product behavior | PRD4 trace/event model, identity attribution, policy model, governance evidence model |
| PRD5-SAAS-001 | Public SaaS, reseller, payment, invoice, coupons, campaigns, support CRM | Shifts product direction away from local-deploy enterprise MaaS | Explicit User product-direction change and separate SaaS/commercial PRD |

## P5-F12 Screening Result

Source: `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`.

| ID | Screening Decision | Current Priority |
|----|--------------------|------------------|
| PRD5-SCHED-001 | Defer full Scheduler V2; route/failover evidence visibility is CI verified after Batch 1Y/PR #131 | P3 for full scheduler |
| PRD5-PLUGIN-001 | Defer runtime plugin platform | P3 |
| PRD5-MARKET-001 | Reject for current PRD5 until runtime plugins exist | P3/out of current scope |
| PRD5-IAM-001 | Reject IAM rewrite into UniRouter core for current PRD5; keep external IdP/OIDC compatibility | P3/out of current scope |
| PRD5-DEPLOY-001 | Defer full managed HA/IaC product; `INFRA-SEC-001` target remediation is closed in Batch 1Z | P2 for full IaC and defense-in-depth host-binding follow-up |
| PRD5-INFRA-001 | Redacted current-process topology is Target verified; productized cluster inventory remains deferred | P2 for cluster inventory |
| PRD5-BUDGET-001 | Hard-block state-machine design is complete; runtime implementation remains deferred | P2/deferred runtime |
| PRD5-CACHE-001 | Reject plaintext runtime cache for current PRD5; defer only privacy-safe opt-in metadata/ref/hash design | P3/out of current scope |
| PRD5-SDK-001 | Defer full official SDK suite until typed API schemas and semver policy stabilize | P3 |
| PRD5-ROLE-001 | Service-account taxonomy/design is complete; runtime authority remains deferred | P2/deferred runtime |
| PRD5-OPS-001 | Minimal incident/SLO evidence design is complete; customer-facing SLA operations remain deferred | P2/deferred runtime |

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
  GitHub organization strategy now, no new cross-project shared ECS staging,
  and structural fields before exporter/outbox. Authorization
  `UR-XI-GATE-A-20260710-01` permits Atlas to use the existing UniRouter ECS
  development/test environment for exactly two controlled scenarios; it does
  not create shared X-Insight staging.

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
