# Model Execution Profile And Capability Status

> Status: accepted architecture direction and current implementation gap inventory.
> Snapshot date: 2026-08-23.
> This document is not a claim that every provider-documented feature is production supported.

## 1. What “supported” means

UniRouter must not describe a provider feature as supported merely because a JSON
field can pass through an OpenAI-compatible request. Every feature is evaluated
on five independent layers:

1. **Protocol** — the ingress can parse, bound, validate and preserve the field.
2. **Capability contract** — the selected model profile describes exact allowed
   values, defaults, incompatibilities and multi-turn preservation rules.
3. **Execution** — routing, guardrails, retries and response handling preserve the
   feature's semantics without inventing or dropping state.
4. **Metering** — provider usage and non-token charges are mapped to an exact,
   versioned wire schema and price authority.
5. **Evidence** — the exact provider surface, model, operation and capability have
   conformance or live evidence and, when required, production approval.

Provider documentation is evidence of what the provider advertises. It is not
evidence that UniRouter has implemented, metered or approved the same behavior.

## 2. Architecture decision

The supported direction is **generic protocol implementation plus composed,
versioned execution profiles**. It is neither one handler per model nor one flat
`capabilities[]` list.

The composition is:

- a protocol operation (`openai.chat_completions.v1`, `openai.responses.v1`,
  `anthropic.messages.v1`, and future Files/FIM/media operations);
- a provider-surface adapter only when the wire contract differs (for example
  Kimi Platform versus Kimi Coding, or DeepSeek Chat versus Responses);
- an immutable model execution profile composed from `tokenizer_profile`,
  `context/output limits`, `reasoning_profile`, `cache_usage_profile`,
  `protocol_profile`, multimodal/tool contracts and evidence references;
- operation-level deployment capabilities and conformance evidence;
- a separate, effective-dated pricing profile with an official default and an
  explicit customer override. Subscription/Token Plan routing is not fabricated
  into pay-as-you-go token pricing.

Handlers own authentication, governance, billing and evidence. Adapters own
wire transformation. Profiles own semantic validation. Model names must not
create provider-specific branches in public handlers.

When the caller omits a provider-owned default, the default remains omitted:

```text
Validate -> Preserve -> Route
```

UniRouter must not silently inject `reasoning_effort=max` or another provider
default merely because that is the provider's current documented behavior.

## 3. Current repository facts

The repository already contains the correct structural start:

- `internal/modelprofile.Profile` freezes tokenizer, reasoning, cache-usage and
  protocol identities and separates `ProviderCapabilities` from the narrower
  executable `Capabilities`.
- exact alias matching can propose a reviewed profile; unknown model names use
  `unverified.chat` instead of guessing limits or behavior.
- Kimi K2.6, K2.7 Code and K3 have modeled reasoning contracts. K3 validates
  `reasoning_effort=low|high|max`; omission preserves the provider default.
- DeepSeek Chat has a typed thinking-toggle/effort contract. It rejects unknown
  effort values, disabled-thinking plus effort, ineffective sampling fields
  and `tool_choice` while thinking is active, and tool-call history that drops
  preserved `reasoning_content`.
- Kimi Platform top-level `usage.cached_tokens` and DeepSeek hit/miss counters
  have profile-bound usage parsers. A generic OpenAI-compatible route cannot
  turn DeepSeek-shaped fields into billing evidence. Generic nested
  `prompt_tokens_details.cached_tokens` is also recognized.
- Once one of those profile-bound provider counters is authoritative, the
  Provider Adapter preserves the original fields and mirrors cache reads into
  `prompt_tokens_details.cached_tokens` for generic OpenAI SDKs and Agent
  clients. This response projection does not create billing authority for an
  untyped route.
- Chat tools, JSON Object/Schema and preserved `reasoning_content` have common
  protocol machinery.
- `kimi.k2.6.api@1.3.0` and `kimi.k3.api@1.3.0` retain typed Chat contracts for
  final-assistant Partial Mode, K3 system-message Dynamic Tools, exact
  `$web_search` admission/evidence, and MIME-bounded inline image/video data
  URLs. K2.6 search requires explicit disabled thinking. Provider URL and file
  references remain outside this inline contract.
- `kimi.k2.6.api@1.3.0` freezes MoonshotAI's reviewed K2.6 `tiktoken.model`,
  regex and special-token identities. Admission counts the retained request with
  this exact tokenizer plus conservative framing instead of the byte upper bound;
  unknown and separately versioned Kimi models continue to fail closed on the
  byte upper bound.
