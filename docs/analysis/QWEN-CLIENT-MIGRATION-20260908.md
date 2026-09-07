# Actionable output-parameter migration

Pre-code object, 2026-09-08. Base ac3e7600c5ed1ace59a475b262ad95d141be98a1,
1.2.70/M114. Scope is the already-approved max_completion_tokens-only Qwen v8
contract and its client migration; no alias normalization or acceptance change.

## Observed problem and exact change

Development Release59/60 passed generated installed-OpenClaw first/continuation
preflight and real two-round streamed read-tool tasks. The old max_tokens request
is correctly refused before Provider/Freeze, but both preflight and ordinary Chat
hide the validator's field reason behind validation_error.model_execution_profile.

Use the existing output-field rejection branch in chat_protocol_contract.go as
sole validator. Return a private typed output-parameter error carrying the rejected
finite field, accepted finite fields, and the existing internal diagnostic. Do
not parse error strings, rediscover current Profile, add a model whitelist, or
forward arbitrary diagnostics. Preserve the typed cause through the existing
errModelExecutionProfileRequestInvalid wrapper and canonical planner.

Project only the two public field names max_tokens/max_completion_tokens. Unknown
fields/contracts or malformed typed errors retain generic disclosure. Preflight
keeps existing reasons and adds optional parameter_errors containing stable code
unsupported_output_parameter, parameter, accepted_parameters and a fixed safe
message. Ordinary Chat keeps its existing HTTP400/code but uses the same safe
message: remove the rejected parameter and use the accepted field; never send both.
This describes spelling compatibility, not a guarantee that unchanged values fit
all contracts. No request bytes, route/credential, private Profile ID, upstream
body or internal error string enters the public result.

## Invariants and validation

No change to Profile/Bundle/Release bytes, limits, admission result, selected route,
normalization, Provider dispatch, metering, schema or history. Existing generic
reasons remain compatible. Authentication/model authorization runs first. Preflight
remains zero Provider/Freeze/execution writes and does not become an entitlement.

Cover Qwen v8 old-field rejection, both fields, valid new field, existing GLM reverse
field requirement, unknown diagnostic privacy, wrapped errors, unauthorized caller
and actual first/tool-continuation shapes. Test normal Chat400 and zero dispatch;
OpenAPI/schema examples and client migration documentation must match. Existing
installed-client zero-send capture and live70 evidence remain valid; this disclosure
change needs no repeat paid Agent task. Target verification uses preflight and one
rejected ordinary Chat request only, with unchanged Provider-attempt count.

Version the product as1.2.71 because public diagnostic behavior changes. Keep current
prices/30-call/CNY5 cumulative limits, old v7 contracts and global OpenClaw defaults.
No general error framework, UI redesign or metering change belongs in this patch.

## Independent pre-code adjudication

Review /root/client_migration_precode_review covered exact SHA c10df9963a6ca2aec2ba669b910b6e987746a384cb3b3a67002db2caee434b81.
No blocker. Accepted required findings: preserve Profile-invalid errors.Is and
typed errors.As without changing planner; reject the whole malformed finite-field
combination; require the outer Profile-invalid identity; preserve earlier conflicting
value precedence; add closed OpenAPI schema/privacy negatives. Normal rejected Chat
may retain its existing rejection trace; only preflight guarantees no execution writes.
Other parameter/value/reasoning diagnostics remain deferred. No new product decision.

## Closing review and local validation

The sole closing review /root/client_migration_precode_review verified patch
SHA d79674a29b513ca85d0f11352b5e63e94753912d3b3778cbe3fa375e76186d88,
15 paths, no blocker; all required findings satisfied. Exact local checks passed:
scoped field/preflight/continuation/admission tests; REQUIRE_TEST_DATABASE=1 gateway
race135.128s; cmd/uniroute and OpenClaw sync; affected vet; frontend types/API contracts,
ApiDocs7tests, production build/bundle budget90assets (largest362364B<460800B).
Reviewer also verified real70 request/usage/tool-result/receipt evidence. Local
fixture-description and sandbox failures are retained; no relaxed product check.
CI/Artifact/Target71 are pending. No additional paid Agent task is required.
GitHub Actions September usage read before CI:22 usage rows, net amount0; no budget,
runner or required-check change. This final addition records review evidence only.
