# PRD8 Enterprise Control Plane Implementation Plan

> Work item: `UR-PRD8-ENTERPRISE-CONTROL-PLANE`
> Date: 2026-08-02
> Owner: UniRouter product/implementation owner
> Status: Local implementation complete; PR/CI and existing-environment release separately authorized; customer-target integrations gated
> Product contract: `docs/PRD-Phase8.md`

## 1. Outcome

This plan turns the PRD8 product boundary into a dependency-ordered local
implementation, including the bounded Provider activation/release controller
and Model Call Authorization PEP. It does not make UniRouter a system of record for enterprise
people, supplier invoices or a general ledger, and it does not create Tool or
MCP Gateway functionality.

The autonomous local sequence is:

```text
UI-1..5 -> UI-PERF-1 -> DOMAIN-0
  -> ORG-1 -> ORG-2
  -> FIN-1 -> FIN-2 -> FIN-3
  -> RUNTIME-1 -> AUTHZ-1
  -> full local regression and compatibility audit
  -> User decision: ORG-3 directory target and/or FIN-4 ERP/GL target
```

`ORG-3` and `FIN-4` are deliberately outside autonomous execution. Their
vendor, field-authority, credential, deployment and target-evidence choices
cannot be truthfully decided without a real customer environment. They do not
block release of the completed product-local capabilities to the existing
UniRouter environment.

## 2. Non-negotiable compatibility invariants

Every batch must preserve:

1. current request admission, routing, retry/failover and Provider dispatch;
2. request-time price evidence, reservation, settlement and ledger finality;
3. Outbox identity and completed-execution Source bytes/hash;
4. completed-execution Feed endpoint, credential, ordering and Cursor behavior;
5. the no-default-source-instance rule;
6. product-local IAM as the request-path authorization authority;
7. the boundary that X-Insight owns cross-source investigation, Tool Gateway
   owns tool execution and MCP Gateway owns MCP protocol/session behavior; and
8. no synchronous dependency from model execution to an HCM, IdP, ERP or
   shared Connector runtime.

## 3. Completed local batches

| Batch | Local result | Evidence gate |
|---|---|---|
| UI-1 | one permanently expanded sidebar; duplicate dashboard launcher removed | navigation/App tests |
| UI-2 | Users, Projects and lifecycle evidence composed under Organization & Members; legacy routes redirect | route/RBAC/component tests |
| UI-3 | Runtime-backed Model Call Authorization is the primary workspace; historical Agent Trust evidence remains a distinct offline tab | copy/route/component tests |
| UI-4 | enterprise finance labels and truthful current-capability notices | finance page tests |
| UI-5 | lightweight sticky toolbar and bounded persistent Inspector layout | shell test and rendered smoke |
| UI-PERF-1 | route and nested-workspace lazy loading; no forced monolithic vendor chunk | current full regression 347/347 frontend tests, type-check and production build |
| DOMAIN-0 | source-neutral directory and finance contracts with no persistence or connector | pure Go contract tests |
| ORG-1 | metadata-only source health and effective-dated organization tree | migration, tenant-isolation and PostgreSQL integration evidence |
| ORG-2 | source-owned people/accounts plus fail-closed local IAM convergence | replay, tombstone, capture-gap, last-admin, tenant and race evidence |
| FIN-1 | fiscal periods, cost centers and report-only budgets | precision, maker/checker, overlap, RBAC and migration evidence |
| FIN-2 | versioned five-layer Rate Cards and immutable pricing snapshots | collision, evidence, historical non-rewrite and integration evidence |
| FIN-3 | exact allocation, close gates and adjustment/reversal journals | replay, coverage, close, reversal and wallet non-mutation evidence |
| RUNTIME-1 | Provider Pack-pinned activation gate and bounded route release lifecycle | migration, lifecycle, CAS, deterministic canary, emergency-disable and Gateway trace evidence |
| AUTHZ-1 | product-local Model Call Authorization management and request PEP | migration, deny precedence, allow intersection, scope/lifecycle, catalog-filter and Gateway trace evidence |
| Final Local gate | full backend/frontend regression, build and rendered UI review | 50/50 files, 347/347 frontend tests; full Go, integration, race, vet and browser checks |

These results are working-tree Local evidence. CI, Target and Production are
recorded only after the separately authorized delivery steps actually pass;
external directory/ERP/Provider integration remains unclaimed.

