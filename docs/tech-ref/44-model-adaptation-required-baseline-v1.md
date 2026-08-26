# Model Adaptation Required Baseline V1 — Draft Candidate

> Baseline ID candidate: `unirouter.model-adaptation.required.v1`
>
> Baseline version candidate: `1`
>
> Drafted at: `2026-08-25` (Asia/Shanghai)
>
> Approval state: **draft; no immutable approved Baseline has been issued**
>
> Requirement source: the product-owner-confirmed clean-baseline model set and
> capability checks, plus the product and architecture decisions confirmed on
> 2026-08-25. Provider documentation is evidence, not a requirement authority.

This draft says what the proposed product scope requires. It is not read by the
gateway and is not a runtime, evidence, release, pricing or progress authority.
The exact approved payload, approval time and approver must be recorded only
when this candidate matrix is explicitly frozen. Until then, every row remains
a candidate and must not promote a Profile, price, Release or route.

Mutable status is projected from the exact Execution Profile, validation
evidence, Release and Pricing Profile authorities and is summarized in
[42-model-execution-profile-capability-status.md](42-model-execution-profile-capability-status.md).

## 1. Baseline semantics

- The only Baseline state is `requirement_disposition`:
  `execution_required`, `documentation_required`, `deferred` or
  `not_applicable`.
- `execution_required` is a product commitment, not a synonym for provider
  documentation. If an exact provider/surface cannot satisfy it, the row needs
  a product decision; it is never silently changed from provider documentation.
- `documentation_required` requires a sourced, exact-surface conclusion but
  does not authorize executable support.
- Provider support, platform support, evidence, release and readiness are
  projections from their existing authorities; none is stored in this file.
  `released` is never an evidence level.
- Product identity is `provider + region + API surface + upstream model/version
  policy`. A floating alias stores family, requested alias and version policy;
  observed identity belongs to probe or immutable attempt evidence.
- Multiple API surfaces require separate Profile identities. Historical exact
  `ID@version` addresses and sealed Releases remain immutable; current-for-
  surface and new-attachment eligibility are separate derived authorities.
- A Baseline row cannot activate a price, authorize paid traffic, change
  canonical routing, promote evidence or rewrite a sealed Release.

## 2. Exact product-identity candidate

The candidate model count is **30**. Because each GPT model requires both
Responses and Chat Completions, this expands to **33 surface-specific product
identities**. `E`, `D`, `F` and `N` mean `execution_required`,
`documentation_required`, `deferred` and `not_applicable` respectively.

Core columns are exact capability milestones:

- `op`: non-stream Chat, Messages or Responses request for the named surface;
- `str`: streaming;
- `out`: explicit output limit and documented maximum/default distinction;
- `rsn`: reasoning request/default semantics;
- `tool`: function tools and exact `tool_choice` behavior;
- `json`: JSON/structured output;
- `use`: provider usage mapping, including final stream usage;
- `cache`: cache-read usage and fail-closed absence/conflict behavior;
- `lat`: latency evidence;
- `price`: sourced pricing draft and bounded fee evidence, not wallet settlement.

