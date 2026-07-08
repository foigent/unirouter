# PRD5 X-Insight Gate A Coordination

> Date: 2026-07-08
> Status: planning coordination accepted by User, not implementation complete
> Scope: UniRouter PRD5 coordination with X-Insight Gate A

## Purpose

This file records the current UniRouter-side PRD5 coordination decisions for
X-Insight Gate A. It does not reopen PRD4 and does not implement runtime code.

Related shared-folder files:

- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-08-x-insight-gate-a-environment-and-prd5-coordination.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-08-unirouter-gate-a-environment-and-prd5-coordination-response.md`
- `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-08-prd5-structure-fields-before-exporter-proposed.md`

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

1. Let X-Insight import the sanitized snapshot locally and save Gate A evidence.
2. Use Gate A findings to prioritize remaining structural-field and payload
   policy gaps.
3. Keep UniRouter-owned evidence readiness moving in scoped PRD5 batches that do
   not depend on X-Insight runtime availability.
4. Revisit read-only views, exporter/outbox, GitHub governance, and shared ECS
   staging only after the earlier gates pass.
