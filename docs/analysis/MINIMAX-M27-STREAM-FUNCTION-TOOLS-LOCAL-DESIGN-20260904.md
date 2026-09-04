# MiniMax M2.7 streamed Function Tools — bounded local candidate

Status: local implementation with completed bounded review; base `f4a0acce2a6e35038cb6eaf3b756c655a0e57438`,
tree `49e212eefc536ff1b27d89e0428c5e29eb5098ff`. This is not Target approval.

## Goal, evidence and authority

Product outcome remains a useful OpenClaw tool task through UniRouter, later
Hermes on a separately available environment. This local milestone only adds
the exact M2.7 PAYG Chat capability composition needed by the captured client:
streaming, explicit/default auto, embedded thinking, function arguments split
across events, multiple tool IDs, tool-result continuation, terminal usage.

The 2026-09-04 direct sample (session `3b4a2fba-f795-4aca-a18a-6d7a08c1733d`)
made two HTTP 200 requests, executed three real read tools, and ended in `stop`.
Request IDs: `06e98a49a446418301e43e492b0c1c94` and
`06e98a4fc9b5bf9ab02321f8aeab62b5`. Input/output totals are 22810/2175.
The answer contains a wrong architecture-doc exemption and ignores the requested
length; it is complete protocol evidence, NOT useful-task acceptance or a
performance comparison. No more paid samples are needed to discover this shape.

Both requests use `MiniMax-M2.7`, `https://api.minimax.cn/v1`, PAYG, stream=true,
tool_choice=auto, max_completion_tokens=8192, include_usage=true. Frames contain
embedded `<think>` content, indexed function tool deltas (including fragmented
arguments), a separate finish frame, then choices=[] usage and clean EOF without
DONE. Core usage is present; cache counters are absent despite input >512 tokens.
Safe normalized fixtures must identify replaced local paths and distinguish
captured frames from constructed negative cases. Original private evidence stays
outside Git. The first historical-profile request regressions already pass.

Authority: local code/tests/design and necessary append-only contracts/generated
metadata only. No PR/push/merge, artifact build, Target action, Publication apply,
new Provider call, Pricing, credentials, Alias, Token Plan, other model or SDK work.

## Exact contract changes

1. Append `minimax.m2.7.payg@1.3.0` and advance only that current pointer;
   preserve all prior versions and M3/Highspeed/GLM/Kimi/DeepSeek identities.
   Append an M2.7-only operation protocol, `protocol.minimax-m27-openai-chat.v1`.
   Keep endpoint, upstream model and tokenizer contracts unchanged. Append an
   M2.7-only reasoning successor: required embedded thinking and split=false
   remain fixed, but assistant tool-history content replay is no longer mandatory.
   The actual second client request has content=null and self-consistent
   client-normalized tool IDs; the Gateway neither rewrites IDs nor invents content.
2. New protocol allows explicit auto and provider-default omission, forwarded
   unchanged. Both output-limit spellings remain supported independently; no
   silent field dropping. Preserve exact historical tool-result association.
   Stream tools require include_usage, one choice, and no unmodeled fields.
3. Reuse shared SSE line splitter/event assembler, strict JSON, MiniMax control
   metadata normalization and existing guardrail/billing flow. Do not clone a
   provider parser. Add bounded shared function-tool delta accumulation used by
   the exact Runtime path and new streaming Probe. It owns tool index/ID/name,
   argument assembly, finish/terminal-usage order and clean-EOF validation;
   callers retain delivery safety and capability-specific semantic assertions.
   Maximum tools follows the existing 128-tool contract; aggregate arguments
   use the existing 1 MiB bounded Probe/response scale. Reject duplicate IDs,
   changed identity, invalid indexes/types, unknown fields, duplicate JSON keys,
   malformed completed arguments, post-finish model data, duplicate usage,
   missing finish/usage and invalid EOF. Do not alter old lifecycle policies.
   Runtime still inspects content and arguments before disclosure; Provider
   usage accepted before a delivery error remains evidence, never a success flag.
