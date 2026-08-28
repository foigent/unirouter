# Endpoint Protocol Conformance V1

> Scope: PRD7 B6 / P7-F08
> Contract: `unirouter.endpoint-protocol.v1`
> Base-profile evidence level: local fixture verified, zero network
> Public execution extension: exact M77/M86/M87-qualified Responses/Messages
> with separately versioned text-SSE and bounded Messages client-tool contracts

## Purpose

This layer freezes strict, endpoint-specific request and response semantics for
versioned endpoint protocols:

- OpenAI Responses, versioned stateless text profiles;
- Anthropic Messages, versioned native text and bounded client-tool profiles;
- Cohere Rerank V2, string-document profile;
- OpenAI GPT Image generation, single inline-image profile, now consumed only
  by the repository-local asynchronous image job plane.

It is deliberately separate from Provider Conformance Pack V1. The existing
pack manifests and adapter behavior remain immutable. The historical
`Profile` interface still does not turn local contract evidence into runtime
support. M77 adds a separate, closed `PublicNonStreamRegistry` and public
execution authority; it does not widen the historical `Profile` contract.

## Fixed Profiles

| Profile | Upstream path | Runtime state | Admitted semantics |
|---------|---------------|---------------|--------------------|
| `unirouter.openai.responses.text.v1` | `/v1/responses` | `local_contract_only` | one string text input, optional bounded metadata/plain-text config, stateless `store=false`, non-streaming text output |
| `unirouter.openai.responses.text.v2` | `/v1/responses` | `local_contract_only` | the same bounded stateless text request with exact non-stream/stream modes; strict core-text SSE only |
| `unirouter.anthropic.messages.text.v1` | `/v1/messages` | `local_contract_only` | native `user`/`assistant` text messages, optional string system prompt, non-streaming text output |
| `unirouter.anthropic.messages.text.v2` | `/v1/messages` | `local_contract_only` | native text with exact non-stream/stream modes |
| `unirouter.anthropic.messages.text-tools.v3` | `/v1/messages` | `local_contract_only` | text plus bounded non-stream client tools; tool streaming and server tools rejected |
| `unirouter.cohere.rerank.v2` | `/v2/rerank` | `local_contract_only` | text query, 1–1000 string documents, bounded `top_n`, ordered exact relevance scores |
| `unirouter.openai.images.gpt-image.v1` | `/v1/images/generations` | `repository_local_image_v1` | GPT Image model, one non-streamed base64 result and exact provider usage partitions |

The registry is closed over these seven exact profiles. The original four
addresses remain immutable; the three appended identities do not reinterpret
v1. There is no runtime registration hook. The table reports manifest runtime
states, not deployment readiness. `repository_local_image_v1` records a
source-code wiring fact, not deployment, credential, object-store, paid-traffic
or production evidence. Public Responses/Messages execution additionally needs
the separate exact public registry, model contract and sealed binding described
below. A local manifest alone cannot authorize it; Cohere remains contract-only.

Executable profiles are resolved only by the exact immutable
`(profile_id, profile_version)` pair recorded by the manifest. There is no
implicit latest-version selection or version fallback. The ID-only lookup is a
single-version compatibility API and fails as ambiguous if an ID ever has more
than one registered version. A request Binding freezes both fields, so a parser
for another version cannot consume it.

## Request Boundary

All profiles require:

- an HTTPS origin-only base URL;
- a nonblank secret that is never included in an error;
- explicit requested-model to mapped-model binding;
- strict UTF-8 JSON with recursive duplicate-key rejection;
- bounded request size and unknown-field rejection;
- deterministic POST method, path, content type and model replacement.

