# PRD6 Competitor Alignment And Four-Quadrant Audit

> Date: 2026-07-11
> Status: source-grounded discovery input; not automatically accepted scope
> Role: Chief AI Architect / MaaS platform assessment

## 1. Product Positioning Used For Comparison

UniRouter is treated as a locally deployable enterprise MaaS gateway and
control plane, not as a public token-resale storefront or an external
investigation product. Competitive value is therefore weighted toward reliable
routing, enforceable tenant policy, explainable billing, private credential
governance, safety controls, and auditable operation.

Payment gateways, invitation rewards, public marketplace growth loops, and
similar public-selling features do not receive priority merely because a
competitor has them.

## 2. Fixed Competitor Baseline

### LiteLLM

The stable baseline used here is
[`v1.91.1`](https://github.com/BerriAI/litellm/releases/tag/v1.91.1), released
2026-07-08. Current documentation was cross-checked against stable source when
streaming behavior could have drifted.

Confirmed strengths:

- broad provider/SDK surface;
- weighted, RPM/TPM-aware, latency, least-busy, cost, and custom routing;
- timeout, retry, cooldown, fallback classes, and optional health-driven
  routing;
- Redis-backed multi-instance state, key/team/user budgets, RPM/TPM and
  concurrency controls;
- response cache backends plus exact/semantic modes;
- prompt-cache usage and configurable cache-read/cache-write token costs;
- custom guardrail hooks, Presidio adapter, pre/post/during modes, and a stable
  per-chunk streaming hook;
- callback/observability integrations and reusable provider credentials.

Important limits:

- LiteLLM documents multi-instance rate-limit drift, and output TPM is
  best-effort; “perfect distributed limiting” is not a supported claim.
- Presidio needs separately deployed analyzer/anonymizer services.
- the documented LlamaGuard callback is Enterprise and currently tied to a
  SageMaker endpoint; it is not proof of a universal embedded local engine.
- a streaming check can stop later chunks, but already emitted content cannot
  be recalled. Stable behavior does not prove zero leakage or uniform
  protocol-native termination.
- multi-region, HA control-plane and several fine-grained governance features
  are Enterprise capabilities.
- LiteLLM prompt-cache costing is a cost primitive, not proof of a complete
  customer-facing multiplier product.

Primary official sources:

- [Routing](https://docs.litellm.ai/docs/routing),
  [fallbacks](https://docs.litellm.ai/docs/proxy/reliability), and
  [health-driven routing](https://docs.litellm.ai/docs/proxy/health_check_routing)
- [Budgets and rate limits](https://docs.litellm.ai/docs/proxy/users) and
  [model rate-limit enforcement](https://docs.litellm.ai/docs/proxy/load_balancing)
- [Caching](https://docs.litellm.ai/docs/proxy/caching),
  [prompt caching](https://docs.litellm.ai/docs/completion/prompt_caching), and
  [custom pricing](https://docs.litellm.ai/docs/proxy/custom_pricing)
- [Guardrail quick start](https://docs.litellm.ai/docs/proxy/guardrails/quick_start),
  [Presidio](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2), and
  [stable streaming implementation](https://github.com/BerriAI/litellm/blob/v1.91.1/litellm/proxy/guardrails/guardrail_hooks/unified_guardrail/unified_guardrail.py)
- [Credentials](https://docs.litellm.ai/docs/proxy/ui_credentials),
  [secret managers](https://docs.litellm.ai/docs/secret_managers/overview), and
  [Enterprise boundary](https://docs.litellm.ai/docs/enterprise)

### New API And One API

The active New API project is
[`QuantumNous/new-api`](https://github.com/QuantumNous/new-api), derived from
the original [`songquanpeng/one-api`](https://github.com/songquanpeng/one-api).
They are one lineage and are not double-counted. New API was checked at stable
[`v1.0.0-rc.20`](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.20)
and source commit `262ab931`; One API is used as the older lineage baseline.

Confirmed New API strengths:

- practical multi-provider/channel administration and multi-key channels;
- static priority plus weighted selection, retry, auto-disable, periodic
  recovery tests, and prompt-cache channel affinity;
- wallet, token quota, model/output/group ratios, cache-read/cache-create
  ratios, subscriptions and a broad public-selling feature set;
- provider-reported prompt-cache token accounting, including model-specific
  read/write ratios;
- token expiry/quota/model/IP restrictions;
- dashboards, request/upstream IDs, usage/cost logs, audit logs and performance
  aggregates;
- shared-database/Redis multi-node deployment guidance.

Important limits and corrections:

- weight is statically configured; no current-code proof supports dynamic
  latency/cost/error-rate weight adjustment.
- auto-disable/recovery is not a complete distributed closed/open/half-open
  circuit-breaker state machine.
- cache ratios price provider-reported prompt-cache tokens. They are not an
  embedded semantic response cache.
- tags are administrative bulk-operation and aggregation dimensions. Enforced
  quotas are based on users/tokens/groups, not a confirmed “tag quota.”
- relay rate limiting is request count by user over a window, not a complete
  per-key/per-model RPM/TPM/concurrency system.
- no integrated Presidio/LlamaGuard policy pipeline or live streaming output
  cut-off was confirmed.
- no general Vault/KMS/workload-identity credential system or complete
  OpenTelemetry/Prometheus exporter was confirmed.

Version-fixed official source examples:

- [New API lineage and overview](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/README.md#L404-L418)
- [Channel selection](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/model/channel_cache.go#L108-L200)
  and [relay retry](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/controller/relay.go#L181-L366)
- [Cache ratios](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/setting/ratio_setting/cache_ratio.go#L7-L177)
  and [settlement](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/service/text_quota.go#L230-L293)
- [Request limiter](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/middleware/model-rate-limit.go#L78-L190)
- [Sensitive-word implementation](https://github.com/QuantumNous/new-api/blob/262ab931231e8bd64e2a498703b145a18e1e0446/service/sensitive.go)

## 3. Capability Alignment

| Capability | UniRouter verified baseline | Competitive pressure | PRD6 judgment |
|------------|-----------------------------|----------------------|---------------|
| Provider breadth and adapters | OpenAI-compatible plus Anthropic-native adapter foundation | LiteLLM has materially broader provider/SDK reach | Advanced selective adapters; do not build an undifferentiated marketplace in MVP |
| Routing/failover | Priority routing, key selection, failure state and route evidence; current correctness/config gaps exist | LiteLLM has richer policies; New API has practical static priority/weight/retry | MVP Routing Policy V1, not full Scheduler V2 |
| Distributed policy | Redis API-key RPM/RPS; concurrency is process-local; no TPM | LiteLLM is broader but approximate; New API is narrower | MVP atomic RPM/RPS/TPM/concurrency with explicit consistency/failure semantics |
| Prompt-cache accounting | No authoritative cache-read/write token model | New API has strong ratios; LiteLLM has cost primitives | MVP normalize provider cache usage and price it; keep response cache separate |
| Response cache | No runtime response cache by accepted design | LiteLLM/Higress/Portkey offer exact/semantic cache | Advanced opt-in only after isolation, deletion, poisoning and billing decisions |
| Guardrails | Input sensitive-word DFA only | LiteLLM provides the strongest framework; New API remains basic | MVP policy kernel, PII adapter and output controls; build adapters, not a proprietary detector first |
| Streaming interception | SSE relay and metadata; no general output-policy hook | LiteLLM stable source has chunk hooks but non-zero disclosure limits | MVP bounded stream policy hook with explicit partial-disclosure/cancel/billing contract |
| Credential governance | AES-GCM protected static upstream keys | LiteLLM Enterprise integrates secret managers; competitors otherwise remain mostly stored credentials | Advanced reference/version/rotation design; workload identity later |
| Billing/governance | Wallet, transactional freeze/settle/cancel, model/org price controls | New API has deep public-selling/catalog mechanics | Preserve enterprise MaaS focus; fix correctness and add cache-aware accounting, not public-commerce scope |
| Evidence/observability | Strong gateway-native logs, trace decisions, evidence search and a sanitized external-consumption boundary | LiteLLM has broad callbacks; New API has practical dashboards | Keep UniRouter gateway-native evidence authoritative; external investigation remains asynchronously decoupled |

## 4. The Four Quadrants

This competitor-oriented quadrant is extended by the source-backed 100x/Agent
audit in `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md`.
Where the two differ in detail, the later source audit controls the current P0/P1
classification.

### Known Knowns

Verified implemented strengths:

- enterprise-local gateway, RBAC/projects, model governance, OIDC/license
  foundations, wallet/billing, usage/cost, runtime status and evidence search;
- priority-based routing, channel/key state, transactional billing, API-key
  model restrictions and Redis API-key RPM/RPS;
- encrypted stored upstream keys and HMAC-protected downstream keys;
- an accepted operator-mediated external receipt boundary with persistent,
  idempotent readback evidence.

Verified gaps or defects:

- half-open recovery and non-atomic/off-by-one rate limiting at the PRD6 base;
- incomplete retry classification, Embeddings failover parity and config truth;
- no TPM or distributed concurrency enforcement;
- no cache-read/cache-write token accounting;
- input-only global sensitive-word control and no generic output/stream policy
  hook;
- no general external secret reference/rotation or OTel/OpenInference export.
- source-confirmed tenant scoping, operation/idempotency, billing terminal-state,
  revocation and synchronous 100x bottlenecks detailed in the later audit.

### Known Unknowns

- expected replica count, tenant count, RPS/TPM and acceptable policy latency;
- whether hard quota must remain available during Redis loss or must be exact;
- provider cache-token fidelity and desired commercial price rules;
- guardrail categories, redaction versus blocking policy, false-positive budget,
  and maximum permissible partial stream disclosure;
- which credential backends matter first;
- whether an exact or semantic response cache has enough measured value to
  justify its security/retention surface.

### Unknown Knowns

Current code already contains useful but incomplete foundations that can reduce
implementation cost:

- retry/circuit/cooldown configuration fields exist but are not consistently
  wired into routing;
- channel RPM/TPM, degradation and channel-key model fields exist in schema or
  admin surfaces but are not consistently enforced;
- normalized `TextDelta` streaming paths provide a natural interception seam;
- Redis, trace decisions, structural evidence fields and route/failover UI are
  already present;
- the accepted external boundary can carry sanitized policy evidence later
  without making any investigation product a request-path dependency.

These are foundations, not completed features.

### Unknown Unknowns

PRD6 verification must actively search for:

- split-brain or double-admission behavior under Redis timeout/failover;
- retry and client-disconnect combinations that duplicate spend or settlement;
- oversized/malformed SSE frames and protocol-specific termination failures;
- Unicode/encoding evasions, delayed PII across chunk boundaries, and content
  already disclosed before a block;
- response-cache cross-tenant leakage, poisoning, stampede and stale
  authorization;
- provider usage-schema drift, cache token misclassification and price-table
  drift;
- secret rotation partial failure and stale-worker credential use.

## 5. Priority Matrix

| Tier | Capability | Product value | Main risk/dependency | Required acceptance proof |
|------|------------|---------------|----------------------|---------------------------|
| MVP / P0 | Batch 0A half-open recovery and atomic exact limiter | Prevents silent outage and over-admission | Accepted existing defects; no new product decision | focused race/concurrency tests, full backend CI, no behavior drift outside defect |
| MVP / P0 | Batch 0B tenant/operation/reservation integrity | Prevents cross-tenant access, charge identity collision, pool starvation and invalid terminal settlement | Proposed after the source-backed anti-pattern audit; remediation decision pending | tenant-negative tests, idempotency migration, pool barriers, terminal races and restart recovery |
| MVP / P0 | Batch 0C retry, endpoint parity, config and multiplier correctness | Prevents misleading failover and financial loss | Proposed; error, routing and settlement semantics must be accepted | adapter/state-machine/billing invariants, fault injection, full backend CI |
| MVP / P1 | Agent Capability And Distributed Policy V1 | Enterprise tenant/ancestor isolation and predictable capacity | failure-mode, capability, revocation, Redis and output-token semantics | delegation attenuation, 100-child aggregate limits, multi-instance atomicity/load/failure tests, shadow/enforce modes |
| MVP / P1 | Routing Policy V1 | Reliable failover with operator-understandable behavior | retry classifier, health ownership, config migration | Chat/Embeddings parity, deterministic failover/fallback traces, fault injection |
| MVP / P1 | Prompt-cache usage and billing | Cost accuracy without cache security expansion | provider-specific usage mapping and price ownership | golden adapter fixtures, cache read/write ledger and reconciliation tests |
| MVP / P1 | Guardrail kernel and non-stream output control | Minimum enterprise safety and PII control | policy ownership and external detector availability | tenant policy tests, redaction/block traces, raw-payload non-retention proof |
| MVP / P1 | Streaming policy hook and cut-off | Limits unsafe live disclosure | buffer/latency tradeoff and protocol semantics | adversarial chunk tests, disconnect/cancel/billing proof, measured exposure bound |
| Advanced / P2 | Secret references, versions and rotation | Reduces long-lived credential blast radius | KMS/Vault/cloud selection | rotation rollback, stale-worker and audit tests |
| Advanced / P2 | Exact/semantic response cache | Latency and cost reduction | tenant isolation, deletion, poisoning, retention, billing | isolation/TTL/purge/stampede/red-team evidence and opt-in control |
| Advanced / P2 | Cost/latency/rate-aware and canary routing | Efficiency and rollout safety | trustworthy telemetry and policy explainability | shadow comparison, bounded rollback, route-decision evidence |
| Advanced / P2 | Policy versioning/simulation and BYO guardrails | Safe enterprise customization | configuration lifecycle/RBAC | versioned audit, dry-run diff, rollback and compatibility tests |
| Long-tail / P3 | Broad SDK/provider ecosystem | Adoption breadth | sustained maintenance and compatibility cost | provider-specific conformance suites |
| Long-tail / P3 | Plugin/marketplace or full Scheduler V2 | Ecosystem/extensibility | supply-chain, sandbox and rewrite risk | separate PRD and security model; not implicit PRD6 scope |
| Long-tail / P3 | Managed HA/IaC and customer SLA operations | Production operations | target/cloud/commercial model | selected target architecture and production evidence; not a source-gap claim |
| Rejected by default | Public payments, recharge, referral and resale mechanics | Low fit for current enterprise-local position | commercial model divergence | only reopen through explicit product-positioning decision |

## 6. Recommended Differentiation

UniRouter should not win by copying the largest feature list. The defensible
direction is an enterprise-local gateway whose routing, policy, spend and
evidence all agree:

1. one versioned policy decision determines admission, routing, guardrails and
   billing evidence;
2. cache savings are observable and billable before response caching is added;
3. stream safety exposes measured residual disclosure instead of claiming
   impossible zero leakage;
4. gateway-native evidence remains authoritative and sanitized, while any deep
   cross-system investigation stays asynchronously off the request path.
