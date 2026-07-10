# PRD6 Implementation Plan

> Date: 2026-07-11
> Status: D6-01/D6-02 accepted; Batch 0B active
> Execution owner: Codex under `docs/agent-workflows/AGENT-PROTOCOL.md`

## 1. Sequence

```text
Batch 0 baseline and scope evidence
  -> Batch 0A existing correctness repairs
  -> D6-01/D6-02 accepted
  -> Batch 0B tenant and financial identity integrity
  -> Batch 0C remaining correctness/config truth
  -> Batch 1 Agent Capability and Distributed Policy V1
  -> Batch 2 Routing Policy V1
  -> Batch 3 durable usage, settlement and cache-aware billing
  -> Batch 4 Guardrail Kernel
  -> Batch 5 Streaming policy and termination
  -> Batch 6 selected advanced capability, if separately accepted
  -> Batch 7 authorized target evidence
```

Batch 0A was safe to execute before the scope gate. D6-01 and D6-02 are now
accepted, so Batches 0B through 5 are authorized repository scope subject to
their prerequisites, independently reviewable PRs, local verification and
required CI. No scope decision grants target/deployment/data/cost authorization.

## 2. Batch Specifications

### Batch 0 — Baseline And Scope Evidence

**Risk:** R2 documentation/research.
**Status:** competitor baseline, Batch 0A and the scalability/anti-pattern audit
were CI verified by PR #139 through final evidence head `6c8c192`; PR #139 was
squash-merged as `62f161e`.

Deliverables:

- clean-base Git, required CI, fresh local test and X-Insight verification;
- PRD5 closure and X-Insight v1 contract reconciliation;
- version-fixed LiteLLM/New API/One API audit with claim corrections;
- source-backed 100x/Agent/streaming/billing anti-pattern audit;
- Four Quadrants, ownership, P0-P3, MVP/advanced/long-tail classification;
- PRD6 entry, PRD, matrix, plan and backlog.

No deployment or external mutation is allowed.

### Batch 0A — Circuit And Atomic Limiter Correctness

**Risk:** R3 request-path correctness change.
**Status:** merged and CI verified by PR #139; no deployment claimed.

Deliverables:

- recovery deadline semantics for half-open channels;
- atomic dual-window Redis limiter with exact boundary;
- request-context propagation;
- explicit preservation of Redis failure fail-open behavior;
- strict sequential and concurrent regression coverage.

Verification:

- focused gateway/middleware unit and race tests;
- repeated concurrency boundary tests;
- full backend test/vet;
- frontend regression suite/build;
- required PR CI.

### Batch 0B — Tenant And Financial Identity Integrity

**Risk:** R4 multi-tenant authorization and financial-state behavior.
**Status:** P6-F16 is CI verified by PR #140; later Batch 0B rows remain ordered
and independently gated.

Split into independently reviewable security/financial PRs:

1. enforce actor-organization predicates on every key/user/project operation
   and project-owned model-set assignment/runtime check, with platform-admin
   (including normalized legacy `admin`) as the only explicit cross-tenant
   exception;
2. introduce an unoverrideable server operation ID and separate client
   idempotency, trace/span, workflow and Agent identities;
3. migrate idempotency to a tenant/key/payload-bound namespace;
4. remove Freeze connection-pool re-entry and capture effective price/multiplier
   version on the reservation;
5. make `frozen -> settled|cancelled` a single atomic terminal transition and
   preserve `settlement_pending` after billable output;
6. define user/project/key/role revocation epochs and an all-replica SLO.

Verification must include cross-tenant negative integration tests, migration
up/down, pool-size barriers, 100-way terminal races, process-kill recovery, and
wallet/ledger invariants. No production exploit or target mutation is required.

The follow-up administrator-surface audit confirmed P6-F32 through P6-F36.
These are urgent inputs, not part of the original D6-01 grant. If accepted, keep
notification, finance, pricing authority, global control-plane authority and
organization-level model governance in separate reviewable PRs rather than
expanding P6-F16 after review.

### Batch 0C — Retry, Endpoint Parity, Config And Billing Truth

**Risk:** R3 request/billing behavior.
**Status:** accepted by D6-01; not started; deliver through a separate branch/PR.

Order within the batch:

1. lock an adapter error/retry matrix and preserve non-retryable responses;
2. add Embeddings route/failover parity without changing authorization order;
3. add real before/after recovery-deadline and success/failure state-transition
   tests, then wire one effective retry/circuit/cooldown configuration source
   and emit its version/effective values in safe evidence;
4. repair organization-multiplier freeze/settlement invariants;
5. handle stream Scanner errors, response-body lifetime, single-writer/flush and
   timeout semantics safely;
6. make concurrent sensitive-word reload safe.

