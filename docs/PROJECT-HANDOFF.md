# UniRouter Agent Handoff

> Purpose: fast onboarding for Codex or any other executor/reviewer taking over this repository.
> Read this file first, then follow the linked current-state documents.
> Current Post-PRD6 release work is
> `codex/v1-commercial-license-upgrade`, based on clean `origin/main`
> `e94a4b184a6f675d76c292602b7c18293ab2ce61`. The User requires signed
> commercial License enforcement and a signed offline upgrade bundle before
> customer delivery. PR #205 is open. A follow-up P0 review found
> customer-controlled runtime replacement of both public trust roots. Exact
> code-bearing head `cd05055afe25cedff77830230d2788d8f06c96b3` now embeds
> separate License/upgrade public rings in formal release binaries, rejects
> runtime overrides and adds a
> double-click macOS License Studio for native-folder key generation, customer
> signing and verification. Focused tests, headed desktop/mobile browser
> verification and all four required checks in run `29359673125` pass. No
> persistent production/customer key or License exists, and no target was
> mutated. Merge,
> production key ceremony and ECS trust-root/License/migration/image activation
> are separate decisions. This does not reopen PRD6 or accept the PRD7 Update
> Center/automatic-upgrade product scope.
> PRD6 is formally `Completed (Accepted Scope)` at repository/CI evidence level. All 27
> accepted rows are verified or closed. P6-F11 closed through PR #189: final
> evidence head `3d4c61b` passed run `29246431182`, squash merge `4c78a5b7`
> passed post-merge main run `29246975012`. A separately authorized 2026-07-13
> ECS update deployed PRD6 stable/canary backend `ce9307a` and final frontend
> `f4fe41d`, verified selected authenticated UI and closed P6-E01 for one
> historical request. This is Target verified only, not a
> Production, capacity/HA, real-provider/detector, TLS or migration-down claim.
> Post-PRD6 V1 release hardening is separate: CI-efficiency PR #198 passed run
> `29299202652`, merged as `31d87de`, eliminated duplicate automatic main CI,
> and left 32 caches / `9,503,335,891` bytes after explicit single-ID cleanup.
> User-selected micro-CNY option B passed PR #202 run `29334221779`, merged as
> `ec0dde201841b8286f6802cc7e588e79eda40719`, and was activated by an
> authorized non-rolling ECS migration through `000036`. Environment-label PR
> #203 passed final run `29339373907`, merged as
> `545e514ab51937e6589512192141b9359cfcb504`, and is now the exact
> stable/canary/frontend development-target release. Database, API, runtime and
> authenticated browser readback passed; the UI displays `开发环境` with zero
> browser console errors/warnings. This is Target verified, not Production,
> real-provider/detector, TLS, capacity/HA or migration-down evidence.
> X-Insight v2 consumer acceptance and the remaining Post-PRD6 product/UI issue
> queue remain separate decision/work gates.

## First Read Order

