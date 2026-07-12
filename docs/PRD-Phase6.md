# UniRouter PRD6: Reliable MaaS Policy And Safety Plane

> Version: 0.7
> Date: 2026-07-12
> Status: D6-01/D6-02/D6-08/D6-09/D6-10/D6-11 accepted; P6-F03-P6-F07, P6-F16-P6-F19, P6-F32-P6-F39 and P6-STREAM-002A applicable rows are CI verified / closed; P6-F11 remains in progress
> Accepted implementation scope: P6-F03 through P6-F11, P6-F16 through P6-F21, and P6-F32 through P6-F39
> Innovation gate: P6-F23 through P6-F31 remain proposed/deferred

## 1. Outcome

PRD6 proposes to turn UniRouter's strong enterprise gateway foundation into a
coherent and scalable MaaS policy and safety plane. Tenant identity, financial
idempotency, admission, routing, cache-aware cost, guardrails, streaming
termination and evidence should use explicit, versioned, testable semantics
across replicas and Agent descendants.

This is not a promise to match every LiteLLM or New API feature. It is a
positioning-led scope proposal based on the verified current repository and the
version-fixed competitor audit.

## 2. Non-Negotiable Starting State

- PRD1 through PRD4 remain completed/frozen according to their own accepted
  boundaries.
- PRD5 remains `Accepted Scope Complete`; PRD6 does not rewrite or reopen it.
- X-Insight Phase 1 remains complete and its fixture/synthetic-only target is
  `Target verified`, not `Production verified`.
- The only accepted cross-project runtime contract is operator-mediated
  `xinsight.import-receipt.v1` sidecar reconciliation.
- PRD5 deferred/rejected inputs are not PRD6 scope unless this PRD explicitly
  accepts them after a decision.
- Local, CI, target, production, and external-integration evidence levels remain
  distinct.

## 3. Scope Model

### 3.1 Accepted And Verified Batch 0A

Batch 0A repaired and CI verified the confirmed existing correctness defects:

- expired half-open channels must regain bounded probe eligibility;
- API-key Redis RPM/RPS checks must enforce the exact boundary atomically and
  use the request context;
- the Batch 0A patch preserved the prior Redis-unavailable fail-open behavior;
  accepted D6-02 governs its later replacement in Distributed Policy V1.

### 3.2 Accepted MVP Scope

The User accepted D6-01 and D6-02 on 2026-07-11. The following is authorized
repository implementation scope subject to the batch, review, safety and
evidence gates in this PRD. Acceptance does not authorize deployment, target
mutation, real or paid calls, credential changes, raw-payload expansion, or a
new cross-project runtime contract.

#### P6-MVP-00 Tenant And Financial Integrity Foundation

- enforce organization ownership on every non-platform-admin object operation;
- separate server operation identity, client idempotency, trace, span, workflow,
  and Agent identities;
- make reservation terminal transitions atomic and preserve
  `settlement_pending` after billable output;
- remove billing connection-pool re-entry and bind reservation to the effective
  price/multiplier version;
- define versioned user/project/key/role revocation with a measurable all-replica
  SLO.

#### P6-MVP-01 Agent Capability And Distributed Policy V1

- exchange supervisor-held long-lived keys for short-lived, attenuating Agent
  capabilities; children never receive the root secret;
- enforce ancestor workflow/project/organization cost, token, rate, concurrency,
  depth, fan-out, model and endpoint limits;
- atomic RPM/RPS across replicas;
- TPM policy with explicit input, reserved-output and settled-output semantics;
- concurrency control moved from process-local defaults to shared policy where
  configured;
- weighted workload classes for short chat, long stream, embedding, batch and
  future media work;
- selectors for API key, organization/project, model and an explicitly defined
  policy label; a channel admin tag is not silently treated as a quota label;
- shadow, enforce and disabled modes;
- explicit Redis unavailable behavior per policy class;
- bounded cardinality metrics and traceable policy decisions.

