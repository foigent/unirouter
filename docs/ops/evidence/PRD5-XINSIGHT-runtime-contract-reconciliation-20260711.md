# PRD5 X-Insight Runtime Contract Reconciliation

> Date: 2026-07-11
> UniRouter pre-change baseline: `40a371df89a8d6b28f6bdeffd7539cc1b9e348d9`
> X-Insight audited baseline: `97d58e7` on `main`
> Scope: repository and shared-workspace reconciliation only; no deployment,
> target mutation, real request, credential change, or expanded data exchange.

## Outcome

X-Insight answered all 13 questions in UniRouter's runtime-contract request with
implemented facts or explicit `not implemented` / `not decided` values. The
answer closes the information request but does not create a durable runtime
contract.

The accepted cross-project boundary is:

- controlled, operator-mediated `xinsight.import-receipt.v1` sidecar
  reconciliation remains supported;
- UniRouter does not activate an exporter/outbox, automated receipt transport,
  downstream deletion, or a synchronous X-Insight dependency;
- `import_job_id` and `deep_link` remain reserved nullable columns and must not be
  populated;
- only the four warning codes in the canonical X-Insight v1 schema are accepted;
- future network or durable-job integration requires a new versioned contract
  and explicit acceptance.

## Confirmed X-Insight Facts

Source response:
`/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-x-insight-prd5-runtime-contract-required-information-response.md`.

Project-owned source record:
`/Users/m4/Desktop/projects/X-Insight/docs/architecture/runtime-contract-capability-status.md`.

| Area | Confirmed fact |
|---|---|
| Current intake | Synchronous local CLI, controlled file snapshot, persistent event readback, exclusive-create sidecar receipt |
| Receipt schema | `xinsight.import-receipt.v1`; statuses `validated`, `persisted`, `rejected` |
| Warning registry | `approval_manifest_hash_missing`, `persistence_not_requested`, `source_capture_gaps_present`, `source_exchange_id_missing` |
| Event idempotency | Persisted key is `(source_system_id, adapter_manifest_id, idempotency_key)`, not `(exchange_id, artifact.sha256)` |
| Target | Fixture/synthetic-only non-production target is `Target verified` for exact release artifact commit `b9e5811cc53a5cb06614956e66a1c54756617f0a` |
| Stable public surface | None; current read APIs are unauthenticated loopback-only development surfaces |

The X-Insight repository was clean on `main` at `97d58e7`; `go test ./...
-count=1` passed on 2026-07-11.

## X-Insight Capabilities Not Implemented

- durable import-job resource, lifecycle, API, stable identifier, or deep link;
- tenant isolation, job authorization, or guess-resistant job URL contract;
- receipt HTTP transport, transport auth, retry/status semantics, or receipt
  authenticity signature;
- retention enforcement, downstream deletion API, immutable deletion receipt,
  or read/download/export/delete audit;
- public collector API, adapter SDK, exporter/outbox, or continuous gateway
  integration;
- production LiteLLM, New API, one-api, OpenTelemetry/OpenInference, MCP, SDK,
  or business-system adapters;
- compatibility/deprecation policy and import batch/count/byte limits.

These are X-Insight or future joint-contract gaps. They do not reopen completed
UniRouter PRD5 implementation.

## UniRouter Finding And Fix

Confirmed finding: the Batch 1Y Data Exchange API had reserved nullable database
columns but still accepted non-empty `import_job_id` and `deep_link` values, and
accepted arbitrary non-empty warning codes. That behavior was broader than the
answered X-Insight v1 contract.

The reconciliation change:

- removes durable-job/deep-link fields from the receipt write request, so direct
  API attempts are rejected as unknown fields;
- omits both fields from receipt inserts, keeping the reserved columns null;
- enforces the four-code v1 warning registry and uniqueness;
- makes the frontend reject pasted receipts that claim an unimplemented durable
  job or deep link;
- preserves read-side null placeholders so operators see that X-Insight has not
  supplied these values.

## PRD5 Completion Classification

| Class | Current conclusion |
|---|---|
| UniRouter accepted PRD5 planning | Complete: scope, architecture audit, P5-F12 accept/defer/reject screening, ownership, dependencies, risks, priorities, batches, and evidence gates are documented |
| UniRouter accepted repository functionality | Complete; this contract lock and P5-F35 source behavior are CI verified |
| X-Insight collaboration implemented | Two controlled import/persistence/idempotent-readback loops, v1 sidecar receipt, and the fixture/synthetic-only target are verified |
| X-Insight collaboration not implemented | Durable job/deep link, automated/authenticated receipt, retention/deletion/audit, stable public adapters, continuous ingestion, and compatibility policy |
| Target evidence only | P5-F35 concrete target detail, migration down, TLS 443 behavior, and prerequisite-gated real upstream/SSO runs |
| Intentionally deferred/rejected | Full Scheduler V2, runtime plugins/marketplace, IAM rewrite, managed HA/IaC product, budget hard-block runtime, plaintext cache, SDK suite, service-account runtime authority, and customer SLA operations |

“Accepted PRD5 scope complete” does not mean the intentionally deferred/rejected
products are implemented, nor does CI or target evidence imply production
verification.

## Verification

Passed on 2026-07-11:

```bash
go test ./internal/admin -run 'TestCreateDataExchangeReceipt' -count=1
npm --prefix frontend test -- --run src/pages/__tests__/DataExchangePage.test.tsx
(cd /Users/m4/Desktop/projects/X-Insight && go test ./... -count=1)
go test ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

UniRouter's full frontend suite passed 25 files / 125 tests. PR #136 passed
Backend Tests, Frontend Build, and Docker Build. No target deployment is claimed.
