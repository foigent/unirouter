# PRD8 Enterprise Organization, Finance, and UI Migration

> Date: 2026-08-01
> Status: Authorized product-local migration plan
> Scope: UniRouter only
> Runtime boundary: no deployment, live directory connection, credential,
> traffic, production database, Cursor, or Checkpoint mutation

Local implementation status: `UI-1` through `UI-5`, `UI-PERF-1`, `DOMAIN-0`,
`ORG-1`, `ORG-2`, `FIN-1`, `FIN-2`, and `FIN-3` are implemented and Local
verified in the current product worktree. Current local-account creation remains
the default Organization & Members surface. A real directory adapter (`ORG-3`)
and ERP/GL export (`FIN-4`) remain deferred until a customer POC identifies the
exact target and authority boundary.

## 1. Accepted Decisions

The product owner accepted the following forward design:

1. Rename `Model Execution Authority` to **Model Call Authorization**. The
   navigation entry remains visible with an explicit `Planned` status until a
   runtime-backed authorization workflow exists.
2. Enterprise HCM/directory systems own official people and organization
   structure. UniRouter stores a versioned local projection and continues to
   own project membership, product roles, model access, limits, and request
   enforcement.
3. Replace the prepaid-MaaS UI vocabulary with enterprise finance vocabulary:
   **Budgets & Cost Centers**, **Rates & Rating**, and **Allocation &
   Reconciliation**.
4. Preserve the existing wallet, reservation, settlement, model price, ledger,
   Outbox, completed-execution Feed, and URL compatibility while the enterprise
   read models are introduced.
5. Preserve current local-account creation, role assignment, status management,
   and project membership without waiting for an enterprise directory. A real
   directory adapter is selected only when a customer POC provides an exact
   source and field-authority environment.

## 2. UI To Domain Mapping

| UI surface | Product/domain owner | Backing model | Current implementation state |
|---|---|---|---|
| Organization & Members | UniRouter local IAM; external directory owns official hierarchy | effective-dated directory projection, user/account binding, project membership | local users/projects plus source-neutral organization, people/account projections and fail-closed IAM convergence are Local verified; real Connector deferred |
| Model Call Authorization | UniRouter model PEP | subject-to-model grant plus effective ceilings and revocation | offline contract evidence only; runtime management is planned |
| Shared Limits | UniRouter model PEP | organization/project/subject RPM, TPM, concurrency, and spend policies | runtime-backed for existing supported scopes |
| Budgets & Cost Centers | Finance owns planning dimensions; UniRouter owns execution spend controls | cost centers, fiscal periods, budgets, forecasts, enforcement binding | fiscal periods, cost centers, report-only budgets and exact allocated actuals are Local verified; committed/forecast remain explicitly not modeled |
| Rates & Rating | UniRouter pricing authority | versioned provider, contracted, and internal rate cards | base prices/multipliers remain the request path; five-layer enterprise Rate Cards and immutable snapshot reads are Local verified but not activated for settlement |
| Allocation & Reconciliation | UniRouter owns customer/internal ledger and reconciliation record; supplier owns raw invoice truth | immutable execution charge, captured supplier evidence, allocation rules, close state | exact allocation runs, coverage/close gates and adjustment/reversal evidence are Local verified; ERP/GL export remains unconfigured |

## 3. Organization And Identity Architecture

### 3.1 Separate entities

The implementation must not conflate:

- a `person` from HCM;
- an `organization_unit` such as a department or business unit;
- a login `account` from an IdP;
- a project membership;
- a workload, service account, or Agent identity;
- an API-key owner; and
- the principal that actually initiated a request.

API-key ownership is not proof of individual usage. Unprovable attribution must
remain `shared` or `unallocated`; it must not be guessed.

### 3.2 Field authority

| Field class | Authority |
|---|---|
| employment status, official department, manager, legal entity, cost center, effective dates | configured HCM/HRIS source |
| login account, authentication status, directory groups | configured IdP/directory source |
| UniRouter role, project membership, API key, model grant, runtime limits | UniRouter |
| Tool or MCP permissions | the corresponding Gateway, never UniRouter |

