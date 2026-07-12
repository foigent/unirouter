package billing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestP6F09PromptCachePriceFreezeSettlementAndIdempotency(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`UPDATE model_prices
			 SET input_price_cents = 4,
			     output_price_cents = 20,
			     cache_read_ratio = 0.1,
			     cache_creation_ratio = 1.25,
			     updated_at = NOW()
			 WHERE model_name = $1`, model)
		return err
	}())
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO org_model_prices (org_id, model_name, multiplier)
			 VALUES ($1, $2, 2)`, orgID, model)
		return err
	}())

	b := NewBilling(pool, nil)
	requestID := "op_p6f09_cache_aware_freeze"
	freeze, err := b.Freeze(ctx, FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 1000,
		SafetyFactor:    1,
		MinFreezeAmount: 1,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success, "%s: %s", freeze.ErrorCode, freeze.ErrorDetail)
	require.Equal(t, 40, freeze.Amount, "reservation must cover the most expensive effective token class")
	require.True(t, strings.HasPrefix(freeze.PricingVersion, "pv2_"))

	var pricingVersion string
	var inputPrice, readPrice, creationPrice, outputPrice, readRatio, creationRatio float64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT pricing_version,
		        effective_input_price_cents::float8,
		        effective_cache_read_price_cents::float8,
		        effective_cache_creation_price_cents::float8,
		        effective_output_price_cents::float8,
		        cache_read_ratio::float8,
		        cache_creation_ratio::float8
		 FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&pricingVersion, &inputPrice, &readPrice, &creationPrice, &outputPrice, &readRatio, &creationRatio))
	require.Equal(t, freeze.PricingVersion, pricingVersion)
	require.InDelta(t, 8, inputPrice, 0.000001)
	require.InDelta(t, 0.8, readPrice, 0.000001)
	require.InDelta(t, 10, creationPrice, 0.000001)
	require.InDelta(t, 40, outputPrice, 0.000001)
	require.InDelta(t, 0.1, readRatio, 0.000001)
	require.InDelta(t, 1.25, creationRatio, 0.000001)

	// Later configuration changes must not rewrite the quote frozen for this
	// operation. Settlement is always based on the immutable pv2 receipt above.
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`UPDATE model_prices
			 SET input_price_cents = 100,
			     output_price_cents = 100,
			     cache_read_ratio = 99,
			     cache_creation_ratio = 99,
			     updated_at = NOW()
			 WHERE model_name = $1`, model)
		return err
	}())
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`UPDATE org_model_prices SET multiplier = 3, updated_at = NOW()
			 WHERE org_id = $1 AND model_name = $2`, orgID, model)
		return err
	}())

	usage := SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		PromptTokens:         3500,
		UncachedPromptTokens: 1000,
		CacheReadTokens:      2000,
		CacheCreationTokens:  500,
		CompletionTokens:     100,
		UsageSource:          "upstream",
		CacheUsageSchema:     "anthropic.prompt_cache.v1",
		CacheUsageStatus:     "cache_read_write_reported",
	}
	require.NoError(t, b.MarkBillableOutput(ctx, requestID, orgID))
	require.NoError(t, b.PersistSettlementUsage(ctx, usage))
	require.NoError(t, b.PersistSettlementUsage(ctx, usage), "identical cache usage replay must be idempotent")

	conflict := usage
	conflict.UncachedPromptTokens++
	conflict.CacheReadTokens--
	require.ErrorIs(t, b.PersistSettlementUsage(ctx, conflict), ErrSettlementUsageConflict)
	require.ErrorIs(t, func() error {
		_, settleErr := b.Settle(ctx, conflict)
		return settleErr
	}(), ErrSettlementUsageConflict)

	settled, err := b.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Equal(t, 18, settled.ActualCost)
	require.Equal(t, 18, settled.ChargedCost)
	require.Zero(t, settled.UncoveredCost)
	require.Equal(t, 22, settled.Refund)
	require.Equal(t, freeze.PricingVersion, settled.PricingVersion)
	require.InDelta(t, 8, settled.UncachedInputCostCents, 0.000001)
	require.InDelta(t, 1.6, settled.CacheReadCostCents, 0.000001)
	require.InDelta(t, 5, settled.CacheCreationCostCents, 0.000001)
	require.InDelta(t, 4, settled.OutputCostCents, 0.000001)

	replayed, err := b.Settle(ctx, usage)
	require.NoError(t, err)
	require.Equal(t, settled.SettleID, replayed.SettleID)
	require.Equal(t, settled.ActualCost, replayed.ActualCost)
	require.InDelta(t, settled.CacheReadCostCents, replayed.CacheReadCostCents, 0.000001)
	require.ErrorIs(t, func() error {
		_, replayErr := b.Settle(ctx, conflict)
		return replayErr
	}(), ErrSettlementUsageConflict, "a terminal cache-aware receipt must reject conflicting replay evidence")

	var balance, frozen, spent, terminalEvents int
	var storedStatus, storedSchema, storedCacheStatus string
	var uncachedTokens, readTokens, creationTokens int
	var storedReadCost, storedCreationCost float64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance, frozen, total_spent FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen, &spent))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
		        cache_usage_schema, cache_usage_status,
		        cache_read_cost_cents::float8, cache_creation_cost_cents::float8
		 FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&storedStatus, &uncachedTokens, &readTokens, &creationTokens,
		&storedSchema, &storedCacheStatus, &storedReadCost, &storedCreationCost))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalEvents))
	require.Equal(t, 982, balance)
	require.Zero(t, frozen)
	require.Equal(t, 18, spent)
	require.Equal(t, "settled", storedStatus)
	require.Equal(t, 1000, uncachedTokens)
	require.Equal(t, 2000, readTokens)
	require.Equal(t, 500, creationTokens)
	require.Equal(t, usage.CacheUsageSchema, storedSchema)
	require.Equal(t, usage.CacheUsageStatus, storedCacheStatus)
	require.InDelta(t, 1.6, storedReadCost, 0.000001)
	require.InDelta(t, 5, storedCreationCost, 0.000001)
	require.Equal(t, 1, terminalEvents)
}