## 4. Closed batch contracts

The following contracts are retained as the acceptance record. All five
organization/finance batches and both runtime-control batches are implemented
and Local verified in the current working tree; they
are no longer remaining work. Exact evidence is linked in section 5.

### ORG-1 — directory projection storage and health

Risk: `R3` because this adds a database migration and tenant-scoped admin read
surfaces.

Implement:

- source-neutral directory-source registrations containing metadata only;
- orthogonal connection, freshness, reconciliation and coverage health;
- effective-dated legal entity, business unit, department and team projection;
- stable external identity, source revision and snapshot identity;
- full/delta application with strict organization scoping;
- explicit gaps and conflicts without fabricated people or departments; and
- read-only organization-tree and source-health service/API for authorized
  organization administrators.

Acceptance:

- migration up/down and schema constraints;
- cross-organization reads/writes rejected;
- stale, future, duplicate and conflicting revisions fail closed;
- effective-time tree reads do not rewrite history;
- no credential columns and no request-path dependency; and
- focused package, migration and admin tests pass.

### ORG-2 — people/account binding and JML convergence

Risk: `R3` because local IAM safety is involved.

Implement:

- idempotent projection application ledger and deterministic replay result;
- HCM-owned person/employment/assignment facts and IdP-owned account facts;
- person-to-local-account binding without treating API-key ownership as a
  caller identity;
- Joiner/Mover/Leaver projection states, tombstones and effective windows;
- stale-revision, resurrection and source-authority conflict rejection;
- a local IAM adapter that may suspend/revoke on proved terminal state but
  never grants a new role or privilege from stale, gapped or unapproved data;
- durable, auditable convergence outcomes and repairable failures.

Acceptance:

- semantic replay is a no-op and conflicting replay is rejected;
- full snapshot omission and explicit delta tombstone have distinct semantics;
- suspension/deprovision revokes applicable local sessions/capabilities using
  existing product-local IAM mechanisms;
- reactivation does not restore prior privileges automatically;
- no cross-tenant identity binding; and
- focused, integration and race-sensitive tests pass.

### FIN-1 — fiscal periods, cost centers and budgets

Risk: `R3` because enterprise finance persistence and RBAC are involved.

Implement:

- effective-dated legal entity/cost-center ownership references;
- fiscal calendars and period state transitions;
- versioned budget plans with maker/checker evidence;
- report-only budget default distinct from runtime spend policy;
- exact decimal-integer amounts and immutable revision history;
- actual, committed, forecast, variance and coverage read models; and
- finance-role admin APIs without changing wallet settlement semantics.

Acceptance:

- tenant isolation, precision, period overlap and approval constraints;
- budgets cannot block a request without a separate approved spend policy;
- closed periods cannot be silently reopened or rewritten;
- existing request settlement and project-budget compatibility tests pass.

### FIN-2 — enterprise rate cards and pricing snapshots

Risk: `R3` because pricing identity affects future economic evidence.

Implement:

- separately versioned provider list, supplier contract, supplier effective,
  internal showback and internal chargeback layers;
- maker/checker approval and deterministic activation simulation;
- non-overlapping effective windows per exact scope/model/meter/layer;
- component canonicalization and hash identity;
- immutable request-time pricing snapshot binding; and
- historical reads that never recalculate settled requests from a new card.

Acceptance:

- exact large integer quantities and micro-unit money vectors;
- supplier-effective rates require source evidence;
- activation collision and retroactive-rewrite attempts fail closed;
- current pricing/settlement/Feed identities remain byte-compatible.

### FIN-3 — allocation, reconciliation and close

Risk: `R3` because period close and reporting ownership are financial controls.

Implement:

- deterministic direct and rule-based allocation runs;
- explicit unallocated and conflict states;
- allocation coverage and reconciliation summaries;
- preliminary, reconciled and closed period workflow;
- adjustment/reversal journals instead of closed-history mutation;
- idempotent reruns bound to rule and source revisions; and
- export-ready internal records without selecting or emulating an ERP/GL.

Acceptance:

- allocation never creates customer debt or mutates request settlement;
- replay, conflict, close and reversal tests;
- source revision and rule revision are always visible;
- cross-period and cross-organization writes fail closed;
- no supplier invoice or GL authority is claimed.

