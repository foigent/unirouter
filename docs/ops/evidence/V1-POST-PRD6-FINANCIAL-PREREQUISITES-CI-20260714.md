# V1 Post-PRD6 Financial Prerequisites CI Evidence — 2026-07-14

## Classification and boundary

- `V1-WALLET-001`: confirmed bug/regression, Critical, UniRouter wallet/admin
  ownership, CI verified and closed.
- `V1-BILL-001`: confirmed bug/regression, Critical, UniRouter pricing/billing
  ownership, CI verified and closed at repository level.
- `V1-FIN-001`: confirmed bug/regression, Critical/P0, implementation remains in
  progress under the User-approved micro-CNY ledger option B.
- PRD6 remains `Completed (Accepted Scope)`. No target database, deployment,
  credential, production state or X-Insight runtime was changed.

## Wallet atomicity prerequisite

1. The prior admin UI derived an absolute target balance from a page-load
   snapshot. The backend later wrote that value after locking the current row,
   so a concurrent charge, top-up or administrator operation could be
   overwritten.
2. PR #196 replaces that contract with a signed `delta_cents` applied to the
   latest locked balance, requires an operation reason and stable
   `client_request_id`, returns exact retries idempotently and rejects identity
   reuse with a different payload.
3. Local post-main-merge verification passed focused admin wallet tests and 43
   Wallet/API frontend tests.
4. Exact head `2c5e930f6a725e958f6c05d20d7e80a888797f2a` passed Change Routing,
   Backend Tests, Frontend Build and Docker Build in run `29300560434`.
5. PR #196 squash-merged as
   `86a3bd8cfa595e52f6828aac98fd902c7b04a357` on
   `2026-07-14T02:24:08Z`. No automatic post-merge CI run was created.

## Pricing authority prerequisite

1. The prior public model-price UI/API unit conflicted with the runtime
   calculation basis, historical seed comments used another basis and a
   missing price silently fell back to invented 2/10-cent rates.
2. PR #197 exposes one public contract, CNY per 1,000,000 tokens; rejects the
   ambiguous legacy mutation fields; fails unpriced models closed; removes
   default prices from new-install seed data; and adds migration `000035` to
   document the unchanged internal decimal basis without rewriting existing
   rows.
3. Local post-main-merge verification passed actionlint, focused billing/admin
   pricing tests, 49 frontend tests, frontend type-check/production build and
   OpenAPI YAML parsing.
4. Exact head `74ebf623fafb6acccd36e09f477b0625c05942ba` passed Change Routing,
   Backend Tests, Frontend Build and Docker Build in run `29301071299`. The
   dedicated V1 pricing-authority/unit-contract PostgreSQL step also passed.
5. PR #197 squash-merged as
   `94c93dccd8dff8543ac4ee1cdc7c9219c384a5d0` on
   `2026-07-14T02:45:35Z`. No automatic post-merge CI run was created.

## Remaining release gate

Repository/CI correctness does not establish the provenance of existing target
price rows. Before any target migration or traffic activation, every configured
model price still needs explicit currency, source and effective-date review;
there is no authorized bulk conversion or automatic target price mutation.

PR #196 and #197 are now the merged prerequisites for `V1-FIN-001`. The next
implementation must preserve wallet delta/idempotency semantics and the public
CNY-per-million price contract while replacing terminal integer-cent authority
with checked integer micro-CNY. Migration `000036` and any target execution
remain behind the documented R3 User decision gate.