| Profile identity candidate | Provider | Region | API surface | Requested upstream identity / policy | op | str | out | rsn | tool | json | use | cache | lat | price |
|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `deepseek.v4-flash.api` | DeepSeek | CN | OpenAI-compatible Chat | `deepseek-v4-flash` / pinned | E | E | E | E | E | E | E | E | E | E |
| `deepseek.v4-pro.api` | DeepSeek | CN | OpenAI-compatible Chat | `deepseek-v4-pro` / pinned | E | E | E | E | E | E | E | E | E | E |
| `kimi.k2.6.api` | Kimi | CN | Kimi Platform OpenAI-compatible Chat | `kimi-k2.6` / pinned | E | E | E | E | E | E | E | E | E | E |
| `kimi.k2.7-code.api` | Kimi | CN | Kimi Platform OpenAI-compatible Chat | `kimi-k2.7-code` / pinned; Coding subscription is a different commercial surface | E | E | E | E | E | E | E | E | E | E |
| `kimi.k3.api` | Kimi | CN | Kimi Platform OpenAI-compatible Chat | `kimi-k3` / pinned | E | E | E | E | E | E | E | E | E | E |
| `minimax.m3.token-plan` | MiniMax | CN | MiniMax OpenAI-compatible Chat / Token Plan | `MiniMax-M3` / pinned commercial identity | E | E | E | E | E | E | E | E | E | E |
| `minimax.m2.5-highspeed.token-plan` | MiniMax | CN | MiniMax OpenAI-compatible Chat / Token Plan | `MiniMax-M2.5-highspeed` / pinned commercial identity | E | E | E | E | E | E | E | E | E | E |
| `minimax.m2.7.token-plan` | MiniMax | CN | MiniMax OpenAI-compatible Chat / Token Plan | `MiniMax-M2.7` / pinned commercial identity | E | E | E | E | E | E | E | E | E | E |
| `glm.5.3.api` | GLM | CN | OpenAI-compatible Chat | `glm-5.3` / pinned | E | E | E | E | E | E | E | E | E | E |
| `glm.5.2.api` | GLM | CN | OpenAI-compatible Chat | `glm-5.2` / pinned | E | E | E | E | E | E | E | E | E | E |
| `glm.5v-turbo.api` | GLM | CN | OpenAI-compatible Chat | `glm-5v-turbo` / pinned | E | E | E | E | E | E | E | E | E | E |
| `qwen.3.8-max.api` | DashScope | CN Beijing | workspace OpenAI-compatible Chat | `qwen3.8-max` / pinned, region/workspace explicit | E | E | E | E | E | E | E | E | E | E |
| `qwen.3.7-plus.api` | DashScope | CN Beijing | workspace OpenAI-compatible Chat | `qwen3.7-plus` / pinned, region/workspace explicit | E | E | E | E | E | E | E | E | E | E |
| `qwen.3.7-flash.api` | DashScope | CN Beijing | workspace OpenAI-compatible Chat | `qwen3.7-flash` / pinned, region/workspace explicit | E | E | E | E | E | E | E | E | E | E |
| `gemini.3.7-flash.api` | Gemini | global | Google OpenAI-compatible Chat | `gemini-3.7-flash` / pinned | E | E | E | E | E | E | E | E | E | E |
| `gemini.3.5-flash.api` | Gemini | global | Google OpenAI-compatible Chat | `gemini-3.5-flash` / pinned | E | E | E | E | E | E | E | E | E | E |
| `gemini.3.5-flash-lite.api` | Gemini | global | Google OpenAI-compatible Chat | `gemini-3.5-flash-lite` / pinned | E | E | E | E | E | E | E | E | E | E |
| `mimo.v2.5-pro.api` | MiMo | CN | OpenAI-compatible Chat | `mimo-v2.5-pro` / pinned | E | E | E | E | E | E | E | E | E | E |
| `mimo.v2.5.api` | MiMo | CN | OpenAI-compatible Chat | `mimo-v2.5` / pinned | E | E | E | E | E | E | E | E | E | E |
| `hy3.api` | Hunyuan | CN | OpenAI-compatible Chat | `hy3` / pinned | E | E | E | E | E | E | E | E | E | E |
| `openai.gpt-5.6-sol.responses` | OpenAI | global | Responses | `gpt-5.6-sol` / pinned | E | E | E | E | E | E | E | E | E | E |
| `openai.gpt-5.6-sol.chat` | OpenAI | global | Chat Completions | `gpt-5.6-sol` / pinned | E | E | E | E | E | E | E | E | E | E |
| `openai.gpt-5.6-terra.responses` | OpenAI | global | Responses | `gpt-5.6-terra` / pinned | E | E | E | E | E | E | E | E | E | E |
| `openai.gpt-5.6-terra.chat` | OpenAI | global | Chat Completions | `gpt-5.6-terra` / pinned | E | E | E | E | E | E | E | E | E | E |
| `openai.gpt-5.6-luna.responses` | OpenAI | global | Responses | `gpt-5.6-luna` / pinned | E | E | E | E | E | E | E | E | E | E |
| `openai.gpt-5.6-luna.chat` | OpenAI | global | Chat Completions | `gpt-5.6-luna` / pinned | E | E | E | E | E | E | E | E | E | E |
| `xai.grok-4.6.api` | xAI | global | OpenAI-compatible Chat | `grok-4.6` / pinned | E | E | E | E | E | E | E | E | E | E |
| `anthropic.claude-opus-5.api` | Anthropic | global | Messages | `claude-opus-5` / pinned | E | E | E | E | E | E | E | E | E | E |
| `anthropic.claude-sonnet-5.api` | Anthropic | global | Messages | `claude-sonnet-5` / pinned | E | E | E | E | E | E | E | E | E | E |
| `anthropic.claude-haiku-4.5.api` | Anthropic | global | Messages | `claude-haiku-4-5-20251001` / pinned | E | E | E | E | E | E | E | E | E | E |
| `doubao.seed-evolving.api` | Doubao Ark | CN Beijing | Ark OpenAI-compatible Chat | `doubao-seed-evolving` / floating; observed version is evidence | E | E | E | E | E | E | E | E | E | E |
| `doubao.seed-2.1-pro.api` | Doubao Ark | CN Beijing | Ark OpenAI-compatible Chat | `doubao-seed-2-1-pro-260628` / pinned | E | E | E | E | E | E | E | E | E | E |
| `doubao.seed-2.1-turbo.api` | Doubao Ark | CN Beijing | Ark OpenAI-compatible Chat | `doubao-seed-2-1-turbo-260628` / pinned | E | E | E | E | E | E | E | E | E | E |