1. `docs/ops/CURRENT-STATUS.md` - current verified project status.
2. `docs/planning/PRD6-START-HERE.md` - PRD6 decision state and reading map.
3. `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md` - clean-base, test, X-Insight and current-code evidence.
4. `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md` - source-backed 100x, Agent fan-out, billing, routing, streaming and multimodal audit.
5. `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md` - P6-F16 patch contract, red/green regression and local migrated-database evidence.
6. `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md` - confirmed follow-up finance/notification/pricing/settings/model-governance tenant findings.
7. `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md` - P6-F33 red/green and migrated-database local evidence.
8. `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md` - P6-F37 audit read-boundary red/green evidence.
9. `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md` - P6-F38 frontend-test required-CI evidence.
10. `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md` - current finance/request-log/usage tenant-isolation evidence.
11. `docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md` - current pricing-authority implementation and CI evidence.
12. `docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md` - merged global-control authority implementation, CI evidence and explicit target boundary.
13. `docs/ops/evidence/PRD6-B0B-P6F36-model-governance-tenant-isolation-local-20260711.md` - current model-governance tenant-isolation implementation, local verification and explicit unchanged-product boundaries.
14. `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md` - current server/client/idempotency/attempt implementation, verification and rollout boundary.
15. `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md` - CI-verified pool, wallet, grant, price-pin and single-terminal foundation.
16. `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md` - CI-verified pre-output marker, immutable usage, non-cancellation and restart recovery evidence.
17. `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md` - deterministic migrated admin integration and CI-gate evidence.
18. `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md` - CI-verified provider retry classification evidence.
19. `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md` - CI-verified Embeddings failover-parity evidence.
20. `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md` - CI-verified typed routing/budget-alert configuration truth.
21. `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md` - CI-verified multiplier reservation/settlement/cancel invariants and required-CI gate.
22. `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md` - typed stream transport, partial-failure settlement and current non-claims.
23. `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md` - explicit Agent registration, five-minute capability, refresh/revocation and topology evidence.
24. `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md` - shared per-ancestor cost/token/rate/concurrency implementation and P6-F19 closure evidence.
25. `docs/ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md` - versioned shared selector/rate/token/concurrency, control-plane, migration and CI closure evidence.
26. `docs/ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md` - CI-verified weighted leases, fair queues, fault/mixed-config, PR #170/#180/#181 truth chain and current non-claims.
27. `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md` - CI-verified deterministic weighted routing, bounded snapshots, renewable probe ownership, epoch CAS, preview/evidence, guarded migration, PR #172 and post-merge main evidence.
28. `docs/ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md` - CI-verified provider Prompt Cache normalization, immutable pricing, settlement/recovery, UI/reconciliation, guarded migration, PR #174/#175 and final main evidence.
29. `docs/ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md` - CI-verified durable finalization/outbox closure, PR #177/main runs and non-claims.
30. `docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md` - official competitor HA patterns, current cluster facts and accepted D6-12 boundary.
31. `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md` - source-backed UI, reconciliation, correlation, architecture and upgrade audit.
32. `docs/planning/PRD7-CANDIDATE-BACKLOG.md` - future productization candidates; the separately approved Post-PRD6 signed-bundle foundation does not accept Update Center, automatic apply/rollback or PRD7 scope.
33. `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md` - accepted future independent Cache Service target, planning bands and SaaS prerequisites; implementation deferred.
34. `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md` - version-fixed competitor audit and Four Quadrants.
35. `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md` - third-round boundary, CARTA/shadow/collusion analysis and Killer Features.
36. `docs/planning/prd6-adaptive-trust-hardening/hardening.md` - adaptive-trust structural option portfolio and recommendation.
37. `docs/PRD-Phase6.md` - accepted MVP, proposed innovation and ownership.
38. `docs/ops/PRD6-ACCEPTANCE-MATRIX.md` - accepted, proposed, external and target-evidence rows.
39. `docs/planning/PRD6-IMPLEMENTATION-PLAN.md` - batch order and gates.
40. `docs/planning/PRD6-BACKLOG.md` - classified backlog and decision register.
41. `docs/PRD-Phase5.md` - completed accepted PRD5 scope and explicit remaining boundaries.
42. `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` - PRD5's closed 36-row evidence ledger.
43. `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md` - accepted X-Insight v1 boundary and PRD5 completion classification.
44. `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md` - final PRD5 target evidence.
45. `/Users/m4/Desktop/projects/X-Insight/docs/architecture/runtime-contract-capability-status.md` - X-Insight implemented/not-implemented runtime facts.
46. `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-11-prd5-runtime-contract-v1-boundary.md` - accepted cross-project decision.
47. `docs/issues/OPEN-ISSUES.md` - active issues and future evidence.
48. `docs/agent-workflows/AGENT-PROTOCOL.md` - execution, approval, evidence and optional-review rules.
49. `docs/agent-workflows/EXECUTION-RECORD.md` - current branch/batch verification.
50. `docs/CODEBASE-MAP.md`, `docs/DOC-INVENTORY.md`, and `docs/DOCS-GOVERNANCE.md` - code and documentation lifecycle maps.
51. `docs/ops/evidence/PRD6-B3H-P6F40A-cost-shortfall-local-20260713.md` - current COST-009 shortfall/top-up/pending implementation and verification boundary.
52. `docs/ops/evidence/PRD6-B3H-P6F40B-provider-attempt-ledger-local-20260713.md` - metadata-only provider-attempt state, financial retry/cancel authority, migration safety and local verification boundary.
53. `docs/planning/PRD6-UPSTREAM-COST-RECONCILIATION-AND-PROVIDER-COVERAGE-20260713.md` - accepted Customer Revenue Ledger / Provider COGS boundary, competitor/provider coverage research, Four Quadrants and future capability queue.
54. `docs/ops/evidence/PRD6-B3H-P6F40C-provider-key-reconciliation-local-20260713.md` - affected-key paid-admission block, platform-only amount-free reconciliation, concurrency/crash-gap/migration proof and current local/CI boundary.
55. `docs/ops/evidence/PRD6-B3H-P6F41-usage-delivery-integrity-local-20260713.md` - typed explicit-zero/missing usage, funded estimates, local-write charge gate, interrupted-stream delivered counter, metadata-only receipt and current local/CI boundary.
56. `docs/ops/evidence/PRD6-B4-P6F10-guardrail-kernel-local-20260713.md` - D6-15 immutable policy, bounded detector/payload, output-cost hold, migration and PR #188 exact-head CI closure evidence.
57. `docs/ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md` - D6-16 micro-buffer, termination, reservation extension, checkpoint/recovery and PR #189 CI closure evidence.
58. `docs/ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md` - authoritative 27/27 accepted-row closeout, evidence level and preserved future boundaries.
59. `docs/ops/evidence/PRD6-TARGET-DEPLOYMENT-AND-UI-VERIFICATION-20260713.md` - authorized PRD6 ECS migration/deployment, stable/canary runtime, authenticated UI, P6-E01 historical request readback, cleanup and remaining target non-claims.
60. `docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md` - fresh Git/CI/matrix/target-health audit, formal phase-completion decision, corrected 27/27 accounting and preserved later boundaries.
61. `docs/ops/evidence/V1-POST-PRD6-CI-EFFICIENCY-LOCAL-20260714.md` - CI duplication, change-routing, concurrency and cache baseline evidence.
62. `docs/ops/evidence/V1-POST-PRD6-CI-EFFICIENCY-CI-20260714.md` - PR #198, main dispatch and exact cache-seed/cleanup readback.
63. `docs/planning/V1-MICRO-PRECISION-LEDGER-PLAN-20260714.md` - accepted P0 micro-CNY ledger contract, migration boundary and approval gate.
64. `docs/ops/evidence/V1-POST-PRD6-FINANCIAL-PREREQUISITES-CI-20260714.md` - wallet atomicity and public pricing-unit prerequisite merge/CI evidence.
65. `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-LOCAL-20260714.md` - option B implementation, full local regression, isolated migration/API/browser evidence, issue classification and R3 cutover checklist.
66. `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-TARGET-20260714.md` - PR #202/#203 CI/merge truth, authorized non-rolling ECS activation, database/API/runtime/browser readback, environment-label verification and remaining non-claims.
67. `docs/planning/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-CONTRACT-20260714.md` - User-approved V1 commercial authority and signed offline bundle release contract, recovery surface, acceptance and target-activation boundary.
68. `docs/ops/evidence/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-LOCAL-20260715.md` - complete issue matrix, local implementation/test evidence, CI gate and R4 ECS non-claims.
69. `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-LOCAL-20260715.md` - P0 trust-root bypass closure, click-only macOS tool, security boundary and local browser/test/build evidence.
70. `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-CI-20260715.md` - exact code-head required-CI and formal release Docker-image closure evidence.
71. `docs/tech-ref/16-license-foundation.md` - enforceable License schema, runtime admission, resource limits, recovery and historical PRD2 compatibility.
72. `docs/tech-ref/27-signed-offline-upgrade.md` - bundle format, trust ordering, provenance binding, verify/record workflow and explicit Update Center/TUF non-claims.

## Current Project Shape

UniRouter is a local-deployable enterprise AI gateway/MaaS platform. PRD1 delivered the core gateway, admin API, admin frontend, billing, logs, wallet, pricing, admission controls, health checks, and monitoring foundation. PRD2 is frozen for new feature work after accepted product expansions such as project/RBAC, model governance, notification events, license foundation, OIDC, usage analytics, internal status, and error docs.

PRD3 is complete for the confirmed scope. It was cleanup/optimization only: consistency, semantics, route/API contract, documentation lifecycle, module boundaries, frontend architecture, parity tests, and engineering governance.

PRD4 is complete for the confirmed commercial observability and control foundation scope. Its formal scope is `docs/PRD-Phase4.md`: API key owner usage/cost, quota vocabulary, Flow/Event/Trace foundation, PlungeIn observation-first ports, Status Center runtime observability, external monitoring integration, real upstream/SSO harness assets only, auth UX hardening, production evidence promotion for currently available evidence, and PRD5 backlog consolidation. User decision on 2026-07-08 moved real upstream target E2E and real SSO target E2E out of current PRD4 because no approved real environments are available. `docs/planning/PRD4-BACKLOG.md` remains the candidate-source record, not the formal implementation contract.

PRD5 is `Accepted Scope Complete`. Its 36 rows have completion evidence or an
explicit defer/reject/prerequisite decision. Its X-Insight Gate A, structural
evidence, trace UI, Data Exchange Control, route/failover visibility and
redacted topology history remains under the existing PRD5 paths. PRD6 must not
reinterpret target-evidence debt or X-Insight-owned work as unfinished PRD5
source scope.

