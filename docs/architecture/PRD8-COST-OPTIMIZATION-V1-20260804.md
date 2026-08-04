# PRD8 Cost Optimization V1

> Work item: `UR-PRD8-COST-OPTIMIZATION-V1`
> Date: 2026-08-04
> Product owner: UniRouter
> Status: CO-0 contract frozen; CO-1 Local verified candidate
> Runtime mutation: none

## 1. Outcome

UniRouter adds an enterprise **Cost Optimization** workspace under
`成本与财务`. It explains cost reduction from facts UniRouter already owns and
keeps behavior-changing controls in `模型治理`. It is not a generic MaaS
coupon/cash-account screen and it does not absorb X-Insight investigation,
Tool Gateway execution caches, or MCP Gateway protocol/session behavior.

The first release is read-only. It uses existing terminal request metering and
request-time frozen prices. It does not enable Response Cache, prompt
compression, new traffic, new credentials, deployment, or database migration.

## 2. Four-owner boundary

| Capability | Fact owner | Analysis owner | Enforcement owner | UI host |
|---|---|---|---|---|
| Provider Prompt Cache token classes and request-time price evidence | UniRouter | UniRouter | Provider behavior plus current UniRouter pricing | UniRouter Cost Optimization |
| Exact model Response Cache | UniRouter | UniRouter | UniRouter Gateway, only after an explicit runtime gate | UniRouter Cost Optimization + Model Governance |
| Prompt compression for model requests | UniRouter | UniRouter for deterministic token/quality gates; X-Insight may analyze long-window effects | UniRouter Gateway, only after Shadow/Canary | UniRouter Model Governance; read-only outcomes in Cost Optimization |
| Cross-execution root cause, Case, Finding, long-window causal analysis | Product Source Owners | X-Insight | Action owner after bounded proposal review | X-Insight |
| Tool result cache | Tool Gateway | Tool Gateway / X-Insight | Tool Gateway | Tool Gateway |
| MCP resource/session cache | MCP Gateway or upstream Server | MCP Gateway / X-Insight | MCP Gateway or upstream Server | MCP Gateway |

## 3. Truth hierarchy

### 3.1 Current measurable driver

Provider Prompt Cache is the only driver included in realized savings V1.
An eligible request must satisfy all of the following:

1. terminal HTTP status is `1..399`;
2. usage source is provider-authoritative (`upstream`, `api`, or the preserved
   legacy blank value);
3. `prompt_tokens > 0`;
4. `cache_usage_schema` is present and `cache_usage_status` is reported;
5. `pricing_version` is present; and
6. `uncached_prompt_tokens + cache_read_tokens +
   cache_creation_tokens = prompt_tokens`.

A successful upstream request that does not meet these conditions remains in
the coverage denominator. It is a Gap and must not be interpreted as zero
savings.

### 3.2 Exact amount formulas

All persisted and API amounts are decimal-string micro-CNY with
`amount_scale = 1000000`. Each request uses the immutable prices captured at
request time.

```text
ordinary_input_baseline = round_half_up(
  (prompt_tokens * effective_input_price
   + completion_tokens * effective_output_price) / 1,000,000
)

actual_terminal_cost = call_logs.cost_micros
net_savings = ordinary_input_baseline - actual_terminal_cost

cache_read_savings = round_signed(
  cache_read_tokens * (effective_input_price - effective_cache_read_price)
  / 1,000,000
)

cache_creation_premium = round_signed(
  cache_creation_tokens
  * (effective_cache_creation_price - effective_input_price)
  / 1,000,000
)

rounding_adjustment =
  net_savings - cache_read_savings + cache_creation_premium
```

For nonnegative baseline costs, `round_half_up` rounds a half micro upward.
For signed driver deltas, `round_signed` rounds to the nearest micro with ties
away from zero, matching PostgreSQL exact `numeric` `ROUND`; no floating-point
value participates in the calculation.

`net_savings` is authoritative for the read-only customer-pricing analysis.
The other three values explain drivers and rounding. Negative net savings is
preserved and displayed; it is not clamped to zero.

### 3.3 Explicitly excluded amounts

- These amounts are customer pricing/showback evidence, not supplier COGS.
  Supplier COGS remains in Provider reconciliation evidence.
- Missing cache reporting is not a cache miss and is not assigned zero value.
- The local `internal/responsecache` package is not imported by Gateway, so no
  Response Cache hit or saving exists in current runtime evidence.
- Compression has no Shadow population, quality guard or token delta evidence,
  so its savings are not estimated.
- Prompt and response content remains `not_stored_by_default`; opportunity
  rules use counters, prices, dimensions and terminal status only.

