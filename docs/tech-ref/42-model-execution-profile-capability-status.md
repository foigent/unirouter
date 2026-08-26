# Model Execution Profile And Capability Status

> Status: accepted architecture direction and current implementation gap inventory.
> Snapshot date: 2026-08-26.
> This document is not a claim that every provider-documented feature is production supported.

The current product-scope candidate is
[Model Adaptation Required Baseline V1 — Draft Candidate](44-model-adaptation-required-baseline-v1.md).
It is not yet an immutable approved Baseline. This document and the runtime
authorities project what the repository can currently do.

## B1 integration snapshot — not a completed adaptation claim

Active work is the finite B1 integration closeout in
[Baseline §5](44-model-adaptation-required-baseline-v1.md#5-b1-finite-integration-batch--2026-08-26).
No further model/capability promotion is active. The initial candidate is based
on `4fd2405ef81340b5f1b19f12adce6ee7fdf892bb`; its 204 changed paths are cumulative
WIP, not 204 completed deliverables. Inclusion approval, local integration,
CI, Target and provider-live evidence are distinct gates.

There are **30 upstream model entries and 33 current surface identities**.
The table is a code projection of exact new-attachment pointers, not a Baseline
approval or proof that each model satisfies the full capability matrix.
Recorded local milestones cover bounded subsets. The final local gate checkpoint
is recorded below; CI and all new Target/live/release claims require their own
exact-commit/environment evidence.

<!-- current-profile-inventory:start -->
| Current exact Profile | Surface |
|---|---|
| `deepseek.v4-flash.api@1.4.0` | Chat |
| `deepseek.v4-pro.api@1.4.0` | Chat |
| `kimi.k2.6.api@1.7.0` | Kimi Platform Chat |
| `kimi.k2.7-code.api@1.3.0` | Kimi Platform Chat |
| `kimi.k3.api@1.8.0` | Kimi Platform Chat |
| `minimax.m3.token-plan@1.7.0` | MiniMax CN Chat / Token Plan |
| `minimax.m2.5-highspeed.token-plan@1.5.0` | MiniMax CN Chat / Token Plan |
| `minimax.m2.7.token-plan@1.5.0` | MiniMax CN Chat / Token Plan |
| `glm.5.3.api@1.7.0` | GLM CN Chat |
| `glm.5.2.api@1.7.0` | GLM CN Chat |
| `glm.5v-turbo.api@1.10.0` | GLM CN Chat |
| `qwen.3.8-max.api@1.8.0` | DashScope Beijing Chat |
| `qwen.3.7-plus.api@1.7.0` | DashScope Beijing Chat |
| `qwen.3.7-flash.api@1.7.0` | DashScope Beijing Chat |
| `gemini.3.7-flash.api@1.7.0` | Google OpenAI-compatible Chat |
| `gemini.3.5-flash.api@1.7.0` | Google OpenAI-compatible Chat |
| `gemini.3.5-flash-lite.api@1.7.0` | Google OpenAI-compatible Chat |
| `mimo.v2.5-pro.api@1.5.0` | MiMo CN Chat |
| `mimo.v2.5.api@1.6.0` | MiMo CN Chat |
| `hy3.api@1.6.0` | Hunyuan CN Chat |
| `openai.gpt-5.6-sol.chat@1.0.0` | Chat Completions |
| `openai.gpt-5.6-sol.responses@1.3.0` | Responses |
| `openai.gpt-5.6-terra.chat@1.0.0` | Chat Completions |
| `openai.gpt-5.6-terra.responses@1.3.0` | Responses |
| `openai.gpt-5.6-luna.chat@1.0.0` | Chat Completions |
| `openai.gpt-5.6-luna.responses@1.3.0` | Responses |
| `xai.grok-4.6.api@1.1.0` | xAI Chat |
| `anthropic.claude-opus-5.api@1.2.0` | Messages |
| `anthropic.claude-sonnet-5.api@1.4.0` | Messages |
| `anthropic.claude-haiku-4.5.api@1.2.0` | Messages |
| `doubao.seed-evolving.api@1.8.0` | Ark Beijing Chat / floating alias |
| `doubao.seed-2.1-pro.api@1.6.0` | Ark Beijing Chat |
| `doubao.seed-2.1-turbo.api@1.6.0` | Ark Beijing Chat |
<!-- current-profile-inventory:end -->

Known scope gaps are not new B1 work: Responses function tools and structured
output are still rejected (including non-stream); Responses built-ins are
documentation-required. Claude Opus/Haiku tools and all Claude media/cache are
not executable here. Media/search/cache subsets do not imply full provider
parity or a complete price contract. Official USD prices remain review drafts;
nonstandard tiers cannot debit customer wallets. No entry is declared fully
adapted, provider-live-verified or released by this table.

### B1 correction checkpoint — local candidate only

The following findings were adjudicated as integration blockers, not new model
requirements:

- M85 down could check for empty evidence before a concurrent writer committed.
  It now locks both evidence tables before the lossless-down check. M85 up/down
  also use the migration runner's lifecycle lock; a regression demonstrated both
  old directions bypassing that lock. M84 was not changed.
- Capability evidence hashing now uses the existing evidence authority's
  UTC/microsecond normalization. A non-UTC PostgreSQL timestamp must not turn
  the same capability evidence into a different hash.
- Historical `unverified.chat@1.0.0` sealed public text-v1 bindings retain their
  strict non-stream parser. They do not inherit a current Profile, v2/v3,
  reasoning, tools or streaming. Exact route/Release evidence remains required.
- An explicit public output bound above the sealed limit is a client error
  before credential decryption, Freeze or dispatch, not a failed upstream.
- Stale integration expectations were repaired without weakening guards:
  typed invocation derivation rather than all supported capabilities; the
  same Kimi product across historical Profile versions; complete reverse-order
  migrations; and separate parameterized statements for the older rollback
  fixture.

Fresh local evidence on 2026-08-26:

- Full affected package tests passed for `internal/modelprofile`,
  `internal/modelrouting`, `internal/endpointprotocol` and `internal/gateway`.
  This includes the former DeepSeek output-limit regression and the Kimi K3
  and deadline tests; they are not outstanding red tests.
- Isolated PostgreSQL integration tests passed for M85 predecessor/reapply,
  writer/lifecycle-lock fencing, Freeze/attempt replay, M86/M87 exact tuples,
  sealed-v1 round trips, v2/v3 rollback refusal, sealed Profile pinning, and
  the exact public Responses/Claude plus historical public-v1 paths.
- The two affected frontend onboarding/endpoint suites passed (27 tests).
  Current-Profile table consistency is also checked against the code catalog.

A bounded independent review of these corrections found no further blocker or
required correction. It reviewed code/test sources and relied on the primary
agent's PostgreSQL execution results; it did not rerun those tests or approve
the cumulative batch. Its receipt changes no product or evidence authority.

These are focused local checks, not the final project gate. The product owner
subsequently approved every bounded inclusion row in Baseline §5 for B1; that
approval does not widen any row or manufacture live/release evidence. No push,
CI, Target mutation, supplier call or release was performed for this checkpoint.
No new model/Profile version pointer was promoted during stabilization.

### B1 cumulative integration review corrections

The subsequent whole-batch review and real PostgreSQL failure tests found
additional B1 blockers; the earlier focused review was not whole-batch approval:

- Public streaming wrote a fixed HTTP 200 for transport/rejected outcomes and
  tried to open delivery before persisting response acceptance. It now records
  the observed status, accepts valid HTTP/SSE headers before disclosure, and
  records provider-terminal outcome/health before interrupted delivery. Header
  acceptance is not terminal success or final usage. Failed terminal persistence
  cannot cancel the reservation, authorize failover or advance provider health.
- M86 registered exact v2 tuples but left M77's old delivery literal in attempt,
  call-log and trace checks. M86 now preserves the original legacy/common
  predicates while delegating exact delivery identity to the existing binding FK
  and attempt-projection triggers. NULL contracts fail closed. Down locks all
  affected tables, refuses newer evidence and restores the original predicates.
- A process-loss regression demonstrated that public streaming recovery could
  charge a checkpoint estimate despite the exact provider-final charge contract.
  The shared existing billing entry points now enforce that contract before
  pricing payload/top-up/settlement. Missing provider-final usage retains the
  pending liability and ends recovery without a debit or fabricated final usage.
  Legacy checkpoint settlement and valid final-usage settlement are unchanged.
- Governance onboarding no longer chooses the first shared GPT alias or copies
  request-default output into the public hard cap. Ambiguous surfaces require
  explicit selection; new limits use the shared executable-limit helpers, and
  existing explicitly lower limits are preserved.

Focused local regressions passed for Responses v2 and Messages v2/v3 streams,
transport/rejection/interruption, terminal-write failure plus expiry recovery,
exact attempt-to-call-log/trace projections and malformed tuple rejection,
M86 constraint round trips, existing P6F40/P6F41 and V1 delivery paths, and the
governance API/21-test UI suite with no warning-ratchet violations. These tests
used isolated PostgreSQL 16 and mock upstreams, not Target or paid providers.
The final batch gate and CI remain separate, pending checkpoints.

### B1 final local gate checkpoint — 2026-08-26

The local gate ran once across the batch; failures were repaired with affected
regressions rather than rerunning every already-passing lane. No known local
gate failure remains. This is not a claim of one initially green invocation:

- Go 1.25.12: `go vet ./...` and the full untagged race suite ran. The two
  failures were stale CLI report counts and a historical Profile used as a new-
  attachment fixture; both affected packages subsequently passed with race.
- PostgreSQL 16: source-evidence, admin and the CI-selected consolidated
  integration lanes ran. Fresh bootstrap incorrectly expected only M75/M77's
  two seed contracts after M86/M87 appended three more; its exact tuple and
  total-count checks now accept the five approved identities, still rejecting
  same-count provenance drift, extra support and a missing extension. Real
  fresh-install creation/replay and existing dirty/schema/role refusals passed.
  A bounded independent review found no blocker in that correction.
- Remaining admin failures were obsolete fixture assumptions: current Kimi
  attachment version and a provider-neutral five-axis projection claiming an
  official-only Profile. The projection now uses the explicit unverified
  contract; a separate real-PG test proves official DeepSeek attachment succeeds
  but cross-provider/operation reuse is rejected without partial rows.
- M75 immutability tests now preserve the entire appended tuple set while
  explicitly retaining its two historical rows. Older lifecycle rollback
  fixtures reverse M87/M86/M85 before M84. Their affected regressions passed;
  no production rollback guard was weakened.
- Redis-backed agent capability, limit policy and workload admission integration
  gates passed sequentially against the task-only Redis service.
- Node 24: type checking, API/React guards and the 622-test frontend suite ran.
  Initially 620 passed and two provider-template interactions failed; both
  passed in isolation. The test helper now waits for committed selection and
  the affected interactions use normal event scheduling. Both affected suites
  then passed (45 tests), including the two failures, with zero unexpected,
  React-act or Semi-Form diagnostics. Production build and bundle budget passed.
- `git diff --check` and version/release metadata checks passed. No current
  Profile pointer changed during this closeout. Protocol documentation now
  distinguishes the seven-entry local registry from exact public execution.

The local database, Redis and upstream mocks were isolated; no Target state,
real supplier or private reset backup was used. Local Redis is version 8.8,
whereas CI uses 7. Exact Neutral N1 release qualification, Linux/container/Nginx
and real-backend browser evidence remain CI responsibilities; the two N1 tests
without that local fixture were skipped, not passed. CI results must be read
from the run bound to the pushed commit/PR, not inferred from this source file.
B1 stops after that exact-commit integration check and the handoff; no next
model/capability, Target migration or release is started automatically.

## Historical local milestone log

The following entries preserve prior local checkpoints and their exact version
claims. They do not authorize batch inclusion or start the next milestone.
Sections 3–5 below describe current code/gaps and B1's stop condition.

Most recently closed local milestone:

`Kimi K3 × Kimi Platform OpenAI-compatible Chat × Dynamic Tools exact invocation/response evidence`.

The current/new-attachment pointer now selects `kimi.k3.api@1.8.0` and the new
immutable `protocol.kimi-openai-chat.k3.v5`; historical `@1.6.0` and the
top-level-only `@1.7.0` candidate remain addressable and cannot receive a new
attachment. The exact non-stream contract accepts top-level function tools,
multiple system-message Dynamic Tools declarations and their documented
coexistence, then freezes one normalized visible-function union in the existing
Plan capability snapshot. Omitted choice is provider-default `auto`; explicit
`none`, `auto` and `required` retain their invocation origins. Duplicate names,
invalid K3 names, content-bearing dynamic declarations, required-without-tools,
named choice and function-tool streaming fail closed. The route-aware live and
cache response guard consumes that same snapshot: `required` needs at least one
declared call, `auto` permits only declared names, and `none` prohibits every
tool call. A bounded pre-code and closure review found no remaining blocker.
This is `locally_conformant` only; paid calls, target live verification,
streaming tool deltas, pricing, billing, Release and deployment remain
unverified.

Earlier closed local milestone:

`Kimi K2.7 Code × OpenAI-compatible Chat × non-stream function-tool differential`.

The current/new-attachment pointer now selects
`kimi.k2.7-code.api@1.3.0`; historical `@1.2.0` remains addressable and cannot
receive a new attachment. K2.7 reuses the reviewed Kimi Chat v5 exact
non-stream function-tool contract while retaining its own required-thinking
identity: thinking cannot be disabled, omission remains provider authority,
and preserved thinking defaults to `keep=all`. Model-specific tests cover
omitted and explicit auto choice, required/named rejection, reasoning replay,
tools+stream fail-closed behavior, one Plan snapshot, provider payload and
declared-function response consistency. This homogeneous differential added no
shared behavior, so it did not repeat the representative K2.6 architecture
review. This is `locally_conformant` only; explicit `tool_choice:null`, tool
streaming, the separate highspeed identity, paid calls, pricing, billing, live
verification, Release and deployment remain unverified.

Earlier closed local milestone:

`Kimi K2.6 × OpenAI-compatible Chat × non-stream function-tool invocation/history/response consistency`.

The current/new-attachment pointer selects `kimi.k2.6.api@1.7.0`; historical
`@1.6.0` remains addressable and cannot receive a new attachment. The reviewed
Chat v5 contract accepts omitted/`none`/`auto` function choice, records omitted
choice as provider-default `auto`, freezes the sorted declared-function set in
the single Plan snapshot, validates complete tool history and preserved
reasoning for active-thinking tool calls, and checks every non-stream returned
function against that route evidence. Required, named, parallel, explicit null
and tools+stream fail closed while ordinary text streaming remains supported.
This is an intentionally bounded none/auto subset and `locally_conformant` only.

Earlier closed local milestone:

`DeepSeek V4 Pro × official standard Chat × tools-request reasoning replay differential`.

The current/new-attachment pointer selects `deepseek.v4-pro.api@1.4.0`;
historical `@1.3.0` remains addressable and cannot receive a new attachment.
Pro reuses the reviewed Chat v4 exact history and runtime replay rule through
its own immutable reasoning v4 identity. Its model-specific tests reject
missing replay under active tools, preserve valid reasoning through admission
and provider payload, retain the one Plan/hash, and leave explicitly disabled
thinking unaffected. This is `locally_conformant` only; live multi-turn, agent
orchestration, stream tools, pricing, billing, Release and deployment remain
unverified.

Earlier closed local milestone:

`DeepSeek V4 Flash × official standard Chat × tools-request multi-turn reasoning replay`.

The current/new-attachment pointer now selects
`deepseek.v4-flash.api@1.4.0`; historical `@1.3.0` remains addressable with its
single-turn/exact-response contract and cannot receive a new attachment. The
new immutable reasoning v4 and Chat v4 contracts require every assistant
history message to preserve non-empty `reasoning_content` whenever reasoning
is active and the current request carries function tools—even when an earlier
assistant made no tool call. A first tool-enabled turn needs no manufactured
history; requests without tools and explicitly disabled thinking remain
unaffected. Any present history `reasoning_content` is still typed as
string/null under the exact protocol. The generic OpenAI-compatible request
cannot prove which older turns originally carried tools, so the stateless
contract deliberately applies the conservative rule to all assistant history
when tools are present. Admission and provider-payload tests prove the field is
preserved, while route, Freeze and attempt continue to bind the same Plan
snapshot. A bounded review found and closed the non-string history bypass. This
is `locally_conformant` only; no live conversation, agent orchestration,
streaming tools, beta strict tools, pricing, billing, Release or deployment is
claimed.

Earlier closed local milestone:

`DeepSeek V4 Pro × official standard OpenAI-compatible Chat × reasoning/tool-choice differential`.

The append-only `deepseek.v4-pro.api@1.3.0` milestone left historical `@1.2.0`
addressable with its original reasoning and protocol contracts.
Pro now has its own immutable reasoning v3 identity for the current documented
`low|high|max`, `medium -> high` and `xhigh -> high` mapping while reusing the
reviewed DeepSeek standard Chat v3 function-tool contract. Model-specific tests
cover required choice under default thinking, exact Plan/payload/response
projection and the standard-surface parallel-null rejection. A bounded
differential review found no blocker or required finding. This is
`locally_conformant` only; it does not inherit Flash live evidence and does not
claim expanded tool history, streaming tools, beta strict tools, paid calls,
pricing, billing, Release or deployment.

Earlier closed local milestone:

`DeepSeek V4 Flash × official standard OpenAI-compatible Chat × non-stream function-tool choice/response consistency`.

The append-only `deepseek.v4-flash.api@1.3.0` milestone left historical
`@1.2.0` addressable with its original protocol and reasoning contracts.
The append-only standard-origin contract admits typed omitted/`none`/`auto`/
`required`/named function choice while thinking is active, freezes sorted
declared functions and the selected named function in the one Plan capability
snapshot, and validates every returned choice against that same evidence before
live or cache delivery. The standard origin rejects beta-only strict function
schemas, undocumented `parallel_tool_calls` including explicit JSON null, and
all tool streaming. Flash reasoning now models `low|high|max` with
`medium -> high` and `xhigh -> high` compatibility; omission remains provider
authority rather than a UniRouter-injected wire value. Focused Catalog,
reasoning, admission/prepare/payload, routing-snapshot, response, live-hold,
cache-cancel and affected shared tool-contract regressions pass. A bounded
closing review found the null-presence bypass and independently confirmed its
closure. This is `locally_conformant` code evidence only; exact multi-turn
thinking/tool history expansion, beta strict tools, paid calls, pricing,
billing, live verification, Release and deployment remain outside this
milestone.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × required/named function-tool response consistency`.

This stabilization does not promote a Profile or add a capability. For the
already typed non-stream function-tool surface, the single normalized Plan
capability entry now retains the sorted declared-function set and, for named
choice, the selected function. The constraint is part of the existing Plan
snapshot hash; the route receives only a deep-copied projection, while Freeze
and the provider attempt continue to bind that same hash. Route-aware response
validation now requires every choice under `required|named` to contain at least
one tool call, rejects zero choices, rejects undeclared functions, and requires
every named call to match the selected function. `auto` may return text, but any
tool call must still name a declared function. Live non-stream and response-
cache delivery consume the same validator before customer delivery or
settlement. Focused routing, prepare/admission, response, live-hold, cache-
cancel and affected Doubao/MiniMax/Claude tool regressions pass. A bounded
closing review found the zero-choice bypass and independently confirmed its
closure. This is `locally_conformant` code evidence only; it adds no paid call,
live verification, pricing, billing policy, Release, deployment or UI claim.

Earlier closed local milestone:

`Claude Sonnet 5 × Anthropic official Messages × non-stream user-defined function tools/tool_choice`.

The current/new-attachment pointer now selects
`anthropic.claude-sonnet-5.api@1.4.0`; historical `@1.3.0` remains addressable
with its exact strict text/reasoning and text-SSE contract and is not eligible
for new attachment. The append-only Messages v3 contract accepts only declared
client tools and exact `auto|none|any|tool` choice semantics, preserves exact
tool request/history/response identity under the output guard, and rejects
server tools, strict/cache/beta/MCP extensions and all tool streaming. Required
or named choices must return a matching `tool_use` unless the provider refuses.
The one normalized Plan capability snapshot carries explicit-request and
provider-default origins and remains the authority consumed by route evidence,
Freeze and provider-attempt receipts. Focused endpoint-protocol, Catalog,
qualification, evidence, routing and Gateway tests pass; bounded independent
review found one response-consistency blocker and confirmed its closure. This
is `locally_conformant` only; it does not add provider pricing, customer billing,
live verification, Release, deployment or an agent tool loop.

Earlier closed local milestone:

`Claude Haiku 4.5 × Anthropic official Messages × strict text/summarized-thinking streaming`.

The current/new-attachment pointer now selects
`anthropic.claude-haiku-4.5.api@1.2.0`; historical `@1.1.0` remains addressable
with its exact non-stream manual-thinking contract and is not eligible for new
attachment. The reviewed Messages v2 parser admits ordered summarized
`thinking_delta` events followed by their opaque signature, sends every visible
summary through the output guard, and forbids any non-identity replacement that
would invalidate the provider signature. A required thinking block must precede
text; sensitive cross-delta thinking fails without usage persistence or
settlement. The single Plan snapshot records explicit `reasoning`,
`reasoning_budget` and `streaming` invocation requirements. Focused protocol,
Catalog, product-binding, qualification, evidence and Gateway tests pass, and
the bounded candidate review found no finding. This is `locally_conformant`
only; it does not widen `display`, tools/multi-turn thinking replay, media,
cache, structured content, request service tier, native-USD supplier cost,
customer billing, live, Release or deployment support.

Earlier closed local milestone:

`Claude Opus 5 × Anthropic official Messages × strict text/omitted-reasoning streaming`.

The current/new-attachment pointer now selects
`anthropic.claude-opus-5.api@1.2.0`; historical `@1.1.0` remains addressable
with its exact non-stream Opus reasoning contract and is not eligible for new
attachment. This model-specific differential reuses the reviewed Messages v2
executor while retaining Opus's `thinking.type=disabled` restriction to
`low|medium|high` effort. Default adaptive/high reasoning and explicit
streaming enter the same Plan capability snapshot with their provider-default
and explicit-request origins; disabled thinking combined with `xhigh|max`
still fails before Freeze or dispatch. Focused Catalog, product-binding and
Gateway runtime tests pass, and the bounded candidate/promotion review found no
new defects. This is `locally_conformant` only; it does not widen thinking,
tools, structured content, media, cache, service tier, pricing, live, Release
or deployment support.

Earlier closed local milestone:

`Claude Sonnet 5 × Anthropic official Messages × strict text/omitted-reasoning streaming`.

At that milestone's closure, the current/new-attachment pointer selected
`anthropic.claude-sonnet-5.api@1.3.0`; it is now historical, addressable and not
eligible for new attachment, while the current pointer is recorded at the top
of this document. Historical `@1.2.0` likewise remains addressable with its
exact non-stream adaptive-thinking/effort contract. The append-only Messages v2 identity keeps non-stream and
stream delivery separately compiled. Its stream parser admits the exact
`message_start` / content-block / `message_delta` / `message_stop` lifecycle,
preserves opaque thinking signatures while rejecting unmodeled thinking deltas,
sanitizes provider model identity and errors, and requires final provider usage
before settlement. The single Plan capability snapshot records
`reasoning/provider_default`, `reasoning_effort/provider_default` and
`streaming/explicit_request`; route evidence, Freeze and the provider attempt
bind that same snapshot. Historical Messages v1 `stream:true` remains a
deterministic 400 before Freeze, authorization or provider dispatch. Focused
endpoint-protocol, qualification, evidence, Catalog and Gateway tests pass, and
the bounded closing review found and then confirmed closure of the historical
error-contract regression. This is `locally_conformant` only; it is not tool or
structured-content streaming, prompt caching, media, request service tier,
native USD supplier cost, customer billing, live verification, Release or
deployment.

Earlier closed local milestone:

`GPT-5.6 Luna × OpenAI Responses × strict text streaming`.

The current/new-attachment pointer now selects
`openai.gpt-5.6-luna.responses@1.3.0`; historical `@1.2.0` remains addressable
with its exact non-stream reasoning-effort contract and is not eligible for new
attachment. This model-data-only differential reuses the reviewed Responses v2
stream executor without widening its event, item, reasoning, usage or
settlement contracts. The exact Luna Profile is fixed before request
normalization; its single Plan capability snapshot records
`reasoning_effort/provider_default` and `streaming/explicit_request`, and route
evidence, Freeze and the provider attempt bind that same snapshot. Focused
Catalog, endpoint-protocol and Gateway runtime tests pass. This is
`locally_conformant` only; it is not reasoning-content, tool or
structured-output streaming, live verification, Release, deployment, native
USD supplier cost or wallet pricing evidence.

Earlier closed local milestone:

`GPT-5.6 Terra × OpenAI Responses × strict text streaming`.

The current/new-attachment pointer now selects
`openai.gpt-5.6-terra.responses@1.3.0`; historical `@1.2.0` remains
addressable with its exact non-stream reasoning-effort contract and is not
eligible for new attachment. This model-data-only differential reuses the
reviewed Responses v2 stream executor without widening its event, item,
reasoning, usage or settlement contracts. The exact Terra Profile is fixed
before request normalization; its single Plan capability snapshot records
`reasoning_effort/provider_default` and `streaming/explicit_request`, and route
evidence, Freeze and the provider attempt bind that same snapshot. Focused
Catalog, endpoint-protocol, Gateway runtime, Admin and onboarding tests pass.
This is `locally_conformant` only; it is not reasoning-content, tool or
structured-output streaming, live verification, Release, deployment, native
USD supplier cost or wallet pricing evidence.

Earlier closed local milestone:

`GPT-5.6 Sol × OpenAI Responses × strict text streaming`.

The current/new-attachment pointer now selects
`openai.gpt-5.6-sol.responses@1.3.0`; historical `@1.2.0` remains addressable
with its exact non-stream reasoning-effort contract and is not eligible for new
attachment. The append-only Responses v2 identity keeps non-stream and stream
as separately compiled delivery modes. Streaming accepts only the ordered core
text event lifecycle, anchors sequence numbers at zero, sanitizes every
response-bearing model identity, applies the existing bounded stream-output
guard before committing deltas, and reconciles provider text through
delta/done/item/completed. Final provider usage is accepted only from a valid
`response.completed`; failed/incomplete, invalid framing, early EOF and
conflicting text or usage cannot settle. The one request-scoped stream-policy
deadline spans provider dispatch and body reading, and failover remains legal
only before the first customer byte. Reasoning item shells are allowed only
under the exact composed reasoning contract with empty content/summary and no
opaque encrypted output. The single Plan capability snapshot records
`reasoning_effort/provider_default` and `streaming/explicit_request`; route
evidence, Freeze and provider attempt bind the same snapshot hash. Focused
endpoint-protocol, Planner, Billing, Gateway handler/runtime, Catalog, Admin and
onboarding tests pass, and the bounded independent review closed all three
identified blockers. This is `locally_conformant` only; it is not tool or
structured-output streaming, live verification, Release, deployment, native
USD supplier cost or wallet pricing evidence.

Earlier closed local milestone:

`MiniMax-M2.5-highspeed × MiniMax CN OpenAI-compatible Chat Completions × function tools/tool history/response`.

The current/new-attachment pointer selects
`minimax.m2.5-highspeed.token-plan@1.5.0`; historical `@1.4.0` remains
addressable with required embedded reasoning and automatic cache-read authority
but only its former compatibility-level tool boundary. This model-data-only
differential reuses the same reviewed MiniMax M2 v7 protocol as M2.7: non-stream
function definitions require `name` and object-form `parameters`; omitted
choice derives `auto/provider_default`, while explicit/named choice and parallel
tool controls fail closed. Exact assistant tool-call history requires replayable
non-empty embedded-thinking content and matching tool results; exact responses
bind `finish_reason=tool_calls` to the documented function-call shape. Reasoning,
prompt-cache and cache-read remain profile-always-required; tools/function-tools
are explicit and tool-choice-auto is provider-default-derived in the single Plan
snapshot. Focused model identity, request/history/response, payload, snapshot,
historical attachment, full modelprofile, shared M2.7/M3/Doubao tool and Admin
tests pass. This is `locally_conformant` only; it is not streaming-tools support,
live verification, Release or deployment.

Earlier closed local milestone:

`MiniMax M2.7 × MiniMax CN OpenAI-compatible Chat Completions × function tools/tool history/response`.

The current/new-attachment pointer selects `minimax.m2.7.token-plan@1.5.0`;
historical `@1.4.0` remains addressable with required embedded reasoning and
automatic cache-read authority but only its former compatibility-level tool
boundary. This append-only, same-provider/surface differential reuses the
reviewed MiniMax exact-tools contract: non-stream function definitions require
`name` and object-form `parameters`; explicit `tool_choice`, named choice and
`parallel_tool_calls` remain fail-closed, while omission with declared tools is
typed as `auto/provider_default`. Exact assistant tool-call history requires
replayable non-empty embedded-thinking `content` followed immediately by every
matching tool result; responses require matching `finish_reason=tool_calls` and
the exact function-call shape. M2.7 differs from M3 only in its already-frozen
always-on reasoning semantics: reasoning, prompt-cache execution and cache-read
evidence are always-required, while tools/function-tools are explicit and the
effective auto choice is provider-default-derived. All enter one normalized
Plan snapshot and the provider payload receives no invented choice or parallel
field. Focused request/history/response, admission, payload, capability snapshot,
historical attachment, full modelprofile, shared M3/Doubao tools and Admin tests
pass. This is `locally_conformant` only; it is not streaming-tools support, live
verification, Release or deployment.

Earlier closed local milestone:

`MiniMax M3 × MiniMax CN OpenAI-compatible Chat Completions × function tools/tool history/response`.

The current/new-attachment pointer selects `minimax.m3.token-plan@1.7.0`;
historical `@1.6.0` remains addressable with its exact cache/tier/image/reasoning
contracts and its former compatibility-level function-tool boundary. The
append-only v9 protocol follows the current MiniMax OpenAPI rather than generic
OpenAI assumptions: it accepts only non-stream `tools` entries of type
`function`, requires each function's `name` and object-form `parameters`, and
keeps the local 128-tool/name-shape safety subset. The documented omitted
behavior is typed separately as `auto/provider_default`; explicit `tool_choice`,
named choice and `parallel_tool_calls` remain fail-closed because this exact
surface does not declare those request fields. The provider default is a real
runtime contract, not a Catalog-only label, while immutable earlier contracts
retain their established fallback behavior. Assistant `tool_calls` history must
be followed immediately by matching `role=tool` results and preserve the
complete non-empty replayable `content`; duplicate, unresolved and mismatched
IDs fail closed. Provider responses require matching `finish_reason=tool_calls`,
exact function call IDs/names/string arguments and replayable assistant content;
legacy `function_call` and undocumented per-call response fields are rejected.
Tools/function-tools(explicit), tool-choice-auto(provider-default), reasoning
(provider-default) and cache/cache-read(always-required) are normalized once in
the same Plan snapshot consumed by route selection. Focused typed request,
admission, exact history/response, provider payload, capability snapshot,
historical attachment, full modelprofile, MiniMax/Doubao shared-tool and Admin
regressions pass. A bounded pre-code review found no blocker and its two required
runtime/default and inherited-wire-choice conditions are implemented. This is
`locally_conformant` only; it is not streaming-tools support, live verification,
Release or deployment.

Earlier closed local milestone:

`MiniMax M3 × official RMB pricing evidence × review-only onboarding candidate`.

The official MiniMax PAYG source records four current permanently-discounted
CNY-per-million-token bands: Standard at or below 512K input is
`¥2.10 / ¥8.40 / ¥0.42` for input/output/cache-read and above 512K is
`¥4.20 / ¥16.80 / ¥0.84`; Priority is respectively
`¥3.15 / ¥12.60 / ¥0.63` and `¥6.30 / ¥25.20 / ¥1.26`. The context band uses
total input including cache hits. The official Markdown fetched again on
2026-08-25 has the same SHA-256 as the frozen MiniMax source payload. The
onboarding candidate now projects all four bands and their exact commercial
limits while remaining `manual_review_required`: the current Rate Card selector
cannot express normalized tier plus input-length band, tier-aware
CustomerChargeQuote is not implemented, and a Token Plan subscription key is
not interchangeable with a PAYG API key. Automatic prompt-cache writes have no
extra supplier charge, but the OpenAI-compatible usage surface exposes only the
cache-read counter; no flat rate is manufactured from that fact. Focused UI
candidate tests pass, and a bounded financial-display review closed its two
required wording/regression findings with no blocker remaining. This is
`documented` supplier pricing evidence and a `locally_conformant` review-only UI
projection only. It is not an approved Pricing Profile, SupplierCostQuote,
SupplierCostObservation, CustomerChargeQuote, wallet charge, live verification,
Release or deployment.

Earlier closed local milestone:

`MiniMax M3 × MiniMax CN OpenAI-compatible Chat Completions × automatic cacheRead usage/evidence`.

At this milestone's closure the current/new-attachment pointer selected
`minimax.m3.token-plan@1.6.0`; it is now addressable and non-current, while
historical `@1.5.0` remains addressable with its exact tier/image/reasoning
contract and no executable cache authority. MiniMax's current prompt-cache page
now explicitly lists M3, documents automatic caching without a request switch,
and freezes the OpenAI-compatible read counter as
`usage.prompt_tokens_details.cached_tokens`; this supersedes the earlier local
snapshot in which the model-support table did not list M3. The append-only
Profile reuses the reviewed MiniMax exact cache wire: missing, null, competing
or contradictory counters fail closed, zero remains authoritative, and stream
requests inject or require `stream_options.include_usage=true`. Both a separate
terminal usage frame and a terminal-choice-plus-usage clean EOF are accepted by
the same provider-authority parser. Cache execution/read are always-required;
provider-default or explicit reasoning, explicit vision, explicit service tier
and streaming retain their own origins in the single Plan capability snapshot.
M3 reasoning-active streaming remains rejected, while explicit disabled-
reasoning text streaming uses the exact terminal-usage contract. Priority still
requires cost-observation mode and the surface still reports no delivered-tier
evidence. The documented cache prices and >512K context band remain supplier
pricing evidence only; they do not activate a CustomerChargeQuote, wallet charge
or flattened Rate Card. Focused modelprofile, adapter, non-stream/stream usage,
full prepare/admission/payload, image/tier composition, historical-boundary,
MiniMax regressions and Admin tests pass. A bounded closing review found no
blocker or required finding. This is `locally_conformant` only; it is not
canary-live-verified, released or deployed.

Earlier closed local milestone:

`MiniMax M3 × MiniMax CN OpenAI-compatible Chat Completions × service_tier contract/admission/runtime-policy/evidence`.

At this milestone's closure the current/new-attachment pointer selected
`minimax.m3.token-plan@1.5.0`; it is now addressable and non-current, while
historical `@1.4.0` remains addressable with its bounded inline-JPEG and
adaptive/disabled embedded-reasoning contracts but no executable tier policy.
The append-only Profile composes the exact MiniMax wire values
`standard|priority`, normalizes omission to `standard`, preserves an explicit
wire value in the provider payload and records requested/effective semantics
without inventing delivered-tier evidence. The current response surface has no
documented observed-tier channel, so successful and rejected attempts project
`unavailable_by_surface`; they never manufacture an empty response-header
source, observed tier or downgrade disposition. Explicit tier selection is an
invocation requirement in the one Plan capability snapshot; omitted provider-
default standard is not. Explicit priority remains cost-observation-only and is
rejected before credential decryption and Freeze in prepaid mode; standard does
not require billing-readiness lookup. The provider's documented priority 1.5x
rate is supplier price evidence only and does not activate a CustomerChargeQuote,
SupplierCostObservation or wallet charge. Both tiers retain the existing
300-second provider timeout because no different MiniMax runtime contract is
documented. Focused modelprofile, full prepare/admission/payload, capability
snapshot, billing-boundary, response-evidence, MiniMax regression and Admin
tests pass. A bounded pre-code review found the previously ambiguous missing-
observation behavior; a closing review confirmed that correction and required
one omitted-tier full-Plan regression, which is now covered. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`MiniMax M3 × MiniMax CN OpenAI-compatible Chat Completions × non-stream inline JPEG`.

At this milestone's closure the current/new-attachment pointer selected
`minimax.m3.token-plan@1.4.0`; it is now addressable and non-current, while
historical `@1.3.0` remains addressable with adaptive/disabled embedded
reasoning and no executable image contract. The append-only Profile adds only a
conservative user-message inline `data:image/jpeg` subset: object-form
`image_url`, 1–8 images, at most 256 KiB decoded per image, 15×15 minimum
dimensions, at most 131,072 pixels and aspect ratio at most 150. It is
non-stream and cannot be combined with tools or tool history, non-text response
formats, penalties or `n`; remote URLs, other official image MIME types, video,
Files, `detail`, `max_long_side_pixel` and provider 10 MB/64 MB envelope parity
remain fail-closed. Admission uses the same versioned local four-tokens-per-
pixel safety upper bound while provider payload preserves the exact JPEG bytes;
vision(explicit) and reasoning(provider-default or explicit) enter the single
Plan capability snapshot. Ordinary text, disabled-reasoning stream and
non-stream function tools retain their prior contracts. A bounded review found
that the first candidate inherited the historical unresolved
`cache-usage.minimax-m3.v1`; @1.4 now explicitly clears that field rather than
claiming M3 cache support, and the same reviewer confirmed closure. Focused
catalog, image/admission/payload, negative-combination, historical-boundary,
shared MiniMax reasoning/cache and Admin tests pass. This is
`locally_conformant` only; it is not canary_live_verified, released or deployed.

Earlier closed local milestone:

`MiniMax-M2.5-highspeed × MiniMax CN OpenAI-compatible Chat Completions × automatic cacheRead usage/evidence`.

At this milestone's closure the current/new-attachment pointer selected
`minimax.m2.5-highspeed.token-plan@1.4.0`; it is now addressable and non-current,
while historical `@1.3.0` remains addressable with required embedded reasoning
and no executable cache authority.
The official passive-cache model list includes the M2.5 series, whose
OpenAI-compatible response uses the same exact nested
`usage.prompt_tokens_details.cached_tokens` wire as M2.7. This append-only,
model-data-only promotion therefore reuses the independently reviewed MiniMax
M2 v6 contract: missing/null/competing counters fail closed, zero is
authoritative, streaming requests require terminal usage, and both separate
usage frames and terminal-choice-plus-usage clean EOF are supported under the
MiniMax-only exact opt-in. Reasoning, prompt-cache execution and cache-read
evidence are always-required and enter one normalized Plan snapshot. Focused
model identity, historical attachment, prepare/admission/payload, capability
snapshot and shared M2.7 cache/stream regressions pass. Because this is a pure
copy of the closed provider/surface pattern with only exact model identity and
version changes, it did not trigger another architecture review. Cache pricing,
CustomerChargeQuote, SupplierCost, wallet settlement, live validation, Release
and deployment remain outside this milestone. This is `locally_conformant`
only; it is not canary_live_verified, released or deployed.

Earlier closed local milestone:

`MiniMax M2.7 × MiniMax CN OpenAI-compatible Chat Completions × automatic cacheRead usage/evidence`.

At this milestone's closure the current/new-attachment pointer selected
`minimax.m2.7.token-plan@1.4.0`; it is now addressable and non-current, while
historical `@1.3.0` remains addressable with its required embedded-reasoning
contract and no executable cache authority. MiniMax documents passive prompt
caching for the M2.7 series without a request-side cache switch and reports
cache reads through `usage.prompt_tokens_details.cached_tokens`, including an
authoritative integer zero. The append-only Profile requires this exact counter
for successful non-stream responses and terminal stream usage, while rejecting
missing, null, competing or contradictory cache schemas. Streaming is retained:
UniRouter normalizes `stream_options.include_usage` from omitted to true,
rejects false, safely removes only documented empty/success MiniMax transport
metadata, and accepts both a separate `choices=[]` usage frame and the documented
schema's terminal-choice-plus-usage frame. In both forms the usage must pass the
same provider-authority parser; clean EOF cannot bypass the terminal requirement.
Reasoning, prompt-cache execution and cache-read evidence are always-required;
streaming remains an explicit invocation requirement. They are normalized once
into the same sorted Plan capability snapshot and route hash. Focused
modelprofile, adapter, gateway, relay, historical-boundary and shared MiniMax
regressions pass. A bounded review found the initially omitted combined terminal
wire shape; the contract was corrected with a MiniMax-v6-only opt-in and the same
reviewer confirmed the blocker closed. Cache pricing, CustomerChargeQuote,
SupplierCost, wallet settlement, live validation, Release and deployment remain
outside this milestone. This is `locally_conformant` only; it is not
canary-live_verified, released or deployed.

Earlier closed local milestone:

`MiniMax-M2.5-highspeed × MiniMax CN OpenAI-compatible Chat Completions × required embedded reasoning`.

The current/new-attachment pointer selects
`minimax.m2.5-highspeed.token-plan@1.3.0`; historical `@1.2.0` remains
addressable without executable reasoning. This model reuses the independently
reviewed M2.x required-reasoning contract closed for MiniMax M2.7: reasoning is
always required, ineffective explicit `thinking` controls are rejected,
omitted or `reasoning_split=false` uses embedded replayable content, separate
reasoning output and streaming tools fail closed, and reasoning enters the one
Plan snapshot only as `profile_always_required`. Its upstream identity,
204,800-token maximum output and 65,536 request default remain unchanged.
Model-specific catalog, exact request, prepare/admission/payload, historical
boundary and capability snapshot tests pass, together with the shared M2.7/M3
reasoning regressions. This was a reviewed-pattern differential change and did
not alter shared contracts, so no duplicate architecture review was triggered.
Cache, media, tier, pricing, live validation, Release and deployment remain out
of scope. This is `locally_conformant` only.

Earlier closed local milestone:

`MiniMax M2.7 × MiniMax CN OpenAI-compatible Chat Completions × reasoning request/default/history/response`.

The current/new-attachment pointer selects `minimax.m2.7.token-plan@1.3.0`;
historical `@1.2.0` remains addressable with its streaming/function-tools/
output-limit core and no executable reasoning contract. MiniMax documents M2.x
thinking as always enabled, including when a client sends the otherwise
ineffective `thinking.type=disabled`. UniRouter therefore models reasoning as
`required` and rejects explicit `thinking` rather than forwarding a control
that cannot produce the requested state. Reasoning is the Profile's sole
always-required capability and enters the one Plan snapshot only with
`profile_always_required` origin. Omitted or `reasoning_split=false` uses the
replayable `<think>...</think>` content shape; split output remains fail-closed.
Plain embedded-content streaming is retained, while separate reasoning deltas
and streaming function tools are rejected. Assistant tool history and tool-call
responses require non-empty replay `content`, and canonical assistant tool
messages cannot be rewritten by input or output guardrails. The 204,800-token
provider output maximum and 65,536 request default are unchanged. Focused
catalog, typed request, prepare/admission/provider-payload, history/response,
stream, historical-boundary and shared capability-origin regressions pass;
bounded differential review found no blocker or required finding. Cache,
media, service tier, pricing, live validation, Release and deployment remain
outside this milestone. This is `locally_conformant` only; it is not canary-
live-verified, released or deployed.

Earlier closed local milestone:

`MiniMax M3 × MiniMax CN OpenAI-compatible Chat Completions × reasoning request/default/history/response`.

At this milestone's closure the current/new-attachment pointer selected
`minimax.m3.token-plan@1.3.0`; it is now addressable and non-current, while
historical `@1.2.0` remains addressable with its exact streaming/function-tools/
output-limit core and no executable reasoning contract. The append-only Profile
models `thinking.type=adaptive|disabled`, with omitted `thinking` deriving the
provider default `adaptive`. MiniMax's default embedded mode is explicit:
omitted or `reasoning_split=false` is accepted, while `reasoning_split=true`
remains fail-closed because the official replay prose and request schema do not
provide one consistent exact history contract. Reasoning-active streaming and
streaming function tools remain fail-closed; explicit `thinking.disabled`
retains plain streaming. Reasoning-active assistant tool history must replay its
complete non-empty `content`, and input/output guardrails cannot rewrite that
tool transcript before Freeze or delivery. Separate `reasoning_content`,
`reasoning`, and stream reasoning deltas are rejected by the embedded-only
response contract. The provider-default or explicit reasoning requirement is
normalized once into the Plan capability snapshot consumed by route selection
and bound by the same route hash. Focused catalog, typed request, full
prepare/admission/provider-payload, history-preservation, response, stream,
historical-boundary, MiniMax/DeepSeek/Kimi regressions and UI projection tests
pass. Cache, media, service tier, pricing, live validation, Release and
deployment remain outside this milestone. This is `locally_conformant` only;
it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × non-stream inline JPEG`.

The current/new-attachment pointer selects
`doubao.seed-2.1-pro.api@1.6.0`; historical `@1.5.0` remains addressable with
its exact reasoning/streaming/tools/strict-JSON/implicit-cache contract and no
vision capability. The append-only Profile adds only a conservative
user-message `image_url` subset: inline `data:image/jpeg` payloads, 1–8 images,
decoded size at most 256 KiB per image, 15×15 minimum dimensions, at most
131,072 pixels and aspect ratio at most 150. It is non-stream and cannot be
combined with function tools or tool history, structured output, penalties or
`n`; remote URLs, other MIME types, video, audio and the provider's full 10 MB
image envelope remain fail-closed. Admission uses a versioned four-tokens-per-
pixel upper bound tied to the exact request snapshot while the provider payload
retains the original image bytes. Vision(explicit), cache execution/read
(always-required) and reasoning(provider-default) are normalized once into the
same Plan capability snapshot and route hash. Focused modelprofile,
prepare/admission/payload, negative-shape, shared Turbo/Evolving image and cache
regressions, Admin and frontend tests pass, followed by a bounded review with
no validated blocker or required finding. Pricing, full media parity, live
validation, Release and deployment remain outside this milestone. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Turbo × Ark OpenAI-compatible Chat × non-stream inline JPEG`.

The current/new-attachment pointer selects
`doubao.seed-2.1-turbo.api@1.6.0`; historical `@1.5.0` remains addressable with
its exact reasoning-effort/streaming/tools/strict-JSON/implicit-cache contract
and no vision capability. The append-only Profile adds only a conservative
user-message `image_url` subset: inline `data:image/jpeg` payloads, 1–8 images,
decoded size at most 256 KiB per image, 15×15 minimum dimensions, at most
131,072 pixels and aspect ratio at most 150. It is non-stream and cannot be
combined with function tools or tool history, structured output, penalties or
`n`; remote URLs, other MIME types, video, audio and the provider's full 10 MB
image envelope remain fail-closed. Admission uses a versioned four-tokens-per-
pixel upper bound tied to the exact request snapshot while the provider payload
retains the original image bytes. Vision(explicit), cache execution/read
(always-required) and reasoning effort(provider-default) are normalized once
into the same Plan capability snapshot and route hash. Focused modelprofile,
prepare/admission/payload, negative-shape, shared Evolving image and cache
regressions, Admin and frontend tests pass, followed by a bounded review with
no validated blocker or required finding. Pricing, full media parity, live
validation, Release and deployment remain outside this milestone. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Turbo × Ark OpenAI-compatible Chat × implicit cacheRead usage/evidence`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-turbo.api@1.5.0`; it is now addressable and non-current, while
historical `@1.4.0` remains addressable with
its exact reasoning-effort/streaming/tools/strict-JSON contract and no
cache-usage Profile. The official Ark product table lists Turbo online
inference context caching, while the Chat usage contract reports cache hits via
`usage.prompt_tokens_details.cached_tokens`. The append-only Profile reuses
`cache-usage.doubao-ark-chat.v1`, requires the nested read counter on every
successful non-stream final usage and streaming terminal usage report, and
accepts an explicit zero as authoritative evidence. Prompt-cache execution and
cache-read evidence are always-required capabilities; the same normalized Plan
snapshot combines them with provider-default reasoning effort, and route
compatibility binds the same requirements/hash. Missing counters fail closed.
Focused modelprofile, gateway, shared Pro/Evolving cache regression, Admin
catalog/onboarding and frontend tests pass, followed by a bounded candidate
review with no validated blocker or required finding. Explicit cache creation,
storage pricing, CustomerChargeQuote, supplier cost, wallet settlement, live
validation, Release and deployment remain outside this milestone. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Turbo × Ark OpenAI-compatible Chat × non-stream strict JSON Schema`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-turbo.api@1.4.0`; it is now addressable and non-current, while
historical `@1.3.0` remains addressable with
its exact reasoning-effort/streaming/non-stream-tools contract and no structured
output. The append-only Profile adds only
`response_format.type=json_schema` with an object schema and `strict=true`.
This bounded slice is non-stream and cannot be combined with root function
tools, typed function-tool history, `tool_choice`, `frequency_penalty` or
`presence_penalty`; `json_object`, non-strict schemas and structured streaming
remain fail-closed. Ordinary streaming and non-stream tools retain their prior
exact contracts. The strict capability and provider-default reasoning effort
are normalized once into the Plan capability snapshot consumed by route
selection, and route compatibility binds the same snapshot hash. Strict
responses must contain valid JSON and fail closed on unmodeled refusal or
truncation; plain invocations do not inherit strict response semantics. Focused
catalog, prepare/admission, provider-payload, response, historical-boundary,
shared Pro/Evolving regression and UI onboarding tests pass. A bounded review
found a history-only tools combination bypass; the typed history fact now closes
that path before Plan/Freeze, and the same reviewer confirmed the finding
closed. Pricing, billing, cache/search, live validation, Release and deployment
remain outside this milestone. This is `locally_conformant` only; it is not
canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Turbo × Ark OpenAI-compatible Chat × non-stream function tools`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-turbo.api@1.3.0`; it is now addressable and non-current, while
historical `@1.2.0` remains addressable with its exact
text/reasoning-effort streaming core and no function tools. The append-only
Profile models `none|auto|required|named` tool choice, provider-default `auto`
when tools are declared, provider-default parallel execution and explicit
serial execution. Function tools are non-stream only; ordinary streaming
retains the prior exact terminal-usage contract while stream-plus-tools,
function `strict` and tool-stream deltas remain fail-closed. When reasoning is
active, assistant tool-call history must replay nonempty `encrypted_content`
unchanged; `reasoning_effort=minimal` disables reasoning without manufacturing
opaque state. Typed tool, choice, parallel and reasoning requirements are
normalized once in the Plan capability snapshot consumed by route selection.
Focused catalog, full prepare/admission, payload, exact history/response, shared
provider-extension and UI onboarding tests pass, followed by bounded candidate
and shared-stabilization reviews with no validated blocker or required finding.
Pricing, billing, structured output, live validation, Release and deployment
remain outside this milestone. This is `locally_conformant` only; it is not
canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × implicit cacheRead usage/evidence`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-pro.api@1.5.0`; it is now addressable and non-current, while
historical `@1.4.0` remains addressable with
its exact reasoning/streaming/tools/strict-JSON contract and no cache-usage
Profile. Ark documents implicit cache as automatic, request-option-free and
enabled by default for Chat, while the exact Pro Chat examples report cache
hits through `usage.prompt_tokens_details.cached_tokens`. The append-only
Profile reuses `cache-usage.doubao-ark-chat.v1`, requires the nested read
counter on every successful non-stream final usage and streaming terminal
usage report, and accepts an explicit zero as authoritative evidence. Prompt
cache execution and cache-read evidence are always-required capabilities; the
same `ResolveCapabilityRequirements` authority combines them with typed
invocation requirements, and Plan/route compatibility bind the one normalized
snapshot/hash. Missing/null counters, absent core usage and competing cache
schemas fail closed. Focused modelrouting, modelprofile, gateway, adapter,
historical-boundary and UI tests pass, followed by a bounded candidate review
with no validated blocker or required finding. Explicit cache creation,
storage pricing, customer-wallet settlement, live validation, Release and
deployment remain outside this milestone. This is `locally_conformant` only;
it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × non-stream strict JSON Schema`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-pro.api@1.4.0`; it is now addressable and non-current, while
historical `@1.3.0` remains addressable with
its exact text/reasoning streaming and non-stream function-tools contract but
without structured output. The append-only Profile adds only
`response_format.type=json_schema` with an object schema and `strict=true`.
This bounded slice is non-stream and cannot be combined with function tools,
`tool_choice`, `frequency_penalty` or `presence_penalty`; `json_object`,
non-strict schemas and structured streaming remain fail-closed. Ordinary
streaming and non-stream tools retain their prior exact contracts. The strict
capability is derived from the normalized invocation as an explicit-request
origin, route selection consumes that one sorted Plan capability snapshot, and
the route/attempt compatibility evidence binds the same snapshot hash. Strict
responses must contain valid JSON and fail closed on unmodeled refusal or
truncation; plain invocations do not inherit strict response semantics. Focused
catalog, admission/prepare, payload, response, historical-boundary, Evolving
structured-output regression and UI onboarding tests pass, followed by a
bounded candidate review with no validated blocker or required finding.
Pricing, billing, `json_object`, streaming structured output, live validation,
Release and deployment remain outside this milestone. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × non-stream function tools`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-pro.api@1.3.0`; it is now addressable and non-current, while
historical `@1.2.0` remains addressable with its exact text/reasoning streaming
core and no function tools. The append-only
Profile models `none|auto|required|named` tool choice, provider-default `auto`
when tools are declared, provider-default parallel execution and explicit
serial execution. Function tools are non-stream only; ordinary streaming
retains the prior exact terminal-usage contract while stream-plus-tools and
function `strict` remain fail-closed. With provider-default reasoning active,
assistant tool-call history must replay nonempty `encrypted_content` unchanged;
explicitly disabled reasoning does not manufacture opaque state. Root tool
declarations and already-validated tool history both derive `tools` and
`function_tools` into the same normalized Plan capability snapshot consumed by
route selection. Focused catalog, full prepare/admission, payload, exact
history/response, shared Evolving regression and UI onboarding tests pass,
followed by a bounded review with no validated blocker or required finding.
Pricing, billing, tool streaming, strict function schemas, live validation,
Release and deployment remain outside this milestone. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Turbo × Ark OpenAI-compatible Chat × streaming`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-turbo.api@1.2.0`; it is now addressable and non-current, while
the latest `@1.6.0` current identity is described above. Historical `@1.1.0`
also remains addressable with its exact non-stream reasoning-effort core. The
append-only Profile reuses the
reviewed Ark reasoning stream transport contract while retaining Turbo's
independent `reasoning_effort=minimal|low|medium|high` contract, provider default
`high`, and `minimal`-means-disabled semantics. It adds only streaming, terminal
usage and opaque reasoning phases; tools, structured output, media, cache/search,
pricing, service tier, live validation and Release remain outside this
milestone. Focused model-difference and Pro/Evolving shared-stream regression
tests pass, followed by a bounded review with no validated blocker or required
finding. This is `locally_conformant` only; it is not canary-live-verified,
released or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × streaming`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-pro.api@1.2.0`; it is now addressable and non-current, while
historical `@1.1.0` remains addressable with its exact non-stream text/reasoning
contract. The Ark Chat contract
documents that stream usage is null unless
`stream_options.include_usage=true`; streamed reasoning can arrive through
`reasoning_content`, and the provider may emit a complete `encrypted_content`
block after reasoning and before answer content. The append-only `@1.2.0`
Profile retains that non-stream contract and adds only streaming through
`protocol.doubao-seed-2.1-pro-openai-chat.v2`. It normalizes an omitted
`include_usage` to true once, requires a separate terminal usage frame and
`[DONE]`, models content/reasoning/opaque phases, derives invocation streaming
and provider-default reasoning into the one Plan capability snapshot, and fails
closed on unmodeled tool, service-tier, mixed-usage or missing-terminal-usage
frames. Tools, structured output, media, cache/search, pricing, service tier,
live validation and Release remain outside this milestone. Focused catalog,
request/admission, payload, stream-state, relay/usage and shared Evolving stream
regression tests pass, followed by a bounded closing review with no validated
blocker or required finding. This is `locally_conformant` only; it is not
canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × implicit cacheRead usage/evidence`.

The current/new-attachment pointer selects
`doubao.seed-evolving.api@1.8.0`; historical `@1.7.0` remains addressable with
its exact image/tools/structured-output/streaming/reasoning contract and no
cache-usage Profile. The current Seed Evolving guide documents context caching
as supported and states that implicit cache is automatic, requires no request
option, and is enabled by default for Chat and Responses. The current Chat API
contract reports cache hits as `usage.prompt_tokens_details.cached_tokens`,
including an integer zero when no input token was served from cache; final
streaming usage uses the existing separate `choices=[]` terminal usage frame
requested through `stream_options.include_usage=true`. The append-only
`@1.8.0` Profile binds that exact Ark Chat wire to
`cache-usage.doubao-ark-chat.v1`, requires the read counter on every successful
usage report, and treats both automatic cache execution and cache-read evidence
as always-required route capabilities in the one normalized Plan snapshot.
Missing/null counters and competing top-level, hit/miss or cache-write schemas
fail closed; an explicit zero remains authoritative. Focused local non-stream,
terminal-stream, routing, catalog, admin and UI contract tests pass. Explicit
cache creation, cache storage metering, cache pricing, customer-wallet
settlement, live validation and Release are not part of this milestone. This is
`locally_conformant` only; it is not canary-live-verified, released or deployed.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × non-stream inline JPEG image input`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-evolving.api@1.7.0`; it is now addressable and non-current, while
historical `@1.6.0` remains addressable with its exact
strict-JSON/tools/streaming/reasoning contract and no image support.
The first executable image slice is intentionally narrower than the documented
provider surface: one to eight user-message `image_url` parts using an object
whose `url` is inline `data:image/jpeg;base64,...`, non-stream only. Its
append-only protocol v6 and versioned
UniRouter execution policy limits each decoded JPEG to 256 KiB and 131,072
pixels, requires dimensions of at least 15×15 and an aspect ratio no greater
than 150:1, and reserves the larger of the existing raw JSON byte upper and
the sanitized text upper plus four tokens per decoded pixel. This is a local
safety bound, not a provider tokenizer formula, reported usage or pricing
authority. Exact typed validation creates one request-bound admission shadow;
that same evidence derives invocation `vision` into the one normalized Plan
snapshot, while the provider receives the original image bytes. Ordinary
text/reasoning output remains under the current response guard.
Remote URLs, Files/file IDs, system-message images, `detail`,
`image_pixel_limit`, other MIME types, tools, structured output, penalties,
`n`, provider 10 MB parity, an Evolving-specific image-token formula, pricing,
live validation and other Ark models remain outside this slice.
This milestone is `locally_conformant` only; it is not canary-live-verified,
released or deployed.

Earlier closed evidence milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × image-input evidence intake`.

The official model list updated 2026-08-24 places the floating
`doubao-seed-evolving` identity in the recommended visual-understanding models
and labels it multimodal. The image-understanding guide updated 2026-08-13 says
Chat accepts image URL and Base64 input, is stateless across turns, limits each
URL/Base64 image to 10 MB and the Base64 request body to 64 MB, and requires
width and height above 14 px, 196–36,000,000 total pixels and aspect ratio
1/150–150. It documents JPEG, PNG, GIF, WebP and additional formats, requires
declared/extension format to match actual content, and says visual requests do
not support `frequency_penalty`, `presence_penalty` or `n`. The Chat API page
updated 2026-08-20 models `image_url.url` as either a link or Base64 value, but
its explicit Chat `file_id` support list does not name Seed Evolving. The
`detail` and image-token tables group fixed Seed generations without assigning
the floating Evolving alias a stable default or band. Therefore the intake is
`documented` evidence only; it establishes technical feasibility, not product
authorization for the inline-JPEG implementation. Its batch inclusion is a
separate decision in Baseline §5; documentation cannot promote a Profile.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × non-stream strict json_schema request/admission/response evidence`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-evolving.api@1.6.0`; it is now addressable and non-current, while
historical `@1.5.0` remains addressable with its exact
streaming/reasoning/tools contract. The append-only v5 Chat contract
admits only `response_format.type=json_schema` with `strict=true`, a required
string name and object schema, and an optional string description. It derives
`strict_json_schema` from the invocation into the one normalized Plan
capability snapshot consumed by routing and carried by the attempt. Schema mode
is non-stream and cannot be combined with function tools, `tool_choice`,
frequency penalty or presence penalty. A provider success is deliverable only
when every choice stops normally and contains non-empty valid JSON; non-null
refusal, truncation, malformed JSON, unknown response shape and a guardrail
transform that invalidates JSON all fail closed before delivery/settlement.
The live and cache-hit wiring regressions prove provider-response hold or cache
Cancel with no delivery preparation; the live rejection also proves no
settlement or cache commit. UniRouter does not claim to implement the provider's
full JSON Schema evaluator. `json_object`, non-strict schema, streaming
structured output, pricing, live validation and other Ark models remain outside
this slice. This milestone is `locally_conformant` only.

Earlier closed evidence milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × structured-output evidence intake`.

The 2026-08-24 official model list explicitly marks the floating
`doubao-seed-evolving` identity as supporting structured output and recommends
`json_schema`. The current Chat API contract defines `text|json_schema|json_object`,
requires a string `name` and object `schema`, permits an optional string
`description`, and defines `strict` as a boolean with provider default `false`.
The beta guide recommends `json_schema`; `strict=true` promises adherence to the
supported schema subset, while false or omission only attempts to follow it.
It also states that truncation, task complexity or unclear formats can still
produce erroneous output, that unsupported constraint keywords can be rejected,
and that frequency/presence penalties should not be combined with structured
output. The guide does not establish an Evolving-specific streaming response or
refusal contract. Consequently this intake is `documented` evidence only and
supports the technical basis of the non-stream strict-schema milestone above;
implementation scope derives from the separately requested structured-output
work, not this documentation. It does not itself change an executable Profile.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × non-stream function tools/tool_choice`.

The current/new-attachment pointer now selects
`doubao.seed-evolving.api@1.5.0`; historical `@1.4.0` remains addressable with
its exact streaming/reasoning contract. The append-only v4 Chat contract admits
only non-stream function tools: optional description and parameters, object
parameters when present, boolean `strict`, a local portable name subset and a
128-tool safety maximum. It models `none|auto|required` and named choices,
records omitted-with-tools `auto` and callable-tools parallelism as provider
defaults, and retains explicit/provider-default origins in the one normalized
Plan capability snapshot consumed by route selection. Explicit auto, required
or named choices without declared tools, meaningless parallel flags and
stream+tools fail closed. Reasoning-active assistant tool history must replay a
non-empty `encrypted_content` signature; it stays in the provider payload but
is removed only from the admission shadow. Non-stream output accepts exact
assistant `tool_calls` and `finish_reason=tool_calls`, while legacy
`function_call`, response roles other than assistant and indexed non-stream
tool calls fail closed. Streaming tool deltas, structured output, media,
cache/search, pricing, live validation, Release and deployment remain outside
this slice. This milestone is `locally_conformant` only.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × streaming contract/admission/payload/response/usage evidence`.

The current/new-attachment pointer now selects
`doubao.seed-evolving.api@1.4.0`; historical `@1.3.0` remains addressable and
non-stream. The append-only exact stream contract admits only `content` and
`reasoning_content` text deltas plus response-only non-empty
`encrypted_content`; the opaque block is optional, but if present it is a
single reasoning-to-answer phase and never crosses the content guard. The
provider request normalizer sets `stream_options.include_usage=true` only when
the client omitted it; explicit false, `chunk_include_usage`, obfuscation,
tools, service-tier fields and other unmodeled frames fail closed. A successful
provider terminal requires one separate `choices=[]` usage frame followed by
`[DONE]`; missing, duplicate, mixed or out-of-order terminal evidence converts
the already disclosed portion to an interrupted stream and never fabricates
provider usage. `completion_tokens_details.reasoning_tokens` remains evidence
inside completion usage rather than a second billable meter. Streaming uses
the existing UniRouter 30-minute maximum-duration, 30-second write-idle and
15-second heartbeat policy; the provider-documented 1,800-second timeout is
still non-stream-only. This slice is `locally_conformant`, not live-verified,
priced for customer billing, released or deployed.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × non-stream reasoning contract/admission/runtime-policy/response evidence`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-evolving.api@1.3.0`; it is now addressable and non-current, while
historical `@1.2.0` remains plain-text-only. The exact reasoning contract models `thinking.type` as
`enabled|disabled` with provider default `enabled`, and
`reasoning_effort` as `none|minimal|low|medium|high|xhigh|max` with provider
default `high`; `none|minimal` disable reasoning and `xhigh|max` retain their
wire values while the provider effective level is `high`. Supplying both
controls fails closed. Ordinary assistant `reasoning_content` history is
accepted, response `reasoning_content` crosses the output guard, and
`encrypted_content` remains response-only opaque evidence. The single Plan
capability snapshot preserves explicit-request versus provider-default origin.
Active non-stream reasoning receives the documented 1,800-second provider
timeout, a 1,860-second application write deadline and a strictly larger
1,920-second Nginx transport ceiling. The exact Plan/evidence stage completes
before that deadline is established; credential decryption and provider-request
materialization occur only afterward, and deadline failure remains pre-Freeze,
pre-credential and pre-dispatch. Tools, structured output, media, cache/search,
service tier and pricing remain closed. This slice is
`locally_conformant`, not live-verified, billed, released or deployed.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark OpenAI-compatible Chat × strict non-stream core`.

At this milestone's closure the current/new-attachment pointer advanced to
`doubao.seed-evolving.api@1.2.0`; it is now an addressable non-current Profile,
and the current `@1.6.0` pointer is recorded above. `@1.2.0` preserves the
floating requested alias and exact Ark Beijing `/api/v3` identity while
admitting only plain non-stream Chat text. The request default is 4,096;
`max_tokens` is bounded at 262,144 and the
compatibility `max_completion_tokens` field at 65,536, and the two fields are
mutually exclusive. Streaming, tools, structured output, service tier,
reasoning controls/history and unmodeled response reasoning/tool shapes fail
closed. The request passes the full typed validation/admission/Planner path
without capability inflation, preserves the exact provider payload, and the
plain response guard is exact-profile bound. Historical `@1.0.0/@1.1.0` remain
addressable and non-current. This slice is `locally_conformant`; it is not
live-verified, priced for customer billing, released or deployed.

Earlier closed local milestone:

`Doubao Seed Evolving × Ark API identity × floating-alias evidence contract`.

The append-only `doubao.seed-evolving.api@1.1.0` candidate records model family
and requested wire alias as `doubao-seed-evolving`, version policy `floating`,
the exact Ark Beijing `/api/v3` endpoint class, the documented 1,048,576
context / 262,144 output maxima and the Chat request default of 4,096. These
fields are exact-address metadata: historical `@1.0.0` keeps the new fields
absent, so it is not retroactively reinterpreted.
`@1.1.0` remains non-current and has no executable capabilities; frontend
onboarding therefore does not offer it. Provider `reported_model` remains probe
evidence and cannot rewrite the requested alias or Baseline. A future observed
model from a real execution belongs in an immutable attempt receipt and is not
implemented here. Go validation, public JSON/OpenAPI/TypeScript projections,
identity/routing negatives and onboarding tests are green. This identity slice
is `locally_conformant`; its execution Profile remains `documented`, not live
verified, released, deployed or billing authority.

Earlier closed local milestone:

`Doubao Seed 2.1 Turbo × Ark OpenAI-compatible Chat × strict non-stream core + reasoning-effort contract`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-turbo.api@1.1.0`; it is now addressable and non-current, with
the latest `@1.6.0` current identity described in the top milestone. The
historical Profile pins
`doubao-seed-2-1-turbo-260628` to the exact Ark Beijing `/api/v3` Chat surface,
preserves the 262,144 provider context/output maxima and separately records the
provider-documented 4,096 request default. `max_tokens` is bounded at 262,144;
`max_completion_tokens` is independently bounded at 65,536 and mutually
exclusive with it. The typed `reasoning_effort` contract admits
`minimal|low|medium|high`, records the provider default `high`, and treats
`minimal` as disabled while preserving the exact wire value. Summarized
reasoning crosses the output guard and nonempty `encrypted_content` remains
profile-bound opaque evidence. Streaming, tools, structured output, media,
history replay, cache/search, service tier and pricing remain fail-closed.
Historical `@1.0.0` remains addressable. Focused request/admission/Planner,
response, onboarding and independent blocker re-review evidence is green. This
slice is `locally_conformant`, not live verified, billing authority, released
or deployed.

Earlier closed local milestone:

`Doubao Seed 2.1 Pro × Ark OpenAI-compatible Chat × strict non-stream core + provider-default reasoning response evidence`.

At this milestone's closure the current/new-attachment pointer selected
`doubao.seed-2.1-pro.api@1.1.0`; it is now addressable and non-current, with
the current Profile described in the latest milestone above. The historical Profile pins
`doubao-seed-2-1-pro-260628` to the exact Ark Beijing `/api/v3` endpoint
class, preserves the 262,144 provider context/output maxima, and records the
Chat API's provider-documented 4,096 request default separately. `max_tokens`
is bounded at 262,144 while `max_completion_tokens` is independently bounded
at 65,536; the aliases are mutually exclusive. Non-stream text and summarized
`reasoning_content` cross the output guard, while nonempty `encrypted_content`
is preserved only under the exact protocol as opaque evidence. Omitted
thinking uses the provider's enabled default and explicit enabled/disabled is
typed; history replay, effort, streaming, tools/tool responses, structured
output, media, cache/search, service tier and pricing remain fail-closed.
Historical `@1.0.0` remains addressable. Deterministic local tests and an
independent blocker re-review are green. This slice is `locally_conformant`,
not live verified, billing authority, released or deployed.

Earlier closed local milestone:

`Claude Haiku 4.5 × Anthropic official Messages × extended-thinking budget contract`.

At that milestone's closure, the current/new-attachment pointer selected
`anthropic.claude-haiku-4.5.api@1.1.0`. It pins the official Messages origin and
`claude-haiku-4-5-20251001`, preserves the 200,000 context / 64,000 provider
maximum / 16,384 request-default distinction, and models manual extended
thinking independently from the Claude 5 adaptive-effort Profiles. Omission and
explicit `disabled` keep thinking off; `enabled` requires a canonical integer
`budget_tokens` from 1,024 through 63,999 and strictly below the request's
`max_tokens`. Adaptive thinking, effort, display selection, tools, media,
streaming, cache and structured output remained fail-closed. Summarized thinking
crosses the output guard while signatures and safety-redacted thinking data are
preserved as opaque evidence; reasoning blocks must precede text. Historical
`@1.0.0` remains addressable and unchanged. `@1.1.0` is now historical and
immutable; current `@1.2.0` adds only the exact strict summarized-thinking and
text streaming subset recorded at the top. Deterministic tests are green; the
exact PostgreSQL integration fixture exists but is environment-SKIP because the
sandbox cannot reach localhost PostgreSQL. This slice is
`locally_conformant`, not live-verified, native-USD supplier cost, customer
billing, released or deployed.

Earlier closed local milestone:

`Claude Opus 5 × Anthropic official Messages × model-specific thinking/effort differential`.

At that milestone's closure, the current/new-attachment pointer selected
`anthropic.claude-opus-5.api@1.1.0`. It reuses the reviewed strict non-stream
Messages text, refusal, inclusive usage and response-metadata path while owning
an Opus-specific reasoning Profile. Adaptive thinking defaults on, effort
defaults to `high`, and explicit `thinking.type=disabled` accepts only
`low|medium|high`; `xhigh|max` combinations fail during typed preparation before
Freeze, credential access or dispatch. Historical `@1.0.0` remains addressable
and unchanged. `@1.1.0` is now historical and immutable; current `@1.2.0` adds
only the exact strict text-streaming subset recorded at the top. The PG integration case exists but was skipped because the
current sandbox cannot access localhost PostgreSQL. This is
`locally_conformant`, not live-verified, billing authority, released or
deployed.

Earlier closed local milestone:

`Claude Sonnet 5 × Anthropic official Messages × explicit thinking/effort controls + omitted-thinking response evidence`.

The append-only `anthropic.claude-sonnet-5.api@1.1.0` candidate pins Anthropic's official origin,
native Messages operation and exact public endpoint protocol. The bounded
request subset requires a final user message, admits explicit `max_tokens` up to
128,000 and records the provider's default adaptive thinking when the control is
omitted. Explicit `thinking` and `output_config.effort` remain rejected by this
version's strict grammar rather than being passed through without a typed
contract. It also rejects manual thinking budgets, non-default sampling
controls, assistant prefill, tools, media and other unmodeled fields before
dispatch. Typed refusal responses preserve usage
and provider-success health; customer-visible `stop_details.explanation` passes
through the same output guard as message text. Historical `@1.0.0` remains
addressable and unchanged. Deterministic non-database tests are green; the SQL
integration fixture could not execute in the current sandbox because localhost
PostgreSQL is unavailable, so it is not counted as passed. Official response
evidence subsequently confirmed that default adaptive thinking may return an
omitted-thinking block with an opaque signature, which `@1.1.0` does not yet
parse. At that milestone's closure, the current/new-attachment pointer selected
`@1.2.0` after closing
typed `thinking`/`output_config.effort`, effective request semantics, omitted
thinking evidence, response-only `usage.inference_geo` / `usage.service_tier`
metadata, and per-invocation capability origins together. Explicit
`thinking.type=disabled` rejects contradictory thinking blocks or positive
thinking-token evidence. `@1.0.0` and `@1.1.0` remain addressable and unchanged.
`@1.2.0` is now historical and remains immutable; current `@1.3.0` adds only
the exact strict text-streaming subset recorded at the top of this document.
Tools, media, cache, structured output and request service tier remain outside
the current Profile. Nothing in these slices is live-verified, native-USD
supplier cost, customer billing, released or deployed.

Earlier closed local milestone:

`GPT-5.6 Luna × Responses × reasoning.effort`.

At this milestone's closure, the current/new-attachment pointer selected
`openai.gpt-5.6-luna.responses@1.2.0`. That historical Profile composes the same
exact stateless, non-stream Responses text protocol reviewed for Sol/Terra with the Luna-specific
`reasoning.openai-gpt-5.6-luna-responses.v1` contract. The request admits only
`reasoning.effort=none|low|medium|high|xhigh|max`; omission preserves OpenAI's
documented `medium` default. Exact runtime and SQL Planner tests prove request
preservation and required route evidence. Luna `@1.2.0` remains addressable but
is no longer eligible for new attachment; historical Luna `@1.1.0` remains
addressable and core-only. This is `locally_conformant`; it is not live-verified,
native-USD cost evidence, customer billing, released or deployed.

Earlier closed local milestone:

`Qwen3.8-Max × DashScope CN Beijing OpenAI-compatible Chat × thinking/preserve/admission/output limit`.

The current/new-attachment pointer now selects `qwen.3.8-max.api@1.8.0`.
Its versioned `reasoning.qwen3.8-max-hybrid.v2` contract preserves the provider
defaults (`enable_thinking=true`, `reasoning_effort=xhigh`,
`thinking_budget=131072`, `preserve_thinking=true`) while accepting both
`max_tokens` and `max_completion_tokens` through the documented 131,072 model
output maximum in thinking and non-thinking modes. The typed provider
extensions survive pre-profile admission, exact Profile validation, Plan
capability derivation and provider payload construction. Historical `@1.7.0`
continues to resolve against the earlier v1 reasoning contract and is not
backfilled. This is `locally_conformant`; it is not live-verified, billing
authority, released or deployed.

Earlier closed local milestone:

`GPT-5.6 Terra × Responses × reasoning.effort`.

At this milestone's closure, the current/new-attachment pointer selected Terra
`@1.2.0` only after the shared capability authority, M85 Freeze-to-attempt
snapshot fence, known focused regressions and the bounded closing review were
closed. Terra `@1.2.0` is now historical and retains that exact non-stream
reasoning contract; Terra `@1.1.0` and Luna `@1.1.0` retain historical core-only
grammar. This is
`locally_conformant`; it is not live-verified, released or deployed.

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
- Profile addressability and new-attachment eligibility are now independent.
  The immutable product-identity manifest materializes every exact historical
  address, while the current-for-surface index contains only the attachable
  version pointer. It cannot fill provider, region, surface, operation or
  request-default fields. A surface-agnostic alias resolver fails closed when
  the same upstream alias has more than one current surface; reviewed onboarding
  must select the exact surface Profile.
- Kimi K2.6, K2.7 Code and K3 have modeled reasoning contracts. K2.6 records
  `thinking.keep=null|all`, K2.7 fixes preserved thinking to `all`, and K3
  validates `reasoning_effort=low|high|max`; omission preserves each provider
  default. The current profiles also reject non-provider sampling values and
  invalid model-specific `tool_choice` combinations before dispatch.
- Current DeepSeek Flash/Pro `@1.4.0` use the immutable v4 thinking/tool-history
  contract: `low/high/max`, with `medium/xhigh -> high` compatibility while
  preserving the original wire value. Non-stream `none/auto/required/named`
  choices are permitted under active thinking. Unknown effort, disabled thinking
  plus effort, ineffective sampling and missing reasoning replay in assistant
  history of an active-thinking tools request fail closed. Earlier exact
  versions retain their historical restrictions; they are not reinterpreted.
- Kimi Platform top-level `usage.cached_tokens` and DeepSeek hit/miss counters
  have profile-bound usage parsers. A generic OpenAI-compatible route cannot
  turn DeepSeek-shaped fields into billing evidence. Generic nested
  `prompt_tokens_details.cached_tokens` is also recognized.
- Once one of those profile-bound provider counters is authoritative, the
  Provider Adapter preserves the original fields and mirrors cache reads into
  `prompt_tokens_details.cached_tokens` for generic OpenAI SDKs and Agent
  clients. This response projection does not create billing authority for an
  untyped route.
- `gemini.3.7-flash.api@1.7.0` composes
  `processing-tier.gemini-openai-compatibility.v1` into the existing Chat
  protocol. Omission and `default` normalize to `standard`; non-stream Flex uses
  a bounded 900-second provider timeout. Every typed tier receives a
  request-specific write deadline that covers its effective provider timeout.
  Streaming keeps the longer of that minimum and the existing stream-policy
  maximum, while the `/v1` proxy chain permits the initial provider wait.
  Response evidence comes only from
  `x-gemini-service-tier`; missing and unknown values remain explicit gaps, and
  Priority-to-Standard is recorded as a provider-reported downgrade only on an
  accepted response rather than inferred locally. A rejected response can retain
  the typed header value but never receives a successful tier disposition.
  Gemini 3.5 Flash and Flash-Lite now compose the same provider/surface policy
  at their respective immutable `@1.7.0` versions.
- Chat tools, JSON Object/Schema and preserved `reasoning_content` have common
  protocol machinery.
- The historical `kimi.k2.6.api@1.5.0`, `kimi.k2.7-code.api@1.1.0` and
  `kimi.k3.api@1.5.0` already used the Kimi API Platform surface with typed Chat
  contracts for final-assistant Partial Mode, K3 system-message Dynamic Tools, exact
  `$web_search` admission/evidence, and MIME-bounded inline image/video data
  URLs. Partial plus JSON Object is rejected as a documented incompatible
  combination. K2.6 and K3 expose one serial, non-stream `$web_search` call per
  provider response with an exact `builtin_web_search_calls` reservation and
  actual-usage meter; K2.7 Code does not claim that capability. Provider URL
  and file references remain outside this inline contract.
- `kimi.k2.6.api@1.5.0` freezes MoonshotAI's reviewed K2.6 `tiktoken.model`,
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
- The product-facing “模型接入” form composes a reviewed provider preset with
  the latest exact version of each matching Profile ID. It offers only safe
  aliases and Profile-compatible operations, fills documented provider maxima
  into public/deployment upper-bound fields, and keeps the provider request
  default in `deployment.default_max_output`. When a provider does not document
  an independent output maximum, the form labels that fact and uses the reviewed
  request default as a safe effective public limit instead of presenting it as a
  provider maximum. It sends the exact Profile identity for backend
  validation/persistence. These are authoring values, not live capability,
  price, credential, probe, Release or traffic evidence; custom/omitted selection
  remains explicitly `unverified.chat@1.0.0`.
- When a separate dated official-price snapshot exists, onboarding may also
  offer an evidence-backed `provider_list` Rate Card draft. That second write
  remains review-only and never turns the execution Profile into price
  authority, auto-activates a card or changes runtime pricing mode.
- The 2026-08-24 onboarding catalog introduced the approved 22-model expansion:
  GLM-5.3/5.2/5V-Turbo; Qwen3.8-Max and
  Qwen3.7-Plus/Flash; Gemini 3.7/3.5 Flash and 3.5 Flash-Lite; MiMo V2.5 Pro
  and V2.5; Hy3; GPT-5.6 Sol/Terra/Luna; Grok 4.6; Claude Opus 5, Sonnet 5
  and Haiku 4.5; and Doubao Seed Evolving, Seed 2.1 Pro and Seed 2.1 Turbo.
  Each product identity freezes or explicitly floats its upstream alias,
  provider/region/surface, documented limits and request-default authority.
  Some have since advanced through local conformance Profiles; the exact current
  Profile is authoritative for executable capabilities. Catalog advancement is
  still not `canary_live_verified` or `released` evidence.

The following are still real gaps:

- Current MiniMax M2.5-highspeed/M2.7 `@1.5.0` and M3 `@1.7.0` model embedded
  reasoning, exact non-stream function history/response and automatic cache-read
  usage. Split reasoning, streaming tools, explicit tool-choice/parallel wire
  controls, full media coverage and an approved customer-price contract remain
  gaps; current support is no longer limited to tools alone.
- Historical Kimi Coding profiles remain separate and do not claim cache
  billing authority. The current K2.7 Code profile deliberately targets the
  official Kimi API Platform (`api.moonshot.cn`) surface, whose top-level
  `usage.cached_tokens` contract is shared with the other Platform models.
- Kimi inline image/video support is locally contract-verified only. Large
  payloads remain subject to UniRouter's conservative byte upper bound; provider
  file IDs and remote URLs require separately governed Files/URL contracts.
- Kimi K2.7 Code Platform Partial Mode, cache, JSON Object and inline
  multimodal shapes are locally contract-verified. Provider-live conformance,
  large-media estimation and the separate subscription Coding surface remain
  distinct evidence gaps.
- Public Responses and Anthropic Messages retain bounded versioned subsets.
  Messages/Responses v1 are non-stream; v2 adds exact text/reasoning SSE.
  Responses rejects function tools, structured output and media even when
  non-stream. Messages text-tools v3 adds only Sonnet's non-stream user-defined
  function tools; it does not confer those capabilities on Opus or Haiku.
  Sol, Terra and Luna Responses compose bounded, model-specific
  `reasoning.effort` request sub-contracts; reasoning summary and multi-turn
  encrypted-state replay remain rejected.
- GPT-5.6 Sol, Terra and Luna now have separate current Chat and Responses
  Profile IDs. Chat keeps its locally conformant core
  streaming/reasoning/function-tools/structured-output/inline-image/cache
  subset. Sol, Terra and Luna Responses have a locally conformant strict
  non-stream core text contract with an exact endpoint-protocol version. Their
  current `@1.3.0` Profiles additionally carry exact strict text SSE on top of
  their profile-bound `reasoning.effort`.
  The narrow `reasoning_effort` capability identity remains model-specific. The
  richer official built-in-tool surface remains documentation-required, not
  executable capability evidence. Claude Sonnet 5, Opus 5 and Haiku 4.5 now
  each have a current exact strict Messages text/reasoning SSE subset; their
  model-specific adaptive-effort and manual-budget contracts remain separate.
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
| Kimi K2.6 | 262,144 context; thinking toggle and optional preserved thinking; text/image/video; ToolCalls, JSON Mode, Partial Mode, automatic cache and built-in search | current `kimi.k2.6.api@1.7.0`; exact frozen tokenizer admission; typed fixed parameters/keep; exact non-stream function tools with omitted/none/auto choice, complete history and declared-function response consistency; standard JSON Object; final-assistant Partial; Kimi cache usage; MIME-bounded inline image/video; one serial non-stream `$web_search` call with exact unit-meter evidence, including thinking-enabled requests | `tool_choice:null`, required/named/parallel and function-tool streaming; provider-file/remote-URL media; large-media estimator; target live capability conformance; streaming search metering; Dynamic Tools are not a K2.6 claim |
| Kimi K2.7 Code | 262,144 context and 32K default output; text/image/video; thinking-only with preserved thinking; ToolCalls, JSON Mode, Partial Mode and automatic cache | current `kimi.k2.7-code.api@1.3.0` on Kimi API Platform; typed required-thinking/fixed parameters; exact non-stream function tools with omitted/none/auto choice, complete reasoning-preserving history and declared-function response consistency; standard JSON Object; final-assistant Partial; Kimi cache usage; MIME-bounded inline media | `tool_choice:null`, required/named/parallel and function-tool streaming; target live capability conformance; large/file media; highspeed and separate subscription Coding identities; Anthropic coding conformance |
| Kimi K3 | 1M provider maximum with a 256K current customer default; 131,072 default and 1,048,576 maximum output; always-on reasoning; `low/high/max`; tools, JSON Object/Schema, Partial, dynamic tools, built-in search, vision/video and automatic cache | current `kimi.k3.api@1.8.0`; typed effort/fixed-parameter constraints; exact non-stream top-level plus system-message Dynamic Tools with one visible-name Plan constraint and omitted/none/auto/required choice; exact tool history/response; standard JSON; final-assistant Partial; Kimi cache usage; MIME-bounded inline image/video; one serial non-stream `$web_search` call with exact unit-meter evidence; historical `@1.6.0/@1.7.0` remain addressable | named choice remains fail-closed because K3 reasoning is always active; provider-file/remote-URL media; large-media estimator; function-tool and search streaming metering; full target live multi-turn/capability conformance |
| DeepSeek V4 Flash | Chat, native Responses and Anthropic surfaces; thinking toggle/effort; tools; JSON; prefix completion; FIM; Files; KV cache. Vision belongs to the separate `deepseek-v4-flash-vision-exp` model | current `deepseek.v4-flash.api@1.4.0`; standard OpenAI-compatible Chat; typed `low/high/max` reasoning plus `medium/xhigh -> high` compatibility; non-stream omitted/none/auto/required/named function choice; single Plan declared/selected-function authority; exact per-choice tool response and function-history validation; active tools requests conservatively require non-empty reasoning replay for all assistant history; standard-surface strict/parallel rejection; profile-bound cache hit/miss usage; versioned 1M/384K provider limits. Historical `@1.2.0/@1.3.0` remain addressable | streaming tools; beta strict tools; live multi-turn/operation conformance; prefix beta; full Responses/Anthropic operations; Files/FIM; separate vision model qualification |
| DeepSeek V4 Pro | Chat, native Responses and Anthropic surfaces; thinking toggle/effort; tools; JSON; prefix completion; FIM; Files; KV cache | current `deepseek.v4-pro.api@1.4.0`; its own immutable reasoning identity models `low/high/max` with `medium/xhigh -> high`, while reviewed standard Chat v4 provides non-stream omitted/none/auto/required/named function choice, one Plan declared/selected-function authority, exact response/history consistency and conservative active-tools reasoning replay; profile-bound cache hit/miss usage and 1M/384K provider limits remain. Historical `@1.2.0/@1.3.0` are addressable | tool streaming; beta strict tools; live multi-turn conformance; beta/native/Files/FIM operations; pricing, billing and Release remain open |
| Qwen3.8-Max | DashScope CN Beijing OpenAI-compatible Chat; 1M context; 131,072 maximum output in normal and thinking modes; thinking defaults and controls; preserved thinking; tools/tool stream; structured output; image/video; cache; Beijing search | `qwen.3.8-max.api@1.8.0`; typed reasoning/preserve contract; both output fields bounded at 131,072; provider extensions pass complete admission/prepare/payload paths; inherited tools, structured-output, cache, inline/HTTPS media and search request contracts remain exact-profile gated; old `@1.7.0` remains addressable with its historical reasoning v1 | live provider validation; region-specific identities beyond Beijing; search/cache pricing and live meter evidence; Release |
| MiniMax M2.5/M2.7/M3 | M2.x 204,800 total window; M3 1M and image/video input; M3 adaptive/disabled thinking; M2.x required thinking; tools and automatic nested cache usage; M3 Chat supports standard/priority tier; Token Plan is quota/subscription access | M2.5-highspeed and M2.7 current `@1.5.0`, plus M3 current `@1.7.0`, all carry exact non-stream function request/history/response contracts—with provider-default auto but no explicit tool-choice/parallel wire claim—on top of exact automatic cache-read and reasoning contracts; M3 also retains bounded inline-JPEG and standard/priority tier support. Cache/vision/tier/reasoning/tool invocation origins remain distinct in one Plan snapshot; exact tokenizer only for M2.7; MiniMax response-envelope normalization is fail-closed. M3's four official CNY tier/context bands are projected as review-only evidence without flattening them into a Rate Card | M3 video, remote/other image formats and full media limits; the existing generic video validator is not sufficient for executable M3 support; streaming tools and explicit tool-choice/parallel controls remain unsupported by this exact surface contract; tier/context-band Rate Card selection, an approved public CNY CustomerChargeQuote and actual delivered-tier evidence are absent; M2.x/M3 split reasoning remains rejected pending a consistent exact replay contract; Token Plan quota evidence. Customer wallet cost remains zero until an approved public CNY price contract exists |
| Gemini 3.7 Flash | OpenAI-compatible Chat supports Standard, Flex and Priority tiers; Flex has long latency and no automatic Standard fallback; Priority may report Standard delivery | `gemini.3.7-flash.api@1.7.0`; core Chat subset plus typed request/effective/observed tier evidence, pre-Freeze cost-observation gate and bounded Flex timeout chain | tier-aware CNY CustomerChargeQuote; native USD supplier cost; live tier response validation; Release; native Gemini cache/search/media surfaces |
| Gemini 3.5 Flash | OpenAI-compatible Chat supports Standard, Flex and Priority tiers; model supports 1M input and 65,536 maximum output | `gemini.3.5-flash.api@1.7.0`; reuses the reviewed Gemini OpenAI-compatibility tier policy and shared runtime/evidence boundary | tier-aware CNY CustomerChargeQuote; native USD supplier cost; live tier response validation; Release; native Gemini cache/search/media surfaces |
| Gemini 3.5 Flash-Lite | OpenAI-compatible Chat supports Standard, Flex and Priority tiers; model supports 1M input and 65,536 maximum output | `gemini.3.5-flash-lite.api@1.7.0`; reuses the reviewed Gemini OpenAI-compatibility tier policy and shared runtime/evidence boundary | tier-aware CNY CustomerChargeQuote; native USD supplier cost; live tier response validation; Release; native Gemini cache/search/media surfaces |
| GPT-5.6 Sol | OpenAI documents both `v1/chat/completions` and `v1/responses`; Responses carries the richer built-in-tool surface; `reasoning.effort` supports `none/low/medium/high/xhigh/max` and defaults to `medium` | `openai.gpt-5.6-sol.chat@1.0.0` is the current locally-conformant core Chat identity; current `openai.gpt-5.6-sol.responses@1.3.0` composes strict stateless core text, profile-bound `reasoning.effort` and exact text SSE through `unirouter.openai.responses.text.v2@1.0.0`; stream order/sequence/text/usage/model privacy, output guard, pre-byte failover and delivery/settlement fences fail closed; the 272K executable/default context cap remains distinct from the visible 1.05M provider maximum; `.responses@1.0.0/@1.1.0/@1.2.0` and mixed `.api` versions remain historical and addressable | Responses reasoning summary and multi-turn encrypted-state replay; reasoning/tool/structured-output streams, built-in tools, service tier and broader response/evidence contracts; >272K pricing band; native USD supplier cost; live validation; Release |
| GPT-5.6 Terra | OpenAI documents both `v1/chat/completions` and `v1/responses`; provider maximum is 1.05M context and 128K output; `reasoning.effort` supports `none/low/medium/high/xhigh/max` and defaults to `medium` | `openai.gpt-5.6-terra.chat@1.0.0` remains the current locally-conformant core Chat identity; current `.responses@1.3.0` composes strict stateless core text, Terra-specific `reasoning.effort` and exact text SSE through `unirouter.openai.responses.text.v2@1.0.0`; stream order/sequence/text/usage/model privacy, output guard, pre-byte failover and delivery/settlement fences fail closed; historical `.responses@1.0.0/@1.1.0/@1.2.0` remain addressable, with `@1.2.0` retaining the exact non-stream reasoning contract; the 272K executable/default context cap remains distinct from the visible 1.05M provider maximum; mixed `.api` versions remain historical and addressable | Responses reasoning summary and multi-turn encrypted-state replay; reasoning/tool/structured-output streams, built-in tools, service tier and broader response/evidence contracts; >272K pricing band; native USD supplier cost; live validation; Release |
| GPT-5.6 Luna | OpenAI documents both `v1/chat/completions` and `v1/responses`; provider limits are 1.05M context, 922K maximum input and 128K output; `reasoning.effort` supports `none/low/medium/high/xhigh/max` and defaults to `medium` | `openai.gpt-5.6-luna.chat@1.0.0` is the current locally-conformant core Chat identity; current `.responses@1.3.0` composes strict stateless core text, Luna-specific `reasoning.effort` and exact text SSE through `unirouter.openai.responses.text.v2@1.0.0`; stream order/sequence/text/usage/model privacy, output guard, pre-byte failover and delivery/settlement fences fail closed; historical `.responses@1.0.0/@1.1.0/@1.2.0` remain addressable, with `@1.2.0` retaining the exact non-stream reasoning contract; the 272K executable/default context cap remains distinct from the visible 1.05M provider maximum; mixed `.api` versions remain historical and addressable | Responses reasoning summary and multi-turn encrypted-state replay; reasoning/tool/structured-output streams, built-in tools, service tier and broader response/evidence contracts; >272K pricing band; native USD supplier cost; live validation; Release |
| Claude Opus 5 | Anthropic Messages; 1M context and 128K maximum output; adaptive thinking defaults on; effort defaults to `high` and supports `low|medium|high|xhigh|max`; disabled thinking is incompatible with `xhigh|max` | current `@1.2.0` exact text/reasoning plus strict text SSE subset; typed controls and combination constraint; provider-default origins; omitted-thinking signature; inclusive final usage; refusal and response-only inference geo/service-tier evidence; exact stream lifecycle, model/error sanitization, output guard and delivery/settlement fences; historical `@1.0.0/@1.1.0` remain addressable | tools and structured-content/tool streaming; media; prompt caching; beta extensions; request service tier; native USD supplier cost; customer billing; PG-capable integration execution; live validation; Release |
| Claude Sonnet 5 | Anthropic Messages; 1M context and 128K maximum synchronous output; adaptive thinking is on by default and can be disabled; `output_config.effort` defaults to `high`; omitted thinking still returns an opaque signature when the model thinks; refusal is a successful response with typed stop details | current `@1.4.0` exact text/reasoning and strict text SSE plus non-stream user-defined function tools; typed `auto/none/any/named` tool choice, provider-default parallel use, exact declared-tool/history/response identity and fail-closed required/named response consistency; one normalized Plan snapshot preserves invocation origins for route/Freeze/attempt; historical `@1.0.0` through `@1.3.0` remain addressable | server tools, tool streaming and structured tool content; media; prompt caching; beta/MCP extensions; request service tier; native USD supplier cost; customer billing; executable SQL integration evidence; live validation; Release |
| Claude Haiku 4.5 | Anthropic Messages; pinned `claude-haiku-4-5-20251001`; 200K context and 64K maximum output; manual extended thinking with `budget_tokens` and summarized output by default; adaptive thinking and interleaved thinking are unsupported | current `@1.2.0` exact non-stream text/reasoning plus strict summarized-thinking/text SSE subset; omitted/default-disabled and explicit enabled/disabled semantics; budget 1,024–63,999 and strictly below `max_tokens`; visible summaries cross the output guard but signed thinking cannot be transformed; signature and `redacted_thinking.data` remain opaque; inclusive final usage and response-only inference geo/service-tier evidence; exact stream order/model privacy/delivery/settlement fences; historical `@1.0.0/@1.1.0` remain addressable | request `display`; tools/multi-turn thinking replay; media; prompt caching; structured content and beta extensions; request service tier; native USD supplier cost; customer billing; PG-capable integration execution; live validation; Release |
| Doubao Seed 2.1 Pro | Ark Beijing OpenAI-compatible Chat; pinned `doubao-seed-2-1-pro-260628`; 262,144 context/output model maxima; Chat `max_tokens` defaults to 4,096; thinking defaults enabled and returns summarized plus encrypted reasoning evidence | current `@1.6.0` exact text/reasoning streaming core plus non-stream function tools, non-stream `strict=true` JSON Schema, mandatory implicit-cache read usage and bounded non-stream inline-JPEG user-image input; exact `/api/v3` endpoint class; separate `max_tokens` 262,144 and `max_completion_tokens` 65,536 wire ceilings; typed enabled/disabled thinking; `include_usage=true` normalization; exact content/reasoning/opaque stream phases, terminal usage and `[DONE]`; typed none/auto/required/named tool choice, provider-default parallel calls, exact tool history/response and encrypted reasoning replay; the one Plan capability snapshot carries declared/named choice constraints and gates live/cache responses before delivery; vision invocation plus always-required cacheRead evidence uses that same authority; historical `@1.0.0` through `@1.5.0` remain addressable | reasoning effort; tool stream and strict function schema; JSON Object/non-strict/streaming structured output; remote/other image forms, video and audio; explicit cache creation/storage pricing; search usage and pricing; service tier; paid/live validation; Release |
| Doubao Seed 2.1 Turbo | Ark Beijing OpenAI-compatible Chat; pinned `doubao-seed-2-1-turbo-260628`; 262,144 context/output model maxima; Chat `max_tokens` defaults to 4,096; `reasoning_effort` defaults to `high` and `minimal` disables thinking | current `@1.6.0` exact text/reasoning-effort streaming core plus non-stream function tools, non-stream `strict=true` JSON Schema, mandatory implicit-cache read usage and bounded non-stream inline-JPEG user-image input; exact `/api/v3` endpoint class; separate `max_tokens` 262,144 and `max_completion_tokens` 65,536 wire ceilings; typed `minimal|low|medium|high`; `include_usage=true` normalization; exact content/reasoning/opaque stream phases, terminal usage and `[DONE]`; typed none/auto/required/named tool choice, provider-default parallel calls, exact tool history/response and encrypted reasoning replay; vision invocation plus always-required cacheRead evidence in one Plan capability snapshot; historical `@1.0.0` through `@1.5.0` remain addressable | thinking-type control; tool stream and strict function schema; JSON Object/non-strict/streaming structured output; remote/other image forms, video and audio; explicit cache creation/storage pricing; search usage and pricing; service tier; paid/live validation; Release |
| Doubao Seed Evolving | Ark Beijing Chat/Responses/Tokenization; floating `doubao-seed-evolving` alias; provider identity may evolve without a pinned version | current `@1.8.0` exact text/reasoning streaming Chat core plus non-stream function tools, non-stream `strict=true` JSON Schema, a bounded non-stream inline-JPEG user-image subset and exact implicit-cache read usage; floating family/requested alias policy and Ark endpoint; exact output aliases/ceilings; typed mutually-exclusive thinking/effort controls and defaults; ordinary reasoning and opaque signature tool-history replay; exact content/reasoning/opaque stream phases; injected terminal-usage request; separate terminal usage plus `[DONE]`; typed tool/strict-schema/vision invocation origins plus always-required prompt-cache/cache-read evidence in one Plan capability snapshot; request-bound admission shadow; exact non-stream tool/JSON responses and fail-closed refusal/truncation/transform handling; final non-stream and terminal-stream usage require `prompt_tokens_details.cached_tokens`, including authoritative zero; historical `@1.0.0` through `@1.7.0` remain addressable; probe `reported_model` stays evidence | real-attempt observed-model receipt; streaming tool deltas; JSON Object/non-strict/streaming structured output; remote/other image formats and provider 10 MB parity; explicit cache creation/storage pricing; search capabilities and pricing; paid/live validation; Release |

Official references used for this snapshot:

- [Volcengine Ark Chat Completions request, stream and usage contract](https://docs.volcengine.com/docs/82379/1494384?lang=zh)
- [Doubao Seed Evolving / Seed 2.1 deep-thinking controls and response evidence](https://docs.volcengine.com/docs/82379/1449737?lang=zh)
- [Volcengine Ark Function Calling, tool choice and parallel-call contract](https://docs.volcengine.com/docs/82379/1262342?lang=zh)
- [Volcengine Ark correct agent replay and encrypted-thinking rules](https://docs.volcengine.com/docs/82379/2636748?lang=zh)
- [Doubao Seed 2.1 Turbo exact model limits and reasoning-effort values](https://ark.volcengine.com/region:cn-beijing/model/detail?Id=doubao-seed-2-1-turbo&Version=260628)
- [Doubao Seed Evolving official current-model guidance](https://docs.volcengine.com/docs/82379/2549861?lang=zh)
- [Doubao structured output beta guide](https://docs.volcengine.com/docs/82379/1568221?lang=zh)
- [Volcengine Ark context caching guide](https://docs.volcengine.com/docs/82379/1398933?lang=zh)
- [Volcengine Ark product table with Turbo context-cache service](https://www.volcengine.com/product/ark)
- [Volcengine Ark image understanding contract](https://docs.volcengine.com/docs/82379/1362931?lang=zh)
- [Doubao Chat API response format contract](https://docs.volcengine.com/docs/82379/1494384?lang=zh)

- [Qwen3.8-Max model limits, regional capabilities and RMB prices](https://help.aliyun.com/zh/model-studio/qwen3-8-max)
- [DashScope OpenAI-compatible Chat parameters](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)

- [OpenAI GPT-5.6 Sol model endpoints and capabilities](https://developers.openai.com/api/docs/models/gpt-5.6-sol)
- [OpenAI GPT-5.6 Terra model endpoints and capabilities](https://developers.openai.com/api/docs/models/gpt-5.6-terra)
- [OpenAI GPT-5.6 Luna model endpoints and capabilities](https://developers.openai.com/api/docs/models/gpt-5.6-luna)
- [OpenAI reasoning controls and stateless reasoning preservation](https://developers.openai.com/api/docs/guides/reasoning)
- [Claude Sonnet 5 model changes and limits](https://platform.claude.com/docs/en/about-claude/models/whats-new-sonnet-5)
- [Claude Opus 5 model changes and limits](https://platform.claude.com/docs/en/about-claude/models/whats-new-claude-4-6)
- [Anthropic Messages request and response schema](https://platform.claude.com/docs/en/api/messages/create)
- [Anthropic thinking controls](https://platform.claude.com/docs/en/build-with-claude/extended-thinking)
- [Anthropic effort controls](https://platform.claude.com/docs/en/build-with-claude/effort)
- [Anthropic Messages stop reasons and refusal details](https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons)

- [Kimi K2.6 pricing and capability summary](https://platform.kimi.com/docs/pricing/chat-k26)
- [Kimi token estimate API](https://platform.kimi.com/docs/api/estimate) and
  [frozen K2.6 tokenizer source](https://huggingface.co/moonshotai/Kimi-K2.6/tree/7eb5002f6aadc958aed6a9177b7ed26bb94011bb)
- [Kimi K2.7 Code pricing and capability summary](https://platform.kimi.com/docs/pricing/chat-k27-code)
- [Kimi model-specific parameter constraints](https://platform.kimi.com/docs/api/models-overview)
- [Kimi K3 pricing](https://platform.kimi.com/docs/pricing/chat-k3) and
  [reasoning effort](https://platform.kimi.com/docs/guide/use-reasoning-effort)
- [Kimi K3 Dynamic Tools](https://platform.kimi.com/docs/guide/use-dynamic-tool-loading)
  and [request-level tool choice](https://platform.kimi.com/docs/guide/use-tool-choice)
- [Kimi context caching](https://platform.kimi.com/docs/guide/use-context-caching-feature-of-kimi-api),
  [Partial Mode](https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api) and
  [ToolCalls](https://platform.kimi.com/docs/guide/use-kimi-api-to-complete-tool-calls)
- [Kimi built-in web search](https://platform.kimi.com/docs/guide/use-web-search) and
  [tool pricing](https://platform.kimi.com/docs/pricing/tools)
- [DeepSeek thinking](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode),
  [vision](https://api-docs.deepseek.com/zh-cn/guides/vision),
  [Responses](https://api-docs.deepseek.com/zh-cn/guides/responses_api),
  [Anthropic](https://api-docs.deepseek.com/zh-cn/guides/anthropic_api),
  [FIM](https://api-docs.deepseek.com/zh-cn/guides/fim_completion) and
  [Files](https://api-docs.deepseek.com/zh-cn/guides/files_api)
- [MiniMax API overview](https://platform.minimaxi.com/docs/api-reference/api-overview),
  [OpenAI Chat contract](https://platform.minimaxi.com/docs/api-reference/text-chat-openai),
  [passive prompt caching](https://platform.minimaxi.com/docs/api-reference/text-prompt-caching) and
  [Token Plan](https://platform.minimaxi.com/docs/token-plan/intro)

## 5. Current batch closeout order

1. Resolve B1 migration/optional-subset inclusion in Baseline §5. Preserve WIP
   while a decision is pending; no provider document can supply that decision.
2. Reconcile exact-version current facts, finite batch scope and onboarding
   wording. Fix only confirmed blockers and their affected tests.
3. Validate retained schema changes on an isolated database with supported
   predecessor/rollback/reapply and negative-evidence cases.
4. Close one bounded independent review and the stable-diff integration gate;
   then one authorized push and one CI. Record exact evidence rather than
   treating earlier local milestones as full-batch acceptance.
5. Stop and deliver B1 with explicit remaining Mission gaps. A next capability
   batch, Target deployment or paid test matrix requires a separate boundary.

Dynamic tool loading is now supported as an exact K3 provider contract; it is
not a cross-provider dynamic-tool engine or agent runtime. Provider built-in
search remains distinguishable from agent or MCP tools because execution
authority, enterprise policy and pricing are different.

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
