# Kimi streaming delivery authority convergence — exact implementation design

> Status: pre-code exact design revised after independent read-only review; prior findings pending focused recheck
> Active Milestone: `Kimi OpenAI-compatible Chat × incremental streaming delivery authority convergence`
> Source baseline: commit `e6905e69d8137fe6747714937e63d8f504d0364d`, tree `819210bfa1eaef6dbf6dadf5319e1bebaa7a3ee9`
> Date: 2026-09-01
> Authorization: this exact local design, independent review, minimal local implementation, focused tests and one risk-proportionate local gate. No push, PR, CI, Publication, migration, Target, credential or provider-call authority.

## 1. Decision and exact scope

The current disposition is `duplicated_stream_state_authority`: Kimi runtime
delivery and immutable capability Probe validation independently decide SSE
framing, terminal order, usage finality and successful completion, and their
accepted byte streams differ.

V1 converges only the shared mechanical facts for the three current exact Kimi
OpenAI-compatible Chat streaming bindings:

- `kimi.k2.6.api@1.7.0` × `protocol.kimi-openai-chat.v5@1.0.0` × SSE Probe v3;
- `kimi.k2.7-code.api@1.3.0` × `protocol.kimi-openai-chat.v5@1.0.0` × SSE Probe v4;
- `kimi.k3.api@1.9.0` × `protocol.kimi-openai-chat.k3.v5@1.0.0` × SSE Probe v4.

Historical SSE Probe v1/v2 remain immutable. They may reuse behavior-preserving
line/event mechanics, but they do not acquire the current v3/v4 state contract.

The shared boundary is:

```text
caller-bounded provider stream
  -> shared incremental SSE line/event mechanics
  -> existing exact-scoped K2.6 empty-reasoning normalization
  -> shared current-Kimi frame facts and lifecycle state
       -> Runtime: canonical guardrail, delivery, backpressure, billing, receipts
       -> Probe: immutable typed schema, exact model, sentinel, reasoning and usage assertions
```

The shared result is process-local. It is not a Profile, protocol identity,
Probe contract, evidence schema, persisted state, registry, database authority
or financial authority. Runtime and Probe select it only from their existing
exact authorities; neither caller becomes the other's oracle.

## 2. Invariants

1. Runtime remains the only customer-delivery, guardrail, backpressure,
   cancellation, usage/cache charging and execution-receipt authority.
2. Probe remains the only exact qualification authority for model identity,
   sentinel content, required reasoning, positive usage and immutable receipt
   diagnostics.
3. Current Kimi v3/v4 logical-stream success requires a supported terminal choice,
   a top-level provider usage observation consistent with any terminal
   choice-level usage, and `[DONE]`. Clean EOF is not current-Kimi success.
4. A `choices=[]` terminal usage event and top-level usage on the terminal
   choice are both accepted when the exact typed schema accepts them. A nested
   `choices[].usage` observation is comparison evidence, not customer-charge
   authority; the top-level usage remains runtime authority.
5. Repeated usage observations must agree on prompt, completion, total and
   cache-read counts. Probe may impose additional detailed-usage assertions.
6. No model delta is disclosed before shared mechanical validation, existing
   canonical guardrail admission and durable delivery start.
7. K2.6 removes only an empty-string `reasoning_content`; non-empty reasoning,
   unknown siblings and duplicate-member ambiguity are not hidden.
8. Kimi streaming tools remain unsupported by the exact current public
   contract. This batch does not enable tool-call streaming.
9. Non-Kimi streams keep their current framing, EOF and usage behavior.
10. `[DONE]` closes the shared logical stream. Runtime does not wait for
    provider transport EOF after it and does not disclose later bytes. Probe
    retains its immutable bounded whole-body assertion that rejects trailing
    data or duplicate DONE; that stricter qualification postcondition is not
    Runtime delivery authority.
11. Historical Probe receipts are never regenerated or reinterpreted.

## 3. Existing capabilities to reuse

- `strictjson.Validate` for duplicate-member and complete-document ambiguity;
- `NormalizeKimiK26ChatStreamChunk` for exact K2.6 normalization;
- `ParseOpenAICompatibleUsageWithCacheProfile` for runtime provider/cache usage;
- `ChatStreamContract` and the canonical Gateway stream guardrail;
- Runtime scanner goroutine, bounded line buffer, synchronous writer,
  cancellation, delivery lease, checkpoint and receipt machinery;
