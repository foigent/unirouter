# PRD6 Batch 3 P6-F09 Provider Prompt Cache Billing — Repository Evidence

> Date: 2026-07-12
> Owner: Codex
> Implementation branch: `codex/prd6-prompt-cache-billing`, restored at exact
> head `711e7182c4875ed8324f6e379f73107ebcb30ffb`
> Base: post-P6-F08 closeout `main` at
> `2d09144b1e8a024079fae2931ca5420d489a8067`
> Integration: PR #174, squash merge
> `6515b3f3f281da022d7685700ec0754fe8ac3b66`; stability-only follow-up PR
> #175, squash merge `257a9ebe14a80f52073ff8b20c21aa9b6ee86931`
> Evidence level: CI verified / closed
> Scope: UniRouter repository only; no response cache, Cache Service,
> deployment, target mutation, real/paid provider request, credential change,
> extra database access, exporter/outbox or X-Insight runtime work

## 1. Confirmed Pre-Change Findings

The following were source-confirmed, not hypotheses:

1. Adapter usage had only inclusive prompt, completion and total token fields.
   Provider cache-read/cache-create token classes were discarded before
   billing.
2. Freeze/Settle priced every prompt token at one effective input price. The
   immutable price version did not contain cache ratios or effective cache
   prices.
3. `call_logs`, request trace evidence, Logs, model-price administration and
   reconciliation did not preserve or explain provider Prompt Cache classes.
4. `_uniroute.cache_hit` existed as response metadata but was hard-coded or
   otherwise not a provider-usage billing contract.
5. The future response Cache Service remains separately deferred to PRD7. It
   is not the same product surface as provider Prompt Cache accounting.

## 2. Version-Fixed Provider Contract

The implementation was checked against current official provider contracts on
2026-07-12:

- [OpenAI Chat Completions usage](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
  defines `prompt_tokens_details.cached_tokens` and the newer
  `cache_write_tokens` field;
- [Anthropic prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
  defines uncached `input_tokens`, `cache_read_input_tokens`,
  `cache_creation_input_tokens`, and the 5-minute/1-hour creation detail; and
- [DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion)
  defines `prompt_cache_hit_tokens`, `prompt_cache_miss_tokens`, and their
  exact sum relationship to `prompt_tokens`.

Normalized `prompt_tokens` is always the inclusive input total. It must equal:

```text
uncached_prompt_tokens + cache_read_tokens + cache_creation_tokens
```

Accepted source schemas are explicit:

| Source contract | Normalized schema | Behavior |
|---|---|---|
| OpenAI `cached_tokens` only | `openai.prompt_tokens_details.v1` | cached subset is cache read; the remainder is ordinary input |
| OpenAI with `cache_write_tokens` | `openai.prompt_tokens_details.v2` | read/write subsets are separate; the remainder is ordinary input |
| DeepSeek hit/miss | `deepseek.prompt_cache.v1` | hit is cache read; miss is ordinary input; exact partition required |
| Anthropic read/create | `anthropic.prompt_cache.v1` | inclusive prompt is uncached + read + create; 5m + 1h detail must equal creation total when both are present |

Provider fields that are unknown, misspelled or not in one of these contracts
remain ordinary input and produce `cache_usage_status=not_reported`. A negative
or contradictory partition rejects the normalized provider response rather
than creating billing truth. DeepSeek's paired hit/miss contract rejects a
partial one-field response instead of assuming the missing class is zero.
Any provider count or combined total above the persisted PostgreSQL integer
boundary is rejected before billing/logging can diverge. Pricing documentation
alone never manufactures a token count.

`_uniroute.cache_hit` is true only when a recognized provider schema reports
`cache_read_tokens > 0`. The response also contains a separate
`_uniroute.provider_prompt_cache` object with token classes, source status,
schema and the frozen pricing receipt. It is not a response-cache hit signal.

## 3. Pricing And Financial Semantics

- Platform administrators own base input/output price and per-model
  `cache_read_ratio` / `cache_creation_ratio`.
- Both ratios default to `1`, preserving prior cost until an operator makes an
  explicit future-request change. Valid range is 0 through 100.
- Organization multiplier still applies after the base class ratio. For an
  input price `I`, organization multiplier `M`, read ratio `R` and creation
  ratio `C`, effective prices are `I*M`, `I*R*M` and `I*C*M`.
- Freeze uses the largest effective input/read/create/output class so a
  cache-write premium cannot recreate the earlier under-freeze defect.
- `pv2_` pricing identity includes base prices, both cache ratios,
  organization multiplier, price timestamps and source. Every reservation
  snapshots all four effective prices and both ratios before wallet mutation.
- Settle stores the exact token partition, provider schema/status and decimal
  class-cost breakdown. The existing integer-cent terminal ledger rule remains
  unchanged.
- Streaming final usage persistence and recovery carry the same cache fields.
  Identical replay is idempotent; a different partition or source contract is
  rejected both before and after the terminal settlement receipt.

The real-PostgreSQL acceptance fixture froze effective prices of 8 cents
ordinary input, 0.8 cache read, 10 cache creation and 40 output per 1K tokens.
After the database prices and multiplier were changed, settlement of 1,000
ordinary + 2,000 read + 500 creation + 100 output tokens still used the frozen
receipt: class costs 8 + 1.6 + 5 + 4 = 18.6 cents, current integer terminal cost
18 cents, one terminal event and a 22-cent refund from the 40-cent hold.

## 4. Persistence, API And UI

Migration `000026_prd6_prompt_cache_billing` adds:

- per-model read/create ratios;
- immutable reservation effective cache prices, ratios, token partition,
  source schema/status and class-cost breakdown; and
- call-log token partition, pricing version/effective prices, cache class
  costs and an organization/status/time index.

Existing rows are backfilled as ordinary input. The up migration permits old
binaries to keep writing legacy rows while every ratio remains 1. Runtime
activation must be non-rolling: old binaries must be drained before any ratio
change or cache-aware billing is exposed. After a non-default ratio, `pv2_`
reservation, or cache-aware log exists, down refuses destructive rollback and
requires roll-forward.

Operator surfaces now provide:

- model-price read/create ratio controls with plain-language examples (`0.1`
  means 10%, `1.25` means 125%) and a response-cache boundary warning;
- backward-compatible price upserts: an older client that omits the two cache
  ratios gets `1` for a new model but preserves an existing model's explicitly
  configured ratios instead of silently resetting financial policy;
- request-log list/detail cache token partition, source contract, frozen
  ratios/effective prices and class costs; the original ratios come from the
  same immutable reservation receipt, with a legacy effective-price fallback;
  and
- reconciliation totals/rows that keep preflight-vs-actual variance separate
  from authoritative provider Prompt Cache read/write usage and cost.

“估算与实际总偏差” is explicitly actual total minus preflight estimate; it is
not a cache discrepancy. “未回传” means UniRouter did not receive a recognized
provider contract and therefore did not infer a hit or discount.

## 5. Acceptance Evidence Map

| Acceptance claim | Evidence |
|---|---|
| OpenAI read/write normalization | `TestOpenAICompatibleParseResponse_CacheUsageFixtures`: v1 read, v2 read/write, write-only, zero-hit, unknown field and invalid partitions |
| DeepSeek hit/miss normalization | same fixture matrix with exact hit+miss partition and contradiction rejection |
| Anthropic non-stream/stream normalization | `TestAnthropicNativeParseResponse_CacheUsage`, creation-detail contradiction test and stream partition test |
| no fake cache billing | unknown cache-like fields stay `not_reported`; partial DeepSeek pairs are rejected; `_uniroute.cache_hit` assertions require authoritative read tokens |
| gateway/settle/log/trace/response propagation | Anthropic non-stream and stream gateway tests assert SettleRequest, 50-field call-log evidence, sanitized cache trace metadata and final response extension |
| immutable pricing and idempotent financial state | `TestP6F09PromptCachePriceFreezeSettlementAndIdempotency` on migrated PostgreSQL, including pre/post-terminal conflict rejection |
| admin/reconciliation readback | real PostgreSQL and handler tests cover omitted-ratio compatibility, explicit model ratios, log detail cache fields, cache totals/costs and tenant-scoped reconciliation |
| UI semantics | Model Prices, Logs and Reconciliation Vitest coverage distinguishes provider Prompt Cache from response cache |
| rollback | `TestP6F09PromptCacheBillingMigrationUpDownGuardAndRollForward` proves safe default down→up plus refusal after ratio or `pv2_` activation |

## 6. Local Verification

Passed on 2026-07-12:

```text
git diff --check
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
REQUIRE_TEST_DATABASE=1 go test -v -race ./internal/billing -run TestP6F09 -count=1
REQUIRE_TEST_DATABASE=1 go test -v -tags=integration -race ./internal/migrate -run TestP6F09PromptCacheBillingMigrationUpDownGuardAndRollForward -count=1
REQUIRE_TEST_DATABASE=1 go test -v -race ./internal/admin -run 'TestP6F34PriceAuthorityIntegration/legacy_price_upsert_preserves_omitted_cache_ratios' -count=1
frontend: npm run type-check
frontend: npm test -- --run (29 files / 144 tests)
frontend: npm run build
OpenAPI YAML parse
```

The frontend build retains the pre-existing large-chunk warning. The local
macOS race linker retains its pre-existing malformed `LC_DYSYMTAB` warning;
the race test exits successfully. Local Docker verification was unavailable on
this machine and was supplied by required CI.

## 7. Required CI, Integration And Closure

The complete integration truth chain is:

1. Exact implementation head
   `711e7182c4875ed8324f6e379f73107ebcb30ffb` passed PR #174 required run
   `29180539621`: Backend Tests 5m56, Frontend Build 3m12 and Docker Build
   1m04. Backend visibly passed both explicit P6-F09 PostgreSQL steps.
2. PR #174 was squash-merged as
   `6515b3f3f281da022d7685700ec0754fe8ac3b66`.
3. Its first post-merge main run `29180730830` was not accepted as closure:
   Frontend passed, Backend failed in the pre-existing P6-F21
   `TestP6F21FairQueueEventuallyAdmitsLongWorkUnderShortContention` fixture,
   and Docker was consequently skipped. The failure occurred in the full test
   step before the explicit P6-F09 gates; the exact P6-F09 implementation head
   had already passed all required jobs.
4. The production-neutral follow-up only increased that miniredis race
   fixture's store timeout from 50ms to the existing 500ms bounded-wait policy.
   It did not change production workload-admission defaults, Lua, weights or
   fairness. Exact follow-up head
   `8a7fefe2a5e42ac8adcd9ac8838bc9ea8cb93fb7` passed PR #175 run
   `29180946857`: Backend Tests 6m34, Frontend Build 3m38 and Docker Build
   1m01.
5. PR #175 was squash-merged as
   `257a9ebe14a80f52073ff8b20c21aa9b6ee86931`. Final post-fix main run
   `29181137210` passed Backend Tests 5m28, Frontend Build 3m35 and Docker
   Build 1m02, including full tests and both explicit P6-F09 PostgreSQL gates.

P6-F09 is therefore **CI verified / closed**. Batch 3 remained active at this
point because P6-F20 was a separate accepted durable-outbox requirement;
P6-F20 was later closed independently by PR #177 and post-merge main run
`29194150134`.

## 8. Explicit Non-Claims

- No target, production, throughput-capacity or 100x verification.
- No response cache, semantic cache or independent Cache Service.
- No automatic provider pricing import or billing based on unrecognized
  provider fields.
- No real or paid provider request; provider compatibility evidence is fixture
  and official-contract based.
- This P6-F09 evidence does not prove P6-F20 general outbox/worker completion;
  that separate row is evidenced by PR #177. It also does not prove P6-F10
  Guardrail completion or P6-F11 semantic streaming-policy completion.
- No deployment, ECS/security-group change, credential change, extra database
  access, exporter/outbox activation or X-Insight work.
