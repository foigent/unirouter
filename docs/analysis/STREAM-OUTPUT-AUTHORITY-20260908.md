# Stream output limits: exact authority and bounded delivery

Status: design candidate for one independent pre-code review. No code, contract,
Bundle, Release, pricing or Target change implements this candidate.
Base: d0365cdb04568c16a957a278a1979fea8f5145d8,
tree 4aa4d51ab9a912f6f6283800f110041e04857088, 1.2.68/M114.

## Active milestone and evidence

Continue the accepted productization work by removing premature Qwen stream
termination without silently weakening financial or execution authority. Exact68
is deployed to development; immutable schema/Publication/Release/Slot projections
are unchanged. Parameter UI and isolated OpenClaw sync pass; a real client request
passes preflight without Provider dispatch. No new paid success is claimed.

The two historical8192 Flash runs ended with stream_output_limit_exceeded; the
32768 experiment completed with5375 provider completion tokens. Current relay.go
1908–1972 uses UTF8 bytes as the pre-disclosure token upper bound and compares it
with ReservedCompletionTokens. PR405 corrected checkpoint units only. The byte
guard remains intentional conservative enforcement, not an exact tokenizer.

model_execution_runtime.go135–184 uses one estimated output number for upstream
limit, context admission and reservation. stream_runtime.go23–41 contains the
delivery reservation state. billing.go1232–1279 independently confirms that Freeze
is an admission hold, not a terminal charge ceiling; insufficient prepaid top-up
remains pending, never an implicit subsidy. This design does not change that rule.

Official sources refreshed2026-09-08:
- https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions
  distinguishes complete output from answer-only output and documents a possible
  ten-token deviation for max_completion_tokens. The supported list excludes
  third-party direct-supply models.
- https://help.aliyun.com/zh/model-studio/deep-thinking
  confirms complete-output and legacy answer-only controls differ.

These sources support only the exact supplier surface. They do not establish an
available exact local tokenizer for proprietary Qwen3.7, or prove a hard bound
against a supplier that violates its contract.

## Required semantic distinction

Keep four independent quantities in the existing execution request/evidence:
1. Requested/public completion limit and its semantic scope.
2. Exact upstream field/value after approved normalization.
3. Reserved financial exposure using frozen prices and the admission estimator.
4. Delivered customer estimate and provider-final usage, with existing sources.

A customer estimate must never silently become strict output-limit authority.
UTF8 bytes must never be represented as actual provider token usage. Current
Profile/Protocol/Release identities remain immutable. Existing contracts retain
the current conservative byte policy until an explicitly different contract is
published, qualified, sealed and activated through the existing authorities.

## Proposed bounded implementation, subject to review and authority decision

Add an exact typed output-enforcement component to ChatProtocolContract, with
explicit defaults for existing entries. It is implementation metadata generated
into runtime support requirements, not a second mutable policy registry. Unknown
components remain unexecutable. No model-name heuristic or arbitrary script/DSL.

The initial new component is limited to the already authorized Qwen3.7 Flash/Max
surface and max_completion_tokens, whose scope is reasoning plus answer. Do not
generalize from that field name to other suppliers or legacy max_tokens. Legacy
answer-only requests continue under the existing contract; a new candidate that
requires complete-output enforcement rejects incompatible shapes before Freeze.

For a requested total cap N, the proposed provider-bound contract emits N-10 and
reserves N, so the documented supplier deviation does not increase the accepted
financial bound. N<=10 is inadmissible under this component; there is no silent
fallback to a different scope. Record both numbers and the normalization reason.
Defaulted requests follow the same rule. Context fit includes the full reserved
completion bound. Do not change public/model caps or interpret a thinking budget
as additional output allocation. The fixed deviation is typed and versioned,
never an administrator-supplied arbitrary allowance.

For that component only, pre-disclosure UTF8 length becomes a separate bounded
resource guard rather than the token limit. A finite resource limit and the
existing request deadline still terminate excess transport, with a distinct
resource-limit error. The exact value must be justified from fixture sizes and
memory limits before coding; it is not acceptable to use an unbounded buffer.
Keep the existing guarded frame parsing, policy checks, delivery receipts,
checkpoint counter, cancellation and usage/settlement responsibilities.

Provider-final completion usage is checked against N. A supplier violation is
recorded as such and never relabeled successful protocol completion. Preserve
supplier observation independently of customer charge eligibility; use the
existing failure/ambiguous settlement contracts. Do not discard known usage,
invent zero cost, retry the upstream call or rewrite historical receipts.
The precise failure transition must be identified in existing code before the
candidate can enter implementation; no generic audit event replaces typed state.

Use a new exact protocol reference and immutable data Profile revision only after
the execution policy is accepted. The existing Bundle writer, materializer,
qualification, Release/Slot and runtime support checks carry it. Historical
definitions and active releases do not acquire the new behavior from this binary.
No schema change or new pricing lifecycle is proposed. This is one platform
implementation change, not evidence of a no-build data batch.

## Decision that cannot be hidden in implementation

The proposed mode relies on the supplier's exact token contract during delivery;
it cannot independently prove every emitted prefix fits a proprietary tokenizer.
The existing byte policy does provide that conservative pre-disclosure guarantee,
at the cost of early cutoff. They are different guarantees. The independent review
must determine whether the accepted task's unchanged fail-closed contract permits
the proposed mode, or whether this requires an explicit product decision. If it
does, prepare the concrete alternatives and keep this change unimplemented while
continuing unrelated authorized work. Do not treat approval of a bug fix as
approval to silently change the guarantee.

An alternative is a verified exact tokenizer/maximum-token-bound implementation,
but an unrelated Qwen open-weight tokenizer or heuristic ratio is not evidence
for the proprietary models. No dependency or service installation is authorized
merely by naming this alternative.

## Invariants and non-goals

- Permissions, emergency revocation, application budgets and frozen pricing stay
  authoritative. No Provider dispatch occurs without the existing Freeze/gate.
- No universal exactly-once claim; an ambiguous upstream attempt is not resent.
- No direct DB authoring, second writer, active-selector reinterpretation,
  changed admin identity, global client default, Production or horizontal models.
- No retroactive checkpoint/receipt correction; no new customer charge formula.
- HTTP2 rollout fault closure, concentrated onboarding and equivalent Probe
  sharing remain distinct accepted slices; this candidate does not close them.

## Verification before any activation

No candidate tests have run yet; source inspection and official documentation are
the evidence. Pre-code review is required by AGENTS.md because this changes
execution/finance boundaries. Required regression coverage if approved:
- Existing contracts produce byte-identical admission, outbound and cap failures.
- New component rejects unknown/answer-only fields, tiny limits, overflow and
  context mismatch before reservation or Provider I/O; N and N-10 are evidenced.
- Chinese/ASCII/reasoning/tool deltas exceeding N bytes but within supplier N
  complete without the old false token error, with bounded resource handling.
- Supplier usage overrun, missing usage, malformed terminal, partial-frame write,
  client cancellation and lease recovery preserve correct typed terminal states.
- One attempt/Freeze/terminal settlement, no automatic retry, current revocation
  still denies dispatch, old Release and support-incompatible fleets fail closed.
- Exact Bundle/Release qualification and only necessary paid Qwen difference
  tests after reading cumulative guards: Flash16/30,CNY3.361465/5 reserved;
  Max9/30,CNY2.626116/5 reserved. Reservations are not invoices.

Reviewer classifications only: validated_blocker, validated_required,
valid_deferred, rejected, product_decision_required. Read only; do not edit or
expand the accepted scope. Known blockers are the semantic decision, exact
supplier-overrun failure transition, and justified resource bound above.