PRD6 is formally `Completed (Accepted Scope)` with D6-01/D6-02/D6-08/D6-09/D6-10/D6-11/D6-12/D6-13/D6-14/D6-15/D6-16 accepted. D6-13 adds P6-F40/P6-F41 without rewriting the CI-verified P6-F18/P6-F20 history. D6-14 requires Customer Revenue Ledger authority to remain separate from Provider COGS evidence: collection uses authoritative usage, local delivery and frozen customer price; aggregate provider bills, balance deltas and operator-entered amounts cannot create customer debt. D6-15 fixes the Guardrail Kernel and content-size boundary: immutable stacked policy decisions remain UniRouter-owned; actual 8/16/16 MiB bounded reads/final responses, registered bounded detectors and explicit output-cost holds do not enable raw persistence or a real external detector. D6-16 closes enforced streaming through PR #189 / merge `4c78a5b7` / main run `29246975012`. Provider-native runtime/conformance packs, live billing connectors, controlled bill import, generalized units, async media, managed billing and Cost Control Service extraction remain future candidates. All 27 accepted rows are repository/CI verified or closed. Batch 0A is merged/CI verified. The second
audit adds open P0 tenant,
operation/idempotency, reservation/terminal-state and revocation findings, plus
P1 Agent/100x blind spots. The accepted MVP covers those integrity
prerequisites, Agent capability/distributed policy, Routing Policy V1, durable
cache-aware billing, and guardrail/stream foundations. Batch 0B begins with the
P6-F16 tenant-integrity security boundary, which is CI verified and merged by
PR #140 as `f244f669`. D6-08 separately accepts the later P6-F32-P6-F36
finance, notification, pricing, global-control and model-governance inputs;
P6-F33 is CI verified by PR #142 / `7173c59`; P6-F32 and adjacent P6-F39 are
merged and CI verified by PR #145 / `609385f`. P6-F34 was squash-merged by PR
#146 as `517ce45381429781f7e81e492bdd9f38093c21ba`; final-head run
`29137670055` and post-merge main run `29137781625` passed all three required
jobs, and its historical branch is retained. P6-F35 PR #147 final head
`fb77083d0f04f5cdaa20f38e763c93b51b89f4d1` passed all required jobs in run
`29139408226`, was squash-merged as
`c08db96167837ade685708c2ab204861a8ecc0ca`, and post-merge main run
`29139534691` passed the same jobs; its historical branch is restored and
retained. P6-F36 PR #148 final required run `29140739477` passed, was
squash-merged as `d83dbbc0ac7137bb44d2346adc5a25debde804e1`, and post-merge
main run `29140850345` passed. P6-F17 Scheme A PR #149 passed required run
`29152968280`, was squash-merged as `c9402fa`, and post-merge main run
`29153106336` passed. P6-F18A PR #151 and F18B PR #153 passed required CI;
F18B was squash-merged as `b30bc9d` and post-merge main run `29156096777`
passed. P6-F18 is CI verified / closed for pool-safe, single-terminal and
billable-stream recovery behavior; P6-F20 was closed separately by PR #177.
These changes add no response cache and make no target or production claim.
P6-F37 audit-log tenant isolation and P6-F38 frontend test
enforcement are merged and CI verified by PR #143 / `a1e0ad6` under the safe
remediation rule. P6-TEST-DB-001 and P6-F03/P6-F04/P6-F05 are CI verified /
closed by PRs #155/#156/#157/#158 and post-merge main CI. For P6-F05,
`effective-config.v1` exposes the driving routing configuration and
one project-scoped budget-alert contract, while the broken global first-wallet
path is removed and its historical keys are marked retired/ineffective. The
P6-F06 multiplier-specific PostgreSQL matrix is CI verified / closed by PR #160,
merge `15ea8a2` and post-merge main run `29161527988`;
it proves insufficient rejection without residue, one exact terminal receipt
with zero uncovered cost under 32 callers, and full idempotent cancel/refund.
P6-STREAM-002A is CI verified / closed by PR #162, merge `7737f8b` and
post-merge main run `29168586124` for typed frame and timeout policy, serialized
write/flush, single body ownership and partial-stream settlement. Its historical
branch is restored. It
does not close D6-10 dynamic reservation extension or P6-F11 semantic policy.
P6-F19 is CI verified / closed by the P6-F19A capability kernel and P6-F19B
ancestor-runtime layers: PR #164/main run `29170507588` prove lifecycle,
topology and revocation; PR #166, merge `b491817` and main run `29171807693`
prove atomic shared cost/token/rate/concurrency enforcement and real Redis 7/
PostgreSQL readback. P6-F07 is independently CI verified / closed by PR #168,
merge `2610e317` and main run `29174001859` for versioned selectors, shared
RPM/RPS/TPM/concurrency, bounded failure behavior, policy evidence, control UI
and safe migration rollback. P6-F21 weighted admission is CI verified / closed
by PR #170 exact-head run `29176160987`, squash merge `04af91f` and post-merge
main run `29176358459`, including two/three-replica exact capacity, fair bounded
queues, crash/fault and mixed-config behavior. Its historical branch is
restored at `a9d8f3e`. Post-closeout test-only PR #180 removed the unreliable
miniredis replica/load claim; PR #181 isolated emulator execution budgets,
passed run `29199377213`, merged as `815fb31`, and final main run `29199635598`
passed all required jobs. No runtime behavior changed. P6-F20 durable terminal
coupling and D6-10 dynamic long-stream extension remain separate.
P6-F08 Routing Policy V1 is CI verified / closed by PR #172. Exact head
`0e374d3` passed run `29178419505`, squash merge `54d8833` and post-merge main
run `29178588075` passed Backend Tests, Frontend Build and Docker Build.
Deterministic server-ID weighting, bounded singleflight snapshots, renewable
PostgreSQL probe ownership, routing/circuit/key epoch CAS, per-channel
thresholds, shared Chat/Embeddings plans, safe preview/trace/UI and guarded
migration are covered. Its historical branch is retained at the exact head;
no target or capacity claim exists.
P6-F09 provider Prompt Cache accounting is CI verified / closed. Exact
implementation head `711e718` passed PR #174 run `29180539621` and was
squash-merged as `6515b3f`. The first post-merge main run exposed an existing
P6-F21 miniredis timeout; production-neutral fixture-stability PR #175 passed
run `29180946857`, merged as `257a9eb`, and final post-fix main run
`29181137210` passed Backend Tests, Frontend Build and Docker Build. The
historical implementation branch is retained at its exact head. This is not
response caching or the future independent Cache Service and makes no target
or production claim. P6-F20 closed Batch 3 through PR #177 exact-head run
`29193900584`, squash merge `30d1e8f` and post-merge main run `29194150134`.
D6-12 assigns exact mid-stream process-crash usage/checkpoint and
client-recovery semantics to P6-F11; P6-F20 closes at final-usage/terminal
durability.
The adaptive-trust, Digital Twin,
shadow, collusion, trust-receipt and claim-assurance rows remain proposed and
are not implied by D6-01.

The User accepted the complete exact-response cache as a future required,
independently deployable `D7-CACHE-001` Cache Service reusable by private and
SaaS regional-cell profiles. This is a PRD7 architecture/backlog decision only:
no PRD6 response persistence, deployment or semantic cache is accepted.

## What Is Current Truth

