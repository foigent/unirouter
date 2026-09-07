# Independent pre-code review and author adjudication

Review object: STREAM-OUTPUT-AUTHORITY-20260908.md,
SHA256 1518d76320925b794ac59270d7fb06b91374b14048f1098d7628668f65719b42.
Base d0365cdb04568c16a957a278a1979fea8f5145d8. Independent reviewer:
/root/stream_checkpoint_precode_review (Parfit), read-only source and official
supplier documentation. No candidate tests or Provider calls. The original
reviewed candidate remains byte-unchanged.

## Findings and adjudication

1. product_decision_required — ACCEPT. Replacing the byte upper bound with
   supplier-contract enforcement changes the pre-disclosure guarantee. N-10 is
   valid only conditional on the supplier's documented ten-token tolerance.
   Rejecting max_tokens and N<=10 also changes admissible shapes after activation.
   Historical releases alone do not preserve all clients of the stable model.
2. validated_required — ACCEPT. Candidate wording “reserves N ... financial bound”
   is incorrect for customer charging. Freeze is a hold, not a terminal ceiling;
   interrupted ChatV1 and process recovery use counter_v2. Per-frame rounding may
   estimate >N even where supplier actual<=N; existing top-up/pending rules remain.
   Any accepted mode must describe this explicitly, never clamp usage or silently
   change rates/charge formulas. Tests must cover small deltas, cancellation and
   real DB checkpoint recovery.
3. validated_blocker — ACCEPT. Merely setting streamTerminationCode loses known
   provider-final charge semantics by selecting delivered-counter fallback.
   HoldNonStreamOutput is not reusable as-is: it expects frozen/response_accepted
   and records output_withheld, false for already-delivered streams. Before coding,
   define an exact fenced stream contract-violation transition that preserves known
   supplier usage as provider evidence, actual delivery, no automatic debit/retry,
   route failure and idempotent recovery. It may reuse existing pending/journal
   storage, but lack of DDL is not presumed. No implementation is approved by
   this record.
4. validated_required — ACCEPT. Use finite existing frame/tool/buffer bounds plus
   a justified cumulative resource guard. Catalog/preflight/OpenAPI must project
   the new exact enforcement mode instead of unconditional utf8_byte_upper_bound.
5. rejected — ACCEPT rejection of generic registry/tokenizer service/full-platform
   redesign. Exact typed implementation + immutable Profile/Protocol/Release is
   sufficient in principle, subject to the terminal-state design above.
6. valid_deferred — Other providers, exact proprietary tokenizer, historical bill
   rewriting and HTTP2 are outside this candidate. HTTP2 remains separately
   authorized existing continuity work.

## Concrete product choice, distinct from implementation approval

Recommended future Qwen Flash/Max contract: use the exact supplier complete-output
contract with its explicit ten-token allowance; retain separate byte/resource and
request-time limits; verify final usage and hold a supplier violation for existing
provider-cost reconciliation instead of marking it success. Missing final usage
continues to use the existing disclosed-output estimator and frozen pricing,
clearly marked estimated; the requested token limit/Freeze is not a monetary cap.
Only max_completion_tokens with N>10 is admitted in this candidate. Stage and test
client compatibility before any stable Slot activation. Existing contracts remain
unchanged until an independently qualified new Release is explicitly activated.

Alternative: retain the conservative byte gate and current compatibility; it can
continue to truncate multilingual/reasoning streams earlier than the supplier's
requested token limit. An exact proprietary tokenizer is not currently available
as a verified alternative.

The choice does not authorize changing customer prices, inventing usage, clamping
charges, removing experimental budgets, retrying Provider attempts, or Production.
Even if chosen, the missing terminal-state design and tests must be completed
before implementation/activation; the user is not asked to waive that blocker.

经验文件无需变化. These findings are not an implemented reusable lesson yet.

## User decision and concrete implementation resolution

The user answered “按照你的建议” to the exact product choice on2026-09-08.
No further approval is needed for that choice. It does not close the technical
blocker. Existing prices, marked interruption estimates, budget counters and old
Release meanings remain unchanged.

Resolve the committed supplier-overrun state with the existing financial owner:

- Add a bounded HoldStreamOutput operation requiring org/request/attempt and the
  exact current live StreamFence plus final provider usage and actual delivered
  byte/chunk counts. Only the new typed contract invokes it for completion>N.
