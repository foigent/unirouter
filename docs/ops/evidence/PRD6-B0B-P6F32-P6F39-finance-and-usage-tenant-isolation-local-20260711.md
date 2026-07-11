# PRD6 Batch 0B P6-F32/P6-F39 Finance And Usage Tenant Isolation — Local Evidence

> Date: 2026-07-11
> Base: `2494bf3b31d8b9c66f74f31e7c0da803ee1cbb3c`
> Branch: `codex/prd6-p6f32-finance-tenant`
> Evidence level: CI verified and merged by PR #145 as `609385f`; post-merge main CI passed; no deployment is claimed

## Confirmed Pre-Fix Findings

The reviewed base had several administrator paths whose route-level role check
did not establish an organization boundary:

- wallet reads, top-ups and balance adjustments selected the first wallet;
- transaction and grant lists/exports were global, while grant creation trusted
  a body organization or fell back to the first organization;
- reconciliation count, rows, summary and the derived estimated-usage event
  used an unconstrained global query;
- request-log lists were global when a project filter was omitted, and detail
  lookup selected by global ID before authorization;
- usage analytics and internal status could become global for an organization-
  zero identity, dashboard channel activity aggregated every tenant, and runtime
  traffic was instance-global.

These were source-reachable cross-tenant confidentiality/integrity defects, not
UI-labeling concerns. P6-F32 covers the finance/reconciliation boundary. P6-F39
records the adjacent request-log and usage-derived read boundary separately so
the finance fix is not misreported as closing every permission surface.

## Repository Change

- one organization resolver now binds every non-platform actor to the positive
  organization in its authenticated identity, rejects an explicit foreign
  organization with `403`, rejects invalid identifiers with `400`, and has no
  database-order fallback;
- a platform administrator defaults to its authenticated organization and may
  select another organization only with an explicit positive `org_id`;
- wallet, top-up, adjustment, transaction list/export, grant list/create and
  reconciliation count/rows/summary are constrained by that resolved
  organization;
- wallet mutations use an organization-specific row, record authoritative
  `balance_after`, and return `404` when that organization has no wallet;
- request-log list/detail/trace queries include the organization predicate in
  the final SQL; a foreign and nonexistent detail identifier both return `404`;
- usage analytics, internal status, dashboard usage/channel activity and
  runtime traffic are constrained to the resolved organization;
- the wallet CSV uses the standard CSV writer, preserves NULL balances as
  unknown, and neutralizes spreadsheet formula prefixes. Usage-analytics CSV
  group labels/keys receive the same formula-prefix protection;
- wallet capability flags now reflect the backend-authorized billing role, and
  typed frontend calls accept an optional explicit organization for the
  platform workflow.

Runtime host, database and Redis health remain instance-level; only the traffic
aggregate is tenant scoped. No target environment, migration, credential,
provider request, external message, data-export destination or X-Insight
contract changed.

## Migrated Signed-JWT Matrix

`TestP6F32FinanceAndUsageTenantIsolationIntegration` starts an empty PostgreSQL
database, applies every migration through `000020`, seeds two organizations and
calls handlers through `AdminAuth` plus the production role middleware. The
primary caller is the second organization's billing administrator so the old
"first wallet" behavior cannot pass accidentally.

The matrix proves:

1. wallet, grants, transactions, CSV, reconciliation, logs, analytics,
   internal status, dashboard and runtime traffic default to the caller's JWT
   organization;
2. response bodies, counts, token/cost totals and incident rows exclude the
   first organization's victim markers;
3. explicit foreign read filters and finance writes return `403` before any
   wallet, grant, transaction, audit or notification side effect;
4. foreign and missing log detail/trace IDs both return `404`, while own detail
   and trace remain readable; same-organization non-member project IDs are also
   non-enumerating for project-scoped actors;
5. same-tenant top-up, adjustment and grant affect only the caller organization
   and create the expected same-tenant audit rows;
6. a platform administrator defaults to its own organization and can explicitly
   select the second organization;
7. a non-platform JWT with organization `0` fails closed;
8. the legacy global grant-idempotency index cannot make a collision disclose
   the foreign grant ID; full cross-tenant key independence remains P6-F17;
9. historical grants with a NULL client-request ID remain listable;
10. CSV parses correctly, excludes victim rows and prefixes a formula-like cell.

`WalletPage.test.tsx` separately proves that backend-authorized billing actions
are visible and that controls remain hidden when those capability flags are
absent.

## Local Verification

The following commands pass on the final local behavior:

```text
go test ./internal/admin -run TestP6F32FinanceAndUsageTenantIsolationIntegration -count=1
go test -tags=integration ./internal/admin -run 'Test(TopUp|CreateGrant|GetWallet|BalanceAdjust).*Integration' -count=1
go test ./internal/admin
go test ./...
go test -race ./...
go vet ./...
npm --prefix frontend test -- --run
npm --prefix frontend run type-check
npm --prefix frontend run build
git diff --check
```

Results: the migrated signed-JWT matrix, all backend packages, race detector and
vet pass; the frontend suite passes 26 files / 128 tests; type-check and the
production build pass. The build retains the existing chunk-size warning. The
macOS race link step emits a non-failing `LC_DYSYMTAB` warning. PR #145
implementation head `3ee6a41048ae143ce887a0c755a8602f6f786a40` passed
`Backend Tests`, `Frontend Build` (including 26 files / 128 tests) and `Docker
Build` in required CI run `29135964946`. PR #145 was squash-merged as
`609385f663cee883ad5965facb25ce4185d8b89a`; post-merge main CI run
`29136296782` passed all three required jobs.

The full legacy `-tags=integration` admin suite is not used as green evidence:
its `SetupTestDB` path depends on a separately pre-provisioned shared schema and
the local database lacks later project/audit/notification columns and tables,
so unrelated API-key/channel/user cases fail. The focused finance-tagged cases
affected by this patch now receive an authenticated organization and pass. The
environment-coupled full-suite defect is tracked separately as
`P6-TEST-DB-001`; it is not hidden by the default test result.

## Explicit Remaining Boundaries

- `grants.client_request_id` still has a global database uniqueness index. A
  tenant-scoped lookup prevents ID disclosure, but compound tenant/payload
  idempotency, migration and mixed-version writer safety remain P6-F17.
- Current grant posting still increases `wallets.balance`; the frozen PRD1
  contract says grants increase `granted_balance`. Wallet-pool semantics,
  reservation/finalization invariants and durable audit coupling remain P6-F18
  and must not be declared correct from this isolation patch.
- reconciliation is still a GET that can emit a same-tenant notification when
  the scoped estimated-usage ratio is high. Removing that read-side effect is
  separate operator-UX/durability work.
- project-scoped roles are tenant confined but the dashboard's organization-
  versus project-granularity policy is still a separate permission decision.
- no migration, target cutover, target evidence, deployment or production
  verification was performed. PR CI does not promote this row to Target or
  Production verified.
