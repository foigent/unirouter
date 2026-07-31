# UniRouter PRD7: Enterprise AI Execution Trust Control Plane

> Version: 0.2
> Date: 2026-07-31
> Status: Repository-local implementation complete through B14; target and
> external gates remain open
> Product position: 企业 AI 执行可信控制面

## 1. Outcome

PRD7 turns the existing gateway, policy, billing, Agent capability and durable
execution-evidence foundations into an enterprise control plane that can answer
five questions for every AI execution:

1. what was requested and by which effective authority;
2. why this provider, route and policy result was selected;
3. what was actually delivered and which financial terminal state is final;
4. which facts are direct, derived, declared, missing or still awaiting
   reconciliation;
5. what configuration or provider change would alter the outcome before it is
   promoted.

This scope was accepted by the User on 2026-07-30 and reconfirmed on
2026-07-31 with an instruction to close the implementation loop unless a
material product decision is required. It supersedes the implementation
authorization state of the older candidate-only backlog without rewriting that
historical document.

Acceptance authorizes code, tests, schemas, local fixtures, documentation and
local commits in the UniRouter repository. It does not authorize deployment,
traffic, live provider or IdP calls, credentials, database mutation outside
isolated tests, push, pull request, merge, or production claims.

The authorized repository-local sequence completed through B14 on 2026-07-31.
This means each accepted row has Local evidence; it does not promote any row to
CI, target, external-integration or production evidence. P7-D03 is resolved
only for the repository-local GPT Image V1 design and implementation recorded
in `docs/architecture/44-async-media-image-v1-runtime-contract.md`. P7-D01,
P7-D02 and P7-D04 through P7-D07 retain their separate runtime, external and
product decision authority.

## 2. Architectural Invariants

- UniRouter remains the source-domain owner for request, route, policy,
  authority, delivery, billing and finality facts.
- Request-path routing, IAM, billing, policy enforcement and persistence cannot
  depend synchronously on X-Insight or another analytics product.
- Shared event and Connector standards are data-plane contracts, not a
  high-privilege universal plugin runtime.
- Provider, billing and telemetry integrations are bounded adapters with
  manifests and conformance fixtures. They do not become extension points for
  UniRouter core IAM, routing or financial authority.
- Unknown, missing, estimated, pending and unavailable are distinct states.
  HTTP success, a local log row or an estimated amount never implies settled,
  provider-reconciled or externally accepted.
- Raw prompt and response bodies remain non-persistent by default.
- New enforcement first runs through deterministic replay, simulation and
  shadow evidence. Promotion is a separate versioned decision.
- Existing completed-execution Feed endpoint, DTO, credentials, event identity,
  source bytes/hash, page and cursor behavior remain compatible.

## 3. Accepted Product Scope

### 3.1 Required market foundations

- Provider Conformance Packs with versioned manifests, synthetic fixtures,
  capability declarations, error/usage/finality rules and change detection.
- Native protocol breadth for OpenAI, Anthropic and Azure OpenAI first, then
  Responses, rerank and image contracts.
- OTel/OpenInference-compatible metadata export with explicit provenance and
  capture gaps.
- External secret references, immutable versions, rotation workflow, stale
  worker detection, rollback and audit.
- Exact tenant-isolated response cache with encryption boundary, TTL, purge,
  stampede protection, poisoning defenses and cache-aware billing semantics.
- Project member lifecycle first; SCIM remains an external-IdP decision gate.
- Offline route/policy simulation, configuration diff and progressive
  promotion.
- Provider COGS ingestion and reconciliation kept separate from customer
  revenue authority.
- Saved views, deep links and a persistent contextual inspector across
  Requests, Audit, Providers and Projects.

### 3.2 Differentiating capabilities

- Execution Decision Receipt Workspace.
- Agent Trust Firewall with effective-authority tree, attenuation proof,
  ancestor budget, TTL and revocation timeline.
