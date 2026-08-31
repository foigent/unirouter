# Kimi non-stream delivery authority convergence — exact implementation design

> Status: reviewed exact implementation design; local implementation candidate pending exact code review
> Active Milestone: `Kimi OpenAI-compatible Chat × non-stream delivery authority convergence`
> Source baseline: commit `9e97f536d35fe61ed1d1246d86ab747a429023ec`, tree `7aff03c27b034d2ef61102b09be4e9b24082b4b8`
> Date: 2026-09-01
> Authorization: read-only inventory, this exact design, independent review, and—only after no current blocker/required finding—the minimal local implementation and verification described here.

## 1. Decision

V1 converges only the Kimi OpenAI-compatible Chat **non-stream document boundary** already duplicated by runtime delivery and capability Probe validation.

The shared boundary is:

```text
bounded provider bytes (the caller owns the byte limit)
  -> existing-authority-selected JSON document ambiguity check
       -> runtime: token-usage extraction
            -> exact-scoped safe wire normalization classification
            -> builtin meter extraction from original bytes when structurally traversable
            -> prepared bytes + closed process-local delivery issue
            -> cache projection
            -> one UseNumber object decoded from the final response body
            -> protocol guardrail and delivery
       -> Probe: exact-scoped safe wire normalization
            -> immutable Probe-contract typed decode and assertions
```

The shared boundary is a pure in-process helper. It is not a new protocol, Profile, Probe contract, delivery-policy identity, public API, database authority, registry, manifest, state machine, qualification rule or financial authority.

V1 must not make runtime or Probe the oracle. Expected behavior is decided from all four existing authorities:

1. exact Profile/operation/protocol identity;
2. published Chat protocol contract;
3. exact immutable Probe contract ID/version;
4. current runtime delivery and usage behavior, including evidence ordering.

When those disagree, the implementation preserves the published identities. It does not force a false all-equal differential result.

## 2. Current data flow and duplicated authority

### 2.1 Runtime non-stream path

```text
Relay.responsePayload
  -> bounded body
  -> OpenAICompatible.ParseResponse
       -> exact Kimi/DeepSeek duplicate-member validation
       -> typed usage/cache extraction from provider body
       -> exact tool_calls[].index normalization classification
          (invalid index deliberately remains for the guardrail)
       -> builtin meter extraction from original bytes
          (a document already classified as structurally undeliverable keeps
           validated token usage even when its tool tree cannot be traversed)
       -> provider-specific envelope normalization
       -> SDK cache-usage projection
  -> decodeJSONUseNumber into map[string]interface{}
  -> inspectNonStreamOutputTextsForRoute
       -> exact protocol field/value/tool/reasoning guardrail
  -> retain provider usage on an undeliverable response
  -> delivery/settlement
```

Runtime owns customer delivery and financial evidence. A delivery-invalid child tool index is intentionally not returned as an adapter parse error after usage was established; the canonical guardrail rejects it and the provider usage is retained as non-customer-charge evidence.

### 2.2 Kimi Probe non-stream path

```text
Admin exact binding snapshot
  -> bounded reader in readKimiChatCompletion
  -> contract-version-selected strictjson.Validate
  -> function-tools v4 exact tool_calls[].index normalization
  -> DisallowUnknownFields typed completion decode
  -> contract-version delivery-field and usage checks
  -> exact model, finish, sentinel, reasoning/tool/JSON assertions
  -> immutable Probe receipt
```

Probe owns only qualification evidence for its exact immutable contract. It is not proof that the runtime artifact can deliver the response.

### 2.3 Existing shared components

- `strictjson.Validate`: complete single document, duplicate-member rejection (including escaped-equivalent keys), and `json.Number` lexeme preservation. It is not a global UTF-8 or number-size policy.
- `NormalizeOpenAICompatibleChatToolCallIndex`: exact-scoped safe removal of a lexical non-negative non-stream child tool-call index. Unknown siblings remain for canonical validation.
- `ParseOpenAICompatibleUsageWithCacheProfile`: typed provider usage/cache evidence; unknown counters do not become financial authority.
- cross-delivery fixtures in `internal/gateway/kimi_k26_qualification_delivery_contract_test.go`.