| Question | Current Source |
|----------|----------------|
| What is the PRD6 decision state? | `docs/planning/PRD6-START-HERE.md` |
| What baseline and findings were verified? | `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md` |
| What did the 100x/Agent/streaming anti-pattern audit find? | `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md` |
| What exactly does the local P6-F16 patch prove? | `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md` |
| Which administrator tenant surfaces remain confirmed gaps? | `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md` |
| What does the P6-F33 local patch prove? | `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md` |
| What does the P6-F37 audit-log patch prove? | `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md` |
| How are frontend behavior tests enforced in required CI? | `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md` |
| What do the current P6-F32/P6-F39 finance and usage patches prove? | `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md` |
| What does the current P6-F34 pricing-authority patch prove? | `docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md` |
| What does the current P6-F35 global-control patch prove, and what remains unverified? | `docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md` |
| What does the current P6-F36 model-governance patch prove, and which product rules remain unchanged? | `docs/ops/evidence/PRD6-B0B-P6F36-model-governance-tenant-isolation-local-20260711.md` |
| What does P6-F17 prove about server/client identity, replay and failover attempts? | `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md` |
| What does P6-F18 prove about wallet invariants and post-stream recovery? | `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md`, `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md` |
| What proves the current admin integration gate is deterministic? | `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md` |
| What do P6-F03/P6-F04 prove about retry and endpoint parity? | `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md`, `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md` |
| What does P6-F05 prove about effective routing and budget-alert truth? | `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md` |
| What does P6-F06 prove about multiplier-aware reservation and settlement? | `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md` |
| What does the current stream-transport slice prove and explicitly not prove? | `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md` |
| What proves the Agent-capability lifecycle and shared ancestor runtime limits, and what remains separate? | `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md`, `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md` |
| What proves shared RPM/RPS/TPM/concurrency policy, bounded failure behavior and safe control/migration semantics? | `docs/ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md` |
| What locally proves weighted cross-replica admission, fairness, crash recovery and bounded Redis failure behavior? | `docs/ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md` |
| What locally proves Routing Policy V1 deterministic weight, snapshots, probe ownership, epoch safety and operator preview? | `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md` |
| What locally proves provider Prompt Cache token normalization, immutable pricing, settlement/recovery and UI/reconciliation semantics? | `docs/ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md` |
| What does the P6-F20 CI-closed repository evidence prove and not prove? | `docs/ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md` |
| What locally proves P6-F41 explicit-zero/missing usage, delivery-gated charging and interrupted-stream separation? | `docs/ops/evidence/PRD6-B3H-P6F41-usage-delivery-integrity-local-20260713.md` |
| What proves P6-F10 Guardrail policy, bounded payload/detector, no-delivery cost-hold and migration behavior? | `docs/ops/evidence/PRD6-B4-P6F10-guardrail-kernel-local-20260713.md` |
| What proves P6-F11 bounded stream disclosure, termination, extension and process-replacement recovery? | `docs/ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md` |
| What proves PRD6 Accepted Scope Complete and what remains later? | `docs/ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md` |
| What proves the fresh audit and formal PRD6 phase-completion decision? | `docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md` |
| How do competitors and UniRouter separate HA from stream resume? | `docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md` |
| What did the UI/reconciliation/correlation/architecture audit conclude? | `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md` |
| Which items are candidates for PRD7 rather than accepted scope? | `docs/planning/PRD7-CANDIDATE-BACKLOG.md` |
| What is the accepted future Cache Service target and when should it split? | `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md` |
| How are customer revenue, provider COGS, competitor practice and supplier coverage separated? | `docs/planning/PRD6-UPSTREAM-COST-RECONCILIATION-AND-PROVIDER-COVERAGE-20260713.md` |
| What is the closed accepted PRD6 scope? | `docs/PRD-Phase6.md` |
| Which PRD6 rows are accepted versus proposed? | `docs/ops/PRD6-ACCEPTANCE-MATRIX.md` |
| What is the PRD6 implementation order? | `docs/planning/PRD6-IMPLEMENTATION-PLAN.md` |
| What did the competitor/Four-Quadrant audit conclude? | `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md` |
| What did the adaptive-trust/Four-Quadrant innovation audit conclude? | `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md` |
| What structural adaptive-trust options were compared? | `docs/planning/prd6-adaptive-trust-hardening/hardening.md` |
| What is the completed PRD5 formal scope? | `docs/PRD-Phase5.md` |
| What PRD5 work was classified at closeout? | `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md` |
| What did the repository-wide PRD5 audit and P5-F12 screening conclude? | `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md` |
| Which accepted UniRouter-owned PRD5 functions are target/CI verified? | `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md` |
| What is the PRD5 acceptance plan? | `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` |
| What is the PRD5 implementation order? | `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` |
| What competitor gaps feed PRD5? | `docs/planning/PRD5-COMPETITOR-COMPARISON.md` |
| What is the X-Insight data exchange UI/control boundary? | `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md` |
| How are newly generated Gate A scenarios authorized? | `docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md` |
| What is the PRD4 formal scope? | `docs/PRD-Phase4.md` |
| What is the PRD4 acceptance plan? | `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` |
| Which items are explicitly deferred to PRD5? | `docs/planning/PRD5-BACKLOG.md` |
| What is actually verified now? | `docs/ops/CURRENT-STATUS.md` |
| Which PRD3 items are accepted? | `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` |
| Which PRD2 items are frozen/accepted/deferred? | `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
| Which capabilities were PRD4 candidates before formal scoping? | `docs/planning/PRD4-BACKLOG.md` |
| What should be fixed next? | `docs/issues/OPEN-ISSUES.md` |
| How does execution, approval and optional review work? | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Which old docs may be stale? | `docs/DOC-INVENTORY.md` |

PRD1 and PRD2 documents remain useful requirements/evidence references, but they are not the active planning surface for new work. Do not treat files under `docs/testing/archive/` as current status. They are historical evidence unless reconciled into the current-state files above.

## Risk-Gated Execution Workflow

Use `docs/agent-workflows/AGENT-PROTOCOL.md` before changing code or status:

1. Codex is the single execution owner for planning, implementation, tests, repository integration and authorized environment work.
2. R2/R3/R4 batches update `docs/agent-workflows/EXECUTION-RECORD.md` and applicable immutable evidence.
3. Codex creates a `codex/` branch and PR; required GitHub CI must pass before merge. Direct push to `main` is prohibited.
4. Authorized non-production deployment may proceed without a separate Atlas gate after secure access exists.
5. Production, destructive target data, credential, expanded export, excess-cost provider and public-release actions require specific User approval.
6. Canary, Sentinel, Atlas or other capable reviewers may independently review without becoming a default serial gate.
7. Independent results go under `docs/agent-workflows/reviews/`; P0/P1 findings block or reopen a batch.
8. Local, CI, target, production and external-integration evidence remain separate status levels.

## Current Verification Baseline

Current execution has completed PRD6 Batch 0B P6-F16 through PR #140 /
`f244f669`; its post-merge `main` CI is green. Fresh full repository checks and
X-Insight tests passed before implementation; Batch 0A is merged and CI
verified. D6-01/D6-02/D6-08/D6-09/D6-10/D6-11/D6-12/D6-13/D6-14/D6-15/D6-16 are accepted; P6-F40 is CI verified / closed across PR #183/#184/#186, with P6-F40C exact implementation/test head `49d740d` / run `29224762127`; P6-F41 is CI verified / closed by PR #187 exact implementation/test head `6d0e6ba` / run `29229320432`; P6-F10 is CI verified / closed by PR #188 exact implementation/test head `76f95ed` / run `29236987646`; P6-F11 is CI verified / closed by PR #189 / merge `4c78a5b7` / main run `29246975012`, while P6-F16 and P6-F33 are CI verified
and merged. P6-F37/P6-F38 are merged and CI verified by PR #143 / `a1e0ad6`,
P6-F32/P6-F39 are merged and CI verified by PR #145 / `609385f`; P6-F34 was
merged by PR #146 as `517ce45381429781f7e81e492bdd9f38093c21ba` after final-head run `29137670055`, and
post-merge main run `29137781625` also passed all required jobs. P6-F35 PR #147
final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1` passed final required CI in
run `29139408226`, was squash-merged as `c08db96167837ade685708c2ab204861a8ecc0ca`,
and post-merge main run `29139534691` passed all required jobs; its historical
branch is restored and retained. P6-F36 PR #148 final required run
`29140739477`, squash merge `d83dbbc` and post-merge main run `29140850345`
passed. P6-F17 full local/race/migrated/frontend verification, PR #149 required
CI, merge and post-merge main CI passed. P6-F18A/F18B full local/race/fault/
migration verification, PR #151/#153 required CI and post-merge main CI passed;
P6-F18 and P6-F20 are independently CI verified / closed. PRD7/target evidence are explicitly later;
historical branches remain retained. No new item is target or production
verified. Any
later P6-F35 target cutover must first drain old control-plane binaries and use
a non-rolling activation; new-version replicas converge global-word changes
through the existing approximately one-minute reload path.
No global-settings UI was added. Typed configuration ownership and the
budget/alert key, scope and audit truth chain are CI verified / closed by P6-F05.
P6-F17 adds
separate operation/client identity and tenant-scoped Audit readback but no
generic mutation replay or durable audit claim.
P6-F07 exact-head run `29173848811`, squash merge `2610e317` and post-merge
main run `29174001859` passed all required jobs, including real Redis/
PostgreSQL policy and migration rollback gates. Its historical branch is
restored at `f52a151`; no target capacity or deployment claim is made.
P6-F21 full local race and two/three-replica real Redis tests pass. PR #170
exact head `a9d8f3e` passed run `29176160987`, was squash-merged as `04af91f`,
and post-merge main run `29176358459` passed all required jobs. The row and
Batch 1 are CI verified / closed. Test-only PR #180/#181 later corrected the
miniredis evidence boundary; PR #181 merge `815fb31` and final main run
`29199635598` passed all required jobs. Local Docker remains unavailable, and
no target/deployment or target-capacity claim is made.
P6-F36 does not reopen P6-F16, add a migration/RLS/foreign-key redesign, change
unregistered-model compatibility or `/v1/models`, or add a selector UI/target
cleanup.
Current details and promotion gates are in
`docs/agent-workflows/EXECUTION-RECORD.md`.

