# UniRouter Document Inventory

> Purpose: classify documents so agents know what is current truth, implementation reference, historical evidence, or stale/conflicting material.

## Current Truth

| File | Role |
|------|------|
| `docs/PROJECT-HANDOFF.md` | First-read handoff for agents |
| `docs/ops/CURRENT-STATUS.md` | Current verified project state |
| `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` | PRD2 acceptance planning and future evidence matrix |
| `docs/ops/PRD2-COMPLETION-PLAN.md` | Current PRD2 remaining-work plan and user-confirmed completion decisions |
| `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | PRD1 acceptance progress and historical closeout evidence |
| `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md` | Current map of what Codex can test and required safe credentials/scope |
| `docs/issues/OPEN-ISSUES.md` | Current unresolved issue queue |
| `docs/DOCS-GOVERNANCE.md` | Documentation rules |
| `docs/agent-workflows/AGENT-PROTOCOL.md` | Required Codex/OpenClaw workflow |
| `docs/agent-workflows/FIX-SIGNAL.md` | Current verification request |
| `docs/agent-workflows/VERIFY-RESULT.md` | Current verification result after OpenClaw runs |

## Product And Design References

| File/Dir | Role | Notes |
|----------|------|-------|
| `docs/PRD-Phase1.md` | Product requirements and acceptance criteria | Requirements source, not current progress source |
| `docs/PRD-Phase1.pdf` | PRD PDF export | May lag Markdown |
| `docs/planning/PRD2-DISCOVERY.md` | PRD2 direction and scope discovery | Confirmed planning direction, not formal PRD |
| `docs/PRD-Phase2.md` | PRD2 formal product requirements | Draft for review |
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
| `docs/tech-ref/05-openapi-spec.yaml` | Current PRD1 API path snapshot aligned to backend routes |
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

PDF copies in `docs/tech-ref/` are export artifacts and may lag Markdown/YAML.

## Operations And Testing

| File | Current Role |
|------|--------------|
| `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md` | Executable ownership map for remaining PRD1 acceptance tests |
| `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md` | Current local evidence and external acceptance queue |
| `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md` | Final strict closeout gate and evidence procedure |
| `docs/ops/deployment-runbook.md` | Deployment reference |
| `docs/ops/evidence/` | Current runtime/deployment evidence cited by acceptance matrices and status docs |
| `docs/ops/canary-testing.md` | Canary deployment/testing reference |
| `docs/ops/test-plan.md` | Test case catalog |
| `docs/ops/test-report.md` | Historical test report; contains stale date/status and must not be used as current truth |
| `docs/ops/MVP-TEST-EXECUTION.md` | Historical E2E evidence plus pending cases; reconcile into acceptance matrix before treating as current |

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
