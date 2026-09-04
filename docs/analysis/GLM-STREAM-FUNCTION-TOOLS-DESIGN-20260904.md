# GLM exact streamed Function Tools delivery

Status: local candidate; pre-code and sole closing review adjudicated. Live
GLM-5.3 discovery is not six-model qualification or Target activation.
Base: `116f898db0574b22a0b722f9b5d5021fd6bef512`, tree
`49bd10ce8884431c001dd949ecb1a20ed2ad30d7`, source1.2.50/M112.

## Objective and scope

Product mission remains usable, governed UniRouter. Selected workstream is
cross-client exact Chat execution, not an OpenClaw-specific SDK. The current
14-model Agent task needs a streamed tools combination, not isolated flags.
This batch qualifies the local GLM combination and makes its eventual activation
legal using the already-merged repeat-successor command. It does not claim a
live task until the exact deployment is actually exercised.

No Computer Use, media qualification, Token Plan, Pricing redesign, native API
conversion, new parser registry, Agent-ready table, combined qualification
platform, admin credential changes or Production. Historical M111 is frozen.

## Evidence and exact successors

The authorized GLM-5.3 official CN PAYG direct sample has two HTTP200 turns,
stream=true, tool_choice=auto, max_tokens=65536, include_usage=true; the second
request contains actual tool results. Stream deltas use reasoning_content,
content and indexed function calls. The terminal choice includes finish_reason
and usage, followed by DONE. Full tool arguments were observed, not fragmented
arguments. The task's final answer has an independent factual error; that is not
a Gateway defect. Private raw task/response bytes must not enter this repository.
Official shared Chat contract reference: https://docs.bigmodel.cn/api-reference/模型-api/对话补全 .

Append-only Profile successors (same product, endpoint, model, context/output,
default-output, tokenizer, reasoning, cache and media contracts):

| Profile | Prior | New | New Chat protocol |
| --- | --- | --- | --- |
| glm.5.3.api | 1.8.0 | 1.9.0 | protocol.glm-openai-chat.v7@1.0.0 |
| glm.5.2.api | 1.8.0 | 1.9.0 | protocol.glm-openai-chat.v7@1.0.0 |
| glm.5.1.api | 1.0.0 | 1.1.0 | protocol.glm-openai-chat.v7@1.0.0 |
| glm.5-turbo.api | 1.0.0 | 1.1.0 | protocol.glm-openai-chat.v7@1.0.0 |
| glm.5.api | 1.0.0 | 1.1.0 | protocol.glm-openai-chat.v7@1.0.0 |
| glm.5v-turbo.api | 1.11.0 | 1.12.0 | protocol.glm-openai-chat.glm5v.v6@1.0.0 |

Old GLM response contracts did not model full streaming delivery; do not reinterpret
those immutable identities. New current pointers select the successors. Exact
official open.bigmodel.cn endpoint only. GLM-5V retains prior media bounds, but the
new Probe/bindings prove pure-text tools only and do not admit media-plus-tools.
New tuples retain existing exact non-stream adapter preparation so they cannot
lose approved request_id normalization merely because the Profile version changed.

## Shared mechanics and role-owned policy

Reuse bounded SSE line/event assembly and FunctionToolStreamState: indexed ID/name/
argument association, contiguous completion, identity stability, duplicate IDs,
strict argument JSON,128-call/1MiB retention limits. Expected upstream model comes
from the frozen exact route/binding, never a hardcoded MiniMax name or a guessed
public model. The old M111 parser wrapper keeps its exact acceptance behavior.

Add a contract-selected parser entry for these facts. It consumes existing
ChatStreamContract delta text fields and finish rules, not a second provider
strategy directory. Strict duplicate-key, envelope, choice and typed tool checks
remain. GLM permits only role/content/reasoning_content/tool_calls deltas; reasoning
content must be a string or the existing contract-permitted null, never opaque data.
Unknown envelope siblings fail closed in the new exact stream path. No new metadata
normalization is inferred from the actual sample; existing non-stream request_id
handling stays unchanged. Do not widen old MiniMax or GLM identities.

New GLM stream policy: include_usage required, terminal authoritative usage and
DONE required, one choice index0, finish stop/length/tool_calls. Terminal usage may
share a terminal choice. Tools require complete valid arguments before tool_calls;
length with incomplete tools is delivery-invalid. Reasoning is passed through as
reasoning_content, never rewritten as answer or used as billing authority. Existing
guardrail text checks, frozen allowed tool names, usage/cache parser and failed-
delivery accounting remain Runtime authorities. Do not reuse stale parsed facts
across normalization or guardrail mutation boundaries.

