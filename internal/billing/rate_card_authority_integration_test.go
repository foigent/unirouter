//go:build integration

package billing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRateCardAuthorityFreezesCompleteActiveQuoteAndIgnoresLaterChanges(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := insertRateAuthorityOrg(t, ctx, pool, "active")
	model := "rate-authority-active-model"
	active := createActiveRequestRateCard(t, ctx, pool, orgID, "contract-active", "provider-a", model, 11, 37)

	service := NewBilling(pool, nil).WithMode(BillingModeCostObservation).
		WithRequestPricingAuthority(RequestPricingAuthorityRateCard)
	frozen, err := service.Freeze(ctx, FreezeRequest{
		RequestID: "rate-authority-freeze", OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 0,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.Regexp(t, `^rcq2_[a-f0-9]{64}$`, frozen.PricingVersion)
	require.LessOrEqual(t, len(frozen.PricingVersion), 80)

	// A later database change cannot alter the already-frozen reservation.
	_, err = pool.Exec(ctx, `
		UPDATE enterprise_finance_rate_components SET amount_minor=999
		WHERE rate_card_version_id=$1 AND meter='input_tokens'`, active.ID)
	require.NoError(t, err)
	var version string
	var input, output, cacheRead, cacheCreation int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pricing_version,effective_input_price_micros_per_million,
		       effective_output_price_micros_per_million,effective_cache_read_price_micros_per_million,
		       effective_cache_creation_price_micros_per_million
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, "rate-authority-freeze").Scan(
		&version, &input, &output, &cacheRead, &cacheCreation,
	))
	require.Equal(t, frozen.PricingVersion, version)
	require.Equal(t, int64(11), input)
	require.Equal(t, int64(37), output)
	require.Equal(t, int64(2), cacheRead)
	require.Equal(t, int64(13), cacheCreation)
}

func TestRateCardAuthoritySettlementUsesFrozenExplicitCacheRates(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := insertRateAuthorityOrg(t, ctx, pool, "cache-settlement")
	model := "rate-authority-cache-model"
	createActiveRequestRateCard(t, ctx, pool, orgID, "contract-cache", "provider-a", model, 40_000_000, 200_000_000)

	service := NewBilling(pool, nil).WithMode(BillingModeCostObservation).
		WithRequestPricingAuthority(RequestPricingAuthorityRateCard)
	const requestID = "rate-authority-cache-settlement"
	freeze, err := service.Freeze(ctx, FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model,
		EstimatedTokens: 1000, SafetyFactor: "1", MinFreezeCents: 0,
	})
	require.NoError(t, err)
	require.Regexp(t, `^rcq2_[a-f0-9]{64}$`, freeze.PricingVersion)

	usage := SettleRequest{
		RequestID: requestID, OrgID: orgID,
		PromptTokens: 3500, UncachedPromptTokens: 1000, CacheReadTokens: 2000, CacheCreationTokens: 500,
		CompletionTokens: 100, UsageSource: "upstream",
		CacheUsageSchema: "anthropic.prompt_cache.v1", CacheUsageStatus: "cache_read_write_reported",
	}
	require.NoError(t, service.MarkBillableOutput(ctx, requestID, orgID))
	require.NoError(t, service.PersistSettlementUsage(ctx, usage))
	settled, err := service.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Equal(t, freeze.PricingVersion, settled.PricingVersion)
	require.Equal(t, int64(40_000), settled.UncachedInputCostMicros)
	require.Equal(t, int64(20_000), settled.CacheReadCostMicros, "cache-read must use the frozen 1/4 Rate Card component")
	require.Equal(t, int64(20_000), settled.CacheCreationCostMicros, "cache-creation must use its explicit Rate Card component")
	require.Equal(t, int64(20_000), settled.OutputCostMicros)
	require.Equal(t, int64(100_000), settled.ActualCostMicros)
}

func TestRateCardAuthorityDoesNotFallbackToLegacyModelPrice(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := insertRateAuthorityOrg(t, ctx, pool, "legacy-no-fallback")
	model := "legacy-only-model"
	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name,provider,input_price_cents,output_price_cents)
		VALUES ($1,'legacy-provider',2,10)`, model)
	require.NoError(t, err)

	frozen, err := NewBilling(pool, nil).WithMode(BillingModeCostObservation).
		WithRequestPricingAuthority(RequestPricingAuthorityRateCard).Freeze(ctx, FreezeRequest{
		RequestID: "legacy-no-fallback", OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 0,
	})
	require.Nil(t, frozen)
	require.ErrorIs(t, err, ErrModelPriceNotConfigured)
	require.ErrorIs(t, err, enterprisefinance.ErrRequestPricingNotReady)
	var reservations int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=$1`, orgID).Scan(&reservations))
	require.Zero(t, reservations)
}

