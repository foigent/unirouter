# PRD6 Batch 0B P6-F34 Pricing Authority — Local Evidence

> Date: 2026-07-11
> Base: `609385f663cee883ad5965facb25ce4185d8b89a`
> Branch: `codex/prd6-p6f34-price-authority`
> Evidence level: CI verified by PR #146 implementation head `0e15dbb` / run `29137361064`; final documentation head and merge pending; no deployment is claimed

## Confirmed Pre-Fix Finding

The reviewed base mounted all global-price and organization-multiplier reads and
writes behind the same organization-administrator role guard. An organization
administrator could therefore upsert, rename or delete a global base price and
could use a body organization or global multiplier row ID to change another
tenant's billing multiplier. The billing runtime consumes the product of both
tables, so the defect affected subsequent real charge calculation rather than
only an administration screen.

The organization-multiplier page also had three confirmed contract failures:
it always queried organization `1`, read `data` while the backend returned
`items`, and sent absolute input/output prices while the backend accepts only a
multiplier. The visible CRUD workflow was therefore not a reliable description
of runtime behavior.

## Repository Change

- production pricing routes now share one registrar: base-price and multiplier
  reads remain available to `platform_admin` and `org_admin`, while every POST,
  PUT and DELETE is `platform_admin` only;
- each mutation handler independently fails closed unless the authenticated
  role normalizes to platform administrator, so a future route-wiring mistake
  does not silently restore tenant writes;
- organization multiplier reads default to the JWT organization, reject an
  explicit foreign organization, reject organization-zero identities and let a
  platform administrator select a positive target explicitly;
- all six mutations put the business write and audit row in the same database
  transaction. Audit failure or commit failure leaves no price change;
- create/upsert and rename operations take a transaction-scoped advisory lock
  for the target price key. Concurrent first writes therefore serialize and the
  second audit row records the first committed value as `before_state`;
- multiplier input is constrained to `0.001..99.999`, matching the existing
  `NUMERIC(5,3)` column. Values such as `0.0004` can no longer round to `0.000`
  and collapse large usage toward the one-cent billing floor; values at or over
  `100` fail before the database instead of becoming an opaque server error;
- the organization response preserves legacy `items` while adding the standard
  `data`, selected `org_id`, organization display name, pagination and an
  informational mutation capability. The global response now includes the
  timestamp already shown by the UI;
- the UI labels the feature as an organization billing multiplier, uses the
  multiplier API contract, removes the organization-1 fallback, keeps tenant
  views read-only and exposes explicit target-organization selection only to a
  platform administrator;
- backend HTML docs, frontend API docs, OpenAPI RBAC metadata and route-parity
  coverage reflect the same authority boundary. Extracting the registrar also
  removes this pricing block from `main.go` without changing unrelated routes.

No schema migration, target deployment, credential, provider request, external
message, exporter, retained-data scope or X-Insight contract changed.

## Migrated Signed-JWT Matrix

`TestP6F34PriceAuthorityIntegration` creates an empty PostgreSQL database,
applies every migration through `000020`, seeds two organizations and invokes
the pricing API through signed JWT authentication plus role middleware.

It proves:

1. organization administrators can read global base prices and only their own
   multiplier rows; foreign reads return `403` and organization `0` fails
   closed;
2. a platform administrator defaults to its JWT organization and can explicitly
   select the other organization;
3. organization, billing, project-admin, developer and viewer roles receive
   `403` on every base-price and multiplier mutation, including valid own- and
   foreign-organization payloads and invalid JSON that never reaches parsing;
4. denied calls leave both price tables, audit count and both organizations'
   effective `base × multiplier` billing results exactly unchanged;
5. all six platform mutations succeed, emit the expected actor/target audit
   rows and preserve billing calculation (`12 × 1.5 = 18`, `60 × 1.5 = 90`,
   `12 × 2.5 = 30`, `60 × 2.5 = 150`);
6. concurrent first upserts for the same global key and the same organization
   key each create two audit rows, with exactly the later row containing a
   non-empty `before_state`;
7. missing and invalid bearer tokens return `401`. A production-route wiring
   test separately proves platform and legacy-admin compatibility, tenant
   denial and organization-admin read access.

Mock transaction tests additionally prove that audit insertion failure invokes
rollback without commit, handler-level tenant denial occurs before opening a
transaction, target-name locking precedes a rename mutation, empty model names
are rejected and multiplier bounds fail before database access.

## Local Verification

The final local diff passed:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
npm --prefix frontend test -- --reporter=dot
npm --prefix frontend run type-check
npm --prefix frontend run build
git diff --check
```

Results: every backend package, the race detector and vet pass; the frontend
suite passes 27 files / 135 tests; type-check and production build pass. The
build retains the existing large-chunk warning. The macOS race link step emits
the existing non-failing `LC_DYSYMTAB` warning. Independent review found no
remaining blocker, high or medium finding after the fail-closed, numeric-bound
and concurrent before-state fixes. PR #146 implementation head
`0e15dbb85418aab99c24189eb99d4ddb05068f49` passed `Backend Tests`, `Frontend
Build` (including the 135-test suite) and `Docker Build` in required CI run
`29137361064`. The final documentation head remains subject to the same required
checks before merge.

## Explicit Remaining Boundaries

- Production admin authentication still does not generate or propagate an
  authoritative admin `request_id`. Tests inject a correlation ID to verify
  audit storage, but P6-F34 does not claim the broader P6-F17 correlation and
  idempotency redesign is complete.
- Reservation currently does not apply/pin the organization multiplier and a
  mid-flight price change is not version-bound to final settlement. Those are
  P6-F06/P6-F18 financial-state items, not a reason to reopen this authority
  fix.
- Existing old application replicas do not know the new role guard or advisory
  lock. A mixed-version target rollout is not safe evidence until all old
  pricing writers are drained/replaced. Target verification is explicitly
  deferred until after PRD6 closeout under the accepted target-evidence queue.
- The platform UI uses an explicit organization ID rather than a canonical,
  searchable organization directory. A directory endpoint/selector and
  OpenAPI-generated frontend types remain later UX/API productization, not a
  security prerequisite for P6-F34.
- No target or production behavior is claimed from local tests or future PR CI.
