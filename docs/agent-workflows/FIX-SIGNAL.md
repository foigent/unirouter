# FIX-SIGNAL.md - PRD4 Preflight Hardening

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary

## Context

User approved continuing after the consultant issue review. This batch fixes
three confirmed preflight hardening items without starting PRD4 feature scope:

1. price lookup query errors must not silently fall back to hardcoded default
   billing prices;
2. user-visible error docs registry needs direct regression coverage;
3. graceful shutdown should not use a hardcoded 30-second timeout when normal
   streaming responses can run up to the backend write timeout.

This is PRD4 preflight hardening, not a PRD4 feature implementation batch. It
does not implement usage/cost UI, Trace/Event foundation, Observability, SSO,
plugin/PlungeIn runtime, admin package refactoring, router caching, or any PRD5
deferred capability.

Behavior boundary:

- Missing/unconfigured model price may still use the existing default price
  policy.
- Database/context/query failures now return an error and must not charge at
  default prices.
- Non-stream and embedding responses are withheld with a billing error if
  settlement price calculation cannot be trusted.
- Stream responses cannot be withheld after data has already been sent; the
  frozen billing amount is canceled where possible, the call log records zero
  charged cost, and the final SSE `_uniroute` metadata includes a
  `price_lookup_failed_*` billing status.
- `errdocs.All()` returns a defensive copy and all registered docs have required
  fields plus URL mapping coverage.
- `SHUTDOWN_TIMEOUT_SECONDS` defaults to 120 seconds, is exposed in env/compose
  docs, and `cmd/uniroute` uses it for graceful shutdown.

## Files Changed

- `.env.example`
- `docker-compose.yml`
- `internal/billing/price_provider.go`
- `internal/billing/billing.go`
- `internal/billing/billing_price_test.go`
- `internal/billing/billing_integration_test.go`
- `internal/gateway/relay.go`
- `internal/gateway/relay_mock_test.go`
- `internal/errdocs/errdocs_test.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `cmd/uniroute/main.go`
- `docs/ops/deployment-runbook.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Required Verification

1. Confirm fallback behavior is limited to missing model prices, not query
   failures:

   ```bash
   rg -n "using default|using defaults|fallback to default|QueryErrorDoesNotFallback|price_lookup_failed" internal/billing internal/gateway
   ```

   Expected: no `using default(s)` error fallback remains in billing/gateway
   price paths. `fallback to default` should appear only for the unpriced-model
   policy/tests. `QueryErrorDoesNotFallback` tests and `price_lookup_failed_*`
   stream status should be present.

2. Run focused backend tests:

   ```bash
   go test ./internal/billing -run 'Test(DBPriceProvider_QueryErrorDoesNotFallback|QueryModelPriceForOrg_QueryErrorDoesNotFallback|QueryModelPrice_|CalculateCost_|EstimateFreezeAmount_)' -count=1 -v
   go test ./internal/gateway -run 'TestCalculateCost' -count=1 -v
   go test ./internal/errdocs -count=1 -v
   go test ./internal/config -count=1 -v
   ```

3. Run package regression tests:

   ```bash
   go test ./internal/billing ./internal/gateway ./internal/errdocs ./internal/config ./cmd/uniroute -count=1
   ```

4. Run broad validation if available in the current environment:

   ```bash
   go test ./... -count=1
   git diff --check
   git diff --name-only
   ```

## Expected Result

Canary should mark this batch PASS only if:

- price lookup DB/context/query errors return an error and zero cost;
- `DBPriceProvider.QueryModelPrice` only defaults on `pgx.ErrNoRows`;
- `Billing.QueryModelPriceForOrg` no longer defaults on query failure;
- gateway non-stream and embedding billing price failures cancel the freeze and
  return `billing_error.settle_failed`;
- gateway stream billing price failures cancel the freeze where possible and
  emit zero charged cost with `price_lookup_failed_cancelled` or
  `price_lookup_failed_unresolved`;
- missing/unconfigured model-price fallback remains intact;
- `errdocs` registry copy, required fields, and URL fallback behavior are tested;
- `SHUTDOWN_TIMEOUT_SECONDS` is loaded, validated for negative values, exposed
  in sample deployment config, and used by graceful shutdown;
- no frontend, migration, route, deployment topology, PRD2 freeze, or PRD3
  completion semantics are changed; deployment docs/config only expose the new
  shutdown timeout setting.
