# Model Execution Profile And Capability Status

> Status: accepted architecture direction and current implementation gap inventory.
> Snapshot date: 2026-08-29.
> This document is not a claim that every provider-documented feature is production supported.

The current product-scope candidate is
[Model Adaptation Required Baseline V1 — Draft Candidate](44-model-adaptation-required-baseline-v1.md).
It is not yet an immutable approved Baseline. This document and the runtime
authorities project what the repository can currently do.

For development/test Target identity, the final 1.2.12 snapshot below
supersedes the older 1.2.10 Target block still present in the release-bearing
System Fact Map. Repository policy requires any Fact Map edit to ship with a
new artifact version; synchronizing that projection is therefore deferred to
the next authorized release-bearing batch rather than manufacturing a doc-only
1.2.13 artifact. Runtime and deployment evidence remain the current authority.

## Completed delivery batch — DeepSeek V4 Pro Chat Core Target closeout

The completed source batch is `DeepSeek V4 Pro × OpenAI-compatible Chat × Core
Conformance`. It is limited to the current `deepseek.v4-pro.api@1.5.0` Profile,
the exact `protocol.deepseek-openai-chat.v4@1.0.0` operation tuple and nine
independent capability qualifications: streaming, reasoning, reasoning effort,
tools, function tools, tool choice auto/named/required and JSON Object. M94
publishes only those exact bindings; it does not create a generic tuple,
Approval, Release, Alias, pricing or traffic authority.

The batch first removes onboarding's dependency on capability qualification for
preferred protocol selection. An explicit, append-only model-profile mapping
owns the immutable Chat operation-binding V1 identity; absence of an override
preserves only the independently validated server-owned base operation, while
invalid or ambiguous typed declarations fail closed. Capability evidence,
catalog evidence and Chat-contract registration cannot create this identity.

Within the delivery batch, non-streaming text, cache-read/usage mapping and the
UI/fixed-SDK closeout remain distinct evidence checkpoints rather than new
routing Approval types. One Active Milestone is advanced at a time. Grouped
capability approval remains `valid_deferred` with
`design_status=candidate`, `runtime_authority=none`,
`requirement_baseline=none` and explicit future product approval required; it is
not part of this batch.

### Final Target snapshot — 1.2.12 / M94 / Pro exact tuple

As of 2026-08-29, development/test stable, canary and frontend run the exact
main merge commit `7748346e3f9e0862de7bb085fbf80904c88f05c1`, tree
`2f9e258e8e63a5bc433673fbbfc4c87646bf2d55`, version 1.2.12. Stable and
canary use backend image
`sha256:fe860a88c9b7b0803e5412056d9c89b70880e68f0b592ff2c8be8679b129ad16`;
frontend uses
`sha256:cf255bf1bf45eb948a5e3b8531bd8613367c85eee392ceedf15c2b3007d72ea0`.
Both public and local readiness return database and Redis healthy. The database
has 93 ordered migration rows ending at
`000094_deepseek_v4_pro_chat_core_qualification`; 1.2.12 did not change schema.
The release worktree is clean at that exact commit/tree. Runtime identity now
reports 1.2.12/`7748346e...`, while the pre-existing environment remains
canonical route authority with compatibility disabled and async media disabled.
The protected pre-upgrade backup remains on Target under the deployment-evidence
directory; its private contents are not part of this document.

PR #340 (CI run 33212334966), PR #341 (33216249020) and the final operation-
evidence correction PR #342 (33229132370) each passed all five required jobs
before merge. The extra diffs were live-found, independently gated corrections:
they do not support a claim that the batch used only one push or one CI run.
The supported customer-authority repair PR #343 passed all five jobs in CI run
33235124887 and merged as `acc2fe7c75e2644328b4e2eb8d9b1e9071e037e0`.
The final Sponsor-diagnostic UI correction PR #344 passed all five jobs in CI
run 33240368418 and merged as the deployed `7748346e...` artifact. The latter
changes only the operator diagnosis and does not alter sponsorship, Grants,
routing or provider traffic.
The final frontend no longer owns a DeepSeek Profile/protocol allowlist;
`capability_probe_contracts` is a server-owned exact-operation projection, and
the UI renders only the returned contracts. This removes the Pro-specific UI
authority duplication rather than deferring it to K2.6.

Deployment 3 is bound to `deepseek.v4-pro.api@1.5.0 ×
protocol.deepseek-openai-chat.v4@1.0.0 × openai.chat_completions.v1`. Exact
operation probe 3 succeeded against provider-reported `deepseek-v4-pro` and
advanced the operation revision to 2. The nine Profile-scoped capability probes
were then rerun and separately approved under that revision: streaming
(approval 33), reasoning (34), reasoning effort (35), tools (36), function
tools (37), tool choice auto/named/required (38/39/40) and JSON Object (41).
All nine receipt-aware qualifications are `qualified`; no grouped command was
used. UI evidence includes valid SSE termination/final usage, reasoning and
effort semantics, tool calls, the three tool-choice variants and JSON output.

The UI also authored, sealed and initially activated Release 24 for the exact
Pro tuple. Authoring receipt
`rcr_deployment_20260829t035153_e25908a1c4fbf97dbb541745` binds route hash
`sha256:c6496aac675f5756c8a7a5f55abc053dbecb2e9efca8ab67f9090226ed47763e`;
lifecycle receipt 3 moved the model-local slot from 0 to 1. This does not change
the platform-wide route authority, enable compatibility, publish pricing or
constitute a Production release. The pre-existing runtime configuration remains
canonical route authority with compatibility disabled and async media disabled.

This Pro batch used its approved maximum 20 supplier calls: nine
earlier capability probes on the superseded operation revision, then the exact
operation probe, nine current capability probes above and one final
`openai-python@3.5.0` public workflow. That last request
`op_2f4c76e0702b47944c602b53cc8f0e6b` / attempt
`att_490a6e4dcea569533578966cac41fd81` returned HTTP 200 and `PRO_SDK_OK`,
with provider-reported final usage of 15 input, zero cache-read and 4 output
tokens. It bound deployment 3, the exact v4 protocol identity and Release 24.
Its billing mode was `cost_observation`; Freeze, calculated customer cost,
charged customer cost and wallet movement were all zero. The CNY 5 value remains
an authorized supplier-spend ceiling, not a supplier invoice or native-cost
Observation.

The SDK workload Application was then archived through the supported UI. Grants
12/13 are revoked, task-scoped Keys 88/89/90 are disabled, and user 40 is
disabled/offboarded with no project membership or live session. The first
offboarding attempt correctly failed and rolled back atomically while active
sponsorship remained; archiving removed that blocker without reassigning a
Sponsor or creating replacement access. The wallet remains CNY with zero
balance, grant, freeze and cumulative spend. Browser readback on the deployed
frontend shows V1.2.12, the exact Pro Profile/output ceiling and all nine
server-projected capability sections. No supplier call was made during the
1.2.12 deployment or this final readback.

The deployment used an incremental Git bundle against the actual Git common
object store of the active Target release; a full-history bundle was not used.
The supported retention workflow removed exactly 21 authorized historical
images and BuildKit cache older than 168 hours without removing containers,
volumes, database state, backups or release directories. The deployed 1.2.12
images and release are now explicitly retained. This exact Pro Chat Core subset
is `canary_live_verified` on development/test; pricing activation, customer
billing, Production and broader provider parity remain unclaimed.

## Active delivery batch — Kimi K2.6 Chat

The next Active Milestone is `Kimi K2.6 × OpenAI-compatible Chat ×
non-streaming text`. The Kimi series in the current inventory is K2.6,
K2.7 Code and K3; K2.7 does not denote a separate general-chat model. The
authorized K2.6 live budget is at most 20 supplier calls and CNY 5, but no call
may occur until the exact matrix is frozen and the user enters the prepared Kimi
Open Platform credential once through the supported UI.

The current local source candidate is version 1.2.13 with schema ceiling M95.
It prepares one finite K2.6 Chat Core Delivery Batch: an exact Profile-scoped
operation-protocol binding plus typed probe contracts for streaming, reasoning,
tools, function tools, automatic tool choice and JSON Object. Those contracts
and their local tests are batch preparation, not live evidence and not six
prematurely closed milestones. M95 creates only declared projections for the
exact `kimi.k2.6.api@1.7.0 × protocol.kimi-openai-chat.v5@1.0.0` tuple; it does
not bind K2.7/K3, create a generic tuple, call Kimi, approve routing, change a
Release/Alias or activate pricing. The development/test Target remains the
1.2.12/M94 snapshot until an exact artifact is gated and deployed.

The Kimi probe validators and the Gateway delivery guard currently remain two
parsing implementations. Cross-delivery fixtures are required for this batch,
and public UI/SDK delivery remains necessary evidence; a successful Probe alone
must not be described as complete delivery proof. This is a bounded architecture
warning rather than a second runtime authority. Before any next provider adds a
third provider-specific full JSON/SSE envelope parser, the shared
OpenAI-compatible delivery-shape boundary must be re-evaluated. If a Kimi Probe
succeeds while the same exact tuple is withheld by public delivery shape, the
warning becomes a blocker immediately. No shared parser refactor is authorized
merely by this checkpoint.

K2.6 then advances one milestone at a time through streaming, thinking, tools,
JSON and usage/cache-read mapping. Non-stream latency evidence records gateway
stages, connection establishment or reuse, response headers, full provider
response and downstream completion; it does not report first-token or SSE
metrics. Streaming separately records first content event, downstream first
content, inter-chunk gaps and completion, excluding headers and keepalive
comments from first-token claims. Cold and reused connections stay distinct,
and a small live sample reports individual observations rather than P95/P99.
Direct-provider comparison, if needed, must use the same region, model, prompt,
connection temperature and streaming mode. Contract-preserving fast-path work
is permitted only after measurements identify stable UniRouter-added latency or
buffering; it is not part of this milestone by default.

## Historical delivery snapshot — DeepSeek Flash Chat completion

> Retained evidence from the Flash closeout. It does not describe the current
> development/test artifact, which is the final 1.2.12 Target snapshot above.

