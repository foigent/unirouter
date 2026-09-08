# Concentrated qualification GUI — 2026-09-09

Cutoff: 01:50 China time. This verifies the paid shared-Probe, per-capability
results and refresh path on the existing development Target. It does not close
the whole approved onboarding/product-optimization workstream, rerun an Agent,
or exercise a new Approval/Release activation.

## Exact pre-state and one paid action

The user restored UniRouter console login. Chrome displayed development1.2.76
at `https://8.130.18.85/channels?deployment_id=43&operation_id=openai.chat_completions.v1`.
No administrator credentials, roles or session settings were read or changed.

Read-only invocation `t-wl06wgw10ptzx8g` confirmed instance
`i-0jl0x8x34xgaake6li28`, DB system7655821080247336993, M115 and both healthy
counter-v3 backends at commit `6a89eef6be75fc1a767e0258fa6c84109cef7924`, image
`sha256:3714ea67f910b0dc77042bf3985ca7142565b05d0a23efb73e54a4961ff9fb91`.
Flash Release59/Slot3 and snapshot
`75b94aa6b080dee5b85d99324f0b4490721d59c68a5bfc9e17c935be2934aa99` were unchanged.

The GUI selected existing credential binding14 for deployment43, exact Profile
`qwen.3.7-flash.api@1.9.0`, Protocol `protocol.qwen-openai-chat.flash.v8@1.0.0`.
Opening the panel loaded the plan and current facts without a Provider call.
It stated one paid request for six capabilities; Verify was disabled before
explicit confirmation. The existing cumulative guard reserved one call as
`concentrated-gui-20260909-flash-01` before the checkbox and Verify click.

The fixed request uses stream=true, tool_choice=auto, enable_thinking=true,
thinking_budget1024, max_completion_tokens2038 and include_usage=true. Exact
Probe contract is `unirouter.capability-probe.qwen-chat-stream-tools.v2@1.0.0`;
request digest is `80c9cd315361b6ce20e15c88f369d5c69e06e81390876d05098ae4b097bf4993`.
No paid retry or additional Operation Probe was sent.

## Observed result and refresh

One physical observation: `op_0d2775e36a1708fdae770862e15f2c2f`.
Plan digest: `70d8924fcd43df45da2fd3b91d4afdf4834e289e26ac782595b9f63c423fcdd2`.
All six new facts have HTTP200/success=true and the SAME source_ref:

| Probe | Capability |
|---|---|
| 259 | function_tools |
| 260 | reasoning |
| 261 | reasoning_budget |
| 262 | streaming |
| 263 | tool_choice_auto |
| 264 | tools |

The response summary records dispatch_state=response_received,
outcome_ambiguous=false, tool_call_observed=true, reasoning_observed=true,
finish_reason=tool_calls and provider-reported usage305 input/188 output/493 total.
This is ONE usage observation, not six charges. It is a capability Probe, not a
full tool-result continuation/Agent answer-quality or customer-settlement test.

The GUI displayed the shared observation, six new Probe IDs, and a disabled
Verify button after the paid confirmation reset. Clicking “刷新逐项状态” retained
the same IDs and observation. Readbacks `t-wl06wgw5dfsnklc` and
`t-wl06wgwahbyjoqo` confirmed six added rows but only one added physical source_ref;
after refresh the last ID remained264. Counts:

| Authority | Before | After / after refresh |
|---|---:|---:|
| Capability Probe facts | 258 | 264 |
| Capability Approvals | 235 | 235 |
| Releases | 39 | 39 |
| Slots | 17 | 17 |
| Publications | 13 | 13 |
| Gateway billing ProviderAttempts | 124 | 124 |

Admin capability Probes do not create Gateway billing ProviderAttempts; the
unchanged124 count is not evidence of zero paid calls. The shared physical
observation is the call-count authority for this verification.

Existing six independent Approvals still reference Probes247–252. They were not
rebound to259–264; no approval or activation button was pressed. The current
qualification column still says “已批准”, while its observation column shows the
latest Probe. This is existing valid qualification, not automatic approval of the
new observation. The next UX change should expose the approved evidence reference
separately from the latest observation; this run does not verify creating a new
Approval through the UI.

The client section displayed the correct gateway `/v1` URL, stable model and an
application-key placeholder. Its OpenClaw command is dry-run by default and uses
the existing sync tool and actual application-key contract. No key was exported,
no local client configuration was written and no global default was changed.

## Budget

Flash original pool is now30/30 calls and conservative reservedCNY4.95651/5;
Max remains18/30 andCNY4.689972/5. Reservations are not supplier invoices.
The new user authorization adds30 calls/CNY5. It is conservatively recorded as
one shared additional pool for the ongoing authorized tests, not per-model
expansion. None of that additional pool was used; original histories/guards were
not reset or rewritten. Further calls require a guard that accounts for both the
retained original history and the explicitly allocated additional pool.

Raw official readback JSON and exact read-only scripts are retained under
`/private/tmp/unirouter68-exact-source-20260908/gui76-{before,after,refresh}-*`.

## Advisor assessment and bounded next work

The late compatibility diagnosis is supported by the retained74/75/76 failures.
Fail-closed protected traffic, but did not prevent avoidable Artifact rework.
The existing code now handles exact Docker missing-object diagnostics and finite
RFC3339Nano timestamps; its real carrier CI uses Python3.10 and final TCP
PostgreSQL readiness, including lost-response cleanup. That is partial progress:
the job still follows image construction and its recorded Docker28 differs from
Target29. Do not describe a pre-build environment gate or Docker29 CI as delivered.

| Priority | Next bounded task | Acceptance / value |
|---|---|---|
| P1, before the next affected Artifact build | Reuse the formal rollout tool and test suite to check supported Python/Docker behavior before expensive build/transfer; split image-independent parsing/environment checks from the new-image CLI test | Python3.10 and the supported Docker28/29 boundary are explicit; absence, daemon errors, timestamps and cleanup fail closed; retain a real new-image carrier check after build. Do not move a test requiring a candidate image before that image exists, add a second owner, or replace the current required checks |
| P1, existing onboarding scope | Make the saved connection → selected models → verification → approval → activation/client setup path clear, including latest observation versus approved evidence | Reuse the saved Key; show per-model next action and exact evidence; no automatic paid retry/approval/activation; use existing formal APIs. Verify new-approval UI separately when it is actually needed |
| P2 | Classify CI by actual dependencies and report current slice versus overall scope | Prove routing for add/delete/rename and preserve required coverage before reducing work. Record Artifact rebuilds, physical calls, user actions and time to Agent result; no new statistics platform |

The314 unauthenticated short-request observations retain their original scope.
The cancellation remains estimated cost_observation with one terminal, not a
supplier invoice or positive customer debit. Historical v2 output29 versus v3
output10 came from different partial text and cannot demonstrate savings. The
final76 v2→v3 change was same-image; the full counter batch included74/75/76 builds.

Experience: revise existing MAR-008 with the already implemented finite parsing
and real carrier/recovery checks. The proposed pre-build gate and revised approval
presentation remain tasks, not active lessons, until executable enforcement and
verification exist. No new architecture project or full model retest is warranted.