The missing seam is a reusable two-phase document-ambiguity/index-normalization boundary consumed by both paths. Runtime preserves its evidence order by running only ambiguity validation before usage, then normalization after usage. Its structural object is decoded only after all adapter projections, from the exact final body handed to the guardrail. Probe does not need that generic object and continues its exact typed decode over the prepared bytes.

## 3. Compatibility facts that V1 must preserve

| Existing path/contract | Duplicate members | child `tool_calls[].index` | unknown/nullable fields | Required V1 behavior |
| --- | --- | --- | --- | --- |
| runtime exact reviewed Kimi Chat | rejected before usage/map decoding | safe integer removed; invalid value left for guardrail | protocol guardrail decides; usage already parsed is retained on delivery failure | unchanged |
| Kimi core v1 Probe | current decoder behavior retained | unknown field rejected | historical strict schema/null behavior retained, including root `null` reaching `reported_model` | no reinterpretation |
| Kimi core v2 reasoning/JSON Probe | current decoder behavior retained | unknown field rejected | v2 detailed-usage and strict envelope behavior retained, including root `null` diagnostics | no reinterpretation |
| Kimi reasoning-effort v1 Probe | duplicate members rejected | unknown field rejected | exact effort/detailed-usage behavior retained | unchanged |
| function-tools v3 Probe | duplicate members rejected | rejected as historical unknown field | v3 delivery fields and bounded integer-tree usage extensions retained | unchanged |
| function-tools v4 Probe | duplicate members rejected | safe integer removed; invalid value is `delivery_shape` | v3/v4 delivery and usage semantics retained | unchanged |

In particular, V1 may not add duplicate-member rejection to an existing Probe contract that did not previously require it. Doing so would silently change the meaning of already issued receipts. A desired acceptance-set change requires a separate Probe contract/version decision and is outside this batch.

## 4. Minimal implementation

### 4.1 Placement

Add one small helper beside the existing OpenAI-compatible adapter code, preferably:

`internal/adapters/openai_compatible_chat_nonstream.go`

This package is already consumed by both Gateway and `modelcapabilityqualification`, so no new dependency edge or package hierarchy is needed.

### 4.2 Internal API

The exact names may change during implementation, but the behavior must match this shape:

```go
type ChatNonStreamMechanics struct {
    RejectDuplicateMembers bool
    NormalizeToolCallIndex bool
}

type ChatNonStreamPrepared struct {
    Body          []byte
    Changed       bool
    DeliveryIssue ChatNonStreamDeliveryIssue
}

func ValidateOpenAICompatibleChatNonStreamDocument(
    body []byte,
    mechanics ChatNonStreamMechanics,
) error

func PrepareOpenAICompatibleChatNonStreamDocument(
    provider string,
    upstreamModel string,
    protocolProfile string,
    protocolProfileVersion string,
    body []byte,
    mechanics ChatNonStreamMechanics,
) (ChatNonStreamPrepared, error)
```

Rules:

1. Caller-owned body limits remain where they are. The helper never reads an unbounded stream.
2. `Validate...` runs `strictjson.Validate` before any usage or map decode when `RejectDuplicateMembers` is selected. `false` preserves historical last-wins typed-decoder behavior only where an existing immutable Probe contract already has that behavior.
3. `Prepare...` invokes the existing exact-scoped normalizer only when `NormalizeToolCallIndex` is selected. The helper never infers policy from provider family.
4. After the ambiguity check has succeeded, any normalizer failure over the same syntactically valid bytes—invalid child index or invalid `choices`/`message`/`tool_calls` structure—is one closed `tool_call_normalization_invalid` process-local delivery issue. It is not represented by an arbitrary `error`. The original body remains available for runtime guardrail rejection and usage retention; Probe v4 maps it to its existing `delivery_shape` category.
5. Duplicate ambiguity, malformed/multiple-document JSON and lexical `01` remain fatal `json` failures during the pre-usage ambiguity phase. They are never converted to a delayed delivery issue.
6. Root `null`, array or scalar inputs are not globally reclassified by this helper when an existing Probe contract currently reaches its typed-decode diagnostics. Runtime's final object decoder continues to require the existing object shape.
7. If no approved extension is removed, `Body` is the original byte slice and byte order is preserved. If removal is required, semantic equivalence—not JSON object key order—is the contract.
8. The helper does not validate finish reason, reported model, reasoning content, sentinel, tool identity, usage authority or route capability. Those remain role-specific assertions.

