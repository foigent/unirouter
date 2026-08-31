# Kimi K3 Chat Core Publication Plan — 2026-08-31

> Status: closing-review candidate after adjudicated pre-code findings; no
> runtime, Target or publication authority.
>
> Active Delivery Batch: `Kimi K3 × Kimi Platform OpenAI-compatible Chat ×
> Core Conformance`.
>
> First Active Milestone: `Kimi K3 × Kimi Platform OpenAI-compatible Chat ×
> non-streaming text`.
>
> Baseline commit/tree: `f3a73f3cd84cf4c75e9a4159dbe1831d9c517379` /
> `ce36a61e0027513b9f668dda6229a8d5d0590de7`.

This document is the exact object for the required independent pre-code
contradiction review. It freezes only the proposed K3 Core batch. It does not
authorize code, migration generation, Target publication, a provider call,
pricing, Release, Alias, canonical routing or Production.

## 1. Frozen identity and evidence classes

The batch may add and publish only this append-only local identity:

- Execution Profile `kimi.k3.api@1.9.0`;
- operation `openai.chat_completions.v1`;
- protocol `protocol.kimi-openai-chat.k3.v5@1.0.0`;
- provider `kimi`;
- upstream model `kimi-k3`;
- public path `/v1/chat/completions`.

The proposed independent capability qualifications are:

1. `streaming`;
2. `reasoning`;
3. `reasoning_effort`;
4. `tools`;
5. `function_tools`;
6. `tool_choice_auto`;
7. `tool_choice_required`;
8. `json_object`.

Non-streaming text is proved by operation qualification plus a public request;
it is not fabricated as a capability Approval. Usage and cache-read mapping are
delivery evidence. Fixed-version SDK and administrator UI workflows are batch
closeout evidence. None of those three evidence classes becomes a new
capability receipt merely to make the list uniform.

The Core batch does not include `json_schema`, Dynamic Tools, Partial Mode,
built-in or official search, media, Files, Batch, Responses, Messages, named or
null tool choice, parallel tools, function-tool streaming, pricing activation,
CustomerChargeQuote, SupplierCostObservation, Release, Alias, canonical route
or Production. Official documentation or the wider local Profile cannot
promote these capabilities into this publication.

The model-execution semantic delta from `@1.8.0` to `@1.9.0` is limited to
adding `reasoning_effort` to both the immutable provider capability declaration
(`ProviderCapabilities`) and the executable support set (`Capabilities`). The
identity/lifecycle delta is also explicit: add the immutable Chat operation
binding for exact `@1.9.0`, advance the K3 current pointer from `@1.8.0` to
`@1.9.0`, and keep the existing semantic protocol
`protocol.kimi-openai-chat.k3.v5` with Chat operation-binding V1 `1.0.0`.
Neither `@1.8.0` nor its protocol identity is modified in place.

K3 `@1.9.0` inherits the already modeled Dynamic Tools declaration from
`@1.8.0`. Publication and routing remain per capability: absence of a Dynamic
Tools binding/Probe/Approval must leave that invocation fail-closed. Historical
`@1.6.0/@1.7.0/@1.8.0` remain addressable, but are never current, new-attachment
or M102 publication targets after the pointer advances.

## 2. Proposed M102 static declaration

The proposed declaration-owned publication identity is
`000102_kimi_k3_chat_core_qualification`. Its exact static and schema
predecessor is `000101_kimi_k27_chat_core_qualification`.

It may append and promote only the eight exact `kimi.k3.api@1.9.0` bindings
below:

| Capability | Probe contract |
|---|---|
| `streaming` | existing `unirouter.capability-probe.kimi-chat-sse.v1@4.0.0` |
| `reasoning` | existing `unirouter.capability-probe.kimi-chat-reasoning.v1@2.0.0` |
| `reasoning_effort` | proposed `unirouter.capability-probe.kimi-chat-reasoning-effort.v1@1.0.0` |
| `tools` | existing `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `function_tools` | existing `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `tool_choice_auto` | existing `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `tool_choice_required` | existing `unirouter.capability-probe.kimi-chat-function-tools.v1@4.0.0` |
| `json_object` | existing `unirouter.capability-probe.kimi-chat-json-object.v1@2.0.0` |

Every selector has an explicit absent predecessor. No generic Kimi tuple and no
K2.6/K2.7/K3 historical tuple, including `kimi.k3.api@1.8.0`, may be published
or promoted. Exact declaration, selector and projection cardinalities are
eight; generated artifacts must derive those counts rather than adding another
hand-maintained authority.

The new reasoning-effort contract is a new request/evidence identity, not a new
response parser. It sends one server-owned non-stream request with
`reasoning_effort=low`, requires the exact K3 model, non-empty bounded
`reasoning_content`, the fixed sentinel answer and authoritative final usage,
and reuses the current Kimi response validator. It must not reinterpret the
historical reasoning v1/v2 receipt set.

For K3 the Probe request builder must never send `thinking=disabled` or the
K2.7-only `thinking={type:enabled,keep:all}`. K3 reasoning is always on. The
reasoning probe may omit `reasoning_effort` to prove provider-default behavior;
the independent effort probe sends `low`. Tool-choice `required` uses the
existing exact function-tools response grammar but must send the exact required
request rather than silently reusing auto. Tools remain non-streaming. JSON
Object and streaming requests omit `thinking` and do not acquire Dynamic Tools,
media or search fields.

No normalizer acceptance set is widened in advance. If the real K3 response
contains a wire variant rejected by both runtime and Probe, it is classified
before any repair. If runtime accepts it but Probe rejects it, that is a shared-
validator finding, not permission to add another Kimi-only envelope parser.

## 3. Publication and projection invariants

Publication V1 remains the only declaration owner. M102 must be generated from
the declaration and verified byte-for-byte; it is not hand-written. It changes
control data only and creates no schema table, Deployment, Probe, Approval,
Release or live qualification.

Target projection planning retains the frozen classification:

- no exact K3 Deployment: publish binding/selector only, with no projection
  action;
- exact Deployment plus absent capability row: `insert` the exact declared
  projection;
- exact `enabled/declared` predecessor: `update` only when the complete expected
  state matches;
- already-final state without ledger: `unledgered_publication_state` conflict;
- declared predecessor mismatch: `projection_pre_state_mismatch` conflict;
- `live_probed` or `production_approved`: preserve/exclude from publication DML;
  qualification currentness remains resolver-owned;
- historical, generic or otherwise non-exact Deployment: not a target and
  untouched.

`preserve/excluded` is only a planning diagnostic. It is not a new action,
database state or lifecycle. `no_op` remains limited to baseline/predecessor
verification. Any conflict invalidates the whole publication plan.

If no exact Deployment exists at apply time, a regression test must prove that
a later administrator-created exact attachment derives exactly these eight
declared rows from current bindings, creates no Probe/Approval, copies no
historical/generic tuple and remains route-ineligible until exact operation and
capability qualification complete.

## 4. Local implementation ceiling

After the independent review is adjudicated, the maximum local implementation
scope is:

- append-only `kimi.k3.api@1.9.0`, containing the `@1.8.0` execution semantics
  plus `reasoning_effort` in both `ProviderCapabilities` and executable
  `Capabilities`, with current pointer promotion from `@1.8.0` to `@1.9.0`;
- one immutable exact-Profile Chat operation binding for
  `kimi.k3.api@1.9.0` to `protocol.kimi-openai-chat.k3.v5` plus Chat binding V1;
- one exact K3 declaration and generated M102 up/down/verify artifacts;
- the minimal Kimi reasoning-effort request/validator identity;
- exact K3 Probe request construction for default reasoning, effort, streaming,
  ordinary tools, auto/required choice and JSON Object;
- declaration, publication, projection, onboarding, Admin and gateway
  regressions;
- affected generated inventory, release metadata and truthful current-status
  documentation.

It may not modify `@1.8.0` in place, add another Profile version beyond the
approved `@1.9.0`, introduce a new semantic protocol ID/version, table, Planner
path, grouped approval, provider-specific page, frontend Profile allowlist,
third envelope parser or automatic startup publication. Existing generic
capability UI must render the server-owned projection; no K3 tuple may be
copied into frontend eligibility logic.

Required local evidence before a stable candidate includes:

1. exact eight-binding declaration, absent predecessors and no sibling/generic
   tuple;
2. deterministic M101 to M102 generation, apply, verify, guarded down/reapply
   and retained-evidence rollback refusal;
3. exact absent/declared/unledgered/mismatch/dynamic-lifecycle/non-exact
   projection cases;
4. later-created exact Deployment projection plus route fail-closed behavior;
5. K3 `@1.9.0` operation/protocol/onboarding identity, current pointer, and
   `@1.8.0` historical/addressable but non-attachable invariants;
6. K3 Probe request fixtures proving no forbidden `thinking`, exact
   `reasoning_effort=low`, exact auto/required choice and no out-of-scope fields;
7. existing K2.6 and K2.7 request, validator and qualification regressions;
8. runtime/Probe response fixtures for K3 reasoning, tools, JSON and SSE without
   a new envelope parser;
9. even after every K3 Core qualification is current, Dynamic Tools and JSON
   Schema have no declared projection and remain route-ineligible/fail-closed;
10. repository release, migration, generated-artifact and diff gates.

One stable exact commit/tree receives the required independent candidate review
before remote write. Only a verified current blocker or required finding enters
the diff; deferred findings do not create backlog. A code-changing CI repair is
reviewed only as the new diff from the last reviewed tip.

## 5. CI, two-stage dry-run and Target boundary

The normal delivery target is one coherent gate, PR, CI and deployment batch.
Target publication remains separately preflighted:

1. build and verify the exact candidate artifact;
2. before the maintenance window, run an exact zero-write dry-run against the
   current M101 Target and freeze the valid Plan digest;
3. revalidate the exact K3 Deployment/projection path selected by the Target;
4. drain and stop old backends, then verify the database backup;
5. apply reacquires the publication serialization boundary, rereads the ledger
   and projections, rebuilds the Plan and requires the approved digest;
6. apply M102 atomically, verify final state inside the transaction, then do
   immediate post-commit readback inside the owner-orchestrated serial window;
7. start only the exact artifact whose ledger and folded static projection
   match M102;
8. verify stable/canary/frontend/Nginx provenance, ledger and unchanged
   historical dynamic evidence.

The pre-window dry-run reserves no state and performs no Target write. A Target
conflict, exact tuple mismatch or post-commit verification failure stops the
batch; it is never repaired by partial apply, automatic predecessor selection
or a second unreviewed plan.

## 6. Live milestones and financial boundary

After exact M102 Target verification, close one Active Milestone at a time:

1. non-streaming text and explicit output limit;
2. streaming text/SSE;
3. always-on reasoning/provider default;
4. reasoning effort `low` (with local value-domain coverage for
   `low|high|max`);
5. ordinary tools;
6. function tools and typed-contract multi-turn history;
7. tool choice auto;
8. tool choice required;
9. JSON Object;
10. usage/cache-read delivery evidence;
11. fixed-version SDK and administrator UI closeout.

Each route capability keeps its own immutable Probe and Approval. Publication
does not create either. Before the first supplier call, revalidate the exact
Target, model availability, credential binding, billing mode, missing-price
behavior, customer-wallet state and a K3-specific call/spend authorization.
No prior K2.7 call count or temporary application key is silently reused.
Provider retries remain disabled unless explicitly counted inside that budget.

Cost-observation means no customer charge; it does not prove exact supplier
cost or turn missing pricing into zero. K3 pricing remains a manual-review draft
until separately approved and cannot block zero-customer-charge conformance.

## 7. Stop conditions and deferred process debt

Stop for product/architecture decision if implementation requires:

- any identity/current-pointer/Profile/protocol change beyond the approved
  append-only `kimi.k3.api@1.9.0`, its exact immutable Chat binding, and the
  K3 current-pointer promotion from `@1.8.0` to `@1.9.0`;
- JSON Schema, Dynamic Tools, search, media, Partial Mode or another excluded
  capability;
- schema, Planner, Release, pricing/billing or permission architecture changes;
- a third complete provider envelope parser or a second Kimi runtime/Probe
  acceptance authority;
- a frontend exact-tuple allowlist or provider-specific admin workflow;
- historical rewrite, generic tuple publication, dynamic-lifecycle mutation or
  partial publication.

The current placement of dynamic Target snapshots in release-bearing
`SYSTEM-FACT-MAP.md` is a separate `valid_deferred` process issue. A future
batch may evaluate keeping dynamic snapshots primarily in non-release-bearing
Current Status/evidence while retaining stable architecture and authority
facts in the System Fact Map. It does not reopen K2.7 or enter K3 Core. Target
UI may correctly continue to show deployed `1.2.23`; docs-only source candidate
`1.2.24` must not be deployed merely for display parity.