4. Append a cache usage contract for this Profile: retain non-stream v2's
   threshold requirements, but streamed absence is `not_reported`, not an
   authoritative zero or discount. Invalid/null/present contradictory counters
   still fail. Pass stream context explicitly from transport, never infer it
   from the model name or an upstream object field. No customer pricing change.
5. Append a precise streamed-function-tools Probe identity. Reuse request,
   parser/assembler and usage mechanics; Probe additionally requires the named
   bounded test tool, exact arguments, embedded reasoning and a tool_calls finish.
   Historical Probe acceptance sets stay unchanged. Bind streaming, reasoning,
   tools, function_tools and tool_choice_auto to this proof. Existing non-stream
   cache Probe implementation, acceptance contract and local fixtures may be
   reused through explicit new Profile bindings, not its old live Probe or
   Approval receipts. The streaming sample is not cache qualification. Future
   exact 1.3 deployments need their own formal qualification evidence.
6. Only after these contracts and tests are stable, generate one append-only
   declaration/migration through the existing Publication compiler, with the
   current M110 predecessor and exact 1.3 attachment guard. Do not edit/replay
   existing declarations. No Target-bound Plan exists in this local milestone.

## Tests and finite completion

- Captured client shape: old Profile continues rejecting streaming tools and
  explicit auto; new Profile passes admission and exact outbound projection.
- Shared stream tests: one/multiple/interleaved tools, fragmented arguments,
  exact IDs and history, terminal core usage, absent versus null/zero cache,
  EOF/DONE, malformed/duplicate/unknown fields and resource boundaries.
- Cross-path fixture: Probe and Runtime reuse mechanics; role-specific
  assertions stay explicit. Failure retains valid observed usage and never
  silently delivers a successful tool result.
- Applicable package tests during WIP; stable diff runs release metadata,
  Publication compiler/checks, schema/local PostgreSQL gates when required and
  risk-proportionate project checks. One closing review maximum for this object.
- No third old Kimi/guard/Plan review. No new parser-policy registry or platform.
- Local acceptance is a tested immutable-addressable candidate, not OpenClaw
  useful-task success, live qualification, deployment or speedup evidence.

## Pre-code review adjudication

Original reviewed digest: `0d8f63be4972724345c4ff9d1525cf213bdf4eb9c839b10697d5584410cc3f3a`.
Both validated_required findings were independently verified and accepted:
the reasoning admission layer rejects null content despite the protocol history
validator allowing it, and old live qualification does not transfer to Profile
1.3. Above revisions are the single design correction. The historical null-content
rejection is now a local regression. No extra Plan or old-object review is needed.

## Closing review adjudication

The single closing review examined the 43-path implementation object
`a277cd03b0a8da94bc3e9e2178ea2770a42673daab4b612ddb57acec173b1f12`.
Its one validated_required finding was independently confirmed: streamed tool
names were not checked against the frozen invocation constraint. The fix reuses
the existing FunctionToolChoice response authority before first tool identity
disclosure; missing, undeclared, conflicting and inapplicable constraints fail
closed. `TestMiniMaxM27StreamRejectsToolsOutsideFrozenRequest` and the positive
actual-relay test passed with race enabled. Historical protocols are unchanged.
No further independent review is required for this correction.

Local M111 PostgreSQL tests cover the exact M110 predecessor, read-only planning,
seven new bindings, preserved historical binding rows, apply/down/reapply and a
retained Profile attachment blocking downgrade. A gate-required test-only update
adds M111 to the existing legacy-fixture reverse-order list; its affected
Gateway rollback/financial-outcome integration tests pass with race enabled.
Version 1.2.49 is source metadata only. No PR, artifact, Target-bound Plan,
Publication apply, deployment or additional Provider experiment follows from
these local tests. MAR-012 is revised, not duplicated, with executable explicit
stream-context and historical-threshold regressions.
