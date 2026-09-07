# Qwen streamed-tools exact contract supplement

Pre-code review object. Base main3332a869, attached to
PLATFORM-MODEL-ONBOARDING-20260907.md. This supplement covers only Qwen3.7-Flash
on the existing ordinary PAYG DashScope Beijing shared/workspace Chat surface.
No Qwen production implementation changes have been written at this point.

## Active milestone

Make PublicModel3159 qwen3.7-flash execute one real OpenClaw streamed multi-round
function-tool task through a new immutable data Profile, precise qualification,
sealed Release and Slot. Preserve Deployment40 and its original Operation.
Then deliver another data revision on the same image to verify the complete
no-rebuild revision path. Model answer quality is separate from protocol success.

## Verified implementation gap

Compiled qwen.3.7-flash.api@1.7.0 references
protocol.qwen-openai-chat.flash.v6. It models tools and streaming separately but
has no FunctionToolDeltas / exact tool-history and response contract. No typed
Qwen data Probe composition exists. Existing generic Operation is insufficient.
No request was sent merely to diagnose this static gap.

## Exact planned changes

1. Add protocol.qwen-openai-chat.flash.v7 as a new immutable typed implementation.
   Derive from Qwen's established text/tools contract, not a MiniMax/GLM provider
   envelope. Preserve max_completion_tokens/max_tokens mutual exclusion and the
   documented 131072 output ceiling. For this initial execution subset accept
   omitted/auto tool_choice, exact function tools and tool-result history;
   required/named/parallel, structured output, search and media remain excluded.
   Keep Qwen's function-name/description/parameter constraints explicit. Enable
   exact tool response/history and invocation capability checks. Stream facts
   use the existing bounded SSE/tool-delta mechanics: content/reasoning_content,
   index/ID/arguments, tool_calls/stop/length, terminal usage, required DONE.
   No new parser, model-name inference or provider envelope normalizer.
2. Reuse reasoning.qwen3.7-hybrid.v1 and cache-usage.qwen-context-cache.v1.
   enable_thinking is a boolean; thinking_budget is separate from output and
   bounded by its existing contract. Preserve-thinking is not inferred from
   another provider. Existing optional cache counters retain unknown/absent
   semantics; no fabricated zero or cache-hit claim.
3. Add one typed Probe implementation
   unirouter.capability-probe.qwen-chat-stream-tools.v1@1.0.0.
   Its data binding accepts only provider dashscope, the new protocol revision,
   Qwen hybrid reasoning and exact Qwen cache contract. It validates immutable
   definition identity/provider/upstream/operation using existing BindDefinition.
   Capabilities: streaming, reasoning, reasoning_budget, tools, function_tools,
   tool_choice_auto (use exact existing capability constant strings).
   Request: one existing echo function, auto choice, stream/include_usage,
   enable_thinking=true, thinking_budget=1024, max_completion_tokens=1024.
   Response uses validateChatStreamFunctionTools, requiring the echo arguments,
   reasoning observation, final provider usage and proper termination.
   A bound check of 1024 is evidence of budget request acceptance, not a claim
   that all possible budget values change reasoning quality.
4. Register that exact Probe in the existing builder/validator dispatch and
   SupportManifest generated from the real implementation registry. Do not
   add a model list to leases, broaden existing Probe IDs or change old contracts.
5. Publish qwen.3.7-flash.api@1.8.0 as data, provider dashscope / cn-beijing /
   openai_chat_completions / same upstream product and endpoint classes.
   Keep exact public hard limits and provider-default authority. Capability
   list is limited to the six qualified facts above. Product lineage remains
   the existing Qwen PAYG product; no dated PublicModel or schema migration.
   The first data definition has no fabricated predecessor digest; its compiled
   predecessor is documented explicitly. A following revision pins its actual
   prior definition digest. Use official CI Bundle carrier and single materializer.
6. Create a separate candidate Deployment with published definition digest,
   existing Connection14 and explicit binding14. Probe, approve each exact fact,
   author/seal Release, activate only this model's Slot. Historical Deployment40,
   Profile1.7.0 and other models/releases/slots remain intact.

## Tests before and after implementation

Already passed before this supplement: existing modelprofile,
modelcapabilityqualification and adapters baseline package tests. First UI slice
has 58 passing frontend tests and targeted discovery/OpenAPI/isolated-DB checks;
those are not Qwen execution evidence.

