# PRD5 Current Gap Audit And Next-Window Plan

> Date: 2026-07-10
> UniRouter audited baseline: `81c97cb` / PR #129 on `main`
> X-Insight audited baseline: `9172633` / PR #12 on `main`
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

The current PRD5 acceptance matrix contains 32 rows:

| Classification | Count | IDs |
|---|---:|---|
| Historical accepted scope | 25 | P5-F01-F07 and P5-F13-F30 |
| Target/external integration verified | 2 | P5-F31-F32 |
| Deferred pending User/architecture decision | 2 | P5-F08-F09 |
| Deferred pending target prerequisite | 2 | P5-F10-F11 |
| Planned, screening not complete | 1 | P5-F12 |

This means 27 rows have passed the acceptance boundary defined for those rows.
It does **not** mean 27 product features have complete runtime implementations.
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
- temporary Gate A API keys 36 and 37 disabled.

The second controlled cycle is target verified only for the denied path. It does
not prove a paid upstream provider call, production readiness, or X-Insight ECS
runtime deployment.

## UniRouter-Owned Functionality Not Implemented

### Confirmed Design-Only Or Partial Rows

| Gap | Current fact | Decision/implementation needed |
|---|---|---|
| Data Exchange Control surface | P5-F28 accepted the product boundary only. No UI, API, migration, RBAC, or audit implementation exists | Define an MVP for outbound authorization, artifact ledger, delivery/re-delivery/delete audit, and receipt reconciliation. Do not copy X-Insight investigation UI into UniRouter |
| Trace search ergonomics | Existing Logs detail shows structural evidence, but PRD5 competitor work still lists request-ID and source-reference search improvements | Audit existing Logs filters/API indexes, then design exact request/source lookup and evidence-ref navigation |
| Route/failover decision visibility | Structural decision fields exist, but route/retry/failover evidence is not yet presented at competitor-comparable clarity | Audit gateway decision emission and UI read-side coverage before adding events or panels |
| X-Insight deep links | Placeholder boundary exists; no stable X-Insight import-job ID or URL contract exists | Keep blocked until X-Insight exposes durable import-job/execution IDs and stable URLs |
| Advanced payload policy modes | Runtime supports only `metadata_only` and `disabled` | Redacted/hash/reference/raw modes require a separate User decision plus encryption, key management, retention, delete/export, and audit-on-read design |
| Optional OTel/OpenInference bridge | Listed as medium-priority catch-up; no bridge is implemented | Screen only after native evidence behavior and ownership are stable; do not make it a PRD5 commitment by implication |

### Planned Or Decision-Gated Work

| ID | Current fact | Why implementation must not start automatically |
|---|---|---|
| P5-F08 read-only DB/views | Two controlled imports prove compatibility, but no read-only access is approved | Requires view scope, least-privilege role, network path, audit, retention, revocation, outage behavior, and User approval |
| P5-F09 cursor exporter/outbox | No exporter exists; two low-volume snapshots do not establish scale or availability requirements | Requires observed throughput/latency needs, cursor and retry semantics, dedupe, redaction, backpressure, producer availability, and operating owner |
| P5-F12 larger deferred backlog screening | Screening has not been completed | Scheduler V2, runtime plugins/marketplace, managed HA/IaC, infrastructure topology, hard budget blocking, runtime cache, SDKs, roles/service accounts, and SLA operations require separate product/architecture decisions |

### Target Evidence And Operations, Not Missing Local Features

- P5-F10 real OpenRouter/direct Anthropic E2E requires approved credentials,
  model slugs, target owner, cost/risk approval, and cleanup.
- P5-F11 real enterprise OIDC/SSO browser E2E requires an approved IdP tenant,
  test user, callback, target endpoints, DB audit access, and redaction review.
- `INFRA-SEC-001` is Critical and Open: the current development/test ECS security
  group exposes all TCP ports publicly. Remediation needs a separately authorized,
  rollback-tested access cutover; the in-use group must not be edited ad hoc.
