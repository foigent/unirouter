# Current Execution Record

**Batch:** PRD6 Batch 0B P6-F16 Tenant Isolation
**Owner:** Codex
**Risk:** R4 multi-tenant authorization and project policy enforcement
**Status:** P6-F16 CI verified by PR #140; P6-F32-P6-F36 are separately confirmed unaccepted inputs
**Base:** `62f161e2866a3a4c699abb9a647feef48bded1b2`
**Branch:** `codex/prd6-b0b-tenant-isolation`
**Previous phase:** PRD5 Accepted Scope Complete; target backend evidence remains `95d1da9` / PR #133

## Scope

1. Bind every accepted-scope key/user/project operation to the authenticated
   non-platform actor organization.
2. Block organization-admin creation, promotion, mutation or deletion of
   platform authority, including the final update race.
3. Bind project membership to JWT org, database user org and project org.
4. Make project model-set replacement same-org and atomic, and prevent malformed
   historical cross-org assignments from influencing live model admission.
5. Prove same-tenant, cross-tenant, platform/legacy exception, org-zero
   fail-closed and no-side-effect behavior through a migrated PostgreSQL/JWT
   matrix.
6. Record later confirmed administrator-surface gaps without expanding P6-F16
   or reopening PRD5.

## Behavior Change

- Non-platform key/user/project list, read and mutation sinks are constrained to
  the signed actor organization.
- Organization admins cannot create/promote platform roles or mutate/delete an
  existing platform target; the final SQL predicate closes the role-change race.
- Project-scoped membership requires consistent user/project/JWT organization.
- Project model-set replacement validates before mutation and commits delete/
  insert atomically; the execution query ignores cross-org assignments.
- Same-tenant and platform/legacy-admin behavior remains available.
- No new X-Insight runtime field or dependency is introduced.
- P6-F32-P6-F36 change no runtime behavior in this branch. They remain new
  source findings and proposed acceptance evidence, not implemented fixes,
  incidents, target verification, or global tenant-isolation proof.

## Files And Evidence

- Baseline evidence:
  `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md`.
- Product/competitor evidence:
  `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md`.
- Scalability/anti-pattern evidence:
  `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md`.