One source is authoritative for each field class. Connector precedence is
explicit and fail-closed; two sources cannot silently overwrite the same field.

### 3.3 Synchronization

The future directory adapter uses:

1. an initial full snapshot;
2. source events, webhooks, or delta watermarks for incremental updates;
3. idempotent version checks and tombstones;
4. periodic reconciliation to detect missed events;
5. explicit freshness, checkpoint, conflict, and capture-gap state; and
6. effective-dated Joiner, Mover, and Leaver transitions.

UniRouter reads the local projection on the request path. It never calls HCM,
Feishu, DingTalk, Entra ID, or AD synchronously while admitting a model request.
Directory staleness blocks new privilege grants or elevation, raises an
operator-visible alert, and does not reinterpret missing data as safe.

### 3.4 Historical attribution

An execution attribution snapshot will eventually bind stable product-local
identifiers for:

- initiating principal when proven;
- paying project/application;
- effective organization unit and cost center;
- mapping revision and effective time; and
- attribution state (`direct`, `derived`, `shared`, `unallocated`, or
  `conflict`).

Moving a person between departments does not rewrite historical execution
facts. Open periods may be restated through an auditable allocation run; closed
periods require an adjustment journal.

## 4. Enterprise Finance Architecture

### 4.1 Separate financial concepts

- A financial budget plans and reports spend. It is soft by default.
- A runtime spend policy may warn, throttle, or block model admission.
- A settlement reservation prevents an accepted request from overspending its
  allowed envelope.
- A supplier invoice remains supplier-owned evidence.
- Showback reports cost to an owner; chargeback creates an approved accounting
  export or journal and is optional per enterprise policy.

In plain terms, **showback** answers “which department/project consumed this
cost?” without creating an internal debt or accounting entry. **Chargeback**
turns an approved allocation into an ERP/general-ledger posting or internal
bill. UniRouter implements the showback data model first; chargeback remains a
customer-POC integration decision because legal entities, cost-center codes,
period rules, approval chains, and ERP targets are enterprise-specific.

The current wallet remains a compatibility and settlement control. The product
UI must not present it as the enterprise's bank account or general ledger.

### 4.2 Cost layers

The future rating model separates:

1. provider list cost;
2. contracted supplier cost;
3. billed/effective supplier cost;
4. internal showback or chargeback rate; and
5. optional external customer price only in a separately enabled commercial
   deployment profile.

Every settled request retains the exact pricing snapshot used for its financial
decision. Later rate changes do not rewrite it.

### 4.3 Enterprise controls

Future persisted changes require:

- legal entity, business unit, department, and cost-center dimensions;
- fiscal period and currency;
- rate-card version and effective interval;
- contract or purchase-order reference without embedding credentials;
- maker/checker approval for rate activation, allocation, and financial
  adjustments;
- simulation before activation;
- immutable audit and explicit reversal rather than silent mutation; and
- period states such as `open`, `preliminary`, `reconciled`, and `closed`.

### 4.4 Primary UI metrics

The finance overview starts with three primary metrics:

1. finalized effective AI cost, with estimates shown separately;
2. allocation coverage = allocated eligible finalized cost / eligible finalized
   cost; and
3. forecast variance = forecast period-end cost - approved budget.

Drivers include cost per successful execution, model mix, retry/failure waste,
cache savings, rate-card coverage, unallocated cost, and supplier-evidence
finality. Targets are not frozen until a trustworthy historical baseline exists.

## 5. Compatibility Invariants

This migration must preserve:

- existing API routes and role checks;
- legacy frontend URLs as redirects or compatibility views;
- model request, project, API-key, wallet, reservation, settlement, and ledger
  behavior;
- immutable request-time pricing evidence;
- Outbox identity and completed-execution Source bytes/hash;
- Feed endpoint, credential, event identity, ordering, and Cursor behavior;
- the no-default-source-instance rule; and
- X-Insight, Tool Gateway, and MCP Gateway ownership boundaries.