This is a candidate matrix, not a claim that every `E` is currently supported.
An `E` row is closed only by the exact typed implementation and evidence chain;
documented provider support alone cannot close or create it.

## 3. Exact additional-capability dispositions

For all 33 identities, multimodal media, Partial Mode, Dynamic Tools, built-in
search and search charging are `documentation_required` unless an explicit
product decision promotes an exact row. This requires truthful implementation-
state verification and forbids inference from generic compatibility or provider
marketing. Batch and Realtime are also `documentation_required` and are separate
operations.

GPT Responses built-ins are independently scoped; none is an aggregate
`builtin_tools` capability:

| Responses identity | `responses.web_search` | `responses.file_search` | `responses.code_interpreter` | `responses.image_generation` | `responses.computer_use` | `responses.remote_mcp` |
|---|---:|---:|---:|---:|---:|---:|
| `openai.gpt-5.6-sol.responses` | D | D | D | D | D | D |
| `openai.gpt-5.6-terra.responses` | D | D | D | D | D | D |
| `openai.gpt-5.6-luna.responses` | D | D | D | D | D | D |

Promotion of one built-in to `execution_required` requires its own request,
response/state, usage/non-token meter, customer price, supplier estimate,
provider evidence, timeout/retry/failure and data/security/audit contract.
Ordinary Chat function calling remains independently `execution_required`.

Native-USD supplier cost, FX and multi-currency wallet settlement are
`deferred`. Official USD prices remain review-only drafts. V1 customer pricing
is an approved target: route-neutral, normalized-tier-aware public CNY pricing;
supplier-cost pass-through and cost-plus are `not_applicable`.

Non-standard tier settlement and SLA credit are `deferred`. Until an explicit
settlement rule exists, flex/priority may reach protocol-level local conformance
and separately authorized bounded supplier-cost evidence, but cannot enter the
prepaid customer-wallet path.

## 4. Acceptance classes

- Executable request: typed positive/negative cases, full prepare/admission path
  and exact provider payload preservation.
- Response: non-stream/stream mapping, exceptional fields and missing/conflicting
  evidence behavior.
- Usage: non-stream, final stream usage and missing/conflicting counters.
- Pricing: selector, Freeze, settlement and Receipt only when an approved
  CustomerChargeQuote exists; supplier source drafts are never customer prices.
- Identity/routing: provider, region, surface, endpoint, operation and exact
  Profile negative tests.
- Lifecycle: no new attachment or current-for-surface promotion before its
  milestone closes; no evidence or Release state may skip its existing gates.
- UI onboarding: exact mapping and truthful copy, without fabricated provider,
  financial, live or release evidence.
- Documentation-only: exact source and matrix/schema completeness; no invented
  runtime or financial test.

Active Milestone, current Profile versions and progress remain mutable
projections outside this draft Baseline.

## 5. B1 finite integration batch — 2026-08-26

The product owner approved stopping capability expansion and converging the
existing worktree. B1 is **integration and truthfulness closeout**, not completion
of every `E` in the Mission candidate above. There is no automatic next-model
milestone inside B1.

Initial candidate snapshot:

- branch `codex/v122-model-catalog-22`;
- base commit `4fd2405ef81340b5f1b19f12adce6ee7fdf892bb`;
- 204 changed paths: 120 tracked and 84 untracked; tracked diff +25,678/-1,623;
- sorted `{path,sha256(file bytes)}` JSON manifest SHA-256:
  `3e48093a651bbe4d7a5cf316a62915a54e26a041727d45a117ef1eef7f8d2b23`.

