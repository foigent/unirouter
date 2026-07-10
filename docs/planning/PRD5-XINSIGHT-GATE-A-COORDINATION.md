# PRD5 X-Insight Gate A Coordination

> Batch 1Y update: UniRouter Data Exchange Control is Local verified. Remaining
> X-Insight runtime information is consolidated in shared request
> `requests/2026-07-10-unirouter-prd5-runtime-contract-required-information.md`;
> UniRouter does not wait on that response for unrelated repository-owned work.

> Date: 2026-07-10
> Status: planning coordination accepted by User; Batch 1V produced controlled
> exchange `uxchg_f7de31e541516893a0d9b31cc0f3892a` with one explicit
> denied-scenario gap; X-Insight persistent import/readback is External
> integration verified at `4a562a6` / PR #9 and `5f0fa49` / PR #10; Batch
> 1W live-path fix is CI verified at `931400a` / PR #126; target
> deployment/rerun remains pending
> Scope: UniRouter PRD5 coordination with X-Insight Gate A

## Purpose

This file records the current UniRouter-side PRD5 coordination decisions for
X-Insight Gate A. It does not reopen PRD4 and does not implement runtime code.

Related shared-folder files:

- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-08-x-insight-gate-a-environment-and-prd5-coordination.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-08-unirouter-gate-a-environment-and-prd5-coordination-response.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-08-prd5-structure-fields-before-exporter-proposed.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-prd5-controlled-snapshot-handoff-prep.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-x-insight-data-exchange-control-surface-response.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-x-insight-exchange-envelope-and-controlled-run-request.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-controlled-run-authorization.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-exchange-envelope-and-controlled-run-response.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-10-gate-a-controlled-run-authorization-recommendation.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-atlas-execution.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-10-unirouter-gate-a-atlas-run-result.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-snapshot-20260710.json`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-persistent-import-and-receipt.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-execution-owner-and-review-process-update.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-x-insight-gate-a-import-result.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-x-insight-execution-owner-process-response.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-snapshot-20260710.receipt.json`

### Execution Contact And Review Process

- Codex is the UniRouter execution contact under Agent Protocol v2.0.
- Canary, Sentinel and Atlas are optional independent-review profiles, not
  mandatory serial gates.
- This internal workflow change does not grant target credentials, reassign
  `UR-XI-GATE-A-20260710-01`, or weaken per-action User approval.
- X-Insight should continue returning import receipts and schema/evidence
  feedback through the shared folder; P0/P1 findings block or reopen the
  affected UniRouter batch.
- X-Insight accepted the same single execution owner and optional independent
  review controls in its process response.

## Current Accepted Decisions

### 1. Data Access Sequence

X-Insight should use a two-step Gate A data-access path:

1. sanitized controlled snapshot first;
2. restricted read-only database or read-only views only after local snapshot
   import works.

Sanitized snapshot conditions:

- include only the minimum Gate A tables, initially `request_trace_events` and
  `call_logs`;
- include a small controlled scenario set: successful request, routing/provider
  failure if available, billing/quota/budget event if available, streaming
  request if available, and usage/cost call-log evidence;
- remove or mask secrets, raw prompt/response, tokens, credentials, customer
  data, and IPs unless explicitly needed;
- preserve IDs, `request_id`, timestamps, status, ordering, usage/cost fields,
  and sanitized metadata needed for importer, source-ref, capture-gap, and
  dedupe tests;
- keep real or environment-derived snapshots out of source control unless User
  explicitly approves a synthetic fixture version.

Read-only DB/view conditions:

- X-Insight local migrations, fixtures, and snapshot import must pass first;
- GitHub CI must be green before any shared environment update;
- access must be read-only and limited to required tables or views;
- prefer read-only views over whole-table privileges if feasible;
- do not place secrets, DSNs, passwords, or access tokens in shared docs;
- importer queries must use bounded filters/cursors and avoid unbounded scans on
  the shared development/test database.

Current Batch 1Q handoff rule:

- X-Insight imported and persisted the real controlled UniRouter snapshot,
  returned a reconciled `persisted` receipt, and read back 10 events/3 gaps;
- this closes External integration verification for the current snapshot but
  does not close the denied producer-side source gap or authorize wider access;