- Kimi top-level `usage.cached_tokens` is normalized as cache-read evidence.
  A provider response ending in the documented `$web_search` tool call also
  produces one bounded `builtin_web_search_calls` meter. M83 freezes its exact
  Rate Card rational and persists its actual quantity and cost through
  settlement, recovery, receipts and call logs.
- `/v1/models?include=execution_profile` exposes an authenticated,
  authorization-filtered executable projection. `uniroute-openclaw-sync`
  validates and optionally writes the corresponding OpenClaw model metadata
  while retaining user-owned client fields.
- M82 pins `execution_profile_id/version` plus the effective public
  context/output limits onto each canonical v2 Release. Canonical Planner reads
  those pins from the selected stable/canary Release; changing `model_registry`
  affects only a newly authored Release. The pins participate in the immutable
  plan snapshot hash, while route safety may still narrow an upper bound.

The following are still real gaps:

- MiniMax profiles still name provider surfaces whose `reasoning_split`,
  `reasoning_details`, full assistant-history and cache semantics are not yet
  modeled end to end. Their executable capability set is therefore deliberately
  limited to standard tools; reasoning/cache remain provider-declared only.
- Kimi Coding cache response semantics are not yet an executable cache-usage
  contract. K2.7 Code and the K3 coding profile therefore do not claim cache
  billing authority even though the provider surface advertises caching.
- Kimi inline image/video support is locally contract-verified only. Large
  payloads remain subject to UniRouter's conservative byte upper bound; provider
  file IDs and remote URLs require separately governed Files/URL contracts.
- Kimi K2.7 Code has not inherited the K2.6/K3 Platform contract. Its coding
  surface, Partial Mode, cache and multimodal semantics still require their own
  exact profile and live evidence.
- public Responses and Anthropic Messages are intentionally strict non-stream
  text subsets. They reject tools, rich reasoning and multimodal payloads.
- there is no public Files API or FIM `/v1/completions` operation.
- DeepSeek Chat vision is not attached to Flash/Pro: the separate
  `deepseek-v4-flash-vision-exp` model still needs its own execution profile,
  route and operation evidence. DeepSeek beta prefix/FIM, native Responses,
  Anthropic and Files surfaces remain separate operation work rather than Chat
  field allowlisting.
- target model-registry labels are not a substitute for a canonical deployment,
  operation probe and capability evidence.

## 4. Current model-family status

| Model/family | Provider-documented surface | Current UniRouter support | Not yet production-supported |
|---|---|---|---|
| Kimi K2.6 | 262,144 context; thinking toggle; text/image/video; ToolCalls, JSON Mode, Partial Mode, automatic cache and built-in search | `kimi.k2.6.api@1.3.0`; exact frozen tokenizer admission; thinking/tool-history contract; standard tools; final-assistant Partial; exact non-stream built-in search meter; Kimi cache usage; MIME-bounded inline image/video | provider-file/remote-URL media; large-media estimator; JSON qualification and exact target live capability evidence; Dynamic Tools are not a K2.6 claim |
| Kimi K2.7 Code | 262,144 context; text/image/video; thinking-only; ToolCalls, JSON Mode, Partial Mode and automatic cache | versioned 256K profile; required-thinking contract; standard tools | coding-surface cache wire qualification; Partial Mode; multimodal admission; OpenAI and Anthropic coding conformance; complete JSON qualification |
| Kimi K3 | 1M provider maximum with a 256K current customer default; always-on reasoning; `low/high/max`; tools, JSON Object/Schema, Partial, dynamic tools, built-in search, vision/video and automatic cache | `kimi.k3.api@1.2.0`; exact reasoning; standard tools/JSON; final-assistant Partial; system-message Dynamic Tools; exact non-stream built-in search meter; Kimi cache usage; MIME-bounded inline image/video | provider-file/remote-URL media; large-media estimator; full target live multi-turn/capability conformance |
| DeepSeek V4 Flash/Pro | Chat, native Responses and Anthropic surfaces; thinking toggle/effort; tools; JSON; prefix completion; FIM; Files; KV cache. Vision belongs to the separate `deepseek-v4-flash-vision-exp` model | OpenAI-compatible Chat; typed thinking/effort, thinking-mode `tool_choice` rejection and preserved tool-history contract; standard tools/JSON; profile-bound cache hit/miss usage; versioned limits | live operation/capability conformance for the configured V4 identities; prefix beta; full Responses/Anthropic operations; Files/FIM; separate vision model qualification |
| MiniMax M2.5/M2.7/M3 | M2.x 204,800 total window; M3 1M and image/video input; M3 adaptive/disabled thinking; M2.x required thinking; tools and nested cache usage; Token Plan is quota/subscription access | versioned limits; standard function tools; exact tokenizer only for M2.7; MiniMax response-envelope normalization; reasoning/cache labels are provider-declared, not executable | MiniMax typed reasoning/history/cache contracts; M3 multimodal admission; `reasoning_split`; cache/pricing qualification; Token Plan quota evidence. Customer cost remains zero until an independently verified pay-as-you-go key/price is configured |