- Under the existing reservation/attempt/live-lease locks, persist the complete
  provider usage vector in freeze_idempotency with purpose=provider_evidence,
  finality=final and the unchanged frozen pricing identity. Use the already
  supported settlement_pending/provider_response_unusable hold, no charge/top-up
  and no automatic settlement scheduling. The automatic recovery index excludes
  that reason. Preserve the payload and meter journal using the same authorities
  as existing output holds; do not label it customer-charge usage.
- Append the real stream interrupted delivery receipt and update request
  idempotency counters in the same transaction. Use attempt delivery_failed /
  known_usage, failure_kind=provider_output_limit_exceeded, retry_authorized=false.
  Those states are permitted by M73; do not use output_withheld or outcome_unknown
  when usage/disclosure is known. Finish the exact stream lease as interrupted
  in that same transaction, preventing generic checkpoint recovery from replacing
  the committed provider hold with a customer estimate.
- The receipt and reservation payload retain stream owner/generation. Idempotent
  replay must match final usage, actual delivery counts, reason and frozen fence;
  a stale owner or different vector fails. Return success only after commit or
  exact commit-ambiguity readback. Terminal settlement never occurs in this
  method; the existing explicit provider-cost reconciliation owns resolution.
- Detect overrun before route success is chosen; use the existing single health
  outcome/fence to record provider_failure. A health callback error cannot erase
  the separately committed financial hold. Send an accurate SSE error, never a
  successful DONE. If no content was ever disclosed/no stream lease created,
  the existing nonstream-style undisclosed output hold is semantically usable;
  it must not be used for an already disclosed stream.
- A persistence failure is not a successful hold. Stop delivery, preserve the
  original failure and exact retry/readback context, and leave only the existing
  recovery authority. No claim can be made that volatile usage survives loss of
  both process memory and unavailable storage; tests distinguish a committed hold,
  known rollback and ambiguous commit. If there is no durable provider-final
  evidence, the user-approved existing marked interruption-estimate policy is
  the only fallback, never a fabricated supplier usage vector.

This design targets existing states/columns (M56/M73 stream support), but code and
real-DB tests must confirm all downstream readers respect the hold. DDL remains
unnecessary only if that verification succeeds; do not modify historical rows.

The new exact typed contract has independent finite resource ceilings:16MiB of
cumulative decoded model output and128MiB of upstream SSE wire input. Count all
wire lines, including ignored/empty frames, before parser acceptance; count
reasoning/content/tool deltas before disclosure. These are transport limits,
never token estimates. Existing2MiB default/16MiB maximum SSE line,4KiB enforced
inspection buffer and tool-argument limits remain. The bounds permit the existing
1MiB line fixture and substantial headroom over the retained5375-token real run;
128MiB also allows roughly1KiB framing per token at the current131072 public cap.
They do not promise to admit every conceivable tokenization/framing pattern.
Processing stays incremental; neither ceiling allocates an accumulating buffer.
The new contract publishes the caps and distinct resource error in catalog and
preflight. Test exact-limit, overflow, split-frame and ignored-frame exhaustion.

Preflight freezes accepted totalN, emittedN-10 and the exact mode; no later stage
subtracts twice. Missing defaults use the same derivation; every matching route
must agree on the exact field/scope. Reserved/estimatedN and input/context fit
stay unchanged. Use a new typed protocol/profile reference and existing support
manifest generation. Do not mutate the byte policy of the current protocol.

Remaining pre-code verification: inspect affected receipt/reconciliation readers
for the chosen existing states, then implement this exact narrow resolution.
Required tests include known overrun after disclosure, no disclosure, repeated
hold/settlement attempts, stale fence, failed/ambiguous commit and expired-process
recovery; existing normal interrupted-estimate and provider-final paths unchanged.

Reader verification completed before code: provider_reconciliation.go accepts
known_usage/delivery_failed for explicit refund/subsidy and rejects collect unless
funding_pending/customer usage. RecordDelivery preserves provider_response_unusable;
normal settlement requires customer-charge purpose and the exact live stream fence.
Existing freeze columns retain settlement_stream_owner_id/generation. An atomic
interrupted lease removes the hold from expired-stream claims, and the pending
settlement selector excludes provider_response_unusable. No DDL is needed for this
transition. Implementation shares the existing transactional delivery receipt
writer, with exact terminal-hold readback after an uncertain commit.

