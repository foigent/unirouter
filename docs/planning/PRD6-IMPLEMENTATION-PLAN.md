# PRD6 Implementation Plan

> Date: 2026-07-11
> Status: executable plan with a scope gate after Batch 0A
> Execution owner: Codex under `docs/agent-workflows/AGENT-PROTOCOL.md`

## 1. Sequence

```text
Batch 0 baseline and scope evidence
  -> Batch 0A existing correctness repairs
  -> User MVP and failure-mode decisions
  -> Batch 0B remaining correctness/config truth
  -> Batch 1 Distributed Policy V1
  -> Batch 2 Routing Policy V1
  -> Batch 3 Cache-aware usage and billing
  -> Batch 4 Guardrail Kernel
  -> Batch 5 Streaming policy and termination
  -> Batch 6 selected advanced capability, if separately accepted
  -> Batch 7 authorized target evidence
```

Batch 0A is safe to execute before the scope gate. Batch 0B is recommended next
because it addresses confirmed correctness/parity risks, but it is kept
separate from the current PR. Batches 1 through 5 require the MVP boundary and
failure-mode decisions.

## 2. Batch Specifications

### Batch 0 — Baseline And Scope Evidence

**Risk:** R2 documentation/research.
**Status:** complete locally; PR/CI pending.

Deliverables:

- clean-base Git, required CI, fresh local test and X-Insight verification;
- PRD5 closure and X-Insight v1 contract reconciliation;
- version-fixed LiteLLM/New API/One API audit with claim corrections;
- Four Quadrants, ownership, P0-P3, MVP/advanced/long-tail classification;
- PRD6 entry, PRD, matrix, plan and backlog.

No deployment or external mutation is allowed.

### Batch 0A — Circuit And Atomic Limiter Correctness

**Risk:** R3 request-path correctness change.
**Status:** Local verified on the PRD6 kickoff branch; PR/CI pending.

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

### Batch 0B — Retry, Endpoint Parity, Config And Billing Truth

**Risk:** R3 request/billing behavior.
**Status:** proposed; separate branch/PR.

Order within the batch:

1. lock an adapter error/retry matrix and preserve non-retryable responses;
2. add Embeddings route/failover parity without changing authorization order;
3. add real before/after recovery-deadline and success/failure state-transition
   tests, then wire one effective retry/circuit/cooldown configuration source
   and emit its version/effective values in safe evidence;
4. repair organization-multiplier freeze/settlement invariants;
5. handle stream scanner errors and concurrent sensitive-word reload safely.

Each item may be split into its own R3 PR if the diff ceases to be independently
reviewable.

### Batch 1 — Distributed Policy V1

**Risk:** R3/R4 shared admission behavior.
**Gate:** User accepts MVP and quota failure mode.

Design first:

- canonical selector and precedence model;
- RPM/RPS/TPM/concurrency reservation and settlement semantics;
- Redis key schema, script versioning and cluster compatibility;
- shadow/enforce/disabled modes;
- fail-closed, fail-open and last-known-safe behavior by policy class;
- low-cardinality metrics and sanitized trace contract.

Implementation must include multi-instance tests, Redis outage/latency injection,
clock/TTL boundary tests, hot-key behavior and rollback compatibility.

### Batch 2 — Routing Policy V1

**Risk:** R3 provider selection behavior.
**Gate:** Batch 0B and Batch 1 policy evidence foundation.

Deliverables:

- explicit static priority/weight semantics;
- retry classifier and bounded attempts;
- health/cooldown/half-open state transitions and probe ownership;
- Chat/Embeddings parity;
- effective config version and candidate/exclusion/fallback evidence;
- operator preview and safe rollback.

Adaptive latency/cost routing remains advanced scope.

### Batch 3 — Cache-Aware Usage And Billing

**Risk:** R3 financial semantics.
**Gate:** version-fixed provider usage fixtures and price ownership decision.

Deliverables:

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
