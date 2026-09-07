# Concentrated qualification and exact shared Probe observations

Pre-code review object, 2026-09-08. Code baseline is PR409 head
16edcbde9faf4838a6816d9f1329bb1872a008fd / tree
8c7d16849ecc0822934beb704969b31dc2cb175b. PR409 CI is pending;
Independent implementation starts from current main ac3e7600c5ed1ace59a475b262ad95d141be98a1. Git proves all admin, qualification, model-endpoint UI/API and route-owner files are byte-identical to the reviewed head. Sync PR409 after its merge before final validation.

## Accepted purpose and scope

The user authorized concentrated onboarding after the real Qwen Max first-model
closure, and exact equivalent paid Probe sharing with independent Approvals.
Flash/Max v8 Release59/60 already complete real two-round OpenClaw calls on
1.2.70/M114. This slice reduces repeated configuration actions and calls without
changing runtime contracts, prices, Release/Slot authority or credentials.
No new provider/model, fake data revision, old-writer retirement, generic workflow
platform, metering rewrite, Production, automatic activation or automatic approval.

Existing ModelEndpointBatchSheet saves selected models with existing connection /
credential IDs and canonical create idempotency. Its results stop at saved IDs.
ModelEndpointDetailsSheet renders separate capability panels. Current streamed
function-tool Probe wrappers build identical requests across several exact
capabilities and validate the same full tool/reasoning/usage contract. Each panel
currently dispatches a separate paid request. Current persisted probes are
append-only and have source_ref, per-capability fingerprints and exact revisions;
Approvals retain a unique capability_probe_id and independent actor/reason.

## User flow

Keep one saved connection/key and existing multi-model creation. Saved results
provide direct next-action links to the exact deployment. A concentrated
qualification panel presents all exact capability states in one table, one
credential choice and server-derived grouping. Show the number of physical paid
requests before an explicit Verify action. A fresh test is never automatic on
opening, refreshing, selecting a model or recovering a network error.

Successful groups show one shared observation reference and individual capability
facts. The operator explicitly selects eligible facts and approves them through
existing independent Approval commands. Show partial and unconfirmed outcomes,
re-read server states and never imply approval from Probe success. Link to the
existing Release Workspace for explicit seal/activation; do not build a second
Release writer. Detailed per-capability panels remain available for inspection
and non-equivalent contracts, not a second default workflow.

Client setup shows stable model, gateway base URL and a placeholder for an
application key (never provider/admin credentials). Reuse the existing
uniroute-openclaw-sync command, dry-run by default, actual application-key catalog
and request preflight; do not synthesize supportsTools/intensity from admin
qualification. No global OpenClaw default or credentials are changed by export.

## Exact server grouping, no model-name inference

Add a read-only administrative probe-plan endpoint under the existing deployment /
operation owner, accepting the selected binding ID. Scope and actor checks match
individual Probe. Return finite groups, exact capability IDs and a digest of the
relevant snapshot and generated request, plus unsupported/separate items. The
plan performs no Provider I/O, execution writes or Approval.

Only the existing typed streamed-function-tools contracts are initially eligible:
MiniMax M2.7, GLM, DeepSeek, Kimi, MiniMax M3, Qwen v1 and Qwen v2. Eligibility is
selected by exact Probe implementation identity/version, not provider/model names
or advertised booleans. For every proposed member, run its existing exact binding
validation and request builder. Group only when operation, immutable Profile /
Definition, Protocol and Probe contract IDs/versions, provider/upstream/endpoint,
connection/credential/deployment/operation revisions and exact serialized request
bytes match. Capability IDs and their individual support revisions remain pinned
and checked. A finite typed equivalence helper certifies that these wrappers use
the same complete outcome predicate after their binding check; any future
contract is separate until explicitly reviewed. Unknown/mismatched items cannot
be coalesced, and non-stream/cache/media/named/required contracts stay separate.

POST one selected group includes expected plan digest, binding and exact member
IDs. Rebuild and check the plan before dispatch; drift is 409 with zero sends.
Use the existing server-owned request, adapter, no-redirect bounded HTTP doer and
strict full-response validator once. An HTTP200 alone qualifies nothing. Do not
invent new parsers or infer reasoning/tool facts absent from validation. One
outcome can support all group members only because each exact binding and its
full predicate were proven equivalent before sending. Future per-capability
predicate changes must split the equivalence class.

