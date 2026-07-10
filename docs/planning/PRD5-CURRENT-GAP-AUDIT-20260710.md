# PRD5 Current Gap Audit And Next-Window Plan

> Date: 2026-07-10
> UniRouter audited baseline: `95d1da9` / PR #133 on `main`
> X-Insight re-audited baseline: `97d58e7` on `main` (2026-07-11)
> Purpose: separate completed PRD5 acceptance, UniRouter-owned implementation
> gaps, X-Insight coordination gaps, and target-evidence prerequisites before a
> repository-wide architecture and product optimization pass.

## Boundary

- PRD4 remains complete for its confirmed scope. This audit does not reopen it.
- Canary, Sentinel, and Atlas are not mandatory serial gates. Codex is the
  single execution owner; independent review remains optional under Agent
  Protocol v2.
- The shared cooperation folder is communication and sanitized evidence only.
  It is not an authoritative source-code repository and must not hold secrets,
  DSNs, raw prompts/responses, raw IPs, or raw upstream errors.
- Local, CI, target, production, and external-integration evidence remain
  separate status levels.

## Matrix Accounting

The current PRD5 acceptance matrix contains 36 rows:

| Classification | Count | IDs |
|---|---:|---|
| Historical accepted scope | 25 | P5-F01-F07 and P5-F13-F30 |
| Target/external integration verified | 5 | P5-F31-F34 and P5-F36 |
| Deferred pending User/architecture decision | 2 | P5-F08-F09 |
| Deferred pending target prerequisite | 2 | P5-F10-F11 |
| Local verified screening complete | 1 | P5-F12 |
| CI verified UniRouter-owned functionality | 1 | P5-F35 |

This means 32 rows have passed the acceptance boundary defined for those rows,
including P5-F12 screening, three Batch 1Z target rows, and one CI-verified row.
It does **not** mean 32 product features have complete production implementations.
Several accepted rows intentionally delivered contracts, runbooks, or product
boundaries only.

## Completed PRD5 Runtime And Integration Work

The following are implemented and have repository evidence:

- nullable structural execution fields and migration `000018`;
- gateway-owned trace/span/source/capture/decision/actor/session evidence writes;
- bounded upstream request-ID capture and explicit capture-gap semantics;
- metadata-only or disabled payload evidence modes, with raw payload capture off;
- trusted internal evidence-context intake, default off;
- request-level evidence quality and structural tags in the existing Logs detail;
- sanitized, exact-request snapshot generation, approval manifests, exchange
  identity, handoff notes, and controlled-run authorization;
- Chat and Embeddings API-key model-permission parity before billing/provider work;
- two controlled UniRouter to X-Insight snapshot/import/persist/idempotent-readback
  loops, including the corrected denied path;
- temporary Gate A API keys 36 and 37 disabled;
- structural Logs search, route/failover summary, Data Exchange Control, and
  redacted runtime topology implemented and integrated;
- target migration/deployment, canary runtime parity, selected browser evidence,
  and `INFRA-SEC-001` cutover recorded in Batch 1Z.

The second controlled cycle is target verified only for the denied path. It does
not prove a paid upstream provider call, production readiness, or X-Insight ECS
runtime deployment.

## UniRouter-Owned Implementation And Remaining Evidence

### Confirmed Implemented Or Gated Rows

| Gap | Current fact | Decision/implementation needed |
|---|---|---|
| Data Exchange Control surface | Migration/API/UI/RBAC/audit and target browser evidence are complete in Batch 1Y/1Z; the answered X-Insight v1 contract is reconciled | Migration down remains unexecuted; unimplemented durable job/deep-link/automated receipt contracts remain external and are locked off |
| Trace search ergonomics | Exact/global structural search and target Logs filter evidence are complete | Keep regression coverage; no broader export follows automatically |
| Route/failover decision visibility | UI summary and CI regression are complete | Open a concrete target request detail before promotion from CI verified to Target verified |
| X-Insight deep links | Placeholder boundary exists; no stable X-Insight import-job ID or URL contract exists | Keep blocked until X-Insight exposes durable import-job/execution IDs and stable URLs |
| Advanced payload policy modes | Runtime supports only `metadata_only` and `disabled` | Redacted/hash/reference/raw modes require a separate User decision plus encryption, key management, retention, delete/export, and audit-on-read design |
| Optional OTel/OpenInference bridge | Listed as medium-priority catch-up; no bridge is implemented | Screen only after native evidence behavior and ownership are stable; do not make it a PRD5 commitment by implication |

### Planned Or Decision-Gated Work

| ID | Current fact | Why implementation must not start automatically |
|---|---|---|
| P5-F08 read-only DB/views | Two controlled imports prove compatibility, but no read-only access is approved | Requires view scope, least-privilege role, network path, audit, retention, revocation, outage behavior, and User approval |
| P5-F09 cursor exporter/outbox | No exporter exists; two low-volume snapshots do not establish scale or availability requirements | Requires observed throughput/latency needs, cursor and retry semantics, dedupe, redaction, backpressure, producer availability, and operating owner |
| P5-F12 larger deferred backlog screening | Screening is local verified in `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md` | Full Scheduler V2, runtime plugins/marketplace, managed HA/IaC, hard budget blocking, runtime cache, SDKs, roles/service accounts, and SLA operations remain separate scoped follow-ups |

### Target Evidence And Operations, Not Missing Local Features

- P5-F10 real OpenRouter/direct Anthropic E2E requires approved credentials,
  model slugs, target owner, cost/risk approval, and cleanup.
- P5-F11 real enterprise OIDC/SSO browser E2E requires an approved IdP tenant,
  test user, callback, target endpoints, DB audit access, and redaction review.
