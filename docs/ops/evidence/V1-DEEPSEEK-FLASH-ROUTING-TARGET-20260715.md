# V1 DeepSeek Flash Routing — Target Evidence

**Date:** 2026-07-15 (Asia/Shanghai)

**Target:** existing Alibaba Cloud development/test ECS in `cn-wulanchabu`;
this is not a Production claim

**Result:** DeepSeek `DeepSeek-V4-Flash` is Target verified for one bounded
non-stream request. The public UniRouter model was mapped to the exact live
supplier identifier `deepseek-v4-flash`, stable/canary route previews each
selected the one healthy DeepSeek channel/key, the real request returned HTTP
200, provider-reported usage settled exactly, and temporary access was
disabled. No application code, image, schema, supplier credential, Kimi,
DeepSeek Pro, Production or X-Insight state changed.

PRD6 remains `Completed (Accepted Scope)`. This separately authorized
Post-PRD6 evidence partially advances only the real-upstream part of P6-E03 and
does not reopen an accepted row or accept PRD7 scope.

## Authorization and safety boundary

The User approved provider-by-provider target normalization and, for this
batch, exactly one minimal paid DeepSeek Flash request. The request reused a
conservative UniRouter-ledger cap of CNY 0.35, used a one-character synthetic
prompt, `max_tokens=1` and non-thinking mode, and required immediate temporary
key cleanup.

The batch did not:

1. add DeepSeek Pro or Kimi;
2. rotate, reveal or persist the supplier credential outside its existing
   encrypted target record;
3. change a model price, wallet, organization multiplier or License;
4. deploy code or images; or
5. store a raw access key, JWT, prompt response or supplier secret in evidence.

## Supplier model authority

The current official DeepSeek documentation and change log identify
`deepseek-v4-flash` and `deepseek-v4-pro` as the V4 API model names. They also
state that `deepseek-chat` and `deepseek-reasoner` are compatibility names for
Flash and will be deprecated on 2026-07-24:

- https://api-docs.deepseek.com/
- https://api-docs.deepseek.com/updates/

The target credential was then used only in target process memory to call the
supplier `GET /models` endpoint. Invocation `t-wl06r09s1hzgy68` returned HTTP
200 and exactly `deepseek-v4-flash` plus `deepseek-v4-pro`, both owned by
DeepSeek. The credential value was never printed. An earlier syntax-error
invocation, `t-wl06r09qjlqud4w`, executed no HTTP request.

This resolves the model-identity fact independently of UI display labels or
historical successful calls through the retiring `deepseek-chat` alias.

## Pre-change target facts

The exact read-only preflight found:

- channel #5 `deepseek` active and healthy at
  `https://api.deepseek.com`, priority/weight `100/100` and zero failures;
- channel models `deepseek-chat,deepseek-reasoner` and no model mapping;
- encrypted channel key #4 active, zero failures/cooldown, but restricted to
  public model `DeepSeek-V4-Flash`;
- public model price row `DeepSeek-V4-Flash` present;
- no organization multiplier, so the effective multiplier was `1`;
- wallet balance `7,987,000`, frozen `0` and spent `2,633,000` micro-CNY; and
- zero unresolved provider attempts, outbox work or reservations.

Before the change, stable route preview returned HTTP 200 and correctly showed
no candidate for `DeepSeek-V4-Flash`. This proved the preview endpoint itself
was live, but the target configuration could not route the public identifier.

## Exact target configuration

The routing core already consumes a channel `model_mapping`, but the current
channel create/update API and admin UI do not expose that field. A narrowly
scoped database transaction was therefore used with an exact before-state
precondition. It changed only channel #5:

| Field | Before | After |
|---|---|---|
| `models` | `deepseek-chat,deepseek-reasoner` | `DeepSeek-V4-Flash` |
| `model_mapping` | `{}` | `{"DeepSeek-V4-Flash":"deepseek-v4-flash"}` |
| `routing_epoch` | `1` | `2` |

Channel/key active, health, priority, weight, failure and cooldown fields were
unchanged. Key #4 remained restricted to `DeepSeek-V4-Flash`. Audit event #193
records `channel.target.configure` for `channel/5`, source `system`, request
`deepseek-config-20260715` and the exact before/after values. Target invocation:
`t-wl06r09x6j57bpc`.

After the transaction, stable and canary previews both returned HTTP 200 with
one candidate: channel #5, one eligible key, no exclusion, live and not
degraded. Invocation: `t-wl06r0a18dbsc8w`. This is real route-preview
evidence, and the paid call below proves the previewed route could dispatch.

## Bounded real request and ledger evidence

Two conservative preparation attempts stopped before creating a key or sending
a provider request: `t-wl06r0a68ukodts` failed local SQL-input quoting, and
`t-wl06r0a9vpx5wcg` rejected an incorrectly scaled cap calculation. Neither
mutated target state or incurred a call.

The one successful invocation, `t-wl06r0adb26o35s`, used public model
`DeepSeek-V4-Flash`, mapped supplier model `deepseek-v4-flash`,
`thinking.type=disabled` and `max_tokens=1`:

| Evidence | Exact result |
|---|---|
| Request | `op_773ffcc46d00b98e1bb373ef161b90d1` |
| Gateway / provider | HTTP 200 / DeepSeek channel #5, key #4, route 1 of 1 |
| Response model | `deepseek-v4-flash` |
| Usage | 5 input / 1 output / 6 total; `provider_reported`, `final` |
| Provider attempt | `att_084c92d8a84aa85f8c2ca3ac858c1633`; `settled`, `known_usage`, HTTP 200 |
| Call log | gateway 200, upstream 200, non-stream, customer charge |
| Charge | 7,000 micro-CNY = CNY 0.007 |
| Wallet | balance 7,987,000 -> 7,980,000; spent 2,633,000 -> 2,640,000; frozen 0 |
| Transactions | freeze 100,000, then settle 7,000; no residue |

