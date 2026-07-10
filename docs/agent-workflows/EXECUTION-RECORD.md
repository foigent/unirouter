# Current Execution Record

**Batch:** PRD5 Batch 1Z - Target Deployment And Infrastructure Security
**Owner:** Codex
**Risk:** R4 authorized ECS security-group cutover; R3 migration/deployment/canary/runtime/browser verification
**Status:** Target verified for the recorded Batch 1Z evidence
**Base:** final deployed `origin/main` at `95d1da9` / PR #133
**Previous batch:** PRD5 Batch 1Y implementation merged by `88a7354` / PR #131

## Scope

1. Merge and CI-verify Batch 1Y repository-owned functionality.
2. Back up the target, apply migration `000019`, deploy stable/canary/frontend, and run target browser checks.
3. Fix target Go-module build-network configurability and stable/canary runtime configuration parity through normal PR/CI flow.
4. Execute the authorized `INFRA-SEC-001` least-privilege security-group cutover with proven SSH, Cloud Assistant, application, dependency, external-denial, and rollback evidence.
5. Remove temporary QA users and credentials and synchronize current-state documents.

## Behavior Change

- Migration `000019` and the Data Exchange Control surface are active in the development/test target.
- Stable and canary run the final backend commit with aligned evidence/runtime/monitoring configuration.
- The target ECS no longer attaches the public all-TCP security group; public internal-service protocol access is denied.
- Structural Logs filters, Data Exchange, and redacted topology have target browser evidence. Route/failover detail remains CI verified.

## Files And Evidence

- Target evidence:
  `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`
- Local implementation evidence:
  `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`
- Migration/API: `migrations/000019_prd5_data_exchange_control.*.sql`,
  `internal/admin/data_exchange_handlers.go`, `cmd/uniroute/main.go`
- UI: `frontend/src/pages/DataExchangePage.tsx`,
  `frontend/src/pages/LogsPage.tsx`, `frontend/src/pages/InternalStatusPage.tsx`
- X-Insight request: shared-folder
  `requests/2026-07-10-unirouter-prd5-runtime-contract-required-information.md`
- Status/matrix/backlog/issue synchronization: `docs/ops/CURRENT-STATUS.md`,
  `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`, `docs/planning/PRD5-BACKLOG.md`,
  `docs/issues/OPEN-ISSUES.md`

## Verification

Repository and target results on 2026-07-10:

- `go test ./internal/admin -count=1`: PASS;
- `go test ./... -count=1`: PASS;
- `go vet ./...`: PASS;
- `npm --prefix frontend run type-check`: PASS;
- `npm --prefix frontend test -- --run`: PASS, with existing React Router,
  Semi Form, and `findDOMNode` warnings; 25 files / 124 tests;
- `npm --prefix frontend run build`: PASS, with existing large chunk warning;
- `git diff --check`: PASS;
- PR #131, PR #132, PR #133 Backend Tests / Frontend Build / Docker Build: PASS;
- pre-migration backup plus `pg_restore -l`: PASS;
- migration `000019` forward apply and table readback: PASS;
- stable/canary health and final canary runtime API readback: PASS;
- Data Exchange, Logs structural filters, redacted topology, desktop/mobile browser checks: PASS;
- `INFRA-SEC-001` cutover, external protocol-denial, SSH, Cloud Assistant, internal health/monitoring: PASS;
- temporary QA user/credential cleanup: PASS.

Migration down, TLS 443 behavior, and a concrete target route/failover detail
remain unexecuted. X-Insight runtime contracts remain external.

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
- X-Insight ECS target deployment/runtime verification is not part of the receipt
  and remains unverified.
- The first temporary snapshot deletion is due 2026-07-17. The second
  authorization did not specify retention for the new snapshot, so deletion is
  pending a retention decision.
- Current ECS security exposure and capacity findings are recorded in the target
  evidence. They require separate authorization and do not alter this result.

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
