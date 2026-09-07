# Qwen data Operation Probe correction

Base57de16eaa5cbe4fa50a848e1e4156b9766cf657d (1.2.63/M114). The active milestone
is the Qwen3.7-Flash Bundle → qualification → Release → OpenClaw tool loop.
The independently reviewed pre-code object has SHA256
3db9ac004045addfc610ee60b38d045a0d218827d4d62a7087ab68e6e35649c4.
The review found no additional blocker; exact boolean shape, safe summary,
actual handler persistence and matching operational budget were required and
already included. This document records that bounded design and adjudication.

## Observed boundary

On development/test1.2.63, the formal Qwen Bundle committed without changing the
image/schema or prior35Releases/15Slots. CandidateDeployment41 uses the published
qwen.3.7-flash.api@1.8.0 Definition and existing Connection14/binding14. Its
Operation Probe rejected before Provider I/O because the data request builder
only encoded thinking.type, whereas the existing Qwen reasoning implementation
uses a top-level enable_thinking boolean. Successful capability probes cannot
substitute for the required independent operation/binding evidence.

## Change and invariants

Extend only the data connectivity builder's typed boolean branch: exact field
and root path, boolean wire type and allowed disabled value are required. Send
false and the protocol-owned total output limit64; do not inherit the provider's
reasoning default. Unknown shapes remain unsupported. Legacy and thinking.type
branches retain their behavior. The request summary decodes an optional boolean
so false is retained, without storing prompt or credential content.

No model-name parser selection, JSON policy, new Profile/Bundle identity, DDL,
pricing lifecycle, Release/Slot mutation, fallback or legacy writer retirement.
The existing Definition and candidate remain reusable after the platform fix.
The extra Artifact is a real limitation of this batch's no-rebuild acceptance.

## Validation and paid execution boundary

Unit tests cover an uncompiled data revision, exact request/safe summary and
identity/protocol/upstream rejection. The existing family integration test now
covers Qwen candidate ProbeAvailable, the actual operation handler through a fake
HTTP transport, one dispatch, persisted exact data pins and false control, and
unchanged legacy operation rows. Its failed/successful capability probes continue.
The existing GLM data/legacy operation regressions remain applicable.

The local experiment guard is a separate reviewed operational artifact. It must
reserve before a UI call using the exact installed payload, model/contract pins,
30-call/CNY5 aggregate and unknown-outcome retention. The new operation allowance
only accepts max_completion_tokens64, enable_thinking=false and the fixed text;
the old max_tokens1/default-thinking shape cannot use that smaller bound.
No live Provider call is part of Local test evidence.

Official references checked2026-09-07:
- https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions
- https://help.aliyun.com/zh/model-studio/qwen3-7-flash

After this fix, complete real Qwen qualification and Agent evidence before
wider platform coverage. The next architecture acceptance remains first creation
of a genuinely uncompiled PublicModel from a published Definition; an artificial
Qwen revision is not a substitute. MAR-001/MAR-007 already cover the enforcement;
经验文件无需变化.