- UniRouter must receive a User/UniRouter-owner approval manifest with approved
  request IDs, scenario labels, a non-secret source instance label, and approved
  source access before generating a controlled snapshot artifact;
- no DSN, credential, raw prompt, raw response, raw IP, or raw upstream error
  may be written to the repo or shared folder.

Current Batch 1R approval rule:

- a `request_id` is treated as the receipt number for one UniRouter request;
- approval is a human data-minimization decision, not a technical broad-export
  switch;
- User/UniRouter owner approval should be recorded first in the shared approval
  worksheet, then converted into the JSON approval manifest used by the snapshot
  tool;
- `approved` rows may be exported as sanitized metadata-only evidence for that
  exact request; `pending` and `unavailable` rows must not be exported;
- auto-discovery, read-only DB/view access, and exporter/outbox remain later
  options because they widen access beyond the first Gate A sample set.

Current Batch 1S control-surface rule:

- a future UniRouter UI entry is appropriate only as a Data Exchange Control
  surface for outbound approval, export ledger, handoff artifacts, import
  receipts, and X-Insight deep links;
- UniRouter should not host X-Insight importer internals, normalized evidence
  store, read-side audit, investigation timeline, graph UI, policy simulation,
  or evidence packs;
- the first UI slice, if later approved, should be approval queue plus export
  ledger only; contextual links from Logs can add candidate request IDs later,
  but Logs should not become the source of truth for integration control;
- X-Insight should review stable receipt/deep-link fields before UniRouter adds
  durable schema or UI for receipt reconciliation.

Current Batch 1T exchange rule:

- each generated snapshot carries a new source-created `exchange_id`;
- manifest-selected snapshots carry the SHA-256 of the exact approval manifest
  bytes, while X-Insight computes the separate snapshot artifact SHA-256;
- Gate A reconciliation uses `(exchange_id, artifact.sha256)`;
- X-Insight's current receipt statuses are `validated`, `persisted`, and
  `rejected`; it has no durable `import_job_id` or deep link yet;
- existing requests retain exact-ID approval;
- newly created controlled requests require prior approval of scenario set,
  data classes, source instance, safety profile, retention, and operator;
- the operator uses unique scenario-marked `X-Request-ID` values and captures
  returned IDs directly; no broad request discovery is allowed.

Current Batch 1V authorization rule:

- User accepted `UR-XI-GATE-A-20260710-01` on 2026-07-10;
- Atlas is the only authorized operator and must stop if the target is
  production, the environment cannot be attested, or Atlas lacks access;
- the approved scenario set is exactly `successful_non_stream` and
  `model_permission_denied` with synthetic content and discarded response
  bodies;
- only the two returned and verified request IDs may be marked `approved`;
- temporary snapshot retention is seven days after X-Insight receipt
  reconciliation;
- the existing UniRouter ECS development/test deployment is not a new shared
  X-Insight staging environment and does not relax the separate staging gate.

Actual Batch 1V result:

- `successful_non_stream` produced one call log and seven trace events;
- `model_permission_denied` returned 403 but lacked returned-ID correlation and
  produced no source row because a pre-relay wrapper check bypassed Relay;
- the immutable snapshot reports two `not_available_from_source` gaps for the
  denied request and is safe for diagnostic X-Insight import;
- Batch 1W removes the duplicate pre-relay API-key check and passed CI at
  `931400a` / PR #126; the current snapshot remains unchanged;
- User accepted follow-up authorization `UR-XI-GATE-A-20260710-02`. Codex
  deployed target `e823abc293f996264763c6c1aca19d3d4d6e705f`, ran exactly
  one denied scenario, verified matching response/trace request ID and zero
  call-log/billing/provider work, disabled keys 36/37, and completed a second
  X-Insight persistent/idempotent/readback receipt.

### 2. GitHub Strategy

Do not create a shared private GitHub organization strategy now.

Reason:

- UniRouter and X-Insight are separate projects;
- X-Insight has a different future open-source strategy;
- both repos already have their own GitHub remotes and CI workflows;
- shared CODEOWNERS, branch protection, release, issue, and secret policies are
  not on the Gate A critical path;
- revisit shared GitHub governance only after Gate A evidence and PRD5
  structural-field planning are accepted.