func TestRateCardAuthorityFailsClosedOnPreexistingProviderAmbiguity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := insertRateAuthorityOrg(t, ctx, pool, "ambiguous")
	model := "ambiguous-provider-model"
	insertRawActiveRequestRateCard(t, ctx, pool, orgID, "ambiguous-a", "provider-a", model, 10, 20)
	insertRawActiveRequestRateCard(t, ctx, pool, orgID, "ambiguous-b", "provider-b", model, 9, 19)

	result, err := NewBilling(pool, nil).WithMode(BillingModeCostObservation).
		WithRequestPricingAuthority(RequestPricingAuthorityRateCard).Freeze(ctx, FreezeRequest{
		RequestID: "ambiguous-freeze", OrgID: orgID, Model: model,
		EstimatedTokens: 100, SafetyFactor: "1", MinFreezeCents: 0,
	})
	require.Nil(t, result)
	require.ErrorIs(t, err, enterprisefinance.ErrRequestPricingAmbiguous)
}

func insertRateAuthorityOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) int64 {
	t.Helper()
	var orgID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','customer') RETURNING id`,
		fmt.Sprintf("rate-authority-%s-%d", suffix, time.Now().UnixNano()),
	).Scan(&orgID)
	require.NoError(t, err)
	return orgID
}

func createActiveRequestRateCard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID int64, ref, provider, model string, input, output int64) enterprisefinance.RateCardRecord {
	t.Helper()
	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	card := enterprisefinance.RateCard{
		RateCardRef: ref, OrgID: orgID, ProviderRef: provider, ModelRef: model,
		Layer:         enterprisefinance.RateSupplierContract,
		EffectiveFrom: time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond),
		Components:    requestRateComponents(input, output),
	}
	draft, err := store.CreateRateCardDraft(ctx, card, "user:101")
	require.NoError(t, err)
	approved, err := store.ApproveRateCard(ctx, orgID, ref, draft.Card.Version, "user:102")
	require.NoError(t, err)
	active, err := store.ActivateRateCard(ctx, orgID, ref, approved.Card.Version, "user:102", time.Now().UTC().Truncate(time.Microsecond))
	require.NoError(t, err)
	return active
}

func insertRawActiveRequestRateCard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID int64, ref, provider, model string, input, output int64) {
	t.Helper()
	var cardID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO enterprise_finance_rate_card_versions (
		  org_id,rate_card_ref,version,provider_ref,model_ref,layer,state,effective_from,
		  created_by,approved_by,activated_by,activated_at
		) VALUES ($1,$2,1,$3,$4,'supplier_contracted','active',NOW()-INTERVAL '1 hour',
		  'user:201','user:202','user:202',NOW()) RETURNING id`,
		orgID, ref, provider, model,
	).Scan(&cardID)
	require.NoError(t, err)
	for _, component := range requestRateComponents(input, output) {
		hash, hashErr := enterprisefinance.RateComponentIdentity(component)
		require.NoError(t, hashErr)
		_, err = pool.Exec(ctx, `
			INSERT INTO enterprise_finance_rate_components (
			  rate_card_version_id,org_id,meter,unit_quantity,amount_minor,currency,amount_scale,component_hash
			) VALUES ($1,$2,$3,$4::numeric,$5::numeric,$6,$7,$8)`,
			cardID, orgID, component.Meter, component.UnitQuantity, component.Amount.ValueMinor,
			component.Amount.Currency, component.Amount.Scale, hash,
		)
		require.NoError(t, err)
	}
}

func requestRateComponents(input, output int64) []enterprisefinance.RateComponent {
	values := []struct {
		meter string
		value int64
	}{
		{enterprisefinance.MeterInputTokens, input},
		{enterprisefinance.MeterOutputTokens, output},
		{enterprisefinance.MeterCacheReadTokens, input / 4},
		{enterprisefinance.MeterCacheCreationTokens, input + 2},
	}
	components := make([]enterprisefinance.RateComponent, 0, len(values))
	for _, value := range values {
		components = append(components, enterprisefinance.RateComponent{
			Meter: value.meter, UnitQuantity: "1000000",
			Amount: enterprisefinance.Amount{
				ValueMinor: fmt.Sprintf("%d", value.value), Currency: "CNY", Scale: 1_000_000,
			},
		})
	}
	return components
}