The Responses request uses a `map[string]json.RawMessage` envelope so admitted
integers and nested JSON are never converted through `float64`. This is a
lossless parsing property, not permission to execute unknown semantics. Only
the explicitly admitted request fields are forwarded; unknown top-level or
nested text-control fields fail before provider dispatch. The M77 executor uses
this same closed base subset and requires production-approved authority before
the common executor can dispatch it. An immutable Model Execution Profile may
compose a further typed request sub-contract only after Planner selects its
exact ID/version and route evidence.
Metadata is the narrow exception because the current OpenAI contract defines it
as at most 16 bounded string pairs and it does not select tools, state, output
modality or execution behavior.

For Responses, `max_output_tokens` is a canonical positive int64 and the
current official API reference minimum of 16 is enforced. `temperature`,
`top_p`, metadata, `text.format={"type":"text"}` and the documented verbosity
values are validated without rewriting their values. The mapped model is the
only caller-provided field replaced; `store=false` is made explicit. V1 also
makes `stream=false` explicit, while v2 preserves the strictly typed requested
non-stream/stream mode.

Current Sol/Terra/Luna Responses `@1.3.0` are the current such compositions. They
admit only `reasoning={"effort":"none|low|medium|high|xhigh|max"}` through the
`reasoning_effort` capability evidence identity. Omission is forwarded as
omission, preserving OpenAI's documented `medium` default. Historical
Responses addresses retain their exact prior grammar.

OpenAI profiles propagate an optional printable-ASCII
`X-Client-Request-Id` of at most 512 bytes. The Cohere profile does not invent
or forward an unsupported request-ID header. The Anthropic profile uses
`x-api-key` plus the fixed `anthropic-version: 2023-06-01` header and never
substitutes bearer authentication.

Known semantics outside the safe subset fail as
`unsupported_semantics`; they are never silently removed:

- Responses state, storage, background mode, tools, include expansions,
  reasoning controls other than the exact model-bound effort object,
  reasoning summaries, prompt templates, moderation, automatic truncation,
  structured/multimodal input, `response_format` or non-text `text.format`,
  unknown execution fields, plus any stream outside Sol/Terra/Luna exact v2 core-text
  SSE contract;
- Anthropic v1 rejects tools/tool blocks, non-text content, thinking, beta
  features, cache controls, structured output and streaming. V2 adds text
  streaming; v3 adds bounded non-stream client tools. Model-bound public
  thinking compositions remain separate; media, beta features, explicit cache,
  structured output, server tools and tool streaming remain outside B1;
- rerank document objects, returned documents and streaming;
- image multiple/partial results, hosted URLs, streaming and unsupported
  output controls.

## Response, Finality And Units

An HTTP 2xx response is insufficient. The profile must prove its own terminal
shape:

- Responses requires `object=response`, exact mapped-model identity,
  `status=completed`, completed assistant message/output-text items, null
  error/incomplete details, optional top-level `output_text` consistency and
  exact provider usage totals. Queued/in-progress/incomplete responses are not
  final successes; failed/cancelled responses are terminal upstream failures.
  Empty completed reasoning metadata items may precede the text message. Under
  an exact model-bound reasoning composition in non-stream mode, a nonempty
  string `encrypted_content` is
  shape-validated and discarded; it is never delivered, billed as a distinct
  unit, persisted for replay or treated as customer-visible evidence. Nonempty
  reasoning summary/content remains rejected. The core Profiles continue to
  reject nonempty encrypted content;
- Anthropic Messages requires a terminal assistant `message`, only text content
  blocks, an admitted stop reason, exact mapped-model identity and provider
  input/cache-write/cache-read/output usage. Nullable or absent cache counters
  normalize to zero; positive provider-reported counters remain authoritative;
- rerank requires unique in-range source indexes, non-increasing exact decimal
  scores, API version `2`, a non-experimental response and positive integral
  `search_units`;
- image requires exactly one strict base64 result plus exact
  `input=text+image` and `total=input+output` token partitions.