No UI rename is allowed to claim that a planned backend contract is already
runtime-backed.

## 6. Code Change Matrix And Sequence

| Batch | UI change | Architecture/code change | Compatibility and proof |
|---|---|---|---|
| UI-1 | all secondary menus permanently expanded; remove duplicate dashboard task launcher | controlled navigation state only | frontend navigation and App-shell tests |
| UI-2 | consolidate Users, Projects, and lifecycle evidence under Organization & Members | composed product-local workspace; legacy URLs redirect to exact tabs | route/RBAC and component tests |
| UI-3 | Model Call Authorization marked Planned | rename and boundary copy only; no new runtime authority | offline-contract tests and no-mutation statement |
| UI-4 | enterprise finance labels and current-capability notices | retain wallet/model-price/reconciliation APIs unchanged | existing finance tests plus copy assertions |
| UI-5 | lightweight sticky global utility bar and consistent desktop Inspector sizing | global search/environment/notification/account only; no duplicate navigation or business actions | App-shell structure test, responsive CSS review and rendered-page acceptance |
| UI-PERF-1 | reduce initial UI payload after visual/interaction acceptance | route-level lazy loading first; measured vendor chunking second; loading/error boundaries remain explicit | compare against 2,235.70 kB minified / 625.78 kB gzip baseline, full frontend tests, build and authenticated route smoke |
| DOMAIN-0 | no new runtime surface | `internal/orgdirectory` and `internal/enterprisefinance` freeze source-neutral entity separation, orthogonal health, effective attribution, budget/policy separation, rate layers, allocation and close invariants | pure Go contract tests; no persistence, credential or connector |
| ORG-1 | directory source health and organization tree | source-neutral, effective-dated projection contract; no vendor credentials | schema/contract tests and tenant isolation |
| ORG-2 | people/account binding and JML convergence | idempotent projection worker and local IAM adapter | replay, stale revision, tombstone, suspension, and fail-closed tests |
| ORG-3 | first real directory Connector | select one target only when a customer source is known | sandbox/target evidence; no request-path dependency |
| FIN-1 | budgets and cost centers | fiscal periods, cost-center identities, budget vs enforcement separation | migration, precision, RBAC, audit, and tenant-isolation tests |
| FIN-2 | enterprise rate cards | versioned cost layers, approval, activation simulation, request snapshot binding | pricing vectors and historical non-rewrite tests |
| FIN-3 | allocation and close | allocation rules, unallocated/conflict states, close/reversal workflow | reconciliation, replay, close, and export tests |
| FIN-4 | ERP/GL export | customer-selected adapter; no second general ledger | contract and target-environment evidence |

Dependency order is `UI-1..5 -> UI acceptance -> UI-PERF-1 -> ORG-1..2 ->
FIN-1..3`; `ORG-3` and `FIN-4` wait for a real enterprise POC because their
provider/ERP choice materially changes the implementation.

### 6.1 Current local evidence

- Navigation groups are controlled-open and their disclosure chevrons are
  removed; the navigation column owns its own vertical scroll.
- The sticky global toolbar contains only request-log search, environment,
  notifications, and account access. It does not duplicate task navigation or
  host page-local business actions.
- Desktop Inspectors use a bounded 400-480px detail column. The request-log
  explorer stays three-column at 1440px, changes to filter-above plus
  list-and-Inspector at 1024px, and does not require a second detail-open click.
- The dashboard no longer renders a duplicate copy of the sidebar task map.
- `/organization-members` is the canonical composed workspace and defaults to
  current local-account management. `/users`, `/project-management`, and
  `/member-lifecycle` preserve old links through exact account, project, and
  lifecycle redirects.
- `/wallet`, `/model-prices`, and `/reconciliation` retain their APIs and
  runtime behavior while exposing truthful current-capability notices.
- `internal/orgdirectory` refuses to infer a person from API-key ownership,
  separates HCM and IdP field authority, and blocks new privilege elevation on
  stale, unreconciled, conflicting or gapped projections.