The mechanics are selected by two local deterministic functions from existing authorities, not supplied as arbitrary caller combinations and not treated as a new policy axis:

- runtime selector: exact Execution Profile ID/version + operation + provider/model + protocol ID/version;
- Probe selector: exact binding provider/model/protocol ID/version + Probe contract ID/version;
- there is no database field, public version, current pointer or registry for these options.

The helper rejects impossible combinations, including `NormalizeToolCallIndex=true` with `RejectDuplicateMembers=false`. A protocol ID match with a version mismatch is invalid, never a fallback to provider/model heuristics.

### 4.3 Runtime integration

For exact reviewed Kimi non-stream Chat routes only:

1. carry exact operation-binding protocol ID **and version** from `modelrouting.Route` into `RouteResult` and adapter response input; do not discard the version during compatibility-route conversion;
2. resolve mechanics from the exact execution tuple; an ID/version mismatch fails closed before the Kimi mechanics apply;
3. run only the shared ambiguity validation before any usage or ambiguous map decode, preserving the existing duplicate-member rejection;
4. parse token usage from the original provider bytes before delivery-shape classification;
5. classify exact normalization after token usage is established, then parse builtin meter evidence from the original provider bytes when the same document remains structurally traversable;
6. preserve every `tool_call_normalization_invalid` outcome: do not turn an invalid index or invalid `choices`/`message`/`tool_calls` structure into an early adapter error that discards validated token usage; retain the original body for the guardrail. If that already-invalid structure cannot be traversed for builtin-tool metering, retain no inferred meter quantity rather than discarding token usage or inventing non-token evidence;
7. apply SDK cache projection, then decode **once** with `UseNumber` from `NormalizedResponse.Body` into the process-local final structural object;
8. carry that final object in an optional field of `NormalizedResponse`; `relayNonStreamRequest` asserts/uses that same-body object instead of independently decoding Kimi again, while other adapters retain the existing fallback decode;
9. the canonical route guardrail remains the only customer-delivery decision.

DeepSeek, MiniMax, other providers and streaming do not migrate in this batch. Existing exact normalizers remain intact.

### 4.4 Probe integration

The Admin binding snapshot must pass the already selected exact protocol ID/version into the Kimi non-stream validator; it must not be inferred from provider family or hard-coded as Kimi v5. The production validator call fails closed when the snapshot protocol ID/version is not the exact binding expected by the existing Probe contract.

`readKimiChatCompletion` uses the shared document result and then performs the current strict typed Probe decode and contract-specific checks. Existing public validator signatures may be replaced or wrapped only as needed to carry the exact protocol without creating a second resolver.

Contract mechanics remain:

- core v1/v2: no new duplicate-member rule and no child-index normalization;
- reasoning-effort v1: existing duplicate-member rule, no child-index normalization;
- function-tools v3: existing duplicate-member rule, no child-index normalization;
- function-tools v4: existing duplicate-member rule plus exact safe index normalization.

Probe continues to own sentinel, finish, reported model, tool identity, JSON-object and qualification usage assertions. No new Probe contract is published.

## 5. Differential verification

### 5.1 Helper tests

- unchanged valid body preserves bytes;
- ordinary and Unicode escaped-equivalent duplicate keys are rejected only when the existing contract selects rejection;
- valid lexical child indexes (`0`, multi-digit) are removed only in exact reviewed scope;
- negative, fractional, exponent, quoted, null and boolean indexes remain delivery-invalid;
- unknown siblings are never removed;
- malformed and multiple-document JSON keep their existing fatal class;
- root `null`, array and scalar fixtures pin the existing v1/v2/v3/v4 Probe code/member/failure stage rather than being globally reclassified;
- no global UTF-8 or number ceiling is added;
- changed bodies are judged semantically, not by object key order.
- mechanics selectors reject protocol ID/version mismatch and impossible option combinations.