#### P6-MVP-02 Routing Policy V1

- static priority and weight with deterministic semantics;
- error classification that distinguishes retryable provider failures from
  client/policy errors;
- bounded retries, cooldown and half-open probe behavior driven by effective
  configuration;
- Chat and Embeddings route/failover parity;
- fallback, route candidate, exclusion reason and config-version evidence;
- no latency/cost-adaptive scheduler requirement in V1.

#### P6-MVP-03 Durable Usage And Cache-Aware Billing

- preserve a durable minimal usage/decision journal and idempotent finalization;
- keep billable output in `settlement_pending` until one terminal settlement;
- isolate detailed trace/log/notification work behind a bounded transactional
  outbox while financial state remains strongly consistent;
- normalize provider-reported cache-read and cache-create token classes;
- support source-owned per-model cache token prices or ratios;
- show authoritative cache usage and cost in logs, ledger and reconciliation;
- prevent unverified `_uniroute.cache_hit` metadata from becoming billing truth;
- no exact/semantic response cache in MVP.

#### P6-MVP-04 Guardrail Kernel

- tenant/project/key-selectable policy chain with versioned configuration;
- input and non-stream output stages;
- sensitive-word compatibility plus PII redact/block adapter and generic
  external classifier adapter;
- policy outcome, latency, detector version and redaction/block evidence;
- raw prompt/response remains non-persistent by default;
- external detector unavailability follows the accepted policy failure mode.

#### P6-MVP-05 Streaming Policy And Termination

- a normalized per-chunk hook before client flush;
- configurable bounded buffering or sliding-window inspection;
- cross-chunk policy matching and oversized/malformed-frame handling;
- explicit protocol termination behavior for OpenAI-compatible and Anthropic
  streams;
- upstream cancellation, client disconnect and billing settlement invariants;
- measured and reported maximum disclosure; no “zero leakage” marketing claim.

### 3.3 Advanced Scope Candidates

- external secret references, credential versions, rotation and rollback;
- opt-in exact/semantic response cache with tenant isolation, encryption, TTL,
  purge, audit, stampede protection and cache-aware billing semantics;
- latency/cost/rate-aware routing, shadowing and canary policies;
- policy versioning, simulation, BYO guardrails and config rollback;
- selected provider adapters backed by conformance suites.
- an asynchronous image/video job plane with tenant-scoped object references,
  modality queues, leases, status/cancel, retention, and final settlement.
- a signed, expiring, attenuation-only adaptive-risk assertion and local policy
  snapshot;
- an offline Governance Digital Twin, failure-to-evaluation manifests and an
  explicitly authorized live-shadow mode;
- semantic data-lineage passports, collusion/cascade risk analysis and
  risk-adjusted routing;
- verifiable MaaS trust receipts and domain-bounded claim assurance.

These require separate acceptance rows and cannot begin merely because the MVP
is accepted.

### 3.4 Long-Tail, Deferred Or Rejected By Default

- full Scheduler V2 rewrite;
- plugin/marketplace and runtime third-party installation;
- IAM rewrite or service-account runtime permission expansion;
- managed HA/IaC, customer SLA operation and public SaaS control plane;
- broad SDK suite;
- public recharge/payment/referral/resale mechanics;
- plaintext prompt/response cache or raw-payload-by-default storage.

Each requires a new product, security, ownership and evidence decision.

## 4. Ownership

### UniRouter Owns

- request-path admission, routing, provider dispatch and failover;
- API-key/tenant policy enforcement;
- provider usage normalization and billing truth;
- gateway guardrail orchestration and stream termination;
- gateway-local trace, audit and sanitized evidence emission;
- configuration version, rollback and operator-facing route/policy visibility.

### Boundary With External Projects

- UniRouter owns creation of authoritative request identity, authorization,
  routing, usage, billing, and sanitized gateway audit facts.
