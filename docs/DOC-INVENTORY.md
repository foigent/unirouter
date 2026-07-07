# UniRouter Document Inventory

> Purpose: classify documents so agents know what is current truth, implementation reference, historical evidence, or stale/conflicting material.

## Current Truth

| File | Role |
|------|------|
| `docs/PROJECT-HANDOFF.md` | First-read handoff for agents |
| `docs/ops/CURRENT-STATUS.md` | Current verified project state |
| `docs/PRD-Phase4.md` | Formal PRD4 scope and implementation-phase reference |
| `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` | Active PRD4 acceptance matrix and evidence boundaries |
| `docs/planning/PRD5-BACKLOG.md` | Capabilities explicitly deferred beyond PRD4 |
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
| `docs/agent-workflows/AGENT-PROTOCOL.md` | Required Codex/OpenClaw workflow |
| `docs/agent-workflows/FIX-SIGNAL.md` | Active verification signal: PRD4 Batch 1K Trace Retention And Cleanup awaiting Canary |
| `docs/agent-workflows/VERIFY-RESULT.md` | Latest OpenClaw verification result: PRD4 Batch 1J Runtime Host Telemetry PASS 8/8 |

## Product And Design References

| File/Dir | Role | Notes |
|----------|------|-------|
| `docs/PRD-Phase1.md` | Product requirements and acceptance criteria | Requirements source, not current progress source |
| `docs/PRD-Phase1.pdf` | PRD PDF export | May lag Markdown |
| `docs/planning/PRD2-DISCOVERY.md` | PRD2 direction and scope discovery | Historical planning input for frozen PRD2 |
| `docs/PRD-Phase2.md` | PRD2 formal product requirements | Frozen for new feature work |
| `docs/planning/PRD3-CONSISTENCY-AUDIT.md` | PRD3 consistency audit | Confirmed PRD3 input; completed in Batch 17 closeout |
| `docs/PRD-Phase3.md` | PRD3 formal product requirements | Completed cleanup/optimization scope |
| `docs/PRD-Phase4.md` | PRD4 formal product requirements | Active PRD4 implementation-phase scope |
| `docs/planning/PRD4-BACKLOG.md` | PRD4 candidate backlog | Source record for PRD4 scope discovery |
| `docs/planning/PRD5-BACKLOG.md` | PRD5 backlog | Explicitly deferred capabilities and reopen prerequisites |
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
| `docs/tech-ref/09-admission-control.md` | Admission control reference |
| `docs/tech-ref/10-provider-adapter-framework.md` | PRD2 provider adapter framework draft |
| `docs/tech-ref/11-project-rbac-model.md` | PRD2 project/RBAC model draft |
| `docs/tech-ref/12-token-metering-v2.md` | PRD2 token metering source and reconciliation contract |
| `docs/tech-ref/13-audit-schema-upgrade.md` | PRD2 audit source, filter, and before/after contract |
| `docs/tech-ref/14-notification-event-foundation.md` | PRD2 notification event, webhook routing, and project budget threshold contract |
| `docs/tech-ref/15-ha-readiness.md` | PRD2 multi-instance, shared-state, smoke, and rollback contract |
| `docs/tech-ref/16-license-foundation.md` | PRD2 offline license, audit, warning, and Batch 22 smoke contract |
| `docs/tech-ref/17-cache-research.md` | PRD2 cache research boundary; exact/semantic risks and no default runtime cache |
| `docs/tech-ref/18-oidc-sso.md` | PRD2 OIDC first SSO, claim mapping, provisioning, and audit contract |
| `docs/tech-ref/19-prompt-response-storage-policy.md` | PRD2 P2-F11 prompt/response storage policy: plaintext disabled by default and future opt-in controls |
| `docs/tech-ref/20-prd3-product-glossary.md` | PRD3 canonical terminology for provider/channel, usage analytics, notification status, roles, and PRD4 boundaries |
| `docs/tech-ref/21-prd4-quota-vocabulary.md` | PRD4 RPM/RPS/TPM/tokens/s quota vocabulary and `qps_limit` compatibility boundary |
| `docs/tech-ref/22-prd4-plungein-port-contract.md` | PRD4 PlungeIn Observe/Act/Enrich/Decide/Mutate port boundaries and runtime-plugin deferral contract |
| `docs/tech-ref/23-prd4-real-upstream-evidence.md` | PRD4 real upstream evidence harness, credential guardrails, and target-environment acceptance boundary |
| `docs/tech-ref/24-prd4-real-sso-evidence.md` | PRD4 real SSO evidence harness, Logto/OIDC credential guardrails, and target-environment acceptance boundary |
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
| `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` | PRD4 active acceptance matrix and evidence rules |
| `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md` | Final strict closeout gate and evidence procedure |
| `docs/ops/deployment-runbook.md` | Deployment reference |
| `docs/ops/evidence/` | Current runtime/deployment evidence cited by acceptance matrices and status docs |
| `docs/ops/canary-testing.md` | Canary deployment/testing reference |
| `docs/ops/test-plan.md` | Test case catalog |
| `docs/ops/test-report.md` | Historical test report; contains stale date/status and must not be used as current truth |
| `docs/ops/MVP-TEST-EXECUTION.md` | Historical E2E evidence plus pending cases; reconcile into acceptance matrix before treating as current |
| `docs/ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md` | Canary comprehensive code/deployment/security regression evidence; active risks are tracked in `docs/issues/OPEN-ISSUES.md` |

## Agent Workflow Files

| File | Role |
|------|------|
| `docs/agent-workflows/README.md` | Agent workflow index |
| `docs/agent-workflows/AGENT-PROTOCOL.md` | Current protocol |
| `docs/agent-workflows/FIX-SIGNAL.md` | Current Codex to OpenClaw verification instruction |
| `docs/agent-workflows/VERIFY-RESULT.md` | Current OpenClaw/Sentinel/Atlas verification result for the latest completed round |
| `docs/agent-workflows/CHECKLIST.md` | Canary checklist/reference; may need reconciliation |
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