Each item may be split into its own R3 PR if the diff ceases to be independently
reviewable.

### Batch 1 — Agent Capability And Distributed Policy V1

**Risk:** R3/R4 shared admission behavior.
**Gate:** D6-01/D6-02 accepted; Batch 0B identity/tenant prerequisites and the
selected policy contract must be complete.

Design first:

- capability exchange, claims, signature/key versions, maximum TTL, monotonic
  delegation, ancestor budgets and refresh jitter/singleflight;
- auth/policy snapshot invalidation and revocation SLO;
- canonical selector and precedence model;
- RPM/RPS/TPM/concurrency reservation and settlement semantics;
- weighted workload classes and separate short/long-work fairness;
- Redis key schema, script versioning and cluster compatibility;
- shadow/enforce/disabled modes;
- fail-closed, fail-open and last-known-safe behavior by policy class;
- low-cardinality metrics and sanitized trace contract.

Implementation must include multi-instance tests, Redis outage/latency injection,
clock/TTL boundary tests, hot-key behavior and rollback compatibility.

### Batch 2 — Routing Policy V1

**Risk:** R3 provider selection behavior.
**Gate:** Batch 0B, Batch 0C and Batch 1 policy evidence foundation.

Deliverables:

- explicit static priority/weight semantics;
- retry classifier and bounded attempts;
- health/cooldown/half-open state transitions and probe ownership;
- Chat/Embeddings parity;
- effective config version and candidate/exclusion/fallback evidence;
- operator preview and safe rollback.

Adaptive latency/cost routing remains advanced scope.

### Batch 3 — Durable Usage, Settlement And Cache-Aware Billing

**Risk:** R3 financial semantics.
**Gate:** version-fixed provider usage fixtures and price ownership decision.

Deliverables:

- reservation-aware stream consumption and durable `settlement_pending`;
- minimal decision/usage journal plus a transactional internal outbox;
- bounded, idempotent trace/log/notification workers and incremental budget
  threshold events;
- cache read/create token fields in normalized usage;
- provider adapter mappings with unknown-field behavior;
- cache price/ratio source, version and audit;
- freeze/settle/ledger/log/reconciliation alignment;
- UI labels that distinguish upstream prompt-cache use from response cache.

No response cache is enabled.

### Batch 4 — Guardrail Kernel

**Risk:** R3/R4 request denial and privacy behavior.
**Gate:** policy categories, failure mode, external detector and latency budget.

Deliverables:

- versioned policy chain and selector precedence;
- input and non-stream output stages;
- compatibility sensitive-word stage;
- PII redact/block adapter and generic external classifier adapter;
- decision/latency/version evidence without raw-payload retention;
- shadow rollout and rollback.

### Batch 5 — Streaming Policy And Termination

**Risk:** R4 live content disclosure and billing.
**Gate:** accepted disclosure/buffering/latency and protocol termination contract.

Deliverables:

- normalized pre-flush chunk hook;
- cross-chunk sliding window or bounded buffering;
- oversize/malformed-frame and detector-timeout behavior;
- OpenAI-compatible and Anthropic termination semantics;
- upstream cancellation, client disconnect and exactly-once settlement;
- measured residual disclosure in evidence and documentation.

### Batch 6 — Selected Advanced Capability

Only one separately accepted advanced capability should enter at a time:

- secret references/rotation;
- opt-in response cache;
- adaptive/canary routing;
- policy simulation/BYO guardrails;
- selected adapter/SDK work.
- asynchronous object-reference image/video job plane.
- attenuation-only adaptive-risk assertions;
- offline Governance Digital Twin or explicitly authorized live shadowing;
- semantic lineage/collusion analysis, verifiable trust receipts, or
  domain-bounded claim assurance.

Each needs its own threat model, acceptance rows and rollback plan.

### Batch 7 — Target Evidence

Target actions remain authorization-gated. Build an exact runbook and evidence
manifest before requesting access. Do not combine source implementation,
credential mutation, data expansion and production claims in one authorization.

## 3. PR And CI Discipline

- use `codex/` branches;
- one independently reviewable risk boundary per PR where practical;
- no direct push to `main` and no required-check bypass;
- required checks: `Backend Tests`, `Frontend Build`, `Docker Build`;
- local verification is recorded before push; CI evidence is read back before
  merge or status promotion;
- an optional independent review may run without becoming a serial gate;
- P0/P1 findings block or reopen the affected batch.

## 4. Rollback Principles

- repository behavior rolls back through a new reviewed PR;
- Redis scripts and key schemas must remain compatible across rolling versions;
- policy/config changes require versioned rollback, not ad hoc database edits;
- target deployment rollback needs separate authorization and exact image/data
  evidence;
- no PRD6 rollback may rewrite PRD5 evidence history.