PRD5 Batch 1Z Target Deployment And Infrastructure Security remains Target
verified for its explicit evidence. The previous Batch 1W denied path remains target
verified under `UR-XI-GATE-A-20260710-02` on target
`e823abc293f996264763c6c1aca19d3d4d6e705f`, containing `931400a` / PR
#126; both are reconciled in `docs/agent-workflows/EXECUTION-RECORD.md`.
Batch 1V Atlas execution produced an immutable sanitized snapshot with a known
denied-scenario correlation/source-trace gap. X-Insight persistent import/readback
is External integration verified. The Batch 1W denied follow-up and key disable
are complete, and its second snapshot passed X-Insight persistence/idempotency/
readback. X-Insight's fixture/synthetic-only non-production target is Target
verified at release artifact `b9e5811`; real upstream E2E and unimplemented
durable runtime contracts remain separate.

Batch 1X and Batch 1Y are integrated through PR #131. Batch 1Z records final
target commit `95d1da9`, migration `000019` forward apply, stable/canary health,
target Data Exchange/Logs/topology browser checks, canary runtime parity, and the
least-privilege ECS cutover. Evidence is
`docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`.
P5-F35 route/failover visibility remains CI verified pending a concrete target
request-detail check; migration down is also unexecuted.
X-Insight-required information is consolidated under the shared-folder request
`requests/2026-07-10-unirouter-prd5-runtime-contract-required-information.md`.

PRD4 is complete for the confirmed current scope. PRD5 must preserve the PRD4
deferred-boundary decisions recorded here.

PRD5 Batch 1A/1B/1C:

- Scope: planning closure, first snapshot artifact, and nullable structural
  evidence fields plus lightweight trace UI exposure.
- Status: Canary PASS 17/17 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `e1e76e3` / PR #106.
- Adds: PRD5 formal scope, PRD5 matrix, implementation plan, competitor
  comparison, sanitized snapshot runbook, structural evidence field contract,
  `scripts/prd5_xinsight_snapshot.go`, a shared-folder snapshot contract,
  migration `000018`, gateway trace/log structural writes, and existing trace
  API read-side structural fields, and Logs detail evidence quality / structural
  evidence tags.
- Does not add: new API routes, deployment changes, read-only credentials,
  exporter/outbox, trusted inbound correlation headers, raw prompt/response
  capture, X-Insight timeline/graph UI, or PRD4 matrix changes.

PRD5 Batch 1D:

- Scope: provider-returned upstream request ID correlation for gateway evidence.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `bf435ca` / PR #107.
- Adds: bounded response-header allowlist extraction, sanitized
  `upstream_request_id` persistence on provider trace events and call logs, and
  focused gateway tests.
- Does not add: inbound public-header trust, migration, new route, frontend
  behavior, exporter/outbox, read-only credential, raw payload capture, or PRD4
  matrix changes.

PRD5 Batch 1E:

- Scope: event-level capture-gap emission for trace metadata safety.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `a19830f` / PR #108.
- Adds: `capture_gap='redacted'` when UniRouter drops restricted metadata,
  rejects unsupported metadata shapes, or truncates allowed metadata strings;
  focused `traceutil` and gateway tests.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw payload capture, fabricated session/delegation
  facts, or PRD4 matrix changes.

PRD5 Batch 1F:

- Scope: restricted payload capture mode configuration.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `e9670ad` / PR #109.
- Adds: `PAYLOAD_CAPTURE_MODE` defaulting to `metadata_only`; active support for
  `metadata_only` and `disabled` only; config validation that rejects unsupported
  payload modes; gateway trace/log writes that persist the configured
  `payload_capture_mode`; source-event hashes that include the configured mode;
  `.env.example`, `docker-compose.yml`, and deployment-runbook docs.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw prompt/response capture, encrypted payload object
  store, redacted/hash/reference active modes, fabricated session/delegation
  facts, or PRD4 matrix changes.

PRD5 Batch 1G:

- Scope: default-off trusted evidence context intake for internal
  trace/session/actor/delegation propagation.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `c701843` / PR #110.
- Adds: `EVIDENCE_CONTEXT_TRUST_SECRET`, `X-UniRouter-Evidence-Auth` trust gate,
  bounded `X-UniRouter-*` evidence header parsing, trusted `Traceparent`
  fallback, trace/log structural writes for trusted `trace_id`,
  `parent_span_id`, `session_id`, `actor_*`, and `on_behalf_of`, plus
  source-event hashes that include those fields.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, public inbound header trust by default, raw payload
  capture, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1H:

- Scope: gateway span hierarchy for UniRouter-emitted request trace events.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `61f5610` / PR #111.
- Adds: per-request in-memory span state, gateway root span assignment on the
  first UniRouter trace event, trusted inbound parent preservation for the root
  span, later UniRouter trace events parented to the gateway root span, and
  source-event hashes that include the resulting `parent_span_id`.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, public inbound header trust by default, raw payload
  capture, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1I:

- Scope: explicit payload capture policy decision evidence for chat and
  embeddings.
- Status: Canary PASS 11/11 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `1dc20b5` / PR #112.
- Adds: `payload.policy.applied` trace events after gateway request acceptance;
  sanitized metadata for `payload_capture_mode`, `payload_policy_reason`,
  `raw_payload_stored=false`, and `payload_ref_present=false`; and
  `decision_kind=payload_capture` with `allowed` for metadata-only mode or
  `skipped` for disabled mode.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw prompt/response capture, payload hashing, payload
  object references, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1J:

- Scope: provider upstream request ID capture-gap evidence.
- Status: Canary/Sentinel complete; Sentinel merge reflected by `f5223df` /
  PR #113.
- Adds: `capture_gap=provider_not_returned` on
  `provider.response_accepted` and `provider.response_rejected` trace events
  when UniRouter has no sanitized provider-returned `upstream_request_id`.
  Existing returned upstream IDs and redaction gaps are preserved.
- Does not add: migration, new route, frontend behavior, exporter/outbox,
  read-only credential, raw prompt/response capture, X-Insight runtime
  dependency, public inbound-header trust, provider request/response semantic
  changes, or PRD4 matrix changes.

PRD5 Batch 1K:

- Scope: Gate A snapshot Markdown handoff-note support for X-Insight discovery.
- Status: Canary/Sentinel complete; Sentinel merge reflected by `2db830a` /
  PR #114.
- Adds: optional `--handoff-note <path>` support in
  `scripts/prd5_xinsight_snapshot.go`; a generated DSN-free Markdown note with
  generated date, source instance, schema version, snapshot output path,
  selected request IDs, scenario labels, counts, safety confirmation, and
  command shape without DSN; and a shared-folder contract for X-Insight.
- Does not add: real snapshot generation without approved request IDs and a
  read-only DSN, read-only DB/view credentials, shared ECS staging,
  exporter/outbox, migration, API route, frontend behavior, raw prompt/response
  capture, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1L:

- Scope: request-local component span refinement for UniRouter trace events.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `ffebf83` / PR #115.
- Adds: per-request component span tracking in `internal/gateway/trace.go` so
  first component events parent to the gateway root and repeated same-component
  events parent to that component's first span; focused gateway coverage in
  `internal/gateway/relay_mock_test.go`; shared X-Insight contract for the
  request-local hierarchy boundary.
- Does not add: migration, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, raw prompt/response capture,
  X-Insight runtime dependency, cross-system agent/tool/business graph
  inference, or PRD4 matrix changes.

PRD5 Batch 1M:

- Scope: provider failure capture-gap evidence.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `4a4e254` / PR #116.
- Adds: `capture_gap=not_available_from_source` for provider-stage failure
  events that occur before an upstream provider response source exists; preserves
  `provider_not_returned` for provider response events that lack a returned
  upstream request ID; preserves `redacted` priority for sanitized metadata
  gaps; passes available embeddings upstream request IDs into
  `provider.response_parse_failed`; and shares the gap contract with X-Insight.
- Does not add: migration, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, raw prompt/response capture,
  X-Insight runtime dependency, provider dispatch semantic change, or PRD4
  matrix changes.

PRD5 Batch 1N:

- Scope: snapshot capture-gap candidate categories for Gate A handoff artifacts.
- Status: Canary PASS 10/10 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `92cb8ce` / PR #117.
- Adds: optional `candidate_capture_gaps` in generated snapshot `capture_gaps`;
  candidates `sampled_out`, `retention_expired`, and `not_instrumented` when a
  call log exists but no trace rows are exported; and a capture-gap table in the
  Markdown handoff note.
- Does not add: migration, runtime request-path change, API route, frontend
  behavior, read-only DB/view credential, shared ECS staging, exporter/outbox,
  raw prompt/response capture, X-Insight runtime dependency, definitive
  sampling/retention cause assertion, or PRD4 matrix changes.

PRD5 Batch 1O:

- Scope: status reconciliation and Gate A controlled sanitized snapshot handoff
  preparation.
- Status: Canary PASS 8/8 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `b2eea2e` / PR #118.
- Adds: PRD5 status reconciliation for Batch 1N completion, P5-F24 acceptance
  wording, and a shared-folder request for approved request IDs, scenario
  labels, source instance naming, and X-Insight import/read confirmation.
- Does not add: a real snapshot artifact, DSN, credential, raw prompt/response,
  raw IP, raw upstream error, migration, runtime request-path change, API route,
  frontend behavior, read-only DB/view credential, shared ECS staging,
  exporter/outbox, X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1P:

- Scope: runtime model-permission decision evidence for the existing chat
  `allowed_models` rejection path.
- Status: Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `61091c` / PR #119.
- Adds: `auth.model_not_allowed` trace evidence before the existing 403 response
  with `source_component=auth`, `capture_gap=permission_denied`,
  `decision_kind=auth`, `decision_result=rejected`, and
  `decision_reason=auth_error.model_not_allowed`; focused gateway coverage for
  the direct trace writer and handler denial path; and shared-folder contract
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/2026-07-10-unirouter-prd5-model-permission-decision-evidence.md`.
- Does not add: migration, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, raw prompt/response capture,
  X-Insight runtime dependency, Embeddings authorization behavior change,
  billing/routing/provider dispatch semantic change, or PRD4 matrix changes.

PRD5 Batch 1Q:

- Scope: Gate A request approval manifest for X-Insight's scenario data
  requirement.
- Status: Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `ed8c69d` / PR #120.
- Adds: `--approval-manifest <path>` to
  `scripts/prd5_xinsight_snapshot.go`; manifest schema
  `unirouter.prd5.xinsight.approval.v1`; `approved` / `pending` /
  `unavailable` row handling; rejection for placeholder approved IDs; and a
  UniRouter response to X-Insight at
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-10-unirouter-approved-request-ids-response.md`.
- Does not add: a real snapshot artifact, approved request IDs, DSN,
  credential, raw prompt/response, raw IP, raw upstream error, migration,
  runtime request-path change, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, X-Insight runtime dependency,
  or PRD4 matrix changes.

PRD5 Batch 1R:

- Scope: human-readable Gate A request ID approval runbook and shared approval
  worksheet.