All counters remain canonical nonnegative int64 decimal strings. Relevance
scores are parsed as exact base-10 rationals without binary floating point.
Exponent-form unit counters, overflow, duplicate keys, missing usage, partial
states, invalid partitions and unsupported output item types fail closed.
For Responses, absent or JSON-null input/output detail objects and null cache
or reasoning subcounters are accepted as unreported optional detail. Reported
cache counters cannot exceed input tokens, reported reasoning tokens cannot
exceed output tokens, and `total_tokens` must equal input plus output without
int64 overflow. The historical local parser may tolerate unknown non-control
response fields in its provider-observation outcome; they cannot create
observed units or customer debt. The public delivery parser is stricter:
unknown root, output/content or usage members are rejected and never delivered.

Every emitted unit has:

- `authority=provider_reported`;
- `finality=final`;
- `charge_state=provider_observed_not_customer_chargeable`.

The historical contract therefore records provider observations but does not
create customer debt, prices or ledger entries. M77 settlement uses a separate
versioned charge contract and never reinterprets the old outcome.

## Historical M77 Public Non-Stream Execution

The original M77 bindings for `POST /v1/responses` and `POST /v1/messages`
admit only the first strict, synchronous text subset. This section describes
those immutable v1 bindings, not the full current registry. Public execution resolves an
exact immutable eight-field identity: model operation, public path, executor
contract, endpoint profile ID/version, delivery contract, charge contract and
idempotency contract. The selected sealed release item must carry its own M77
public-execution binding; an older M75-qualified item never upgrades by
association with another release or binding.

The runtime boundary is deliberately narrower than the historical profile:

- Responses accepts exactly one `Authorization: Bearer` credential and rejects
  `x-api-key`, Anthropic version/beta headers, streaming and unknown semantics;
- Messages accepts exactly one nonblank `x-api-key`, exact
  `anthropic-version: 2023-06-01`, no `Authorization`, no `anthropic-beta`, and
  only native `user`/`assistant` string-content messages;
- both routes retain at most 8 MiB at the HTTP boundary, then apply the stricter
  2 MiB endpoint-protocol document limit, recursive duplicate-member rejection,
  input guardrails and a second strict prepare after any transformation;
- the caller's public model is used for API-key scope, governance, enterprise
  authorization and downstream delivery. Only the provider request receives
  the private mapped model;
- the Planner selects only an exact M77-qualified sealed item and a current
  credential/link/latest-own-probe. After one Freeze, the Coordinator opens an
  owner-fenced attempt first, then revalidates release item, binding, credential
  and probe authority in the same transaction before dispatch;
- response cache is always bypassed. `Idempotency-Key` is tenant, credential and
  canonical-payload-bound metadata-only at-most-once authority; response bytes
  are never stored or replayed;
- a customer body is built only from the strict whitelisted delivery struct,
  restores the public model, carries canonical provider usage, passes output
  guardrails and then follows delivery-started/completed/failed receipts before
  settlement. Guard block/error may retain parsed usage solely as
  provider-liability evidence; those bytes are never written to the client;
- a known provider rejection first completes the attempt and applies terminal
  health. Retry-authorized rejection may then fall back to the next candidate;
  non-retryable or exhausted rejection cancels the reservation. Transport
  ambiguity or an unusable 2xx response retains settlement-pending liability,
  performs no fallback and is never cancelled as if undispatched.

The three public contracts are
`unirouter.public-nonstream-delivery.v1`,
`unirouter.provider-final-token-charge.v1`, and
`unirouter.public-at-most-once.v1`. All four canonical token counters and their
input-plus-output total must fit the persisted signed-32-bit boundary before a
response can become customer-deliverable.

### B1 exact public extensions