Official references used for this snapshot:

- [Kimi K2.6 pricing and capability summary](https://platform.kimi.com/docs/pricing/chat-k26)
- [Kimi token estimate API](https://platform.kimi.com/docs/api/estimate) and
  [frozen K2.6 tokenizer source](https://huggingface.co/moonshotai/Kimi-K2.6/tree/7eb5002f6aadc958aed6a9177b7ed26bb94011bb)
- [Kimi K2.7 Code pricing and capability summary](https://platform.kimi.com/docs/pricing/chat-k27-code)
- [Kimi K3 pricing](https://platform.kimi.com/docs/pricing/chat-k3) and
  [reasoning effort](https://platform.kimi.com/docs/guide/use-reasoning-effort)
- [Kimi context caching](https://platform.kimi.com/docs/guide/use-context-caching-feature-of-kimi-api),
  [Partial Mode](https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api) and
  [ToolCalls](https://platform.kimi.com/docs/guide/use-kimi-api-to-complete-tool-calls)
- [DeepSeek thinking](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode),
  [vision](https://api-docs.deepseek.com/zh-cn/guides/vision),
  [Responses](https://api-docs.deepseek.com/zh-cn/guides/responses_api),
  [Anthropic](https://api-docs.deepseek.com/zh-cn/guides/anthropic_api),
  [FIM](https://api-docs.deepseek.com/zh-cn/guides/fim_completion) and
  [Files](https://api-docs.deepseek.com/zh-cn/guides/files_api)
- [MiniMax API overview](https://platform.minimaxi.com/docs/api-reference/api-overview),
  [OpenAI Chat contract](https://platform.minimaxi.com/docs/api-reference/text-chat-openai) and
  [Token Plan](https://platform.minimaxi.com/docs/token-plan/intro)

## 5. Required next implementation order

1. **Truth first (implemented for this slice):** DeepSeek now has typed
   reasoning/cache contracts; unmodeled MiniMax and Kimi Coding axes have been
   removed from executable capability claims. Never let catalog labels outrun
   ingress.
2. **Release authority (implemented in M82):** profile identity and public
   limits are sealed into the existing Release/Plan authority. Profile edits do
   not mutate live traffic; they require authoring and promoting a new Release.
   Pricing remains independently frozen by Billing because capability and price
   versions have different lifecycles.
3. **Separate provider surfaces:** Kimi Platform/Coding, DeepSeek
   Chat/Responses/Anthropic, and MiniMax Token Plan/pay-as-you-go need distinct
   protocol and metering identities even when they share a model family.
4. **Qualify the existing eight models:** create canonical deployment operations,
   exact live probes and per-capability evidence before marking them production
   approved.
5. **Close the Chat mainline:** Kimi Partial/cache/tool history; DeepSeek live
   thinking/JSON/tools/cache conformance; MiniMax reasoning/cache, then
   multimodal admission.
6. **Add new operations deliberately:** DeepSeek full Responses/Anthropic,
   Files/FIM; Kimi dynamic tools and built-in search. Each needs its own admission,
   metering, evidence and negative fixtures.

Dynamic tool loading should first be supported as an exact provider contract.
UniRouter must not become an agent runtime by inventing a cross-provider dynamic
tool engine in this phase. Provider built-in search must be distinguishable from
agent or MCP tools because execution authority, enterprise policy and pricing are
different.

## 6. Competitive reference

The dominant product pattern is also a unified API plus provider transformations
and a model catalog, not one handler per model:

- [OpenRouter](https://openrouter.ai/docs/guides/overview/models) exposes model
  context, modalities, tokenizer, supported parameters and granular pricing
  (including cache and web-search units).
- [LiteLLM](https://docs.litellm.ai/) combines provider-specific request/response
  transformations with shared model cost and capability metadata.
- [Portkey](https://portkey.ai/docs/product/model-catalog) exposes a governed
  model catalog, default pricing and
  [customer model overrides](https://portkey.ai/docs/product/model-catalog/model-overrides).

UniRouter should not copy only their breadth. Its product differentiation is to
bind model metadata to immutable profile versions, exact target deployment
evidence, financial authority and explainable execution receipts. A provider
catalog entry is useful discovery data; it is not production approval.