- Status: Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `c15cd7d` / PR #121.
- Adds: `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md`; shared worksheet
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`;
  plain-language approval meaning, `approved` / `pending` / `unavailable`
  definitions, tradeoffs versus auto-discovery/read-only DB/exporter, and an
  example approval sentence for the User.
- Does not add: a real snapshot artifact, approved request IDs, DSN,
  credential, raw prompt/response, raw IP, raw upstream error, migration,
  runtime request-path change, API route, frontend behavior, read-only DB/view
  credential, shared ECS staging, exporter/outbox, X-Insight runtime dependency,
  or PRD4 matrix changes.

PRD5 Batch 1S:

- Scope: X-Insight data exchange control surface product/architecture boundary.
- Status: Canary PASS 6/6; Sentinel merge reflected by `c6ae2a7` / PR #122.
- Adds: `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`; shared X-Insight
  review request
  `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md`;
  recommendation for a future UniRouter Data Exchange Control entry covering
  outbound approvals, export ledger, handoff artifacts, import receipts, and
  deep links; and an explicit boundary that X-Insight owns importer internals,
  normalized evidence, read-side audit, investigation timeline/graph, policy
  simulation, and evidence packs.
- Does not add: UI code, API route, DB migration, real snapshot artifact,
  approved request IDs, DSN, credential, raw prompt/response, raw IP, raw
  upstream error, read-only DB/view, shared ECS staging, exporter/outbox,
  X-Insight runtime dependency, or PRD4 matrix changes.

PRD5 Batch 1T:

- Scope: Gate A exchange envelope, exact approval-manifest hashing, receipt
  contract reconciliation, and controlled-run authorization.
- Status: Canary PASS 7/7; Sentinel merge reflected by `fc62e65` / PR #123.
- Adds: snapshot/handoff `exchange_id`; manifest-selected
  `approval_manifest_sha256`; manifest-preserving handoff commands; focused
  tests; `docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md`; shared exchange
  contract, authorization worksheet, and UniRouter response.
- Post-Batch decision: User approved the environment/operator/retention
  recommendation as `UR-XI-GATE-A-20260710-01`; Atlas executed it in Batch 1V.
- Does not add: real requests or artifacts, approved IDs, secrets, UI, API,
  migration, runtime path changes, read-only DB/view, shared ECS,
  exporter/outbox, X-Insight runtime dependency, or PRD4 changes.

PRD5 Batch 1U:

- Scope: Embeddings API-key model-permission parity with Chat.
- Status: Canary PASS 7/7; Sentinel merge reflected by `b2ba892` / PR #124.
- Adds: shared `allowed_models` predicate; Embeddings pre-billing 403;
  `X-Request-ID`; identical `auth.model_not_allowed`, `permission_denied`, and
  auth decision evidence; focused helper/Chat/Embeddings tests; shared X-Insight
  contract.
- Does not add: route, migration, frontend, billing amount, routing/provider
  success-path, raw payload, exporter/outbox, X-Insight runtime dependency, real
  Gate A execution, target claim, or PRD4 changes.

PRD5 Batch 1V:

- Scope: activate the User-approved Gate A controlled run and hand target
  deployment/execution to Atlas without widening data access.
- Status: Canary PASS 7/7; Sentinel merge reflected by `cc38094` / PR #125.
  Atlas then executed `UR-XI-GATE-A-20260710-01` against deployed app commit
  `b2ba892` and generated exchange `uxchg_f7de31e541516893a0d9b31cc0f3892a`;
  X-Insight externally verified persistent import/readback in `4a562a6` / PR #9
  and closed it in `5f0fa49` / PR #10.
- Adds: one immutable metadata-only snapshot with one call log, seven trace
  events, two generated capture gaps, exact manifest binding, and a reconciled
  `persisted` receipt. Import accepted 10/10 events; first persistence inserted
  10, rerun produced 10 duplicate/no-op with 0 conflict, and readback returned
  10 events/3 persisted gaps.
- Known gap: the denied request returned 403 but the live pre-relay wrapper
  omitted `X-Request-ID` and the Batch 1P trace; the artifact is valid diagnostic
  gap evidence, not complete denied-scenario target evidence.
- Does not add: fabricated request IDs, production traffic, automatic operator
  reassignment, raw prompt/response or secrets, broad discovery, X-Insight
  read-only DB/view, shared cross-project ECS staging, exporter/outbox, PRD4
  changes, or `Production verified` claims.
- Remaining: exact-path deletion of both temporary snapshot JSON artifacts by
  2026-07-17, real upstream E2E, and future X-Insight durable runtime contracts.
  Receipts, hashes, sanitized evidence, and persisted normalized evidence remain
  retained. X-Insight's synthetic-only target verification and UniRouter ECS
  security remediation are complete.

PRD5 Batch 1W:

- Scope: ensure live API-key `allowed_models` denials reach Relay after project
  model-governance resolution.
- Status: target verified under `UR-XI-GATE-A-20260710-02` on target
  `e823abc293f996264763c6c1aca19d3d4d6e705f`; second X-Insight snapshot
  persistence/idempotency/readback verified.
- Adds: removal of the duplicate pre-relay API-key check; full-handler
  integration coverage for HTTP 403, matching `X-Request-ID`, exact auth
  decision trace fields, zero call-log rows, and zero billing freezes.
- Local verification: targeted `cmd/uniroute` and `internal/gateway` tests,
  migrated-DB integration test, and full `go test ./... -count=1` passed.
- Does not add: project model-governance semantic changes, route, migration,
  frontend, raw payload, target rerun, X-Insight runtime dependency,
  exporter/outbox, PRD4 changes, or production claims.

Latest completed PRD4 verification result is PRD4 Batch 1S Open Issue Boundary
Closeout:

- Status: Canary PASS 7/7 in `docs/agent-workflows/VERIFY-RESULT.md`;
  Sentinel merge reflected by `5b41b87` / PR #103.
- Scope: documentation/status-boundary only.
- Mapping: `PRD4-PLUGIN-001` -> `PRD5-PLUGIN-001` / `PRD5-MARKET-001`,
  `PRD4-DEPLOY-001` -> `PRD5-DEPLOY-001`, and `PRD4-INFRA-001` ->
  `PRD5-INFRA-001`.
- Target cleanup: User reported Atlas deleted `uniroute_test`, closing
  `PRD4-DEPLOY-002`.
- Evidence: `docs/ops/evidence/PRD4-final-closeout-20260708.md`.
- Boundary: no Go, frontend, migration, API route, runtime behavior, deployment
  script, or `Production verified` claim changed.

Previous completed verification result is PRD4 Batch 1R Runtime Build Chain
Follow-up:

- Status: Canary PASS 9/9; Sentinel merge reflected by `819846c` / PR #102.
- Scope: repo-controllable `PRD4-RUNTIME-002` runtime/build-chain gaps.
- Evidence: `docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md`.
- Changes: Go toolchain pin to `go1.25.12`, Docker builder patch version,
  version/commit `ldflags`, runtime metadata fallback, compose
  `RUNTIME_ENVIRONMENT=local`, compose `GRAFANA_URL=http://grafana:3000`,
  backend `/health/ready` healthcheck, env/runbook docs.
- Verification: focused Go tests, `go test ./... -count=1`, `go build`
  with `-ldflags`, `go env`
  showing `go1.25.12` / `auto`, YAML parse, and `git diff --check`.
- Boundary: Docker CLI was unavailable to Canary, so live container health still
  needs CI/Atlas before any Production verified runtime claim; target test
  database cleanup was later closed by User-reported Atlas deletion of
  `uniroute_test`.

Previous completed verification result is PRD4 Batch 1Q Production Evidence
Promotion:

- Status: Canary PASS 8/8; Sentinel commit reflected by `f0d34e1`.
- Scope: documentation/evidence-boundary only.
- Evidence: `docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md`.
- Decision: current target evidence is target-smoke evidence for selected PRD4
  areas, but no PRD4 matrix row is promoted to `Production verified`.
- Reason: the final report covers the PRD4 Batch 1N target state, Batch 1O
  fixed later findings, and `PRD4-RUNTIME-002` was still open at that point.
- No Go, frontend, migration, API route, runtime behavior, deployment script,
  evidence harness code, or production deployment change was made in Batch 1Q.

Latest accepted PRD4 documentation/status-boundary batch is PRD4 Batch 1P Scope
Deferral And Closeout Alignment:

- Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `9e2927f` / PR #100.
- Scope: documentation/status-boundary only. User decision on 2026-07-08 moves
  `P4-F08` / `P4-F09` real target execution out of current PRD4 because no
  approved real upstream or real SSO environments are available.
- Batch 1H/1I harnesses remain accepted reusable assets.
- `docs/planning/PRD5-BACKLOG.md` now tracks `PRD5-UPSTREAM-001` and
  `PRD5-SSO-001`.
