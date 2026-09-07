# Qwen3.7-Max: first model from a data Definition

Verified development/test cutoff: 2026-09-07T08:34Z. The first-model delivery and
OpenClaw protocol acceptance passed. Answer quality did not pass. This is one
real new-model batch, not completion of the two-batch old-writer retirement gate
or of planned-change continuity.

## Fixed baseline and independent carrier

The compiled Catalog at platform merge
`437497de51090dc482b29fe0e4a4fb3cd58759ee` has no `qwen.3.7-max.api`.
PR #402 adds only `capability-bundles/qwen-max-first-model.json`, using existing
typed implementations. Merge `efde443db214b2984c25401c68b2e4b79e67bffc`, tree
`da05dc03edb11ab6b3f32b6de7af70f7195dd4f6`, required CI `34094200014` passed.
The independent official carrier is CI `34096786915`, Artifact `10008904826`;
the existing publisher verified repository, main commit, provenance, archive,
tracked Git blob and canonical Bundle bytes.

- Bundle: `9cf6eeb9cd23dfee536eaf62bb381bf1e8ec3ab2fcaa098280927f162a8a2577`.
- Definition: `812462e1c6eaf903a9ad9f523b2479756e1591ed847cf0cd649bb9826dd35fdd`.
- Publication: `development-qwen-max-first-model-20260907-efde443d`.
- Plan: `559c7784e2e063dfd001850de0541ab16d8bfbf8905c738b286141dde88e1335`.

Target `cn-wulanchabu/i-0jl0x8x34xgaake6li28`, DB system identifier
`7655821080247336993`, stayed on 1.2.65/M114. Both backends retained image
`sha256:8966709abee4bfeebc2728443439c18b9efaad6e3da3eb89d4b73495eadef993`.
Publication changed no existing Release, Slot, binding, selector or Definition
row; it added one Definition and six bindings/selectors. M100–M113 history stayed
intact. No backend rebuild, rollout, DDL, parser or Go Catalog change occurred
from Max Publication through the final Agent request.

The retained 1.2.64 Artifact also passed offline `check-bundle` with the exact
official bytes and isolated required startup configuration. This is compatibility
validation, not a claim that Max was executed on 1.2.64.

## First creation and qualified activation

Through the ordinary UI, the published Definition first created PublicModel
3252 (`qwen3.7-max`) and Deployment 42, reusing Connection 14 and credential
binding 14. Profile `qwen.3.7-max.api@1.0.0`, lineage `dashscope/qwen3.7-max`,
protocol `protocol.qwen-openai-chat.flash.v7@1.0.0` are exact references; the
historical implementation name does not select models by name or fallback.

Operation Probe 39 and capability Probes 241–246 all returned HTTP 200.
Approvals 218–223 independently qualified function_tools, reasoning,
reasoning_budget, streaming, tool_choice_auto and tools. The six capability
requests observed tool calls, reasoning and final provider usage. The operation
Probe has no retained usage; missing usage is not treated as free usage.

Release 58 (`rcr_deployment_20260907t081241_15586f086e87b1cfc9e7c8e9`), authoring
receipt 39, pins the exact Definition and six evidence/approval decisions.
Execution snapshot digest:
`27c4e7e5e1a5b7f885e9ec1da3906c302fdddf51179603c9571ac98c15580659`.
Lifecycle receipt 55 activated the first Slot, version 1; one route, no canary
or successor. A model-restricted application Key 104 reused the existing ordinary
OpenClaw application grant; its value was saved only in OpenClaw Secret Store.

## Real OpenClaw result

Actual SDK preflight returned HTTP 200 and locally admissible for the exact data
Profile, with zero Provider calls. The isolated client then used HTTPS, the
application Key and stable model name, with stream/tools/auto/tool history and
an explicit 8192 total-output experiment limit. The existing global client
configuration and default model were unchanged.

| Request | ProviderAttempt | Final provider usage | Terminal receipt |
|---|---|---|---|
| `op_90649529fab960489538aed7e386e7f4` | `att_a120d3cdcfdce17e57a380f588c7cfdd` | 6246 input / 189 output | 271 |
| `op_40949ec42b1518f27f79a0793b4af48f` | `att_57154e7cb013a2a6e52d556ed13503dd` | 19122 input / 1584 output | 272 |

Both requests have exactly one settled attempt, no authorized retry, one final
cost-observation receipt and completed delivery. Usage journal equals terminal
SSE usage. Both bind Release 58 / Slot 1 and runtime
`uri_83155ac8dcebc97fef4d2f7b9a6af702` on the fixed image. Request route snapshots
remain separate per-request identities. The client executed three successful
read tools; the second request returned all three source files byte-for-byte.
The final stop completed without fallback in 50.454 seconds.

Answer quality: **failed**. Max correctly identified the release-bearing paths
and VERSION/CHANGELOG/frontend package/lock requirements. It incorrectly said
the resulting patch would skip all build/test jobs: VERSION falls into the
supplied CI's fail-safe arm and frontend metadata triggers frontend/docker.
No Gateway behavior or release gate was changed to improve the model's score.

The cumulative exact-model budget is 9 actual calls (7 qualification + 2 Agent),
with CNY 2.626116 conservatively reserved against 30 calls / CNY 5. It is not an
invoice. Gateway cost-observation zero charge and OpenClaw's zero cost estimate
do not establish zero supplier cost. No reservations were reset or refunded.

## Reuse metrics and next work

This model batch added zero production Go lines, zero parsers, zero DDL and zero
backend releases. First PublicModel creation to final delivery was approximately
36m45s, including manual qualification, approvals, activation and client setup;
Agent execution alone was 50.454s. No complete user-action count or comparable
repeatable onboarding-time baseline was captured, so those goals remain open.
PR #401's first platform implementation cost is separate from this model batch.

Next: consolidate formal rollout planning/recovery; diagnose the retained HTTP/2
failure and verify withdrawal of the backend carrying a live stream, its drain
and stop, plus cancellation. Then reduce onboarding actions and deduplicate
exactly equivalent paid observations while keeping approvals independent.
Horizontal model expansion and old-writer retirement remain deferred.

Local evidence: `/private/tmp/unirouter-qwenmax-first-model-20260907/agent-audit.json`,
redacted HTTP/SSE captures, cumulative guard and exact tool-result comparisons.
Target publication evidence:
`/opt/unirouter/deployment-evidence/20260907-qwen-max-data65`.
Readback invocations: `t-wl06wbxysq30b9c` (active identity),
`t-wl06wby6f95roxs` (attempt/usage/delivery/settlement/image).

经验文件无需变化. Existing exact-Definition, qualification, frozen execution and
bounded-call checks enforce the applicable lessons. The current HTTP/2 symptom
is not admitted as a model-adaptation lesson without an established cause.