At that 2026-08-29 checkpoint, the exact reviewed artifact was commit
`18c7153fbdc0b1a9ed2b1d4898893f0dc687dd47`, tree
`5721a8488c68ff4d596a4dd5ad3c57e3f639f400`, version 1.2.7 and schema ceiling
`000093_deepseek_function_tool_probe_contract_v2`. Automatic CI run
33187407637 and protocol-conformance run 33187407720 both passed on that exact
commit. Development/test stable, canary and frontend then ran images built from
`18c7153fbdc0`; both backends were healthy. The database had 92 ordered migration
rows with M93 as its maximum. Canonical model-route authority was enabled
with compatibility disabled; this batch did not change the Source Instance,
public-model identity, Profile, protocol, sealed Release 23, Alias, pricing or
Production traffic.

The actual routable identity is deployment 2,
`deepseek.v4-flash.api@1.5.0 × openai.chat_completions@1.0.0`, public model
`deepseek-v4-flash-20260828`. After M93 advanced only the function-tool probe
interpretation contract to v2, Admin UI issued and separately approved five
typed probes. Current receipt-aware qualification is `qualified` for:

- `tools`: probe 21 / approval 19;
- `function_tools`: probe 22 / approval 20;
- `tool_choice_auto`: probe 23 / approval 21;
- `tool_choice_named`: probe 24 / approval 22;
- `tool_choice_required`: probe 25 / approval 23.

The ordinary-user Developer Console then produced three exact public-call
checkpoints on deployment 2 / Release 23:

- `op_dfd4ce1c4f59a2cfb2a4a8ebbdc3415a`: non-stream function tool, HTTP 200,
  provider-reported final usage 388 input / 256 cache-read / 76 output; the
  canonical response removed the non-authoritative child tool-call `index`;
- `op_4e2d9bacc2a62a58a00e92f01f840f03`: streaming with an explicit output
  limit, provider HTTP 200 followed by the correctly surfaced terminal
  `stream_error.output_limit_exceeded`; partial usage remains
  `gateway_counter/estimated`, not a successful final usage claim;
- `op_610602dbacefbcef39b03ce62da5a0cb`: streaming with
  `thinking=disabled`, HTTP 200 and provider-reported final usage 28 input / 12
  output.

A fixed `openai-python@3.5.0` workflow issued exactly three additional requests:

- `op_8d8a3a25a0b6457751f17d90bf30ce4c`: function tool, child `index` absent,
  reasoning preserved for replay, provider-reported final usage 384 input / 256
  cache-read / 71 output;
- `op_63b8117120eded7cec53d6275c1e465b`: tool-result replay returned
  `ROUNDTRIP_OK`, with provider-reported final usage 106 input / 5 output;
- `op_e5aff789d82e1076dc49b6c489422aa7`: SSE returned `STREAM_SDK_OK`, with
  provider-reported final usage 12 input / 5 output.

This M93 closure sub-batch therefore has an explainable total of 11 supplier
calls: five Probe v2 calls plus the six public UI/SDK requests listed above. It
is not a standing call-count ceiling and is not a claim about the whole Flash
history. The previously approved call-count ceiling was removed; the remaining
authorization boundary is a cumulative CNY 5 supplier-spend ceiling. Native
supplier cost still lacks invoice or supplier-reported Observation authority,
so neither the repository nor this snapshot claims to have precisely measured
actual supplier spend. Customer 2's CNY wallet remains zero for balance, grant,
freeze and cumulative spend, with zero customer financial transactions.

The two task-scoped keys `ui-flash-v127-final-20260829` and
`ui-flash-v126-b-20260828` are disabled. The Developer Console credential field,
local clipboard and temporary fixed-SDK environment were cleared. This exact
DeepSeek Flash Chat subset is `canary_live_verified` on development/test; it
does not promote another model, prove full provider parity, constitute customer
manual acceptance or change `release_state`.

There are **30 upstream model entries and 33 current surface identities**.
The table is a code projection of exact new-attachment pointers, not a Baseline
approval or proof that each model satisfies the full capability matrix.
Recorded local milestones cover bounded subsets. Local/CI, the historical
preflight and B2 Target acceptance are recorded separately below; supplier-live
and Release claims still require their own exact identity/evidence.

<!-- current-profile-inventory:start -->
| Current exact Profile | Surface |
|---|---|
| `deepseek.v4-flash.api@1.5.0` | Chat |
| `deepseek.v4-pro.api@1.5.0` | Chat |
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

### Historical DeepSeek Flash M92/local-M93 checkpoint — superseded

> Historical checkpoint from 2026-08-28. Present-tense statements in this
> subsection describe that checkpoint only and are superseded by the
> 2026-08-29 completion snapshot above.

At that checkpoint, the finite batch remained one model family, one surface,
one operation and the existing exact attachment identity
`deepseek.v4-flash.api@1.5.0 × OpenAI-compatible Chat Completions`. It does not
authorize another model, surface, Production, customer settlement or a
platform-wide canonical switch. Development/test then ran exact
commit `c4ee78fc3eb41f144e72000d01917f41d7470bfb`, version 1.2.6 and schema
M92. Public model 697 (`deepseek-v4-flash-20260828`) and sealed active Release
23 bind immutable Profile `deepseek.v4-flash.api@1.5.0`; the active organization
Alias `deepseek-v4-flash` currently targets that identity. M93 changes only the
current function-tool probe-contract version for this existing identity; Target
M92 evidence cannot qualify that new v2 interpretation contract.

The ordinary test account was user 39 in customer 2 / Default Project 86. A
replacement model-restricted test key remained active only for this approved
test batch; its secret was not read or recorded. The product owner removed the
call-count ceiling for this Flash closure while retaining a cumulative CNY 5
supplier-spend ceiling.
Eighteen supplier calls preceded that decision. Calls remain short and typed;
there is no automatic retry or unbounded failover. The following calls 6 and 7 are retained as earlier
successful UI checkpoints:

- Supplier call 6: non-empty SSE stream with explicit `thinking=disabled`.
  Request `op_ce3c68c4be82840191e6bbd7f4dfa30e` returned HTTP 200,
  `STREAM_OK`, terminal `finish_reason=stop`, provider-reported final usage
  10 input + 3 output = 13 tokens, one canonical attempt and zero customer
  charge. The wallet remained zero. This closes only the exact deployed
  streaming slice as `canary_live_verified`.
- Supplier call 7: non-stream explicit `max_tokens=4` with
  `thinking=disabled`. Request `op_3ff9153305c46ab1cc3a52eb6f865446`
  returned HTTP 200, `OK`, `finish_reason=stop` and provider-reported final
  usage 8 + 1 = 9 tokens. A UI request with `max_tokens=384001` was rejected
  before provider attempt, Freeze or call-log creation. This locally and live
  verifies the Profile's 384K ceiling boundary without claiming that 384K was
  generated.

The completed Profile 1.5 transition did not rewrite model 677 or sealed Release
22; model 697 and Release 23 are separate retained identities. The response
wire-compatibility correction below does not create another Profile, Protocol,
public model, Release or Alias transition. Historical v1 probe and approval
receipts remain addressable and are not reinterpreted.

The deployed explicit-output-limit stream correctly ended with an SSE error,
but the workbench displayed it as `(empty stream)`. The local candidate then
surfaces the terminal error code/message, request identity and billing status;
its real `ReadableStream` regression test passed. This UI correction was not yet
Target-verified at that checkpoint.

Later UI and typed-capability work reached calls 16–18. Call 16 verified the
deployed Profile 1.4 reasoning path. Call 17 exercised `tool_choice=auto`, but
the Developer Console's `max_tokens=64` fixture capped the result before the
expected function-call assertion, so it is not positive tools evidence. Call
18 sent a named choice while omitting the thinking control; because DeepSeek
defaults thinking to enabled, the provider rejected the combination. The local
Profile 1.4 contract had allowed that impossible composition, while individual
Admin tool probes had forced `thinking=disabled` and therefore could not reveal
the defect. No retry was made.

The deployed append-only correction created DeepSeek Flash and Pro Profile 1.5 with
reasoning contract v5. Under active/default thinking, explicit `required`,
named and `none` choices fail before Freeze or credential use; redundant
explicit `auto` with tools is normalized to omission, preserving the provider
default. Named/required choices remain valid when thinking is explicitly
disabled. Historical Profile 1.4 and all sealed evidence remain unchanged.
M92 adds only Flash 1.5's exact nine-capability probe bindings for the two
already-supported Chat protocol identities. Canonical onboarding now
materializes all exact migration-approved capabilities rather than streaming
alone. Empty-attachment M92 rollback is tested; any retained Flash/Pro Profile
1.5 model attachment, probe or approval blocks downgrade before mutation. These
Profile and M92 control-plane changes are deployed Target facts; individual
capabilities still require exact live evidence and approval.

The next live tool call exposed a separate delivery-shape defect: DeepSeek's
successful non-stream function-tool response included an SDK-compatible
per-tool integer `index`, while immutable protocol v4 rejected that field and
withheld the provider HTTP 200 response. The local correction preserves Profile
1.5, protocol v4, Release 23 and the Alias. One exact-scoped shared normalizer
accepts only a lexical non-negative decimal integer at
`choices[].message.tool_calls[].index`, deletes that non-authoritative child
field, preserves `choices[].index`, and leaves every unknown sibling for the v4
or retained generic exact-route guardrail to reject. Any residual invalid child
index also fails closed on either tuple. It performs no machine-integer conversion or undocumented
numeric bounding. Runtime delivery and function-tool Probe v2 consume this same
normalizer; the probe then checks its bounded semantic sentinel and usage.

M93 adds ten immutable v2 bindings: five existing function-tool capabilities
under each of Profile 1.5's two exact protocol identities. It changes the ten
current pointers from v1 to v2 without changing the total current-binding count.
Old v1 receipts remain historical facts and become `probe_contract_stale` for
new routing qualification. M93 rollback is blocked by retained v2 probes or
approvals and by stable projection-identity drift that would leave a dangling
v2 evidence reference, but not merely by the pre-existing unchanged Profile 1.5
attachment. At that checkpoint this was `locally_conformant` only; no M93 Target
migration, v2 probe/approval, supplier call, Release or Alias change had
occurred.

M90 and M91 are the deployed bounded continuation. M90 adds typed qualification
for explicit `thinking=enabled` and `reasoning_effort=low`; the validator records
only that non-empty reasoning evidence existed, never the private reasoning
text. M91 adds fixed no-side-effect function-tool probes for `tools`,
`function_tools` and auto/named/required tool choice, plus a fixed JSON Object
probe. The Admin UI reuses one two-stage probe/approval component for every
capability; the developer workbench exposes only fixed function-tool and JSON
scenarios rather than arbitrary provider JSON. Exact current and historical
Chat surface tuples are migration-owned; mismatched tuples stay absent and an
explicit `unsupported` decision is preserved.