- `P4-F13` PRD5 Backlog Consolidation is accepted.
- No Go, frontend, migration, API route, runtime behavior, deployment script,
  evidence harness code, or Production verified claim changed.

Latest accepted PRD4 implementation batch remains PRD4 Batch 1O API Key Input And
Security Header Hardening:

- Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `59721d7` / PR #99.
- Issue/matrix: `PRD4-SECURITY-001` / `P4-F12`.
- Scope: API key create/update rejects unsafe or overlong display names and
  negative `project_id`; the HTTP chain adds baseline browser security headers;
  and unknown GET paths no longer fall through to the root service/version
  response.
- This batch does not change billing, routing, RBAC, migrations, trace storage,
  SSO, upstream, wallet, provider dispatch, frontend behavior, Go toolchain
  version, Grafana target config, runtime `ldflags`, container healthcheck
  wiring, target test database cleanup, or Production verified claims.
- Runtime/build-chain findings from the 2026-07-08 target report passed Batch 1R
  Canary verification as `PRD4-RUNTIME-002`; target test database cleanup was
  later closed by User-reported Atlas deletion of `uniroute_test`.

Previous accepted PRD4 implementation batch is PRD4 Batch 1N Developer Console
Self Usage And Model Cost:

- Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `6194062` / PR #98.
- Batch 1N uses existing Usage Analytics with `scope=my`, selected `project_id`,
  and `group_by=alias` to show the current user's own usage, cost, and
  model/alias breakdown in Developer Console.
- Target-environment final test evidence for the deployed PRD4 Batch 1N state is
  archived at `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`.
- The report also records follow-up security/runtime issues; application-code
  issues are in Batch 1O, repo-controllable runtime/build-chain issues passed
  Batch 1R Canary verification as `PRD4-RUNTIME-002`, and target DB cleanup was
  later closed by User-reported Atlas deletion of `uniroute_test`.

Previous accepted trace redaction batch is PRD4 Batch 1M:

- Canary PASS 8/8 was recorded before Batch 1N overwrote the current
  `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `2f43fca` / PR #97.
- Batch 1M shares trace metadata sanitization across gateway trace writes and
  `GET /api/v1/logs/{id}/trace` responses.
- Restricted trace metadata keys such as `api_key`, `authorization`,
  `access_token`, `request_body`, and `prompt` are not returned by the trace
  API even if they exist in historical or manually inserted rows.
- Batch 1M does not change route registration, RBAC, DB schema, trace
  sampling/storage semantics, frontend layout, billing, routing, auth, SSO,
  upstream, wallet, provider dispatch, or Production verified target evidence.

Previous accepted runtime redaction batch is PRD4 Batch 1L:

- Canary PASS 8/8 was recorded before Batch 1M overwrote the current
  `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `4426efd` / PR #96.
- Batch 1L redacts Prometheus/Grafana `url` and `health_url` response fields in
  `GET /api/v1/runtime/status` by stripping URL userinfo, query strings, and
  fragments while raw configured monitoring URLs are still used for probes.

Previous accepted trace retention batch is PRD4 Batch 1K:

- Canary PASS 10/10 was recorded before Batch 1L overwrote the current
  `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `1d9ea50` / PR #95.
- Batch 1K adds `TRACE_EVENT_RETENTION_DAYS`, defaults it to
  `LOG_RETENTION_DAYS`, exposes both retention knobs in env/compose/runbook, and
  wires existing cleanup to delete expired `request_trace_events`.
- Batch 1K does not add trace partitioning, archival, OpenTelemetry
  ingestion/export, prompt/response plaintext storage, runtime cache, or
  Production verified target-environment evidence.

Previous accepted runtime telemetry batch is PRD4 Batch 1J:

- Canary PASS 8/8 is recorded in earlier verification history.
- Sentinel merge is reflected by `8707346` / PR #94.
- Batch 1J adds host CPU/disk/network summaries to
  `GET /api/v1/runtime/status` and `/status` Runtime Observability cards without
  cluster inventory, raw topology, private IP/hostname or cloud identifiers,
  OpenTelemetry, metric alerts, or Production verified target evidence.

Accepted real SSO evidence-harness batch is PRD4 Batch 1I, with real target
execution deferred to PRD5:

- Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `2c02430` / PR #92.
- Batch 1I adds guarded tooling for `P4-F09` real SSO evidence:
  `scripts/prd4-real-sso-smoke.sh`, `scripts/prd4_real_sso_smoke.mjs`,
  `scripts/prd4_real_sso_audit_check.go`, and
  `docs/tech-ref/24-prd4-real-sso-evidence.md`.
- Preflight mode is safe without target credentials: it reports `SKIP`, does
  not drive a browser, does not call an IdP, and does not read the database.
- Real mode requires approved Logto or enterprise OIDC target access, a test
  user, console/backend/issuer URLs, DB DSN, and
  `UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`.
- User decision on 2026-07-08: because no approved real SSO environment exists,
  current PRD4 keeps this harness only and defers real target execution to
  `PRD5-SSO-001`.

Latest workflow verification gate is Protocol Risk-Gated Workflow Update And
Status Reconciliation:

- Canary PASS 10/10 was recorded in `docs/agent-workflows/VERIFY-RESULT.md`
  before later PRD4 product batches overwrote the current result file.
- No runtime code, migrations, PRD4 product behavior, or target-environment
  evidence changed.

Accepted real upstream evidence-harness batch is PRD4 Batch 1H, with real
target execution deferred to PRD5:

- Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `d3acfa6` / PR #91.
- Atlas deployment has not been reported for Batch 1H.
- Batch 1H adds guarded tooling for `P4-F08` real upstream evidence:
  `scripts/prd4-real-upstream-smoke.sh`, `scripts/prd4_real_upstream_seed.go`,
  `scripts/prd4_real_upstream_smoke.mjs`, and
  `docs/tech-ref/23-prd4-real-upstream-evidence.md`.
- Preflight mode is safe without credentials: it reports `SKIP`, does not
  mutate the database, and does not call providers.
- Real mode requires approved OpenRouter and direct Anthropic credentials/model
  slugs, matching backend secrets, and
  `UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`.
- User decision on 2026-07-08: because no approved real upstream environment
  exists, current PRD4 keeps this harness only and defers real target execution
  to `PRD5-UPSTREAM-001`.

The latest accepted route-contract baseline remains PRD3 Batch 6 on 2026-07-05:

```bash
go test ./cmd/uniroute -run 'Test(OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage|AdminRoutesRegisteredForFrontendClient)' -count=1 -v
npm --prefix frontend test -- --run ApiDocsPage client
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- Route parity target tests: PASS.
- Go tests: PASS.
- Frontend tests: PASS, 18 files / 97 tests.
- Frontend build: PASS with Vite large chunk warning.

Always check `docs/ops/CURRENT-STATUS.md`, the active acceptance matrix,
`docs/agent-workflows/EXECUTION-RECORD.md`, and applicable immutable evidence
before making new claims. `VERIFY-RESULT.md` is protocol v1.x history.

## Worktree Warning

Always run `git status --short` before editing. The worktree may contain user or agent changes from another round. Do not revert them unless the user explicitly requests it. Avoid mixing unrelated cleanup with the current batch.

## No Bulk Deletion

Project instruction forbids bulk deletion of files or directories. If cleanup requires removing files, stop and ask the user. Only one explicit file path may be deleted at a time after approval.
