# V1 DeepSeek Flash Official Price — Target Evidence

**Date:** 2026-07-16 (Asia/Shanghai)

**Target:** existing Alibaba Cloud development/test ECS in `cn-wulanchabu`;
this is not a Production claim

**Result:** the target `DeepSeek-V4-Flash` customer price is now aligned with
DeepSeek's current public CNY rates for one dated pass-through pilot
configuration. The supported Admin API wrote the exact price, its transactional
audit preserves the prior and new values, and one separately approved minimal
request returned HTTP 200 and settled exactly for 7 micro-CNY. Stable/canary,
the signed release, migration, License, route mapping and supplier credential
were unchanged. The temporary API key and operator account are disabled, and no
pending billing or outbox state remains. The admin JWT was not persisted, but
account disablement does not cryptographically revoke an already issued token.

PRD6 remains `Completed (Accepted Scope)`. This separately authorized
Post-PRD6 target evidence closes only `V1-TARGET-PRICING-001` for the named,
dated DeepSeek Flash pilot rate. It does not reopen PRD6 or accept PRD7 scope.

## Authorization and safety boundary

The User selected option A: align only DeepSeek Flash to the supplier's current
public rate, then send exactly one additional non-thinking request with a
one-character synthetic prompt, `max_tokens=1`, no client retry and an approved
actual-settlement spend ceiling of CNY 0.01.

The CNY 0.01 value is an operation authorization boundary, not a technical API
key budget. The current ordinary access-key contract has no per-key spend-cap
field enforced by the gateway. UniRouter's minimum-freeze rule temporarily
reserved 100,000 micro-CNY (CNY 0.10), then released all but the actual 7
micro-CNY charge. The final frozen and unresolved counts are zero.

The batch did not change or add Kimi, DeepSeek Pro, code, image, schema,
organization multiplier, wallet credit, supplier credential, License,
Production or X-Insight state.

## Official price authority and internal mapping

DeepSeek's official price page was read on 2026-07-16. It publishes V4 Flash
prices per million tokens as CNY 1 for cache-miss input, CNY 0.02 for cache-hit
input and CNY 2 for output. It also warns that prices may change:

- https://api-docs.deepseek.com/zh-cn/quick_start/pricing/
- https://api-docs.deepseek.com/

The target has no `org_model_prices` row for this model, so the effective
organization multiplier is exactly `1`.

| Price class | Before target value | After target value | Public-rate meaning |
|---|---:|---:|---:|
| Ordinary / uncached input | `100` cents / 1K | `0.1` cents / 1K | CNY `1` / 1M |
| Cache read ratio | `1` | `0.02` | CNY `0.02` / 1M |
| Generic cache-creation ratio | `1` | `1` | CNY `1` / 1M |
| Output | `200` cents / 1K | `0.2` cents / 1K | CNY `2` / 1M |

DeepSeek `prompt_cache_miss_tokens` normalizes to ordinary uncached input, not
to UniRouter's independent generic cache-creation class. DeepSeek V4 usage has
no separate cache-creation token class; keeping `cache_creation_ratio=1` does
not invent a DeepSeek SKU.

This is dated target-configuration parity, not supplier-invoice reconciliation,
discount/tax validation or a permanent zero-markup commercial policy.

## Preflight and exact Admin API update

Read-only preflight invocation `t-wl06r0fjpeovugw` proved:

- stable and canary ready on the formal ae9 release;
- price row #9 was still the preserved legacy `100/200` cents per 1K with
  cache ratios `1/1`;
- no organization multiplier existed;
- channel #5/key #4 remained active and healthy with the exact
  `DeepSeek-V4-Flash` to `deepseek-v4-flash` mapping;
- wallet balance/granted/spent/frozen were
  `7,980,000 / 0 / 2,640,000 / 0` micro-CNY; and
- unresolved attempt/outbox/reservation counts were `0/0/0`.

Invocation `t-wl06r0m05h889vk` then stopped on a shell-quoting error in the
read-only route precondition before creating a temporary operator, writing a
price or creating/sending a key/request. Its exact-name fallback cleanup ran;
the subsequent successful invocation reasserted the unchanged old price as a
hard precondition.

Successful invocation `t-wl06r0m80nccidc` used:

```text
PUT /api/v1/model-prices/9
input_price_cny_per_million_tokens=1
output_price_cny_per_million_tokens=2
cache_read_ratio=0.02
cache_creation_ratio=1
```

The API returned HTTP 200. Price audit #196 is
`model_price.update / model_price/9`, source `admin_console`, server operation
`op_c4080691d371ed08e61a70dd33df5988` and client correlation
`v1-price-update-20260715T173820Z-6a0fcd`. Its exact public before/after state is
`1000/2000/1/1` to `1/2/0.02/1` in CNY-per-million/ratio terms.

## One bounded real request and exact ledger

The same single remote process created temporary shared access key #42, held
its raw value only in shell memory, sent one request and disabled it through the
API. The request used public model `DeepSeek-V4-Flash`, mapped supplier model
`deepseek-v4-flash`, `thinking.type=disabled`, `stream=false` and
`max_tokens=1`.

