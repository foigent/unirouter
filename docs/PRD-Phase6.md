# UniRouter PRD6: Reliable MaaS Policy And Safety Plane

> Version: 0.1
> Date: 2026-07-11
> Status: Scope Proposal; PRD6 discovery formally active
> Accepted implementation now: Batch 0A verified correctness fixes only
> Product-scope gate: User decision pending

## 1. Outcome

PRD6 proposes to turn UniRouter's strong enterprise gateway foundation into a
coherent MaaS policy and safety plane. Admission, routing, cache-aware cost,
guardrails, streaming termination and evidence should use explicit,
versioned, testable semantics across replicas.

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

### 3.1 Accepted Before Scope Gate

Batch 0A may repair confirmed existing correctness defects:

- expired half-open channels must regain bounded probe eligibility;
- API-key Redis RPM/RPS checks must enforce the exact boundary atomically and
  use the request context;
- current Redis-unavailable fail-open behavior is preserved until the broader
  failure-mode decision is accepted.

### 3.2 Recommended MVP Scope

The following become accepted only after the User accepts or edits the MVP
boundary.

#### P6-MVP-01 Distributed Policy V1

- atomic RPM/RPS across replicas;
- TPM policy with explicit input, reserved-output and settled-output semantics;
- concurrency control moved from process-local defaults to shared policy where
  configured;
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

#### P6-MVP-03 Cache-Aware Usage And Billing

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

### X-Insight Or Other Projects Own

- X-Insight Phase 2 execution search, detail/timeline, node detail,
  provenance/source display and capture-gap investigation;
- cross-source evidence analysis, policy simulation and deep investigation UI;
- future durable import jobs, deep links, automated/authenticated receipts,
  retention/delete workflow, stable public adapters and continuous ingestion,
  unless a later joint contract explicitly assigns work.

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
| P6-F03 | Honor retryable/non-retryable error classification and preserve safe response semantics | P0 | Proposed Batch 0B |
| P6-F04 | Give Embeddings Chat-equivalent routing/failover behavior | P0 | Proposed Batch 0B |
| P6-F05 | Make effective retry/circuit/cooldown configuration observable and remove hard-coded divergence | P0 | Proposed Batch 0B |
| P6-F06 | Correct freeze/settlement behavior when organization multipliers increase actual cost | P0 | Proposed Batch 0B |
| P6-F07 | Enforce shared RPM/RPS/TPM/concurrency policies with explicit selectors and failure modes | P1 | Proposed MVP |
| P6-F08 | Implement Routing Policy V1 with explainable candidates, exclusions, retry and failover | P1 | Proposed MVP |
| P6-F09 | Normalize and bill provider cache-read/cache-create tokens | P1 | Proposed MVP |
| P6-F10 | Add versioned input/non-stream-output Guardrail Kernel | P1 | Proposed MVP |
| P6-F11 | Add safe streaming interception, cancellation and settlement semantics | P1 | Proposed MVP |
| P6-F12 | Add external secret references/versioning/rotation for selected backends | P2 | Advanced candidate |
| P6-F13 | Add opt-in isolated exact/semantic response cache | P2 | Advanced candidate |
| P6-F14 | Add adaptive and canary routing policies | P2 | Advanced candidate |
| P6-F15 | Add policy simulation/BYO guardrails/config rollback | P2 | Advanced candidate |

## 6. Safety And Authorization Boundaries

Without specific applicable User authorization, PRD6 must not:

- deploy or change ECS, security groups, DNS, TLS, databases or target runtime;
- send a real or paid provider request;
- create, rotate, reveal or change credentials;
- expand database access, data export, raw-payload capture or retention;
- enable exporter/outbox, automated X-Insight delivery or continuous ingestion;
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

## 8. Scope Gate

The User must accept or modify:

1. the recommended MVP group P6-F07 through P6-F11; and
2. the policy-specific fail-open/fail-closed/last-known-safe posture.

Batch 0A does not prejudge those choices. Batch 0B may be accepted separately
because its rows are confirmed existing correctness/parity work.
