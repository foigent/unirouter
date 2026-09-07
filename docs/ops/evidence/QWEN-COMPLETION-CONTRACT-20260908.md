# Qwen completion contract: development/test acceptance, 2026-09-08

Exact main ac3e7600c5ed1ace59a475b262ad95d141be98a1/tree c4e9e1ef205d38649329e7729cedc93d06d42f19,
1.2.70/M114. Backend sha256:79d2c0b6e5d754260595acc6f616955aa05544cc5cef9dd60ed774c03bacde32.
Flash Profile1.9/Deployment43/Release59/Slot3; Max Profile1.1/Deployment44/Release60/Slot3.
Both use typed protocolv8/Probev2. Target i-0jl0x8x34xgaake6li28, development/test only.

## Result

Both real installed-OpenClaw tasks completed two HTTP200 streamed rounds, two real
read tools, exact file result history, terminal tool_calls then stop/DONE, final
provider usage, one attempt and one settled cost-observation receipt per request.
Both answers correctly identify historical VERSION1.2.48 and docs/architecture/*
requiring VERSION/CHANGELOG changes under that historical script. Han character
counts52/56 satisfy the180-Han limit. This bounded task is simpler than older failed
CI-analysis tasks; it neither erases those failures nor proves general model quality.
No fallback. Flash10.816s/Max16.827s includes client startup and tools; no direct-path
performance comparison or SLA claim. Exact request/usage/receipt rows: shown below.

| Round | Request | Finish | Provider input / completion | Receipt |
|---|---|---|---|---|
| flash 1 | `op_24c99d6a0c70c7228e95f9d4bdcbd7b7` | tool_calls | 6242 / 144 | 279 |
| flash 2 | `op_ff98298491a8d8868822b43b52795292` | stop | 6758 / 248 | 280 |
| max 1 | `op_3629b7bf12ef99578e18305c978a51b7` | tool_calls | 6242 / 172 | 281 |
| max 2 | `op_dbf9096546b00eb28a7133b08ad8b632` | stop | 6786 / 357 | 282 |


Client migration: exact70 sync dry-run/write in isolated configs preserves the root
configuration and8192 experiment override; max_completion_tokens8192 maps to provider8182.
Both generated SDK first and tool-continuation shapes preflight successfully.
Qwen off/low menus represent disabled/enabled thinking, not native low/medium/high
intensity. thinking_budget remains distinct. Legacy max_tokens is refused before
Provider I/O; generic public diagnostic is a remaining migration defect tracked in
QWEN-CLIENT-MIGRATION-20260908, not declared complete.

One local harness start was rejected before reservation/Provider I/O because the
old experiment guard omitted enable_thinking. The guard now opts in only exact
Qwen IDs/new output field/enable_thinking=true. Original request bytes, price/call
caps and retry fence remain;40 offline tests pass (3 localhost tests initially
sandbox-denied, then all8 shared tests passed with loopback permission).

## Cancellation and finance boundaries

New-v8 cancellation op_22779cdfb7645dfd10f3e3d80bcba983 received4 client chunks1013B,
then explicitly canceled. Target wrote11chunks2811B before disconnect propagated;
these are different observation points. Target has one attempt
att_7e918a6df2d792520c7272fcb72c0ec9, one interrupted terminal, one Freeze, one
estimated gateway_counter receipt283, and one settled journal (input1154/output29,
counter_v2). No retry/fallback or duplicate terminal. Final checkpoint matches
live counts. This is graceful cancellation, not a crash recovery experiment.
Crash/stale-owner/commit-ambiguity/known-final-overrun recovery remain covered by
exact PR408 local real-DB tests, not a forced official Provider overrun. The chunk-
sensitive counter remains a separate P1 correction; no historical bill rewrite.

These applications use cost_observation: UR calculated/charged amounts are0, not
proof of zero supplier cost or a priced customer-billing Target trial. Supplier
invoice is unavailable. Cumulative conservative experiment reserves, never reset:
Flash28/30,CNY4.856140/5; Max18/30,CNY4.689972/5. Reservations, final usage and
supplier invoices are distinct evidence. No more paid calls are needed for this slice.

Prior69 loaded-backend withdrawal/drain/stop plus44 qualified H1/H2 observations
remain separately bound to that image/task;70 formal rollout passed short requests
only. No claim of every client, unlimited streams, Production or a fresh Agent task
spanning a70 rollout. Both Publications/models reused the same70 binary/schema after
this new typed implementation; the70 platform batch itself is not a no-build batch.
Earlier65 Max first-uncompiled-model proof remains.

## Evidence

`/private/tmp/unirouter70-qwen-acceptance-20260908/agent-target-evidence.json` and
`/private/tmp/unirouter70-qwen-acceptance-20260908/cancel-target-evidence.json` decode official readbacks
t-wl06wdsuya4hla8 and t-wl06wdt0709b6dc. summarize.py verifies one attempt/Freeze/
terminal receipt/settled journal per request, matching supplier usage and exact
read-tool results. Per-model files retain isolated config, sync, zero-send/preflight/
real requests, raw SSE, redacted Agent stdout and active Release evidence. No keys.
Target qualification/activation: /opt/unirouter/deployment-evidence/20260908-qwen-completion70.
Formal rollout and Publication directories retain their exact plans and receipts.

经验文件无需变化: MAR-001/009/010 cover composition, safe diagnostics and Publication
separation. Guard omission is an experiment incident; the unimplemented estimator
correction does not meet executable-lesson admission.