- External products own their own investigation, aggregation, storage, and UI.
- They may consume a versioned sanitized contract asynchronously, but they do not
  decide UniRouter authorization/routing/billing and cannot be a request-success
  dependency.
- No external-project feature is a PRD6 functional requirement. Existing PRD5
  contracts remain historical boundaries and are not reopened here.

### Target-Evidence Only

- a concrete target P5-F35 route/failover detail;
- migration down, TLS 443, real upstream and real enterprise SSO;
- selected target multi-instance load/capacity and HA/DR proof.

These do not become UniRouter source-code gaps without new evidence.

## 5. Functional Requirements

| ID | Requirement | Priority | Scope state |
|----|-------------|----------|-------------|
| P6-F01 | Recover expired half-open channels into bounded probe eligibility | P0 | Accepted Batch 0A |
| P6-F02 | Atomically enforce the exact Redis RPM/RPS boundary | P0 | Accepted Batch 0A |
| P6-F16 | Enforce organization ownership for every non-platform-admin key/user/project and project-owned model-set operation | P0 | Accepted urgent Batch 0B; CI verified by PR #140 |
| P6-F17 | Separate operation, idempotency, trace/span, workflow and Agent identities | P0 | Accepted urgent Batch 0B; PR #149 merged and CI verified |
| P6-F18 | Make reservation/finalization single-terminal, pool-safe and durable after billable output | P0 | Accepted urgent Batch 0B |
| P6-F03 | Honor retryable/non-retryable error classification and preserve safe response semantics | P0 | Accepted Batch 0C |
| P6-F04 | Give Embeddings Chat-equivalent routing/failover behavior | P0 | Accepted Batch 0C |
| P6-F05 | Make effective retry/circuit/cooldown configuration observable, remove hard-coded divergence, and reconcile budget-alert configuration/scope truth | P0 | Accepted Batch 0C |
| P6-F06 | Correct freeze/settlement behavior when organization multipliers increase actual cost | P0 | Accepted Batch 0C; CI verified / closed by PR #160 |
| P6-F07 | Enforce shared RPM/RPS/TPM/concurrency policies with explicit selectors and failure modes | P1 | CI verified / closed by PR #168, merge `2610e317` and post-merge main run `29174001859` |
| P6-F08 | Implement Routing Policy V1 with explainable candidates, exclusions, retry and failover | P1 | Accepted MVP |
| P6-F09 | Normalize and bill provider cache-read/cache-create tokens | P1 | Accepted MVP |
| P6-F10 | Add versioned input/non-stream-output Guardrail Kernel | P1 | Accepted MVP |
| P6-F11 | Add safe streaming interception, cancellation and settlement semantics | P1 | Accepted MVP; transport prerequisite P6-STREAM-002A CI verified / closed by PR #162; semantic policy remains |
| P6-F19 | Add short Agent capability, monotonic delegation, ancestor budgets and versioned revocation | P1 | CI verified / closed by P6-F19A PR #164/main run `29170507588` plus P6-F19B PR #166/main run `29171807693`; P6-F20/P6-F21 remain separate and P6-F07 is independently closed by PR #168 |
| P6-F20 | Add durable minimal usage/finalization outbox and bounded evidence/notification workers | P1 | Accepted MVP |
| P6-F21 | Admit weighted workload classes without replica-multiplied quotas or long-stream starvation | P1 | Accepted MVP |
| P6-F12 | Add external secret references/versioning/rotation for selected backends | P2 | Advanced candidate |
| P6-F13 | Add opt-in isolated exact/semantic response cache | P2 | Advanced candidate |
| P6-F14 | Add adaptive and canary routing policies | P2 | Advanced candidate |
| P6-F15 | Add policy simulation/BYO guardrails/config rollback | P2 | Advanced candidate |
| P6-F22 | Add an asynchronous object-reference media job plane | P2 | Advanced candidate; only if separately accepted |
| P6-F23 | Add signed, expiring, attenuation-only adaptive-risk assertions and local enforcement snapshots | P2 | Advanced candidate; separately accepted only |
| P6-F24 | Add an offline Governance Digital Twin and failure-to-evaluation manifest | P2 | Advanced candidate; separately accepted only |
| P6-F25 | Add explicitly authorized live shadow routing | P2 | Advanced candidate; separately accepted only |
| P6-F26 | Add authenticated data-lineage passports and sink-bound enforcement | P2 | Advanced candidate; separately accepted only |
| P6-F27 | Add a verifiable MaaS trust receipt | P2 | Advanced candidate; separately accepted only |
| P6-F28 | Add domain-bounded claim assurance and risk-adjusted routing | P2 | Advanced candidate; separately accepted only |
| P6-F29 | Feed approved failure cases to an external model-lifecycle workflow | P3 | Deferred; no automatic raw-case training or promotion |
| P6-F30 | Add a multimodal provenance passport | P3 | Deferred pending media-roadmap decision |
| P6-F31 | Add semantic collusion/cascade risk analysis | P3 | Research candidate; shadow only until separately accepted |
| P6-F32 | Tenant-scope wallet, grant, transaction and reconciliation administration | P0 | Accepted by D6-08; merged and CI verified by PR #145 / `609385f` |
| P6-F33 | Tenant-scope notification evidence and webhook sinks | P0 | Accepted by D6-08; CI verified by PR #142 |
| P6-F34 | Restrict global base-price and organization-multiplier mutation to platform authority | P0 | Accepted by D6-08; PR #146 squash-merged as `517ce45381429781f7e81e492bdd9f38093c21ba`; final-head run `29137670055` and post-merge main run `29137781625` passed all required jobs; historical branch retained |
| P6-F35 | Restrict deployment settings, license material and global sensitive words to platform authority | P0 | Accepted by D6-08; PR #147 final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1` passed all required jobs in run `29139408226`, squash-merged as `c08db96167837ade685708c2ab204861a8ecc0ca`, and post-merge main run `29139534691` passed all required jobs; historical branch retained |
| P6-F36 | Enforce organization ownership across registry, alias, catalog and organization model-set endpoints | P1 | Accepted by D6-08; final required run `29140739477` passed, PR #148 squash-merged as `d83dbbc0ac7137bb44d2346adc5a25debde804e1`, and post-merge main run `29140850345` passed |
| P6-F37 | Tenant-scope audit-log reads and reject foreign organization/project filters | P0 | Accepted safe remediation under PRD6 work rule 7; CI verified by PR #143 |
| P6-F38 | Run the existing frontend test suite in required CI with stable, bounded interaction timing | P1 | Accepted safe remediation under PRD6 work rule 7; CI verified by PR #143 |
| P6-F39 | Tenant-scope request-log list/detail/trace, usage analytics/internal status, dashboard usage-derived aggregates and runtime traffic | P0 | Accepted safe remediation under PRD6 work rule 7; merged and CI verified by PR #145 / `609385f` |