- P6-F16 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md`.
- Follow-up administrator surface audit:
  `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md`.
- Adaptive-trust innovation evidence:
  `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md`.
- Structural hardening portfolio:
  `docs/planning/prd6-adaptive-trust-hardening/hardening.md`.
- Truth chain: `docs/planning/PRD6-START-HERE.md`, `docs/PRD-Phase6.md`,
  `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`,
  `docs/planning/PRD6-IMPLEMENTATION-PLAN.md`, and
  `docs/planning/PRD6-BACKLOG.md`.
- Code/tests: `internal/admin/{key,user,project,rbac_helpers,model_governance}_handlers.go`,
  `internal/modelgov/modelgov.go`, `internal/admin/tenant_isolation_test.go`,
  `internal/admin/tenant_isolation_integration_test.go`, and
  `internal/modelgov/modelgov_test.go`.

## Verification

Pre-change repository results on 2026-07-11:

- `go test ./... -count=1`: PASS;
- `go vet ./...`: PASS;
- `npm --prefix frontend run type-check`: PASS;
- `npm --prefix frontend test -- --run`: PASS, with existing React Router,
  Semi Form, and `findDOMNode` warnings; 25 files / 125 tests;
- `npm --prefix frontend run build`: PASS, with existing large chunk warning;
- `git diff --check`: PASS;
- X-Insight `go test ./... -count=1`: PASS.

Focused Batch 0A post-change results:

- `go test -race ./internal/gateway -count=1`: PASS;
- `go test -race ./internal/middleware -count=1`: PASS;
- strict limiter tests repeated 20 times: PASS;
- real Redis 8.8.0 script check admitted two and rejected the third: PASS;
- focused middleware vet and `git diff --check`: PASS.

Focused read-only Batch 0B audit results on baseline `d5a5249`:

- `go test ./internal/auth ./internal/middleware ./internal/admin ./internal/billing -count=1`: PASS;
- `go test ./internal/gateway ./internal/middleware ./internal/billing ./internal/circuitbreaker -count=1`: PASS;
- `go test -race ./internal/gateway ./internal/middleware ./internal/circuitbreaker -count=1`: PASS;
- `git diff --check` and cited-repository-path existence check: PASS;
- these results do not cover PostgreSQL/Redis multi-instance races or prove a
  100x workload.

Focused P6-F16 results on `codex/prd6-b0b-tenant-isolation`:

- regression-before-fix suite: FAILED on the expected tenant, membership,
  model-set atomicity/runtime and role-race paths;
- `go test -race ./internal/admin -run '^TestP6F16' -count=1`: PASS;
- the authenticated test created a temporary database, applied migrations
  `000001`-`000019`, and ran twelve subtests without skip;
- `go test ./internal/admin -count=1`: PASS;
- `go test ./internal/modelgov -count=1`: PASS;
- `go test -race ./... -count=1`: PASS, with the existing macOS linker warning;
- `go vet ./...`: PASS;
- frontend type-check and build: PASS;
- frontend standalone rerun: PASS, 25 files / 125 tests; an earlier run
  concurrent with race/vet/build had one five-second Data Exchange test timeout;
- `git diff --check`: PASS;
- independent final code/test review: no P6-F16 blocker;
- PR #140 implementation head `ee025ec04b677c7ba0371b9a247f0ca70ec9aa0e`:
  `Backend Tests`, `Frontend Build`, and `Docker Build` PASS;
- Backend CI log: migrated P6-F16 integration and all twelve subtests PASS,
  not SKIP; injected partial-write rollback regression PASS;
- final status/evidence head must pass the same three required checks before
  merge.

Full Batch 0A/kickoff repository results:

- `go test ./... -count=1`: PASS;
- `go vet ./...`: PASS;
- `go test -race ./internal/gateway ./internal/middleware -count=1`: PASS,
  with the existing macOS linker `LC_DYSYMTAB` warning only;
- `npm --prefix frontend run type-check`: PASS;
- `npm --prefix frontend test -- --run`: PASS, 25 files / 125 tests, with the
  existing framework warnings;
- `npm --prefix frontend run build`: PASS, with the existing large-chunk
  warning;
- `git diff --check`: PASS.

PR #139 required checks for implementation commit `000ea64`:

- `Backend Tests`: PASS;
- `Frontend Build`: PASS;
- `Docker Build`: PASS.

PR #139 required checks for final scalability-audit evidence head `6c8c192`:

- `Backend Tests`: PASS;
- `Frontend Build`: PASS;
- `Docker Build`: PASS.

P6-F01, P6-F02 and P6-F16 are `CI verified`. P6-F32-P6-F36 are documentation/source
evidence only and remain unaccepted/unremediated; documentation verification
does not promote them to implemented or target verified.

No deployment, target mutation/probe, real or paid request, credential change,
exporter/outbox, deletion or expanded data exchange occurred. Migration down,
TLS 443 behavior and concrete target route/failover detail remain target-evidence
debt. Unimplemented X-Insight runtime capabilities remain external.

## Previous Batch 1Z Record

PRD5 Batch 1Z remains Target verified in
`docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`.
It records migration `000019` forward apply, stable/canary deployment, selected
browser evidence, canary parity, cleanup, and the authorized `INFRA-SEC-001`
least-privilege cutover. The target backend remains `95d1da9` / PR #133.

## Previous Batch 1W Record

## Scope

1. Remove the duplicate pre-relay API-key `allowed_models` denial from the live Chat wrapper.
2. Keep project model-governance resolution in the wrapper and let Relay own API-key denial correlation and decision evidence.
3. Add full-handler integration coverage for response correlation, trace evidence, and pre-billing termination.
4. Preserve the immutable Gate A snapshot as diagnostic evidence and reconcile X-Insight persistent import/readback without rewriting target history.
5. Replace the mandatory Codex -> Canary -> Sentinel -> Atlas chain with one execution owner plus optional independent review.

## Behavior Change

- A project-allowed but API-key-denied Chat request now reaches Relay.
- Relay returns HTTP 403 with `X-Request-ID` and records `auth.model_not_allowed` decision evidence.
- The denial still occurs before billing freeze, routing, provider dispatch, or call-log write.
- Project model-set, retired-model, unregistered-model and alias-rewrite behavior remains wrapper-owned.

## Files And Evidence

- Code: `cmd/uniroute/gateway_handlers.go`
- Integration coverage: `cmd/uniroute/prd2_p0_e2e_integration_test.go`
- Local evidence: `docs/ops/evidence/PRD5-B1W-pre-relay-model-permission-parity-local-20260710.md`
- External integration evidence: `docs/ops/evidence/PRD5-GATE-A-XINSIGHT-external-integration-20260710.md`
- Target rerun evidence: `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md`
- Workflow decision evidence: `docs/ops/evidence/AGENT-PROTOCOL-v2-single-owner-20260710.md`
- Gate A artifact remains in the UniRouter/X-Insight shared folder with SHA-256 `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`.

## Verification

Required before PR:

```bash
go test ./cmd/uniroute ./internal/gateway -count=1
go test -tags=integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -count=1 -v
go test ./... -count=1
git diff --check
```

Current local results on 2026-07-10:

- focused `cmd/uniroute` and `internal/gateway`: PASS;
- migrated-DB `TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration`: PASS;
- full `go test ./... -count=1`: PASS;
- `git diff --check`: PASS.

Required in GitHub before merge:

- `Backend Tests`
- `Frontend Build`
- `Docker Build`

PR #126 results:

- `Backend Tests`: PASS;
- `Frontend Build`: PASS;
- `Docker Build`: PASS;
- squash merge: `931400a`.

## Non-Goals

- No project model-governance semantic change.
- No route, migration, frontend, billing amount, provider or raw-payload change.
- No regeneration of the Batch 1V snapshot or reuse of its two-request authorization.
- No read-only X-Insight DB/view, shared ECS staging or exporter/outbox.
- No PRD4 reopen and no production claim.

## Target Execution

- User authorization `UR-XI-GATE-A-20260710-02` permitted deployment of the
  CI-passing target and exactly one synthetic denied-scenario rerun.
- Target source/runtime metadata is
  `e823abc293f996264763c6c1aca19d3d4d6e705f`, containing Batch 1W
  `931400a` / PR #126. Stable and canary readiness checks passed.
- Request `gatea-model-denied-b1w-283194e67a0c3ff9` returned HTTP 403 with a
  matching `X-Request-ID` and the expected `auth.model_not_allowed` trace.
- The request created zero call logs, billing freezes, transactions, and
  provider events. No paid upstream request was sent.
- Temporary API keys 36 and 37 are disabled.
- The second snapshot receipt is `persisted`: first persistence inserted two,
  rerun produced two duplicate/no-op with zero conflict, and readback returned
  two events/two gaps for exchange
  `uxchg_8deae233291214154815c336c9278983`.
- The first immutable Gate A snapshot remains externally verified: 10 accepted,
  first persistence 10 inserted, rerun 10 duplicate/no-op, and readback 10
  events/3 gaps.
- X-Insight target verification is separate from the receipt and is now Target
  verified for the fixture/synthetic-only non-production release artifact
  `b9e5811`; this is not a public API or production claim.
- Both temporary Gate A snapshot JSON artifacts are due for exact-path deletion
  by 2026-07-17. Receipts, hashes, sanitized evidence, and persisted normalized
  evidence remain retained under the accepted decision.
- UniRouter Batch 1Z completed the approved infrastructure-security remediation;
  remaining capacity and production-hardening work does not alter this result.

## Rollback

- Code rollback: revert the Batch 1W commit through a new PR; do not bypass branch protection.
- Deployment rollback: stable/canary rollback images are retained as
  `rollback-20260710151314`; no rollback was required.
- Data rollback: none; Batch 1W has no migration and does not mutate the immutable Gate A artifact.

## Optional Independent Review

Reviewers may verify this batch without becoming a serial gate. Write results under `docs/agent-workflows/reviews/` and include:

1. Diff review for the removed duplicate authorization path.
2. Targeted and migrated-DB integration test output.
3. Confirmation that no billing freeze or call log is written on denial.
4. PR/CI verification against the merged commit.
5. If target access exists, a newly authorized single-scenario rerun and evidence comparison.

P0/P1 findings block or reopen the batch. P2/P3 findings are triaged under the protocol.
