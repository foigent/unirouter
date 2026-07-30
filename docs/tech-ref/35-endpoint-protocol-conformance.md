# Endpoint Protocol Conformance V1

> Scope: PRD7 B6 / P7-F08
> Contract: `unirouter.endpoint-protocol.v1`
> Evidence level: local fixture verified, zero network

## Purpose

This layer freezes strict, endpoint-specific request and response semantics for
three protocols that are not yet part of the UniRouter Gateway runtime:

- OpenAI Responses, text-only stateless profile;
- Cohere Rerank V2, string-document profile;
- OpenAI GPT Image generation, single inline-image profile.

It is deliberately separate from Provider Conformance Pack V1. The existing
pack manifests and adapter behavior remain immutable. Endpoint Protocol V1
does not dynamically register provider code, extend IAM/routing/billing, or
turn contract evidence into runtime support.

## Fixed Profiles

| Profile | Upstream path | Runtime state | Admitted semantics |
|---------|---------------|---------------|--------------------|
| `unirouter.openai.responses.text.v1` | `/v1/responses` | `local_contract_only` | one text input, stateless `store=false`, non-streaming text output |
| `unirouter.cohere.rerank.v2` | `/v2/rerank` | `local_contract_only` | text query, 1–1000 string documents, bounded `top_n`, ordered exact relevance scores |
| `unirouter.openai.images.gpt-image.v1` | `/v1/images/generations` | `decision_gated_p7_d03` | GPT Image model, one non-streamed base64 result and exact provider usage partitions |

The registry is closed over those three profiles. There is no runtime
registration hook.

## Request Boundary

All profiles require:

- an HTTPS origin-only base URL;
- a nonblank bearer secret that is never included in an error;
- explicit requested-model to mapped-model binding;
- strict UTF-8 JSON with recursive duplicate-key rejection;
- bounded request size and unknown-field rejection;
- deterministic POST method, path, content type and model replacement.

OpenAI profiles propagate an optional printable-ASCII
`X-Client-Request-Id` of at most 512 bytes. The Cohere profile does not invent
or forward an unsupported request-ID header.

Known semantics outside the safe subset fail as
`unsupported_semantics`; they are never silently removed:

- Responses state, storage, background mode, tools, structured/multimodal
  input, structured output and streaming;
- rerank document objects, returned documents and streaming;
- image multiple/partial results, hosted URLs, streaming and unsupported
  output controls.

## Response, Finality And Units

An HTTP 2xx response is insufficient. The profile must prove its own terminal
shape:

- Responses requires `object=response`, `status=completed`, completed
  assistant message/output-text items, null error/incomplete details and exact
  provider usage totals;
- rerank requires unique in-range source indexes, non-increasing exact decimal
  scores, API version `2`, a non-experimental response and positive integral
  `search_units`;
- image requires exactly one strict base64 result plus exact
  `input=text+image` and `total=input+output` token partitions.

All counters remain canonical nonnegative int64 decimal strings. Relevance
scores are parsed as exact base-10 rationals without binary floating point.
Exponent-form unit counters, overflow, duplicate keys, missing usage, partial
states, invalid partitions and unsupported output item types fail closed.

Every emitted unit has:

- `authority=provider_reported`;
- `finality=final`;
- `charge_state=provider_observed_not_customer_chargeable`.

The contract therefore records provider observations but does not create
customer debt, prices or ledger entries.

## Error And Retry Boundary

HTTP status classification reuses the existing UniRouter failover classes:

- `401/403`: `auth`, not retryable;
- `408` and `500/502/503/504`: `channel_failure`, retryable;
- `429`: `rate_limit`, retryable;
- other 4xx: `client_request`, not retryable;
- otherwise: `unknown`, not retryable without evidence.

`ProtocolError` exports only a stable code. Prompts, documents, image bytes,
API keys and provider response bodies are excluded.

## Immutable Local Evidence

Embedded artifacts live under `internal/endpointprotocol/artifacts`:

- one manifest Schema;
- three manifests;
- three synthetic fixtures with positive and negative cases;
- one deterministic golden report.

`uniroute-endpoint-conformance` runs without network access. The current report
contains 3 profiles and 25 passing cases. It verifies request bytes, model and
header behavior, terminal outcomes, exact units, unsupported semantics, nested
duplicate rejection and status classification.

## Compatibility And Non-Goals

This batch does not change:

- `/v1/chat/completions` or `/v1/embeddings`;
- Gateway routes, adapter registration, routing, policy, IAM or billing;
- Provider Conformance Pack V1 artifacts or their digests;
- database Schema or persisted data;
- completed-execution Outbox/Feed DTO, event identity, source bytes/hash,
  endpoint, page or cursor behavior;
- response-cache runtime integration;
- deployment, traffic, credentials or external provider state.

P7-D03 still gates an image/media runtime, object store, retention and billing
trigger. A future runtime integration for Responses or rerank needs its own
Gateway, pricing, persistence and target-evidence review.

## Primary Protocol References

- OpenAI Responses create API:
  <https://developers.openai.com/api/reference/resources/responses/methods/create>
- OpenAI image generation API:
  <https://developers.openai.com/api/reference/resources/images/methods/generate>
- OpenAI client request ID contract:
  <https://developers.openai.com/api/reference/overview#supplying-your-own-request-id-with-x-client-request-id>
- Cohere Rerank V2 API:
  <https://docs.cohere.com/reference/rerank>