## Persistence, identity and failure

Refactor the existing persist owner into a transaction-scoped helper, reused by
individual and grouped Probe. Group writes one transaction, with root/actor and
all relevant current rows locked before appending. Sort member IDs for lock order.
Recheck all frozen inputs; if any drifted, record the physical outcome as stale
and grant no new qualification to the group. Append one existing typed probe row
per capability, each with its own exact fingerprint and approval reference, but
all with the SAME server-generated physical source_ref. Store bounded group
metadata: group-contract version, request digest, member IDs, shared observation
reference anchored to the first immutable probe row. There is one physical
observation, projected into several capability facts; no historical rows are
changed and old observations lacking this metadata are not retroactively reused.
No new schema is expected, provided existing constraints/readers enforce these
semantics. Each original single Probe remains one physical observation.

The shared source_ref, not row count or repeated usage fields, identifies paid
calls. Audit/results explicitly report physical requests and per-capability facts
separately. Do not introduce an aggregate that sums shared usage repeatedly.
Existing financial/runtime ledgers are unchanged. Each Approval still invokes its
existing owner with the exact capability probe ID, actor, reason and current
safety checks. Existing immutable active Releases are not reinterpreted.

The existing Probe call is not universally exactly-once. This endpoint must not
add automatic network retries or rerun Provider I/O after a persistence failure.
Before transport dispatch failures have zero attempts; after Do starts, a network
error may be ambiguous, even without HTTP status. UI stops the batch on an
unconfirmed result and offers readback, not a retry-new-ID loop. An explicit fresh
Probe is a separately charged action; a page refresh cannot resend it. DB commit
ambiguity may be resolved read-only by physical source_ref when available; absent
rows do not prove no upstream call. Do not build a general durable command engine
or misuse AuditEvent as an external-side-effect receipt. If an immutable typed
receipt is required for stronger promised recovery, identify that as a separate
necessary contract rather than silently asserting exactly-once here.

## Verification and delivery

- Pure equivalence tests cover all supported families and negative profile,
  definition, protocol, contract, endpoint, key/revision, request and capability
  changes; same model name alone never suffices.
- Real isolated DB: one recorded HTTP dispatch -> several exact probe rows with
  one source_ref -> separate independent Approvals -> qualification; no automatic
  Approval/Release/Slot write. Existing active Release pins unchanged.
- Failure tests: unauthorized zero-send, wrong/stale plan zero-send, HTTP200 invalid
  tools/usage never qualifies, mid-flight actor/config change, partial DB failure
  rolls back grouped facts, commit ambiguity readback does not dispatch again.
- UI: one binding selection, one grouped Verify, per-item results/explicit
  approvals, partial failures, actor/model changes, late responses, refresh and
  request ambiguity. Non-equivalent capabilities remain visibly separate.
- OpenAPI, API docs, frontend types, applicable tests/build and exact CI checks.
- Real Target representative: at most one new bounded Flash or Max Probe within
  the existing cumulative budget; compare actual dispatch count to the previous
  six-call pattern and read exact facts/independent approval results. No need to
  rerun already completed Agent tasks if runtime and Release remain unchanged.
- Report user action count, paid request count and backend releases. Do not claim
  overall onboarding completed until configuration, verification, explicit
  activation and safe client setup have usable connected entry points.

Retirement: replace the repeated default panels with the concentrated entry;
retain supported individual API/history inspection and original approvals. No
historical migration/receipt or provider implementation is removed.

## Required review clarifications (implementation contract)

Physical source_ref is generated by the server for each fresh Probe, never from
plan digest or a reusable client ID. Add bounded dispatch_state to the outcome
and immutable group summary: not_started before Do, started immediately before
Do, response_received only with a real HTTP response. A started transport failure
is ambiguous. Distinct source_ref counts physical observation records, not actual
supplier invoices, and not necessarily requests that reached a Provider. Report
not-started, dispatch-attempted and response-observed counts separately.