- existing Kimi v1/v2/v3/v4 Probe fixtures and cross-delivery fixtures.

V1 must not add a third Kimi full-envelope validator. The shared preparation
extracts only mechanical facts required by both callers; each caller retains
its existing role-specific final decoder.

## 4. Minimal shared mechanics

### 4.1 Placement

Add a small helper beside the shared OpenAI-compatible adapter code:

`internal/adapters/openai_compatible_chat_stream.go`

The package is already imported by Gateway and `modelcapabilityqualification`,
so this creates no reverse dependency or package cycle.

### 4.2 SSE line and event mechanics

Expose a behavior-pinned scanner split and incremental event assembler with
this effective contract:

```go
func SplitSSELines(data []byte, atEOF bool) (advance int, token []byte, err error)

type ChatSSEEventAssembler struct { /* bounded current event only */ }

func (a *ChatSSEEventAssembler) AcceptLine(line string) (event ChatSSEEvent, ready bool, err error)
func (a *ChatSSEEventAssembler) Finish() (event ChatSSEEvent, ready bool, err error)
```

Rules:

- recognize CRLF, LF and bare CR without buffering the complete response;
- ignore blank events and comment lines;
- join multiple `data:` lines with one newline;
- reject every non-`data` SSE field before payload disclosure;
- preserve the optional single space after `data:` as SSE syntax;
- keep line/body limits caller-owned; the assembler stores only the current
  event and rejects an event whose accumulated data exceeds its configured
  caller-supplied limit;
- process one final unterminated event at clean transport EOF.

Probe retains its 1 MiB body and 256 KiB line limits. Runtime retains the
configured line, duration and write-idle limits; the exact Kimi event limit is
the same configured runtime frame bound, so multi-line framing cannot bypass
the existing per-frame safety ceiling.

### 4.3 Exact current-Kimi preparation and state

Use an existing-authority-selected process-local mechanics value. Exact names
may change, but the behavior must match:

```go
type KimiChatStreamMechanics struct {
    NormalizeEmptyReasoning bool
    RequireDone             bool
    RequireTopLevelUsage    bool
}

type KimiChatStreamPrepared struct {
    Body              []byte
    Changed           bool
    ChoiceFrame       bool
    TerminalChoice    bool
    UsageOnly         bool
    TopLevelUsage     *Usage
    ChoiceUsages      []Usage
}

type KimiChatStreamState struct { /* process-local mechanical state */ }

func PrepareKimiChatStreamEvent(... exact identity ..., data []byte, mechanics KimiChatStreamMechanics) (KimiChatStreamPrepared, error)
func (s *KimiChatStreamState) Accept(prepared KimiChatStreamPrepared) error
func (s *KimiChatStreamState) AcceptDone() error
func (s *KimiChatStreamState) FinishEOF() error
```

Preparation rules:

1. Reject impossible mechanics and exact identity mismatch; never fall back to
   provider-family inference after a partial match.
2. Apply the existing K2.6 normalizer exactly once, including its duplicate-key
   check. K2.7/K3 run the same shared strict-document ambiguity check without
   removing reasoning.
3. Extract only `choices` presence/count, supported finish state, top-level
   usage, nested choice usage and whether the frame is usage-only. Do not
   validate model identity, sentinel content or required reasoning here.
4. Decode every mechanical usage observation through the existing runtime
   usage/cache parser. Choice usage is wrapped only for decoding and never
   promoted to customer-charge authority.
5. Preserve prepared bytes for the existing typed Probe decoder and canonical
   Gateway guardrail. Unknown fields remain visible to those authorities.

State rules for current v3/v4:

- before terminal finish: accept only nonterminal choice frames with no usage;
- terminal choice: require a supported exact finish; allow matching nested
  choice usage and/or top-level usage;
- after terminal finish: allow only top-level usage-only frames whose counts
  match every prior usage observation;
- after a usage-only frame: reject further JSON data;
- `[DONE]`: require terminal finish and top-level usage, then close the state;
- usage before finish, conflicting usage and clean/early EOF are closed shared
  mechanical failures. The first valid `[DONE]` closes shared logical state.

