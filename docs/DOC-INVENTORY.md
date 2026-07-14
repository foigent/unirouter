# UniRouter Document Inventory

> Purpose: classify documents so agents know what is current truth, implementation reference, historical evidence, or stale/conflicting material.

## Current Truth

| File | Role |
|------|------|
| `docs/PROJECT-HANDOFF.md` | First-read handoff for agents |
| `docs/ops/CURRENT-STATUS.md` | Current verified project state |
| `docs/planning/PRD6-START-HERE.md` | PRD6 Completed (Accepted Scope) state, preserved future boundaries and read order |
| `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md` | PRD6 Git/CI/test/X-Insight/code-finding baseline evidence |
| `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md` | PRD6 source-backed 100x, Agent fan-out, billing, routing, streaming and multimodal anti-pattern evidence |
| `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md` | P6-F16 patch contract, regression-before-fix and local migrated-database evidence |
| `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md` | Confirmed finance, notification, pricing, global-control and model-governance tenant gaps outside P6-F16 |
| `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md` | P6-F33 notification/Webhook patch, local regression and CI-promotion evidence; target cutover remains separate |
| `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md` | P6-F37 audit-log tenant predicate and migrated two-tenant JWT evidence |
| `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md` | P6-F38 frontend test timing and required-CI enforcement evidence |
| `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md` | P6-F32/P6-F39 finance, request-log and usage-derived tenant-isolation evidence; PR #145 merged and CI verified |
| `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md` | P6-F17 server/client identity, metadata-only idempotency, attempt ledger, migration and PR #149/post-merge CI evidence |
| `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md` | P6-F18A pool-safe Freeze, price/source snapshot, wallet/grant conservation and single-terminal CI evidence |
| `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md` | P6-F18B pre-output marker, immutable usage, non-cancellation, leased restart recovery and PR #153/post-merge CI evidence |
| `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md` | P6-TEST-DB-001 stale shared-schema reproduction, isolated migrated admin suite and required-CI gate evidence |
| `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md` | P6-F03 retry classification, safe response, failover, billing and trace evidence |
| `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md` | P6-F04 Embeddings bounded failover, billing, trace and malformed-response evidence |
| `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md` | P6-F05 typed effective routing plus project budget-alert contract and PostgreSQL transition/scope evidence |
| `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md` | P6-F06 multiplier-aware insufficient-funds, concurrent exact-settlement and cancel/refund PostgreSQL evidence |
| `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md` | P6-STREAM-002A typed frame/timeout/writer/body/finalization prerequisite evidence; dynamic reservation and semantic interception were later closed by P6-F11 |
| `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md` | P6-F19A explicit Agent exchange/delegation, five-minute token, refresh/revocation, bounded authority and 100-child local plus PR #164/post-merge CI evidence |
| `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md` | P6-F19B shared ancestor cost/token/rate/concurrency local, PR #166 and post-merge CI evidence; closes P6-F19 while preserving P6-F20/P6-F21 boundaries |
| `docs/ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md` | P6-F07 shared selector RPM/RPS/TPM/concurrency, bounded failure, UI/control, migration and PR #168/post-merge CI closure evidence |
| `docs/ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md` | P6-F21 local plus PR #170/#180/#181 and final-main CI evidence for weighted cross-replica leases, bounded fairness/fault/config/UI behavior and the miniredis/real-Redis test boundary |
| `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md` | P6-F08 repository and CI evidence: deterministic weighted route snapshots, renewable cross-replica probes, epoch CAS, safe preview/evidence, guarded migration, PR #172 and post-merge main closure |
| `docs/ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md` | P6-F09 local plus PR #174/#175 and final main CI evidence: authoritative provider cache usage, immutable class pricing, settlement/recovery, logs/reconciliation/UI and guarded migration |
| `docs/ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md` | P6-F20 local, PR #177 exact-head and post-merge main CI closure evidence for durable finalization/outbox, bounded workers, replay and terminal-hook recovery |
| `docs/ops/evidence/PRD6-B3H-P6F40A-cost-shortfall-local-20260713.md` | D6-13/P6-F40A pre-fix reproduction, atomic prepaid cash top-up, non-terminal funding hold, gateway no-cancel, migration guard and current local/CI boundary |
| `docs/ops/evidence/PRD6-B3H-P6F40B-provider-attempt-ledger-local-20260713.md` | D6-13/P6-F40B metadata-only durable attempt state, financial retry/cancel authority, production billing-decorator pass-through, migration guard and current local/CI boundary |
| `docs/ops/evidence/PRD6-B3H-P6F40C-provider-key-reconciliation-local-20260713.md` | D6-13/D6-14 affected-key paid-admission block, platform-only amount-free reconciliation, tenant/concurrency/crash-gap/migration proof and current local/CI boundary |
| `docs/ops/evidence/PRD6-B3H-P6F41-usage-delivery-integrity-local-20260713.md` | D6-13/D6-14 typed explicit-zero/missing usage, funded estimates, local-delivery charge gate, interrupted-stream delivered counter, metadata-only receipt and current local/CI boundary |
| `docs/ops/evidence/PRD6-B4-P6F10-guardrail-kernel-local-20260713.md` | D6-15/P6-F10 immutable policy chain, bounded detectors/payloads, no-delivery cost holds, tenant-safe control/UI, guarded migration and PR #188 exact-head CI closure evidence |
| `docs/ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md` | D6-16/P6-F11 enforced stream buffering, strict inspection, termination, reservation extension, checkpoint/recovery and PR #189/main CI closure evidence |
| `docs/ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md` | PRD6 27/27 accepted-row closeout, repository/CI evidence level, non-claims and preserved PRD7/target boundaries |
| `docs/ops/evidence/PRD6-TARGET-DEPLOYMENT-AND-UI-VERIFICATION-20260713.md` | Authorized PRD6 ECS migration/deployment, stable/canary runtime, authenticated UI, selected historical route/failover readback, cleanup and remaining target non-claims |
| `docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md` | Fresh Git/CI/matrix/target-health audit, formal PRD6 phase-completion decision, corrected 27/27 accounting and preserved later boundaries |
| `docs/planning/V1-MICRO-PRECISION-LEDGER-PLAN-20260714.md` | User-approved Post-PRD6 option B money contract, non-rolling migration boundary and R3 decision gate |
| `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-LOCAL-20260714.md` | V1 micro-CNY implementation matrix, full local regression, isolated migration/API/browser evidence and target/X-Insight activation gates |
| `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-TARGET-20260714.md` | PR #202/#203 CI and merge truth, authorized non-rolling development-ECS migration/cutover, exact release, DB/API/runtime/browser/environment-label evidence and preserved non-claims |
| `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-LOCAL-20260715.md` | P0 runtime trust-root replacement closure, double-click macOS License Studio, local security/API/browser/build evidence and preserved key/merge/ECS/production boundaries |
| `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-CI-20260715.md` | PR #205 exact code-head required-CI closure for embedded License/upgrade trust rings, License Studio and formal release Docker image; merge/key/ECS decisions remain open |
| `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-MERGE-20260715.md` | User-approved PR #205 squash merge, exact final-head CI/main readback, no duplicate automatic main run and preserved key/ECS/production boundaries |
| `docs/ops/evidence/V1-FIRST-RELEASE-PRODUCT-AUDIT-LOCAL-20260715.md` | Complete first-release issue matrix, existing development-target API/browser facts, Local-verified defect/UX corrections and explicit provider/IdP/detector/X-Insight/Production boundaries |
| `docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md` | Official competitor HA patterns, current UniRouter cluster facts, Four Quadrants and accepted D6-12 stream/HA boundary |
| `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md` | Source-backed audit/reconciliation/correlation/UI/architecture/upgrade findings, Four Quadrants and PRD6 batches |
| `docs/planning/PRD6-UPSTREAM-COST-RECONCILIATION-AND-PROVIDER-COVERAGE-20260713.md` | Accepted customer-revenue/provider-COGS boundary, competitor and supplier billing evidence, Four Quadrants, PRD6 non-claims and future cost/metering/media candidates |
| `docs/planning/PRD7-CANDIDATE-BACKLOG.md` | PRD7 candidate split; first future direction selected, implementation remains later after PRD6 closeout and still requires its own authorization |
| `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md` | Accepted future Cache Service direction, service boundary, capacity planning triggers, SaaS cell reuse and non-authorization guardrails |
| `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md` | Version-fixed competitor audit, Four Quadrants and priority matrix |
| `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md` | Third-round Four Quadrants, CARTA/shadow/collusion ownership, Killer Features and candidate acceptance rows |
| `docs/planning/prd6-adaptive-trust-hardening/` | Evidence-bound adaptive-trust structural proposal, JSON, diagrams and option comparison; no implementation plan until selection |
| `docs/PRD-Phase6.md` | PRD6 Completed (Accepted Scope), D6-10/D6-16 stream and D6-11 Agent lifecycle contracts, plus separately proposed/deferred rows |
| `docs/ops/PRD6-ACCEPTANCE-MATRIX.md` | PRD6 authoritative 27/27 accepted-row closure plus proposed/external/target-evidence truth |
| `docs/planning/PRD6-IMPLEMENTATION-PLAN.md` | PRD6 batch order, risk gates, verification and rollback |
| `docs/planning/PRD6-BACKLOG.md` | PRD6 classified candidate queue and decision register |
| `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md` | Answered X-Insight runtime request, accepted v1 sidecar boundary, UniRouter contract lock, and PRD5 completion classification |
| `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md` | PRD5 repository-wide audit, P5-F12 screening, P0-P3 batches, and INFRA-SEC-001 evidence design |
| `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md` | PRD5 accepted UniRouter-owned completion evidence for trace search, Data Exchange Control, route/failover, redacted topology, and remaining boundaries |
| `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md` | PRD5 final target deployment, migration, browser, canary runtime parity, cleanup, and INFRA-SEC-001 cutover evidence |
| `docs/PRD-Phase5.md` | Formal completed accepted PRD5 scope and explicit deferred/external/evidence boundaries |
| `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` | PRD5 acceptance matrix; Batch 1W external/denied-path evidence, Batch 1Z target deployment/security evidence, and remaining Local/CI/target boundaries are recorded |
| `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md` | PRD5 Gate A request ID approval runbook for non-technical User/UniRouter-owner approval before controlled X-Insight snapshot generation |
| `docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md` | PRD5 Gate A prior-authorization and narrow operator runbook for newly generated controlled scenarios |
| `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` | PRD5 batch plan and UniRouter/X-Insight ownership split |
| `docs/planning/PRD5-COMPETITOR-COMPARISON.md` | PRD5 competitor comparison and UniRouter-owned catch-up decisions |
| `docs/PRD-Phase4.md` | Formal PRD4 scope and implementation-phase reference |
| `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` | Completed PRD4 accepted/deferred matrix and evidence boundaries |
| `docs/planning/PRD5-BACKLOG.md` | Capabilities explicitly deferred beyond PRD4 |
| `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md` | PRD5 X-Insight Gate A coordination decisions and access sequencing |
| `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md` | PRD5 X-Insight data exchange control surface boundary and staged UI decision |
| `docs/planning/PRD5-DEFERRED-CONTROL-DESIGNS-20260710.md` | Implementation-ready budget hard-block, service-account, and incident/SLO designs whose runtime remains deferred |
| `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` | Completed PRD3 cleanup/optimization acceptance state |
| `docs/PRD-Phase3.md` | Completed PRD3 cleanup/optimization scope |
| `docs/planning/PRD3-CONSISTENCY-AUDIT.md` | PRD3 lineage audit and completed cleanup input |
| `docs/planning/PRD4-BACKLOG.md` | PRD4 candidate backlog source record; formal PRD4 scope is `docs/PRD-Phase4.md` |
| `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` | Frozen PRD2 acceptance/deferred state |
| `docs/ops/PRD2-COMPLETION-PLAN.md` | Frozen PRD2 closeout decisions and historical plan |
| `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | PRD1 historical acceptance progress and closeout evidence |
| `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md` | PRD1 historical test-ownership map |
| `docs/issues/OPEN-ISSUES.md` | Current unresolved issue queue |
| `docs/DOCS-GOVERNANCE.md` | Documentation rules |
| `docs/governance/README.md` | Engineering governance minimum for ADR/API/migration/release/incident/deprecation rules |
| `docs/agent-workflows/AGENT-PROTOCOL.md` | Required single-owner execution, approval, evidence and optional-review workflow |
| `docs/agent-workflows/EXECUTION-RECORD.md` | Current V1 target-evidence closeout, exact release/CI/deployment truth and preserved Production/X-Insight/future boundaries; previous PRD6/PRD5 evidence remains linked |
| `docs/agent-workflows/reviews/` | Optional immutable independent review results |
| `docs/agent-workflows/FIX-SIGNAL.md` | Protocol v1.x compatibility entry pointing to the current execution record |
| `docs/agent-workflows/VERIFY-RESULT.md` | Historical protocol v1.x result: PRD5 Batch 1V PASS 7/7 |

## Product And Design References

| File/Dir | Role | Notes |
|----------|------|-------|
| `docs/PRD-Phase1.md` | Product requirements and acceptance criteria | Requirements source, not current progress source |
| `docs/PRD-Phase1.pdf` | PRD PDF export | May lag Markdown |
| `docs/planning/PRD2-DISCOVERY.md` | PRD2 direction and scope discovery | Historical planning input for frozen PRD2 |
| `docs/PRD-Phase2.md` | PRD2 formal product requirements | Frozen for new feature work |
| `docs/planning/PRD3-CONSISTENCY-AUDIT.md` | PRD3 consistency audit | Confirmed PRD3 input; completed in Batch 17 closeout |
| `docs/PRD-Phase3.md` | PRD3 formal product requirements | Completed cleanup/optimization scope |
| `docs/PRD-Phase6.md` | PRD6 product requirements | D6-01/D6-02 accepted; advanced innovation remains separately gated |
| `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md` | PRD6 product/architecture/UX audit | Confirmed findings, hypotheses and user requirements remain separately labeled |
| `docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md` | PRD6 HA and stream-failover architecture reference | Official-source competitor facts, explicit inferences and recommendations remain separately labeled; no deployment authorization |
| `docs/planning/PRD6-UPSTREAM-COST-RECONCILIATION-AND-PROVIDER-COVERAGE-20260713.md` | PRD6 provider-cost reconciliation reference | D6-14 accepted architecture boundary and future candidate classification; no live connector, bill import, media implementation, service split or production claim |
| `docs/planning/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-CONTRACT-20260714.md` | Post-PRD6 V1 release contract | User-approved commercial License enforcement, signed offline bundle, recovery, acceptance and separate R4 target-activation boundaries; does not reopen PRD6 |
| `docs/planning/PRD7-CANDIDATE-BACKLOG.md` | PRD7 candidate backlog | The separately approved Post-PRD6 gate implements minimum signed-bundle verification/state/source-free delivery; Update Center, automatic apply/rollback, N-1 policy and all other PRD7 candidates remain separately gated |
| `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md` | Future Cache Service candidate | Exact-cache service boundary and private/SaaS cell profiles accepted as direction only; no implementation authorization |
| `docs/PRD-Phase5.md` | PRD5 completed product requirements | Accepted Scope Complete; preserve as closed phase evidence |
| `docs/PRD-Phase4.md` | PRD4 formal product requirements | Completed current PRD4 scope |
| `docs/planning/PRD4-BACKLOG.md` | PRD4 candidate backlog | Source record for PRD4 scope discovery |
| `docs/planning/PRD5-BACKLOG.md` | PRD5 backlog | Explicitly deferred capabilities and reopen prerequisites |
| `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md` | PRD5 X-Insight Gate A coordination | Planning coordination for sanitized snapshot first, Batch 1O controlled snapshot handoff prep, read-only DB/view later, structural fields before exporter/outbox, and no shared GitHub/ECS staging yet |
| `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md` | PRD5 X-Insight data exchange control | Implemented UniRouter-owned outbound approval/export/delivery/receipt surface and remaining X-Insight contract without duplicating downstream audit/investigation UI |
| `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` | PRD5 implementation plan | Batch order, gates, and ownership split for UniRouter vs X-Insight |
| `docs/planning/PRD5-COMPETITOR-COMPARISON.md` | PRD5 competitor comparison | Source-grounded comparison against LiteLLM, New API, one-api, Langfuse, and Helicone |
| `docs/design/` | UI design decisions | Current design reference unless contradicted by code |
| `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md` | PRD2 Developer Console design spec | Draft for review |
| `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md` | Frontend UI/architecture audit | Current PRD1 closeout and PRD2 frontend architecture guidance |
| `docs/consultations/` | External/internal consultation history | Background, not current status |
| `docs/analysis/UNIROUTER-VS-ONEAPI-COMPARISON.md` | OneAPI comparison | Reference only |

## Technical References

| File/Dir | Role |
|----------|------|
| `docs/tech-ref/README.md` | Technical reference index |
| `docs/tech-ref/01-provider-capability-matrix.md` | Provider capability reference |
| `docs/tech-ref/02-billing-state-machine.md` | Billing state reference |
| `docs/tech-ref/03-error-handling-matrix.md` | Error handling and retry reference |
| `docs/tech-ref/04-oneapi-hook-manifest.md` | OneAPI hook analysis |
| `docs/tech-ref/05-openapi-spec.yaml` | Current UniRouter API route contract aligned to backend routes and docs parity guards |
| `docs/tech-ref/06-frontend-spec.md` | Frontend spec reference |
| `docs/tech-ref/07-structured-logging.md` | Logging plan/reference |
| `docs/tech-ref/08-prometheus-metrics.md` | Metrics reference |
| `docs/tech-ref/09-admission-control.md` | Replica-invariant weighted Chat/Streaming/Embeddings admission, fairness, lease and failure-contract reference |
| `docs/tech-ref/10-provider-adapter-framework.md` | PRD2 provider adapter framework draft |
| `docs/tech-ref/11-project-rbac-model.md` | PRD2 project/RBAC model draft |
| `docs/tech-ref/12-token-metering-v2.md` | PRD2 token metering source and reconciliation contract |
| `docs/tech-ref/13-audit-schema-upgrade.md` | PRD2 audit source, filter, and before/after contract |
| `docs/tech-ref/14-notification-event-foundation.md` | PRD2 notification event, webhook routing, and project budget threshold contract |
| `docs/tech-ref/15-ha-readiness.md` | PRD2 multi-instance, shared-state, smoke, and rollback contract |
| `docs/tech-ref/16-license-foundation.md` | V1 commercial License enforcement and historical PRD2 compatibility boundary |
| `docs/tech-ref/17-cache-research.md` | PRD2 cache research boundary; exact/semantic risks and no default runtime cache |
| `docs/tech-ref/18-oidc-sso.md` | PRD2 OIDC first SSO, claim mapping, provisioning, and audit contract |
| `docs/tech-ref/19-prompt-response-storage-policy.md` | PRD2 P2-F11 prompt/response storage policy: plaintext disabled by default and future opt-in controls |
| `docs/tech-ref/20-prd3-product-glossary.md` | PRD3 canonical terminology for provider/channel, usage analytics, notification status, roles, and PRD4 boundaries |
| `docs/tech-ref/21-prd4-quota-vocabulary.md` | PRD4 RPM/RPS/TPM/tokens/s quota vocabulary and `qps_limit` compatibility boundary |
| `docs/tech-ref/22-prd4-plungein-port-contract.md` | PRD4 PlungeIn Observe/Act/Enrich/Decide/Mutate port boundaries and runtime-plugin deferral contract |
| `docs/tech-ref/23-prd4-real-upstream-evidence.md` | PRD4 real upstream evidence harness and credential guardrails; real target execution is deferred to PRD5 |
| `docs/tech-ref/24-prd4-real-sso-evidence.md` | PRD4 real SSO evidence harness and Logto/OIDC credential guardrails; real target execution is deferred to PRD5 |
| `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md` | PRD5 X-Insight sanitized snapshot tool, handoff note, and handoff contract |
| `docs/tech-ref/26-prd5-evidence-field-contract.md` | PRD5 structural evidence field dictionary and migration/writer plan |
| `docs/tech-ref/27-signed-offline-upgrade.md` | V1 signed offline bundle, verification, sequence/schema and readback contract |
| `docs/governance/ADR-TEMPLATE.md` | Architecture Decision Record template |
| `docs/governance/adr/ADR-0001-prd4-plungein-ports.md` | PRD4 PlungeIn ports architecture decision |
| `docs/governance/adr/ADR-0002-prd4-api-key-owner-usage.md` | PRD4 API key owner usage attribution decision draft |
| `docs/governance/adr/ADR-0003-prd4-status-center-observability.md` | PRD4 Status Center and runtime observability decision draft |

PDF copies in `docs/tech-ref/` are export artifacts and may lag Markdown/YAML.

## Operations And Testing

| File | Current Role |
|------|--------------|
| `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md` | Executable ownership map for remaining PRD1 acceptance tests |
| `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md` | Current local evidence and external acceptance queue |
| `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` | PRD4 completed acceptance matrix and evidence rules |
| `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` | PRD5 completed acceptance matrix and evidence rules |
| `docs/ops/PRD6-ACCEPTANCE-MATRIX.md` | Current PRD6 Accepted Scope Complete and proposed/external/target-evidence truth |
| `docs/ops/evidence/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-LOCAL-20260715.md` | Complete Post-PRD6 License/upgrade issue matrix, local implementation/test evidence, required-CI gate and target non-claims |
| `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md` | Current PRD6 local baseline and code-finding evidence |
| `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md` | Current PRD6 scalability/anti-pattern audit; findings remain source/local, not 100x or target verified |
| `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md` | Current P6-F33 local plus PR/post-merge CI evidence; target cutover not executed |
| `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md` | P6-F37 local and CI evidence; PR #143 and post-merge required CI passed |
| `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md` | P6-F38 local and CI evidence; PR #143 proves Vitest executes in required CI |
| `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md` | Current P6-F32/P6-F39 local, merged PR #145 and required-CI evidence |
| `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md` | Current P6-F17 local and CI-verified Scheme A identity/idempotency evidence; target/production claims remain separate |
| `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md` | Current P6-F18A local and CI evidence for wallet/reservation invariants |
| `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md` | Current P6-F18B local and CI evidence; P6-F20 and target/production claims remain separate |
| `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md` | Current P6-TEST-DB-001 isolated migrated admin-suite and required-CI evidence |
| `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md` | Current P6-F03 local and CI retry-classification and billing/trace invariant evidence |
| `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md` | Current P6-F04 local and CI Embeddings multi-route/final-attempt evidence |
| `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md` | Current P6-F05 local and CI effective configuration, retired global alert keys, project budget scope, API/UI/trace and PostgreSQL evidence |
| `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md` | Current P6-F06 local and CI multiplier reservation/settlement/cancel evidence; PR #160 and post-merge main CI passed |
| `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md` | Current stream-transport prerequisite local plus PR #162/post-merge CI evidence; remaining D6-10/P6-F11 work is separate |
| `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md` | Current P6-F19A local and CI-verified capability-kernel prerequisite evidence |
| `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md` | Current P6-F19B local and CI-verified shared ancestor runtime evidence; P6-F19 is closed |
| `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md` | PRD5 closeout accounting and historical next-window input |
| `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md` | PRD5 Batch 1X local repository audit, P5-F12 screening decisions, safe local Logs/CI fixes, and INFRA-SEC-001 plan |
| `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md` | PRD5 Batch 1Y accepted UniRouter-owned implementation and local full verification |
| `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md` | PRD5 Batch 1Z target deployment, migration, browser, canary parity, cleanup, and security-group cutover |
| `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md` | X-Insight runtime response facts, synthetic-only target evidence, unimplemented capabilities, and UniRouter v1 contract lock |
| `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md` | PRD5 Gate A request ID approval runbook and data-minimization boundary |
| `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md` | Final strict closeout gate and evidence procedure |
| `docs/ops/deployment-runbook.md` | Deployment reference |
| `docs/ops/evidence/` | Current runtime/deployment evidence cited by acceptance matrices and status docs |
| `docs/ops/canary-testing.md` | Canary deployment/testing reference |
| `docs/ops/test-plan.md` | Test case catalog |
| `docs/ops/test-report.md` | Historical test report; contains stale date/status and must not be used as current truth |
| `docs/ops/MVP-TEST-EXECUTION.md` | Historical E2E evidence plus pending cases; reconcile into acceptance matrix before treating as current |
| `docs/ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md` | Canary comprehensive code/deployment/security regression evidence; active risks are tracked in `docs/issues/OPEN-ISSUES.md` |
| `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md` | Target-environment final test report for Alibaba Cloud PRD4 Batch 1N / PR #98; positive evidence and follow-up risks are reconciled into `CURRENT-STATUS.md`, `PRD4-ACCEPTANCE-MATRIX.md`, and `OPEN-ISSUES.md` |
| `docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md` | PRD4 Batch 1Q production evidence promotion decision; classifies target-smoke evidence and records that no PRD4 row is promoted to `Production verified` |
| `docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md` | PRD4 Batch 1R local evidence for repo-controllable runtime/build-chain fixes under `PRD4-RUNTIME-002` |
| `docs/ops/evidence/PRD4-B1S-open-issue-boundary-closeout-20260708.md` | PRD4 Batch 1S open issue boundary evidence; maps remaining plugin/deploy/infra PRD4 open items to PRD5 and leaves target DB cleanup to Atlas |
| `docs/ops/evidence/PRD4-final-closeout-20260708.md` | PRD4 final closeout evidence; records PRD4 complete for current scope and target test DB cleanup completion by User-reported Atlas action |
| `docs/ops/evidence/AGENT-PROTOCOL-v2-single-owner-20260710.md` | User-approved single execution owner, Git/CI, approval and optional-review decision evidence |
| `docs/ops/evidence/PRD5-B1W-pre-relay-model-permission-parity-local-20260710.md` | Batch 1W local root-cause, implementation, regression and target-boundary evidence |
| `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md` | Batch 1W target deployment/rerun, temporary-key cleanup, second X-Insight receipt, runtime boundary and infrastructure findings |
| `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md` | Batch 1X local audit, P5-F12 screening, Logs/CI fixes, and INFRA-SEC-001 remediation evidence design |
| `docs/ops/evidence/PRD5-GATE-A-XINSIGHT-external-integration-20260710.md` | Gate A snapshot/manifest/receipt reconciliation, persistent import, idempotency, readback and remaining-boundary evidence |

## Agent Workflow Files

| File | Role |
|------|------|
| `docs/agent-workflows/README.md` | Agent workflow index |
| `docs/agent-workflows/AGENT-PROTOCOL.md` | Current Agent Protocol v2.0 |
| `docs/agent-workflows/EXECUTION-RECORD.md` | Current V1 target-evidence closeout, branch/CI gate and preserved Production/X-Insight/future boundaries; prior execution evidence remains linked |
| `docs/agent-workflows/reviews/README.md` | Independent review naming, contents and severity rules |
| `docs/agent-workflows/FIX-SIGNAL.md` | Protocol v1.x compatibility entry |
| `docs/agent-workflows/VERIFY-RESULT.md` | Historical protocol v1.x result: PRD5 Batch 1V PASS 7/7 |
| `docs/agent-workflows/CHECKLIST.md` | Historical Canary checklist/reference; optional review input only |
| `docs/agent-workflows/PLAN.md` | Historical/current plan notes; do not treat as canonical without reconciliation |
| `docs/agent-workflows/CONTEXT_HANDOFF.md` | Context transfer notes; reference only |

## Historical Archive

| Path | Role |
|------|------|
| `docs/testing/archive/` | Historical testing stages, coverage, bug log, and status snapshots |
| `docs/archive/` | Historical knowledge and todo notes |

Archive files are useful for evidence and root-cause history, but any active status must be copied into `CURRENT-STATUS.md`, `PRD-ACCEPTANCE-MATRIX.md`, or `OPEN-ISSUES.md`.

## Known Stale Or Conflicting Items

| File | Issue |
|------|-------|
| `docs/ops/test-report.md` | Historical report with stale date/status; top banner now marks it non-current and corrects the AES-GCM encryption note |
| `docs/testing/archive/TEST-STATUS.md` | Historical status lists bugs as pending that later records mark fixed |
| `docs/testing/archive/BUG-LOG.md` | Contains reused bug numbers and mixed historical/current language |
| `docs/ops/MVP-TEST-EXECUTION.md` | Contains valuable E2E evidence but also pending cases; must be reconciled item by item |
