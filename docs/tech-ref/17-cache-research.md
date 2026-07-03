# PRD2 Cache Research

> Scope: P2-F15 research spike only. Runtime prompt/response caching is not enabled by default in PRD2.

## Decision

PRD2 records cache as a future opt-in capability, not a shipped runtime behavior. The current Batch 23 implementation keeps Playground and request troubleshooting metadata-only: prompts and responses are not persisted by default.

## Exact Cache

Exact cache hashes a normalized request and reuses a response only when the request is byte-equivalent under approved normalization rules.

Risks:

- Tenant isolation must include org, project, API key or policy scope, model alias, resolved model, provider family, system prompt, tool schema, temperature, and any safety settings.
- Billing must define cache-hit pricing and distinguish provider cost from UniRouter charge.
- Audit must record cache hit/miss, lookup scope, model alias, and request id without storing plaintext by default.
- Correctness depends on stable normalization. Seemingly small fields such as stream mode, tool calls, max tokens, response format, and model lifecycle can change output.

Potential future shape:

- Disabled by default.
- Per org/project opt-in.
- Cache key includes org/project/model/policy scope.
- Cached value encrypted at rest.
- TTL and purge controls are mandatory.
- Audit event for every hit and administrative purge.

## Semantic Cache

Semantic cache matches similar prompts through embeddings or another similarity signal. It has materially higher risk than exact cache.

Risks:

- Cross-tenant leakage is possible if vector indexes or thresholds are shared incorrectly.
- Similarity matches can return plausible but wrong responses for business-critical tasks.
- Prompt and response storage policy becomes mandatory because semantic cache needs retained text or embeddings.
- Billing and audit semantics are harder: a semantic hit may avoid provider cost but still consume internal search and retrieval resources.
- Model/version drift can make old semantic answers stale even when the prompt is similar.

PRD2 recommendation: do not implement semantic cache in runtime. Treat it as PRD3+ research after prompt/response storage policy, encryption, tenant-isolated indexes, retention, and audit-on-read are approved.

## Acceptance Boundary

Batch 23 can be accepted for P2-F15 only if:

- The repository clearly says no runtime cache is enabled by default.
- Exact and semantic cache risks are documented.
- Privacy, billing, audit, correctness, tenant isolation, invalidation, and opt-in controls are covered.
- Future implementation requires explicit approval.

