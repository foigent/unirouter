# Versioned Pricing Profiles And Client Projection

> Status: implemented repository contract; target activation and provider-live
> evidence remain environment-specific.
> Snapshot date: 2026-08-24.

## 1. Pricing authority

Request pricing is an independent, versioned Rate Card authority. A model
execution profile never owns customer or supplier prices, and a provider
adapter never invents a discount ratio.

Each active Rate Card version fixes:

- a stable provider and model reference;
- currency and integer amount scale;
- one absolute `amount_minor / unit_quantity` rational per meter;
- an effective `[from, to)` interval;
- optional complete UTC daily bands;
- source-evidence identity and payload digest;
- lifecycle state and maker/checker actors.

The four token meters are always explicit:

```text
input_tokens
cache_read_tokens
cache_creation_tokens
output_tokens
```

For example, Kimi K2.6 list prices of CNY 6.50 ordinary input and CNY 1.10
cache-read input per million tokens are stored as two independent components:

```text
input_tokens       = 6_500_000 micro-CNY / 1_000_000 tokens
cache_read_tokens  = 1_100_000 micro-CNY / 1_000_000 tokens
```

No six-decimal ratio is involved. The same representation can exactly retain
fractions such as `1/30`, regardless of whether that ratio remains present on a
later provider price page. Current provider prices must always be imported with
dated source evidence instead of reconstructed from an old ratio.

The official pages checked on 2026-08-24 currently publish these flat CNY
prices per million tokens:

| Model | Ordinary input | Cache-read input | Output | Current daily bands |
|---|---:|---:|---:|---|
| DeepSeek V4 Flash | 1.00 | 0.02 | 2.00 | none published |
| DeepSeek V4 Pro | 3.00 | 0.025 | 6.00 | none published |
| Kimi K2.6 | 6.50 | 1.10 | 27.00 | none published |
| Kimi K2.7 Code | 6.50 | 1.30 | 27.00 | none published |
| Kimi K3 | 20.00 | 2.00 | 100.00 | none published |
| MiniMax M2.7 | 2.10 | 0.42 | 8.40 | none published |
| MiniMax M2.7 Highspeed | 4.20 | 0.42 | 16.80 | none published |
| MiniMax M2.5 Highspeed | 4.20 | 0.21 | 16.80 | none published |
| GLM-5.3 / GLM-5.2 | 8.00 | 2.00 | 28.00 | none published |
| Qwen3.8-Max | 12.00 | 1.50 implicit / 1.00 explicit | 36.00 | none published |
| MiMo V2.5 Pro | 3.00 | 0.025 | 6.00 | none published |
| MiMo V2.5 | 1.00 | 0.02 | 2.00 | none published |
| Hy3 | 1.00 | 0.25 | 4.00 | none published |
| Doubao Seed Evolving / Seed 2.1 Pro | 6.00 | 1.20 | 30.00 | none published |
| Doubao Seed 2.1 Turbo | 3.00 | 0.60 | 15.00 | none published |

The associated Kimi built-in web-search unit is CNY 0.03 per provider-reported
call. These are dated `provider_list` inputs, not permanent constants and not
customer contract prices. A future provider schedule can use complete UTC
daily bands, but UniRouter must not retain or fabricate an old band schedule
when the current official source publishes a flat rate.

