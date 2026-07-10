# PRD6 Start Here

> Date: 2026-07-11
> Status: formally started; discovery and scope gate in progress
> Owner: Codex
> Base commit: `deacae2d4164b98c5241cf72844f09f53ca7510d`

## Current Decision State

PRD6 has started as a separate planning and execution window. Its accepted
product scope is not yet equal to every item in this document set:

- the CI-verified repository-correctness fixes in Batch 0A may proceed because
  they repair existing behavior without adding a new product surface;
- the proposed MVP, advanced, and long-tail capabilities remain subject to the
  scope decisions listed below;
- PRD5 remains `Accepted Scope Complete` and is not reopened by PRD6;
- a PRD5 backlog or competitor feature is an input, not an automatic PRD6
  commitment.

## Read Order

1. `docs/ops/CURRENT-STATUS.md`
2. `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md`
3. `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md`
4. `docs/PRD-Phase6.md`
5. `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`
6. `docs/planning/PRD6-IMPLEMENTATION-PLAN.md`
7. `docs/planning/PRD6-BACKLOG.md`
8. `docs/issues/OPEN-ISSUES.md`
9. `docs/agent-workflows/AGENT-PROTOCOL.md`
10. `docs/agent-workflows/EXECUTION-RECORD.md`

PRD5 evidence remains under its existing paths. Do not rewrite it as PRD6
evidence.

## Verified Starting Baseline

- UniRouter `main`, local `HEAD`, and `origin/main` matched
  `deacae2d4164b98c5241cf72844f09f53ca7510d` after fetch; the worktree was
  clean before the PRD6 branch was created.
- PR #136, #137, and #138 were merged. `Backend Tests`, `Frontend Build`, and
  `Docker Build` were successful for all three.
- Fresh local verification passed: `go test ./... -count=1`, `go vet ./...`,
  frontend type-check, 25 Vitest files / 125 tests, and frontend build.
- PR #139 verified the PRD6 Batch 0A implementation with successful
  `Backend Tests`, `Frontend Build`, and `Docker Build`.
- X-Insight `main` and `origin/main` matched
  `e43add8e0184ba1eb059484053bc5e9d9f4cf6ed`; `go test ./... -count=1`
  passed. Phase 1 remains complete and Phase 2 product priority is the
  X-Insight-owned operator investigation UI.
- The accepted cross-project v1 boundary remains operator-mediated
  `xinsight.import-receipt.v1` sidecar reconciliation. It does not include a
  durable import job, deep link, automated/authenticated receipt transport,
  public adapter, exporter/outbox, or continuous ingestion.
- The two temporary Gate A JSON snapshots still exist and their SHA-256 values
  match the accepted retention decision. Exact-path deletion is due by
  2026-07-17; receipts, hashes, sanitized evidence, and persisted normalized
  evidence remain retained.
- Document-backed target evidence remains valid for Batch 1Z. This kickoff did
  not deploy, probe or mutate the target environment.

## PRD5 Closure Guard

PRD5's 36 acceptance rows each have completion evidence or an explicit
defer/reject/prerequisite decision. The following are not PRD5 source-code
completion defects and must not be used to reopen PRD5:

- P5-F35 concrete target route/failover detail;
- migration down, TLS 443, real upstream and real SSO target evidence;
- X-Insight capabilities beyond the accepted v1 receipt sidecar;
- previously deferred Scheduler V2, plugin/marketplace, IAM rewrite, managed
  HA/IaC, runtime budget hard-block, plaintext cache, SDK, service-account
  runtime permission, and customer-SLA operations.

## Minimum User Decisions

1. **MVP boundary:** accept or adjust the recommended PRD6 MVP: existing
   correctness repairs, Distributed Policy V1, Routing Policy V1,
   cache-aware token metering/billing, and the guardrail/stream interception
   foundation. Exact/semantic response cache remains opt-in advanced scope.
2. **Enforcement failure mode:** accept the proposed policy-specific posture:
   blocking safety policies fail closed; audit-only policies fail open with an
   explicit degraded trace; hard quota enforcement fails closed or uses a
   bounded last-known-safe state instead of the current unconditional Redis
   fail-open behavior.

Until these decisions are made, only Batch 0A and documentation/evidence work
may move to implementation.