P6-F17 repository/CI evidence is Scheme A evidence. `X-Request-ID` is
now a server operation ID, caller correlation is separate, and Chat/Embeddings
use a 24-hour tenant/API-key/payload-bound metadata receipt without response
storage. Audit and Logs support separate client/operation/attempt readback.
Migration `000021` is not old/new mixed-binary safe and its safe partial down is
not an old-binary rollback authorization. P6-F18 financial terminal-state and
P6-F20 durable-finalization work remain separate.

P6-F35 local evidence is not target or production evidence. A later authorized
target cutover must be non-rolling for these control-plane endpoints: drain old
control-plane binaries before exposing only the new version, because old
binaries do not provide mixed-version safety. Other new-version instances
converge to a committed sensitive-word version through the existing reload path
in approximately one minute; no new global-settings UI is included. The typed
configuration registry and confirmed budget/alert configuration-truth gaps are
CI verified / closed by P6-F05. P6-F17 supplies separate operation/client
correlation and Audit readback, but does not imply generic mutation replay or
P6-F20 durability.

P6-F36 local evidence is also repository-only evidence. It preserves the
P6-F16 project-assignment/live-admission contract, adds no migration, RLS or
foreign-key redesign, and does not change unregistered-model compatibility or
`/v1/models` semantics. A selector UI and target malformed-assignment cleanup
are outside this batch; no target or production behavior is claimed.