- `internal/enterprisefinance` keeps budgets report-only by default, requires a
  separate runtime spend policy, separates five rate layers, binds immutable
  request pricing snapshots, and prevents allocation from creating debt or
  mutating settlement.
- Route-level lazy loading reduced the production entry JavaScript from
  `2,235.70 kB` minified / `625.78 kB` gzip to the current `273.66 kB`
  minified / `86.88 kB` gzip (about 86.1% less gzip). The largest minified
  JavaScript chunk is the Dashboard at `376.63 kB` / `100.87 kB` gzip; the
  largest gzip JavaScript payload is `103.75 kB`. The build emits no 500 kB
  warning, and measured output does not justify a forced monolithic vendor
  chunk.
- The production Manifest's current reproducible initial static dependency
  budgets are: login 20 JS / 10 CSS assets (`359.47 kB` / `56.44 kB` gzip),
  dashboard 26 JS / 11 CSS assets (`348.75 kB` / `37.85 kB` gzip), and
  Organization & Members' default account view 34 JS / 18 CSS assets
  (`442.67 kB` / `69.07 kB` gzip).
  These are emitted static-import closures, not claims about browser network
  request counts under caching, preload, or HTTP multiplexing. Project and
  member-lifecycle tabs remain a second lazy boundary outside the default view.
- Local rendered-browser smoke passed the login surface and authenticated
  platform overview, request-log Inspector, Organization & Members, enterprise
  budget, enterprise Rate Card, and allocation/close routes using synthetic,
  non-production API data. At 1440, 1024 and 768 pixels the document did not
  overflow the viewport; browser console errors and warnings were both zero.
  The subsequent bounded runtime-control batch brings the current frontend
  regression to 50/50 files and 347/347 tests; type-check and production build
  pass. The enterprise organization/finance closeout itself remains the
  immutable 49-file/345-test checkpoint recorded in its evidence file.
- Source-owned request, pricing, settlement, Outbox, Feed, and Cursor code was
  not changed by `UI-1` through `UI-5`, `UI-PERF-1`, or `DOMAIN-0`.
- `ORG-1` is Local verified through migration `000047`: metadata-only directory
  sources, orthogonal health, immutable full/delta snapshots, exact predecessor
  revisions, effective-dated organization trees, duplicate/no-op replay,
  conflict evidence and tenant-scoped read APIs. The Organization & Members
  directory tab reads this local state and still fabricates no source or unit.
  Exact evidence is
  `docs/ops/evidence/PRD8-ORG1-enterprise-directory-foundation-local-20260802.md`.
- `ORG-2` is Local verified through migration `000048`: HCM-owned people,
  IdP-owned accounts, tenant-safe binding, replay-safe snapshots and terminal
  state convergence that can revoke but never grant or silently restore local
  privilege. Exact evidence is
  `docs/ops/evidence/PRD8-ORG2-enterprise-directory-iam-convergence-local-20260802.md`.
- `FIN-1` through `FIN-3` are Local verified through migrations `000049` to
  `000051`: periods, cost centers, report-only budgets, five-layer Rate Cards,
  immutable pricing evidence, exact allocation, close gates and adjustment /
  reversal journals. Existing settlement remains unchanged and GL export is
  `not_configured`. Exact evidence is
  `docs/ops/evidence/PRD8-FIN1-FIN3-enterprise-finance-local-20260802.md`.
- The complete autonomous Local sequence and stop gate are recorded in
  `docs/ops/evidence/PRD8-enterprise-org-finance-local-closeout-20260802.md`.

## 7. Decisions Deferred Without Blocking Current Work

Two decisions are intentionally deferred:

1. the first customer-POC directory target (for example Entra/SCIM, Feishu,
   DingTalk, or an HCM product); and
2. whether a specific customer needs more than showback and therefore requires
   an ERP-backed chargeback export.

Neither decision blocks local-account management, the product-local UI
migration, or the source-neutral domain contracts. They must be made from a
real target environment, not from a generic connector marketplace assumption.
