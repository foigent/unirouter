# PRD6 Backlog And Decision Register

> Date: 2026-07-11
> Rule: this is a classified input queue, not an implementation promise.

## 1. Accepted Active Work

| ID | Item | Why it is active | Exit evidence |
|----|------|------------------|---------------|
| P6-ROUTE-001 | Half-open recovery deadline correction | Confirmed current-code outage risk; no new product scope | focused/race/full tests and CI |
| P6-RL-001 | Atomic exact Redis RPM/RPS enforcement | Confirmed over-admission and multi-instance race; no new surface | sequential/concurrent/context/failure tests and CI |
| P6-DOC-001 | Independent PRD6 truth chain | Required to prevent PRD5 reopening and scope leakage | entry, PRD, matrix, plan, backlog, status/inventory sync |

## 2. Recommended MVP — User Decision Pending

| ID | Item | Value | Dependency / risk | Priority |
|----|------|-------|-------------------|----------|
| P6-ROUTE-002 | Retry classification and response preservation | prevents unsafe/expensive retry and misleading 502 | provider error matrix and billing invariants | P0 |
| P6-ROUTE-003 | Embeddings failover parity | removes endpoint reliability asymmetry | adapter and billing/trace parity | P0 |
| P6-CONFIG-001 | Effective routing/circuit config truth | operator control and explainability | migration/default compatibility | P0 |
| P6-BILL-002 | Multiplier-aware freeze/settle correctness | prevents uncollected cost | wallet/transaction semantics | P0 |
| P6-LIMIT-002 | Shared RPM/RPS/TPM/concurrency policy | enterprise isolation/capacity | failure-mode and output TPM semantics | P1 |
| P6-ROUTE-004 | Routing Policy V1 | predictable priority/weight/retry/failover | telemetry, policy version and rollback | P1 |
| P6-BILL-001 | Prompt-cache token usage and billing | accurate provider economics | versioned adapter/price sources | P1 |
| P6-GUARD-001 | Guardrail Kernel | enterprise safety and PII controls | detector/failure/latency policy | P1 |
| P6-STREAM-001 | Streaming interception and termination | limits unsafe live disclosure | buffer/latency/protocol contract | P1 |

## 3. Advanced Candidates

| ID | Item | Reopen prerequisite | Priority |
|----|------|---------------------|----------|
| P6-CRED-001 | Vault/KMS/cloud secret references and versioned rotation | select first backend, threat model and rotation SLO | P2 |
| P6-CACHE-001 | Exact response cache | measured workload value; tenant/auth/TTL/purge/billing design | P2 |
| P6-CACHE-002 | Semantic response cache | exact-cache safety first; semantic poisoning/equivalence red team | P2 |
| P6-ROUTE-005 | Latency/cost/rate-aware routing | trustworthy telemetry and stable Routing V1 | P2 |
| P6-ROUTE-006 | Canary/shadow traffic policy | explicit paid-request/data boundary and rollback | P2 |
| P6-GUARD-002 | BYO guardrails and policy simulation | versioned kernel, RBAC and compatibility contract | P2 |
| P6-ADAPTER-001 | Selected provider/SDK expansion | named customer/provider need and conformance ownership | P2/P3 |

## 4. Other-Project Ownership

| Item | Owner | UniRouter responsibility now |
|------|-------|------------------------------|
| X-Insight operator investigation UI | X-Insight Phase 2 | preserve sanitized evidence and v1 receipt compatibility only |
| Durable import jobs/deep links | X-Insight or future joint contract | do not emit or depend on unimplemented fields |
| Automated/authenticated receipt transport | future joint contract | keep v1 operator mediated |
| Retention/delete/audit workflow for imported evidence | X-Insight | retain current accepted artifacts/decisions; no implicit API work |
| Stable public adapters/continuous ingestion | X-Insight or integration project | no exporter/outbox enablement |
| Secret backend service | selected Vault/KMS/cloud owner | UniRouter later consumes versioned references after acceptance |

## 5. Target Evidence Queue

These are evidence needs, not automatic source implementation:

- P5-F35 concrete target route/failover detail;
- migration `000019` down path;
- TLS 443 behavior;
- real upstream and real SSO;
- selected replica/load/capacity test;
- selected HA/DR, restore and production-hardening evidence.

Each needs an exact environment, operator, data, credential, cost and retention
authorization before execution.

## 6. Deferred Or Rejected By Default

| Item | Disposition | Reason |
|------|-------------|--------|
| Full Scheduler V2 | Deferred | Routing V1 correctness and evidence must stabilize first |
| Plugin/marketplace/runtime installation | Deferred | supply-chain, sandbox and governance surface is disproportionate to current value |
| IAM rewrite | Deferred | current RBAC/project/OIDC foundation is not proven inadequate for MVP |
| Managed HA/IaC/customer SLA operations | Target/commercial prerequisite | requires chosen topology and operating model, not a generic code task |
| Runtime budget hard-block | Not auto-restored from PRD5 | may be reconsidered inside Distributed Policy V1 only through explicit semantics |
| Plaintext prompt/response cache | Rejected | conflicts with accepted default-off payload posture |
| Broad SDK suite | Deferred | provider/customer demand must justify maintenance |
| Service-account runtime permission | Deferred | requires identity/threat model and explicit authorization |
| Public payment/recharge/referral/resale features | Rejected by current positioning | UniRouter is scoped as enterprise-local MaaS, not a public token storefront |
| Exporter/outbox/continuous X-Insight ingestion | External/future contract | not implemented or accepted in v1 |

## 7. Decision Register

| Decision | Recommended default | Status | Effect |
|----------|---------------------|--------|--------|
| D6-01 MVP boundary | P0 correctness + Distributed Policy V1 + Routing Policy V1 + cache-aware billing + guardrail/stream foundation | User decision pending | gates Batches 1-5 |
| D6-02 Enforcement failure mode | blocking safety fail closed; audit-only fail open with degraded trace; hard quota fail closed or bounded last-known-safe | User decision pending | gates limiter/guardrail design |
| D6-03 Response cache | keep out of MVP; advanced opt-in only | Proposed | avoids premature privacy/retention expansion |
| D6-04 Product position | enterprise-local MaaS gateway/control plane | Proposed based on current repository | rejects public-commerce copying by default |
| D6-05 X-Insight boundary | keep v1 operator-mediated receipt; no synchronous dependency | Already accepted cross-project boundary | prevents contract leakage |
