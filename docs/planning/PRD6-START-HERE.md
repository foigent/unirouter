# PRD6 Start Here

> Date: 2026-07-12
> Status: D6-01/D6-02/D6-08/D6-09/D6-10/D6-11 accepted; P6-F03-P6-F08, P6-F16-P6-F19, P6-F21, P6-F32-P6-F39 and P6-STREAM-002A applicable rows are CI verified / closed; P6-F11 remains in progress
> Owner: Codex
> P6-F16 merge: `f244f669c529fec8767f85fff209882b333d8ed2`

## Current Decision State

PRD6 is a separate planning and execution window. The User accepted the
recommended MVP boundary and policy-specific failure posture on 2026-07-11:

- the CI-verified repository-correctness fixes in Batch 0A may proceed because
  they repair existing behavior without adding a new product surface;
- P6-F03 through P6-F11 and P6-F16 through P6-F21 are accepted implementation
  scope, executed through the batch/PR/CI gates;
- D6-08 accepts P6-F32 through P6-F36: tenant roles remain confined to their
  JWT organization; global prices/settings/license/global sensitive words and
  organization multipliers are `platform_admin` only;
- P6-F22 through P6-F31 and other advanced/long-tail capabilities remain
  proposed or deferred and require separate decisions;
- D6-10 selects reservation-aware long-stream Scheme A, while D6-11 selects
  explicit supervisor capability exchange with a five-minute token and
  separate workflow lifetime/budget limits;
- PRD5 remains `Accepted Scope Complete` and is not reopened by PRD6;
- a PRD5 backlog or competitor feature is an input, not an automatic PRD6
  commitment.

## Read Order