This fingerprint identifies the starting candidate, not an approval, final
artifact, test result or deployment. The exact current Profile versions are
listed in the status document; B1 must not advance those version pointers.
Only confirmed blockers, affected tests/CI selectors, documentation and truthful
onboarding copy may change after this boundary. A necessary runtime correction
must identify its failing path and affected invariant before implementation.

### Inclusion decisions

The product owner approved every row below for **B1 batch inclusion** on
2026-08-26. Approval retains and verifies only the exact bounded candidate
described in each row; it does not promote the capability to a broader
requirement, authorize provider traffic/deployment/activation, or imply full
provider parity. No historical approval is inferred from a local milestone log,
provider document or reviewer finding.

| Candidate | Why it exists / exact boundary | Inclusion state |
|---|---|---|
| M85 | Two nullable immutable Plan snapshot hash columns on Freeze/attempt; no historical backfill. Existing route hash does not bind the request-level Plan at Freeze. Runtime consumers and migration must travel together. | approved for B1 inclusion: exception to the earlier no-schema boundary |
| M86 | Exact Responses/Messages v1 and v2 support tuples coexist; removes latest-per-operation selection as historical execution authority. Existing sealed identities remain unchanged. | approved for B1 inclusion: same exception |
| M87 | Exact Messages text-tools v3 tuple for the already implemented Sonnet function-tool subset; depends on M86. No new server/built-in tools. | approved for B1 inclusion: same exception |
| GLM 5V Turbo | Existing typed image/video/file-reference subset only, not new Files operations or full media parity. | approved bounded B1 subset |
| Qwen 3.8 Max / 3.7 Plus / 3.7 Flash | Existing Beijing media, explicit-cache request/usage and search-request contracts only; no search metering or storage billing completion. | approved bounded B1 subset |
| Gemini 3.7 / 3.5 Flash / 3.5 Flash-Lite | Existing inline JPEG/WAV subset. The separately approved service-tier protocol/runtime-policy slice remains zero-charge observation-only for nonstandard tiers. | approved bounded B1 media subset |
| MiMo V2.5 | Existing image/audio/video shapes only; V2.5 Pro does not inherit media. | approved bounded B1 subset |
| Hy3 | Existing search-request contract only, not search execution/usage/billing completeness. | approved bounded B1 subset |
| GPT Sol / Terra / Luna Chat | Existing image and explicit-cache request/usage subset only; no transfer of those capabilities to Responses. | approved bounded B1 subset |
| Doubao Evolving / Seed 2.1 Pro / Turbo | Existing bounded non-stream inline JPEG subset only; no remote images, other media or provider-max parity. | approved bounded B1 subset |
| MiniMax M3 | Existing bounded inline JPEG and standard/priority policy only; no video or tier-aware customer settlement. | approved bounded B1 subset |
| Kimi Partial / media / K3 Dynamic Tools / bounded search | These capabilities already existed at the base commit. Exact invocation/history/response repairs are existing-capability corrections, not authorization derived from documentation. | existing-scope correction; no expansion |

### Acceptance boundary and stop condition

1. Resolve the inclusion rows without deleting or rewriting historical evidence.
2. Correct contradictory current-state and UI claims; keep target architecture
   separate from implemented facts and the full Mission separate from B1.
3. Clear known deterministic regressions. Verify retained migrations on an
   isolated PostgreSQL database, including populated predecessor, ordered
   rollback/reapply, negative rollback and immutable-history cases.
   Lossless rollback requires no references to the removed tuple, including
   probe/approval evidence, not merely no new Release binding. Existing foreign
   keys must continue to reject evidence-destructive rollback.
4. One bounded independent closing review of the exact retained diff. Findings
   are recommendations; only validated B1 blockers enter implementation.
5. Once that diff is stable, one necessary integration gate, one authorized
   push and one CI run. Repairs after a failure require only affected regression
   evidence, not automatic replay of every prior gate.
6. Report Local/CI separately and stop. No automatic next model or capability.

Not included: new provider/model/surface/capability, financial-architecture
rework, native-USD observation, FX, nonstandard-tier wallet settlement, paid
supplier traffic, Target migration/deployment, reset, identity changes,
canonical switch or Release promotion. Those require their own later boundary.