### 5.2 Historical Probe matrix

All existing Kimi v1/v2/v3/v4 tests remain authoritative, including:

- v1 rejecting v2 detailed usage;
- v2 bounded detailed usage;
- historical nullable fields remaining rejected;
- function-tools v3 accepting only its published provider usage extensions while rejecting child index;
- v4 accepting only the safe index and rejecting duplicate/invalid index values;
- K2.7 required reasoning and K3 effort behavior;
- diagnostics keeping stable code and bounded member name separate.

No old Probe receipt is regenerated, rewritten or re-approved.

### 5.3 Runtime/Probe cross-delivery matrix

Run all DeepSeek and Kimi non-stream fixtures through the affected local packages. Compare:

- customer-visible delivery semantics;
- error disposition and failure stage;
- tool identity/history;
- usage authority/finality and hold behavior;
- Probe observation fields required by the exact contract.

Runtime regressions additionally prove:

- `NormalizedResponse` final object decodes exactly from `NormalizedResponse.Body` after cache projection;
- only the closed `tool_call_normalization_invalid` class is delayed until the guardrail;
- duplicate/syntax failures occur before usage authority;
- invalid child index and valid-JSON `choices`/`message`/`tool_calls` type errors preserve the existing provider-usage hold reason and failure stage.

Do not require byte/digest equality when an approved extension is removed or an SDK usage projection is added. Do not require Probe and runtime to accept an input when their published contracts intentionally differ.

### 5.4 Local gate

WIP uses focused tests. Once the diff is stable, run the repository-required risk-proportionate local gate once. A failure with no code change is classified before any rerun.

## 6. Independent review and delivery sequence

1. Hash this exact design and send it with the baseline commit/tree, Active Milestone, invariants, non-goals and baseline test results to one independent read-only reviewer.
2. Reviewer dispositions are limited to `validated_blocker`, `validated_required`, `valid_deferred`, `rejected` and `product_decision_required`.
3. Only a blocker or main-Agent-validated current required finding changes this batch. Deferred findings do not create a backlog; product decisions stop for the user.
4. Implement the smallest reviewed diff.
5. Create one immutable candidate after local final gate; independently review the exact candidate. A necessary minimal fix receives at most one closing review.
6. Push/PR/CI follows the existing remote-write authorization only after a reviewed tip exists.
7. CI code changes are reviewed from the last reviewed tip; infrastructure-only failures rerun only the failed job when repository policy permits.
8. Target/live work is evidence-driven. This behavior-preserving refactor does not automatically require paid provider calls, new Probe/Approval, publication or deployment.

## 7. Stop conditions

Stop and return for product/architecture decision if any implementation requires:

- changing an existing Probe contract acceptance set or evidence interpretation;
- a new Probe/Profile/protocol/delivery-policy identity or version;
- schema, migration, Publication V1 declaration, current pointer, Release, Alias or canonical change;
- a new registry/framework/Canonical IR platform;
- buffering complete SSE or entering streaming convergence;
- changing usage, pricing, billing or customer-charge authority;
- advancing MiniMax/GLM qualification, Target or live state;
- provider calls, credentials or spend not separately authorized;
- weakening fail-closed delivery to make differential tests pass.

## 8. Non-goals and expected benefit

This batch does not claim to solve all model-adaptation delivery cost or the entire Probe/runtime delivery validator split. It removes one proven duplicate Kimi non-stream document-ambiguity/index-normalization boundary while preserving the distinct immutable Probe schema and customer-delivery guardrail.

It does not solve late provider characterization, streaming convergence, static publication cost, dynamic Target snapshot governance or every provider dialect. Those remain separate, evidence-triggered decisions.

Success means:

- no new full Kimi non-stream envelope parser;
- one shared prepared-byte/closed-issue result is consumed by runtime and Probe, and runtime decodes its final guardrail object once from the final body;
- existing published acceptance and evidence semantics remain unchanged;
- MiniMax/GLM remain paused at runtime qualification/Target boundaries until this seam proves reusable;
- no new authority axis or platform framework is introduced.
