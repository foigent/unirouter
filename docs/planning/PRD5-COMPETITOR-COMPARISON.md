# PRD5 Competitor Comparison And Feature Catch-Up

> Date: 2026-07-10
> Status: Source-grounded PRD5 planning comparison with Batch 1J through Batch
> 1N evidence-readiness catch-up slices accepted and Batch 1O handoff prep fixed
> awaiting Canary.
> Purpose: compare UniRouter against adjacent gateway and observability systems,
> then decide which gaps belong in UniRouter PRD5 versus X-Insight.

## Source Basis

Local project sources:

- X-Insight `docs/discussion/gateway-adapter-mapping-matrix-v0.1.md`
- X-Insight `docs/discussion/market-landscape-architecture-analysis.md`
- X-Insight `docs/discussion/adapter-integration-strategy.md`
- UniRouter current repo schema and PRD4 trace/log implementation

Official source refresh on 2026-07-08:

- LiteLLM proxy logging, callbacks, OpenTelemetry, and callback configuration:
  `https://docs.litellm.ai/docs/proxy/logging`,
  `https://docs.litellm.ai/docs/observability/callbacks`,
  `https://docs.litellm.ai/docs/observability/opentelemetry_integration`,
  `https://docs.litellm.ai/docs/proxy/config_settings`
- New API repository and changelog:
  `https://github.com/QuantumNous/new-api`,
  `https://www.newapi.ai/en/docs/guide/wiki/changelog`
- one-api repository and English README:
  `https://github.com/songquanpeng/one-api`,
  `https://github.com/songquanpeng/one-api/blob/main/README.en.md`
- Langfuse docs and repository:
  `https://langfuse.com/docs`,
  `https://langfuse.com/docs/observability/overview`,
  `https://github.com/langfuse/langfuse`
- Helicone docs and repository:
  `https://docs.helicone.ai/gateway/overview`,
  `https://docs.helicone.ai/features/advanced-usage/custom-rate-limits`,
  `https://github.com/Helicone/helicone`

Refresh this document before public claims, pricing claims, or adapter
implementation because these projects move quickly.

## Positioning Summary

| Product | Primary Center | UniRouter-Relevant Strength | Natural Limit For UniRouter Planning |
|---------|----------------|-----------------------------|--------------------------------------|
| UniRouter | Local-deploy enterprise AI gateway/MaaS | RBAC, billing/wallet, usage/cost, model governance, OIDC/license, runtime status, PRD4 trace foundation, Chinese 2B deployment fit | Lacks explicit multi-agent structural evidence fields, native exporter/outbox, ecosystem adapter story, and mature trace/eval UI |
| LiteLLM | OpenAI-compatible proxy/SDK/gateway | Broad provider compatibility, spend logs, callbacks, OpenTelemetry, metadata, team/org/user/session/agent/MCP-adjacent fields | Not a governance evidence contract by itself; payload-heavy logs require policy controls |
| New API | Private AI API gateway/management | Practical gateway breadth, organization auth, multi-model management, usage analytics, cost accounting, private deployment, upstream request correlation in recent logs | Historical/import fidelity, not native execution governance; `other`/log fields are not a stable governance schema |
| one-api | OpenAI-format API relay/management | Simple private deployment, token/channel/quota/cost basics, broad local ecosystem familiarity | Lower evidence fidelity; no first-class trace/span/capture-gap/decision model observed in current research |
| Langfuse | LLM observability/evals/prompt platform | Strong trace UX, debugging, cost/latency, evals, prompt management, self-hosting | App instrumentation/eval center, not gateway admission/routing/billing control |
| Helicone | AI gateway plus LLM observability | Fast base-URL adoption, sessions/users/custom properties, cost tracking, prompt management, caching, rate limits, gateway routing/fallbacks | Strong SaaS/developer orientation; not an enterprise execution-governance evidence contract by default |
| X-Insight | AI/Agent evidence governance plane | Cross-source import, capture gaps, evidence refs, operator investigation, policy simulation, future adapters | Separate product; should not be copied into UniRouter UI |

## Capability Matrix

| Capability | UniRouter Today | LiteLLM | New API | one-api | Langfuse | Helicone | PRD5 Decision |
|------------|-----------------|---------|---------|---------|----------|----------|---------------|
| OpenAI-compatible gateway | Strong | Strong | Strong | Strong | Not primary | Strong gateway path | Keep UniRouter focus |
| Provider routing/failover | Existing gateway routing/circuit foundation | Strong | Strong | Practical | Not primary | Strong | Improve evidence around route decisions |
| API keys/RBAC/projects | Strong enterprise local model | Team/org/user support | Org/user/token management | User/token/channel | Project/user metadata | Org/user/session properties | Keep UniRouter-owned |
| Usage/cost/billing | Strong billing/wallet/call logs | Spend logs | Usage/cost accounting | Quota/token logs | Cost tracking | Cost tracking | Add source refs and structural evidence |
| Quota/rate limits | RPM/RPS vocabulary and key limits | Gateway limits | Gateway limits | Quota controls | Not primary | Custom rate limits | Later scheduler/hard budget separately |
| Trace/span semantics | Request trace events but no explicit span fields | OTel/callback-friendly | Logs/import | Low fidelity | Strong trace UX | Strong observability | Add PRD5 structural fields |
| Capture gaps/provenance | Not first-class | Not native governance | Not native | Not native | Partial via missing spans | Partial via logs | UniRouter should emit enough for X-Insight |
| Payload policy | Default no raw prompt/response in trace | Payloads may be logged/callbacked | Depends on logs/config | Depends on logs/config | Prompt/completion capture central | Request observability central | UniRouter keeps default-off policy metadata |
| Export/callback/OTel | Not native | Strong callbacks and OTel | Import/API path | Import path | SDK/API/OTel-adjacent | API/gateway logs | Add exporter/outbox only after Gate A |
| UI trace/debug | Lightweight Logs trace | Admin/UI ecosystem | Admin UI | Admin UI | Mature trace/evals UI | Mature observability UI | Keep UniRouter lightweight, deep-link later |
| Prompt/eval management | Not a gateway need | Not core | Not core | Not core | Strong | Strong prompt mgmt | Leave to X-Insight or out of UniRouter |