| Evidence | Exact result |
|---|---|
| Request | `op_240a63dd8e3de67036d894cd5b7d0d8f` |
| Gateway / upstream | HTTP 200 / HTTP 200; DeepSeek channel #5, key #4, route 1 of 1 |
| Response model | `deepseek-v4-flash` |
| Usage classes | uncached input `5`; cache read `0`; cache creation `0`; output `1` |
| Usage authority | `provider_reported / customer_charge / final` |
| Provider attempt | `att_f427b66ed0a10fd1484eaeb07f674da9`; one attempt, no retry, `settled / known_usage` |
| Effective prices | input `1,000,000`; cache read `20,000`; generic cache creation `1,000,000`; output `2,000,000` micro-CNY / 1M |
| Charge | `7` micro-CNY = CNY `0.000007`, below the approved CNY `0.01` actual-settlement ceiling |
| Wallet | balance `7,980,000 -> 7,979,993`; granted `0 -> 0`; spent `2,640,000 -> 2,640,007`; frozen `0 -> 0` |
| Transactions | freeze `100,000`, then settle `7`; refund/release `99,993`; uncovered `0` |

The pricing snapshot is
`pv3_768e03556863deda6332a3b9633821375f1702ab789112fbd1a9d413ab5ad512`.
The before and after wallet authority both total 10,620,000 micro-CNY
(`balance + granted + spent`), so exact conservation holds.
`routing.route_selected`, `provider.response_accepted` and
`billing.settle.succeeded` share the same request and attempt identities.

## Cleanup and independent readback

Temporary key #42
`v1-deepseek-flash-price-20260715T173820Z-6a0fcd` is disabled. Audit #198 records
creation and #199 records disablement with their separate client correlations.
Temporary platform administrator #15 is disabled. Its generated password, JWT
and raw API key were never printed or persisted and were cleared from the
process.

Admin JWT authentication currently does not perform status revocation on every
request; disabling user #15 therefore does not cryptographically revoke the
already issued 24-hour token. The token existed only in the completed remote
process and is no longer available to the operator. This evidence claims
account disablement and non-persistence, not server-side token revocation.

Independent readback invocation `t-wl06r0meexcuu4g` passed all assertions:

- stable/canary are healthy with zero restarts on exact image
  `sha256:fca7fe8fdabfcbfd32198c8bb4ba054e1b465f8342f671af39dba76b54b822d0`;
- release state remains sequence `3`, version `1.0.0`, code
  `ae9ce2c927460e993dd00c6d81c8736cc5a015a2` and schema `000037`;
- the active enforceable development/test License and release trust are
  unchanged;
- price, audit, route, response, one provider attempt, pricing snapshot,
  call log, freeze, transactions and wallet match the values above;
- temporary key/user active counts are zero; active capacity returned to
  `26` keys, `4` users, `1` project and `3` channels; and
- unresolved provider attempts, internal outbox events and reservations are
  `0/0/0`.

## Issue matrix

| Issue | Classification | Impact | Cause | Priority / owner | Fix or next step | Acceptance evidence |
|---|---|---|---|---|---|---|
| `V1-TARGET-PRICING-001` | target environment configuration | Ordinary/cache-miss input and output were 1000x the selected rates; cache-hit input was 50,000x | Preserved test data had no approved source/effective date | Critical before customer traffic / commercial authority + target operator | **Target verified / closed for the dated DeepSeek Flash pass-through pilot rate.** Recheck the source before later rollout; decide any durable markup policy separately | Official source/date, no org multiplier, supported API, audit #196, exact readback, one 7-micro-CNY settlement and wallet conservation |
| `V1-TARGET-ROUTING-001` | target environment configuration | Kimi, DeepSeek Pro and other catalog identities remain unverified | Provider identifiers require one-by-one authority and bounded target evidence | High / provider + target owner | Keep Mimo/Flash unchanged; select the next exact provider/model separately | This call reconfirms Flash only; broader row remains open |
| `V1-TARGET-PILOT-001` | target evidence | Five bounded calls do not prove load, HA or customer operations | Capacity, IdP, detector and customer scenarios require separate environments/authorization | High / target owners | Preserve bounded facts; approve the next scenario separately | Cumulative charge is now `810,007` micro-CNY; temporary keys/accounts are disabled and pending state is zero. The unpersisted admin JWT is not claimed revoked |
| `V1-CHANNEL-MAPPING-001` | UI/UX improvement | Mapping still requires a controlled operator procedure | Current channel API/UI does not expose validated `model_mapping` mutation | Medium / UniRouter product | Keep the V1 procedure; reconsider only after pilot feedback | Existing audited mapping and the second Flash call remain healthy |
| `V1-XI-LEDGER-001` | X-Insight coordination | Downstream v2 acceptance remains unknown | UniRouter-only evidence cannot prove consumer behavior | High / X-Insight owner | Obtain explicit v2 consumer acknowledgement later | No X-Insight state changed |

## Preserved non-claims

1. No DeepSeek Pro, Kimi or other model/provider is verified by this price.
2. One additional request is not supplier-invoice, concurrency, capacity,
   retry/failover, HA or stability evidence.
3. Public-price parity can expire when DeepSeek changes its page and is not a
   permanent pricing/markup policy.
4. The CNY 0.01 authorization boundary is not an API-key hard limit; the
   temporary minimum freeze was CNY 0.10 and fully reconciled.
5. No enterprise SSO, external detector, customer backup/restore, Production,
   TLS/HA, migration-down or X-Insight consumer test was run.
6. The development/test License, signing roots and images are not a production
   key ceremony or customer artifact.