Probe shares these mechanics, but separately requires the fixed echo tool/value,
reported exact model, observed reasoning and tool_calls completion. It cannot grant
capabilities from Profile claims or transfer old evidence to the new tuple. The
new `unirouter.capability-probe.glm-chat-stream-tools.v1@1.0.0` is bound only to the
six exact tuples above. Request uses max_tokens1024, explicit allowed enabled
thinking, auto, stream/include_usage and the existing fixed echo tool. The fixed
Probe is not a full Agent history task; local delivery regression and eventual
real Agent task independently cover first-turn/results/continuation behavior.

Extract the common bounded SSE consumption from M111 only where behavior is
identical; retain its optional-DONE and embedded-thinking assertions. Preserve
its error/usage observation behavior with before/after regressions. Different
Probe/Runtime accepted sets remain explicit; neither complete validator must be
merged to satisfy this work.

## Publication and activation

One M113 declaration, `000113_glm_stream_function_tools_qualification`, appends
five bindings per exact model: streaming, reasoning, tools, function_tools,
tool_choice_auto. Every capability keeps independent Probe/approval evidence;
there is no claim that one HTTP request automatically authorizes all five.
Current selectors start absent for each new tuple, with six attachment guards.
No cache or media binding is invented. Old M103/M105/M110/M111 identities and
receipts remain addressable. Static predecessor M111, schema predecessor M112.
Use the existing compiler for migrations/generated declarations and source1.2.51
gate-required metadata (subject to live main/version pre-push check).

The final exact artifact can carry M111, ordinary M112 and M113. Target planning
and writes must follow the live ledger, stop old writers before incompatible
schema/apply, and generate each next exact Plan only at its valid predecessor.
No intermediate artifact is required. A new Target successor is prepared with
Probe/approval/sealed Release and no public Slot, then the existing controlled
cutover switches the stable request identity atomically. Repeat mode is required
where a primary Alias already exists. No direct SQL bypass or dated ordinary
request identity. First representative GLM-5.3 task precedes the remaining five.

Integration clarification: the existing Plan may include ordinary M112 before
M113 once the independent M111 Publication has been applied. The regression
covers that valid pending range and also plans M113 after explicit M112; it does
not impose a new restriction on the publisher or merge Publication identities.

## Verification and completion

Use constructed, clearly labelled fixtures from the observed shapes; no paid
negative-case generation. Parameterize six exact tuples through request/default/
explicit output/admission/outbound, tool-result history, Probe and actual relay.
Cover reasoning and answer, multi-tool and fragmented arguments, same-frame finish/
usage/DONE, absent DONE, wrong model/choice/type/index/name, duplicate keys/IDs,
unknown fields, null versus absent, undeclared tool, malformed/truncated arguments,
usage missing/invalid, trailing exclusion, cancellation and failed-delivery usage.
General numeric-invalid/bounds logic stays in shared tests; successor selection,
default65536 where applicable, explicit1024/8192 and context-exceeding max are
connected by exact-profile regression without a fourteen-model Cartesian product.

Publication tests cover exact existing M112 predecessor, guarded Plan no writes,
apply/readback/down/reapply, old binding preservation, invalid predecessor and
retained new-Profile down refusal. No blanket old fixture rewrites to hide failures.
Run targeted WIP checks, affected race/integration and project gates at stable diff,
one bounded closing review, exact PR revision CI and normal merge. Prior unrelated
baseline fixture failures remain labelled; no gate or workflow weakening.

One independent pre-code review of THIS object and at most one closing review.
Known unresolved design blockers: none before review; other GLM live behaviors
remain intentionally unqualified until independent evidence. New findings require
main-agent verification, not automatic acceptance. Scope does not expand to other
families during this batch. After its deployment resumes the existing 14-model task,
report protocol, answer quality, counts/fees and comparable performance separately.
Budget remains cumulative CNY10/30 per model, reservation is not actual spend.

## Pre-code adjudication

Independent review of pre-code SHA256
`3c8f9cc5829eb57923da921cd1950fcefebb2b38959379ca4da8ae30ac2b293d`
returned no validated blocker/required finding. Main verified the existing media
capability gate and exact non-stream identity selection. Corrected only the 5V
protocol spelling above. No old M111/M112 review was reopened. This digest names
the prior reviewed design object, not this annotated file's current bytes.

## Closing adjudication

The sole closing review found no new production blocker. Its required finding
was the current-Profile parameter matrix still expecting six historical protocol
IDs. Updated only the current expectations; maxima/defaults and historical exact
Lookup assertions remain intact. Complete modelprofile race recheck passed.
The main agent also reproduced and fixed the new GLM stream's missing frozen-tool
constraint rejection before disclosure; actual relay regressions now enforce it.

Full project race found two fixture/contract mirror gaps (OpenAPI Profile-version
enum and the current-Profile matrix), corrected with affected race rechecks; other
passed package evidence is retained. Real PostgreSQL covers M113 apply/down/
reapply and attachment rejection, and M112's round-trip starts at its proper
historical ceiling rather than bypassing M113's later-migration guard. CI adds
the M113 database regression to the existing integration lane. No third review,
Provider request, Target Publication, or deployment was used to close these gaps.