1. `docs/ops/CURRENT-STATUS.md`
2. `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md`
3. `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md`
4. `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md`
5. `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md`
6. `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md`
7. `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md`
8. `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md`
9. `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md`
10. `docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md`
11. `docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md`
12. `docs/ops/evidence/PRD6-B0B-P6F36-model-governance-tenant-isolation-local-20260711.md`
13. `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md`
14. `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md`
15. `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md`
16. `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md`
17. `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md`
18. `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md`
19. `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md`
20. `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md`
21. `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md`
22. `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md`
23. `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md`
24. `docs/ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md`
25. `docs/ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md`
26. `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md`
27. `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md`
28. `docs/planning/PRD7-CANDIDATE-BACKLOG.md`
29. `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md`
30. `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md`
31. `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md`
32. `docs/planning/prd6-adaptive-trust-hardening/hardening.md`
33. `docs/PRD-Phase6.md`
34. `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`
35. `docs/planning/PRD6-IMPLEMENTATION-PLAN.md`
36. `docs/planning/PRD6-BACKLOG.md`
37. `docs/issues/OPEN-ISSUES.md`
38. `docs/agent-workflows/AGENT-PROTOCOL.md`
39. `docs/agent-workflows/EXECUTION-RECORD.md`

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
  Build`, and `Docker Build`; it was squash-merged as `62f161e`.
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
- P6-F16 is CI verified and squash-merged by PR #140 as `f244f669`. Backend
  logs prove the migrated PostgreSQL matrix and all twelve authenticated
  subtests passed rather than skipped; post-merge `main` CI also passed.
- P6-F32 through P6-F36 are later source-confirmed administrator-surface inputs
  accepted by D6-08. P6-F32/P6-F39 are merged and CI verified by PR #145 /
  `609385f`; P6-F33 is CI verified by PR #142 / `7173c59`; P6-F34 was
  squash-merged by PR #146 as `517ce45381429781f7e81e492bdd9f38093c21ba`,
  final-head run `29137670055` and
  post-merge main run `29137781625` passed all required jobs, and its historical
  branch is retained. P6-F35 PR #147 final head
  `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1` passed run `29139408226`, was
  squash-merged as `c08db96167837ade685708c2ab204861a8ecc0ca`, and post-merge
  main run `29139534691` passed all required jobs; its historical branch is
  restored and retained. P6-F36 PR #148 final evidence head `75be4f3` passed
  required CI run `29140739477`, was squash-merged as `d83dbbc`, and post-merge
  main run `29140850345` passed all required jobs. Neither row has a
  target/production claim. P6-F37 and P6-F38
  are merged and CI verified by PR #143 / `a1e0ad6`.
- P6-F17 and P6-F18 are CI verified / closed by PRs #149, #151 and #153;
  their post-merge main required runs passed. P6-TEST-DB-001 is separately CI
  verified / closed by PR #155, merge `4b18ace` and main run `29157561036`.
- P6-F03/P6-F04/P6-F05 are CI verified / closed by PRs #156/#157/#158 and their post-merge
  main runs. `effective-config.v1` exposes the driving
  `routing-config.v1` and project-scoped budget-alert contract; routing state
  transitions and two-tenant budget scope pass against real PostgreSQL, and
  ambiguous global alert keys are retired. PR #158 merged as `ca02ecb`; main
  run `29160494169` passed all required jobs.
- P6-F06 is CI verified / closed. P6-F18A already freezes the
  multiplier-adjusted price; the dedicated isolated PostgreSQL matrix proves
  insufficient rejection without residue, exact 32-way settlement with one
  terminal receipt and zero uncovered cost, and full idempotent cancel/refund.
  Required Backend runs the matrix fail-closed; PR #160, merge `15ea8a2` and
  post-merge main run `29161527988` passed all required jobs.
- P6-STREAM-002A is CI verified / closed by PR #162, merge `7737f8b` and
  post-merge main run `29168586124`; its historical branch is restored. It replaces
  the hidden 64-KiB scanner and timeout/writer/body-lifecycle defects with
  `stream-transport.v1`, safe partial failure settlement and focused race/UI
  gates. The remaining D6-10/P6-F11 work is pending.
- P6-F19 is CI verified / closed by PR #164/main run `29170507588` and PR
  #166/main run `29171807693`; its capability and ancestor-runtime evidence
  remains separate from generic policy and durable terminal recovery.
- P6-F07 is CI verified / closed by PR #168 exact-head run `29173848811`,
  squash merge `2610e317` and post-merge main run `29174001859`. Shared
  selector RPM/RPS/TPM/concurrency, shadow/enforce, bounded last-known-safe,
  policy evidence, control UI and safe migration rollback passed required CI.
  P6-F20, D6-10 and target capacity remain unclaimed.
- P6-F21 is CI verified / closed by PR #170. Exact head `a9d8f3e` passed run
  `29176160987`; squash merge `04af91f` and post-merge main run `29176358459`
  passed Backend Tests, Frontend Build and Docker Build. Two/three-replica real
  Redis, fairness, crash/fault, bounded queue, typed error, UI and OpenAPI
  evidence are covered; the historical branch is restored at its exact head.
  Batch 1 is closed without a deployment or target-capacity claim.
- P6-F08 Routing Policy V1 is CI verified / closed by PR #172. Exact head
  `0e374d3` passed run `29178419505`; squash merge `54d8833` and post-merge
  main run `29178588075` passed Backend Tests, Frontend Build and Docker Build.
  Deterministic weight, bounded snapshots, renewable cross-replica probes,
  epoch CAS, per-channel thresholds, Chat/Embeddings parity, safe preview/
  trace/UI and guarded migration are covered. The historical branch is
  retained; no target or capacity claim. P6-F09 is the next accepted row.

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
3. **D6-08 administrator authority split — accepted with option B.** Tenant
   roles are confined to their JWT organization. Global prices, settings,
   license, global sensitive words and organization multipliers require
   `platform_admin`; no new global billing role is implied.
4. **D6-09 closeout sequencing — accepted.** Finish PRD6 repository scope
   first. D7-UPGRADE-001/002 is the selected future PRD7 direction, but all
   PRD7 implementation and target-evidence execution are deferred until PRD6
   closeout. Historical branches remain retained; the existing Gate A file
   deadline is unchanged.
5. **D6-10 long-stream hard budget — accepted as Scheme A.** Extend an
   existing reservation idempotently before a guard band; on failure stop the
   upstream and settle only delivered output. The current transport slice is a
   prerequisite and does not yet implement extension.
6. **D6-11 child-Agent capability lifecycle — accepted.** Child discovery is
   explicit supervisor exchange/registration. Capabilities last at most five
   minutes and normally refresh around minute three with jitter/singleflight;
   normal expiry blocks new admission without killing admitted work. Active
   revocation converges across replicas within 30 seconds. Workflow lifetime,
   depth, fan-out and budget remain separate, and UniRouter grants model-gateway
   authority only.

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

D6-08 authorizes P6-F32 through P6-F36 through separate reviewable PRs.
P6-F32-P6-F39 are merged and CI verified. P6-F35 was
squash-merged by PR #147 as `c08db961`; post-merge main run `29139534691`
passed all required jobs. P6-F36 PR #148 final evidence head `75be4f3` passed
required CI run `29140739477`, was squash-merged as `d83dbbc`, and post-merge
main run `29140850345` passed all required jobs.
P6-F37/P6-F38 are merged and CI verified by PR #143; post-merge main run
`29133690338` passed Backend Tests, Frontend Build and Docker Build.

P6-F17 is CI verified for the accepted metadata-only Scheme A: UniRouter
generates the server operation ID, keeps client correlation separate, scopes
idempotency by tenant/API key/payload, and records workflow/Agent/attempt
identity. PR #149 and post-merge main required CI passed; it does not store or
replay a model answer and does not claim P6-F20. P6-F18 is separately CI
verified by PR #151 and #153: billable stream output is durably marked before
delivery, final usage is immutable, and failed immediate settlement is retained
for leased restart recovery rather than refunded. Its post-merge main run
`29156096777` passed; per-chunk process-kill journal and the general outbox
remain P6-F20. The complete response-cache
Scheme B is a future required PRD7 Cache Service direction only.

P6-F35 adds no global-settings UI. D6-09 defers target execution; a later
authorized activation must drain old control-plane binaries and be non-rolling,
because mixed old/new binaries are not safe for these endpoints. New-version
replicas converge committed global-word changes through the existing
approximately one-minute reload. Typed config ownership plus the budget/alert
key, scope and audit truth chain are CI verified / closed by P6-F05; durable ambiguous-commit recovery
remains P6-F20 rather than being claimed by P6-F17 Scheme A.

P6-F36 does not reopen P6-F16. It adds no migration, RLS/foreign-key redesign,
unregistered-model denial, `/v1/models` semantic change, selector UI or target
cleanup.
