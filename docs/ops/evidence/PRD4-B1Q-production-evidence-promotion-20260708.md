# PRD4 Batch 1Q Production Evidence Promotion

Date: 2026-07-08
Scope: `P4-F11` Production Evidence Promotion
Change type: documentation and evidence classification only

## Purpose

This note classifies the target-environment evidence that currently exists for
PRD4 and decides whether any PRD4 matrix row can be promoted to `Production
verified`.

Input evidence:

- `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`
- `docs/agent-workflows/VERIFY-RESULT.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`

## Promotion Rule

`Production verified` requires target-environment evidence with:

- date and environment;
- commit or deploy artifact;
- credential boundary;
- command or manual path;
- expected result;
- actual result;
- rollback or cleanup note;
- no unresolved blocker for the same behavior on the same target artifact.

Local-only, mock-only, partial, stale, or pre-fix evidence is useful but does not
promote a matrix row to `Production verified`.

## Current Decision

No PRD4 matrix row is promoted to `Production verified` in Batch 1Q.

Reason: the final target report is valuable target-smoke evidence for the PRD4
Batch 1N deployed state, but it also found issues that were fixed later in Batch
1O and found runtime/build-chain gaps that remain open as `PRD4-RUNTIME-002`.
Therefore it is not a clean production-verification artifact for the current
post-1P repo state.

## Evidence Classification

| Area | Evidence Available | Classification | Reason |
|------|--------------------|----------------|--------|
| `P4-F02` Usage And Cost Attribution | Final report verifies `/api/v1/usage/analytics`, `scope=my`, `group_by=alias`, analytics/logs consistency, and model/cost fields | Target smoke evidence only | Evidence is from Batch 1N target state and does not cover all owner/key/project/time permutations |
| `P4-F04` Flow/Event/Trace Foundation | Final report verifies call logs, six trace events, trace lifecycle, and trace metadata without sensitive fields | Target smoke evidence only | Evidence is representative but not a sustained production trace-storage/storage-policy verification |
| `P4-F06` Status Center And Runtime Observability | Final report verifies `/api/v1/runtime/status` and CPU/disk/network response availability | Partial target smoke evidence only | Runtime/build-chain findings remain open: missing runtime env, version/commit injection, container health marker, and test DB cleanup |
| `P4-F07` External Monitoring Integration | Final report verifies Prometheus and `/metrics` availability | Partial target smoke evidence only | Grafana URL is misconfigured in the target environment, so the integration is not production verified |
| `P4-F10` Auth UX Hardening | Final report verifies `/login` page availability | Page availability evidence only | It does not specifically verify password visibility controls in the target browser |
| `P4-F12` Security, Privacy, And Redaction Guardrails | Batch 1O fixed final-report security/API findings and Canary passed 8/8 | Source/Canary verified only | No target redeploy evidence for post-1O behavior is recorded here |
| `P4-F08` Real Upstream Evidence Harness | Batch 1H harness accepted | Deferred to PRD5 | Real target execution is out of current PRD4 because no approved real environment exists |
| `P4-F09` Real SSO Evidence Harness | Batch 1I harness accepted | Deferred to PRD5 | Real target execution is out of current PRD4 because no approved real environment exists |

## Remaining Production Verification Gaps

`PRD4-RUNTIME-002` remains the main blocker for runtime production promotion:

- Go standard-library vulnerabilities require a newer Go patch release or build
  image;
- target Grafana URL is misconfigured;
- runtime version/commit are not injected;
- `RUNTIME_ENVIRONMENT` is unset;
- `uniroute-api` lacks a container health marker;
- target test database cleanup remains pending Atlas.

The following remain outside current PRD4 production promotion:

- real upstream target E2E: `PRD5-UPSTREAM-001`;
- real SSO target E2E: `PRD5-SSO-001`;
- managed HA/IaC productization: `PRD5-DEPLOY-001`;
- runtime plugin platform and marketplace: `PRD5-PLUGIN-001` /
  `PRD5-MARKET-001`.

## Acceptance Boundary

Batch 1Q closes the PRD4 `P4-F11` planning row by recording the production
evidence decision. It does not:

- change Go, frontend, migrations, API routes, deployment scripts, runtime
  behavior, or evidence harness code;
- mark any row `Production verified`;
- resolve `PRD4-RUNTIME-002`, `PRD4-INFRA-001`, `PRD4-DEPLOY-001`, or
  `PRD4-PLUGIN-001`;
- reopen `P4-F08` or `P4-F09`.
