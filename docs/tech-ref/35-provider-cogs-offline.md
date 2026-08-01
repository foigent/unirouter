# Provider COGS Offline Contract And Financial Truth

> Contract: `unirouter.provider-cogs-offline.v1`
> Source snapshot: `unirouter.provider-attempt-snapshot.v1`
> Report: `unirouter.provider-cogs-report.v1`
> Runtime state: zero-persistence local preview; real connector credentials,
> account scope and retention remain gated by P7-D05

## Authority separation

The contract keeps three financial authorities separate:

1. supplier-reported cost from a supplier usage/cost export;
2. UniRouter's provider-cost estimate captured for a source attempt;
3. customer revenue already recorded by the customer ledger.

No supplier line creates, changes or releases customer debt. Every report and
match says `customer_ledger_mutation=none` /
`customer_debt_action=none`. Existing
`POST /api/v1/provider-cost/reconciliations` remains the independent
amount-free recovery path for unresolved customer funding; it is not reused as
a supplier COGS import.

## Strict import model

- The JSON parser rejects unknown fields, nested duplicate keys, trailing JSON,
  non-UTF-8/control-bearing identities and inputs above 4 MiB.
- Money and counters are canonical decimal strings parsed as exact signed or
  unsigned int64 values. Scales must be positive powers of ten.
- Times are canonical UTC with exactly six fractional digits.
- Page IDs are unique, indexes are contiguous decimal strings, cursors form an
  exact chain and only the last page may be final.
- Supplier line identity is immutable. Repeating an identical line is a
  duplicate/no-op; the same line ID with different content is a conflict.
- Import replay uses a canonical semantic hash, so whitespace-only JSON
  changes are duplicate/no-op. A content change under the same `import_id` is
  a conflict. The accepted report retains the original raw payload hash.

## Attribution and coverage

Request attribution is allowed only when a request-granular supplier line has
a non-empty upstream request ID and resolves to exactly one source attempt
after the optional hashed project scope is applied. Zero candidates becomes
`supplier_only`; multiple candidates becomes `ambiguous`.

`project_bucket`, `account_bucket` and `invoice_line` evidence always remains
`aggregate_unattributed`. It cannot inherit a request ID, customer amount or
debt action.

A closed coverage window can classify an unmatched in-window source attempt as
`source_only`. Provisional coverage uses
`supplier_evidence_pending` and must declare `provider_data_delayed`.
The fixed declared capture-gap vocabulary is:

- `provider_data_delayed`;
- `request_attribution_unavailable`;
- `discount_credit_unallocated`;
- `tax_fx_unallocated`.

## Product surface

- `GET /api/v1/provider-cogs/offline-contract` returns the contract, limits,
  fixed gap codes, immutable artifact identities and non-persistence boundary.
- `POST /api/v1/provider-cogs/offline-preview` performs one in-memory
  evaluation. It has no database, supplier-network or customer-ledger write
  dependency.
- Both endpoints require platform or billing authority and set no-store
  response headers.
- The Financial Truth page keeps customer-ledger validation and supplier COGS
  evidence distinct. Its evidence inspector is permanently present and
  auto-selects the first result after a successful preview.
- `uniroute-provider-cogs-preview` is a zero-network file-in/report-out CLI.
- `uniroute-provider-cogs-conformance` reproduces the embedded golden report.

## Immutable local artifacts

The embedded schema, two synthetic fixtures and golden report live under
`internal/providercogs/artifacts/`. The fixtures cover closed request-level
matching with pagination and duplicate/no-op plus provisional invoice-level
charge/credit evidence with all four declared gaps.

The golden report contains two fixtures and fifteen passing cases across
strict JSON, schema shape, cursor, gap, financial authority, exact decimal,
duplicate, finality, attribution, privacy, customer-ledger separation,
determinism, hash, canonicalization, replay, conflict and negative cursor
classes.

## External evidence boundary

The contract shape reflects provider reporting realities:

- OpenAI exposes detailed usage separately from financial Costs and documents
  that usage data may not reconcile perfectly for financial purposes:
  <https://platform.openai.com/docs/api-reference/usage>;
- Anthropic reports usage in pageable time buckets groupable by API key,
  workspace, model and service tier:
  <https://docs.anthropic.com/en/api/admin-api/usage-cost/get-messages-usage-report>;
- Azure Cost Management exports are scope-based scheduled files with daily
  granularity and partition manifests:
  <https://learn.microsoft.com/en-us/rest/api/cost-management/exports>.

These sources justify explicit aggregate, lag and attribution-gap states. They
do not prove any UniRouter customer account, negotiated price, invoice or
production reconciliation.

## Non-goals

This batch does not add real supplier credentials, account discovery, scheduled
imports, persistent COGS tables, foreign-exchange policy, tax allocation,
invoice approval, runtime polling, customer debt, deployment or production
evidence. P7-D05 must define real account scope, credential ownership and
retention before those paths can start.
