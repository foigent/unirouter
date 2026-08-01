# PRD8 FIN-1 Through FIN-3 Enterprise Finance — Local Evidence

> Work item: `UR-PRD8-ENTERPRISE-ORG-FINANCE-LOCAL`
> Batches: `FIN-1`, `FIN-2`, `FIN-3`
> Date: 2026-08-02
> Status: Local verified
> Risk: R3 finance persistence, approval and close controls

## Scope

The three local finance batches add an enterprise control and reporting layer
without relabeling UniRouter's existing settlement wallet as a bank account or
general ledger.

### FIN-1 — budgets and cost centers

- non-overlapping fiscal periods with `open`, `preliminary`, `reconciled` and
  irreversible `closed` states;
- effective-dated, source-revision-bound cost-center versions;
- exact integer money, versioned budget plans and maker/checker approval;
- `report_only` as the mandatory budget runtime effect; and
- budget-position reads with exact allocated actuals and explicit
  `not_modeled` committed/forecast values.

### FIN-2 — enterprise rate cards

- five separate economic layers: provider list, supplier contract, supplier
  billed-effective, internal showback and internal chargeback;
- immutable card/component versions, exact quantities and money, approval and
  activation states, evidence requirements and effective-window conflicts;
- deterministic product-local component/snapshot identities; and
- read-only request pricing-snapshot lookup without a public producer endpoint.

The existing request price and organization-multiplier path remains active and
unchanged. Enterprise Rate Cards are not yet connected to request settlement.

### FIN-3 — allocation, reconciliation and close

- immutable, source-revision and rule-revision-bound allocation runs;
- request-level `direct`, `rule_based`, `unallocated` and `conflict` outcomes;
- complete-period Source coverage, exact Source/allocated totals and close
  readiness;
- closed-period adjustment/reversal journals with maker/checker approval; and
- explicit `ledger_mutation=none`, `customer_debt_action=none` and
  `gl_export_state=not_configured` authority boundaries.

## Main artifacts

- `migrations/000049_prd8_enterprise_finance_budgeting.{up,down}.sql`
- `migrations/000050_prd8_enterprise_finance_rate_cards.{up,down}.sql`
- `migrations/000051_prd8_enterprise_finance_allocation_close.{up,down}.sql`
- `internal/enterprisefinance/`
- `internal/admin/enterprise_finance_handlers.go`
- `cmd/uniroute/enterprise_finance_routes.go`
- `frontend/src/pages/EnterpriseFinanceWorkspaces.tsx`
- `frontend/src/pages/WalletPage.tsx`
- `frontend/src/pages/ModelBillingPage.tsx`
- `frontend/src/pages/ReconciliationPage.tsx`
- `docs/tech-ref/05-openapi-spec.yaml`

## Verified invariants

1. Every write and read is organization-scoped; cross-tenant identities,
   periods, cards, allocations and journals fail closed.
2. Money and counters remain exact decimal integers; browser amount parsing
   accepts at most six decimal places and does not round through floating
   point.
3. Budget approval, rate-card approval/activation and journal approval use
   append-only versions and maker/checker separation.
4. A budget does not block a model request. Runtime spend enforcement requires
   a separately approved UniRouter policy and is not inferred from finance
   reporting state.
5. Supplier billed-effective rates require source evidence. Effective card
   collisions and historical rewrites are rejected.
6. Allocation replay with the same exact input is a no-op; a changed input
   under the same identity is a conflict and an explicit superseding run is
   required.
7. A period with spending cannot close unless every Source request is present,
   no result is unresolved or conflicting and allocated/source totals match
   exactly.
8. A closed period cannot be reopened or rewritten. Corrections are new
   adjustment/reversal records, and a reversal must match an approved original
   record exactly.
9. Allocation and journals never create customer debt, change the wallet,
   modify request settlement or claim ERP/GL posting authority.
10. Allocation identities are explicitly product-local. They are not a second
    definition of the shared Event & Connector canonical hash.

## Commands and results

- `go test ./... -count=1`: PASS.
- `REQUIRE_TEST_DATABASE=1 go test -tags=integration ./internal/orgdirectory
  ./internal/enterprisefinance ./internal/admin ./internal/migrate -run
  'Test(ProjectionStore|PostgresStore|EnterpriseDirectory|EnterpriseFinance|EnterpriseRate|EnterpriseAllocation)'
  -count=1`: PASS for all four packages.
- `go test -race ./internal/orgdirectory ./internal/enterprisefinance -count=1`:
  PASS.
- `go vet ./...`: PASS.
- Frontend full regression: 49/49 files and 345/345 tests PASS.
- `npm run type-check`: PASS.
- `npm run build`: PASS, 4,311 transformed modules and no 500 kB warning.
- Focused Wallet and Reconciliation UI regression: 14/14 tests PASS.

The integration run used the existing local PostgreSQL test service. It did not
write a target or production database.

## UI result

- **Budgets & Cost Centers** separates the compatibility settlement wallet from
  enterprise periods, cost centers, budgets and exact actual coverage.
- **Rates & Rating** separates current request pricing from the enterprise Rate
  Card control layer and immutable snapshot evidence.
- **Allocation & Reconciliation** shows close readiness, immutable allocation
  runs and closed-period adjustments without claiming an ERP posting.
- Bulk allocation input is intentionally exposed through the controlled API,
  not hand-authored row by row in the browser. A real producer or import format
  belongs to the later customer integration decision.

## Boundary and non-claims

- No ERP, general ledger, chart of accounts, supplier invoice connector or
  chargeback posting target was selected.
- No deployment, finance credential, external traffic or production database
  mutation occurred.
- No request record, customer billing result, price snapshot already used by a
  request, Outbox identity, completed-execution Feed, Source bytes/hash or
  Cursor behavior changed.
- This is Local evidence, not CI, Target, Production or external-integration
  evidence.
