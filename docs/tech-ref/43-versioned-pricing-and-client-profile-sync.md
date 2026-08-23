# Versioned Pricing Profiles And Client Projection

> Status: implemented repository contract; target activation and provider-live
> evidence remain environment-specific.
> Snapshot date: 2026-08-23.

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

## 3. Time and currency boundary

Rate Card storage supports an explicit ISO currency on every amount and exact
effective intervals. Daily bands are versioned in UTC and must repeat the full
meter set, so an interval cannot accidentally inherit a stale cache or tool
price.

The current prepaid request wallet settles only CNY at micro-CNY precision.
A non-CNY Rate Card may be retained by the generic finance domain, but it is not
request-executable until a separately governed wallet/FX settlement contract
exists. UniRouter does not silently convert currencies.

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
or capabilities.

`uniroute-openclaw-sync` consumes this projection and maps it to OpenClaw's
model-level `contextWindow`, `maxTokens`, `reasoning`, `input` and safe `compat`
fields. It reads the existing provider model list, preserves user-owned fields
such as `cost`, `params`, `headers` and unrelated compatibility overrides, and
removes models no longer authorized by the key. An unverified profile clears
previously synchronized execution fields so stale metadata cannot masquerade as
current verification.

The command never accepts a key on argv and never prints it. It performs an
OpenClaw schema dry-run first; only explicit `--write` changes the config:

```bash
export UNIROUTER_BASE_URL='https://gateway.example.internal/v1'
export UNIROUTER_API_KEY='replace-at-runtime'

go run ./cmd/uniroute-openclaw-sync --provider unirouter
go run ./cmd/uniroute-openclaw-sync --provider unirouter --write
```

This is a client projection, not a second execution authority. Runtime routing
continues to use legacy `candidate.v1` while compatibility mode is selected;
M82 canonical Release pins remain unchanged until a separately authorized
canonical cutover.