### RUNTIME-1 — Provider activation and bounded progressive release

Risk: `R4` because this changes live route assignment inside UniRouter while
deliberately excluding credentials, endpoints, retries, billing and IAM.

Implement and acceptance:

- immutable candidate/baseline hashes, exact Provider Pack/Manifest pins and a
  CAS-protected lifecycle of draft, validated, shadow, canary, active and
  rolled_back;
- deterministic request assignment, maximum 50% canary and at most one canary
  per model;
- only channel admission, priority and weight may be overlaid; emergency
  disabled always wins and direct activation/update cannot bypass the release;
- rollback remains available after a channel epoch or emergency-disable change;
- release ref, stage and assignment identity are attached to approved trace
  metadata without altering request, settlement, Outbox, Feed or Cursor
  identity; and
- migration `000052`, package, Gateway/admin, integration and race tests pass.

### AUTHZ-1 — Model Call Authorization PEP

Risk: `R4` because this can deny model calls but must never grant authority.

Implement and acceptance:

- organization-scoped policies for API Key, project, user and Agent subjects;
- environment, requested/resolved model and Endpoint scopes, optional validity
  windows, draft/active/revoked lifecycle and optimistic revision checks;
- explicit deny wins; allow policies for each matching subject are unioned
  within the subject and intersected across subjects; absence of a policy keeps
  existing authority unchanged;
- authorization is evaluated after existing credential/project/Agent gates and
  can only narrow them; Tool/MCP authority is structurally unsupported;
- model catalog visibility uses the same policy snapshot as call admission and
  decisions emit approved trace metadata; and
- migration `000053`, unit/admin/Gateway/integration and race tests pass.

## 5. Final local verification gate — complete

After FIN-3 the implementation owner completed the following gate:

1. run focused package, migration, admin and finance tests for each batch;
2. run full Go tests, race-appropriate suites and `go vet`;
3. run all frontend tests, type-check and production build;
4. run `git diff --check` and migration-order checks;
5. audit the compatibility invariants in section 2;
6. record immutable Local evidence with exact commands and results; and
7. keep CI, Target, Production and external-integration status explicitly
   unclaimed unless separately authorized and actually executed.

Results:

- full Go tests and `go vet`: PASS;
- required local-PostgreSQL directory/finance/admin/migration integration
  selection: PASS;
- complete migration integration suite, including legacy rollback ordering
  through new migrations `000047`–`000053`: PASS;
- directory, finance, runtime-change and authorization race tests: PASS;
- frontend 50/50 files and 347/347 tests, type-check and production build:
  PASS;
- production build: 4,313 modules, no 500 kB warning, 275.35 kB / 87.11 kB
  gzip entry JavaScript;
- rendered 1440/1024/768 UI review: no document-width overflow and browser
  console 0 errors / 0 warnings; and
- exact evidence:
  `docs/ops/evidence/PRD8-ORG1-enterprise-directory-foundation-local-20260802.md`,
  `docs/ops/evidence/PRD8-ORG2-enterprise-directory-iam-convergence-local-20260802.md`,
  `docs/ops/evidence/PRD8-FIN1-FIN3-enterprise-finance-local-20260802.md`, and
  `docs/ops/evidence/PRD8-enterprise-org-finance-local-closeout-20260802.md`, and
  `docs/ops/evidence/PRD8-P7F04-P7F10-P7F12-runtime-controls-local-20260802.md`.

## 6. User decision gate

Stop autonomous implementation only when one of these becomes reachable:

| Decision | Evidence the User must have | Why it cannot be guessed |
|---|---|---|
| ORG-3 first real directory Connector | customer HCM/IdP, supported APIs/events, field authority, tenant topology and credential boundary | Feishu, DingTalk, Entra/SCIM, AD and HCM products have materially different identity and lifecycle semantics |
| FIN-4 ERP/GL export | customer accounting system, legal entities, chart of accounts, posting policy and showback/chargeback requirement | choosing an adapter or journal model early risks creating a false second ledger |

The customer-integration path therefore stops here. The Product Owner separately
authorized commit/PR/CI and release of the completed product-local UI and
functions to the existing UniRouter environment on 2026-08-02. That delivery
authorization does not select ORG-3/FIN-4, add credentials, send real Provider
test traffic, mutate Cursor/Checkpoint state, or establish Production evidence
without actual verification.
