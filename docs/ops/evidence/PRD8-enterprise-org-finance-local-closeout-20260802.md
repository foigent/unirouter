# PRD8 Enterprise Organization And Finance — Local Closeout

> Work item: `UR-PRD8-ENTERPRISE-ORG-FINANCE-LOCAL`
> Date: 2026-08-02
> Status: Autonomous local sequence complete; customer-target decision required
> Baseline commit/tree: `c0182861c7ec1079e335e317863f7ec5bcabd731` /
> `0bfeed8938eb6235ec20db05450f2472e43460a3`

## Closed sequence

```text
UI-1..5 -> UI-PERF-1 -> DOMAIN-0
  -> ORG-1 -> ORG-2
  -> FIN-1 -> FIN-2 -> FIN-3
  -> full Local regression and compatibility audit
```

The working tree now contains every autonomous batch in the dependency plan.
`ORG-3` and `FIN-4` remain deliberately unimplemented because a real customer
directory and ERP/GL target have not been selected.

## Final Local verification

- `go test ./... -count=1`: PASS.
- Required PostgreSQL integration selection across directory, finance, admin
  and migration packages: PASS.
- Complete `internal/migrate` integration suite after extending the legacy
  rollback helper through `000051 -> 000047`: PASS. The first full run exposed
  the stale helper when the `000046` guard correctly refused to cross newer
  PRD8 migrations; the focused repair and complete rerun both passed.
- `go test -race ./internal/orgdirectory ./internal/enterprisefinance -count=1`:
  PASS.
- `go vet ./...`: PASS.
- Frontend: 49/49 files and 345/345 tests PASS.
- Frontend focused finance interaction tests: 14/14 PASS.
- `npm run type-check`: PASS.
- `npm run build`: PASS, 4,311 modules, no 500 kB warning.
- Reproducible manifest build: PASS. Current entry JavaScript is 273.66 kB
  minified / 86.88 kB gzip. The largest minified JavaScript chunk is the
  Dashboard at 376.63 kB / 100.87 kB gzip; the largest gzip JavaScript payload
  is 103.75 kB.
- Static-import closure measurements: login 20 JS / 10 CSS assets (359.47 kB /
  56.44 kB gzip), dashboard 26 JS / 11 CSS (348.75 kB / 37.85 kB), and the
  Organization & Members default account view 34 JS / 18 CSS (442.67 kB /
  69.07 kB). Lazy project/lifecycle tabs remain outside the default view.
- Browser verification with synthetic, non-production API data: desktop menus
  remain fully expanded; the top row is a lightweight utility toolbar only;
  enterprise budget, Rate Card and allocation/close pages and their create
  dialogs render and are operable without submission; document width equals
  viewport width at 1440, 1024 and 768 pixels; no page-breaking console error
  was observed. Vite development mode still emits known React Router and Semi
  UI deprecation warnings, so this is not claimed as a zero-warning run.
- Local links in the 12 touched PRD8/current-truth documents: PASS.
- `git diff --check`: PASS.

## Compatibility conclusion

The local changes do not alter request admission, model routing, retry,
provider dispatch, current request pricing, reservation, settlement, wallet
ledger finality, Outbox identity, completed-execution Source bytes/hash, Feed
endpoint/DTO/credential/order/Cursor behavior or the no-default-source-instance
rule. New directory and finance records are additive product-local projections
and controls.

X-Insight remains the owner of cross-source investigation; Tool Gateway owns
tool side effects; MCP Gateway owns MCP protocol/session behavior. No shared
high-privilege plugin runtime was introduced.

## Exact stop gate

Further implementation requires one of two real customer decisions:

1. **ORG-3:** name the first HCM/IdP/directory, its supported full/delta/event
   interfaces, field authority, tenant topology and credential boundary.
2. **FIN-4:** only if the customer needs chargeback, name the ERP/GL, legal
   entities, chart of accounts, posting policy, approval flow and export
   transport.

The default remains local accounts plus showback/reporting. No generic
Connector marketplace, guessed vendor adapter or second ledger will be built
in place of those decisions.

## Non-claims

- No commit, push, PR, merge, release or deployment was performed.
- No real directory/ERP credential, external traffic, production DB,
  Cursor/Checkpoint or product-runtime state was changed.
- This closeout is Local evidence only; it is not CI, Target, Production or
  external-integration proof.
