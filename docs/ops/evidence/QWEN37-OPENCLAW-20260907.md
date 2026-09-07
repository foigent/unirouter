# Qwen3.7-Flash OpenClaw protocol result

2026-09-07 development/test readback: protocol passed; answer quality failed.
The [machine-readable evidence](QWEN37-OPENCLAW-20260907.json) contains both
requests, immutable execution/price identities and exact Target query results.

Target 1.2.64/M114, main `ce3b6f2c`, backend
`sha256:765ff70390a65207efaa1fb8f97767b74246eb638af268e1f17b518d1bcb7c14`.
Stable `qwen3.7-flash` / PublicModel3159 used Deployment41, Connection14 and
sealed Release57 / Slot1 / `qwen.3.7-flash.api@1.8.0`. Its Definition is
`e0cea94fc6780d101c7ff547c778259f0515c084fd574322d84a79ffc1132b0a`.

| Round | Request | Result | Provider input/output |
|---|---|---|---|
| 1 | `op_5a1f4906d96fccff8cb9075bfc3eec90` | HTTP200, three read tools, tool_calls | 6246 / 199 |
| 2 | `op_ff983a9881a50b70ecee1492ad7709d3` | HTTP200, final stop | 19122 / 1369 |

Both streams supplied terminal usage and DONE. All three tool results in round2
exactly match the files read. Each request has one settled ProviderAttempt, one
completed delivery and one terminal cost-observation receipt (266/267); no retry
or fallback. The requests ran on two instances of the same Artifact. This proves
per-request execution identity, not Agent session affinity.

Installed OpenClaw used an isolated official `agent exec --config` path, UniRouter
URL, scoped application Key103 and stable model name. The running local Gateway's
old SecretRef snapshot caused two preflight403 responses; an alternate provider
name failed before I/O. The successful config used OpenClaw's documented `${ENV}`
reference with the scoped secret passed in memory from its existing secure store.
No root defaults, other provider credentials, or admin identity were changed.
These failures made no Provider call. Installed Node24 was required; Node23's
SQLite safety refusal is retained as a pre-I/O environment failure.

The model read the right files but contradicted itself about release metadata
and misread CI routing; its task answer is therefore failed. The recommendation
was 173 words against the requested 220-word limit, so length passed. Gateway
request/tool-history corruption was not observed and no model-quality retry or
Gateway workaround was performed.

Wall time was 30.305 seconds; response-header latency was 2.541 and 6.844 seconds.
There is no matched direct-provider performance comparison. Total provider usage
was 25,368 input and 1,568 output tokens. Seven preceding qualification calls plus
these two Agent rounds produce cumulative 9/30 calls and CNY0.805295/5 conservative
reservation. Zero cost in client/observation fields is not a Provider invoice or
proof that upstream calls were free. The operation Probe lacked persisted usage;
its reservation remains and the budget has not been reset or refunded.

Readback `t-wl06wbhn98tn0n4` completed all six business queries but failed the
generation query because of an incorrect column name (exit1, retained).
Generation-only correction `t-wl06wbhrsgjsmio` succeeded without any model call.
The independent Bundle Publication preserved image/schema and old Release rows;
the batch nevertheless needed platform1.2.64 to correct an operation Probe path.
It is not an end-to-end no-build batch, a previously uncompiled-model proof, a
joint long-stream rolling/settlement acceptance, or Production evidence.

The next acceptance is genuine first PublicModel creation from a formal published
Definition absent from the compiled Catalog, through qualification, Release and
OpenClaw on one unchanged backend image. Historical writer retirement remains out.

经验文件无需变化.