## 4. Product decisions frozen on 2026-08-04

1. A future exact Response Cache hit has provider request count and provider
   cost `0`; default actual-cost showback is `0`. A future service fee or quota
   is a separate commercial mechanism and must never masquerade as Provider
   cost.
2. Response Cache identity binds the realized route outcome, including actual
   channel, credential epoch, runtime allocation and every existing v1 scope,
   policy, price and request-byte input. It is not merely a model-name cache.
3. Compression starts `default_off` and then `shadow_only`; no request payload
   mutation may occur before quality, safety, token and latency evidence is
   reviewable.
4. Read-only outcomes live at `成本与财务 → 成本优化`. Any later setting that
   changes requests lives at `模型治理 → 调用优化`, with explicit revision,
   validation, Shadow/Canary, rollback and audit semantics.

## 5. CO-1 API and RBAC

Read-only endpoints:

- `GET /api/v1/cost-optimization/summary`
- `GET /api/v1/cost-optimization/trend`
- `GET /api/v1/cost-optimization/breakdown`
- `GET /api/v1/cost-optimization/opportunities`

Platform, organization and billing administrators may read the workspace.
Platform administrators may select an organization. Other roles remain bound
to the authenticated organization; the backend reuses the existing tenant and
project filter authority. `group_by` is a closed whitelist: overall, project,
model, provider, or channel.

Opportunity V1 is deterministic and review-only:

- evidence coverage gaps;
- negative net Prompt Cache savings; and
- cache read tokens that have not yet amortized cache-creation tokens.

These are review candidates, not root-cause findings and not automatic policy
changes.

## 6. CO-1 UI contract

The page contains:

1. organization/time/project/model/provider/channel filters;
2. Prompt Cache, Exact Response Cache and Compression runtime-state cards;
3. net savings, read savings, write premium and evidence coverage KPIs;
4. daily/cumulative trend;
5. Savings-by-driver breakdown and deterministic review candidates; and
6. a desktop-persistent, auto-selected right Evidence Inspector with exact
   formulas, Gap context, request-log link, reconciliation link and bounded
   X-Insight integration link.

The top toolbar remains the already-frozen lightweight global toolbar. Cost
Optimization is a left-sidebar item and does not add a duplicate top
navigation strip.

## 7. Dependency-ordered continuation

| Batch | Scope | Entry gate | Runtime effect |
|---|---|---|---|
| CO-0 | Contract, formulas, ownership and UI placement | Product decisions above | none |
| CO-1 | Read-only aggregation/API/UI/opportunities | Existing terminal metering and frozen prices | none |
| CO-2 | Response Cache commercial ledger/Receipt contract, metrics and operator key-backend contract | CO-1 exact evidence plus decision 1 | none until separately authorized |
| CO-3 | Gateway Response Cache adapter, runtime switch, Shadow/Canary and rollback | CO-2 conformance, target key backend and explicit runtime authorization | gated |
| CO-4 | Compression measurement contract and shadow evaluator | Quality/safety metrics and corpus rules frozen | Shadow only |
| CO-5 | Bounded compression Canary | CO-4 acceptance and separate runtime authorization | gated |

CO-2 must preserve the current route-outcome identity decision and cannot
reuse Provider Prompt Cache pricing. CO-4/CO-5 cannot persist raw prompts by
default or put X-Insight in the synchronous request path.

## 8. Compatibility and non-goals

CO-0/CO-1 do not change:

- request admission, route selection, Provider dispatch, retries or failover;
- Provider credentials or credential epochs;
- current customer reservation, settlement, debt or ledger entries;
- existing Prompt Cache token normalization and frozen prices;
- request/Outbox identity, Source Snapshot bytes/hash, completed-execution
  Feed endpoint/DTO/page/Cursor, or X-Insight availability; or
- database schema, traffic, deployment or runtime configuration.

This batch does not claim Target, external integration or Production evidence.
No push, PR, merge, deployment, new credential, test traffic, data reset,
Cursor/Checkpoint or database mutation is authorized by this contract.

## 9. Local verification

CO-1 passed the full serialized Go suite against the local test database and
temporary migrated databases, `go vet`, all 55 frontend files / 365 tests,
TypeScript, the production build and bundle budget, route/OpenAPI/frontend-doc
parity, and synthetic 1440/1024/768 browser interaction checks. Exact commands,
responsive results, known warnings and the non-runtime boundary are recorded
in `docs/ops/evidence/PRD8-COST-OPTIMIZATION-CO0-CO1-LOCAL-20260804.md`.