- `INFRA-SEC-001` is closed by the Batch 1Z rollback-tested least-privilege
  security-group cutover. Compose host bindings remain a defense-in-depth follow-up.
- Existing provider load capacity remains unstable under the recorded 20 VU test;
  production HA/DR also remains incomplete. These are project-wide operational
  gaps and should be considered in architecture prioritization.
- Route/failover detail remains CI verified because no concrete target request
  detail was opened; this is target evidence debt, not missing source behavior.

## X-Insight Coordination Work Not Complete

| Gap | Verified current fact | Ownership/boundary |
|---|---|---|
| X-Insight ECS runtime | X-Insight's final source-backed report verifies exact release artifact `b9e5811`, health/readiness, migrations 000001-000009, synthetic insert/idempotent rerun/readback, restart preservation, and loopback isolation | Target verified for the named fixture/synthetic-only non-production host; not Production verified and not a public API claim |
| Durable receipt/import-job API | Current X-Insight receipt is a sanitized sidecar artifact; no durable `import_job_id` or deep link is emitted | X-Insight owns the import-job/API model. UniRouter may later reconcile receipts but must not fabricate IDs |
| LiteLLM/NewAPI/one-api adapters | Current X-Insight source contains a dedicated UniRouter adapter only. Fixture/schema support is not equivalent to a production adapter | X-Insight owns the public/open-source adapter strategy. It is not a UniRouter PRD5 blocker |
| Stable execution URL contract | No stable ID/URL contract is available for UniRouter UI links | Joint contract after X-Insight implements durable execution/import identity |
| Ongoing ingestion mechanism | Controlled snapshots pass twice; read-only views and exporter/outbox remain unapproved | Joint architecture decision only if snapshot cadence, scale, or operational needs prove it necessary |
| Artifact retention | First snapshot deletion is due 2026-07-17; the second snapshot retention period is not recorded | Joint operational reconciliation; no data-scope expansion follows from it |

UniRouter does not need to wait for X-Insight. P5-F12 screening and the
repository-wide audit are now Local verified in
`docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`. UniRouter can
now treats Data Exchange Control MVP, trace search, and redacted current-process
topology as Target verified; route/failover visibility is CI verified. Evidence
is `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`.
Only durable deep links/jobs, receipt API automation/authenticity, downstream
retention/deletion, public adapters, and continuous ingestion require a
cross-project contract.

## Recommended Priority

1. **P0 architecture/security:** `INFRA-SEC-001` cutover is Target verified in
   Batch 1Z. Loopback-only or removed internal host publication remains P2
   defense-in-depth work.
2. **P1 repository-wide architecture and product audit:** completed in
   `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`; confirmed
   findings and hypotheses are separated there.
3. **P1 P5-F12 screening:** Local verified in the same evidence file. Full large
   backlog items remain behind separate scoped decisions.
4. **P1 UniRouter product implementation:** Data Exchange Control MVP, trace
   search, and redacted topology are Target verified; route/failover visibility
   is CI verified. Migration down and concrete target route detail remain.
5. **P2 prerequisite evidence:** run real upstream and SSO E2E only after the
   required environments and credentials are explicitly approved.
6. **P2 cross-project evolution:** answer the consolidated shared-folder runtime
   contract request before deep links, automated receipts, or continuous ingestion.
   The request is now Answered with explicit not-implemented values; those
   capabilities remain disabled until a later implemented versioned contract is
   accepted.

## Architecture Audit Plan Status

The architecture audit plan below was executed in Batch 1X and produced
`docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`. Keep it here as
the checklist for any follow-up audit or independent review:

1. **Truth and dependency map:** reconcile current docs, module boundaries,
   routes, schemas, frontend pages, tests, CI, and deployment topology.
2. **Backend architecture:** inspect composition/root wiring, gateway admission to
   relay/billing/provider ownership, duplicated policy checks, admin handler/module
   boundaries, transaction/error semantics, and test seams.
3. **Data and evidence architecture:** inspect migrations, indexes, retention,
   trace/log consistency, decision/source references, snapshot ownership, and
   future exchange-control persistence needs.
4. **Frontend product architecture:** inspect navigation by role, API contract
   parity, Logs/trace workflows, dense operational UI quality, responsive behavior,
   and Local-verified items still lacking target browser evidence.
5. **Deployment and security:** inspect immutable-release feasibility, Compose
   exposure, secret lifecycle, rollback, monitoring, capacity, backup/restore,
   and the evidence needed for a safe security-group cutover.
6. **Prioritized output:** list P0-P3 findings with file/line evidence, product
   impact, dependency, proposed batch, risk level, and required local/CI/target
   acceptance evidence. Mark uncertain items as hypotheses until verified.

The audit is allowed to identify issues outside PRD5, but it must route them into
the active issue/backlog system rather than silently reopening PRD1-PRD4 scope.

## New-Window First Read

1. `docs/PROJECT-HANDOFF.md`
2. `docs/ops/CURRENT-STATUS.md`
3. `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md`
4. `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`
5. `docs/PRD-Phase5.md`
6. `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
7. `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`
8. `docs/planning/PRD5-BACKLOG.md`
9. `docs/planning/PRD5-COMPETITOR-COMPARISON.md`
10. `docs/issues/OPEN-ISSUES.md`
11. `docs/agent-workflows/AGENT-PROTOCOL.md`
12. `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md`
13. X-Insight `docs/architecture/runtime-contract-capability-status.md` at
    re-audited baseline `97d58e7`

Continue from the runtime-contract reconciliation: obtain required repository
CI; keep migration rollback, concrete route-detail, X-Insight future contracts,
target-environment items, and deferred P5-F12 subitems separate.
Do not deploy, rerun controlled requests, change credentials, expand export scope,
or mutate target infrastructure without the applicable explicit authorization.