At that checkpoint, focused local evidence had passed:

- typed validator and Admin handler tests, including private-data non-retention;
- isolated PostgreSQL M90/M91 exact projection, guarded down and reapply tests;
- DeepSeek Profile/routing/endpoint/gateway capability tests;
- capability panel and developer workbench suites, 39 tests;
- frontend type/API/react compatibility checks and `git diff --check`.

At that checkpoint, the remaining Profile 1.5 tools/tool-choice, JSON Object,
repeated-prefix cacheRead, fixed SDK workflow and local UI changes remained
independently scoped. No M93 Target migration, v2 typed live probe, v2 capability
approval, new ordinary public tool call, push or CI had occurred. M93 deliberately
requires a
temporary fail-closed tool window after deployment until the actual routable
Deployment is re-probed and approved; the UI must describe this as a probe
contract upgrade awaiting revalidation, not a provider capability regression.
Zero customer charge is not zero supplier cost; there is no call-count ceiling,
and the cumulative supplier-spend hard cap remains CNY 5.

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

### B1 CI follow-up — bounded frontend animation-lifecycle repair

Commit `a65afe97928e2c1702d801e5d09e71d26cea9f33` did not close B1:
[CI run 32948564489](https://github.com/foigent/unirouter/actions/runs/32948564489)
passed Backend Tests (including exact Neutral N1 qualification) and PR4 Browser
E2E, but Frontend Build failed the same two custom-provider selection tests
(620/622 passed). Docker stopped at its routed prerequisite check, not an image
build defect. The separate
[protocol run 32948564453](https://github.com/foigent/unirouter/actions/runs/32948564453)
passed. Earlier isolated frontend passes were insufficient integration evidence.

The product owner approved one bounded repair commit/push and CI verification.
The initial fixed-clock hypothesis was insufficient: allowing time to advance
still reproduced both failures in the full frontend suite. That candidate was
withdrawn. The actual Select popup uses CSSAnimation, enabling motion after
positioning; jsdom supplies no CSS `animationend`. Fast isolated interactions
could use the pre-positioning, non-animated path, while slower runs waited for a
close event before committing the controlled value. The helper now exercises
the positioned popup's enter/leave lifecycle explicitly, on the exact option's
dialog, and retains the original committed-selection assertion. No production
UI, model, Profile, clock, timeout, warning allowance or retry behavior changes
are included. With only enter completion supplied, both original tests failed
deterministically; supplying leave completion passed all 24 page tests and the
full 622-test frontend suite. Type checking, API/React guards, production build,
bundle budget and the unchanged warning ratchet passed. The bounded independent
source review found no blocker. The repair's CI outcome is recorded separately
below; no Target/live/release claim follows from the local results.

### B1 exact-commit CI closeout

Read back on 2026-08-26: [PR #339](https://github.com/foigent/unirouter/pull/339)
remains open, with head `ab4b5b3f42e6819d27ac946ad7899ac08498affb` and source tree
`91abc085c3df1927ed3444791deeb9403ebc7266`.
[CI 32951189399](https://github.com/foigent/unirouter/actions/runs/32951189399)
passed Backend Tests, Frontend Build, PR4 Browser E2E and Docker Build;
[protocol conformance 32951189406](https://github.com/foigent/unirouter/actions/runs/32951189406)
also passed. This closes B1 integration, not all 30 models' adaptation. The next
batch does not reopen the tested runtime diff or automatically trigger more CI.

### B2 read-only Target preflight — 2026-08-26, approximately 17:48 CST

Commands ran from the local workstation to the verified `unirouter-dev` SSH
target, 8.130.18.85. No Target writes or supplier calls were made.

| Item | Observed current state |
|---|---|
| Application | 1.2.2; stable/canary/frontend revision `4b0ca40040f011e06349ee66762a5dccccf387d2` |
| Release checkout | `/opt/unirouter/releases/4b0ca40040f011e06349ee66762a5dccccf387d2`; tree `5d0a4378356e4afd4ebedd4be37676aab037e224`; migration directory reports no Git changes |
| Backend image ID | Both replicas: `sha256:a86ce9968f089d6d3e593870006f038575237c59a2f9fae0c51a7b9d4750812a` |
| Frontend image ID | `sha256:ca55c8a96b7515000fda7f225c4683b426f9e35f195ad3f34b16f941afa1a7e8` |
| Database | Both replicas use the same database; ledger count 82, ceiling M83; installed `migrate status` reports no pending migration for the old artifact |
| Health | Stable/canary readiness: application, DB and Redis OK; frontend/Nginx containers running (not browser/TLS acceptance) |
| Routing/source | `legacy`, compatibility enabled, async media disabled; source instance `unirouter-test-reset-20260824-v3` on both replicas |
| Billing | Both billing environment overrides absent; installed exact-version config resolves defaults to `cost_observation` / `legacy`. This is configuration evidence, not proof of future request settlement or native-USD cost support |
| Baseline | One active admin, Default Project and CNY wallet; balance/grant/frozen/spent all zero. Models, connections, deployments, credential bindings, channels, API keys, rate cards, model prices, calls, transactions, attempts and Freeze records all zero |
| Audit | One `login` / `user` entry dated 2026-08-24; audit is no longer empty. Current browser session was not inspected |
| Host capacity | Docker/containerd filesystem: approximately 14 GiB available, 64% used; recheck before building |

Deployment prerequisites still open, not verified by the table:

- Explicit authority for build, server-local upgrade backup, maintenance
  and non-rolling M84–M87 migration/deployment. No `ab4b5b3f42e6` deployment images
  were listed on Target; the authorized build must match the pinned candidate
  commit/tree and record resulting image identities. PR merge is not a B2 step.
- All-entry drain and stop sequence. Stable's direct port 8080 and frontend's
  port 3000 exist in addition to Nginx; stopping Nginx alone is insufficient.
  Both backend containers have no explicit stop timeout. The application's
  120-second shutdown setting does not prove the container will wait that long.
- Verify deployed migration file bytes against the final source, not only image
  labels: both replicas bind-mount the release checkout's `migrations` into
  `/app/migrations:ro`. The startup ledger check verifies names, not SQL hashes.
- One migration owner, then complete exact-ledger readback before both new
  replicas start. The runbook's M84 section alone does not cover this M87 job.
- Server-local backup/recovery verification and an application-plus-database
  recovery decision before migration. USD drafts, new snapshot hashes and
  binding/probe/Release evidence can prevent guarded down migrations even with
  zero supplier calls. No reset backup was accessed; no recoverability claim is
  made from merely retaining old images.

The bounded independent deployment review's migration-sequence, shutdown-budget,
mount-identity and recovery findings are adjudicated `validated_required` for
B2 preflight. They do not authorize execution or reopen B1's runtime scope.

Only B2's read-only and unsaved-form acceptance is presently planned; persistent
test data must not silently enter the batch. SDK workflows and performance are
later bounded acceptance work, not B2 blockers. The existing chat k6 script
counts 429 as success, labels full request time as upstream latency and adds ten
tokens after JSON parsing failure; its old results cannot substantiate success,
upstream latency or token-throughput claims. Do not run it against paid providers
or expand B2 to repair it. Finance remains capability-dependent, with no global
billing-mode change, wallet activation, canonical switch or automatic next call.

### B2 authorized execution — historical build/preflight checkpoint

The product owner subsequently authorized the exact B2 maintenance deployment,
including server-local backup/recovery verification and zero-supplier-call
template acceptance. The preflight snapshot above is historical; authorization
is now satisfied, while artifact, drain, migration and acceptance gates still
need actual execution evidence. No PR merge or formal release is included.
Target lacks GitHub private-repository authentication, so source transfer uses
a verified standard Git bundle over SSH, without copying credentials. The exact
commit/tree and clean detached checkout remain the artifact authority.

Execution checkpoint on 2026-08-26, approximately 18:13 CST:

- Bundle SHA-256 `3fe88df13461074db0331f045c4b777566e418b8e5d4a9e0635fa5e264d4afa3`
  verified on both hosts; candidate commit/tree and detached checkout are exact.
- A new server-private M83 dump restored successfully into a separate rehearsal
  database. Ledger is 82/M83; admin/org/project non-sensitive whitelist fields
  and zero wallet values compare equal. No password/verifier query was run.
  The private dump and rehearsal database remain retained; the old reset backup
  was not accessed. Candidate up/down rehearsal still awaits the new binary.
- Candidate Compose preserves every existing application environment field;
  only version/commit and stable's explicit `SKIP_MIGRATION=true` differ.
  Both replicas receive a 150-second container stop budget. No existing service
  has been recreated. Candidate TLS configuration changes only `/v1/` transport
  timeouts to 1920/960 seconds; isolated `nginx -t` passed. A preceding Compose
  one-off hit the existing static-IP reservation; a no-published-port container
  verified the same configuration without that test-harness conflict.
- The official-proxy build stalled on `proxy.golang.org` connectivity and was
  canceled without an image. The default chain's direct-source alternative
  failed because the pinned Alpine builder contains no Git executable. Neither
  failure proves a model/runtime defect. Do not patch the pinned Dockerfile or
  disable checksum verification to bypass this prerequisite.
- The runbook-listed `https://goproxy.cn,direct` returned HTTP 200 for a locked
  dependency metadata request from Target. This is a reachability check, not a
  successful build. Its use requires the runbook's explicit proxy approval;
  no third build has started and no new image is claimed.
- Target remains on 1.2.2/M83; stable/canary are healthy, with zero calls,
  provider attempts and transactions. No maintenance stop or live migration
  occurred. Chrome still shows the existing admin/empty-model view after its
  refresh; post-upgrade UI/template acceptance remains pending.

The product owner then explicitly approved `https://goproxy.cn,direct` for this
build only. The exact candidate helper subsequently completed with that build
argument; runtime configuration, pinned Dockerfile and checksum verification
were unchanged. The following checkpoint supersedes the pending-state claims
above, without rewriting that earlier evidence.

### B2 Target deployment and bounded zero-call acceptance — 2026-08-26

At the B2 checkpoint, development/test ran **1.2.3 / `ab4b5b3f42e6819d27ac946ad7899ac08498affb`**,
tree `91abc085c3df1927ed3444791deeb9403ebc7266`, with the exact **86-entry M87**
ledger. The maintenance window was **18:28:50–18:29:56 CST**. This is internal
candidate deployment evidence, not PR merge, formal Release or supplier-live
verification.

- The supported clean-checkout helper built backend image ID
  `sha256:7237ffd4c941627e174843ba41cf4c958dd3398a09e0f6b91123d0314adc0848`
  and frontend image ID
  `sha256:3331d3c77219f23d710ae2ee9e35fc342ac8efcb5e6cb4e29db63160110885c7`.
  Both carry the exact version/commit labels. Source, image and both actual
  read-only migration mounts have equal per-file SHA-256 manifests.
- A new server-private M83 backup restored into a separate database. The new
  CLI upgraded that restored database to the full M87 ledger; the checked-in
  guarded downs, in 87/86/85/84 order, restored the exact original M83 ledger.
  Non-sensitive admin/org/project identity fields and wallet amounts remained
  equal. A fresh post-drain M83 dump was also created and its archive listing
  validated; the restore/up/down drill used the earlier preflight dump, not
  this second payload. Both backups and the rehearsal database remain on ECS.
  The old private reset backup and password verifiers were not inspected.
- Nginx/frontend and both old backends stopped with exit 0; no other business
  database client remained. One CLI migration owner advanced the live database.
  Only after full-ledger equality did matching stable/canary/frontend start,
  followed by Nginx. All Compose operations retained project `unirouter` and its
  network. PostgreSQL, Redis and monitoring container IDs are unchanged.
- Both replicas are healthy with `SKIP_MIGRATION=true`, a 150-second container
  stop budget, and exact startup-schema gates. Legacy routing, compatibility
  `true`, async media `false`, source instance and application configuration
  remain unchanged. Billing/pricing environment overrides remain absent; the
  unchanged compiled defaults are `cost_observation` / `legacy`. This is not
  proof of a native-USD cost or customer-settlement contract.
- Public HTTPS readiness and frontend returned 200 with normal TLS verification.
  Effective Nginx `/v1/` transport ceilings are 1920/960 seconds; they are not a
  provider-specific semantic deadline or a performance/capacity qualification.
  The keep lists protect the new images/source and retain every old entry;
  historical M83 states are explicitly not application-only M87 rollback points.
  No cleanup was run. A failed future startup must stop any partial new fleet
  before considering guarded down; evidence guards must never be bypassed.
- Chrome's existing admin session remained valid after a real page reload and
  displayed V1.2.3 with the empty deployment/Release view. Read-only database
  checks retained the Default Project and CNY wallet with all amounts zero.
  The two audit entries are login events, both predating the maintenance window;
  they are not supplier calls or leftover billing activity.

Unsaved template checks covered 16 exact Profiles across all 12 provider
families: DeepSeek Flash; Kimi K2.6/K3; MiniMax M2.5 Highspeed/M3; GLM 5.2;
Qwen 3.7 Flash; Gemini 3.5 Flash; MiMo V2.5; Hy3; GPT Luna Chat/Responses;
Grok 4.6; Claude Haiku; Doubao Seed 2.1 Pro/Evolving. This is representative UI
mapping acceptance, **not exhaustive execution acceptance of 30 models/33
surface identities**. Provider/region, endpoint, exact upstream ID, operation,
Profile limits/default authority and bounded-capability labels were inspected.
DeepSeek's 384,000 maximum and 32,768 request default are separate; Kimi K2.6's
undocumented maximum is not replaced by an invented provider claim. GPT surfaces
resolve separately, Qwen's business-space host requires explicit user input,
and Gemini remains named as an OpenAI-compatible core subset. Unsupported price
selectors stay manual-review warnings; MiMo/Doubao's supported CNY draft offers
remain unchecked, review-only and explicitly exclude unmodeled meters. USD is
not shown as an activated CNY settlement price. All forms were canceled without
entering credentials or submitting any create/probe/price/Release action.

Final post-form readback confirmed zero models, connections, deployments,
credential bindings, channels/keys, calls, provider attempts, wallet transactions,
Freeze rows, prices/Rate Cards/source drafts and probes. The single admin and
non-sensitive identity/wallet fingerprints remained unchanged. Both replicas
were healthy and the candidate checkout was clean. The independent closing
review found no remaining B2 blocker in the retained documentation/evidence
claims; it did not independently rerun Target checks. `git diff --check` passed.
Only the three delivery/architecture documents differ locally; no runtime code,
new gate/CI run, push, merge or formal release was added to B2.

B2 stops here. It does not establish SDK workflows, performance, functional
provider calls, usage/billing correctness, live capability evidence or release
readiness. The next DeepSeek milestone still needs its exact model/operation/
capability matrix and total supplier-call/cost budget, including retry/failover.

### DeepSeek zero-call preparation — blocked preflight, 2026-08-26

The subsequent approval covers DeepSeek V4 Flash onboarding preparation and a
development/test canonical switch **only after supported prerequisites pass**.
It does not authorize provider probes/calls, an identity change, a reset or
bypassing startup/Release evidence. This checkpoint is not a completed cutover.

At 18:53–18:54 CST, both backends still ran exact `ab4b5b3f42e6` and were healthy
in `legacy` / compatibility `true`, with async media `false`. Read-only SQL found
only active platform admin and organization 1 (`Platform Admin`, root
`partner`), no explicit customer root, and a CNY wallet with all amounts zero.
Models, connections, deployments, credential bindings, deployment probes,
runtime Releases/slots and call logs were all zero; audit count remained two.

The deployed binary's supported read-only `model-route-authority check`, with
canonical environment overrides for that CLI process only and organization 1
as the inspected candidate, returned exit 1 / `ready=false` with
`customer_root_mismatch` and `authority_inventory_empty`. No daemon configuration
changed; HTTPS readiness remained healthy. This is an expected precondition
rejection, not a failed attempted rollout.

The blockers and their boundaries are:

- `validated_blocker`: canonical startup requires one explicit active root
  customer and nonempty assigned runtime inventory (`modelrouting/authority.go`).
  The legacy customer-context fallback to a sole partner root does not satisfy
  that authority gate. Creating model data under that fallback now would not
  resolve the eventual canonical customer identity.
- `validated_blocker`: the current first-customer API accepts strict-reset
  (zero wallets) or exact fresh-seed checkpoints. This Target has a runtime
  wallet and two login audits, so neither accepted predecessor matches
  (`admin/first_customer_bootstrap_handlers.go`). This conclusion combines the
  inspected guards with Target readback; no bootstrap POST was attempted.
- `validated_blocker`: sealing and initial activation share a proof requiring
  active credentials and their own successful exact-revision operation probes
  (`runtimechange/deployment_manifest.go`). An entirely zero-provider-call
  first canonical activation is therefore not available through this workflow.
- `product_decision_required`: any additional first-customer initialization or
  compatibility repair must explicitly preserve admin/platform identities and
  retained evidence. It is not authorized by the cutover approval. Probe count,
  total supplier-spend cap and credential entry remain a separate traffic gate.

No persistent onboarding, bootstrap, probe, Release or configuration mutation
was performed. The exact preparation identity remains
`deepseek.v4-flash.api@1.4.0` / `openai.chat_completions.v1` / upstream
`deepseek-v4-flash`; a template is not an attachment or live evidence. Stop here
until the initialization boundary is resolved; do not reset, delete login
audits, change the partner type, forge probe evidence or relax startup checks.
The bounded independent review confirmed these code prerequisites and found no
supported zero-credential/probe bypass; it did not independently inspect Target
or authorize a repair. Only the existing three documentation files changed;
`git diff --check` passed. No runtime tests, CI, push or deployment were added.

### First-customer runtime-reset repair — Local/CI and Target preview, 2026-08-26

The product owner approved the compatibility repair and subsequent independent
test-customer initialization after the blocked preflight. The exact Target
runtime shell was read back as project 85 under partner organization 1, no
members/budget/creator, all-zero CNY wallet, one valid session and two login
audits; settings are the four reset-preserved keys and M87 has 86 entries.

The candidate adds a distinct runtime-reset predecessor without changing the
old strict-reset or fresh-seed requirements. It checks the exact automatic
project, zero wallet, reset-owned empty tables/settings and full artifact ledger;
it retains login/session/outbox evidence and validates the current committing
session. Project-linked audit/outbox evidence rejects normalization so foreign
key behavior cannot rewrite retained evidence. No admin, partner identity,
verifier, License/Release, schema or canonical/probe rule is changed.

The same API now accepts strict-boolean `dry_run:true` and rejects duplicate JSON
members, null and non-boolean preview flags. Preview allocates no identity and
creates no audit or receipt. The deployment-operator panel lists exact shell
objects and requires explicit confirmation; a changed name, actor or session
invalidates preview/confirmation, and uncertain submissions retain their
idempotency intent only within the same identity/session. Ordinary customer
workspaces have no additional initialization entry. At the preview checkpoint,
Target shell removal awaited separate exact-object confirmation; the later
approved execution is recorded below.

Local evidence: the original handler returned 409 for the runtime-shell fixture;
the new path passes the first-customer integration suite, including existing
fresh/strict predecessors, evidence preservation, read-only checks and preview,
schema/data/identity/session drift, invalid preview flags, fresh-fallback denial,
and concurrent bootstrap/baseline behavior with `-race`. This used isolated
PostgreSQL **18.3** on the developer machine, not PostgreSQL 16 Target evidence.
The 67 focused frontend tests and type/API/React guards passed with zero new
warnings. The bounded independent review found one UI actor/session lifecycle
issue; its local form-remount fix and two regression tests were independently
rechecked, with no remaining blocker or required correction.

The bounded local integration gate then passed `go vet ./...`, the untagged
Admin/customer-context/CLI race suites, release metadata and diff checks. The
full frontend run initially passed 629/631 tests: two unchanged UsersPage tests
clicked a Semi option before its popup/controlled selection had committed.
They passed in isolation; their test-only fix now follows the existing explicit
CSS-animation/committed-selection contract, without changing product behavior,
timeouts or the warning ratchet. The affected UsersPage/bootstrap/network
25-test regression, production build and bundle budget then passed. Previously
passing lanes were not mechanically rerun. The repair adds only that one
test-file correction to the reviewed scope. These local checks alone do not
establish Target execution or a canonical cutover.

The one pushed candidate is `e888c05f9d5d70f4809bcd5362373b8fa8f8ecf4`, tree
`99e461f381d714bbb30a96c8a5068e26d294ff62`, version 1.2.3. Both the
[main CI run](https://github.com/foigent/unirouter/actions/runs/32963747418) and
[protocol run](https://github.com/foigent/unirouter/actions/runs/32963747345)
passed without reruns. Main CI includes the real PostgreSQL 16 Admin integration
tests, full frontend suite, browser E2E and Docker build. PR #339 remains open;
there was no merge or formal Release.

Target acceptance on 2026-08-26 (UI runtime observation at 20:00 CST):

- A verified standard Git bundle transported the exact candidate to a clean
  detached ECS worktree. The supported helper built each component once, using
  the default official Go proxy. Backend image ID is
  `sha256:d87061f11ab69a0ccf90812f9d7ea23ff9b7e65504ccbc858e5e83c4c62c0480`;
  frontend image ID is
  `sha256:6969a14d44bb8e4525fd3fc6dcd29b6cde7a7a32ca75f3354f4792516ea07a6f`.
- Canary and stable use that same backend image and are healthy; frontend and
  HTTPS readiness are healthy. The schema remains M87/86 entries. This repair
  has no migration, Compose or Nginx source diff. The B2 environment/transport
  overlays were retained; Nginx was validated and reloaded for refreshed DNS.
  Legacy routing, compatibility true, async media false and the unchanged
  cost-observation/legacy compiled defaults remain in effect.
- The existing admin login remains valid. The deployed operator UI reports
  `e888c05f9d5d` and its real `dry_run` succeeds for the proposed name
  `UniRouter Clean Test`, explicitly listing automatic project 85 and the zero
  wallet for platform organization 1. Confirmation stays unchecked and actual
  submission was not performed.
- Before/after readbacks of non-sensitive admin/org/project/wallet fields are
  equal. There is still one admin, one platform org, one automatic project and
  one all-zero wallet; two login audits, one session and zero outbox events.
  Models, connections, deployments, keys, call logs and runtime Releases remain
  zero. This is Target preview evidence, not Target bootstrap mutation evidence.
- The current image/source keep entries were added without removing any old
  entry or running cleanup. Server-private deployment evidence is under
  `/opt/unirouter/deployment-evidence/20260826-bootstrap-e888c05f`; it includes
  the source bundle, build log and credential-free acceptance record. The old
  reset-evidence backups were not read, downloaded or deleted.

Diagnostic boundaries: the initial Target Git fetch used an existing local-
bundle origin without the requested branch and was corrected to the supported
bundle path; a manual health check used the wrong `/health` path, then passed
at `/health/ready`; a Chrome navigation timeout was followed by a loaded page,
HTTP 200 and successful UI preview. None establishes a product regression.
The unchanged dependency lock produced audit advisories: the production-only
audit reports two moderate findings and zero high/critical findings. Exploit
reachability was not assessed and dependencies were not changed in this batch.

Deployment uses the same M87/86-entry ledger; the application rollback
target is B2 `ab4b5b3f42e6819d27ac946ad7899ac08498affb` on the same schema;
rollback must not undo any subsequently approved customer or audit records.
The repair/preview tranche stopped at the exact-object normalization gate;
the subsequent approval and initialization checkpoint follow below. Paid
probes/calls still require a separate bounded traffic budget.
These post-Target documentation updates are local evidence follow-up, not a
second implementation push or CI cycle.

### First-customer initialization — Target verified, 2026-08-26

The product owner explicitly approved removing automatic empty project 85 and
the all-zero wallet of platform organization 1 through the supported bootstrap.
After rechecking the same healthy `e888c05f9d5d` runtime and exact predecessor,
the existing authenticated operator UI submitted the same previewed name,
`UniRouter Clean Test`. The transaction committed at **20:24:58 CST**:

- Independent active root customer **2** and its CNY wallet were created; all
  wallet amounts are zero. Project 85 and the platform zero wallet were removed
  only by the guarded bootstrap transaction, not by reset or manual SQL.
- Admin remains user 1 in platform organization 1 with the same role, status and
  auth epoch 6. Platform organization 1 remains the active root partner. Both
  login audits remain; exactly one `bootstrap.first_customer` audit identifies
  actor 1, platform org 1 and created customer 2. No verifier was read or output.
  The bootstrap code does not modify admin/verifier, License or Release state.
- Canary then stable were restarted at the same image/configuration. The normal
  baseline path created **Default Project 86 / customer 2** once, and the second
  startup reused it. There are no project memberships; the platform admin was
  not converted into a customer user or project member. Both replicas are healthy.
- The operator panel disappeared after the returned customer context refreshed.
  The customer dashboard now explicitly says `UniRouter Clean Test`; request,
  deployment and model inventories remain empty. This empty dashboard is not a
  performance or pricing qualification.
- Final readback has zero model registry rows, provider credentials, API keys,
  calls, transactions and Freeze rows. Schema remains M87/86; legacy routing,
  compatibility true, async media false, source instance, and absent billing/
  pricing overrides remain unchanged. Effective defaults are cost observation
  and legacy request pricing, not prepaid charging.
- The supported canonical **check-only** CLI for customer 2 now reports only
  `authority_inventory_empty`. The former `customer_root_mismatch` is gone.
  Exit 1 is an expected missing-inventory result, not a failed cutover. Process-
  local CLI overrides did not modify daemon configuration or activate canonical.

A new mode-0600 custom-format pre-initialization backup is retained only in
the existing ECS deployment-evidence directory. Its archive list validated;
this checkpoint did not repeat a restore drill or inspect the old reset backup.
Pre-initialization recovery material is retained, but this particular payload
has not been restore-tested. Restoration is a separately scoped data operation,
not an automatic rollback of newly retained customer/audit records. No new code,
gate, push, CI or formal
Release was needed for this already-tested workflow.

### DeepSeek first paid workflow — bounded proposal, not yet authorized

Candidate identity: `deepseek.v4-flash.api@1.4.0` × CN official OpenAI-compatible
`chat.completions` × non-stream text. Proposed public model ID `deepseek-v4-flash`,
upstream `deepseek-v4-flash`, endpoint `https://api.deepseek.com`. Connection,
deployment, operation and credential revisions must be resolved and frozen
before the connectivity probe. Only a successful exact probe permits subsequent
Release authoring/activation; exact Release, route and client-key identities
must then be frozen before the gateway request. The later saved-onboarding
checkpoint below records the pre-probe identities; Release and client keys
remain absent.
This staged ordering does not permit fabricated probe evidence or skipping
canonical preconditions.

| Request | Maximum upstream calls | Frozen request boundary |
|---|---:|---|
| Supported operation connectivity probe | 1 | Short fixed text, `stream=false`, `max_tokens=1`; existing 15-second probe client, no redirect or application retry. Provider-default thinking remains in effect; this is not reasoning conformance evidence. |
| Generic-client gateway non-stream text | 1 | Single short text message, explicit `thinking.type=disabled`, `max_tokens=128`, `stream=false`; exact Profile/Release/route binding required. |
| Direct upstream comparison | 1 | Same input, model and output/thinking settings as the gateway request; no tools/media/search. |

Proposed total ceiling: **3 provider requests / CNY 1**, including the probe;
no automatic retry or failover and stop for diagnosis on any failure. A timeout
or ambiguous dispatch consumes a call slot and cannot be treated as free.
The two functional requests use at most 128 completion tokens and a conservative
512-input-token planning allowance. This is a proposed authorization envelope,
not a claim that a local wallet can enforce the supplier's bill. The current
customer wallet must retain zero holds, zero debits and no customer financial
transaction; absent price remains unavailable, not an approved zero tariff.

Official Chinese [DeepSeek pricing](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)
was re-read on 2026-08-26: Flash's off-peak input/cache-hit/output prices per
million tokens are CNY 1.5/0.05/4.5; weekday peaks are 3/0.10/9 (Beijing
09:00–12:00 and 14:00–18:00). The conservative three-request token estimate at
peak rates is CNY 0.008064, distinct from the proposed CNY 1 authorization cap.
Rates must be checked again before dispatch. This is a local supplier-cost
estimate, not supplier-reported cost, customer pricing or an active Rate Card.
The [Chat reference](https://api-docs.deepseek.com/zh-cn/api/create-chat-completion/)
documents the explicit thinking toggle; current provider output maximum is
384K, while the Profile's 32,768 is a UniRouter request default and is not used
for these tightly bounded test requests.

The older 2026-08-24 frontend price summary is not current price evidence.
It already remains `manual_review_required` and supplies no executable rates;
no stale price was activated. Updating that dated summary is a bounded pricing-
documentation follow-up, not authority to implement missing cache-write meters,
customer charging or a financial redesign. The existing POC smoke script is
not run unchanged: it lacks this explicit output/thinking bound and does not
provide sufficient usage/receipt evidence. No Agent framework is required.

Before operator credential entry, the Chrome onboarding form was prepared but
unsaved: it projected the official
DeepSeek endpoint, exact Flash @1.4.0 Profile, stable upstream/public ID,
Chat operation, 1M context, 384,000 public maximum output and 32,768 request
default. At that checkpoint no supplier credential was entered or created, and
no probe ran.
Its existing price-summary warning still contains the older numbers; those
must not be used for the proposed test budget or represented as current pricing.

The operator subsequently entered the credential in the secure UI, as recorded
below; no secret was pasted into this conversation or read back by the agent.
At that checkpoint the budget decision was still pending. Zero-call setup and canonical
preconditions do not authorize a probe implicitly.
The bounded independent closing review found no additional bootstrap blocker.
Two `validated_required` wording findings were accepted: separate pre-probe
identity freezing from post-probe Release/client freezing, and avoid claiming
that archive-list validation proves restoration. Both are corrected above;
the reviewer did not independently repeat Target checks or approve traffic.

### DeepSeek Flash saved onboarding — zero-call readback, 2026-08-26

At 22:13 CST, after the operator reported completing credential entry, read-only
Target checks found the onboarding already saved. No duplicate submit was made.

- Public model **677**, customer **2**, `deepseek-v4-flash`, lifecycle `staging`,
  exact `deepseek.v4-flash.api@1.4.0`, context 1,000,000 and output cap 384,000.
- Connection **9@revision 1**, DeepSeek official HTTPS origin, adapter
  `openai_compatible`, enabled. Deployment **1@revision 1** binds model 677 and
  connection 9 to upstream `deepseek-v4-flash`; priority/weight 100/100, context
  1,000,000 and request-default output 32,768.
- Credential binding **9@state revision 1** is active and linked only to that
  deployment; last-used/cooldown are null and fail count is zero. Only metadata
  was queried, never the encrypted key or plaintext. This does not prove the
  credential is accepted by DeepSeek.
- Operation **`openai.chat_completions.v1`@revision 1** is `declared`, with no
  verified timestamp or capability-evidence rows. There are zero probes, calls,
  runtime Releases, client keys, legacy model/org prices, transactions or Freeze
  rows. The customer wallet remains all-zero; one create audit was added while
  the bootstrap audit and two login audits remain.
- The current canonical check still returns `authority_inventory_empty` for
  runtime assignments; it does not mean the newly saved deployment is missing.
  No daemon mode, configuration, Release or traffic was changed in this readback.

At this saved-onboarding checkpoint the first 3-call/CNY-1 supplier budget was
still unapproved. Credential entry or
successful persistence cannot authorize a paid connectivity probe implicitly.

### DeepSeek first paid workflow — approved pre-dispatch checkpoint, 2026-08-26

Following the saved-onboarding readback, the product owner explicitly confirmed
the proposed **maximum 3 supplier requests / CNY 1** batch. The preceding
unapproved proposal/readback paragraphs are historical checkpoints, not the
current authorization state. Only the three rows of that frozen matrix are
approved: one supported operation probe, one generic-client non-stream request,
and one direct comparison. No automatic retry, failover, additional probe,
streaming, tools, reasoning-conformance or other model traffic is approved.
Any failed or ambiguous dispatch consumes its slot and stops further traffic
for diagnosis; unused slots are not permission to change the matrix.

The pre-probe identity is customer 2 / public model 677 /
`deepseek.v4-flash.api@1.4.0` / connection 9@1 / deployment 1@1 /
`openai.chat_completions.v1` operation revision 1 / credential binding 9@1.
The operation endpoint protocol remains `openai.chat_completions@1.0.0`;
it is not the model's execution Profile identity. Release, route and caller
identities must be established through supported workflows and separately
frozen before the gateway request, never fabricated as probe prerequisites.

Readiness was rechecked at 22:58 CST: both replicas remain healthy at
`e888c05f9d5d`, API readiness reports DB/Redis healthy, and supplier probes,
gateway calls, Releases, client keys, financial transactions and Freeze rows
are still zero. Customer 2's CNY wallet is all-zero. Billing/pricing overrides
remain absent (`cost_observation` / `legacy` compiled defaults); daemon routing
remains legacy with compatibility enabled and async media disabled.
The official Chinese pricing page was refreshed again before dispatch and
retains the peak/off-peak rates quoted above. The peak-priced conservative
token estimate remains CNY 0.008064, not an invoice or customer tariff.

No supplier request has been sent at this checkpoint. The secure generic-client
credential path and transport no-replay boundary are being checked independently;
reviewer findings do not grant additional authority or change the matrix.

### DeepSeek Flash exact connectivity probe — Target verified, 2026-08-26

At **23:02:34 CST**, the authenticated operator submitted the supported exact
probe once. Durable probe **1** records HTTP **200**, **298 ms**, 533 response
bytes, `success=true`, `provider_success=true`, `current=true`, reason `ok`,
and reported model `deepseek-v4-flash`. Connection 9@1, deployment 1@1 and
credential binding 9@1 match the frozen pre-dispatch identity. The operation
advanced from `declared` revision 1 to `live_probed` revision 2 with `probe:1`.
No capability evidence was synthesized. The returned alias is not evidence of
an exact upstream model release or a permanent version pin.

This consumes **1 of 3 supplier-call slots**. Gateway and direct-comparison
slots are unspent. The probe does not persist token usage or supplier-reported
cost, so its actual cost is **unavailable**, not zero. The token/rate planning
allowance above remains an Estimate, not an Observation. Its 298 ms latency is
for this tiny connectivity probe, not a text-generation performance baseline.
Target readback still has zero gateway call logs, Freeze records and financial
transactions; all customer-wallet amounts remain zero. No Release was authored,
no client key issued, and no canonical switch occurred.

At this post-probe checkpoint, the remaining functional requests were paused
at two preconditions. The first is now resolved by the verified handoff below;
the customer caller/accountability prerequisite remains open:

- `validated_blocker`: the default gateway `providerClient` followed HTTP
  redirects inside one `.Do`, outside the provider-attempt count. A local test
  using the real adapter's replayable POST and controlled transport reproduced
  two HTTP requests for each of 301/302/303/307/308. There is no evidence that
  DeepSeek performed such a redirect. The minimal local fix refuses redirects
  and preserves the original status/body for ordinary failure handling; it
  changes no Profile, schema, routing selector, retry count or price. Target
  at that checkpoint still ran the preceding `e888c05f9d5d` implementation.
- `validated_blocker` (clarified on 2026-08-27): a valid customer-side calling
  identity is missing. Service grants require a same-organization active
  sponsor account with calling/administrative authority; the only user is
  platform admin 1/org1, not a customer 2 account. The minimum existing path is
  an explicitly attributed customer `member`, project 86 `project_user`, and a
  non-production workload/direct grant. An authorized platform administrator
  can manage the resulting service credential and the Agent can execute the
  test. The sponsor need not personally operate or accept each request. Code
  does not prove natural-person identity or customer acceptance; an invented
  user row would not establish responsibility. No user, membership or grant
  has been created as a workaround. Personal credentials remain subject-self-
  service, not a platform-admin impersonation path.

The bounded reviewer supplied source findings only. The main agent validated
the transport defect with the failing local test and separately classified the
accountability prerequisite; neither finding authorizes extra paid calls or a new
identity policy. Remaining scope is still the original gateway/direct pair.

Local repair closeout: the redirect regression now passes for 200 and all five
redirect statuses. After the bounded independent code review found no blocker,
the necessary local gate passed once: `go test -race ./internal/gateway
./internal/adapters` (gateway 98.911 s, adapters 2.715 s), followed by
`go vet ./internal/gateway ./internal/adapters` and `git diff --check`.
The review's one ambiguous milestone sentence was corrected without expanding
scope. A macOS linker warning did not fail the gate. At this local checkpoint,
the repair had not yet been pushed, CI-tested or deployed; the earlier commit's
CI was not evidence for the new diff. The remaining live requests awaited the
customer-side responsibility decision; the separately authorized repair handoff
subsequently completed as recorded below.
No additional supplier traffic was used by these tests.

### Cumulative paid-test budget update — approved, 2026-08-26

The product owner subsequently authorized continued testing with a cumulative
ceiling of **20 supplier calls / CNY 5**, including the successful probe already
recorded above. This supersedes the earlier 3-call/CNY-1 ceiling, not its evidence
or the approved model/surface/capability scope. Current usage is one call; at
most 19 additional calls remain available. The earlier probe's actual supplier
charge is unavailable and must not be treated as a free call or zero cost.

The current active matrix remains Flash non-stream text: one gateway request
and one same-payload direct comparison with thinking disabled, `max_tokens=128`
and no tools/media/search. Remaining budget is not a target to consume. Any
subsequent already-approved capability needs its own bounded request matrix and
cost estimate frozen before dispatch, within this same cumulative ceiling.
Probe, retries, direct comparisons, failover and ambiguous dispatches all count;
automatic retry/failover remains disabled for this matrix and failures stop
further traffic for diagnosis. No wallet mode, price authority, identity policy,
canonical prerequisite or Release guard is relaxed by the larger budget.

The budget update did not explicitly identify a customer-side human sponsor.
That narrow responsibility question remains separate. The reviewed no-redirect
repair subsequently completed its one push/CI/Target handoff below, with no new
supplier request used for deployment acceptance.

### No-redirect transport repair — deployment and zero-call acceptance, 2026-08-27

The finite repair is committed and pushed once as
`35517b9196ec94b7625f4532303d0d45262f22b1`, tree
`022686fc1153183862195b35e81044c7a20a76ab`, version **1.2.3**. Its executable
scope is the default provider HTTP client's no-redirect boundary and its
regression test; the remaining changed paths are the existing three evidence/
scope documents. No Profile, protocol, frontend, migration, routing policy or
pricing contract changed.

- Local verification and the bounded independent review are recorded above.
  The manually dispatched [CI run 32986199076](https://github.com/foigent/unirouter/actions/runs/32986199076)
  passed at this exact commit, attempt 1: backend tests/integration/conformance,
  PR4 browser E2E, frontend build and Docker build. No automatic run was observed
  after the one push at that checkpoint, so the existing workflow was dispatched.
  A later PR readback on 2026-08-27 also found automatic CI 32986498453 successful
  at the same commit. Therefore this was not a sole-run handoff; the earlier
  observation did not prove an automatic run was absent. For the next batch,
  inspect the exact-commit PR run before considering any manual dispatch.
  No re-push or CI rerun was used, and PR 339 remains unmerged.
- The supported Target source-build helper verified the clean exact source
  worktree and produced backend
  `unirouter-release/backend:35517b9196ec`, image
  `sha256:92d024eb5943bc2fea4f40a4dcdeef4cc050e45fdc002af31e249f005606d91e`.
  The helper verified unchanged frontend ancestry/path content and reused
  `unirouter-release/frontend:e888c05f9d5d`, image
  `sha256:6969a14d44bb8e4525fd3fc6dcd29b6cde7a7a32ca75f3354f4792516ea07a6f`.
  The frontend was not rebuilt or relabeled as the new backend revision.
- The source diff has no migrations or Compose changes. Read-only migration
  status, database count/max and the new processes' exact-ledger startup gate
  agree on **M87 / 86 entries**. No migration was executed. Both replicas were
  replaced sequentially using the existing TLS and runtime overlays, stop
  budget and `SKIP_MIGRATION=true`; no schema-crossing rolling upgrade occurred.
- Both backend containers became healthy with zero restart count; Nginx syntax
  validation and reload passed. The first post-reload public readiness request
  returned curl error 16 (HTTP/2 framing). A subsequent bounded diagnostic got
  HTTP 200 with DB/Redis healthy over both HTTP/2 and HTTP/1.1, and HTTP 200 from
  the local canary entry. The public frontend also returned HTTP 200 over HTTP/2.
  The initial failure did not recur in these checks; its cause is unproven, and
  no timeout or Nginx policy was changed to hide it.
- Both replicas retain `MODEL_ROUTE_AUTHORITY=legacy`, compatibility `true`,
  async media `false`, source instance `unirouter-test-reset-20260824-v3` and
  absent billing/pricing overrides (`cost_observation` / `legacy` defaults).
  The post-deployment database readback at **00:12:28 CST** has one probe and
  zero gateway calls, provider attempts, runtime Releases, client keys, project
  memberships, financial transactions or Freeze records. Customer 2's CNY
  wallet remains all-zero; admin 1/org1 and auth epoch 6 are unchanged. The
  operation remains `live_probed` revision 2/ref `probe:1`.
- A read-only refresh of the existing Chrome model-access page retained the
  authenticated admin view and saved DeepSeek Flash entry, without credential
  inspection or another probe. This is a session/readback check, not a fresh
  password login or functional model test.

Build/deployment and additive retention metadata are retained server-side at
`/opt/unirouter/deployment-evidence/20260826-redirect-35517b91`; the unchanged
private reset-evidence directory was not accessed. The preceding `e888c05f9d5d`
M87 backend and source worktree remain available for application-only rollback;
no rollback was exercised, database restore performed, or image/worktree cleanup
run. Older M83 artifacts are not application-only rollback candidates for M87.

This closes the transport repair's Local/CI/deployment acceptance, **not** the
first functional workflow, all-model adaptation, canonical enablement, billing
acceptance or a formal Release. Cumulative supplier usage is still **1/20 calls**;
the **CNY 5** ceiling is unchanged and the existing probe's supplier charge
remains unavailable, not zero. No additional paid traffic was generated by the
repair. The same gateway/direct pair is pending an explicitly attributed
customer-side account and supported least-privilege caller/Release preparation.
This account prerequisite is not a requirement for the user to test personally,
and it is not resolved by increasing the test budget.

### Test executor and caller accountability clarification — 2026-08-27

The product owner delegated all test execution to the Agent and reserved the
customer-side test-responsibility role for later. The earlier wording conflated
test execution, service-grant sponsorship and customer acceptance. It is
corrected here: these are separate responsibilities; no rule requires the
sponsor to log in or personally perform the tests.

The main agent validated the bounded review's distinction against
`accesssponsor/sponsor.go`, `accessprovisioning/service.go` and the public gateway
authentication chain. The actual runtime prerequisite remains: a same-customer
active account must support the service Grant, and its service credential must
authenticate the public request. An admin login session is not a gateway key;
an Agent capability also needs a valid root key. The administrative operation
probe bypasses the public gateway/admission/Freeze path and cannot prove it.
The old PRD4 SQL-seeded legacy fixture is not a substitute for this current
clean-baseline identity/Grant flow. No new operator bypass is authorized or
implemented by this review.

At **01:17:52 CST**, read-only Target checks found one total user, zero customer
2 users, project memberships or client keys, and healthy `35517b91` replicas.
A single credential-free public Chat request returned **401** with
`auth_error.invalid_api_key`, as required. It is a negative authentication
check, not a supplier call or a functional-model result. Post-check readback at
**01:19:47 CST** still has one probe and zero gateway calls, provider attempts,
Freeze records, client keys or customer users. No credential was read,
account created, responsibility assigned, provider probe repeated or customer
acceptance recorded. The remaining gateway/direct pair still awaits supported
caller preparation; the Agent must not claim it can execute the full workflow
using only the existing admin probe. The cumulative ceiling remains 20 calls/
CNY 5, with the single earlier probe consuming one supplier-call slot.

### UI-led account and first-call preparation — 2026-08-27

The latest user instruction replaces script-driven provisioning with product
UI interaction. The unexecuted temporary CLI draft was not used. Chrome retains
the original admin session; a separate browser session holds the ordinary test
login. Login passwords were not read back or printed. The later one-time Key
automation incident and immediate revocation are recorded in the next section.

- UI empty-submit validation rejected missing username/password. One submit
  created `unirouter-test-20260827` (user 38), ordinary member of customer 2;
  the default-project option created project 86 membership `project_user`.
  UI member-list readback and a fresh test-account login succeeded. Personal
  acceptance remains deferred, but is no longer an account-creation blocker.
- UI authoring created Release 22,
  `rcr_deployment_20260826t173443_8a00fcf89d8123b56058e0be`, validated@2,
  authoring receipt 1 at 01:34:43 CST. The UI confirmed exact
  `deepseek.v4-flash.api@1.4.0`, context 1,000,000, output 384,000 and only
  deployment 1 / Chat. UI confirmation activated it at 01:35:13,
  active@3 / slot version 1 / lifecycle receipt 1. This is a runtime Release
  inside development/test, not a software release or functional-model result.
- The same UI opened model 677's developer catalog visibility and then its GA
  catalog lifecycle. No capability label, Profile, limit, pricing or evidence
  was upgraded. Even after this configuration, the member workbench remained
  empty and offered a fictional `company-chat` fallback.
- Read-only Target checks at 01:39:48 confirmed one customer user/member,
  zero client keys/calls/attempts/Freeze rows and the original single supplier
  probe. The old assignment view projected Release 22 as stable/active with
  `channel_id=null`, `admission=''`: it expands v1 overrides, not v2 deployment
  items. Both daemons still used legacy routing, compatibility true, async false.

The main agent adjudicated the independent review as follows:

| Finding | Disposition / finite correction |
|---|---|
| v2 Release omitted from routable project catalog | `validated_blocker`: consume existing normalized route items with exact tenant/public-model identity and stable/active/allow constraints; retain explicit historical v1 compatibility. No schema or Planner change. |
| Frozen max_tokens/thinking request cannot be expressed; empty catalog invents a model | `validated_blocker`: optional explicit controls, omission preserved by default; no arbitrary JSON merge or provider capability expansion; no model means no dispatch. |
| HTTP 200 malformed/non-text body shown as success; latency stops at headers | `validated_required` for this acceptance: show response error, preserve HTTP status, measure complete body duration separately from header latency; show original usage and finish reason. |
| Directory refresh silently substitutes another model with old thinking parameters | `validated_required`: pin the selected model in UI state; disappearance disables Run until explicit reselection; shared Select/manual-input changes clear provider extensions. Two failing regressions were reproduced and corrected. |
| Streaming parser and member diagnostic model-list permissions | `valid_deferred`: not part of this non-stream text repair; no permission relaxation or streaming-complete claim. |

The finite local batch is the workbench, its tests, catalog query, catalog tests
and these three existing evidence/scope documents. Local workbench tests and
isolated PostgreSQL tests cover canonical activation, same-name tenant isolation,
independent catalog approval/lifecycle and retained v1 behavior. The closing
review confirmed the final correction without expanding scope. All 30 workbench
tests, the affected admin package race tests (15.280 s), admin vet, isolated
PostgreSQL integration and the production frontend build/bundle guards passed.
The existing Semi findDOMNode warning remains within its tracked ratchet; the
macOS linker warning did not fail the race gate. The first candidate was pushed
as `fe692d0da1203b7d3250eada740a75ab2fcaae1f`, tree
`6a6bf0523c4378167a3de093043323a5a95986f6`. Its automatic CI 32996980104
passed real-backend browser acceptance but failed frontend testing: 644/645
passed, with the new bounded-request test missing thinking in its captured body.
This candidate was not deployed; its built images/evidence remain identifiable.

The correction changes only the test interaction and this evidence record.
User-event clicks plus value waits alone also reproduced the problem in the
Node24 full suite. The existing ModelEndpointsPage test pattern supplies the
positioned popup's enter/leave completion: jsdom does not emit CSS animation
events, and the controlled Select defers onChange until popoverClose. The test
still clicks real options and asserts exact payloads; it does not change product
code, disable animation, increase timeouts or relax assertions. A bounded
independent counter-review confirmed that boundary. Node **24.19.0** full
frontend tests then passed **82 files / 645 tests** in **41.02 s**, with the
existing warning ratchets unchanged. The corrected candidate subsequently
passed its own CI and was deployed; the first candidate's failure is not
relabeled green. At this local checkpoint no paid call had been used to discover
or reproduce these defects; cumulative supplier dispatch remained **1/20**,
CNY 5 ceiling, prior probe charge unavailable (not zero), and the gateway/direct
functional pair remained unexecuted. The next section records later Target facts.

### First ordinary-user UI request — pre-dispatch blocker and bounded correction

The corrected workbench candidate was deployed between 02:34 and 02:35 CST.
The ordinary-user UI then created API key 73, restricted to
`deepseek-v4-flash`, 1 RPM and 1 RPS. The UI retained the key only in its
password control; neither the one-time value nor the control value was read
back, logged or copied into evidence.

The first Run click used exactly non-stream Chat, `max_tokens=128`,
`thinking.type=disabled`, no tools/media/search/retry/failover, and the prompt
`Reply with exactly: UNIROUTER_UI_OK`. The UI reported HTTP 503 and
`No available channel for model: deepseek-v4-flash`; call log 369 / request
`op_a2cb96a94999b506cee57bead4a25fd5` recorded `usage_status=no_route`, zero
tokens, zero cost and no route identities. Read-only Target evidence showed zero
provider attempts, zero Freeze rows, zero transactions and an unchanged zero
wallet. This click therefore did not consume a supplier-call slot; the bounded
supplier count remains 1/20.

The exact Release/profile/operation route existed, but no capability evidence
row existed. Typed validation correctly normalized the explicit disabled value;
the invocation-capability derivation subsequently treated the mere presence of
the toggle as active reasoning. The canonical selector then correctly failed
closed. The correction carries the typed effective toggle value into the one
Plan capability snapshot: disabled controls remain present in the provider wire
payload but do not require reasoning route evidence; enabled and omitted
provider-default reasoning still do. It does not insert capability evidence,
relax canonical eligibility, modify the Profile/Release or remove the provider
control.

Focused positive/negative tests, the affected gateway/modelrouting race gate,
vet and `git diff --check` passed locally. An independent blocker review
confirmed the correction and its boundary. Exact commit
`e47af1faacd8ca96403c25b4cf2c47ec155bd980` then passed automatic CI
33002920179 and protocol-conformance CI 33002920186. Development/test reused the
unchanged `1f30b1cd4c8d` frontend, built backend image
`sha256:6a6d066ab4986a1ac2e1786062ff290ee60fdf69585e6c5dcd9a5296a75801eb`
once, drained both prior canonical replicas and brought both exact replicas
healthy without a schema migration. HTTP/1.1 and HTTP/2 readiness passed.

The post-deployment UI clicked the same frozen request exactly once. It returned
HTTP 200 / request `op_38289be7c6561dfbf7340a6faac8f4b8`, exact content
`UNIROUTER_UI_OK`, `finish_reason=stop`, 16 prompt tokens, 8 completion tokens,
24 total tokens, zero cache-read tokens and 16 cache-miss tokens. The UI showed
1,584 ms complete-client time and 1,583 ms headers time; the durable call row
records 834 ms gateway/provider latency. UI refresh projected two total calls,
one success/one failure, 24 tokens, 50% error rate and zero customer charge.
Request lookup showed the exact DeepSeek provider, official connection and
routed model without exposing request content or credentials.

The successful request has one route, one provider attempt and no retry
authority. Call, attempt and Freeze bind deployment 1, connection/credential
binding 9, Chat operation, Release 22 and the same immutable Plan snapshot
`mrs1_159eccb69f57f6277dead9df9c836249633aab64fae833abfeada5fa39c32fca`.
Usage authority is provider-reported/final. The cost-observation Freeze settled
with zero frozen/calculated/charged customer amount and append-only observation
receipt 163; there is no customer transaction, supplier-cost reconciliation or
wallet movement. This zero customer charge is not evidence that the supplier
call was free. Probe 1 plus this UI attempt consume 2/20 supplier-call slots;
the failed no-route UI click and key-management actions consumed none. All test
keys are disabled. This closes only Flash non-stream text as
`canary_live_verified`; personal customer acceptance, direct parity, reasoning,
tools, billing, supplier-cost authority, other capabilities and `released`
remain separate.

### DeepSeek Flash streaming qualification control plane — local M88 candidate

The next finite milestone is exactly:

`DeepSeek V4 Flash × OpenAI-compatible Chat Completions × streaming capability qualification`.

It does not add streaming to another model or surface. It also does not add
tools/reasoning/cache approval, automatic approval, batch approval, revocation,
dual control, pricing authority, Release or canonical cutover.

M88 introduces one generic append-only capability probe/approval receipt model,
but exposes only the exact typed DeepSeek Flash Chat SSE probe contract in this
batch. Its historical `@1.0.0` contract validates `text/event-stream`, legal
deltas, a finish reason, the then-documented separate final `choices=[]` usage
frame and `[DONE]`. It creates
`live_probed` evidence only. A platform approval is a separate immutable action
labelled “批准在当前环境进入路由池”; it is not `released`, an SLA claim or a
long-term stability claim. The UI places both actions in one guided capability
validation panel, discloses one expected supplier call and states that unavailable
supplier cost is not zero cost.

The migration materializes an `enabled/declared` streaming projection for an
already-existing deployment only when its provider, upstream model, operation,
Execution Profile and protocol Profile match one of two exact M88 support
tuples: the current DeepSeek v4 Chat protocol, or the historical generic Chat
protocol already sealed into pre-M88 DeepSeek Flash Releases. This is not a
generic Chat inference. The canonical deployment creator continues to refine
new exact applications to the v4 protocol and applies the same compiled-plus-
database check. Both paths are zero-provider-call declaration
writers: they create neither live evidence nor approval, routing, Release or
pricing authority, and `ON CONFLICT DO NOTHING` preserves any prior explicit
support decision, including `unsupported`. Rollback removes only the untouched
M88 declaration marker; retained probe, approval or attempt evidence prevents
destructive down migration. The M87→M88→M87→M88 integration path asserts that
both exact declarations are restored, a mismatched tuple stays absent and an
existing unsupported decision remains unchanged.

The mutable `model_deployment_capabilities` row is not routing authority. The
receipt-aware qualification resolver covers all three execution consumers:
Planner, cutover/manifest qualification and pre-dispatch revalidation. New
Plans/cutovers deterministically select the preferred current receipt. A frozen
Plan instead validates its already-bound approval identity; a newer equivalent
receipt does not invalidate it. Stable deployment/operation/Profile/protocol/
probe-contract drift does invalidate qualification. Transient credential health
or cooldown does not revoke the streaming approval, although existing
connectivity gates may still make the route temporarily unavailable.

Local deterministic tests and isolated PostgreSQL 16 integration tests cover
forged projection rejection, preferred A/B selection, the same selection in
Planner and cutover, bound-A validation after B is appended, credential-health
revision separation, stable-tuple drift, typed SSE parsing and the two-stage
Admin/UI flow. The frozen local gate on 2026-08-27 passed `go vet ./...`, the
complete Go unit/race suite, all Admin integration tests, the exact M88
migration/routing/revalidation/reset/bootstrap integration selector under the
race detector, all 83 frontend test files / 648 tests, the production frontend
build, bundle budget and `git diff --check`.

The exact 08d artifact was then deployed non-rolling to development/test after
an isolated M87→M88 rehearsal and a drained database backup. Stable, canary,
frontend and Nginx passed zero-provider-call health/readback. The Admin UI ran
the typed streaming probe once. The provider returned HTTP 200 in 984 ms, but
the typed stream contract rejected the response; no approval or qualification
was created, and no ordinary streaming request followed. This is supplier call
3/20. Customer wallet, transactions and prior Freeze counts remained unchanged.
The deployed validator retained only the generic
`provider_stream_contract_invalid` category and discarded partial parse
metrics, so the historical receipt cannot prove whether its live root cause was
line endings, terminal usage ordering or another typed-contract mismatch.

The subsequent parser stabilization accepts the three SSE-standard line endings
CRLF, LF and CR while retaining the narrower DeepSeek data/comment field
contract. It preserves a bounded typed partial observation—sanitized model,
terminal and usage fields when parsed, plus byte, event and delta counters—and
an enumerated error category in the immutable receipt and Admin UI; raw provider
response content is never stored. Deterministic unit, Admin PostgreSQL, OpenAPI
and UI tests cover the success and typed-invalid paths. The exact 4c6 Target UI
then ran supplier call 4/20 once. Probe 2 recorded provider HTTP 200 in 922 ms,
12 data events, 11 deltas and reported model `deepseek-v4-flash`, but correctly
retained failure evidence `usage_mixed_with_choices`; no approval was created,
no ordinary streaming request followed, and the CNY wallet and transaction
count remained zero.

Current DeepSeek documentation now states that no separate usage-only chunk is
emitted: final usage rides on the last single-choice chunk, which has no new
content and a non-null `finish_reason`, immediately before `[DONE]`. The live
shape and official contract therefore agree; the immutable M88 `@1.0.0`
contract is historical rather than wrong evidence. The bounded local M89
candidate preserves `@1.0.0`, adds `@2.0.0` for the current final-choice usage
shape, and moves only the exact Flash Chat current bindings to `@2.0.0`.
Versioned parser dispatch prevents either receipt version from being
reinterpreted by the other. M89 migration up/down/reapply, typed positive and
negative parsing, Admin probe/approval, preferred-receipt and frozen-receipt
integration tests pass locally. This remains `locally_conformant`: M89 is not
deployed, and no `@2.0.0` live probe or approval exists yet.

An additional broader-than-CI integration invocation was not used as release
evidence: unchanged historical health/runtimechange tests attempted to create
database states that existing constraints reject (a disabled deployment
retaining a probe lease, an expired lease identity and a mutable deployment
identity tuple). The repository's actual CI selectors do not include those
legacy fixtures. They are recorded as separate test-suite debt, not silently
called green and not expanded into M88. The parser stabilization remains
`locally_conformant`; the M88 base is Target-deployed, while successful live
probe, approval and the ordinary streaming request remain separate unmet
evidence steps.

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

At that milestone the current/new-attachment pointer selected
`deepseek.v4-pro.api@1.4.0`; it is now historical and the current append-only
Profile 1.5 correction is described above.
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

At that milestone the current/new-attachment pointer selected
`deepseek.v4-flash.api@1.4.0`; it is now historical and the current append-only
Profile 1.5 correction is described above. Historical `@1.3.0` remains addressable with its
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
- Current DeepSeek Flash/Pro `@1.5.0` use the immutable v5 reasoning/tool-choice
  composition over the reviewed Chat v4 history contract: `low/high/max`, with
  `medium/xhigh -> high` compatibility while preserving the original wire
  value. Under active/default thinking, omitted choice and redundant explicit
  `auto` preserve provider-default selection while explicit
  `none`/`required`/named choices fail closed; with thinking explicitly disabled,
  non-stream `none`/`auto`/`required`/named choices remain available. Unknown
  effort, disabled thinking plus effort, ineffective sampling and missing
  reasoning replay in assistant history of an active-thinking tools request
  fail closed. Earlier exact versions retain their historical restrictions;
  they are not reinterpreted.
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
| DeepSeek V4 Flash | Chat, native Responses and Anthropic surfaces; thinking toggle/effort; tools; JSON; prefix completion; FIM; Files; KV cache. Vision belongs to the separate `deepseek-v4-flash-vision-exp` model | current `deepseek.v4-flash.api@1.5.0`; standard OpenAI-compatible Chat; typed `low/high/max` reasoning plus `medium/xhigh -> high` compatibility; with active/default thinking, omitted choice and redundant explicit auto use provider-default selection while explicit none/required/named fail closed; explicit none/auto/required/named are available only with thinking disabled; single Plan declared/selected-function authority; exact per-choice tool response and function-history validation; active tools requests conservatively require non-empty reasoning replay for all assistant history; standard-surface strict/parallel rejection; profile-bound cache hit/miss usage; versioned 1M/384K provider limits; the bounded Chat subset has development/test public UI/SDK evidence and retained Release 23. Historical `@1.2.0/@1.3.0/@1.4.0` remain addressable | streaming tools; beta strict tools; broader live multi-turn/operation conformance; prefix beta; full Responses/Anthropic operations; Files/FIM; separate vision model qualification; pricing/customer billing and Production release |
| DeepSeek V4 Pro | Chat, native Responses and Anthropic surfaces; thinking toggle/effort; tools; JSON; prefix completion; FIM; Files; KV cache | current `deepseek.v4-pro.api@1.5.0`; its own immutable reasoning identity models `low/high/max` with `medium/xhigh -> high`; active/default thinking rejects explicit none/required/named and normalizes redundant explicit auto to provider-default omission, while the reviewed standard Chat v4 tool-choice subset remains available with thinking disabled; one Plan declared/selected-function authority, exact response/history consistency and conservative active-tools reasoning replay; profile-bound cache hit/miss usage and 1M/384K provider limits remain; the exact Chat Core tuple has nine Target-qualified capabilities, fixed-SDK delivery evidence and model-local stable Release 24. Historical `@1.2.0/@1.3.0/@1.4.0` are addressable | tool streaming; beta strict tools; broader live multi-turn conformance; beta/native/Files/FIM operations; pricing/customer billing and Production release |
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