Required: immutable old/new contract comparisons; accepted streamed tool history
and rejected unsupported combinations; exact data-Profile/Probe compatibility
and rejection of other provider/reasoning/usage implementation substitutions;
bounded Probe request; shared stream fixtures including fragmented arguments,
repeated IDs, missing DONE/usage, malformed JSON, wrong model and tool results;
Runtime continuation and terminal usage behavior using the new exact Profile;
SupportManifest requirements; real canonical create/release/preflight paths with
no mutation of legacy targets or active releases. Reuse shared negative fixture
coverage where its exact code path is unchanged, add Qwen composition coverage.

Before paid execution, capture the actual OpenClaw SDK request with zero-send,
run request preflight, enforce explicit experiment output/thinking limits, then
run the task. Persist Request ID, exact definition/release/artifact, each Provider
attempt, terminal state, usage, settled observation, fallback=false and quality.
One new data revision then repeats this complete path on the same image; do not
call this proof for brand-new-model bootstrap or retire the old writer.

## Budget and scope

This batch has 30 upstream requests / CNY5 per exact submodel, combining both
logged-in portals, protocols, retries and direct diagnostics. Reserve before
sending. Metadata discovery is not a generation or qualification call. All prior
experiment counters remain. Use a bounded prompt and explicit output/thinking
caps; conservative price bound CNY4.8 per million tokens for all token categories
is above all currently documented Beijing Qwen3.7-Flash token tiers. Missing
usage retains the full request reservation. Provider ambiguity never retries
automatically. No admin credential/role/session changes or fallback.

Sources verified 2026-09-07:
- https://help.aliyun.com/zh/model-studio/qwen3-7-flash
- https://help.aliyun.com/zh/model-studio/qwen-function-calling
- https://help.aliyun.com/zh/model-studio/list-models

Known blockers: Qwen's exact streaming-tools contract is not installed and there
is no qualified Release. The actual request and provider response still need
bounded live validation. Native Messages and other Bailian model families are
subsequent exact supplements, not covered by this review object.

## Pre-code review adjudication

Independent read-only review bound to SHA256357bf71f4d36f48983b52c168ac86fcd51e9764c4efce83d30bb8ef8532ce0a7.
Main verified the strict shared parser and official Chat parameter/response
examples; no Provider request has been sent. Accepted required adjustments:

- Replace the prohibition on any envelope preparation with finite, v7-only
  Qwen preparation. Null system_fingerprint/service_tier/logprobs/function_call/
  refusal/role/tool_calls represent no extra fact and may be omitted; non-null
  unmodeled metadata remains rejected. Non-null assistant role and actual tool
  deltas remain intact. This is a documented generic DashScope Chat shape, not
  yet observed Qwen3.7-Flash evidence. Reuse the shared parser/state and the same
  preparation in Probe and Runtime; keep historical protocols unchanged.
- Define an empty continuation tool ID as no new identity in v7's preparation.
  The shared state still requires the first complete nonempty ID/type/name,
  exact repeated nonempty identity, unique IDs and valid indexes. No synthesis
  or name inference. This is a bounded new-contract interpretation; SDK/native
  examples are not claimed to prove the exact Flash wire. Name/type null is
  not silently normalized without corresponding wire evidence.
- max_completion_tokens is thinking plus answer; use 2048 in the Probe and
  thinking_budget1024 so a tool answer has space. The existing alternate
  max_tokens controls answer only. Reserve input byte/token upper bound plus
  max_completion_tokens+10 for this Probe; for an explicit max_tokens request,
  reserve answer bound + thinking bound +10. Multiply by CNY4.8/M, a bound not
  below any documented Beijing tier. A length finish is a failed qualification.

No need for a new parser, Publication writer or execution authority was found.

### Implementation candidate and exact verification

The admitted changes are in the v7-only envelope preparer, typed data Probe
composition and Profile1.8 Bundle. Strict JSON and exact model identity must pass
before independently valid supplier usage may survive an unrelated delivery
error; such a frame is not delivered. Gateway tests cover successful tools and
answer delivery, missing DONE, invalid metadata with usage, continuation history,
explicit thinking disable and exactly one settlement callback. The shared parser
and old protocol implementations retain their previous acceptance boundaries.

The Qwen budget capability now has an explicit management/UI/API presentation.
The actual data Probe handler records `enable_thinking`, `thinking_budget` and
`max_completion_tokens` without prompt or private reasoning text. The safe
summary is not a new pricing authority. RuntimeSupportManifest tests require both
new Chat and Probe implementation IDs, even at the same M114 schema/epoch.

The draft Bundle is 7,552 canonical bytes, one ProfileDefinition digest
`e0cea94fc6780d101c7ff547c778259f0515c084fd574322d84a79ffc1132b0a`,
six binding appends and six absent-to-current selectors. Source validation is
not an official CI carrier, Target publication, qualification or Agent result.
All new paid-call counters are still zero before Target delivery.
