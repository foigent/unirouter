# V1 Post-PRD6 Pricing Authority Local Evidence — 2026-07-14

## Boundary

- This is Post-PRD6 release-readiness work. It does not reopen or reinterpret
  PRD6 Accepted Scope Complete.
- Classification: `confirmed bug/regression`.
- Severity: Critical for unit ambiguity and unpriced fallback; High and still
  open for sub-cent settlement rounding.
- Evidence level in this document: local repository and migrated PostgreSQL
  verification only. No target price was changed and no target/production claim
  is made.

## Confirmed defects

1. `model_prices.input_price_cents` and `output_price_cents` are consumed by
   billing as decimal CNY cents per 1,000 tokens (`rate * tokens / 1000`).
2. The management page described and submitted yuan per 1,000 tokens, while
   migration `000002` described its values as cents per 1,000,000 tokens. The
   same stored number therefore had incompatible documented meanings.
3. `DBPriceProvider.QueryModelPrice` and the organization quote query silently
   substituted 2/10 when a model price row did not exist. A route could be
   admitted and charged without an explicit price authority.
4. The create form populated its model selector from `model_prices` itself, so
   a genuinely new model could not be priced through the UI.
5. The browser parser used `parseFloat`, which accepted inputs such as
   `2.0tail` and scientific notation without an explicit unit/precision
   contract.
6. Fresh installations received mutable vendor price examples with no source,
   currency conversion, effective date or update authority.

## Implemented contract

- The V1 admin API and UI expose only:
  - `input_price_cny_per_million_tokens`
  - `output_price_cny_per_million_tokens`
  - `currency = CNY`
  - `price_basis_tokens = 1000000`
- Legacy `input_price_cents` and `output_price_cents` request fields are rejected
  by a bounded strict JSON decoder rather than interpreted heuristically.
- Public prices are nonnegative, below the database numeric bound, and limited
  to five decimal places. The internal decimal representation remains CNY cents
  per 1,000 tokens so existing configured values and immutable reservation
  snapshots are not repriced.
- Missing model price rows return `ErrModelPriceNotConfigured`; direct lookup,
  organization lookup and Freeze all fail before wallet mutation.
- An explicit zero/zero price row remains a valid free-model authority and is
  distinguishable from a missing row.
- The UI accepts a trimmed model name directly and uses strict full-string
  price parsing.
- Fresh databases no longer seed unsourced model prices.
- Migration `000035_v1_pricing_unit_contract` adds database column comments
  only. Its migrated-database test proves down/up behavior and preserves the
  exact `0.2/0.1` sample row; it does not update, delete, verify or approve any
  existing price.

## Verification

- `go vet ./...` — pass.
- `go test ./... -count=1` — pass.
- `go test -tags=integration -race ./internal/admin -count=1` — pass.
- `go test -tags=integration -race ./internal/billing ./internal/migrate -run 'TestV1Pricing(Authority|UnitContract)' -count=1` — pass.
- `npm test -- --run` — 30 files / 154 tests pass.
- `npm run type-check` — pass.
- `npm run build` — pass; existing Vite chunk-size warning remains non-blocking.
- OpenAPI YAML parse and `git diff --check` — pass.

## Explicit non-claims and deployment gate

- Existing ECS price rows are not automatically converted, deleted or marked
  verified. Before deploying this contract, each routed model must be reviewed
  against an approved source, currency/FX policy and effective date.
- The current schema still rounds terminal customer charges to integer cents and
  enforces a one-cent minimum in several paths. Removing that minimum safely is
  not a local arithmetic edit: sub-cent liability must remain attributed to the
  original cash or grant source, including grant expiry. That separate decision
  is tracked as `V1-BILL-002` and is not silently resolved by this patch.