The helper returns role-neutral bounded process-local issues. Runtime maps all
pre-DONE issues to its existing safe invalid-frame terminal path. A
version-aware Probe adapter maps each shared or role-specific failure at the
same observation-update point as today. It must preserve every existing v1/v2/
v3/v4 error code and diagnostic member, including v3 `delivery_shape`, v4
`json`, `data_after_terminal`, `cached_tokens`, `cached_tokens_conflict` and
`reasoning_tokens`. No new receipt code is introduced.

## 5. Runtime integration

For an exact current Kimi route only:

1. Select mechanics from the complete route tuple: operation, Execution
   Profile ID/version, provider, mapped model, protocol ID and operation-binding
   protocol version. An incomplete or mismatched tuple does not silently select
   Kimi exact mechanics.
2. Use the shared line split and event assembler in the existing scanner
   goroutine. Keep the unbuffered channel, cancellation and writer behavior.
3. Classify the existing typed provider error envelope before current-Kimi
   preparation so it retains `upstream_stream_provider_error` disposition.
4. Prepare and accept each complete provider-owned data event once before
   usage authority, client projection, guardrail or delivery. Do not call the
   K2.6 normalizer a second time.
5. Apply the existing client cache projection using only prepared top-level
   usage. The projection is downstream compatibility output, never state or
   billing input.
6. Feed the shared lifecycle state before the existing canonical frame
   guardrail. A state failure follows the current invalid-frame path.
7. Use only prepared top-level usage as runtime provider/billing evidence.
   Nested choice usage is comparison evidence only.
8. Feed the projected prepared bytes into the existing generic canonical
   guardrail. Exact
   Kimi response mechanics do not replace content/reasoning/tool safety.
9. Validate `[DONE]` through the shared state. Do not accept current Kimi clean
   EOF after a finish frame. After the first valid `[DONE]`, close the provider
   body as today; do not add a validation drain that can delay completion until
   provider transport EOF or the 30-minute stream-duration ceiling.
10. Preserve current HTTP-200 terminal SSE error, no-output cancellation,
   partial settlement, checkpoint and downstream write-failure behavior.

Generic OpenAI-compatible, MiniMax, DeepSeek, Doubao and other provider streams
remain on their existing paths.

## 6. Probe integration

Add `ValidateKimiChatSSEForBinding(Binding, contentType, reader)` as the
production entrypoint. The Admin path passes
`kimiChatProbeBindingFromSnapshot(snapshot)`, so current mechanics are selected
from operation, capability, Profile ID/version, protocol ID/version, Probe
ID/version, provider, upstream model and public path. The existing argument-
based wrapper remains only for compatibility tests and never establishes
current-binding authority.

The binding-aware validator keeps its content-type and bounded-reader checks,
then:

1. uses the shared line split/event assembler for all immutable versions where
   doing so is behavior-identical;
2. selects shared current state only for published v3/v4 bindings;
3. prepares each current event once and decodes the prepared bytes through the
   existing strict typed Kimi schema;
4. keeps exact model, one-choice schema, finish assertion, sentinel, required
   v4 reasoning and detailed usage validation in Probe;
5. supplies its typed base usage counts to the same shared state result rather
   than maintaining a second terminal/usage lifecycle;
6. preserves v1/v2 state and diagnostics exactly where their immutable grammar
   differs from current v3/v4;
7. continues scanning its bounded qualification body after logical `[DONE]` to
   preserve existing duplicate-DONE/trailing-data diagnostics. This is a Probe-
   only postcondition and does not cause Runtime to drain provider transport.

No Probe contract, selector, Approval or historical receipt changes.

## 7. Exact Profile request-preparation boundary

Do not mutate the shared Kimi protocol entries: K3 historical Profile 1.8 and
current 1.9 share `protocol.kimi-openai-chat.k3.v5`, so protocol-wide request
normalization would exceed this milestone.

Instead, the existing `validateChatExecutionProfileRequest` evidence path adds
`stream_options.include_usage=omitted->true` only for the three complete current
Profile tuples in section 1, only when `stream=true`, and rejects explicit
`include_usage=false` for those tuples. `normalizeProviderRequest` already owns
the reviewed byte rewrite and remains unchanged except for consuming this exact
evidence.

K3 1.8 and every non-current/non-Kimi tuple retain current behavior. The exact
Profile gate makes runtime requests obtain the top-level usage required by
current-Kimi shared success without changing protocol identity or publishing a
database binding. If the complete tuple cannot own this correction without an
immutable identity/version change, stop for a product decision.

