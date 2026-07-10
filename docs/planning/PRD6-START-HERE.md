# PRD6 Start Here

> Date: 2026-07-11
> Status: D6-01/D6-02 accepted; Batch 0B implementation active
> Owner: Codex
> Base commit: `deacae2d4164b98c5241cf72844f09f53ca7510d`

## Current Decision State

PRD6 is a separate planning and execution window. The User accepted the
recommended MVP boundary and policy-specific failure posture on 2026-07-11:

- the CI-verified repository-correctness fixes in Batch 0A may proceed because
  they repair existing behavior without adding a new product surface;
- P6-F03 through P6-F11 and P6-F16 through P6-F21 are accepted implementation
  scope, executed through the batch/PR/CI gates;
- P6-F22 through P6-F31 and other advanced/long-tail capabilities remain
  proposed or deferred and require separate decisions;
- PRD5 remains `Accepted Scope Complete` and is not reopened by PRD6;
- a PRD5 backlog or competitor feature is an input, not an automatic PRD6
  commitment.

## Read Order

1. `docs/ops/CURRENT-STATUS.md`
2. `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md`
3. `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md`
4. `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md`
5. `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md`
6. `docs/planning/prd6-adaptive-trust-hardening/hardening.md`
7. `docs/PRD-Phase6.md`
8. `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`
9. `docs/planning/PRD6-IMPLEMENTATION-PLAN.md`
10. `docs/planning/PRD6-BACKLOG.md`
11. `docs/issues/OPEN-ISSUES.md`
12. `docs/agent-workflows/AGENT-PROTOCOL.md`
13. `docs/agent-workflows/EXECUTION-RECORD.md`

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
- PR #139 verified the PRD6 Batch 0A implementation and Batch 0B audit
  documentation through final evidence head `6c8c192`, with successful `Backend Tests`, `Frontend
  Build`, and `Docker Build`.
- The source and focused-test scalability audit found P0 tenant-isolation,
  operation/idempotency, billing pool re-entry, terminal-state, post-delivery
  settlement, and revocation risks. These are source findings, not production
  incident or 100x capacity evidence, and no remediation is implemented by the
  audit document.
- X-Insight `main` and `origin/main` matched
  `e43add8e0184ba1eb059484053bc5e9d9f4cf6ed`; `go test ./... -count=1`
  passed. Phase 1 remains complete; no X-Insight product functionality is an
  input to the UniRouter PRD6 scope.
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

## Resolved Decisions And Next Optional Gate

1. **D6-01 MVP boundary — accepted.** Reviewed repository work first prevents
   one tenant, Agent retry, or failed settlement from affecting another charge;
   it then adds Agent capability/ancestor budgets, shared weighted admission,
   Routing Policy V1, durable usage finalization, cache-aware billing, and the
   guardrail/stream foundation.
2. **D6-02 enforcement failure mode — accepted.** Identity, tenant boundary,
   mandatory blocking safety, and hard budget fail closed; optional analysis
   continues only with durable degraded evidence; rate/capacity gets a very
   small bounded last-known-safe lease and then rejects, never unlimited
   fail-open.

UniRouter owns the request-path and its authoritative authorization, routing,
usage, billing, and audit facts. Other projects may consume a sanitized,
versioned contract asynchronously, but no external investigation feature or
runtime is part of the PRD6 MVP or a prerequisite for request success.

The optional next product gate is whether to adopt the Evidence-to-Control
Flywheel and which one of P6-F23 through P6-F31 to promote first. The recommended
first experiment is P6-F24 fixture-only Governance Digital Twin; no answer is
required to continue the accepted P0/P1 work. D6-01/D6-02 do not authorize
deployment, target mutation, paid shadow traffic, raw-payload expansion, a new
X-Insight contract, semantic response cache, video execution, or an IAM rewrite.