M86 retains v1 and adds exact Responses/Messages v2 tuples; M87 adds the
Messages client-tools v3 tuple. Current Sol/Terra/Luna Responses compose v2
core-text streaming/reasoning. Current Claude Profiles compose Messages v2
text streaming/reasoning; Sonnet alone composes v3 non-stream client tools.
These tuples use their own delivery identities, not the v1 delivery literal.
Provider-final usage is still required for settlement; an interrupted stream
checkpoint without that evidence retains pending liability and cannot become
an estimated customer charge. Exact model subsets and local evidence are in
[the B1 status snapshot](42-model-execution-profile-capability-status.md).

## Error And Retry Boundary

HTTP status classification reuses the existing UniRouter failover classes:

- `401/403`: `auth`, not retryable;
- `408` and `500/502/503/504`: `channel_failure`, retryable;
- `429`: `rate_limit`, retryable;
- Anthropic `402`: `billing`, not retryable; `409`: `conflict`, not retryable;
  `413`: `client_request`, not retryable; `529`: `channel_failure`, retryable;
- other 4xx: `client_request`, not retryable;
- otherwise: `unknown`, not retryable without evidence.

`ProtocolError` exports only a stable code. Prompts, documents, image bytes,
API keys and provider response bodies are excluded.

## Immutable Local Evidence

Embedded artifacts live under `internal/endpointprotocol/artifacts`:

- one manifest Schema;
- seven manifests;
- seven synthetic fixtures with positive and negative cases;
- one deterministic golden report.

`uniroute-endpoint-conformance` runs without network access. The current report
contains 7 profiles and 65 passing cases. It verifies request bytes, model and
header behavior, terminal outcomes, exact units, unsupported semantics, nested
duplicate rejection and status classification. Direct Responses profile tests
add 64-bit preservation, unknown-field fail-closed behavior, nullable usage
details, reasoning/cache partitions, OpenAI HTTP error classes, and output-text
consistency.

## Compatibility And Non-Goals

This public extension does not add or change:

- any `/v1/embeddings` behavior, the legacy Chat/Embeddings executor, Messages
  tool streaming, or a Responses stream outside the exact v2 core-text SSE contract;
- adapter registration. Responses/Messages use the closed endpoint-protocol
  executor, not legacy `RouteResult` or the Chat-to-Anthropic adapter;
- Provider Conformance Pack V1 artifacts or their digests;
- completed-execution Outbox/Feed DTO, event identity, source bytes/hash,
  endpoint, page or cursor behavior;
- prompt-cache runtime integration;
- Responses background execution, conversation/previous-response state,
  stored responses, prompt templates, tools, structured or multimodal input,
  structured output, tool/reasoning deltas, include expansions, reasoning
  summaries or unknown provider execution extensions;
- Cohere rerank public execution or broader provider extension semantics.

P7-D03 authorized the repository-local Image V1 job-plane implementation.
Target deployment, storage lifecycle, credentials, paid traffic, and production
proof remain separate gates. A future runtime integration for rerank or video,
or any broader Responses/Messages profile, needs its own product decision,
persistence/pricing model, Endpoint Profile where applicable, and
target-evidence review.

`internal/asyncmedia/artifacts` remains the immutable pre-P7-D03 planning
snapshot. Its decision-gated fixtures and report describe what was true before
the User decision; their dependency digests are historical evidence and do not
track or redefine the current Endpoint Profile or Image V1 runtime contract.

## Primary Protocol References

- OpenAI Responses create API:
  <https://developers.openai.com/api/reference/resources/responses/methods/create>
- Anthropic Messages create API:
  <https://platform.claude.com/docs/en/api/messages/create>
- Anthropic API errors:
  <https://platform.claude.com/docs/en/api/errors>
- Anthropic prompt caching:
  <https://platform.claude.com/docs/en/build-with-claude/prompt-caching>
- OpenAI image generation API:
  <https://developers.openai.com/api/reference/resources/images/methods/generate>
- OpenAI client request ID contract:
  <https://developers.openai.com/api/reference/overview#supplying-your-own-request-id-with-x-client-request-id>
- Cohere Rerank V2 API:
  <https://docs.cohere.com/reference/rerank>