## 6. Safety And Authorization Boundaries

Without specific applicable User authorization, PRD6 must not:

- deploy or change ECS, security groups, DNS, TLS, databases or target runtime;
- send a real or paid provider request;
- create, rotate, reveal or change credentials;
- expand database access, data export, raw-payload capture or retention;
- enable an external exporter/continuous delivery, or enable an internal outbox
  in any target runtime;
- delete Gate A files before the accepted date/procedure;
- claim Production verified from local, CI, synthetic or fixture evidence.

Repository-local tests, fixtures, static analysis and synthetic in-process
failure injection are allowed when they do not cross these boundaries.

## 7. Evidence Standard

Every accepted functional row must define:

- a deterministic unit/integration test for its policy boundary;
- concurrency/race/failure-injection tests where shared state is involved;
- negative tests proving a denied or failed request does not dispatch, double
  settle, or store forbidden payload;
- observable decision/config-version evidence;
- local verification, PR review and all required GitHub CI;
- a separate target-evidence row if target behavior is required.

## 8. Scope Decision Record

The User accepted on 2026-07-11:

1. D6-01: P6-F16 through P6-F18, the existing Batch 0C correctness rows,
   P6-F07 through P6-F11, and P6-F19 through P6-F21; and
2. D6-02: identity, tenant, mandatory safety and hard budget fail closed;
   advisory analysis may continue only with durable degraded evidence; capacity
   may use a small time/amount-bounded last-known-safe lease and then reject;
3. D6-08: platform authority owns global price/settings/license/global words and
   organization multipliers, while tenant roles remain confined to their JWT
   organization; and
4. D6-09: complete PRD6 repository scope first; defer all PRD7 implementation
   and target-evidence execution until PRD6 closeout, while retaining historical
   branches. The existing Gate A file deadline is unchanged;
5. D6-10: use hard-budget Scheme A for long streams: reserve first, request an
   idempotent extension before the remaining reservation crosses a guard band,
   continue only after durable extension, and otherwise stop upstream, return a
   safe terminal error and settle only delivered output. The current
   P6-STREAM-002A slice repairs transport prerequisites but does not yet
   implement reservation extension; and
6. D6-11: discover child Agents only through an explicit supervisor capability
   exchange/registration, never by guessing from traffic. A capability has a
   maximum five-minute TTL and normally refreshes around minute three with
   jitter and singleflight. Expiry blocks new admission but does not kill an
   already admitted request; explicit revocation, hard-budget exhaustion and a
   mandatory safety stop may terminate it. Workflow lifetime, depth, fan-out
   and ancestor budgets remain separate from token TTL, and active revocation
   must converge across replicas within 30 seconds. UniRouter issues only model-
   gateway authority; Tool, IAM, database and export owners enforce their own
   capabilities.

Batch 0B is therefore authorized for repository implementation and begins with
P6-F16. Each R4 security/financial boundary remains an independently reviewable
PR with local, race/integration where applicable, and required CI evidence.
P6-F22 through P6-F31 are not implied by D6-01 and remain separate decisions.
The later source audit found P6-F32 through P6-F36. D6-08 explicitly accepted
those rows with the platform/tenant authority split; they were not retroactively
implied by D6-01. P6-F37 through P6-F39 are separately recorded safe repository
remediations under PRD6 work rule 7. P6-F39 closes only cross-tenant usage-derived
reads; project-dashboard granularity and instance-level dependency health remain
explicit follow-ups rather than implied completion claims.