- Existing provider load capacity remains unstable under the recorded 20 VU test;
  production HA/DR also remains incomplete. These are project-wide operational
  gaps and should be considered in architecture prioritization.
- Several frontend fixes are Local verified but still need an authorized target
  browser check; they are not unimplemented source features.

## X-Insight Coordination Work Not Complete

| Gap | Verified current fact | Ownership/boundary |
|---|---|---|
| X-Insight ECS runtime | X-Insight `README.md` says first ECS deployment is in progress; network isolation, data disk, and PostgreSQL preparation are reported, but source transfer is blocked and no target-runtime claim exists | X-Insight owns deployment. This does not block independent UniRouter product work |
| Durable receipt/import-job API | Current X-Insight receipt is a sanitized sidecar artifact; no durable `import_job_id` or deep link is emitted | X-Insight owns the import-job/API model. UniRouter may later reconcile receipts but must not fabricate IDs |
| LiteLLM/NewAPI/one-api adapters | Current X-Insight source contains a dedicated UniRouter adapter only. Fixture/schema support is not equivalent to a production adapter | X-Insight owns the public/open-source adapter strategy. It is not a UniRouter PRD5 blocker |
| Stable execution URL contract | No stable ID/URL contract is available for UniRouter UI links | Joint contract after X-Insight implements durable execution/import identity |
| Ongoing ingestion mechanism | Controlled snapshots pass twice; read-only views and exporter/outbox remain unapproved | Joint architecture decision only if snapshot cadence, scale, or operational needs prove it necessary |
| Artifact retention | First snapshot deletion is due 2026-07-17; the second snapshot retention period is not recorded | Joint operational reconciliation; no data-scope expansion follows from it |

UniRouter does not need to wait for X-Insight. It can continue P5-F12 screening,
the architecture/product audit, Data Exchange Control MVP design, trace search,
and route/failover evidence work independently. Only deep links, receipt API
automation, and any continuous ingestion mechanism require a cross-project
contract.

## Recommended Priority

1. **P0 architecture/security plan:** audit deployment architecture and prepare
   the `INFRA-SEC-001` cutover design. Actual ECS mutation remains separately
   authorized.
2. **P1 repository-wide architecture and product audit:** map backend composition,
   gateway request/evidence flow, admin API ownership, data model/migrations,
   frontend information architecture, and deployment/release design. Separate
   confirmed defects from refactoring hypotheses.
3. **P1 P5-F12 screening:** make an explicit accept/defer/reject decision for each
   large backlog item, with product value, dependency, risk, and acceptance evidence.
4. **P1 product implementation choice:** decide the smallest useful Data Exchange
   Control MVP and whether trace search or route/failover visibility should precede
   it. Do not implement all three before the audit establishes user workflows and
   existing API/index support.
5. **P2 prerequisite evidence:** run real upstream and SSO E2E only after the
   required environments and credentials are explicitly approved.
6. **P2 cross-project evolution:** add deep links or continuous ingestion only
   after X-Insight publishes stable contracts and observed demand justifies them.

## New-Window Architecture Audit Plan

The next window should start from repository facts and produce an implementation-
ready report before broad refactoring:

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
4. `docs/PRD-Phase5.md`
5. `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`
6. `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`
7. `docs/planning/PRD5-BACKLOG.md`
8. `docs/planning/PRD5-COMPETITOR-COMPARISON.md`
9. `docs/issues/OPEN-ISSUES.md`
10. `docs/agent-workflows/AGENT-PROTOCOL.md`
11. `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md`
12. X-Insight `README.md` at audited baseline `9172633`

Start the new window with the architecture/product audit and P5-F12 screening.
Do not deploy, rerun controlled requests, change credentials, expand export scope,
or mutate target infrastructure without the applicable explicit authorization.