Sources: [DeepSeek models and pricing](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/),
[Kimi current model prices](https://platform.kimi.com/) and
[Kimi web-search pricing](https://platform.kimi.com/docs/pricing/tools), and
[MiniMax pay-as-you-go pricing](https://platform.minimaxi.com/docs/guides/pricing-paygo),
[GLM pricing](https://bigmodel.cn/pricing),
[Qwen pricing](https://help.aliyun.com/zh/model-studio/qwen3-8-max),
[MiMo pricing](https://mimo.mi.com/docs/en-US/price/pay-as-you-go),
[Hy3 pricing](https://cloud.tencent.com/document/product/1759/127342), and
[Doubao pricing](https://www.volcengine.com/product/doubao).

The same snapshot retains USD, without FX conversion, for Gemini, OpenAI, xAI
and Anthropic. Flat Gemini Developer API **Standard** token rates can be
imported as USD review-only drafts: Gemini 3.7 Flash promotional input/output/
cache-read are USD 0.75/3.75/0.075 through 2026-12-31; Gemini 3.5 Flash are
1.50/9.00/0.15; and Gemini 3.5 Flash-Lite are 0.30/2.50/0.03 per million
tokens. Cache storage, search, Batch/Flex/Priority and later promotional rates
remain separate dimensions.

The catalog records, but does not flatten, provider price schedules that the
current Rate Card cannot select without loss: GLM-5V-Turbo and Qwen3.7
context-length bands; GPT-5.6 service tiers and long-context multipliers; Grok
4.6 short/long-context tiers and tool calls; Claude 5-minute versus 1-hour cache
writes; and MiniMax M3 context/service tiers. Their exact official summaries
and source URLs are shown during onboarding with `manual_review_required`.

## 2. Draft, approval and override

Official-source ingestion is a source connector boundary, not a price authority.
The supported API accepts an already retrieved payload digest, source URL,
source revision, fetch/freshness times and transform version, then atomically
creates a `provider_list` **draft**. It performs no server-side web fetch and
cannot approve or activate the result.

An administrator must review and approve a distinct version before activation.
Replacing an active card retires the old version and activates the reviewed
successor atomically; requests already frozen against the old version do not
change.

Customer/supplier-contract pricing remains an explicit override. Runtime
selection prefers one unambiguous active `supplier_contracted` card over an
active `provider_list` card. It fails closed when the winning layer has no rate
or more than one provider candidate.

The model-onboarding UI may compose a dated, repository-reviewed official price
snapshot into the same supported `import-official-draft` API. It sends the exact
retrieved-payload SHA-256, a bounded freshness window and deterministic draft
and evidence references. A replay first attempts the same deterministic import,
then performs a read-only exact-evidence check instead of creating duplicate
drafts. This composition is still a separate write after the atomic model
application create: failure leaves the model draft intact and is reported as a
partial success. It never approves, activates or changes
`REQUEST_PRICING_AUTHORITY`.

MiniMax Token Plan profiles do not imply pay-as-you-go prices. M2.x amounts are
offered only after the operator explicitly confirms an ordinary pay-as-you-go
API Key. MiniMax M3 is not auto-transformed: its current price depends on the
512K input threshold and `service_tier`, dimensions the current UTC-band Rate
Card cannot represent without loss. Kimi built-in-search price comes from a
separate official source and is likewise not silently spliced into the model
price payload; it requires its own reviewed evidence composition.

## 3. Time and currency boundary

Rate Card storage supports an explicit ISO currency on every amount and exact
effective intervals. Daily bands are versioned in UTC and must repeat the full
meter set, so an interval cannot accidentally inherit a stale cache or tool
price.

The current prepaid request wallet settles only CNY at micro-CNY precision.
The official importer may retain a single-currency USD `provider_list` draft
and its source evidence, but approval still runs the CNY request-pricing
validator and fails closed. Mixed-currency drafts are rejected. A non-CNY card
is not request-executable until a separately governed wallet/FX settlement
contract exists. UniRouter does not silently convert currencies.

## 4. Freeze and receipts

Before provider dispatch, Billing Freeze selects exactly one active Rate Card
and content-addresses the complete quote. It persists the Rate Card reference
and version, selected UTC band, source evidence, component hashes and exact
amount/unit rationals. Settlement and recovery reuse that frozen version even
if an administrator publishes a newer card concurrently.

Migration `000083_versioned_unit_meter_billing` extends the same contract to
non-token meters. The first executable unit is
`builtin_web_search_calls`. Freeze records its conservative maximum quantity;
settlement accepts only profile-bound provider evidence and persists the actual
quantity, exact unit rate and non-token cost in the reservation, journal,
observation receipt and call log. A missing rate or actual quantity above the
reservation fails closed.

M83 also versions source evidence for mixed token/unit payloads as
`versioned_rate_card_components.v1`. Its guarded down refuses both durable
meter evidence and source evidence that the M82 token-only constraint cannot
represent; rollback never deletes either form of evidence.

Kimi `$web_search` is represented as:

```text
builtin_web_search_calls = 30_000 micro-CNY / 1 call
```

It is charged only from the documented provider response condition, independently
from the token components. Observation mode continues to generate zero customer
charge while retaining the same evidence.

## 5. OpenClaw projection

`GET /v1/models` keeps its ordinary OpenAI-compatible shape by default.
Authenticated callers may opt in with:

```text
GET /v1/models?include=execution_profile
```

The extension contains only the caller's already-authorized models and
repository-reviewed executable capabilities. Unregistered, unverified or
invalid profile bindings are labeled explicitly and receive no inferred limits
or capabilities. `verified` is static Profile/limit validation, not evidence
that the current request combination has an executable route. With a canonical
stable Release, `source=stable_release` identifies its sealed Profile and limits;
`request_default_output_tokens` is the existing Profile policy default narrowed
by that sealed public ceiling. This value is not automatically an official
Provider recommendation. Registry-only history has `source=registry` and no
active request default. Invalid stable pins do not fall back to registry values.

`uniroute-openclaw-sync` consumes this projection and maps it to OpenClaw's
model-level `contextWindow`, `maxTokens`, `reasoning`, `input` and safe `compat`
fields. It reads the existing provider model list, preserves user-owned fields
such as `cost`, `params`, `headers` and unrelated compatibility overrides, and
removes models no longer authorized by the key. An unverified profile clears
previously synchronized execution fields so stale metadata cannot masquerade as
current verification.

The sync preserves valid conservative local context/output ceilings and explicit
`params.maxTokens`, `params.max_completion_tokens` or `params.max_tokens`
overrides. A missing request default is written via supported `params.maxTokens`,
not by replacing the `maxTokens` capability ceiling. Invalid/conflicting aliases
and values outside the retained/effective limits stop before any write. A server
without stable/default provenance is reported, never guessed from its maximum.
Dry-run reports model, source, selected limits/default, retained-override status
and exact thinking-control projection; unrelated params and credentials are not
printed. `parameter_controls` describes the selected immutable Profile's existing
implementation references without changing Definition bytes or Release identity.
Admin catalogs expose the same description for an explicitly selected candidate.
It distinguishes output limit, on/off shape, native effort, aliases and independent
thinking budget. A 262144 thinking budget does not replace a 131072 public output
ceiling. `stream_output_accounting=utf8_byte_upper_bound` describes the current
conservative stream guard, not provider-reported tokens; a request may reach that
guard before its declared token maximum.

The normal installed OpenClaw host and its selected Chat transport are the
supported client path. Qwen's boolean-only contract offers off and one low menu
choice meaning enabled, not native low effort. Independently valid root effort
contracts project exact native values and explicit aliases. Their enabled toggle
default stays omitted. This projection does not configure thinking budgets;
raw body overrides need separate validation. Sync refuses explicit reasoning or
output fields in `params.extra_body`/`extraBody` that could overwrite its verified
projection. Unrelated body fields remain untouched.
The installed normal host has an ID-triggered DeepSeek V4 wrapper. For those
client IDs, matching typed contracts use its deepseek format with verified
off/high only, preserving active reasoning history through tool continuation.
Other stable aliases use the generic root-effort path without an inferred toggle.
An incompatible typed contract stops sync; model routing is never inferred by ID.
Aliases targeting native max are withheld alongside the max menu: advertising max
also introduces ultra in this host, while omitting max from its accepted set would
silently downgrade the wire value. Both are unsuitable for a verified projection.

When no faithful control path exists, no thinking menu is advertised and the
request retains provider defaults. This host can label its empty-menu fallback
off even though it sends no disabling field; that label does not disable required
or provider-default thinking. Unsupported controls remain visible in the report.
The report lists provider defaults by exact field and marks the client default
`host_selected_not_measured_by_sync`; an offline projection cannot measure the
installed host's chosen level. Version-bound transport tests do measure it. Static
control projection does not prove tools/thinking/stream request qualification.

Previously supplied thinking maps or formats that conflict with the exact
projection stop synchronization before any config write. New-to-invalid and
new-to-old-server transitions also stop rather than leave stale explicit controls
or guess ownership. Nullable menus constrain normal host selection; they cannot
recover an unknown original SDK label after a standalone SDK has transformed it.
Gateway validation continues to reject invalid parameters it actually receives.

The command never accepts a key on argv and never prints it. It performs an
OpenClaw schema dry-run first; only explicit `--write` changes the config:

```bash
export UNIROUTER_BASE_URL='https://gateway.example.internal/v1'
export UNIROUTER_API_KEY='replace-at-runtime'

go run ./cmd/uniroute-openclaw-sync --provider unirouter
go run ./cmd/uniroute-openclaw-sync --provider unirouter --write
```

This is a client projection, not a second execution authority. Runtime routing
retains exact Release pins and current safety checks; sync does not activate a
Release or change historical execution identity.

Client-specific transport checks are optional local regressions. Select the
installed version's `OPENCLAW_TRANSPORT_MODULE`, `OPENCLAW_RUNTIME_HOST_MODULE`
and, if needed, `OPENCLAW_NODE_BINARY`, then run the sync package's
`TestInstalledOpenClawTransportConsumesProjectedDefaults`. It calls the actual
payload builder with synthetic input and a no-network guard, not a live Agent.
Set `OPENCLAW_THINKING_MODULE` and `OPENCLAW_EXTRA_PARAMS_MODULE` from the same installation to run
`TestInstalledOpenClawThinkingControlsReachActualTransport`: it checks the actual
host menu/default, normal extra-parameter wrapper and captures first and tool-result turns at onPayload before
dispatch, with fetch/undici network guards. This is client-contract evidence, not
a paid Provider observation or full Agent success.

## 5.1 Optional request-level preflight

`POST /v1/chat/completions/preflight` accepts the actual Chat request with the
same key and Chat operation authorization. It reuses canonical Profile,
capability, routing and credential-free outbound preparation. It reports safe
exclusion codes such as `unsupported_capability:streaming`, not route secrets or
internal successor names. It does not dispatch, decrypt, reserve execution
quota/funds, claim a half-open lease or create execution/qualification evidence.

The response is a non-cacheable local snapshot. A locally admissible route does
not prove dynamic quota availability, content-policy/compression outcomes, live
credential validity, Provider response delivery or Agent task completion.
Normal requests still run every authoritative check. Clients need not invoke
preflight before every request; SDK configuration alone is not a compatibility
guarantee. No persistent Agent-ready flag or client-specific model directory is
introduced.

Provider cache usage has a separate response projection. When the selected
execution profile makes Kimi's top-level `cached_tokens` or DeepSeek's exact
hit/miss pair authoritative, the OpenAI-compatible response retains those
provider fields and adds the equivalent
`prompt_tokens_details.cached_tokens`. Generic OpenClaw/OpenAI clients can then
display cache reads without changing the frozen Billing evidence or trusting
cache-like fields from an unverified provider surface.