The charged amount is below the approved CNY 0.35 cap and exact wallet
conservation holds. Temporary access key #41
`v1-deepseek-flash-20260715T152527Z` was disabled through the API with HTTP
200. Audit #194 records its creation and audit #195 its disablement. No
temporary user was created.

## Independent readback

Readback invocations `t-wl06r0ajwq0dngg` and corrected
`t-wl06r0am03jklc0` proved:

- stable/canary still run exact formal backend image
  `sha256:fca7fe8fdabfcbfd32198c8bb4ba054e1b465f8342f671af39dba76b54b822d0`,
  healthy with zero restarts;
- release trust remains `license_keys=1 upgrade_keys=1` with an active,
  enforceable test License;
- release state remains sequence `3` at code
  `ae9ce2c927460e993dd00c6d81c8736cc5a015a2` and schema `000037`;
- route configuration and the request/attempt/call-log/transactions/wallet
  match the exact values above;
- `routing.route_selected`, `provider.response_accepted` and
  `billing.settle.succeeded` traces all record mapped model
  `deepseek-v4-flash`;
- temporary key #41 is disabled, and active temporary-key, unresolved-attempt,
  outbox and reservation counts are zero; and
- stable/canary previews still return the one healthy candidate with zero
  exclusions.

The first readback's final release-history query used a wrong column name and
failed only after all preceding checks had passed. The corrected invocation
verified sequences `1`, `2` and `3` and the exact current release state. No
mutation occurred in either readback.

## Issue matrix

| Issue | Classification | Impact | Cause | Priority / owner | Fix or next step | Acceptance evidence |
|---|---|---|---|---|---|---|
| `V1-TARGET-ROUTING-001`, DeepSeek Flash portion | target environment configuration | Public Flash previously returned no route despite a healthy credential | Channel aliases and key allowlist used different identity domains and no public-to-supplier mapping existed | High / target operator | **Target verified for Flash:** exact one-model mapping and allowlist alignment; leave Pro/Kimi untouched | Both previews select one candidate; one real HTTP 200 call settled; audit/readback/cleanup above |
| `V1-CHANNEL-MAPPING-001` | UI/UX improvement | Routine provider-model alignment currently requires a controlled DB operation, increasing operator error risk | Routing core supports `model_mapping` but channel API/UI does not expose safe mutation | Medium / UniRouter product + operator tooling | Do not add a new release feature now. Preserve the audited/preconditioned operator procedure for V1; decide after pilot whether a validated API/UI editor is warranted | Current transaction had exact precondition, audit, preview and real-call proof; future UI would require API validation, RBAC/audit and routing regression evidence |
| `V1-TARGET-PRICING-001` | target environment configuration / commercial pricing decision | If the row is not an intentional resale price, first customers could be charged 1000x the current supplier list rate | Existing target rows were preserved through the unit/micro-CNY migrations and have no approved source, markup or effective-date record | **Critical / P0 before customer traffic** / User plus commercial pricing authority | **Decision required; no automatic repricing.** Approve the target row as an intentional sell price, or approve a supplier-price-plus-margin policy and one exact audited update | Approved source/currency/effective date/markup, exact row readback, bounded quote/call and wallet conservation |
| `V1-TARGET-PILOT-001` | target evidence | Mimo and Flash single calls are proven, but broad release reliability is not | Provider, capacity, IdP, detector and customer-operation evidence require separate environments/authorization | High / selected provider and target owners | Keep bounded facts; authorize remaining provider or capacity/IdP/customer scenarios separately | This file plus Mimo target evidence; no broad/Production claim |
| `V1-XI-LEDGER-001` | X-Insight coordination | Consumer acceptance remains unknown | UniRouter-only evidence cannot prove downstream v2 parsing | High / X-Insight consumer owner | Unchanged; obtain explicit v2 consumer acknowledgement later | No X-Insight state changed in this batch |

## Target price decision gate

The target `DeepSeek-V4-Flash` row is internally stored as CNY cents per 1,000
tokens at `100` input and `200` output. Under the current public contract this
is CNY `1000` input and `2000` output per million tokens.

DeepSeek's current official CNY page publishes Flash at CNY `0.02` per million
cache-hit input tokens, CNY `1` per million cache-miss input tokens and CNY `2`
per million output tokens:

- https://api-docs.deepseek.com/zh-cn/quick_start/pricing/

The target customer input rate is therefore 1000x the official cache-miss
supplier rate, and the output rate is 1000x the official supplier rate. A
customer sell price may intentionally differ from supplier COGS, so this fact
does not prove a billing-code defect. It does prove that pricing provenance and
commercial intent must be decided before customer traffic. No price was changed
in this batch.

## Preserved non-claims

1. No DeepSeek Pro, Kimi or other provider/model is verified by this result.
2. One minimal request is not capacity, concurrency, HA, retry/failover,
   supplier-invoice or long-running stability evidence.
3. The 7,000-micro-CNY value is UniRouter's configured customer ledger, not
   confirmation of the supplier's final debit.
4. No enterprise SSO, external content detector, customer backup/restore,
   Production/TLS/HA or migration-down test was run.
5. The development/test License, signing roots and images are not a production
   key ceremony or customer artifact.
6. X-Insight v2 consumer acceptance remains separate.
7. Earlier no-DeepSeek evidence remains correct at its checkpoint; this file
   supersedes only the explicitly authorized DeepSeek Flash scope.
