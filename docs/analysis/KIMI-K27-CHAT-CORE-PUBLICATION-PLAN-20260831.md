# Kimi K2.7 Code Chat Core Publication Plan — 2026-08-31

> Status: locally implemented candidate under verification; no Target or
> runtime authority.
>
> Active Delivery Batch: `Kimi K2.7 Code × Kimi Platform OpenAI-compatible Chat × Core Conformance`.
>
> Baseline commit/tree: `781f913e8a8e1e594e94bddedfc4886bd81cea0d` /
> `77dc8ad2dff86cd9b5c67e1de3ceb62cf8223676`.
>
> The exact pre-code contradiction review is complete. It identified required-
> thinking Probe coverage, operation-connectivity ordering and atomic generator
> output as current-required findings; the implementation below addresses only
> those findings. Target and provider evidence remain separate.

## 1. Frozen outcome and non-goals

The batch publishes and validates only the already modeled exact identity:

- Execution Profile `kimi.k2.7-code.api@1.3.0`;
- operation `openai.chat_completions.v1`;
- protocol `protocol.kimi-openai-chat.v5@1.0.0`;
- provider `kimi`;
- upstream model `kimi-k2.7-code`;
- public path `/v1/chat/completions`.

The six independent capability qualifications are:

1. `streaming`;
2. `reasoning`;
3. `tools`;
4. `function_tools`;
5. `tool_choice_auto`;
6. `json_object`.

Usage and cache-read mapping are delivery evidence, not new capability
Approval identities. The fixed-SDK workflow is batch closeout evidence, not a
route qualification. `tool_choice:null`, required/named/parallel tool choice,
function-tool streaming, highspeed/Coding subscription identity, Partial,
media, built-in search, Dynamic Tools, Responses, Messages, pricing activation,
Release and Alias are out of scope.

No Profile, protocol, Planner, schema table, grouped approval, provider-specific
admin page or frontend Profile allowlist may be added in this batch. K2.7 must
reuse the current Kimi typed validators and server-owned capability projection.

## 2. Exact M101 declaration

The declaration-owned publication identity is
`000101_kimi_k27_chat_core_qualification`. Its exact static and schema
predecessor is `000100_kimi_k26_function_tools_tool_index_normalizer`.

It appends and promotes six exact bindings:

| Capability | Probe contract |
|---|---|
| `streaming` | `unirouter.capability-probe.kimi-chat-sse.v1@4.0.0` |
| `reasoning` | `unirouter.capability-probe.kimi-chat-reasoning.v1@2.0.0` |
| `tools` | `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `function_tools` | `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `tool_choice_auto` | `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `json_object` | `unirouter.capability-probe.kimi-chat-json-object.v1@2.0.0` |

All selectors have an explicit absent predecessor. No generic Kimi tuple is
published. Each projection effect is exact-scoped to the identity in section 1,
allows insertion of a missing declared row, and uses the corresponding final
`capability_contract:<id>@<version>` evidence reference.

The new contract versions are deliberately narrow additions to the existing
Kimi parser, not a new envelope parser:

- SSE v4 preserves K2.6 v1-v3 behavior, accepts bounded non-empty
  `reasoning_content` only for the exact K2.7 required-thinking binding and
  requires that reasoning evidence before terminal success;
- function-tools v4 already has the required strict non-stream grammar; its
  existing lexical `tool_calls[].index` normalizer is extended only to the
  previously unbound exact
  `kimi-k2.7-code + protocol.kimi-openai-chat.v5` scope, removes that
  non-authoritative member before canonical validation, and leaves K2.6
  behavior unchanged;
- JSON Object v2 and reasoning v2 already accept exact reported-model
  parameters and the required bounded reasoning member. K2.7 fixtures prove
  those existing grammars without changing their acceptance sets;
- no historical K2.7 Probe/Approval exists, and the new exact Profile binding
  is the only authority that makes these parameterized contracts applicable to
  K2.7.

The typed Probe request builder and the operation-connectivity Probe must
resolve the exact Execution Profile.
K2.7 requests explicitly use `thinking={type:enabled,keep:all}` and never send
the K2.6-only disabled form. The operation Probe also uses the Profile-owned
`max_completion_tokens` path instead of the generic `max_tokens` request.
Existing K2.6 request and response semantics remain behaviorally unchanged.
Fixtures cover both families so the K2.7 applicability change cannot
reinterpret historical K2.6 evidence.

## 3. Publication generator completion

Publication V1 already verifies generated SQL byte-for-byte but has no
supported developer entrypoint that emits repository migration artifacts.
Hand-writing M101 would recreate the duplication that Publication V1 exists to
remove. The minimum current-required implementation is a pure local generator
command that:

- accepts one exact compiled publication ID and an explicit migration output
  directory;
- resolves its compiled predecessor and calls the existing
  `GenerateMigration` function;
- creates the `.up.sql`, `.down.sql` and `.verify.sql` files using fail-if-exists
  semantics and repository-safe file modes;
- preflights the complete three-file destination set and stages all outputs
  before publication; an existing second/third target or any staged-write or
  finalization failure leaves no new partial artifact set and never overwrites
  a user file;
- writes no database, Target, receipt, audit or temporary runtime state;
- refuses an unknown publication, existing destination, invalid declaration or
  generated-byte mismatch;
- is covered by deterministic output and refusal tests.

This is a developer artifact generator, not a second publication owner or
runtime writer. Existing `VerifyGeneratedMigrationFiles`, artifact inventory,
Strict V1 plan/apply and migration owner remain authoritative.

## 4. Target preflight facts and projection classification

Read-only development/test checks on 2026-08-31 found:

- exact Target artifact `1.2.22/M100`, commit/tree equal to the baseline above;
- no `kimi.k2.7-code.api` or `kimi.k3.api` model, Deployment, operation or
  capability projection row;
- K2.6 uses Kimi connection `10` and credential binding `10`; connection and
  binding are active, revisions are `1`, fail count is zero and cooldown is
  absent;
- backend runtime does not override `BILLING_MODE` or
  `REQUEST_PRICING_AUTHORITY`, so the exact compiled defaults are
  `cost_observation` and `legacy`;
- `model_prices` contains no K2.6, K2.7 or K3 row. Missing price remains absent,
  never an approved zero price.

The frozen projection behavior is:

- no exact Deployment: publish binding/selector only; the Plan has no
  projection action;
- exact Deployment with absent capability row: `insert` declared projection;
- exact `enabled/declared` predecessor: `update` only when exact expected state
  matches;
- already-final without ledger: `unledgered_publication_state` conflict;
- declared predecessor mismatch: `projection_pre_state_mismatch` conflict;
- `live_probed` or `production_approved`: preserve/exclude from publication
  DML; qualification currentness remains resolver-owned;
- historical, generic or otherwise non-exact Deployment: not a target and
  untouched.

`preserve/excluded` is diagnostic language only. It is not a new action,
database state or lifecycle value. `no_op` remains limited to baseline and
predecessor verification.

## 5. Local implementation and verification

The code-bearing batch is limited to:

- the exact M101 declaration JSON;
- generator entrypoint and its focused tests;
- generated M101 up/down/verify artifacts;
- declaration, binding, selector, exact-projection and onboarding regression
  tests;
- release metadata and current-status documentation that are actually affected.

Required local evidence before the stable candidate commit:

1. declaration normalization/digest and generated-byte determinism;
2. M100 → M101 apply, transaction final verification and post-commit static
   verification;
3. clean guarded down/reapply plus retained Probe/Approval/Freeze/attempt
   refusal cases;
4. absent exact Deployment produces zero projection actions;
5. exact absent row inserts the correct declared evidence reference;
6. exact declared predecessor update, unledgered-final conflict and declared
   mismatch conflict;
7. dynamic lifecycle row preserved/excluded and historical/generic rows
   untouched;
8. after publication, a newly created exact attachment/Deployment derives the
   six declared rows from current bindings, creates no Probe/Approval, and
   remains fail-closed for route selection until exact qualification;
9. catalog-wide exact identity/onboarding regressions and existing K2.6
   qualification regressions;
10. exact K2.7 Probe-request fixtures for SSE, reasoning, tools and JSON plus
    exact operation-connectivity required-thinking coverage, negative
    K2.6/K2.7 cross-family and historical-version cases;
11. generator second/third-target conflict and injected mid-write failure leave
    zero partial files;
12. repository release, migration, generated-artifact and diff checks.

One stable exact candidate commit/tree is independently reviewed before push.
Only a validated blocker or current-required finding may produce a minimal
follow-up diff and one closing review. Deferred findings do not enter the batch.

## 6. CI, two-stage dry-run and Target apply

After local review, one coherent PR/CI batch is the normal target. A real code
change discovered by CI creates a new exact review object; infrastructure-only
failure is retried without code churn.

The Target sequence is:

1. build and verify the exact candidate artifact;
2. before the maintenance window, run a zero-write dry-run against M100 and
   freeze the valid Plan digest; the current expected projection set is empty;
3. drain traffic and stop old backends;
4. take and verify the database backup;
5. run `apply` with the exact approved Plan; it reacquires the publication
   serialization boundary, rereads the Target, rebuilds the Plan and requires
   the same digest;
6. apply M101 atomically, verify final state in-transaction, perform immediate
   post-commit readback inside the owner-orchestrated serial window, and stop on
   any mismatch;
7. start only the exact artifact whose ledger and folded static projection
   match M101;
8. verify stable/canary/frontend/Nginx provenance, schema ledger and unchanged
   dynamic evidence counts.

Pre-window dry-run reserves no state. It creates no publication receipt, audit
or database row. No M101 apply or deployment may occur before the corresponding
external-action authorization and exact preflight are revalidated.

## 7. Attachment, live matrix and financial boundaries

After M101 Target verification, create the exact K2.7 attachment through the
supported administrator flow using the existing Kimi Platform connection and
an active credential binding. This must exercise the later-created Deployment
path described in section 5; it must not copy K2.6, generic or historical
projections.

The exact operation connectivity Probe is the first provider call for the new
Deployment and must succeed before any public request or capability Probe. It
is counted inside the same 20-call ceiling and does not create a new Chat
capability approval contract.

The already authorized K2.7 live budget is a maximum of 20 supplier calls and
CNY 5 supplier spend for this batch, inclusive of typed Probes, public UI/SDK
requests, direct comparisons and any explicitly chosen retry or failover. No
automatic retry is enabled. The count is an upper bound, not a target.

Before the first provider call, revalidate the exact model, Target, credential
status, cost-observation billing mode, missing-price behavior, wallet state and
remaining call/spend budget. Cost-observation means zero customer wallet
charge, not a supplier-cost Observation or a zero supplier price.

Close one Active Milestone at a time: non-stream text, streaming, reasoning,
tools, function tools, tool-choice auto and JSON Object. Usage/cache-read and
fixed-version SDK/UI are closing checkpoints. Each route capability still gets
its own immutable Probe and Approval evidence. Publication never creates either.

## 8. Stop conditions

Stop and return for a product or architecture decision if implementation would
require any of the following:

- a Profile/protocol identity change, new schema table, Planner framework,
  grouped approval, provider-specific UI or generic/highspeed Kimi tuple;
- a third complete provider envelope parser or a new runtime/Probe acceptance
  authority rather than reuse of the existing Kimi path;
- any further provider acceptance-set expansion after SSE v4 and the exact
  K2.7 non-stream applicability fixtures above have been frozen;
- a price, billing, Release, Alias, canonical-route or Production promotion;
- partial publication after conflict, historical rewrite, or mutation of a
  dynamic lifecycle projection;
- provider calls beyond 20 or supplier spend beyond CNY 5.

Provider wire mismatch, credential failure, a non-current exact attachment or
any Publication V1 Plan conflict is reported with evidence and is not bypassed.