### 3. ECS Staging

Do not create or update shared ECS staging before local Gate A import works and
CI is green.

Reason:

- the current UniRouter ECS environment is a shared development/test
  environment, not isolated integration staging;
- early X-Insight importer work should not be coupled to UniRouter runtime
  stability or deployment operations;
- local snapshot import should prove the evidence loop before network,
  deployment, security-group, secret, and rollback concerns are introduced.

### 4. Structural Fields Before Exporter

UniRouter PRD5 should prioritize structural evidence fields before implementing
a native exporter/outbox.

Accepted planning fields:

- `trace_id`
- `span_id`
- `parent_span_id`
- `schema_version`
- `source_component`
- `capture_mode`
- `decision_kind`
- `decision_result`
- `decision_reason`
- optional `policy_ref`
- optional `rule_ref`
- `actor_type`
- `actor_ref`
- optional `on_behalf_of`
- `payload_capture_mode`
- `payload_ref`
- `evidence_ref`
- optional `source_event_hash`

Exporter/outbox should be designed after X-Insight Gate A current-table import
reveals actual capture gaps and field quality issues.

## UniRouter Current Source Facts

`request_trace_events` currently contains metadata-only gateway trace events
with:

- `request_id`;
- org/project/API-key/owner/key-type context;
- event type, stage, severity;
- requested/resolved model, provider, channel;
- status, latency, metadata, and created time.

`call_logs` currently contains request usage/outcome facts with:

- `request_id`;
- org/project/API-key/owner/key-type context;
- requested/resolved model, provider, channel;
- token usage, estimated tokens, usage source/confidence;
- cost, latency, TTFT, HTTP/upstream status, upstream error, stream flag, IP,
  and created time.

Recommended importer cursor:

- use `(created_at, id)` for both tables;
- use `request_id` to join request trace events to call logs;
- treat missing call-log or trace-event rows as capture gaps, not importer
  failures.

Current metadata safety:

- `request_trace_events.metadata` is allowlist-sanitized on write;
- admin trace API sanitizes metadata again on read;
- current metadata sanitizer keeps only scalar allowlisted keys;
- trace events can mark `capture_gap='redacted'` when UniRouter drops
  restricted metadata, rejects unsupported value shapes, or truncates allowed
  strings;
- `PAYLOAD_CAPTURE_MODE` can be configured as `metadata_only` or `disabled`;
  invalid values are rejected and raw prompt/response capture is not enabled;
- inbound trace/session/actor/delegation context is ignored unless
  `EVIDENCE_CONTEXT_TRUST_SECRET` is configured and
  `X-UniRouter-Evidence-Auth` matches;
- raw prompt/response and secret-like keys are not allowed in trace metadata.

## Non-Goals

This coordination does not:

- add UniRouter runtime code;
- add migrations;
- expose live database credentials;
- create shared ECS staging;
- create shared GitHub organization governance;
- implement X-Insight importer;
- implement exporter/outbox;
- add raw prompt/response capture;
- promote any PRD4 row to `Production verified`.

## Next PRD5 Steps

1. X-Insight import/persistence/readback and receipt reconciliation are complete:
   10 accepted, 10 first inserts, 10 rerun duplicate/no-op, 0 conflict, and
   readback of 10 events/3 gaps.
2. Batch 1W PR #126, required CI and merge are complete at `931400a` under
   Agent Protocol v2.0.
3. Batch 1W target deployment, the exact authorized denied rerun, and temporary
   key disable are complete under `UR-XI-GATE-A-20260710-02`.
4. The second controlled snapshot is persistently imported, idempotently rerun,
   and read back; this closes the earlier request-correlation producer gap.
5. Use Gate A receipt findings to prioritize remaining structural-field and payload
   policy gaps.
6. Keep UniRouter-owned evidence readiness moving in scoped PRD5 batches that do
   not depend on X-Insight runtime availability.
7. Decide read-only views versus exporter/outbox from security, availability,
   scale, and operational ownership requirements rather than opening either
   automatically. X-Insight ECS target deployment/runtime remains unverified;
   no shared ECS staging claim follows from either receipt.
8. Remediate the current ECS public exposure under separate authorization and
   provision X-Insight separately; do not place it on the capacity-constrained
   UniRouter host.