## 8. Verification matrix

### 8.1 Shared helper

- CRLF, LF and bare CR produce the same events;
- multi-line data joins once and respects the configured event limit;
- comments are ignored; `event`, `id`, `retry` and unknown fields fail closed;
- final unterminated data event is processed;
- exact selector rejects incomplete/mismatched tuple;
- K2.6 normalization is exact-scoped and preserves non-empty reasoning and
  unknown siblings;
- duplicate keys fail before map/usage decoding;
- valid top-level/choice usage agrees; conflicting prompt/completion/total/cache
  counts fail.

### 8.2 Probe preservation

- all existing Kimi v1/v2/v3/v4 tests remain green;
- v3 still accepts K2.6 empty-reasoning normalization and terminal choice usage
  only when later/top-level usage agrees;
- v4 still requires non-empty reasoning and exact sentinel;
- missing usage, usage before finish, missing/duplicate DONE, data after
  terminal and usage conflicts keep existing bounded error categories.
- leading BOM, whitespace-only lines, `DataEventCount`, `DeltaCount`,
  `DoneSeen` and `ResponseBytes` on representative failures remain pinned per
  immutable version.

### 8.3 Runtime exact lifecycle

Full relay tests must carry the complete exact K2.6, K2.7 and K3 route identity
and prove:

- one valid current fixture completes with provider-reported final usage;
- bare-CR and valid multi-data event variants share Probe/runtime framing;
- missing top-level usage, missing DONE, clean EOF after finish and conflicting
  choice/top-level usage fail before successful completion;
- Runtime stops at the first valid `[DONE]` without a transport drain; Probe
  regressions independently preserve duplicate-DONE, trailing-data and trailing
  unsupported-field qualification failures, plus comments-only trailing input;
- HTTP 200 terminal errors, no-output cancellation and partial delivery billing
  remain unchanged;
- provider content and private reasoning never appear in a safe error frame.

Request regressions prove omitted `include_usage` is normalized and explicit
false is rejected only for the three current tuples, while K3 1.8 remains
unchanged.

### 8.4 Gate

Run focused adapter, modelprofile, qualification and Gateway tests while the
diff is changing. When stable, run the repository's existing risk-proportionate
local gate once. Classify any failure before rerunning.

## 9. Independent review and closeout

1. Hash and review this exact design with source commit/tree, Active Milestone,
   invariants, non-goals, tests already run and known blockers.
2. Reviewer dispositions are limited to `validated_blocker`,
   `validated_required`, `valid_deferred`, `rejected` and
   `product_decision_required`; Reviewer does not edit.
3. Main Agent verifies every reported item. The first review's binding-owned
   selection, exact-Profile request gate, version-aware diagnostics and Runtime
   error/cache ordering findings are incorporated. Its proposed Runtime post-
   DONE transport drain is rejected because it would make provider connection
   closure a success condition and can delay completion to the existing stream
   duration ceiling; Probe trailing-body rejection remains an explicit
   qualification-only postcondition.
4. Implement the smallest reviewed diff and run focused tests.
5. Bind at most one closing review to this design digest, the exact final diff,
   adjudicated findings and new test results.
6. Re-read `MODEL-ADAPTATION-LEARNINGS.md` before the final gate. Update MAR-003
   only if the implementation supplies executable enforcement not already
   described; otherwise report `经验文件无需变化`.

## 10. Stop conditions and non-goals

Stop for a product decision if closure requires:

- changing an immutable Probe acceptance set or receipt interpretation;
- a new Profile, protocol, Probe, delivery-policy or evidence identity;
- schema, migration, Publication V1, current pointer, Release, Alias, pricing or
  canonical-route change;
- a generic cross-provider streaming IR/framework or complete response buffer;
- changing customer charge, provider-usage or cache authority;
- enabling streaming tools;
- weakening fail-closed delivery;
- provider calls, credentials, spend, Target or Production work.

V1 does not modify non-stream delivery, other providers, SDK/framework policy,
MiniMax/GLM qualification, deployments or historical evidence. Success means
one shared current-Kimi mechanical event/state result is consumed by Runtime
and Probe, while their distinct delivery and qualification responsibilities
remain explicit and locally verified.
