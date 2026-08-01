# PRD7 B2 Contextual Inspectors — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B2-CONTEXTUAL-INSPECTORS`
> Requirement: `P7-F02`
> Status: Local verified
> Start head: `9cb6fc44b56acd6c09913f365e9098a551646d9e`
> Start tree: `8cbb44342e1025d5cff60e053a0bade2b493c443`
> Branch: `codex/product-iteration-20260730`

## Delivered result

- Audit keeps its filterable result list and a read-only event inspector in one
  workspace. It restores filter/page/selection deep links, selects a valid
  `audit_id` or the first current-page event, and supports row/keyboard
  selection.
- Audit separates filter drafts from applied filters, supplies bounded recent
  time shortcuts, and drops stale asynchronous responses.
- Projects keeps the organization-scoped project list, project overview,
  members and licensed model scope in one persistent workspace. It restores
  organization/project/tab/page deep links and automatically selects the
  requested or first current-page project.
- Both surfaces have validated browser-local named views. These contain only
  filter or workspace identity metadata, are bounded to 12 entries, and do not
  claim server, tenant or cross-device synchronization.
- A shared query utility updates only controlled keys, preserves unrelated
  query/hash state, and rejects invalid identity/pagination values.
- Loading or failure clears stale records and detail before exposing a retry or
  empty state.

The precise interaction and storage contract is
`docs/tech-ref/31-contextual-inspectors-and-local-views.md`.

## Browser-discovered defects closed

The real-browser pass found two defects not exposed by the first component
tests:

1. application `React.StrictMode` plus the persistent Project form caused
   unstable Semi Table column identity to enter a nested update loop; stable
   memoized column and row contracts plus a StrictMode regression test close
   it;
2. the desktop `620px` saved-workspace flex basis became height after the
   mobile layout changed to a column; the mobile breakpoint now resets that
   basis and the measured bar height is 152 CSS pixels.

## Verification

| Command/check | Actual result |
|---------------|---------------|
| focused view-state, Audit and Projects tests | PASS — 3 files / 13 tests, including deep links, request race, saved metadata, keyboard selection and application StrictMode |
| `npm --prefix frontend run type-check` | PASS |
| `npm --prefix frontend test -- --reporter=dot --silent=passed-only` | PASS — 42 files / 308 tests |
| `npm --prefix frontend run build` | PASS — TypeScript plus Vite production build |
| `GOCACHE=/tmp/unirouter-go-cache-b2-test go test ./... -count=1` | PASS — all Go packages |
| `GOCACHE=/tmp/unirouter-go-cache-b2-vet go vet ./...` | PASS |
| synthetic Audit browser at 1440×1000 | PASS — first/deep-linked event selected, inspector visible, page width 1440/1440 |
| synthetic Audit browser at 1024×900 | PASS — two columns measured 374/340 CSS px, inspector stays visible |
| synthetic Audit browser at 390×844 | PASS — one 356px column, inspector follows list, page width 390/390 |
| synthetic Projects browser at 1440×1000 | PASS — list/workspace measured 474.6/655.4 CSS px; member tab restored |
| synthetic Projects browser at 1024×900 | PASS — two columns measured 320/400 CSS px |
| synthetic Projects browser at 390×844 | PASS — one 356px column, 152px saved-view bar, page width 390/390 |
| Project row selection | PASS — URL changed from project 42/member tab to `project_id=43`; inspector changed without an open action |
| final browser console | PASS — 0 errors; two pre-existing React Router v7 future-flag warnings retained as non-product migration debt |
| `git diff --check` | recorded after documentation finalization |

The browser used only route-scoped synthetic responses and a synthetic local
session marker. Screenshots are retained under the repository-ignored
`output/playwright/` directory. No external endpoint was contacted.

The production build retains the existing Vite large-chunk warning. B2 makes
no bundle-size or performance claim.

## Truth and mutation boundaries

- Saved views are local browser metadata only. They do not persist or transmit
  Audit event content, project content, credentials, or authorization facts.
- The persistent inspector exposes only fields already returned by the current
  APIs. It does not add backend fields or broaden permissions.
- No backend/API/schema, database, IAM, request, routing, billing, Outbox,
  completed-execution Feed, endpoint/page/cursor, credential, shared protocol,
  X-Insight, deployment, traffic, push, PR or merge action changed.
- This evidence is Local verified only, not CI, Target, external-integration or
  Production evidence.

## Next dependency

P7-F02 is complete across Requests, Providers, Audit and Projects. The next
dependency-ordered repository-local batch is B3: default-off, metadata-only
OTel/OpenInference export with bounded asynchronous delivery and strict
raw-content/cardinality negatives.
