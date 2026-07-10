# PRD5 Batch 1Y UniRouter-Owned Completion Evidence

> Date: 2026-07-10
> Status: Local verified; PR CI and target migration/browser evidence pending
> Base: `935e64e45463c11be49e83d412cf49f1ab77b843`

## Outcome

All repository-owned runtime/UI work accepted by the Batch 1X P5-F12 screening
is implemented locally. Explicitly deferred or rejected products are not
silently reopened.

## Implemented

### Trace search and route visibility

- `GET /api/v1/logs` supports exact `trace_id`, `upstream_request_id`,
  `evidence_ref`, and `source_event_hash` filters and includes those identities
  in global search (`internal/admin/log_handlers.go`).
- Logs UI persists the filters in the URL and exposes exact evidence identity
  controls (`frontend/src/pages/LogsPage.tsx`).
- Request detail reconstructs route attempts and provider outcomes from existing
  trace events and shows failover count/final outcome without changing routing,
  retries, billing, or streaming.

### Data Exchange Control

- Migration `000019_prd5_data_exchange_control` adds approval candidates,
  exchange/member ledger, delivery/re-delivery history, and full current
  `xinsight.import-receipt.v1` count storage.
- Platform-admin-only APIs cover two approval modes, controlled-request ID
  sealing, generated artifact registration, delivery/re-delivery, sidecar
  receipt registration, latest receipt summary, full member/delivery/receipt
  activity history, and ledger-only artifact deletion
  (`internal/admin/data_exchange_handlers.go`, `cmd/uniroute/main.go`).
- Exact approvals and controlled seals require a matching organization-scoped
  `call_logs.request_id`; exchanges require approved and sealed items.
- Receipt `exchange_id`, source instance/system, schema, and optional manifest
  hash are reconciled. Artifact hash mismatch produces `receipt_outdated` and
  does not fabricate downstream completeness.
- Every state-changing operation and ledger view writes an admin audit event.
- The frontend page separates source workflow from X-Insight receipt state,
  parses the current sidecar shape, shows the metadata-only safety boundary,
  and keeps missing `import_job_id`/deep links explicit.

### Redacted topology and deferred-control designs

- Runtime Status now returns a logical current-process topology for UniRouter,
  PostgreSQL, and Redis with redacted identifiers and explicit
  `network_exposure=not_observed` / multi-instance limitations.
- `/status` renders that limited topology without hostnames, IPs, DSNs, or cloud
  identifiers.
- `docs/planning/PRD5-DEFERRED-CONTROL-DESIGNS-20260710.md` completes the accepted
  design-only budget hard-block, service-account, and incident/SLO contracts.
  Their runtime implementation remains deferred.

## X-Insight Inputs Still Required

The consolidated open request is:

`/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-prd5-runtime-contract-required-information.md`

It asks for durable import-job/deep-link, receipt transport/authenticity and
idempotency, canonical receipt schema/warnings, retention/deletion, X-Insight ECS
runtime evidence, public adapter APIs, compatibility, and batch limits. It asks
for no credential, raw payload, raw IP, deployment, or ECS mutation.

## Verification

Passed on 2026-07-10:

```bash
go test ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Frontend result: 25 files / 124 tests passed. Existing React Router future-flag,
Semi Form, `findDOMNode`, intentional error-path logging, and large Vite chunk
warnings remain non-failing.

## Boundaries

- No deployment, real request, credential change, X-Insight runtime call,
  exporter/outbox, broad discovery/export, raw prompt/response capture, ECS
  mutation, or PRD4 change occurred.
- Migration `000019` was not applied to a target database in this batch. PR CI,
  Docker build, migration apply/rollback, and target browser evidence remain
  external verification, not local product gaps.
- X-Insight ECS, durable import jobs/deep links, callback/API transport, public
  adapters, and downstream retention/deletion receipts remain X-Insight-owned.
- Full Scheduler V2, plugins/marketplace, IAM rewrite, managed HA/IaC, budget
  hard-block runtime, plaintext cache, full SDK, persistent service accounts,
  and customer SLA operations remain deferred/rejected per P5-F12.