## UniRouter Advantages

- Strong local-deploy enterprise gateway baseline with RBAC, project model
  governance, wallet/billing, OIDC, license foundation, runtime status, and
  gateway request handling already integrated.
- PRD4 trace/log data is generated inside the actual gateway request path, so it
  has better gateway-native fidelity than a generic historical import.
- Metadata redaction and no-prompt/no-response trace posture are already safer
  for Chinese 2B private deployments.
- UniRouter can become the highest-fidelity reference source for X-Insight
  without making X-Insight a runtime dependency.

## UniRouter Gaps To Catch Up In PRD5

High priority, now partially accepted through Batch 1A/1B/1C, Batch 1D,
Batch 1E, Batch 1F, Batch 1G, Batch 1H, Batch 1I, Batch 1J, Batch 1K,
Batch 1L, Batch 1M, and Batch 1N:

- explicit `trace_id`, `span_id`, `parent_span_id`, and `session_id` handling;
- `schema_version`, `source_component`, `capture_mode`, and capture-gap model;
- `evidence_ref` / source refs and `source_event_hash`;
- decision records for admission, auth, quota, budget, routing, provider,
  billing, payload capture, retry/failover, and trace sampling;
- `actor_type`, `actor_ref`, and `on_behalf_of` for future agent/delegation
  chains;
- metadata-only payload policy fields and raw-payload default-off controls;
- controlled snapshot plus later cursor export/read-only views.

Accepted in Batch 1D:

- `upstream_request_id` capture where providers return a trusted response header.

Accepted in Batch 1E:

- event-level `capture_gap='redacted'` when UniRouter intentionally drops,
  shape-rejects, or truncates trace metadata during safe evidence capture.

Accepted in Batch 1F:

- restricted `PAYLOAD_CAPTURE_MODE` configuration for `metadata_only` and
  `disabled`, with unsupported payload modes rejected until later approval.

Accepted in Batch 1G:

- default-off trusted evidence context intake for internal trace/session,
  actor/ref, and delegation propagation, gated by `EVIDENCE_CONTEXT_TRUST_SECRET`.

Accepted in Batch 1H:

- per-request gateway root span parenting for UniRouter-emitted trace events,
  preserving trusted inbound parents for the root and parenting later UniRouter
  spans to that root.

Accepted in Batch 1I:

- explicit `payload.policy.applied` trace events for chat and embeddings,
  recording metadata-only or disabled payload policy decisions without raw
  prompt/response capture.

Accepted in Batch 1J:

- provider response trace events now mark `capture_gap=provider_not_returned`
  when no sanitized provider-returned upstream request ID exists, preserving
  returned IDs and redaction gaps.

Accepted in Batch 1K:

- controlled snapshots can be paired with a DSN-free Markdown handoff note for
  X-Insight import review.

Accepted in Batch 1L:

- repeated same-component trace events parent to the component's first span while
  preserving the request gateway root.

Accepted in Batch 1M:

- provider-stage failures that occur before an upstream response source exists
  now use `capture_gap=not_available_from_source`.

Accepted in Batch 1N:

- generated Gate A snapshot gap records can include candidate capture gaps for
  missing trace rows without asserting one definitive cause.

Medium priority:

- request-id and source-ref search ergonomics in the trace UI;
- X-Insight execution deep links once X-Insight has stable IDs/URLs;
- optional OpenTelemetry/OpenInference bridge after native evidence fields are
  stable;
- better route/failover decision visibility comparable to gateway-focused
  competitors.

Low priority or separate scope:

- prompt management, eval datasets, playground workflows, and full trace/eval UI;
- semantic cache/runtime cache;
- full public adapter ecosystem in UniRouter itself;
- full Scheduler V2, plugin marketplace, managed HA/IaC product, and hard budget
  blocking without separate User approval.

## Ownership Decision

UniRouter PRD5 should chase gateway evidence quality, not observability product
surface area.

Add to UniRouter:

- structural gateway evidence fields;
- safe export/snapshot/view path;
- lightweight trace UI improvements;
- source refs and capture gap metadata;
- provider/upstream request correlation where available.

Leave to X-Insight:

- cross-system timeline and node detail;
- evidence packs;
- capture-gap panel and import diagnostics;
- policy simulation UI;
- LiteLLM/New API/one-api public adapter strategy;
- prompt/eval observability product features.