## Implemented candidate and validation for the sole closing review

Candidate base has advanced by fast-forward to mainf8da27d7 (PR407); original
pre-code artifact above remains unchanged. Local product1.2.70/M114, no DDL.
The exact closing patch is provided separately and includes all new files.

- Implemented the fenced known-overrun transition in the existing Billing owner,
  sharing RecordDelivery's transaction body. A partial first frame (positive bytes,
  zero complete chunks) remains write_failed, not an invented complete chunk.
- The same new complete-output protocol also checks final non-stream usage before
  disclosure using the existing undisclosed output hold. This prevents a stream=false
  request from bypassing the declared complete-output contract; old protocols
  remain unchanged. Both routes record a provider failure, not a success.
- Added protocol.qwen-openai-chat.flash.v8 and Qwen Probev2. The latter sends2038
  for an accepted2048 cap and rejects an overrun while retaining the observation.
  Typed runtime support includes both exact references. Oldv7/v1 remain readable.
- Candidate data sources are qwen-flash-provider-completion.json (Flash1.9.0,
  Definition804c01dd39c8de5c3a5600bedbd673696bf41ec67a6873389013942745f82b01,
  Bundle4a9a8caf7a30d1733fce6015fb1d19e64e3b7ca56fd28d0cde13705079b8ff42)
  and qwen-max-provider-completion.json (Max1.1.0,
  Definitionb6d68dc44cfae1d311b1aabae3605ebd1402f1eeef293ab321d39d90fecbf36e,
  Bundledfc9ad72d03345dfe223867e3652901da41eb0bfcffd557700fde8b4143aca39).
  Each retains the exact prior definition digest as predecessor, stable lineage,
  prices/limits/defaults, and six candidate capability bindings. No Publication
  receipt or Release is created by these files. Both formal source validators pass.
- Local real-DB tests cover committed/rolled-back/lost-ack transitions, concurrent
  exact replay, stale fences, failed receipt atomicity, no automatic settlement
  or expired-lease estimate after committed hold, explicit reconciliation, and
  relay through existing policy/Agent-cap billing wrappers in prepaid and
  cost-observation modes. Fixtures make zero Provider calls.
- Request tests cover omitted/default/tiny/legacy/conflicting limits, N versusN-10,
  repeated preparation, tools/reasoning, old-byte rejection, new final success,
  overrun and route-health failure, estimated interruption, preflight projection,
  finite decoded-resource cutoff and ignored-wire/exact-bound reader behavior.
  Exact data-catalog pins and runtime inclusion checks are separately exercised.
- Preserved existing checkpoint recovery regression and added required integration
  selectors to the existing CI lane; no check or assertion removed. Whole Go
  regression and targeted race checks are running; attach actual final outcomes
  rather than assuming success. No1.2.70CI/Artifact/Target claim yet.

Activation gate remains formal exact-main carrier -> non-activating Publication ->
paid bounded qualification -> independent Approval -> new sealed Release -> Slot.
Check actual OpenClaw max_completion_tokens shape before activation. Then validate
real Qwen results and loaded-stream/cancellation continuity, with protocol, answers,
provider usage, costs and performance kept distinct. This slice cannot retire old
writers or reopen all fourteen model tasks. 经验文件无需变化.

## Sole independent closing review completed

Parfit reviewed patchf4cc381e2416a3bac4fe32b614d57308dc8ff09bd3f35ab558fe42cbba2e8411
against basef8da27d7, first inspecting the raw candidate and then reconciling the
pre-code object, user decision and actual tests. No validated blocker or new
product decision remains. The exact prior technical requirements are closed;
no second settlement system, DDL or tokenizer service is required.

Independent five-package targeted race execution19147 and both official Bundle
checkers exited0. Author full `go test ./...`, `go vet ./...`, targeted race,
real relay/DB modes, metadata and catalog checks also exited0. Original logs are
retained in the task evidence directory. CI and every Target/Provider activation
step remain separately pending, never implied by this review. No Provider or
Target request was made by the reviewer. 经验文件无需变化.