- Governance Digital Twin for fixture/replay-based policy and route evaluation.
- Financial Truth and Supplier Reconciliation workspace.
- Provenance and Capture-Gap Passport.
- Provider Change Firewall driven by conformance-pack and fixture diffs.
- Risk-adjusted Routing in advice/shadow mode before any enforcement.
- Evidence-to-Control Flywheel that creates a reviewable policy proposal from
  evidence but never self-promotes it.

## 4. Four-Phase Delivery

### Phase 1 — Visible value and contract foundations

- Receipt V0 and persistent Requests inspector;
- persistent inspectors for Audit, Providers and Projects;
- saved views and deep links;
- Provider Conformance Pack framework and at least three core packs;
- metadata-only OTel/OpenInference export;
- backend-neutral secret-reference contract and rotation design.

Exit requires settled and cancelled fixture investigation, no hidden read-only
details, three fixture-verified provider packs and no guessed semantics.

### Phase 2 — Operable enterprise foundations

- exact tenant-isolated response cache;
- Responses, rerank and image protocol support;
- provider COGS connector contract and controlled offline import;
- offline route/policy simulator and progressive release;
- secret rotation workflow with stale-version detection and rollback.

Exit requires cache isolation/poisoning/stampede evidence, explainable provider
billing differences, deterministic fixture diff and rollback.

### Phase 3 — Trusted Agent flagship

- Agent Trust Firewall Console;
- signed decision receipts;
- Governance Digital Twin;
- risk advice in shadow mode.

Exit requires attenuation-only child authority, provable revocation, reproducible
policy diffs and no synchronous dependency on risk analysis.

### Phase 4 — Ecosystem and adaptive control

- Provenance and Capture-Gap Passport;
- one heterogeneous second Reference Connector;
- risk-adjusted routing promotion controls;
- evidence-to-control proposal workflow;
- repository-local GPT Image V1 asynchronous execution after the media
  provider, object-store, retention and charge-trigger decisions were made;
  video remains disabled until it has an independent Endpoint Profile.

Exit requires a Connector without core-engine vendor branches, reuse of the
shared lifecycle/error/replay/conformance model and real-pilot evidence before
adaptive enforcement is called effective.

## 5. Material Decision Gates

Repository-local interface, fixture and simulation work proceeds without these
decisions. The following external/runtime choices must stop at their gate:

| Gate | Decision needed from User | Work allowed before the gate |
|------|---------------------------|------------------------------|
| P7-D01 | First production secret backend and operator ownership | backend-neutral contract, in-memory/file fixture backend, rotation state machine and tests |
| P7-D02 | SCIM/IdP vendor and directory source of truth | project-member lifecycle, provisioning contract and synthetic conformance |
| P7-D03 | Resolved 2026-07-31 for repository-local Image V1: GPT Image V1, tenant-scoped S3-compatible storage, seven-day retention, charge fact only after validated success plus durable output, no wallet debit; video disabled | repository-local Image V1 API/worker/store/UI and isolated tests are complete; target migration, credentials, paid traffic and release remain gated by P7-D05/P7-D06 |
| P7-D04 | Risk-routing promotion KPI, threshold and pilot cohort | replay, simulator, advice and shadow evidence |
| P7-D05 | Real provider/COGS credentials, account scope and retention | offline connector/import contract and synthetic fixtures |
| P7-D06 | Target deployment, traffic, migration and release authority | local implementation, isolated tests, evidence and local commits |
| P7-D07 | Response-cache customer price, ledger unit and plan/version semantics | exact cache service contract, encryption, identity, finality, TTL/purge, stampede and outage tests; no Gateway hit serving or customer debt |

Semantic response cache, managed billing, a plugin marketplace and a universal
high-privilege plugin runtime are not accepted by this PRD.

## 6. Evidence Levels

Every row uses one of: `Accepted`, `Local verified`, `CI verified`,
`Target verified`, `External integration verified`, or `Production verified`.
No lower level implies a higher one. The authoritative row state lives in
`docs/ops/PRD7-ACCEPTANCE-MATRIX.md`.
