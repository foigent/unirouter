# PRD8 Cost Optimization CO-0 / CO-1 — Local Evidence

> Work item: `UR-PRD8-COST-OPTIMIZATION-V1`
> Evidence date: 2026-08-04 (Asia/Shanghai)
> Baseline commit/tree: `c93c12b3e082a39f6a29e3c95e3029e640980682` / `a826702cfd3ccfa060a217ac961fb62d12835730`
> Branch: `main`
> Status: `LOCAL_VERIFIED_CANDIDATE`
> Runtime mutation: none

## Outcome

CO-0 freezes the enterprise Cost Optimization ownership, exact customer-price
formulas, Response Cache identity/commercial decisions, Compression rollout
boundary and UI placement. CO-1 implements a read-only Prompt Cache savings
workspace from existing terminal request facts and request-time frozen prices.

The implementation adds four tenant-scoped GET APIs, a role-aware sidebar
entry under `成本与财务`, daily/cumulative trend, deterministic review
candidates and an always-present Evidence Inspector. Platform, organization and
billing administrators share the existing pricing-read authority. Platform
administrators may select an organization; every other allowed role remains
bound to its authenticated organization.

The only realized driver is Provider Prompt Cache. Missing cache or pricing
evidence remains a coverage Gap. Exact Response Cache is explicitly
`not_connected_to_gateway`; Compression is
`default_off_shadow_not_started`. Neither contributes invented savings.

## Exact truth boundary

- All API amounts are decimal-string micro-CNY; calculations use PostgreSQL
  exact `numeric` arithmetic and its signed ties-away-from-zero `ROUND`.
- The ordinary-input baseline and actual terminal cost use the same request's
  immutable price snapshot and `cost_micros`.
- The current amounts are customer pricing/showback evidence, not supplier
  COGS. Supplier cost remains a reconciliation concern.
- Opportunity rules use only terminal status, dimensions, counters and prices.
  No Prompt or response content is read or retained.
- No migration, cache adapter, compression mutation, route policy, credential,
  request execution, settlement, Outbox, Feed, Cursor or Checkpoint path was
  changed.

## Verification

The following commands passed on the final application state represented by
this evidence:

```text
env -u TEST_DATABASE_URL GOCACHE=/private/tmp/unirouter-go-cache \
  REQUIRE_TEST_DATABASE=1 go test -p 1 ./... -count=1
  PASS for every package using only uniroute_test and temporary migrated DBs

env GOCACHE=/private/tmp/unirouter-go-cache go vet ./...
  PASS

cd frontend && npm run type-check
  PASS

cd frontend && npm test -- --run
  55/55 files, 365/365 tests PASS

cd frontend && npm run build
  TypeScript PASS
  Vite production build PASS (4,319 modules, 80 JavaScript assets)
  largest asset: 362,347 bytes
  enforced limit: 460,800 bytes

git diff --check
  PASS
```

The real PostgreSQL cost-optimization integration test independently verified
exact summary, trend, breakdown and opportunity results, including
organization isolation. Route tests verified the three-role read authority,
developer rejection, registered-route parity, OpenAPI coverage and the
frontend API contract index.

The first sandboxed package run is not reported as a pass: it correctly found
the initially missing frontend API-doc entries, and unrelated existing
`httptest` cases could not bind a sandboxed loopback port. The documentation
parity defect was fixed; the final complete suite above ran outside that
network sandbox and exited successfully.

## Browser interaction and responsive evidence

Playwright used only a local Vite server and synthetic mock API data. It
verified:

- all two-level sidebar groups remain expanded and `成本优化` has a visible
  icon;
- the lightweight global toolbar remains non-navigational;
- daily/cumulative switching changes the chart scale;
- selecting a breakdown row immediately updates the persistent Inspector and
  its matching deterministic review reason;
- filter comboboxes expose `目标组织` and `拆解维度` names;
- 1440px keeps the Inspector to the right; 1024px and 768px stack it directly
  below the evidence table instead of clipping it; and
- all three widths have no document-level horizontal overflow.

The final page produced zero browser console errors. The two emitted warnings
are the repository's existing React Router v7 future-flag notices, not CO-1
runtime errors. No screenshot or synthetic browser output is a Target or live
data claim.

## Compatibility and next gates

A final diff audit is empty for `internal/gateway`, `internal/billing`,
`internal/metering`, `internal/outbox`, `internal/completedfeed`,
`internal/responsecache`, `migrations` and deployment configuration. The full
regression also covers the existing execution, settlement, Outbox and Feed
packages.

This is Local evidence only. Commit, push, PR, CI, merge and deployment are not
claimed. CO-2 may define the separate exact Response Cache commercial ledger,
Receipt and operator key-backend contract, but no Gateway runtime use is
authorized here. CO-4/CO-5 remain blocked behind a separately reviewed
Compression Shadow/Canary contract.