Add exact group-by-source_ref readback under the same scoped deployment/operation
owner. Load complete append-only rows for org/deployment/operation/source-kind/ref;
verify group version, plan/request digest, one anchor and exact member set. Return
per-capability current status using existing qualification authority. Do not
assemble a group from latest LIMIT1. When a persistence error occurs after a
known physical ref was created, return the safe ref when the response can still
be delivered. If a lost HTTP response leaves the ref unknown, the UI remains
unconfirmed; identical plan/latest success does not prove this command completed.
No automatic retry or source-ref reuse after an absent group readback.

## Independent pre-code adjudication

Reviewer /root/onboarding_precode_review verified exact original design SHA
512a714fd68d74fdcef59ad73ece68d0ca2191bb5d727c93860a081c36e2b280 and
head16edcbde. No validated blocker/product decision. CQ-01 dispatch-state and
CQ-02 exact source-ref readback accepted in clarifications above. New table /
generic execution engine rejected as unnecessary for this bounded promise;
crash recovery of all never-persisted Provider facts remains explicitly deferred.
Current-state group recheck and atomic append remain required. Scope and runtime
contracts unchanged. No Target Probe is authorized beyond the existing budget.

## Local implementation and validation checkpoint

Implementation rebased by fast-forward to exact merged PR409 main07761d73,
version1.2.71, without changing the reviewed relevant baseline. Candidate1.2.72
uses the existing Probe transaction owner and source_ref; no new schema/writer.
CQ-01 and CQ-02 are implemented. No additional product decision was required.

Local evidence: affected admin/qualification/cmd Go race tests pass; full admin
integration race suite passes100.085s; eight explicit shared-observation scenarios
cover compiled/data Definition, current drift, unknown transport, invalid200,
partial-write rollback, committed-but-unacknowledged readback, and credential
decryption failure with not_started/zero HTTP dispatch. Unknown actors also
fail before dispatch. The eight-scenario race suite passes9.981s. Family fixture
checks cover DeepSeek, Kimi, MiniMax including highspeed and GLM; Qwen v1/v2 and
Kimi effortv2 remain distinct. No real Provider call in these tests.

Frontend89 feature/page/docs/API tests pass with process exit0 and zero
unexpected stderr/React act warnings (frontend-tests-r4). New tests cover no-send
mount/refresh, double clicks, target/actor changes, uncertain source-ref readback,
wrong observation identity, no-ref network loss, explicit selected approvals and
safe client placeholders. Build passes91 JS assets, largest362364B under460800B.
Go vet and release metadata checks pass. Retained failures were a missing test
mock for the new API and outdated default-panel assertions, plus a quoted YAML
description omission. Closing review CQ-CLOSE-01 correctly rejected an earlier
81-passed assertion summary: unexpected DOM/act warnings still caused exit1.
Legal pre containers and awaited asynchronous test completion resolve it;
17 targeted tests and the89-test suite now both exit0, with the original warning
reporter/baseline unchanged. Build rerun passes the same91-asset budget.

Sole closing review includes the corrective DOM/test diff and zero-send test.
CQ-CLOSE-02 remains valid_deferred: client Provider-name validation should align
with the existing sync CLI; the default unirouter value is valid. This does not
change this batch's supported default path.

Explicit Verify requests a fresh paid observation even for already-qualified
capabilities; only re-approval is skipped for qualified rows. Opening/refreshing
never dispatches. No automatic paid-observation reuse is claimed.

Sole closing review follows this exact patch. CI/Artifact/Target/shared paid Probe
remain unverified until separately recorded. The accepted next Target trial may
use at most one budgeted existing Qwen Probe, with no new model/revision or Agent
rerun. No old writer retirement. 经验文件无需变化.

Closing disposition: the same independent reviewer checked full patch
bf62248006e9b7535258065012c61516012e6e0cc1b597e5a2a89b5c47b7a869 and
corrective logs. CQ-CLOSE-01 is closed/rejected after correction; no remaining
validated_blocker/validated_required. CQ-CLOSE-02 remains valid_deferred as above.
Independent test rerun was unavailable after removing the task's dependency
symlink; conclusions use inspected source and actual original exit-zero logs.
